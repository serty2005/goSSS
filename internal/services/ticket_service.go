package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Regex для поиска UUID файлов в ссылках Naumen (./download?uuid=file$123...)
var naumenFileRegex = regexp.MustCompile(`uuid=(file\$[0-9]+)`)

type TicketService interface {
	// Чтение
	List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error)
	GetLastComments(ctx context.Context, ticketIDs []string) (map[string]tickets.LastCommentInfo, error)
	GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error)
	GetDashboardStats(ctx context.Context) (*tickets.DashboardStats, error)
	GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error)

	// Действия
	CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error)
	ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error)
	AddComment(ctx context.Context, ticketID string, comment string, isPrivate bool, userID uint) (*tickets.TicketComment, error)
	RecordConnectionCopy(ctx context.Context, ticketID string, label string, value string, userID uint) error
	UpdateDescription(ctx context.Context, ticketID string, description string, userID uint) (*tickets.Ticket, error)
	RefreshCommentsFromServiceDesk(ctx context.Context, ticketID string) (int, error)
	UploadAttachments(ctx context.Context, ticketID string, files []*multipart.FileHeader) ([]tickets.Attachment, error)
	Assign(ctx context.Context, ticketID string, assigneeID *uint, actorID uint) (*tickets.Ticket, error)
	ChangeCompany(ctx context.Context, ticketID string, companyID string, actorID uint) (*tickets.Ticket, error)
	UpdateBitrixFields(ctx context.Context, ticketID string, bitrixServicePointID *int64, bitrixDealTitle string, actorID uint) (*tickets.Ticket, error)
	AutoCloseResolvedTickets(ctx context.Context, threshold time.Duration) (int, error)
	LinkToAsset(ctx context.Context, ticketID string, assetID string, assetType string) error
}

type ticketServiceImpl struct {
	logger          logger.LoggerInterface
	ticketRepo      tickets.TicketRepository
	historyWriter   TicketHistoryWriter
	userRepo        user.Repository
	companyRepo     company.Repository
	contractRepo    contract.Repository
	sdClient        external.ExternalSystemClient
	cfg             *config.Config
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	bitrixRepo      bitrix.Repository
}

var ErrReporterNotFound = errors.New("пользователь-автор не найден")

func NewTicketService(
	logger logger.LoggerInterface,
	ticketRepo tickets.TicketRepository,
	userRepo user.Repository,
	companyRepo company.Repository,
	contractRepo contract.Repository,
	sdClient external.ExternalSystemClient,
	cfg *config.Config,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
	bitrixRepo bitrix.Repository,
) TicketService {
	return &ticketServiceImpl{
		logger:          logger,
		ticketRepo:      ticketRepo,
		historyWriter:   NewTicketHistoryWriter(ticketRepo, logger),
		userRepo:        userRepo,
		companyRepo:     companyRepo,
		contractRepo:    contractRepo,
		sdClient:        sdClient,
		cfg:             cfg,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		bitrixRepo:      bitrixRepo,
	}
}

// List возвращает список заявок с фильтрацией.
func (s *ticketServiceImpl) List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error) {
	_, _ = s.ticketRepo.ArchiveStale(ctx, 14*24*time.Hour)
	items, err := s.ticketRepo.Find(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("service: list tickets: %w", err)
	}
	s.applyCommonContractFlag(items)
	s.enrichBitrixDealLinks(ctx, items)
	count, err := s.ticketRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("service: count tickets: %w", err)
	}
	return items, count, nil
}

func (s *ticketServiceImpl) GetLastComments(ctx context.Context, ticketIDs []string) (map[string]tickets.LastCommentInfo, error) {
	comments, err := s.ticketRepo.GetLastComments(ctx, ticketIDs)
	if err != nil {
		return nil, fmt.Errorf("service: get last comments: %w", err)
	}
	return comments, nil
}

func (s *ticketServiceImpl) GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error) {
	_, _ = s.ticketRepo.ArchiveStale(ctx, 14*24*time.Hour)
	items, err := s.ticketRepo.GetCompanyFilters(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("service: get company filters: %w", err)
	}
	return items, nil
}

func (s *ticketServiceImpl) GetDashboardStats(ctx context.Context) (*tickets.DashboardStats, error) {
	stats, err := s.ticketRepo.GetDashboardStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("service: get dashboard stats: %w", err)
	}
	return stats, nil
}

