package services

import (
	"context"
	"encoding/base64"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	contractsvc "etalon-server/internal/services/contract"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	bitrixDescriptionField = "UF_CRM_1766060620"
	bitrixConnectionsField = "UF_CRM_1770623602"
	bitrixTypeField        = "UF_CRM_1766059110729"
	bitrixPointField       = "UF_CRM_1766062398"
)

var bitrixServicePointSelectFields = []string{
	"ID",
	"NAME",
	"CODE",
	"PROPERTY_361",
	"PROPERTY_681",
}

var bitrixImageTagRe = regexp.MustCompile(`(?is)\[IMG\].*?\[/IMG\]`)
var bitrixStaticFileURLTagRe = regexp.MustCompile(`(?is)\[URL=[^\]]*(?:/api/static/|/static/)[^\]]*\].*?\[/URL\]`)
var bitrixStaticFileSimpleURLTagRe = regexp.MustCompile(`(?is)\[URL\]\s*(?:https?://[^\s\]]+)?(?:/api/static/|/static/)[^\]]*\[/URL\]`)
var bitrixInlineStaticRefRe = regexp.MustCompile(`(?is)\[IMG\].*?\[/IMG\]|\[URL=[^\]]*(?:/api/static/|/static/)[^\]]*\].*?\[/URL\]|\[URL\]\s*(?:https?://[^\s\]]+)?(?:/api/static/|/static/)[^\]]*\[/URL\]`)

type BitrixSyncService interface {
	IsEnabled() bool
	EnsureContactByPhone(ctx context.Context, input BitrixEnsureContactInput) (*BitrixEnsureContactResult, error)
	SyncTicketByID(ctx context.Context, ticketID string) error
	SyncComment(ctx context.Context, ticketID string, comment *tickets.TicketComment, etalonUserID uint) error
	RefreshServicePoints(ctx context.Context) (int, error)
	ListServicePoints(ctx context.Context) ([]bitrix.ServicePoint, error)
	ListCachedUsers(ctx context.Context) ([]bitrix.UserCache, error)
	SearchServicePoints(ctx context.Context, term string, limit, offset int, randomWhenEmpty bool) ([]bitrix.ServicePoint, error)
	SearchBitrixUsersByName(ctx context.Context, firstName, lastName, fullName string) ([]bitrix.UserCache, error)
	RefreshUsers(ctx context.Context) (int, error)
	PreviewServicePointsImport(ctx context.Context, fileName string, content []byte) (*ServicePointImportPreview, error)
	PreviewServicePointsSync(ctx context.Context, fileName string, content []byte, mapping ServicePointImportMapping) (*ServicePointSyncPreview, error)
	ImportServicePoints(ctx context.Context, fileName string, content []byte, mapping ServicePointImportMapping, options ServicePointSyncApplyOptions) (*ServicePointSyncApplyResult, error)
	SyncServicePointsFromDailyReport(ctx context.Context, rows []contractsvc.ContractReportRow) (*ServicePointContractSyncResult, error)
	PreviewContractReportSync(ctx context.Context, rows []contractsvc.ContractReportRow) (*ContractReportSyncPreview, error)
	ExecuteContractReportSync(ctx context.Context, rows []contractsvc.ContractReportRow, options ContractReportSyncExecuteOptions) (*ContractReportSyncExecuteResult, error)
}

type bitrixSyncService struct {
	cfg           *config.Config
	log           logger.LoggerInterface
	client        *b24.Client
	redis         *redis.Client
	ticketRepo    tickets.TicketRepository
	serverRepo    server.Repository
	wsRepo        workstation.Repository
	history       TicketHistoryWriter
	userRepo      user.Repository
	repo          bitrix.Repository
	companyRepo   company.Repository
	telephonyRepo telephony.Repository
}

func NewBitrixSyncService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client *b24.Client,
	redisClient *redis.Client,
	ticketRepo tickets.TicketRepository,
	serverRepo server.Repository,
	wsRepo workstation.Repository,
	userRepo user.Repository,
	repo bitrix.Repository,
	companyRepo company.Repository,
	telephonyRepo telephony.Repository,
) BitrixSyncService {
	return &bitrixSyncService{
		cfg:           cfg,
		log:           log,
		client:        client,
		redis:         redisClient,
		ticketRepo:    ticketRepo,
		serverRepo:    serverRepo,
		wsRepo:        wsRepo,
		history:       NewTicketHistoryWriter(ticketRepo, log.With("component", "ticket_history_writer")),
		userRepo:      userRepo,
		repo:          repo,
		companyRepo:   companyRepo,
		telephonyRepo: telephonyRepo,
	}
}

func (s *bitrixSyncService) IsEnabled() bool {
	return s.canReadBitrix()
}

func (s *bitrixSyncService) ListCachedUsers(ctx context.Context) ([]bitrix.UserCache, error) {
	if s == nil || s.repo == nil {
		return []bitrix.UserCache{}, nil
	}
	return s.repo.ListUserCache(ctx)
}

// canReadBitrix проверяет, доступно ли чтение списка точек и пользователей из Bitrix24.
func (s *bitrixSyncService) canReadBitrix() bool {
	return s.cfg != nil && s.cfg.EnableBitrixGateway && s.client != nil && s.client.IsConfigured()
}

// isContractReportDryRun определяет режим тестового прогона для ежедневного отчета 1С.
func (s *bitrixSyncService) isContractReportDryRun() bool {
	return s.cfg != nil && s.cfg.EnableContractGateway && !s.cfg.BitrixWebhookEnabled
}

func (s *bitrixSyncService) SyncTicketByID(ctx context.Context, ticketID string) error {
	if !s.IsEnabled() {
		return nil
	}
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil || ticket.IsArchived || !ticket.SyncWithBitrix {
		return nil
	}
	if ticket.BitrixServicePointID == nil || *ticket.BitrixServicePointID <= 0 {
		return fmt.Errorf("для синхронизации с Bitrix24 не выбрана точка обслуживания")
	}

	dealID, err := s.upsertDealAndLink(ctx, ticket)
	if err != nil {
		return err
	}
	return s.syncPendingComments(ctx, ticket, dealID)
}

