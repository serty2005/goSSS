package gateways

import (
	"context"
	"regexp"
	"strings"
	"time"

	"etalon-server/internal/core/integrations"
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"

	"gorm.io/gorm"
)

var (
	descriptionDownloadFileLinkPattern = regexp.MustCompile(`(?i)(src|href)\s*=\s*["'](?:\.?/)?download\?uuid=file\$[0-9]+["']`)
	descriptionStaticFileLinkPattern   = regexp.MustCompile(`(?i)(src|href)\s*=\s*["'](?:/api)?/static/tickets/[^"']+["']`)
)

type TicketGateway interface {
	Start(ctx context.Context)
}

type ticketGatewayImpl struct {
	cfg             *config.Config
	logger          logger.LoggerInterface
	manager         *integrations.Manager
	ticketRepo      tickets.TicketRepository
	bus             eventbus.EventBus
	db              *gorm.DB
	linkRepo        repositories.LinkRepo
	fileSyncService *ticketFileSyncService
}

func NewTicketGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	manager *integrations.Manager,
	ticketRepo tickets.TicketRepository,
	bus eventbus.EventBus,
	db *gorm.DB,
	linkRepo repositories.LinkRepo,
) TicketGateway {
	return &ticketGatewayImpl{
		cfg:             cfg,
		logger:          logger,
		manager:         manager,
		ticketRepo:      ticketRepo,
		bus:             bus,
		db:              db,
		linkRepo:        linkRepo,
		fileSyncService: newTicketFileSyncService(cfg, logger.With("component", "ticket_file_sync_service"), ticketRepo, linkRepo),
	}
}

