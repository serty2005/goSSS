package services

import (
	"context"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	bitrixDescriptionField = "UF_CRM_1766060620"
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

type BitrixSyncService interface {
	IsEnabled() bool
	SyncTicketByID(ctx context.Context, ticketID string) error
	SyncComment(ctx context.Context, ticketID string, comment *tickets.TicketComment, etalonUserID uint) error
	RefreshServicePoints(ctx context.Context) (int, error)
	ListServicePoints(ctx context.Context) ([]bitrix.ServicePoint, error)
	SearchServicePoints(ctx context.Context, term string, limit, offset int, randomWhenEmpty bool) ([]bitrix.ServicePoint, error)
	SearchBitrixUsersByName(ctx context.Context, firstName, lastName, fullName string) ([]bitrix.UserCache, error)
	RefreshUsers(ctx context.Context) (int, error)
	PreviewServicePointsImport(ctx context.Context, fileName string, content []byte) (*ServicePointImportPreview, error)
	PreviewServicePointsSync(ctx context.Context, fileName string, content []byte, mapping ServicePointImportMapping) (*ServicePointSyncPreview, error)
	ImportServicePoints(ctx context.Context, fileName string, content []byte, mapping ServicePointImportMapping, options ServicePointSyncApplyOptions) (*ServicePointSyncApplyResult, error)
}

type bitrixSyncService struct {
	cfg        *config.Config
	log        logger.LoggerInterface
	client     *b24.Client
	redis      *redis.Client
	ticketRepo tickets.TicketRepository
	userRepo   user.Repository
	repo       bitrix.Repository
}

func NewBitrixSyncService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client *b24.Client,
	redisClient *redis.Client,
	ticketRepo tickets.TicketRepository,
	userRepo user.Repository,
	repo bitrix.Repository,
) BitrixSyncService {
	return &bitrixSyncService{
		cfg:        cfg,
		log:        log,
		client:     client,
		redis:      redisClient,
		ticketRepo: ticketRepo,
		userRepo:   userRepo,
		repo:       repo,
	}
}

func (s *bitrixSyncService) IsEnabled() bool {
	return s.cfg.EnableBitrixGateway && s.client != nil && s.client.IsConfigured()
}

func (s *bitrixSyncService) SyncTicketByID(ctx context.Context, ticketID string) error {
	if !s.IsEnabled() {
		return nil
	}
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil || !ticket.SyncWithBitrix {
		return nil
	}
	if ticket.BitrixServicePointID == nil || *ticket.BitrixServicePointID <= 0 {
		return fmt.Errorf("РґР»СЏ СЃРёРЅС…СЂРѕРЅРёР·Р°С†РёРё СЃ Bitrix24 РЅРµ РІС‹Р±СЂР°РЅР° С‚РѕС‡РєР° РѕР±СЃР»СѓР¶РёРІР°РЅРёСЏ")
	}

	dealID, err := s.upsertDealAndLink(ctx, ticket)
	if err != nil {
		return err
	}
	return s.syncPendingComments(ctx, ticket, dealID)
}