func (s *bitrixSyncService) upsertDealAndLink(ctx context.Context, ticket *tickets.Ticket) (int64, error) {
	if ticket == nil {
		return 0, fmt.Errorf("тикет не найден")
	}

	fields, err := s.buildDealFields(ctx, ticket)
	if err != nil {
		return 0, err
	}

	if s.repo != nil {
		link, linkErr := s.repo.GetDealLinkByTicketID(ctx, ticket.ID)
		if linkErr != nil {
			return 0, linkErr
		}
		if link != nil && link.B24DealID > 0 {
			dealID := link.B24DealID
			if err := s.client.DealUpdate(ctx, dealID, fields); err != nil {
				return 0, err
			}
			s.setDealSuppress(ctx, dealID)
			if err := s.repo.UpsertDealLink(ctx, &bitrix.DealLink{
				TicketID:   ticket.ID,
				B24DealID:  dealID,
				LastSyncAt: time.Now(),
			}); err != nil {
				return 0, err
			}
			return dealID, nil
		}
	}

	deals, err := s.client.DealListByOrigin(ctx, s.cfg.BitrixOriginatorID, ticket.ID)
	if err != nil {
		return 0, err
	}
	if len(deals) > 1 {
		return 0, fmt.Errorf("обнаружено более одной сделки Bitrix24 для тикета %s", ticket.ID)
	}

	var dealID int64
	if len(deals) == 0 {
		dealID, err = s.client.DealAdd(ctx, fields)
		if err != nil {
			return 0, err
		}
	} else {
		dealID = deals[0].ID
		if err := s.client.DealUpdate(ctx, dealID, fields); err != nil {
			return 0, err
		}
	}
	s.setDealSuppress(ctx, dealID)

	if s.repo == nil {
		return dealID, nil
	}
	if err := s.repo.UpsertDealLink(ctx, &bitrix.DealLink{
		TicketID:   ticket.ID,
		B24DealID:  dealID,
		LastSyncAt: time.Now(),
	}); err != nil {
		return 0, err
	}
	return dealID, nil
}

func (s *bitrixSyncService) SyncComment(ctx context.Context, ticketID string, comment *tickets.TicketComment, etalonUserID uint) error {
	if !s.IsEnabled() || comment == nil || strings.TrimSpace(comment.ID) == "" || comment.IsPrivate {
		return nil
	}
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil || ticket.IsArchived || !ticket.SyncWithBitrix {
		return nil
	}
	if ticket.BitrixServicePointID == nil || *ticket.BitrixServicePointID <= 0 {
		return nil
	}

	link, err := s.repo.GetDealLinkByTicketID(ctx, ticketID)
	if err != nil {
		return err
	}
	if link == nil {
		dealID, ensureErr := s.upsertDealAndLink(ctx, ticket)
		if ensureErr != nil {
			return ensureErr
		}
		link = &bitrix.DealLink{
			TicketID:  ticketID,
			B24DealID: dealID,
		}
	}

	existing, err := s.repo.GetCommentLinkByEtalonID(ctx, comment.ID)
	if err != nil {
		return err
	}
	authorID, err := s.resolveBitrixUserID(ctx, etalonUserID)
	if err != nil {
		return err
	}
	authorName, err := s.resolveEtalonUserDisplayName(ctx, etalonUserID)
	if err != nil {
		return err
	}
	message := s.buildCommentBody(ctx, ticketID, comment, authorID, authorName)
	if authorID != nil {
		s.log.Info("Bitrix24: определен пользователь для внутренней ссылки автора комментария", "ticket_id", ticketID, "comment_id", comment.ID, "etalon_user_id", etalonUserID, "b24_user_id", *authorID)
	}

	files, err := s.buildCommentFilesPayload(ctx, ticketID, comment)
	if err != nil {
		return err
	}

	if existing != nil {
		if err := s.client.TimelineCommentUpdateWithFiles(ctx, existing.B24CommentID, message, files); err != nil {
			return err
		}
		s.setCommentSuppress(ctx, existing.B24CommentID)
		if len(files) > 0 {
			finalText, textErr := s.rewriteCommentForBitrixImagePreview(ctx, existing.B24CommentID, message, files)
			if textErr != nil {
				return textErr
			}
			if strings.TrimSpace(finalText) != strings.TrimSpace(message) {
				if err := s.client.TimelineCommentUpdateWithFiles(ctx, existing.B24CommentID, finalText, nil); err != nil {
					return err
				}
				s.setCommentSuppress(ctx, existing.B24CommentID)
			}
		}
		return nil
	}

	var b24ID int64
	if len(files) > 0 {
		b24ID, err = s.client.TimelineCommentAddWithFiles(ctx, "deal", link.B24DealID, message, files)
	} else {
		b24ID, err = s.client.TimelineCommentAdd(ctx, link.B24DealID, message, nil)
	}
	if err != nil {
		return err
	}
	s.setCommentSuppress(ctx, b24ID)
	if len(files) > 0 {
		finalText, textErr := s.rewriteCommentForBitrixImagePreview(ctx, b24ID, message, files)
		if textErr != nil {
			return textErr
		}
		if strings.TrimSpace(finalText) != strings.TrimSpace(message) {
			if err := s.client.TimelineCommentUpdateWithFiles(ctx, b24ID, finalText, nil); err != nil {
				return err
			}
			s.setCommentSuppress(ctx, b24ID)
		}
	}

	return s.repo.UpsertCommentLink(ctx, &bitrix.CommentLink{
		EtalonCommentID: comment.ID,
		B24CommentID:    b24ID,
		TicketID:        ticketID,
		Direction:       "etalon_to_b24",
	})
}

func (s *bitrixSyncService) RefreshServicePoints(ctx context.Context) (int, error) {
	if !s.IsEnabled() {
		return 0, nil
	}
	iblockID := s.cfg.BitrixServicePointsIBlockID
	iblockType, err := s.client.ListsGetIblockTypeID(ctx, iblockID)
	if err != nil {
		return 0, err
	}
	items, err := s.client.ListsElementGetAll(ctx, iblockType, iblockID, bitrixServicePointSelectFields)
	if err != nil {
		return 0, err
	}
	all := make([]bitrix.ServicePoint, 0, 512)
	for _, item := range items {
		all = append(all, bitrix.ServicePoint{
			B24ElementID: item.ID,
			Name:         item.Name,
			RawJSON:      item.RawJSON,
			UpdatedAt:    time.Now(),
		})
	}
	if err := s.repo.ReplaceServicePoints(ctx, all); err != nil {
		return 0, err
	}
	return len(all), nil
}

func (s *bitrixSyncService) ListServicePoints(ctx context.Context) ([]bitrix.ServicePoint, error) {
	return s.repo.ListServicePoints(ctx)
}

func (s *bitrixSyncService) SearchServicePoints(ctx context.Context, term string, limit, offset int, randomWhenEmpty bool) ([]bitrix.ServicePoint, error) {
	return s.repo.SearchServicePoints(ctx, term, limit, offset, randomWhenEmpty)
}

func (s *bitrixSyncService) SearchBitrixUsersByName(ctx context.Context, firstName, lastName, fullName string) ([]bitrix.UserCache, error) {
	items, err := s.repo.ListUserCache(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []bitrix.UserCache{}, nil
	}

	userFirst := normalizePersonToken(firstName)
	userLast := normalizePersonToken(lastName)
	userFull := normalizePersonToken(fullName)

	result := make([]bitrix.UserCache, 0, 1)
	for i := range items {
		item := items[i]
		if !item.Active {
			continue
		}
		cacheFirst := normalizePersonToken(item.FirstName)
		cacheLast := normalizePersonToken(item.LastName)
		cacheFull := normalizePersonToken(strings.Join([]string{item.LastName, item.FirstName, item.SecondName}, " "))

		if userFirst != "" && userLast != "" && userFirst == cacheFirst && userLast == cacheLast {
			result = append(result, item)
			continue
		}
		if userFull != "" && userFull == cacheFull {
			result = append(result, item)
		}
	}
	return result, nil
}