// CreateInternal создает внутренний тикет.
func (s *ticketServiceImpl) CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error) {
	author, err := s.userRepo.GetByID(ctx, authorID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить автора тикета: %w", err)
	}
	if author == nil {
		return nil, ErrReporterNotFound
	}
	if dto.AssigneeID == nil || *dto.AssigneeID == 0 {
		return nil, fmt.Errorf("не выбран исполнитель")
	}
	assignee, err := s.userRepo.GetByID(ctx, *dto.AssigneeID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить исполнителя: %w", err)
	}
	if assignee == nil {
		return nil, fmt.Errorf("исполнитель не найден")
	}

	ownerCompany, err := s.companyRepo.GetByID(ctx, dto.CompanyID)
	if err != nil {
		return nil, err
	}

	syncWithBitrix := true
	if dto.SyncWithBitrix != nil {
		syncWithBitrix = *dto.SyncWithBitrix
	}
	if !isAdminUser(author) {
		syncWithBitrix = true
	}
	resolvedBitrixServicePointID := dto.BitrixServicePointID
	if syncWithBitrix && (resolvedBitrixServicePointID == nil || *resolvedBitrixServicePointID <= 0) {
		mapping, mappingErr := s.bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, dto.CompanyID)
		if mappingErr != nil {
			return nil, fmt.Errorf("не удалось получить сопоставление компании с точкой Bitrix24: %w", mappingErr)
		}
		if mapping != nil && mapping.BitrixServicePointID > 0 {
			mappedID := mapping.BitrixServicePointID
			resolvedBitrixServicePointID = &mappedID
		}
	}
	if syncWithBitrix && resolvedBitrixServicePointID == nil {
		return nil, fmt.Errorf("не выбрана точка обслуживания Bitrix24")
	}
	if syncWithBitrix && strings.TrimSpace(dto.BitrixDealTitle) == "" {
		return nil, fmt.Errorf("не заполнен заголовок сделки Bitrix24")
	}

	ticket := &tickets.Ticket{
		Subject:              dto.Subject,
		Description:          dto.Description,
		Priority:             dto.Priority,
		Type:                 dto.Type,
		Status:               tickets.StatusNew,
		CompanyID:            dto.CompanyID,
		AssigneeID:           dto.AssigneeID,
		ReporterID:           &authorID,
		AssetID:              dto.AssetID,
		AssetType:            dto.AssetType,
		SyncWithBitrix:       syncWithBitrix,
		BitrixServicePointID: resolvedBitrixServicePointID,
		BitrixDealTitle:      strings.TrimSpace(dto.BitrixDealTitle),
	}

	if ownerCompany == nil {
		return nil, fmt.Errorf("компания не найдена")
	}

	if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
		commonContract, err := s.getOrCreateCommonContract(ctx)
		if err != nil {
			return nil, err
		}
		ticket.ContractID = &commonContract.ID
	} else {
		contractID, err := s.resolveCompanyContractID(ctx, dto.CompanyID)
		if err != nil {
			return nil, err
		}
		ticket.ContractID = contractID
	}
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	// Валидация полей
	if ticket.Priority == "" {
		ticket.Priority = tickets.PriorityMedium
	}
	if ticket.Type == "" {
		ticket.Type = tickets.TypeIncident
	}

	if err := s.ticketRepo.Create(ctx, ticket); err != nil {
		return nil, err
	}
	if syncWithBitrix && ticket.BitrixServicePointID != nil && *ticket.BitrixServicePointID > 0 {
		if err := s.bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
			CompanyID:            ticket.CompanyID,
			BitrixServicePointID: *ticket.BitrixServicePointID,
		}); err != nil {
			s.logger.Error("Не удалось обновить сопоставление компании с точкой Bitrix24", "company_id", ticket.CompanyID, "bitrix_service_point_id", *ticket.BitrixServicePointID, "error", err)
		}
	}

	// Запись в историю
	s.recordHistory(ctx, ticket.ID, &authorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.HistorySourceUI, "", tickets.StatusNew, nil)

	return ticket, nil
}