func (s *bitrixSyncService) upsertDealAndLink(ctx context.Context, ticket *tickets.Ticket) (int64, error) {
	if ticket == nil {
		return 0, fmt.Errorf("С‚РёРєРµС‚ РЅРµ РЅР°Р№РґРµРЅ")
	}

	deals, err := s.client.DealListByOrigin(ctx, s.cfg.BitrixOriginatorID, ticket.ID)
	if err != nil {
		return 0, err
	}
	if len(deals) > 1 {
		return 0, fmt.Errorf("РѕР±РЅР°СЂСѓР¶РµРЅРѕ Р±РѕР»РµРµ РѕРґРЅРѕР№ СЃРґРµР»РєРё Bitrix24 РґР»СЏ С‚РёРєРµС‚Р° %s", ticket.ID)
	}

	fields, err := s.buildDealFields(ctx, ticket)
	if err != nil {
		return 0, err
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
	if ticket == nil || !ticket.SyncWithBitrix {
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
	if existing != nil {
		return nil
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

	b24ID, err := s.client.TimelineCommentAdd(ctx, link.B24DealID, message, nil)
	if err != nil {
		return err
	}
	s.setCommentSuppress(ctx, b24ID)

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
	all := make([]bitrix.ServicePoint, 0, 512)
	start := 0
	for {
		items, next, err := s.client.ListsElementGet(ctx, iblockType, iblockID, start, bitrixServicePointSelectFields)
		if err != nil {
			return 0, err
		}
		for _, item := range items {
			all = append(all, bitrix.ServicePoint{
				B24ElementID: item.ID,
				Name:         item.Name,
				RawJSON:      item.RawJSON,
				UpdatedAt:    time.Now(),
			})
		}
		if next <= 0 {
			break
		}
		start = next
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
	users := make([]bitrix.UserCache, 0, 256)
	start := 0
	for {
		page, next, err := s.client.UserGet(ctx, start)
		if err != nil {
			return 0, err
		}
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
		if next <= 0 {
			break
		}
		start = next
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
		b24ID, err := s.client.TimelineCommentAdd(ctx, dealID, message, nil)
		if err != nil {
			return err
		}
		s.setCommentSuppress(ctx, b24ID)
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
		s.log.Warn("Bitrix24: РЅРµ СѓРґР°Р»РѕСЃСЊ СѓСЃС‚Р°РЅРѕРІРёС‚СЊ suppress РєР»СЋС‡ СЃРґРµР»РєРё", "deal_id", dealID, "error", err)
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
		s.log.Warn("Bitrix24: РЅРµ СѓРґР°Р»РѕСЃСЊ СѓСЃС‚Р°РЅРѕРІРёС‚СЊ suppress РєР»СЋС‡ РєРѕРјРјРµРЅС‚Р°СЂРёСЏ", "comment_id", commentID, "error", err)
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

			authorName := "РЎРѕС‚СЂСѓРґРЅРёРє Bitrix24"
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
	fields := map[string]interface{}{
		"CATEGORY_ID":          s.cfg.BitrixCategoryID,
		"TITLE":                buildTicketTitle(ticket),
		"ORIGINATOR_ID":        s.cfg.BitrixOriginatorID,
		"ORIGIN_ID":            ticket.ID,
		"STAGE_ID":             "C17:" + stageCode,
		bitrixDescriptionField: s.buildDealDescription(ticket),
		bitrixTypeField:        mapTicketTypeToBitrixID(ticket.Type),
		bitrixPointField:       ticket.BitrixServicePointID,
	}
	if strings.TrimSpace(ticket.BitrixDealTitle) != "" {
		fields["TITLE"] = strings.TrimSpace(ticket.BitrixDealTitle)
	}

	if ticket.AssigneeID != nil {
		assignedID, err := s.resolveBitrixUserID(ctx, *ticket.AssigneeID)
		if err != nil {
			return nil, err
		}
		if assignedID != nil && *assignedID > 0 {
			fields["ASSIGNED_BY_ID"] = *assignedID
			s.log.Info("Bitrix24: РѕРїСЂРµРґРµР»РµРЅ ASSIGNED_BY_ID РїРѕ РёСЃРїРѕР»РЅРёС‚РµР»СЋ", "ticket_id", ticket.ID, "etalon_user_id", *ticket.AssigneeID, "b24_user_id", *assignedID)
		} else {
			s.log.Warn("Bitrix24: ASSIGNED_BY_ID РЅРµ РЅР°Р№РґРµРЅ РїРѕ РёСЃРїРѕР»РЅРёС‚РµР»СЋ", "ticket_id", ticket.ID, "etalon_user_id", *ticket.AssigneeID)
		}
	} else if ticket.ReporterID != nil {
		assignedID, err := s.resolveBitrixUserID(ctx, *ticket.ReporterID)
		if err != nil {
			return nil, err
		}
		if assignedID != nil && *assignedID > 0 {
			fields["ASSIGNED_BY_ID"] = *assignedID
			s.log.Info("Bitrix24: РѕРїСЂРµРґРµР»РµРЅ ASSIGNED_BY_ID РїРѕ Р°РІС‚РѕСЂСѓ", "ticket_id", ticket.ID, "etalon_user_id", *ticket.ReporterID, "b24_user_id", *assignedID)
		} else {
			s.log.Warn("Bitrix24: ASSIGNED_BY_ID РЅРµ РЅР°Р№РґРµРЅ РїРѕ Р°РІС‚РѕСЂСѓ", "ticket_id", ticket.ID, "etalon_user_id", *ticket.ReporterID)
		}
	}
	return fields, nil
}

func (s *bitrixSyncService) buildCommentBody(ctx context.Context, ticketID string, comment *tickets.TicketComment, authorID *int64, authorName string) string {
	if comment == nil {
		return ""
	}
	lines := []string{s.withBitrixAuthorMention(toPlainText(comment.Text), authorID, authorName)}

	links, err := s.ticketRepo.GetTicketFileLinksByRelation(ctx, ticketID, []string{tickets.RelationTypeInlineComment})
	if err != nil || len(links) == 0 {
		return strings.Join(lines, "\n")
	}

	for _, link := range links {
		if link.CommentUUID == nil || strings.TrimSpace(*link.CommentUUID) != strings.TrimSpace(comment.ServiceDeskUUID) {
			continue
		}
		asset, err := s.ticketRepo.GetFileAssetByID(ctx, link.FileID)
		if err != nil || asset == nil {
			continue
		}
		url := s.buildAttachmentURL(asset.StorageKey)
		if strings.HasPrefix(strings.ToLower(asset.MimeType), "image/") {
			lines = append(lines, "[IMG]"+url+"[/IMG]")
		} else {
			lines = append(lines, url)
		}
	}
	return strings.Join(lines, "\n")
}

func (s *bitrixSyncService) buildDealDescription(ticket *tickets.Ticket) string {
	if ticket == nil {
		return ""
	}
	lines := []string{
		fmt.Sprintf("Тикет Etalon #%d", ticket.Number),
	}
	if strings.TrimSpace(ticket.Subject) != "" {
		lines = append(lines, strings.TrimSpace(ticket.Subject))
	}
	lines = append(lines, s.buildTicketURL(ticket.ID))

	header := strings.Join(lines, "\n")
	body := toPlainText(ticket.Description)
	if body == "" {
		return header
	}
	return header + "\n\n" + body
}

func (s *bitrixSyncService) buildTicketURL(ticketID string) string {
	path := "/tickets/" + strings.TrimSpace(ticketID)
	base := strings.TrimRight(strings.TrimSpace(s.cfg.EtalonTicketBaseURL), "/")
	if base == "" {
		return path
	}
	return base + path
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
		name = "РџРѕР»СЊР·РѕРІР°С‚РµР»СЊ Etalon"
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

func (s *bitrixSyncService) buildAttachmentURL(storageKey string) string {
	base := strings.TrimRight(strings.TrimSpace(s.cfg.EtalonTicketBaseURL), "/")
	if base == "" {
		return "/api/static/tickets/" + strings.TrimLeft(storageKey, "/")
	}
	return base + "/api/static/tickets/" + strings.TrimLeft(storageKey, "/")
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
				u.Integrations[i].IsVerified = true
				u.Integrations[i].IsLocked = true
				u.Integrations[i].VerifiedName = verifiedName
			}
			if !hasBitrixIntegration(&u, id) {
				u.Integrations = append(u.Integrations, user.Integration{
					UserID:          u.ID,
					IntegrationType: user.ExternalTypeBitrix24,
					ExternalID:      strconv.FormatInt(id, 10),
					IsVerified:      true,
					IsLocked:        true,
					VerifiedName:    verifiedName,
				})
				s.log.Info("Bitrix24: СЃРѕР·РґР°РЅР° Р°РІС‚РѕРјР°С‚РёС‡РµСЃРєР°СЏ РёРЅС‚РµРіСЂР°С†РёСЏ РїРѕР»СЊР·РѕРІР°С‚РµР»СЏ", "etalon_user_id", u.ID, "b24_user_id", id, "verified_name", verifiedName)
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

func buildTicketTitle(ticket *tickets.Ticket) string {
	subj := strings.TrimSpace(ticket.Subject)
	if subj == "" {
		subj = "Р—Р°СЏРІРєР°"
	}
	return fmt.Sprintf("#%d %s", ticket.Number, subj)
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
		return tickets.StatusDeferred
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
	out = strings.ReplaceAll(out, "С‘", "Рµ")
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
			s.log.Error("Bitrix24: РЅРµ СѓРґР°Р»РѕСЃСЊ Р°РІС‚РѕР·Р°РєСЂС‹С‚СЊ Р·Р°СЏРІРєСѓ", "ticket_id", ticket.ID, "error", err)
			continue
		}
		_ = s.ticketRepo.AddHistory(ctx, &tickets.TicketHistory{
			TicketID:  ticket.ID,
			UserID:    nil,
			Action:    tickets.HistoryActionFieldChanged,
			Field:     tickets.HistoryFieldStatus,
			OldValue:  tickets.StatusResolved,
			NewValue:  tickets.StatusClosed,
			CreatedAt: time.Now(),
		})
		closed++
	}
	return closed, nil
}