func (s *bitrixSyncService) RefreshUsers(ctx context.Context) (int, error) {
	if !s.IsEnabled() {
		return 0, nil
	}
	page, err := s.client.UserGetAll(ctx)
	if err != nil {
		return 0, err
	}
	users := make([]bitrix.UserCache, 0, len(page))
	now := time.Now()
	for _, u := range page {
		users = append(users, bitrix.UserCache{
			B24UserID:  u.ID,
			Name:       strings.TrimSpace(strings.Join([]string{u.LastName, u.FirstName, u.SecondName}, " ")),
			Active:     u.Active,
			LastName:   u.LastName,
			FirstName:  u.FirstName,
			SecondName: u.SecondName,
			Email:      u.Email,
			Phone:      u.Phone,
			LastSeenAt: &now,
			UpdatedAt:  now,
		})
	}
	if err := s.repo.ReplaceUserCache(ctx, users); err != nil {
		return 0, err
	}
	_ = s.rebuildUserMapFromExternalIDs(ctx)
	return len(users), nil
}

func (s *bitrixSyncService) syncPendingComments(ctx context.Context, ticket *tickets.Ticket, dealID int64) error {
	comments, err := s.ticketRepo.GetComments(ctx, ticket.ID)
	if err != nil {
		return err
	}
	for i := range comments {
		comment := comments[i]
		if comment.IsPrivate {
			continue
		}
		existing, err := s.repo.GetCommentLinkByEtalonID(ctx, comment.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		message := s.buildCommentBody(ctx, ticket.ID, &comment, nil, "")
		files, err := s.buildCommentFilesPayload(ctx, ticket.ID, &comment)
		if err != nil {
			return err
		}
		var b24ID int64
		if len(files) > 0 {
			b24ID, err = s.client.TimelineCommentAddWithFiles(ctx, "deal", dealID, message, files)
		} else {
			b24ID, err = s.client.TimelineCommentAdd(ctx, dealID, message, nil)
		}
		if err != nil {
			return err
		}
		s.setCommentSuppress(ctx, b24ID)
		if len(files) > 0 {
			finalText, textErr := s.rewriteCommentForBitrixImagePreview(ctx, b24ID, message, files)
			if textErr != nil {
				return textErr
			}
			if strings.TrimSpace(finalText) != strings.TrimSpace(message) {
				if err := s.client.TimelineCommentUpdateWithFiles(ctx, b24ID, finalText, nil); err != nil {
					return err
				}
				s.setCommentSuppress(ctx, b24ID)
			}
		}
		if err := s.repo.UpsertCommentLink(ctx, &bitrix.CommentLink{
			EtalonCommentID: comment.ID,
			B24CommentID:    b24ID,
			TicketID:        ticket.ID,
			Direction:       "etalon_to_b24",
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *bitrixSyncService) setDealSuppress(ctx context.Context, dealID int64) {
	if s.redis == nil || dealID <= 0 {
		return
	}
	ttl := s.cfg.BitrixSuppressTTL
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	if err := s.redis.Set(ctx, fmt.Sprintf("b24:suppress:deal:%d", dealID), "1", ttl).Err(); err != nil {
		s.log.Warn("Bitrix24: не удалось установить suppress ключ сделки", "deal_id", dealID, "error", err)
	}
}

func (s *bitrixSyncService) setCommentSuppress(ctx context.Context, commentID int64) {
	if s.redis == nil || commentID <= 0 {
		return
	}
	ttl := s.cfg.BitrixSuppressTTL
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	if err := s.redis.Set(ctx, fmt.Sprintf("b24:suppress:comment:%d", commentID), "1", ttl).Err(); err != nil {
		s.log.Warn("Bitrix24: не удалось установить suppress ключ комментария", "comment_id", commentID, "error", err)
	}
}

func (s *bitrixSyncService) pullCommentsForDeal(ctx context.Context, ticket *tickets.Ticket, dealID int64) (int, error) {
	imported := 0
	start := 0
	for {
		items, next, err := s.client.TimelineCommentList(ctx, dealID, start)
		if err != nil {
			return imported, err
		}
		for _, item := range items {
			existing, err := s.repo.GetCommentLinkByB24ID(ctx, item.ID)
			if err != nil {
				return imported, err
			}
			if existing != nil {
				continue
			}

			authorName := "Сотрудник Bitrix24"
			if item.AuthorID != nil {
				if userMap, _ := s.repo.GetUserMapByB24ID(ctx, *item.AuthorID); userMap != nil {
					if u, _ := s.userRepo.GetByID(ctx, userMap.EtalonUserID); u != nil && strings.TrimSpace(u.FullName) != "" {
						authorName = strings.TrimSpace(u.FullName)
					}
				} else {
					authorName = "Bitrix24 #" + strconv.FormatInt(*item.AuthorID, 10)
				}
			}

			commentID := fmt.Sprintf("b24-%d", item.ID)
			newComment := tickets.TicketComment{
				ID:              commentID,
				TicketID:        ticket.ID,
				ServiceDeskUUID: commentID,
				Text: func() string {
					cleaned, _ := normalizeBitrixCommentForEtalon(item.Comment, item.AuthorID, s.cfg.BitrixIntegrationUserID)
					return cleaned
				}(),
				AuthorName:   authorName,
				CreationDate: time.Now(),
				IsInternal:   false,
				IsPrivate:    false,
			}
			if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{newComment}); err != nil {
				return imported, err
			}
			if err := s.repo.UpsertCommentLink(ctx, &bitrix.CommentLink{
				EtalonCommentID: commentID,
				B24CommentID:    item.ID,
				TicketID:        ticket.ID,
				Direction:       "b24_to_etalon",
			}); err != nil {
				return imported, err
			}
			imported++
		}
		if next <= 0 {
			break
		}
		start = next
	}
	return imported, nil
}

func (s *bitrixSyncService) buildDealFields(ctx context.Context, ticket *tickets.Ticket) (map[string]interface{}, error) {
	stageCode := mapTicketStatusToStage(ticket.Status)
	connections := s.buildDealConnections(ctx, ticket)
	fields := map[string]interface{}{
		"CATEGORY_ID":          s.cfg.BitrixCategoryID,
		"ORIGINATOR_ID":        s.cfg.BitrixOriginatorID,
		"ORIGIN_ID":            ticket.ID,
		"STAGE_ID":             "C17:" + stageCode,
		bitrixDescriptionField: "",
		"COMMENTS":             s.buildDealComment(ticket),
		bitrixConnectionsField: connections,
		bitrixTypeField:        mapTicketTypeToBitrixID(ticket.Type),
		bitrixPointField:       ticket.BitrixServicePointID,
	}

	if ticket.ContactID != nil && *ticket.ContactID > 0 && s.telephonyRepo != nil {
		contact, err := s.telephonyRepo.GetContactByID(ctx, *ticket.ContactID)
		if err != nil {
			return nil, err
		}
		if contact != nil {
			bitrixContact, err := s.ensureBitrixContactForLocalContact(ctx, contact)
			if err != nil {
				return nil, err
			}
			if bitrixContact != nil && bitrixContact.ContactID > 0 {
				fields["CONTACT_ID"] = bitrixContact.ContactID
			}
		}
	}

	if ticket.AssigneeID != nil {
		assignedID, err := s.resolveBitrixUserID(ctx, *ticket.AssigneeID)
		if err != nil {
			return nil, err
		}
		if assignedID != nil && *assignedID > 0 {
			fields["ASSIGNED_BY_ID"] = *assignedID
			s.log.Info("Bitrix24: определен ASSIGNED_BY_ID по исполнителю", "ticket_id", ticket.ID, "etalon_user_id", *ticket.AssigneeID, "b24_user_id", *assignedID)
		} else {
			s.log.Warn("Bitrix24: ASSIGNED_BY_ID не найден по исполнителю", "ticket_id", ticket.ID, "etalon_user_id", *ticket.AssigneeID)
		}
	} else if ticket.ReporterID != nil {
		assignedID, err := s.resolveBitrixUserID(ctx, *ticket.ReporterID)
		if err != nil {
			return nil, err
		}
		if assignedID != nil && *assignedID > 0 {
			fields["ASSIGNED_BY_ID"] = *assignedID
			s.log.Info("Bitrix24: определен ASSIGNED_BY_ID по автору", "ticket_id", ticket.ID, "etalon_user_id", *ticket.ReporterID, "b24_user_id", *assignedID)
		} else {
			s.log.Warn("Bitrix24: ASSIGNED_BY_ID не найден по автору", "ticket_id", ticket.ID, "etalon_user_id", *ticket.ReporterID)
		}
	}
	return fields, nil
}

func (s *bitrixSyncService) buildDealConnections(ctx context.Context, ticket *tickets.Ticket) []string {
	if ticket == nil || strings.TrimSpace(ticket.CompanyID) == "" || s.serverRepo == nil || s.wsRepo == nil {
		return []string{}
	}

	ownerIDs := []string{strings.TrimSpace(ticket.CompanyID)}
	servers, err := s.serverRepo.FindByOwnerIDs(ctx, ownerIDs)
	if err != nil {
		s.log.Warn("Bitrix24: не удалось получить серверы для поля подключений", "ticket_id", ticket.ID, "company_id", ticket.CompanyID, "error", err)
	}
	workstations, wsErr := s.wsRepo.FindByOwnerIDs(ctx, ownerIDs)
	if wsErr != nil {
		s.log.Warn("Bitrix24: не удалось получить рабочие станции для поля подключений", "ticket_id", ticket.ID, "company_id", ticket.CompanyID, "error", wsErr)
	}

	out := make([]string, 0, len(workstations)+1)

	if srv := selectPrimaryServer(ticket, servers); srv != nil {
		out = append(out, formatServerConnectionBlock(ticket.CompanyName, srv))
	}

	sortWorkstationsByPriority(workstations)
	for _, ws := range workstations {
		remote := collectRemoteConnectionIDs(ws.Teamviewer, ws.Anydesk, ws.Litemanager, nil)
		if len(remote) == 0 {
			continue
		}
		out = append(out, formatWorkstationConnectionBlock(ws, remote))
	}

	return out
}

func selectPrimaryServer(ticket *tickets.Ticket, servers []server.Server) *server.Server {
	if len(servers) == 0 {
		return nil
	}

	if ticket != nil && ticket.AssetType != nil &&
		strings.EqualFold(strings.TrimSpace(*ticket.AssetType), tickets.AssetTypeServer) &&
		ticket.AssetID != nil && strings.TrimSpace(*ticket.AssetID) != "" {
		for i := range servers {
			if strings.TrimSpace(servers[i].ID) == strings.TrimSpace(*ticket.AssetID) {
				return &servers[i]
			}
		}
	}

	sort.SliceStable(servers, func(i, j int) bool {
		leftName := displayServerName(servers[i])
		rightName := displayServerName(servers[j])
		leftRank := connectionPriorityRank(leftName)
		rightRank := connectionPriorityRank(rightName)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftName = strings.ToLower(strings.TrimSpace(leftName))
		rightName = strings.ToLower(strings.TrimSpace(rightName))
		if leftName != rightName {
			return leftName < rightName
		}
		return strings.TrimSpace(servers[i].ID) < strings.TrimSpace(servers[j].ID)
	})
	return &servers[0]
}

func sortWorkstationsByPriority(workstations []workstation.Workstation) {
	sort.SliceStable(workstations, func(i, j int) bool {
		leftName := ptrString(workstations[i].DeviceName)
		rightName := ptrString(workstations[j].DeviceName)
		leftRank := connectionPriorityRank(leftName)
		rightRank := connectionPriorityRank(rightName)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftName = strings.ToLower(strings.TrimSpace(leftName))
		rightName = strings.ToLower(strings.TrimSpace(rightName))
		if leftName != rightName {
			return leftName < rightName
		}
		return strings.TrimSpace(workstations[i].ID) < strings.TrimSpace(workstations[j].ID)
	})
}

func connectionPriorityRank(name string) int {
	v := strings.ToLower(strings.TrimSpace(name))
	if strings.Contains(v, "гк") || strings.Contains(v, "main") {
		return 0
	}
	if strings.Contains(v, "втк") {
		return 1
	}
	return 2
}

func formatServerConnectionBlock(companyName string, srv *server.Server) string {
	uniqueID := ptrString(srv.UniqueID)
	if uniqueID == "" {
		uniqueID = "-"
	}
	serverURL := ptrString(srv.IP)
	if serverURL == "" {
		serverURL = "-"
	}
	partnerLink := buildPartnerLink(ptrString(srv.CabinetLink), ptrString(srv.IP))
	if partnerLink == "" {
		partnerLink = "-"
	}
	version := ptrString(srv.ServerVersion)
	if version == "" {
		version = "-"
	}

	company := strings.TrimSpace(companyName)
	if company == "" {
		company = "-"
	}

	return strings.Join([]string{
		company,
		"1. UID: " + uniqueID,
		"2. Link to partner account: " + partnerLink,
		"3. iiko version: " + version,
		"4. URL: " + serverURL,
	}, "\n")
}

func formatWorkstationConnectionBlock(ws workstation.Workstation, remote []string) string {
	name := ptrString(ws.DeviceName)
	if name == "" {
		name = ws.ID
	}

	lines := make([]string, 0, len(remote)+1)
	lines = append(lines, name)
	for i, item := range remote {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, item))
	}

	return strings.Join(lines, "\n")
}

func displayServerName(srv server.Server) string {
	if strings.TrimSpace(ptrString(srv.DeviceName)) != "" {
		return strings.TrimSpace(ptrString(srv.DeviceName))
	}
	if strings.TrimSpace(ptrString(srv.ServerName)) != "" {
		return strings.TrimSpace(ptrString(srv.ServerName))
	}
	return strings.TrimSpace(srv.ID)
}

func collectRemoteConnectionIDs(teamviewer, anydesk, litemanager, rdp *string) []string {
	out := make([]string, 0, 4)
	if v := strings.TrimSpace(ptrString(teamviewer)); v != "" {
		out = append(out, "TeamViewer: "+v)
	}
	if v := strings.TrimSpace(ptrString(anydesk)); v != "" {
		out = append(out, "AnyDesk: "+v)
	}
	if v := strings.TrimSpace(ptrString(litemanager)); v != "" {
		out = append(out, "LiteManager: "+v)
	}
	if v := strings.TrimSpace(ptrString(rdp)); v != "" {
		out = append(out, "RDP: "+v)
	}
	return out
}

func buildPartnerLink(cabinetLink, ip string) string {
	clientID := extractClientID(cabinetLink)
	if clientID == "" {
		return ""
	}
	if looksLikeSyrveCloud(ip) {
		return "https://pp.syrve.com/en/cabinet/client-area/index.html?clientId=" + clientID
	}
	return "https://pp.iiko.ru/ru/cabinet/client-area/index.html?clientId=" + clientID
}

func extractClientID(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	match := digitSequenceRe.FindString(v)
	return strings.TrimSpace(match)
}

func looksLikeSyrveCloud(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(v, "syrve.online") || strings.Contains(v, "syrve.app")
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (s *bitrixSyncService) buildCommentBody(ctx context.Context, _ string, comment *tickets.TicketComment, authorID *int64, authorName string) string {
	if comment == nil {
		return ""
	}
	commentBody := convertEtalonHTMLToBitrix(comment.Text, func(etalonUserID uint) (*int64, bool) {
		bitrixUserID, err := s.resolveBitrixUserID(ctx, etalonUserID)
		if err != nil || bitrixUserID == nil || *bitrixUserID <= 0 {
			return nil, false
		}
		return bitrixUserID, true
	})
	return s.withBitrixAuthorMention(commentBody, authorID, authorName)
}

func (s *bitrixSyncService) buildDealDescription(ticket *tickets.Ticket) string {
	if ticket == nil {
		return ""
	}
	body := convertEtalonHTMLToBitrix(ticket.Description, func(etalonUserID uint) (*int64, bool) {
		bitrixUserID, err := s.resolveBitrixUserID(context.Background(), etalonUserID)
		if err != nil || bitrixUserID == nil || *bitrixUserID <= 0 {
			return nil, false
		}
		return bitrixUserID, true
	})
	body = stripBitrixServiceDescriptionPrefix(body, ticket.ID)
	return body
}

func (s *bitrixSyncService) buildDealComment(ticket *tickets.Ticket) string {
	if ticket == nil {
		return ""
	}
	url := s.buildTicketURL(ticket.ID)
	if strings.TrimSpace(url) == "" {
		return ""
	}
	body := fmt.Sprintf("[p]\n[url=%s]Тикет в XD #%d[/url]\n[/p]", url, ticket.Number)
	description := strings.TrimSpace(s.buildDealDescription(ticket))
	if description == "" {
		return body
	}
	return body + "\n" + description
}

func (s *bitrixSyncService) buildTicketURL(ticketID string) string {
	path := "/tickets/" + strings.TrimSpace(ticketID)
	base := strings.TrimRight(strings.TrimSpace(s.cfg.EtalonTicketBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
}

func stripBitrixServiceDescriptionPrefix(body string, ticketID string) string {
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"))
	if text == "" {
		return ""
	}
	ticketPath := "/tickets/" + strings.TrimSpace(ticketID)
	if ticketPath == "/tickets/" {
		return text
	}
	for {
		next, removed := consumeBitrixServiceBlock(text, ticketPath)
		if !removed {
			break
		}
		text = strings.TrimSpace(next)
	}
	return text
}

func consumeBitrixServiceBlock(text, ticketPath string) (string, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return text, false
	}
	first := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(first, "Тикет Etalon #") && !strings.HasPrefix(first, "Тикет в XD #") {
		return text, false
	}

	restIndex := -1
	if len(lines) >= 2 && strings.Contains(strings.TrimSpace(lines[1]), ticketPath) {
		restIndex = 2
	}
	if restIndex < 0 && len(lines) >= 3 && strings.Contains(strings.TrimSpace(lines[2]), ticketPath) {
		restIndex = 3
	}
	if restIndex < 0 {
		return text, false
	}
	for restIndex < len(lines) && strings.TrimSpace(lines[restIndex]) == "" {
		restIndex++
	}
	if restIndex >= len(lines) {
		return "", true
	}
	return strings.Join(lines[restIndex:], "\n"), true
}

func (s *bitrixSyncService) withBitrixAuthorMention(message string, authorID *int64, authorName string) string {
	body := strings.TrimSpace(message)
	if body == "" {
		return body
	}
	if authorID == nil || *authorID <= 0 {
		return body
	}
	name := sanitizeBitrixMentionName(authorName)
	if name == "" {
		name = "Пользователь Etalon"
	}
	return fmt.Sprintf("[USER=%d]%s[/USER] %s", *authorID, name, body)
}

func sanitizeBitrixMentionName(name string) string {
	replacer := strings.NewReplacer("[", "", "]", "", "\n", " ", "\r", " ", "\t", " ")
	return strings.TrimSpace(replacer.Replace(name))
}

func (s *bitrixSyncService) resolveEtalonUserDisplayName(ctx context.Context, etalonUserID uint) (string, error) {
	if etalonUserID == 0 {
		return "", nil
	}
	u, err := s.userRepo.GetByID(ctx, etalonUserID)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", nil
	}
	if strings.TrimSpace(u.FullName) != "" {
		return strings.TrimSpace(u.FullName), nil
	}
	return strings.TrimSpace(strings.Join([]string{u.LastName, u.FirstName}, " ")), nil
}

func (s *bitrixSyncService) buildCommentFilesPayload(
	ctx context.Context,
	ticketID string,
	comment *tickets.TicketComment,
) ([]b24.FileToUpload, error) {
	if comment == nil || strings.TrimSpace(ticketID) == "" {
		return []b24.FileToUpload{}, nil
	}

	links, err := s.ticketRepo.GetTicketFileLinksByRelation(ctx, ticketID, []string{
		tickets.RelationTypeInlineComment,
		tickets.RelationTypeDirectTicketAttachment,
	})
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return []b24.FileToUpload{}, nil
	}

	commentKeys := map[string]struct{}{}
	if value := strings.TrimSpace(comment.ServiceDeskUUID); value != "" {
		commentKeys[value] = struct{}{}
	}
	if value := strings.TrimSpace(comment.ID); value != "" {
		commentKeys[value] = struct{}{}
	}
	if len(commentKeys) == 0 {
		return []b24.FileToUpload{}, nil
	}

	fileIDs := make([]string, 0, len(links))
	seenFiles := make(map[string]struct{}, len(links))
	for _, link := range links {
		if strings.TrimSpace(link.RelationType) == tickets.RelationTypeInlineComment {
			if link.CommentUUID == nil {
				continue
			}
			commentKey := strings.TrimSpace(*link.CommentUUID)
			if _, ok := commentKeys[commentKey]; !ok {
				continue
			}
		} else if strings.TrimSpace(link.RelationType) == tickets.RelationTypeDirectTicketAttachment {
			asset, err := s.ticketRepo.GetFileAssetByID(ctx, link.FileID)
			if err != nil {
				return nil, err
			}
			if asset == nil {
				continue
			}
			if !commentTextReferencesStorageKey(comment.Text, asset.StorageKey) {
				continue
			}
		} else {
			continue
		}
		fileID := strings.TrimSpace(link.FileID)
		if fileID == "" {
			continue
		}
		if _, exists := seenFiles[fileID]; exists {
			continue
		}
		seenFiles[fileID] = struct{}{}
		fileIDs = append(fileIDs, fileID)
	}
	if len(fileIDs) == 0 {
		return []b24.FileToUpload{}, nil
	}
	basePath := ""
	if s.cfg != nil {
		basePath = strings.TrimSpace(s.cfg.TicketStoragePath)
	}
	if basePath == "" {
		return nil, fmt.Errorf("не задан путь хранилища файлов тикетов")
	}

	result := make([]b24.FileToUpload, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		asset, err := s.ticketRepo.GetFileAssetByID(ctx, fileID)
		if err != nil {
			return nil, err
		}
		if asset == nil {
			continue
		}

		storageKey := strings.TrimSpace(asset.StorageKey)
		if storageKey == "" {
			continue
		}
		absPath := filepath.Join(basePath, filepath.FromSlash(storageKey))
		content, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}

		fileName := strings.TrimSpace(asset.OriginalName)
		if fileName == "" {
			fileName = filepath.Base(storageKey)
		}
		if fileName == "" {
			continue
		}

		result = append(result, b24.FileToUpload{
			Name:          fileName,
			Base64Content: base64.StdEncoding.EncodeToString(content),
		})
	}
	return result, nil
}