func (s *ticketServiceImpl) getOrCreateCommonContract(ctx context.Context) (*contract.Contract, error) {
	commonID := strings.TrimSpace(s.cfg.CommonContractID)
	if commonID == "" {
		return nil, fmt.Errorf("common contract id is empty")
	}

	existing, err := s.contractRepo.GetByID(ctx, commonID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	state := "active"
	now := time.Now()
	commonContract := &contract.Contract{
		Base:           common.Base{ID: commonID},
		State:          &state,
		StateStartTime: &now,
	}
	if err := s.contractRepo.Create(ctx, commonContract); err != nil {
		return nil, err
	}
	return commonContract, nil
}

// ChangeStatus меняет статус тикета и пишет историю.
func (s *ticketServiceImpl) ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error) {
	_, _ = s.ticketRepo.ArchiveStale(ctx, 14*24*time.Hour)
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}
	if ticket.IsArchived && status != tickets.StatusInProgress {
		return nil, fmt.Errorf("архивный тикет можно вернуть только в статус \"В работе\"")
	}

	oldStatus := ticket.Status
	if oldStatus == status {
		return ticket, nil // Статус не изменился
	}

	comment = strings.TrimSpace(comment)
	if (status == tickets.StatusResolved || status == tickets.StatusClosed) && comment != "" {
		oldResult := ticket.Result
		ticket.Result = comment
		if oldResult != ticket.Result {
			s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldResult, tickets.HistorySourceUI, oldResult, ticket.Result, nil)
		}
	}

	ticket.Status = status
	if ticket.IsArchived && status == tickets.StatusInProgress {
		ticket.IsArchived = false
		ticket.ArchivedAt = nil
		ticket.SyncWithBitrix = true
	}
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	// Запись в историю
	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.HistorySourceUI, oldStatus, status, nil)

	// Если есть комментарий, добавляем его как историю или отдельно
	if comment != "" {
		// Для простоты используем легаси структуру Comment, если фронт её ждет,
		// но лучше писать в History с полем "comment"
		// Р В еализуем через History как "comment_added"
		s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, tickets.HistorySourceUI, "", comment, nil)
	}

	// Если заявка синхронизирована с Naumen, нужно отправить обновление туда
	if ticket.ServiceDeskUUID != "" {
		// s.sdClient.UpdateEntity(...) // TODO: Р В еализовать обратную синхронизацию статуса
	}

	return ticket, nil
}

// Assign назначает или снимает исполнителя.
func (s *ticketServiceImpl) Assign(ctx context.Context, ticketID string, assigneeID *uint, actorID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}

	var oldAssigneeName, newAssigneeName string

	// Получаем имена для истории
	if ticket.Assignee != nil {
		oldAssigneeName = ticket.Assignee.FullName
	} else {
		oldAssigneeName = "Не назначен"
	}

	if assigneeID != nil {
		newAssignee, err := s.userRepo.GetByID(ctx, *assigneeID)
		if err != nil || newAssignee == nil {
			return nil, fmt.Errorf("пользователь-исполнитель не найден")
		}
		newAssigneeName = newAssignee.FullName
	} else {
		newAssigneeName = "Не назначен"
	}

	ticket.AssigneeID = assigneeID
	// Если назначаем, переводим в InProgress, если он был New
	if assigneeID != nil && ticket.Status == tickets.StatusNew {
		ticket.Status = tickets.StatusInProgress
	}

	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAssignee, tickets.HistorySourceUI, oldAssigneeName, newAssigneeName, nil)
	return ticket, nil
}

// ChangeCompany меняет компанию в тикете и пересчитывает договор.
func (s *ticketServiceImpl) ChangeCompany(ctx context.Context, ticketID string, companyID string, actorID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}

	targetCompany, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if targetCompany == nil {
		return nil, fmt.Errorf("компания не найдена")
	}

	if ticket.CompanyID == companyID {
		ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
		return ticket, nil
	}

	oldCompanyName := ticket.CompanyName
	if strings.TrimSpace(oldCompanyName) == "" {
		oldCompanyName = ticket.CompanyID
	}

	newCompanyName := companyID
	if targetCompany.Title != nil && strings.TrimSpace(*targetCompany.Title) != "" {
		newCompanyName = strings.TrimSpace(*targetCompany.Title)
	} else if targetCompany.AdditionalName != nil && strings.TrimSpace(*targetCompany.AdditionalName) != "" {
		newCompanyName = strings.TrimSpace(*targetCompany.AdditionalName)
	}

	ticket.CompanyID = companyID
	if targetCompany.ActiveContract != nil && *targetCompany.ActiveContract {
		contractID, err := s.resolveCompanyContractID(ctx, companyID)
		if err != nil {
			return nil, err
		}
		ticket.ContractID = contractID
	} else {
		commonContract, err := s.getOrCreateCommonContract(ctx)
		if err != nil {
			return nil, err
		}
		ticket.ContractID = &commonContract.ID
	}

	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldCompany, tickets.HistorySourceUI, oldCompanyName, newCompanyName, nil)
	ticket.CompanyName = newCompanyName
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	return ticket, nil
}