func (g *ticketGatewayImpl) Start(ctx context.Context) {
	interval := g.cfg.SDeskSyncInterval
	if interval < 1*time.Minute {
		interval = 5 * time.Minute
	}

	g.logger.Info("Запуск шлюза синхронизации заявок (Tickets)", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.syncTickets(ctx)

	for {
		select {
		case <-ticker.C:
			g.syncTickets(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза заявок.")
			return
		}
	}
}

func (g *ticketGatewayImpl) syncTickets(ctx context.Context) {
	g.logger.Info("Начало синхронизации заявок...")

	targetStatuses := []string{
		"registered", "inprogress", "waitClientAnswer", "resumed", "resolved",
	}

	providers := g.manager.GetTicketProviders()
	for _, provider := range providers {
		g.processProvider(ctx, provider, targetStatuses)
	}
}

func (g *ticketGatewayImpl) processProvider(ctx context.Context, provider integration.TicketProvider, statuses []string) {
	log := g.logger.With("system", provider.SystemName())

	receivedTickets, err := provider.GetTickets(ctx, statuses)
	if err != nil {
		log.Error("Ошибка получения списка заявок от провайдера", "error", err)
		return
	}

	log.Info("Получено заявок от провайдера", "count", len(receivedTickets))

	externalUUIDs := make([]string, 0, len(receivedTickets))
	for extUUID, ticket := range receivedTickets {
		if extUUID == "" || ticket == nil {
			continue
		}
		externalUUIDs = append(externalUUIDs, extUUID)
	}

	var filesBySource map[string][]integration.RemoteFile
	if len(externalUUIDs) > 0 {
		batchFiles, batchErr := provider.GetFilesBySources(ctx, externalUUIDs)
		if batchErr != nil {
			log.Warn("Ошибка batch-получения файлов тикетов, будет использован поштучный fallback", "error", batchErr)
		} else {
			filesBySource = batchFiles
		}
	}

	var commentsBySource map[string][]*tickets.Comment
	if len(externalUUIDs) > 0 {
		batchComments, batchErr := provider.GetCommentsBySources(ctx, externalUUIDs)
		if batchErr != nil {
			log.Warn("Ошибка batch-получения комментариев тикетов, будет использован поштучный fallback", "error", batchErr)
		} else {
			commentsBySource = batchComments
		}
	}

	countUpserted := 0
	countRestored := 0
	countContentOK := 0

	for extUUID, ticket := range receivedTickets {
		if extUUID == "" || ticket == nil {
			continue
		}

		localTicket, restored, upsertErr := g.upsertTicket(ctx, provider, extUUID, ticket, log)
		if upsertErr != nil || localTicket == nil {
			continue
		}
		countUpserted++
		if restored {
			countRestored++
		}

		prefetchedFiles, hasPrefetchedFiles := filesBySource[extUUID]
		prefetchedComments, hasPrefetchedComments := commentsBySource[extUUID]
		if err := g.syncTicketContent(ctx, provider, localTicket, prefetchedFiles, hasPrefetchedFiles, prefetchedComments, hasPrefetchedComments, log); err != nil {
			log.Warn("Ошибка синхронизации комментариев/файлов тикета", "ticket_id", localTicket.ID, "external_uuid", extUUID, "error", err)
			continue
		}
		countContentOK++
	}

	log.Info("Синхронизация заявок завершена", "upserted", countUpserted, "restored_links", countRestored, "content_synced", countContentOK)
}

func (g *ticketGatewayImpl) upsertTicket(
	ctx context.Context,
	provider integration.TicketProvider,
	extUUID string,
	ticket *tickets.Ticket,
	log logger.LoggerInterface,
) (*tickets.Ticket, bool, error) {
	if ticket.ServiceDeskUUID == "" {
		ticket.ServiceDeskUUID = extUUID
	}

	link, _ := g.linkRepo.GetByExternalID(ctx, nil, provider.SystemName(), extUUID)
	if link != nil {
		ticket.ID = link.InternalID
		before, _ := g.ticketRepo.GetByID(ctx, ticket.ID)
		if err := g.ticketRepo.Update(ctx, ticket); err != nil {
			log.Error("Ошибка обновления заявки", "id", ticket.ID, "error", err)
			return nil, false, err
		}
		g.recordSyncFieldHistory(ctx, ticket.ID, before, ticket)
		return ticket, false, nil
	}

	existingTicket, err := g.ticketRepo.GetByNumber(ctx, ticket.Number)
	if err == nil && existingTicket != nil {
		log.Info("Найден существующий тикет без связи. Восстановление...", "number", ticket.Number)
		ticket.ID = existingTicket.ID

		newLink := &models.ExternalSystemLink{
			InternalID:      ticket.ID,
			SystemName:      provider.SystemName(),
			ServiceDeskUUID: extUUID,
			EntityType:      "Ticket",
			LastSyncedAt:    time.Now(),
		}
		_ = g.linkRepo.Upsert(ctx, nil, newLink)

		before, _ := g.ticketRepo.GetByID(ctx, ticket.ID)
		if err := g.ticketRepo.Update(ctx, ticket); err != nil {
			log.Error("Ошибка обновления заявки после восстановления связи", "id", ticket.ID, "error", err)
			return nil, false, err
		}
		g.recordSyncFieldHistory(ctx, ticket.ID, before, ticket)
		return ticket, true, nil
	}

	if err := g.ticketRepo.Create(ctx, ticket); err != nil {
		log.Error("Ошибка создания заявки", "uuid", extUUID, "error", err)
		return nil, false, err
	}
	g.addHistory(ctx, ticket.ID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, "", ticket.Status)

	newLink := &models.ExternalSystemLink{
		InternalID:      ticket.ID,
		SystemName:      provider.SystemName(),
		ServiceDeskUUID: extUUID,
		EntityType:      "Ticket",
		LastSyncedAt:    time.Now(),
	}
	_ = g.linkRepo.Upsert(ctx, nil, newLink)
	return ticket, false, nil
}

func (g *ticketGatewayImpl) syncTicketContent(
	ctx context.Context,
	provider integration.TicketProvider,
	ticket *tickets.Ticket,
	prefetchedFiles []integration.RemoteFile,
	hasPrefetchedFiles bool,
	prefetchedComments []*tickets.Comment,
	hasPrefetchedComments bool,
	log logger.LoggerInterface,
) error {
	if ticket == nil || ticket.ServiceDeskUUID == "" {
		return nil
	}

	processedDescription := g.fileSyncService.ProcessInlineContent(
		ctx,
		provider,
		ticket.ID,
		ticket.ServiceDeskUUID,
		ticket.Description,
		tickets.RelationTypeInlineDescription,
		nil,
	)
	if processedDescription != ticket.Description {
		oldDescription := ticket.Description
		ticket.Description = processedDescription
		if err := g.ticketRepo.Update(ctx, ticket); err != nil {
			log.Warn("Ошибка обновления описания тикета с обработанными ссылками", "ticket_id", ticket.ID, "error", err)
		} else if isMeaningfulDescriptionChange(oldDescription, processedDescription) {
			g.addHistory(ctx, ticket.ID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldDescription, oldDescription, processedDescription)
		}
	}

	processedResult := g.fileSyncService.ProcessInlineContent(
		ctx,
		provider,
		ticket.ID,
		ticket.ServiceDeskUUID,
		ticket.Result,
		tickets.RelationTypeInlineResult,
		nil,
	)
	if processedResult != ticket.Result {
		oldResult := ticket.Result
		ticket.Result = processedResult
		if err := g.ticketRepo.Update(ctx, ticket); err != nil {
			log.Warn("Ошибка обновления результата тикета с обработанными ссылками", "ticket_id", ticket.ID, "error", err)
		} else if isMeaningfulDescriptionChange(oldResult, processedResult) {
			g.addHistory(ctx, ticket.ID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldResult, oldResult, processedResult)
		}
	}

	if err := g.syncTicketComments(ctx, provider, ticket, prefetchedComments, hasPrefetchedComments, log); err != nil {
		return err
	}

	if hasPrefetchedFiles {
		if err := g.fileSyncService.SyncDirectTicketFilesWithRemote(ctx, provider, ticket.ID, prefetchedFiles); err != nil {
			return err
		}
	} else if err := g.fileSyncService.SyncDirectTicketFiles(ctx, provider, ticket.ID, ticket.ServiceDeskUUID); err != nil {
		return err
	}

	return nil
}

func (g *ticketGatewayImpl) syncTicketComments(
	ctx context.Context,
	provider integration.TicketProvider,
	ticket *tickets.Ticket,
	prefetchedComments []*tickets.Comment,
	hasPrefetchedComments bool,
	log logger.LoggerInterface,
) error {
	remoteComments := prefetchedComments
	if !hasPrefetchedComments {
		var err error
		remoteComments, err = provider.GetComments(ctx, ticket.ServiceDeskUUID)
		if err != nil {
			return err
		}
	}
	if len(remoteComments) == 0 {
		return nil
	}

	existingMap, err := g.loadCommentMap(ctx, ticket.ID)
	if err != nil {
		return err
	}

	for _, c := range remoteComments {
		if c == nil || strings.TrimSpace(c.UUID) == "" {
			continue
		}

		commentUUID := c.UUID
		processedText := g.fileSyncService.ProcessInlineContent(
			ctx,
			provider,
			ticket.ID,
			ticket.ServiceDeskUUID,
			c.Text,
			tickets.RelationTypeInlineComment,
			&commentUUID,
		)

		creationDate := c.CreationDate
		if creationDate.IsZero() {
			creationDate = time.Now()
		}

		existing, exists := existingMap[c.UUID]
		if exists {
			if existing.Text != processedText || existing.AuthorName != c.AuthorName || existing.IsInternal != c.IsInternal {
				if err := g.db.WithContext(ctx).
					Model(&tickets.TicketComment{}).
					Where("id = ?", existing.ID).
					Updates(map[string]interface{}{
						"text":        processedText,
						"author_name": c.AuthorName,
						"is_internal": c.IsInternal,
					}).Error; err != nil {
					log.Warn("Ошибка обновления комментария", "ticket_id", ticket.ID, "comment_uuid", c.UUID, "error", err)
				}
			}
		} else {
			newComment := tickets.TicketComment{
				TicketID:        ticket.ID,
				ServiceDeskUUID: c.UUID,
				Text:            processedText,
				AuthorName:      c.AuthorName,
				CreationDate:    creationDate,
				IsInternal:      c.IsInternal,
			}
			if err := g.ticketRepo.AddComments(ctx, []tickets.TicketComment{newComment}); err != nil {
				log.Warn("Ошибка сохранения комментария", "ticket_id", ticket.ID, "comment_uuid", c.UUID, "error", err)
				continue
			}
			g.addHistory(ctx, ticket.ID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, "", processedText)
		}
	}

	return nil
}

func (g *ticketGatewayImpl) loadCommentMap(ctx context.Context, ticketID string) (map[string]tickets.TicketComment, error) {
	var items []tickets.TicketComment
	if err := g.db.WithContext(ctx).Where("ticket_id = ?", ticketID).Find(&items).Error; err != nil {
		return nil, err
	}
	result := make(map[string]tickets.TicketComment, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ServiceDeskUUID) == "" {
			continue
		}
		result[item.ServiceDeskUUID] = item
	}
	return result, nil
}

func (g *ticketGatewayImpl) recordSyncFieldHistory(ctx context.Context, ticketID string, before *tickets.Ticket, after *tickets.Ticket) {
	if before == nil || after == nil {
		return
	}

	if before.Status != after.Status {
		g.addHistory(ctx, ticketID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, before.Status, after.Status)
	}
	if isMeaningfulDescriptionChange(before.Description, after.Description) {
		g.addHistory(ctx, ticketID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldDescription, before.Description, after.Description)
	}
	if isMeaningfulDescriptionChange(before.Result, after.Result) {
		g.addHistory(ctx, ticketID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldResult, before.Result, after.Result)
	}
}

func isMeaningfulDescriptionChange(oldDescription, newDescription string) bool {
	return normalizeDescriptionForHistory(oldDescription) != normalizeDescriptionForHistory(newDescription)
}

func normalizeDescriptionForHistory(value string) string {
	normalized := descriptionDownloadFileLinkPattern.ReplaceAllString(value, `$1="__ticket_file__"`)
	normalized = descriptionStaticFileLinkPattern.ReplaceAllString(normalized, `$1="__ticket_file__"`)
	return normalized
}

func (g *ticketGatewayImpl) addHistory(ctx context.Context, ticketID, action, field, oldVal, newVal string) {
	_ = g.ticketRepo.AddHistory(ctx, &tickets.TicketHistory{
		TicketID:  ticketID,
		Action:    action,
		Field:     field,
		OldValue:  oldVal,
		NewValue:  newVal,
		CreatedAt: time.Now(),
	})
}