func commentTextReferencesStorageKey(commentText string, storageKey string) bool {
	text := strings.TrimSpace(commentText)
	key := strings.TrimSpace(storageKey)
	if text == "" || key == "" {
		return false
	}
	path := "/api/static/tickets/" + strings.TrimLeft(filepath.ToSlash(key), "/")
	if strings.Contains(text, path) {
		return true
	}
	if strings.HasPrefix(path, "/api/") {
		legacy := strings.TrimPrefix(path, "/api")
		if strings.Contains(text, legacy) {
			return true
		}
	}
	return false
}

func (s *bitrixSyncService) rewriteCommentForBitrixImagePreview(
	ctx context.Context,
	commentID int64,
	commentText string,
	files []b24.FileToUpload,
) (string, error) {
	if commentID <= 0 || len(files) == 0 {
		return commentText, nil
	}
	if len(bitrixImageTagRe.FindAllString(commentText, -1)) == 0 &&
		len(bitrixStaticFileURLTagRe.FindAllString(commentText, -1)) == 0 &&
		len(bitrixStaticFileSimpleURLTagRe.FindAllString(commentText, -1)) == 0 {
		return commentText, nil
	}
	item, err := s.client.TimelineCommentGet(ctx, commentID)
	if err != nil {
		return commentText, err
	}
	if item == nil || len(item.Raw) == 0 {
		return commentText, nil
	}
	diskIDs := matchImageDiskFileIDsByName(item.Raw, files)
	if len(diskIDs) == 0 {
		diskIDs = b24.ExtractTimelineCommentDiskFileIDs(item.Raw)
	}
	if len(diskIDs) == 0 {
		return commentText, nil
	}
	return replaceBitrixInlineFileReferencesWithDiskMarkers(commentText, diskIDs), nil
}