func (s *ticketServiceImpl) UpdateBitrixFields(ctx context.Context, ticketID string, bitrixServicePointID *int64, bitrixDealTitle string, actorID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}

	nextTitle := strings.TrimSpace(bitrixDealTitle)
	if bitrixServicePointID == nil || *bitrixServicePointID <= 0 {
		return nil, fmt.Errorf("Не выбрана точка обслуживания Bitrix24")
	}
	if nextTitle == "" {
		return nil, fmt.Errorf("Не заполнен заголовок сделки Bitrix24")
	}

	oldPoint := ""
	if ticket.BitrixServicePointID != nil {
		oldPoint = fmt.Sprintf("%d", *ticket.BitrixServicePointID)
	}
	nextPoint := ""
	if bitrixServicePointID != nil {
		nextPoint = fmt.Sprintf("%d", *bitrixServicePointID)
	}
	oldTitle := strings.TrimSpace(ticket.BitrixDealTitle)
	oldSync := ticket.SyncWithBitrix

	ticket.BitrixServicePointID = bitrixServicePointID
	ticket.BitrixDealTitle = nextTitle
	ticket.SyncWithBitrix = true
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	if oldPoint != nextPoint {
		s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, "bitrix_service_point_id", tickets.HistorySourceUI, oldPoint, nextPoint, nil)
	}
	if oldTitle != nextTitle {
		s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, "bitrix_deal_title", tickets.HistorySourceUI, oldTitle, nextTitle, nil)
	}
	if !oldSync {
		s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, "sync_with_bitrix", tickets.HistorySourceUI, "false", "true", nil)
	}
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	return ticket, nil
}

func (s *ticketServiceImpl) AddComment(ctx context.Context, ticketID string, comment string, isPrivate bool, userID uint) (*tickets.TicketComment, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}

	text := strings.TrimSpace(comment)
	if text == "" {
		return nil, fmt.Errorf("комментарий пустой")
	}

	authorName := "Сотрудник"
	u, err := s.userRepo.GetByID(ctx, userID)
	if err == nil && u != nil && strings.TrimSpace(u.FullName) != "" {
		authorName = strings.TrimSpace(u.FullName)
	}

	newComment := &tickets.TicketComment{
		ID:           uuid.New().String(),
		TicketID:     ticket.ID,
		Text:         text,
		AuthorName:   authorName,
		CreationDate: time.Now(),
		IsInternal:   false,
		IsPrivate:    isPrivate,
	}
	if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{*newComment}); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, tickets.HistorySourceUI, "", text, nil)
	return newComment, nil
}

func (s *ticketServiceImpl) AutoCloseResolvedTickets(ctx context.Context, threshold time.Duration) (int, error) {
	if threshold <= 0 {
		threshold = 14 * 24 * time.Hour
	}
	candidates, err := s.ticketRepo.ListResolvedForAutoClose(ctx, threshold)
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}

	closed := 0
	for i := range candidates {
		ticket := candidates[i]
		if strings.TrimSpace(ticket.Status) != tickets.StatusResolved {
			continue
		}
		ticket.Status = tickets.StatusClosed
		if err := s.ticketRepo.Update(ctx, &ticket); err != nil {
			s.logger.Error("Не удалось автоматически закрыть заявку", "ticket_id", ticket.ID, "error", err)
			continue
		}
		s.recordHistory(ctx, ticket.ID, nil, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.HistorySourceSystem, tickets.StatusResolved, tickets.StatusClosed, nil)
		closed++
	}
	return closed, nil
}