func matchImageDiskFileIDsByName(raw map[string]interface{}, files []b24.FileToUpload) []int64 {
	type imageEntry struct {
		id   int64
		name string
	}
	imagesByName := make(map[string][]int64)
	fallback := make([]int64, 0, 4)

	filesRaw, ok := raw["FILES"]
	if !ok {
		return []int64{}
	}
	imageEntries := extractImageEntriesFromRawFiles(filesRaw)
	for _, entry := range imageEntries {
		if entry.id <= 0 {
			continue
		}
		fallback = append(fallback, entry.id)
		if entry.name != "" {
			key := strings.ToLower(strings.TrimSpace(entry.name))
			imagesByName[key] = append(imagesByName[key], entry.id)
		}
	}
	if len(fallback) == 0 {
		return []int64{}
	}

	ordered := make([]int64, 0, len(fallback))
	used := make(map[int64]struct{}, len(fallback))
	for _, file := range files {
		nameKey := strings.ToLower(strings.TrimSpace(file.Name))
		if nameKey == "" {
			continue
		}
		queue := imagesByName[nameKey]
		if len(queue) == 0 {
			continue
		}
		id := queue[0]
		imagesByName[nameKey] = queue[1:]
		if id <= 0 {
			continue
		}
		if _, exists := used[id]; exists {
			continue
		}
		used[id] = struct{}{}
		ordered = append(ordered, id)
	}
	if len(ordered) == 0 {
		return fallback
	}
	for _, id := range fallback {
		if _, exists := used[id]; exists {
			continue
		}
		ordered = append(ordered, id)
	}
	return ordered
}

func extractImageEntriesFromRawFiles(filesRaw interface{}) []struct {
	id   int64
	name string
} {
	out := make([]struct {
		id   int64
		name string
	}, 0, 4)
	switch files := filesRaw.(type) {
	case map[string]interface{}:
		for key, value := range files {
			m, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			if !isImageFileEntry(m) {
				continue
			}
			id := int64FromAny(m["id"])
			if id <= 0 {
				id = int64FromAny(m["ID"])
			}
			if id <= 0 {
				id = int64FromAny(key)
			}
			name := strings.TrimSpace(toString(m["name"]))
			if name == "" {
				name = strings.TrimSpace(toString(m["NAME"]))
			}
			if id > 0 {
				out = append(out, struct {
					id   int64
					name string
				}{id: id, name: name})
			}
		}
	case []interface{}:
		for _, value := range files {
			m, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			if !isImageFileEntry(m) {
				continue
			}
			id := int64FromAny(m["id"])
			if id <= 0 {
				id = int64FromAny(m["ID"])
			}
			name := strings.TrimSpace(toString(m["name"]))
			if name == "" {
				name = strings.TrimSpace(toString(m["NAME"]))
			}
			if id > 0 {
				out = append(out, struct {
					id   int64
					name string
				}{id: id, name: name})
			}
		}
	}
	return out
}