func (s *ticketServiceImpl) RecordConnectionCopy(ctx context.Context, ticketID string, label string, value string, userID uint) error {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return fmt.Errorf("заявка не найдена")
	}

	line := strings.TrimSpace(label)
	if line == "" {
		line = "Подключение"
	}
	val := strings.TrimSpace(value)
	if val == "" {
		return fmt.Errorf("значение подключения пустое")
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionConnectionCopied, tickets.HistoryFieldConnection, tickets.HistorySourceUI, "", line+": "+val, nil)
	return nil
}

// GetDetails возвращает детали тикета, историю и вложения.
func (s *ticketServiceImpl) GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, nil
	}
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	s.enrichBitrixDealLink(ctx, ticket)

	// Загрузка истории
	history, _ := s.ticketRepo.GetHistory(ctx, ticketID)

	// Загрузка вложений
	attachments, _ := s.ticketRepo.GetAttachments(ctx, ticketID)

	details := &tickets.TicketDetails{
		Metadata: *ticket,
		// CompanyName: ticket.CompanyName, // Если это поле есть в структуре (gorm ->)
		History:     history,
		Attachments: attachments,
		Comments:    make([]tickets.Comment, 0),
	}

	// Комментарии из локальной БД (офлайн/сидер)
	localComments, _ := s.ticketRepo.GetComments(ctx, ticketID)
	if len(localComments) > 0 {
		for _, c := range localComments {
			details.Comments = append(details.Comments, tickets.Comment{
				UUID:         c.ServiceDeskUUID,
				Text:         c.Text,
				AuthorName:   c.AuthorName,
				CreationDate: c.CreationDate,
				IsInternal:   c.IsInternal,
				IsPrivate:    c.IsPrivate,
			})
		}
	}

	// Попытка получить описание из SD для легаси тикетов
	if s.isServiceDeskEnabledForReads() && ticket.ServiceDeskUUID != "" && len(localComments) == 0 {
		sdData, err := s.sdClient.FetchEntityDetails(ctx, ticket.ServiceDeskUUID, "Ticket")
		if err == nil {
			if desc, ok := sdData["descriptionRTF"].(string); ok {
				// В идеале description должен быть в БД, но для легаси берем из SD
				if ticket.Description == "" {
					details.Metadata.Description = s.processHtmlContent(ticket.ServiceDeskUUID, desc)
				}
			}
		}
	}

	return details, nil
}

func (s *ticketServiceImpl) enrichBitrixDealLinks(ctx context.Context, items []tickets.Ticket) {
	for i := range items {
		s.enrichBitrixDealLink(ctx, &items[i])
	}
}

func (s *ticketServiceImpl) enrichBitrixDealLink(ctx context.Context, ticket *tickets.Ticket) {
	if ticket == nil || !ticket.SyncWithBitrix || s.bitrixRepo == nil {
		return
	}
	link, err := s.bitrixRepo.GetDealLinkByTicketID(ctx, ticket.ID)
	if err != nil || link == nil || link.B24DealID <= 0 {
		return
	}
	dealID := link.B24DealID
	ticket.BitrixDealID = &dealID
	ticket.BitrixDealURL = s.buildBitrixDealURL(dealID)
}

func (s *ticketServiceImpl) buildBitrixDealURL(dealID int64) string {
	if dealID <= 0 {
		return ""
	}
	base := s.bitrixPortalBaseURL()
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/crm/deal/details/%d/", base, dealID)
}

func (s *ticketServiceImpl) bitrixPortalBaseURL() string {
	if s.cfg == nil {
		return ""
	}
	base := strings.TrimSpace(s.cfg.BitrixBaseURL)
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
	}
	if index := strings.Index(base, "/rest/"); index >= 0 {
		base = base[:index]
	}
	return strings.TrimRight(base, "/")
}

// UpdateDescription обновляет описание тикета.
func (s *ticketServiceImpl) UpdateDescription(ctx context.Context, ticketID string, description string, userID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}

	oldValue := ticket.Description
	ticket.Description = description
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldDescription, tickets.HistorySourceUI, oldValue, description, nil)
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	return ticket, nil
}