func isImageFileEntry(raw map[string]interface{}) bool {
	if len(raw) == 0 {
		return false
	}
	if hasImageMarker(raw["image"]) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(toString(raw["type"])), "image") {
		return true
	}
	contentType := strings.ToLower(strings.TrimSpace(toString(raw["contentType"])))
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	contentType = strings.ToLower(strings.TrimSpace(toString(raw["CONTENT_TYPE"])))
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(toString(raw["name"])))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(toString(raw["NAME"])))
	}
	return strings.HasSuffix(name, ".png") ||
		strings.HasSuffix(name, ".jpg") ||
		strings.HasSuffix(name, ".jpeg") ||
		strings.HasSuffix(name, ".webp") ||
		strings.HasSuffix(name, ".gif") ||
		strings.HasSuffix(name, ".bmp") ||
		strings.HasSuffix(name, ".svg")
}

func hasImageMarker(value interface{}) bool {
	if boolFromAny(value) {
		return true
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return len(typed) > 0
	case []interface{}:
		return len(typed) > 0
	default:
		return false
	}
}

func replaceBitrixImageTagsWithDiskMarkers(comment string, imageDiskIDs []int64) string {
	if len(imageDiskIDs) == 0 {
		return comment
	}
	text := strings.TrimSpace(comment)
	if text == "" {
		return comment
	}
	index := 0
	result := bitrixImageTagRe.ReplaceAllStringFunc(text, func(_ string) string {
		if index >= len(imageDiskIDs) {
			return ""
		}
		id := imageDiskIDs[index]
		index++
		return fmt.Sprintf("[DISK FILE ID=n%d]", id)
	})
	for ; index < len(imageDiskIDs); index++ {
		result = strings.TrimSpace(result + "\n" + fmt.Sprintf("[DISK FILE ID=n%d]", imageDiskIDs[index]))
	}
	return strings.TrimSpace(result)
}

func replaceBitrixInlineFileReferencesWithDiskMarkers(comment string, diskIDs []int64) string {
	if len(diskIDs) == 0 {
		return comment
	}
	text := strings.TrimSpace(comment)
	if text == "" {
		return comment
	}

	index := 0
	replaceNext := func() string {
		if index >= len(diskIDs) {
			return ""
		}
		id := diskIDs[index]
		index++
		return fmt.Sprintf("[DISK FILE ID=n%d]", id)
	}

	result := bitrixInlineStaticRefRe.ReplaceAllStringFunc(text, func(_ string) string {
		return replaceNext()
	})
	for ; index < len(diskIDs); index++ {
		result = strings.TrimSpace(result + "\n" + fmt.Sprintf("[DISK FILE ID=n%d]", diskIDs[index]))
	}
	return strings.TrimSpace(result)
}

func boolFromAny(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		value := strings.TrimSpace(strings.ToLower(x))
		return value == "true" || value == "1" || value == "y" || value == "yes"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return int64(x) != 0
	default:
		return false
	}
}

func (s *bitrixSyncService) resolveBitrixUserID(ctx context.Context, etalonUserID uint) (*int64, error) {
	if etalonUserID == 0 {
		return nil, nil
	}
	item, err := s.repo.GetUserMapByEtalonID(ctx, etalonUserID)
	if err != nil {
		return nil, err
	}
	if item != nil {
		return &item.B24UserID, nil
	}

	u, err := s.userRepo.GetByID(ctx, etalonUserID)
	if err != nil || u == nil {
		return nil, nil
	}

	candidates := collectBitrixCandidateIDs(u)
	for _, id := range candidates {
		ok, _ := s.verifyBitrixUserMatch(ctx, u, id)
		if !ok {
			continue
		}
		_ = s.repo.UpsertUserMap(ctx, &bitrix.UserMap{
			EtalonUserID: etalonUserID,
			B24UserID:    id,
		})
		return &id, nil
	}
	return nil, nil
}

func (s *bitrixSyncService) rebuildUserMapFromExternalIDs(ctx context.Context) error {
	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		return err
	}
	cacheItems, err := s.repo.ListUserCache(ctx)
	if err != nil {
		return err
	}
	for _, u := range users {
		candidates := collectBitrixCandidateIDs(&u)
		autoCandidates := findBitrixUserIDsByName(&u, cacheItems)
		for _, id := range autoCandidates {
			already := false
			for _, existing := range candidates {
				if existing == id {
					already = true
					break
				}
			}
			if !already {
				candidates = append(candidates, id)
			}
		}

		for _, id := range candidates {
			ok, verifiedName := s.verifyBitrixUserMatch(ctx, &u, id)
			if !ok {
				continue
			}
			_ = s.repo.UpsertUserMap(ctx, &bitrix.UserMap{
				EtalonUserID: u.ID,
				B24UserID:    id,
			})
			for i := range u.Integrations {
				normalizedType := strings.TrimSpace(strings.ToLower(u.Integrations[i].IntegrationType))
				if normalizedType != user.ExternalTypeBitrix24 {
					continue
				}
				if strings.TrimSpace(u.Integrations[i].ExternalID) != strconv.FormatInt(id, 10) {
					continue
				}
				u.Integrations[i].IsEnabled = true
				u.Integrations[i].IsVerified = true
				u.Integrations[i].IsLocked = true
				u.Integrations[i].VerifiedName = verifiedName
			}
			if !hasBitrixIntegration(&u, id) {
				u.Integrations = append(u.Integrations, user.Integration{
					UserID:          u.ID,
					IntegrationType: user.ExternalTypeBitrix24,
					ExternalID:      strconv.FormatInt(id, 10),
					IsEnabled:       true,
					IsVerified:      true,
					IsLocked:        true,
					VerifiedName:    verifiedName,
				})
				s.log.Info("Bitrix24: создана автоматическая интеграция пользователя", "etalon_user_id", u.ID, "b24_user_id", id, "verified_name", verifiedName)
			}
			externalType := user.ExternalTypeBitrix24
			externalID := strconv.FormatInt(id, 10)
			u.ExternalType = &externalType
			u.ExternalID = &externalID
			_ = s.userRepo.ReplaceIntegrations(ctx, u.ID, u.Integrations)
			_ = s.userRepo.Update(ctx, &u)
			break
		}
	}
	return nil
}

func hasBitrixIntegration(u *user.User, b24UserID int64) bool {
	if u == nil {
		return false
	}
	target := strconv.FormatInt(b24UserID, 10)
	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
			continue
		}
		if !integration.IsEnabled {
			continue
		}
		if strings.TrimSpace(integration.ExternalID) == target {
			return true
		}
	}
	return false
}