func (s *ticketServiceImpl) RefreshCommentsFromServiceDesk(ctx context.Context, ticketID string) (int, error) {
	if !s.isServiceDeskEnabledForReads() {
		return 0, nil
	}

	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return 0, err
	}
	if ticket == nil {
		return 0, fmt.Errorf("заявка не найдена")
	}
	if strings.TrimSpace(ticket.ServiceDeskUUID) == "" {
		return 0, nil
	}

	rawComments, err := s.sdClient.FetchComments(ctx, ticket.ServiceDeskUUID)
	if err != nil {
		return 0, err
	}
	if len(rawComments) == 0 {
		return 0, nil
	}

	existingComments, err := s.ticketRepo.GetComments(ctx, ticketID)
	if err != nil {
		return 0, err
	}
	existingByUUID := make(map[string]struct{}, len(existingComments))
	for _, item := range existingComments {
		uuid := strings.TrimSpace(item.ServiceDeskUUID)
		if uuid == "" {
			continue
		}
		existingByUUID[uuid] = struct{}{}
	}

	toInsert := make([]tickets.TicketComment, 0, len(rawComments))
	for _, rawComment := range rawComments {
		comment, mapErr := s.sdClient.Mapper().DataToComment(rawComment)
		if mapErr != nil || comment == nil {
			continue
		}
		commentUUID := strings.TrimSpace(comment.UUID)
		if commentUUID == "" {
			continue
		}
		if _, exists := existingByUUID[commentUUID]; exists {
			continue
		}

		text := s.processHtmlContent(ticket.ServiceDeskUUID, comment.Text)
		createdAt := comment.CreationDate
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		toInsert = append(toInsert, tickets.TicketComment{
			TicketID:        ticket.ID,
			ServiceDeskUUID: commentUUID,
			Text:            text,
			AuthorName:      comment.AuthorName,
			CreationDate:    createdAt,
			IsInternal:      comment.IsInternal,
		})
		existingByUUID[commentUUID] = struct{}{}
	}

	if len(toInsert) == 0 {
		return 0, nil
	}
	if err := s.ticketRepo.AddComments(ctx, toInsert); err != nil {
		return 0, err
	}

	return len(toInsert), nil
}

func (s *ticketServiceImpl) UploadAttachments(ctx context.Context, ticketID string, files []*multipart.FileHeader) ([]tickets.Attachment, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}
	if len(files) == 0 {
		return []tickets.Attachment{}, nil
	}

	result := make([]tickets.Attachment, 0, len(files))
	for _, fileHeader := range files {
		if fileHeader == nil || strings.TrimSpace(fileHeader.Filename) == "" {
			continue
		}

		src, openErr := fileHeader.Open()
		if openErr != nil {
			return nil, openErr
		}

		content, readErr := io.ReadAll(src)
		closeErr := src.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		fileID := uuid.New().String()
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		storageKey := filepath.ToSlash(filepath.Join(ticketID, fileID+ext))
		absPath := filepath.Join(s.cfg.TicketStoragePath, filepath.FromSlash(storageKey))
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(absPath, content, 0644); err != nil {
			return nil, err
		}

		mimeType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
		if mimeType == "" {
			mimeType = http.DetectContentType(content)
		}

		hash := sha256.Sum256(content)
		asset, err := s.ticketRepo.UpsertFileAsset(ctx, &tickets.FileAsset{
			ID:           fileID,
			StorageKey:   storageKey,
			OriginalName: fileHeader.Filename,
			MimeType:     mimeType,
			Size:         int64(len(content)),
			Checksum:     hex.EncodeToString(hash[:]),
		})
		if err != nil {
			return nil, err
		}

		if err := s.ticketRepo.UpsertTicketFileLink(ctx, &tickets.TicketFileLink{
			TicketID:     ticketID,
			FileID:       asset.ID,
			RelationType: tickets.RelationTypeDirectTicketAttachment,
		}); err != nil {
			return nil, err
		}

		result = append(result, tickets.Attachment{
			ID:       asset.ID,
			EntityID: ticketID,
			FileName: asset.OriginalName,
			FilePath: "/api/static/tickets/" + storageKey,
			MimeType: asset.MimeType,
			Size:     asset.Size,
		})
	}

	return result, nil
}

func (s *ticketServiceImpl) isServiceDeskEnabledForReads() bool {
	return s.cfg != nil && s.cfg.EnableSDeskGateway && strings.TrimSpace(s.cfg.ServiceDeskKey) != ""
}

func (s *ticketServiceImpl) applyCommonContractFlag(items []tickets.Ticket) {
	for i := range items {
		items[i].IsCommonContract = s.isCommonContractID(items[i].ContractID)
	}
}

func (s *ticketServiceImpl) isCommonContractID(contractID *string) bool {
	if contractID == nil || *contractID == "" {
		return false
	}
	return strings.TrimSpace(*contractID) == strings.TrimSpace(s.cfg.CommonContractID)
}

func (s *ticketServiceImpl) LinkToAsset(ctx context.Context, ticketID string, assetID string, assetType string) error {
	// 1. Получаем заявку, чтобы узнать, какой компании она принадлежит
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("failed to get ticket: %w", err)
	}
	if ticket == nil {
		return fmt.Errorf("заявка не найдена")
	}

	// 2. Проверяем существование актива и совпадение владельца
	var assetOwnerID string

	switch assetType {
	case tickets.AssetTypeServer:
		asset, err := s.serverRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("сервер не найден")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	case tickets.AssetTypeFiscalRegister:
		asset, err := s.frRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("фискальный регистратор не найден")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	case tickets.AssetTypeWorkstation:
		asset, err := s.workstationRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("рабочая станция не найдена")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	default:
		return fmt.Errorf("неподдерживаемый тип оборудования: %s", assetType)
	}

	// 3. Сравниваем владельцев
	// Если у оборудования нет владельца (пустая строка), считаем это риском, но разрешаем (или запрещаем, зависит от бизнес-логики).
	// В данном случае запретим привязку к "чужому" оборудованию.
	if assetOwnerID != "" && assetOwnerID != ticket.CompanyID {
		return fmt.Errorf("conflict: asset belongs to company %s, but ticket belongs to %s", assetOwnerID, ticket.CompanyID)
	}

	oldAsset := ""
	if ticket.AssetType != nil && ticket.AssetID != nil && *ticket.AssetType != "" && *ticket.AssetID != "" {
		oldAsset = *ticket.AssetType + ":" + *ticket.AssetID
	}
	newAsset := assetType + ":" + assetID

	if err := s.ticketRepo.AssociateAsset(ctx, ticketID, assetID, assetType); err != nil {
		return err
	}

	s.recordHistory(ctx, ticketID, nil, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAsset, tickets.HistorySourceUI, oldAsset, newAsset, nil)
	return nil
}

// recordHistory - вспомогательный метод для записи аудита.
func (s *ticketServiceImpl) recordHistory(
	ctx context.Context,
	ticketID string,
	userID *uint,
	action, field, source, oldVal, newVal string,
	meta map[string]interface{},
) {
	if s.historyWriter == nil {
		return
	}
	s.historyWriter.Write(ctx, TicketHistoryWriteRequest{
		TicketID: ticketID,
		UserID:   userID,
		Action:   action,
		Field:    field,
		Source:   source,
		OldValue: oldVal,
		NewValue: newVal,
		Meta:     meta,
	})
}