func findBitrixUserIDsByName(u *user.User, cacheItems []bitrix.UserCache) []int64 {
	if u == nil || len(cacheItems) == 0 {
		return nil
	}
	userFirst := normalizePersonToken(u.FirstName)
	userLast := normalizePersonToken(u.LastName)
	userFull := normalizePersonToken(u.FullName)

	matches := make([]int64, 0, 1)
	for i := range cacheItems {
		cacheFirst := normalizePersonToken(cacheItems[i].FirstName)
		cacheLast := normalizePersonToken(cacheItems[i].LastName)
		cacheFull := normalizePersonToken(strings.Join([]string{cacheItems[i].LastName, cacheItems[i].FirstName, cacheItems[i].SecondName}, " "))
		if userFirst != "" && userLast != "" && userFirst == cacheFirst && userLast == cacheLast {
			matches = append(matches, cacheItems[i].B24UserID)
			continue
		}
		if userFull != "" && userFull == cacheFull {
			matches = append(matches, cacheItems[i].B24UserID)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	return []int64{matches[0]}
}

func collectBitrixCandidateIDs(u *user.User) []int64 {
	if u == nil {
		return nil
	}
	ids := make([]int64, 0, len(u.Integrations)+1)
	seen := make(map[int64]struct{}, len(u.Integrations)+1)

	for _, integration := range u.Integrations {
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
			continue
		}
		if !integration.IsEnabled {
			continue
		}
		if !integration.IsVerified {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(integration.ExternalID), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if u.ExternalType != nil && u.ExternalID != nil &&
		strings.TrimSpace(strings.ToLower(*u.ExternalType)) == user.ExternalTypeBitrix24 {
		id, err := strconv.ParseInt(strings.TrimSpace(*u.ExternalID), 10, 64)
		if err == nil && id > 0 {
			if _, exists := seen[id]; !exists {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func (s *bitrixSyncService) verifyBitrixUserMatch(ctx context.Context, u *user.User, b24UserID int64) (bool, string) {
	cacheItems, err := s.repo.ListUserCache(ctx)
	if err != nil {
		return false, ""
	}
	var target *bitrix.UserCache
	for i := range cacheItems {
		if cacheItems[i].B24UserID == b24UserID {
			target = &cacheItems[i]
			break
		}
	}
	if target == nil {
		return false, ""
	}

	userFirst := normalizePersonToken(u.FirstName)
	userLast := normalizePersonToken(u.LastName)
	cacheFirst := normalizePersonToken(target.FirstName)
	cacheLast := normalizePersonToken(target.LastName)
	if userFirst != "" && userLast != "" && userFirst == cacheFirst && userLast == cacheLast {
		return true, strings.TrimSpace(strings.Join([]string{target.LastName, target.FirstName, target.SecondName}, " "))
	}

	userFull := normalizePersonToken(u.FullName)
	cacheFull := normalizePersonToken(strings.Join([]string{target.LastName, target.FirstName, target.SecondName}, " "))
	if userFull != "" && cacheFull != "" && userFull == cacheFull {
		return true, strings.TrimSpace(strings.Join([]string{target.LastName, target.FirstName, target.SecondName}, " "))
	}
	return false, ""
}

func mapTicketStatusToStage(status string) string {
	switch strings.TrimSpace(status) {
	case tickets.StatusNew:
		return "NEW"
	case tickets.StatusInProgress:
		return "PREPARATION"
	case tickets.StatusPending, tickets.StatusDeferred:
		return "PREPAYMENT_INVOIC"
	case tickets.StatusOnsite:
		return "EXECUTING"
	case tickets.StatusToManager:
		return "FINAL_INVOICE"
	case tickets.StatusDone, tickets.StatusResolved, tickets.StatusClosed:
		return "WON"
	case tickets.StatusSpam:
		return "APOLOGY"
	case tickets.StatusExecution:
		return "LOSE"
	default:
		return "NEW"
	}
}

func mapStageToTicketStatus(stageID string) string {
	stage := strings.TrimSpace(strings.ToUpper(stageID))
	stage = strings.TrimPrefix(stage, "C17:")
	switch stage {
	case "NEW":
		return tickets.StatusNew
	case "PREPARATION":
		return tickets.StatusInProgress
	case "PREPAYMENT_INVOIC":
		return tickets.StatusPending
	case "EXECUTING":
		return tickets.StatusOnsite
	case "FINAL_INVOICE":
		return tickets.StatusToManager
	case "WON":
		return tickets.StatusResolved
	case "APOLOGY":
		return tickets.StatusSpam
	case "LOSE":
		return tickets.StatusExecution
	default:
		return ""
	}
}

func mapTicketTypeToBitrixID(ticketType string) int {
	switch strings.TrimSpace(ticketType) {
	case tickets.TypeIncident:
		return 1599
	case tickets.TypeConsultation, tickets.TypeServiceRequest:
		return 1601
	case tickets.TypeCTO:
		return 1603
	case tickets.TypeAO:
		return 1605
	case tickets.TypePaidWorks:
		return 1607
	default:
		return 1599
	}
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var htmlBreakRe = regexp.MustCompile(`(?i)<br\s*/?>`)
var htmlParagraphRe = regexp.MustCompile(`(?i)</p>\s*<p[^>]*>`)
var htmlPOpenRe = regexp.MustCompile(`(?i)<p[^>]*>`)
var htmlPCloseRe = regexp.MustCompile(`(?i)</p>`)
var digitSequenceRe = regexp.MustCompile(`\d+`)

func toPlainText(v string) string {
	text := strings.ReplaceAll(v, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = htmlBreakRe.ReplaceAllString(text, "\n")
	text = htmlParagraphRe.ReplaceAllString(text, "\n\n")
	text = htmlPOpenRe.ReplaceAllString(text, "\n")
	text = htmlPCloseRe.ReplaceAllString(text, "\n")
	text = htmlTagRe.ReplaceAllString(text, "")
	text = html.UnescapeString(text)

	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizePersonToken(v string) string {
	out := strings.ToLower(strings.TrimSpace(v))
	out = strings.ReplaceAll(out, "ё", "е")
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func (s *bitrixSyncService) closeResolvedTickets(ctx context.Context, threshold time.Duration) (int, error) {
	items, err := s.ticketRepo.ListResolvedForAutoClose(ctx, threshold)
	if err != nil {
		return 0, err
	}
	closed := 0
	for i := range items {
		ticket := items[i]
		if strings.TrimSpace(ticket.Status) != tickets.StatusResolved {
			continue
		}
		ticket.Status = tickets.StatusClosed
		if err := s.ticketRepo.Update(ctx, &ticket); err != nil {
			s.log.Error("Bitrix24: не удалось автозакрыть заявку", "ticket_id", ticket.ID, "error", err)
			continue
		}
		if s.history != nil {
			s.history.Write(ctx, TicketHistoryWriteRequest{
				TicketID: ticket.ID,
				Action:   tickets.HistoryActionFieldChanged,
				Field:    tickets.HistoryFieldStatus,
				Source:   tickets.HistorySourceSystem,
				OldValue: tickets.StatusResolved,
				NewValue: tickets.StatusClosed,
			})
		}
		closed++
	}
	return closed, nil
}