// processHtmlContent ищет ссылки на файлы Naumen, скачивает их и заменяет на локальные URL.
// sdUUID - внешний UUID заявки (например, serviceCall$123), используется для группировки файлов в папке.
func (s *ticketServiceImpl) processHtmlContent(sdUUID string, htmlContent string) string {
	// Р ВРЎвЂ°Р ВµР С все вхождения uuid=file$XXXXX
	matches := naumenFileRegex.FindAllStringSubmatch(htmlContent, -1)
	if len(matches) == 0 {
		return htmlContent
	}

	processedHtml := htmlContent
	// Создаем директорию для заявки: ./storage/tickets/serviceCall$123/
	ticketDir := filepath.Join(s.cfg.TicketStoragePath, sdUUID)
	if err := os.MkdirAll(ticketDir, 0755); err != nil {
		s.logger.Error("Failed to create storage dir for ticket", "dir", ticketDir, "error", err)
		return htmlContent // Возвращаем как есть, если не можем сохранить
	}

	for _, match := range matches {
		// match[0] = "uuid=file$12345"
		// match[1] = "file$12345" (ID файла)
		fileUUID := match[1]

		// 1. Проверяем, скачан ли файл
		localFilePath := filepath.Join(ticketDir, fileUUID) // Сохраняем без расширения или пытаемся угадать
		// Простой вариант: имя файла = UUID. Браузеры часто умеют определять тип по контенту,
		// но лучше сохранять расширение. Пока сохраняем как есть.

		if _, err := os.Stat(localFilePath); os.IsNotExist(err) {
			// 2. Файла нет - скачиваем
			err := s.downloadFileFromNaumen(fileUUID, localFilePath)
			if err != nil {
				s.logger.Error("Failed to download file from Naumen", "fileUUID", fileUUID, "error", err)
				continue // Пропускаем замену, если не удалось скачать
			}
		}

		// 3. Заменяем ссылку в HTML
		// Р ВРЎРѓРЎвЂ¦Р С•Р Т‘Р Р…Р °СЏ: ... src="./download?uuid=file$13205558" ...
		// Целевая:  ... src="/api/static/tickets/serviceCall$123/file$13205558" ...

		// Находим полный кусок "./download?uuid=file$XXXX" и заменяем его
		// Р В егулярка ищет только uuid=..., поэтому заменим грубо, но надежно для Naumen:
		// "./download?uuid=" + fileUUID -> "/api/static/tickets/" + sdUUID + "/" + fileUUID

		oldLink := fmt.Sprintf("./download?uuid=%s", fileUUID)
		newLink := fmt.Sprintf("/api/static/tickets/%s/%s", sdUUID, fileUUID)
		processedHtml = strings.ReplaceAll(processedHtml, oldLink, newLink)

		// На случай, если ссылка без точки в начале (бывает по-разному)
		oldLink2 := fmt.Sprintf("/download?uuid=%s", fileUUID)
		processedHtml = strings.ReplaceAll(processedHtml, oldLink2, newLink)
	}

	return processedHtml
}

// downloadFileFromNaumen выполняет запрос к API Naumen и сохраняет файл.
func (s *ticketServiceImpl) downloadFileFromNaumen(fileUUID, destPath string) error {
	// URL: <baseURL>/services/rest/get-file/file$123?accessKey=<accessKey>
	// Базовый URL в конфиге может быть с /sd или без, нужно аккуратно собрать.
	// Обычно cfg.ServiceDeskBaseURL = "https://myhoreca.itsm365.com/sd"

	// Убираем trailing slash
	baseURL := strings.TrimRight(s.cfg.ServiceDeskBaseURL, "/")
	// Формируем URL для скачивания
	url := fmt.Sprintf("%s/services/rest/get-file/%s?accessKey=%s", baseURL, fileUUID, s.cfg.ServiceDeskKey)

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func filterDirectTicketAttachments(
	attachments []tickets.Attachment,
	descriptionHTML string,
	comments []tickets.Comment,
) []tickets.Attachment {
	if len(attachments) == 0 {
		return attachments
	}

	texts := make([]string, 0, len(comments)+1)
	if descriptionHTML != "" {
		texts = append(texts, descriptionHTML)
	}
	for _, c := range comments {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}

	result := make([]tickets.Attachment, 0, len(attachments))
	for _, a := range attachments {
		if a.FilePath == "" {
			result = append(result, a)
			continue
		}
		if isPathReferencedInTexts(a.FilePath, texts) {
			continue
		}
		result = append(result, a)
	}
	return result
}

func isPathReferencedInTexts(path string, texts []string) bool {
	if path == "" || len(texts) == 0 {
		return false
	}

	candidates := []string{path}
	if strings.HasPrefix(path, "/api/static/") {
		candidates = append(candidates, strings.TrimPrefix(path, "/api"))
	} else if strings.HasPrefix(path, "/static/") {
		candidates = append(candidates, "/api"+path)
	}

	for _, t := range texts {
		if t == "" {
			continue
		}
		for _, c := range candidates {
			if c != "" && strings.Contains(t, c) {
				return true
			}
		}
	}
	return false
}

func isAdminUser(u *user.User) bool {
	if u == nil {
		return false
	}
	for _, role := range u.Roles {
		if strings.TrimSpace(role.Name) == user.RoleAdmin {
			return true
		}
	}
	return false
}

func (s *ticketServiceImpl) resolveCompanyContractID(ctx context.Context, companyID string) (*string, error) {
	ids, err := s.contractRepo.GetActiveContractIDsForCompany(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	contractID := strings.TrimSpace(ids[0])
	if contractID == "" {
		return nil, nil
	}
	return &contractID, nil
}

