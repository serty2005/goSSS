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
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/pyrus"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	contractsvc "etalon-server/internal/services/contract"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Regex для РїРѕРёСЃРєР° UUID файлов РІ ссылках Naumen (./download?uuid=file$123...)
var naumenFileRegex = regexp.MustCompile(`uuid=(file\$[0-9]+)`)

var ticketTextPhoneRegex = regexp.MustCompile(`(^|[^\d+])(\+?79\d{9}|89\d{9}|9\d{9})([^\d]|$)`)

type TicketService interface {
	// Чтение
	List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error)
	GetLastComments(ctx context.Context, ticketIDs []string) (map[string]tickets.LastCommentInfo, error)
	GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error)
	GetDashboardStats(ctx context.Context) (*tickets.DashboardStats, error)
	GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error)
	GetConnectionCopyStats(ctx context.Context, ticketID string) ([]tickets.ConnectionCopyStat, error)

	// Действия
	CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error)
	CreateFromPyrus(ctx context.Context, input TicketCreateFromPyrusInput) (*tickets.Ticket, error)
	ChangeStatus(ctx context.Context, ticketID string, status string, comment string, deferredUntilRaw string, userID uint) (*tickets.Ticket, error)
	AddComment(ctx context.Context, ticketID string, comment string, isPrivate bool, replyToClient bool, userID uint) (*tickets.TicketComment, error)
	UpdateComment(ctx context.Context, ticketID string, commentUUID string, comment string, replyToClient bool, userID uint, roles []string) (*tickets.TicketComment, error)
	DeleteComment(ctx context.Context, ticketID string, commentUUID string, userID uint, roles []string) error
	RecordConnectionCopy(
		ctx context.Context,
		ticketID string,
		label string,
		value string,
		entityType string,
		entityID string,
		connectionField string,
		userID uint,
	) error
	UpdateDescription(ctx context.Context, ticketID string, description string, userID uint) (*tickets.Ticket, error)
	RefreshCommentsFromServiceDesk(ctx context.Context, ticketID string) (int, error)
	UploadAttachments(ctx context.Context, ticketID string, files []*multipart.FileHeader) ([]tickets.Attachment, error)
	Assign(ctx context.Context, ticketID string, assigneeID *uint, actorID uint) (*tickets.Ticket, error)
	ChangeCompany(ctx context.Context, ticketID string, companyID string, actorID uint) (*tickets.Ticket, error)
	UpdateBitrixFields(ctx context.Context, ticketID string, bitrixServicePointID *int64, bitrixDealTitle string, actorID uint) (*tickets.Ticket, error)
	UnlinkFromBitrix(ctx context.Context, ticketID string, actorID uint, roles []string) (*tickets.Ticket, error)
	Delete(ctx context.Context, ticketID string, actorID uint, roles []string) error
	AutoCloseResolvedTickets(ctx context.Context, threshold time.Duration) (int, error)
	ProcessExpiredDeferred(ctx context.Context, now time.Time, limit int) ([]DeferredStatusActivation, error)
	LinkToAsset(ctx context.Context, ticketID string, assetID string, assetType string) error
}

type DeferredStatusActivation struct {
	TicketID        string
	RecipientUserID uint
}

type TicketCreateFromPyrusInput struct {
	TaskID        int64
	CompanyID     string
	Subject       string
	Description   string
	ReporterName  string
	ReporterEmail string
	Status        string
	Type          string
}

type ticketServiceImpl struct {
	logger           logger.LoggerInterface
	ticketRepo       tickets.TicketRepository
	historyWriter    TicketHistoryWriter
	userRepo         user.Repository
	companyRepo      company.Repository
	contractRepo     contract.Repository
	sdClient         external.ExternalSystemClient
	cfg              *config.Config
	serverRepo       server.Repository
	workstationRepo  workstation.Repository
	frRepo           fiscal.Repository
	bitrixRepo       bitrix.Repository
	pyrusRepo        pyrus.Repository
	telephonyRepo    telephony.Repository
	ownerHistoryRepo domainrepos.OwnerHistoryRepo
	contractService  contract.Service
}

var ErrReporterNotFound = errors.New("пользователь-автор не найден")
var ErrTicketNotFound = errors.New("заявка не найдена")
var ErrTicketForbidden = errors.New("недостаточно прав для операции с тикетом")
var ErrCommentNotFound = errors.New("комментарий не найден")
var ErrCommentForbidden = errors.New("недостаточно прав для операции с комментарием")

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
	pyrusRepo pyrus.Repository,
	telephonyRepo telephony.Repository,
	ownerHistoryRepo domainrepos.OwnerHistoryRepo,
	contractService contract.Service,
) TicketService {
	return &ticketServiceImpl{
		logger:           logger,
		ticketRepo:       ticketRepo,
		historyWriter:    NewTicketHistoryWriter(ticketRepo, logger),
		userRepo:         userRepo,
		companyRepo:      companyRepo,
		contractRepo:     contractRepo,
		sdClient:         sdClient,
		cfg:              cfg,
		serverRepo:       serverRepo,
		workstationRepo:  workstationRepo,
		frRepo:           frRepo,
		bitrixRepo:       bitrixRepo,
		pyrusRepo:        pyrusRepo,
		telephonyRepo:    telephonyRepo,
		ownerHistoryRepo: ownerHistoryRepo,
		contractService:  contractService,
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
	s.enrichPyrusTaskLinks(ctx, items)
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
	mappedBitrixServicePointID, err := s.resolveMappedBitrixServicePointID(ctx, dto.CompanyID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить сопоставление компании с точкой Bitrix24: %w", err)
	}
	contractSyncPointID := dto.BitrixServicePointID
	if !isValidBitrixServicePointID(contractSyncPointID) {
		contractSyncPointID = mappedBitrixServicePointID
	}
	if syncedCompany, err := s.syncCompanyContractFromBitrixPoint(ctx, dto.CompanyID, contractSyncPointID); err != nil {
		return nil, fmt.Errorf("не удалось синхронизировать контракт компании по точке Bitrix24: %w", err)
	} else if syncedCompany != nil {
		ownerCompany = syncedCompany
	}

	syncWithBitrix := true
	if dto.SyncWithBitrix != nil {
		syncWithBitrix = *dto.SyncWithBitrix
	}
	if dto.SyncWithBitrix == nil && !isAdminUser(author) {
		syncWithBitrix = true
	}
	if s.cfg == nil || !s.cfg.EnableBitrixGateway {
		syncWithBitrix = false
	}
	resolvedBitrixServicePointID := dto.BitrixServicePointID
	if syncWithBitrix && !isValidBitrixServicePointID(resolvedBitrixServicePointID) {
		resolvedBitrixServicePointID = mappedBitrixServicePointID
	}
	if syncWithBitrix && !isValidBitrixServicePointID(resolvedBitrixServicePointID) {
		return nil, fmt.Errorf("не выбрана точка обслуживания Bitrix24")
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

	if err := s.applyContractForTicket(ctx, ticket, ownerCompany); err != nil {
		return nil, err
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
	s.persistBitrixCompanyServicePointMapping(ctx, ticket.CompanyID, ticket.BitrixServicePointID)
	if err := s.bindTicketTelephonyByText(ctx, ticket, ticket.Description, authorID); err != nil {
		s.logger.Warn("не удалось привязать телефонию по описанию тикета", "ticket_id", ticket.ID, "error", err)
	}

	// Запись РІ историю
	s.recordHistory(ctx, ticket.ID, &authorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.HistorySourceUI, "", tickets.StatusNew, nil)

	return ticket, nil
}

func (s *ticketServiceImpl) CreateFromPyrus(ctx context.Context, input TicketCreateFromPyrusInput) (*tickets.Ticket, error) {
	ownerCompany, err := s.companyRepo.GetByID(ctx, input.CompanyID)
	if err != nil {
		return nil, err
	}
	if ownerCompany == nil {
		return nil, fmt.Errorf("компания не найдена")
	}

	subject := strings.TrimSpace(input.Subject)
	if subject == "" {
		subject = fmt.Sprintf("Обращение из Pyrus #%d", input.TaskID)
	}
	reporterName := strings.TrimSpace(input.ReporterName)
	if reporterName == "" {
		reporterName = "Pyrus"
	}

	ticket := &tickets.Ticket{
		Subject:         subject,
		Description:     strings.TrimSpace(input.Description),
		Status:          normalizePyrusTicketStatus(input.Status),
		Priority:        tickets.PriorityMedium,
		Type:            normalizePyrusTicketType(input.Type),
		CompanyID:       strings.TrimSpace(input.CompanyID),
		ReporterName:    reporterName,
		ReporterEmail:   strings.TrimSpace(input.ReporterEmail),
		ServiceDeskUUID: fmt.Sprintf("pyrus:task:%d", input.TaskID),
		SyncWithBitrix:  false,
	}
	ticket.LastUpdatedBy = "pyrus_webhook"

	if err := s.applyContractForTicket(ctx, ticket, ownerCompany); err != nil {
		return nil, err
	}
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)

	if err := s.ticketRepo.Create(ctx, ticket); err != nil {
		return nil, err
	}
	s.recordHistory(ctx, ticket.ID, nil, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.HistorySourcePyrus, "", ticket.Status, nil)
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

func (s *ticketServiceImpl) applyContractForTicket(ctx context.Context, ticket *tickets.Ticket, ownerCompany *company.Company) error {
	if ticket == nil {
		return nil
	}
	if ownerCompany == nil {
		return fmt.Errorf("компания не найдена")
	}
	if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
		commonContract, err := s.getOrCreateCommonContract(ctx)
		if err != nil {
			return err
		}
		ticket.ContractID = &commonContract.ID
		return nil
	}
	contractID, err := s.resolveCompanyContractID(ctx, ticket.CompanyID)
	if err != nil {
		return err
	}
	ticket.ContractID = contractID
	return nil
}

// ChangeStatus меняет статус тикета и пишет историю.
func (s *ticketServiceImpl) ChangeStatus(ctx context.Context, ticketID string, status string, comment string, deferredUntilRaw string, userID uint) (*tickets.Ticket, error) {
	_, _ = s.ticketRepo.ArchiveStale(ctx, 14*24*time.Hour)
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}
	if isTicketLockedByManagerFlow(ticket) {
		return nil, fmt.Errorf("тикет передан менеджеру: доступно только добавление комментариев")
	}
	if ticket.IsArchived && status != tickets.StatusInProgress {
		return nil, fmt.Errorf("архивный тикет можно вернуть только в статус \"В работе\"")
	}

	oldStatus := ticket.Status
	isReturnFromArchive := ticket.IsArchived && status == tickets.StatusInProgress
	if oldStatus == status && !isReturnFromArchive {
		return ticket, nil // Статус не изменился
	}

	comment = strings.TrimSpace(comment)
	statusRequiresFinalReport := status == tickets.StatusResolved || status == tickets.StatusClosed
	if statusRequiresFinalReport {
		existingComments, commentsErr := s.ticketRepo.GetComments(ctx, ticket.ID)
		if commentsErr != nil {
			return nil, commentsErr
		}
		if len(existingComments) == 0 && comment == "" {
			return nil, fmt.Errorf("для завершения заявки без комментариев необходимо добавить отчёт")
		}
	}

	nextDeferredUntil := ticket.DeferredUntil
	nextDeferredByID := ticket.DeferredByID
	if status == tickets.StatusDeferred {
		deferredUntil, parseErr := parseDeferredUntil(deferredUntilRaw)
		if parseErr != nil {
			return nil, parseErr
		}
		nextDeferredUntil = &deferredUntil
		nextDeferredByID = &userID
	} else {
		nextDeferredUntil = nil
		nextDeferredByID = nil
	}

	ticket.Status = status
	ticket.DeferredUntil = nextDeferredUntil
	ticket.DeferredByID = nextDeferredByID
	if isReturnFromArchive {
		ticket.IsArchived = false
		ticket.ArchivedAt = nil
	}
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	if oldStatus != status {
		// Запись в историю
		s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.HistorySourceUI, oldStatus, status, nil)
	}

	// Отчёт о смене статуса сохраняем как обычный комментарий в тикете.
	if comment != "" {
		authorName := "Сотрудник"
		u, uErr := s.userRepo.GetByID(ctx, userID)
		if uErr == nil && u != nil && strings.TrimSpace(u.FullName) != "" {
			authorName = strings.TrimSpace(u.FullName)
		}
		commentID := uuid.New().String()
		newComment := tickets.TicketComment{
			ID:              commentID,
			TicketID:        ticket.ID,
			ServiceDeskUUID: commentID,
			Text:            comment,
			AuthorName:      authorName,
			CreationDate:    time.Now(),
			IsInternal:      false,
			IsPrivate:       false,
		}
		if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{newComment}); err != nil {
			return nil, err
		}
		s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, tickets.HistorySourceUI, "", comment, nil)
	}

	// Если заявка синхронизирована с Naumen, нужно отправить обновление туда
	if ticket.ServiceDeskUUID != "" {
		// s.sdClient.UpdateEntity(...) // TODO: Реализовать обратную синхронизацию статуса
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
	if isTicketLockedByManagerFlow(ticket) {
		return nil, fmt.Errorf("тикет передан менеджеру: доступно только добавление комментариев")
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
	// Если назначаем, переводим РІ InProgress, если РѕРЅ был New
	if assigneeID != nil && ticket.Status == tickets.StatusNew {
		ticket.Status = tickets.StatusInProgress
	}

	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAssignee, tickets.HistorySourceUI, oldAssigneeName, newAssigneeName, nil)
	return ticket, nil
}

// ChangeCompany меняет компанию в тикете и пересчитывает.
func (s *ticketServiceImpl) ChangeCompany(ctx context.Context, ticketID string, companyID string, actorID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}
	if isTicketLockedByManagerFlow(ticket) {
		return nil, fmt.Errorf("тикет передан менеджеру: доступно только добавление комментариев")
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
	s.persistBitrixCompanyServicePointMapping(ctx, ticket.CompanyID, ticket.BitrixServicePointID)

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldCompany, tickets.HistorySourceUI, oldCompanyName, newCompanyName, nil)
	ticket.CompanyName = newCompanyName
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	return ticket, nil
}

func (s *ticketServiceImpl) UpdateBitrixFields(ctx context.Context, ticketID string, bitrixServicePointID *int64, bitrixDealTitle string, actorID uint) (*tickets.Ticket, error) {
	if s.cfg == nil || !s.cfg.EnableBitrixGateway {
		return nil, fmt.Errorf("интеграция Bitrix24 отключена")
	}

	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}
	if isTicketLockedByManagerFlow(ticket) {
		return nil, fmt.Errorf("тикет передан менеджеру: доступно только добавление комментариев")
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
	s.persistBitrixCompanyServicePointMapping(ctx, ticket.CompanyID, ticket.BitrixServicePointID)

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

func (s *ticketServiceImpl) UnlinkFromBitrix(ctx context.Context, ticketID string, actorID uint, roles []string) (*tickets.Ticket, error) {
	actor, err := s.requireTicketAdmin(ctx, actorID, roles)
	if err != nil {
		return nil, err
	}

	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
	}

	hadBitrixBinding, err := s.unlinkTicketBitrixData(ctx, ticket)
	if err != nil {
		return nil, err
	}
	if !hadBitrixBinding {
		ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
		return ticket, nil
	}

	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}
	if actor != nil {
		s.recordHistory(ctx, ticket.ID, &actor.ID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldBitrixLink, tickets.HistorySourceUI, "linked", "unlinked", nil)
	}

	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	return ticket, nil
}

func (s *ticketServiceImpl) Delete(ctx context.Context, ticketID string, actorID uint, roles []string) error {
	if _, err := s.requireTicketAdmin(ctx, actorID, roles); err != nil {
		return err
	}

	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return ErrTicketNotFound
	}

	storageKeys, err := s.collectTicketStorageKeys(ctx, ticket.ID)
	if err != nil {
		return err
	}
	if _, err := s.unlinkTicketBitrixData(ctx, ticket); err != nil {
		return err
	}
	if err := s.ticketRepo.Delete(ctx, ticket.ID); err != nil {
		return err
	}

	s.cleanupTicketFiles(ticket.ID, storageKeys)
	return nil
}

func (s *ticketServiceImpl) AddComment(ctx context.Context, ticketID string, comment string, isPrivate bool, _ bool, userID uint) (*tickets.TicketComment, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
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

	commentID := uuid.New().String()
	newComment := &tickets.TicketComment{
		ID:              commentID,
		TicketID:        ticket.ID,
		ServiceDeskUUID: commentID,
		Text:            text,
		AuthorName:      authorName,
		AuthorUserID:    &userID,
		Source:          tickets.CommentSourceUI,
		CreationDate:    time.Now(),
		IsInternal:      false,
		IsPrivate:       isPrivate,
		ReplyToClient:   false,
	}
	if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{*newComment}); err != nil {
		return nil, err
	}
	if err := s.bindTicketTelephonyByText(ctx, ticket, text, userID); err != nil {
		s.logger.Warn("не удалось привязать телефонию по комментарию тикета", "ticket_id", ticket.ID, "comment_id", newComment.ID, "error", err)
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, tickets.HistorySourceUI, "", text, nil)
	return newComment, nil
}

func (s *ticketServiceImpl) UpdateComment(
	ctx context.Context,
	ticketID string,
	commentUUID string,
	comment string,
	_ bool,
	userID uint,
	roles []string,
) (*tickets.TicketComment, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, ErrTicketNotFound
	}

	target, err := s.ticketRepo.GetCommentByUUID(ctx, ticketID, commentUUID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrCommentNotFound
	}

	actor, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !canEditTicketComment(ticket, actor, target, roles) {
		return nil, ErrCommentForbidden
	}

	text := strings.TrimSpace(comment)
	if text == "" {
		return nil, fmt.Errorf("комментарий пустой")
	}
	nextReplyToClient := false
	if target.Text == text && target.ReplyToClient == nextReplyToClient {
		return target, nil
	}

	updated, err := s.ticketRepo.UpdateComment(ctx, ticketID, commentUUID, text, nextReplyToClient)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, ErrCommentNotFound
	}

	s.recordHistory(
		ctx,
		ticketID,
		&userID,
		tickets.HistoryActionCommentUpdated,
		tickets.HistoryFieldComment,
		tickets.HistorySourceUI,
		target.Text,
		text,
		map[string]interface{}{"comment_uuid": strings.TrimSpace(commentUUID)},
	)
	return updated, nil
}

func (s *ticketServiceImpl) DeleteComment(
	ctx context.Context,
	ticketID string,
	commentUUID string,
	userID uint,
	roles []string,
) error {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return ErrTicketNotFound
	}

	target, err := s.ticketRepo.GetCommentByUUID(ctx, ticketID, commentUUID)
	if err != nil {
		return err
	}
	if target == nil {
		return ErrCommentNotFound
	}

	actor, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if !canDeleteTicketComment(ticket, actor, target, roles) {
		return ErrCommentForbidden
	}

	var deleted *tickets.TicketComment
	if shouldSoftDeleteComment(target) || isPyrusTicket(ticket) {
		deleted, err = s.ticketRepo.SoftDeleteComment(ctx, ticketID, commentUUID, time.Now())
	} else {
		deleted, err = s.ticketRepo.HardDeleteComment(ctx, ticketID, commentUUID)
	}
	if err != nil {
		return err
	}
	if deleted == nil {
		return ErrCommentNotFound
	}

	s.recordHistory(
		ctx,
		ticketID,
		&userID,
		tickets.HistoryActionCommentDeleted,
		tickets.HistoryFieldComment,
		tickets.HistorySourceUI,
		deleted.Text,
		"",
		map[string]interface{}{"comment_uuid": strings.TrimSpace(commentUUID)},
	)
	return nil
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

func (s *ticketServiceImpl) ProcessExpiredDeferred(ctx context.Context, now time.Time, limit int) ([]DeferredStatusActivation, error) {
	if now.IsZero() {
		now = time.Now()
	}

	items, err := s.ticketRepo.ListExpiredDeferred(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}

	result := make([]DeferredStatusActivation, 0, len(items))
	for i := range items {
		ticket := items[i]
		if strings.TrimSpace(ticket.Status) != tickets.StatusDeferred {
			continue
		}

		recipientID := uint(0)
		if ticket.DeferredByID != nil {
			recipientID = *ticket.DeferredByID
		}

		ticket.Status = tickets.StatusInProgress
		ticket.DeferredUntil = nil
		ticket.DeferredByID = nil
		if err := s.ticketRepo.Update(ctx, &ticket); err != nil {
			s.logger.Error("Не удалось перевести отложенный тикет в работу", "ticket_id", ticket.ID, "error", err)
			continue
		}

		s.recordHistory(
			ctx,
			ticket.ID,
			nil,
			tickets.HistoryActionFieldChanged,
			tickets.HistoryFieldStatus,
			tickets.HistorySourceSystem,
			tickets.StatusDeferred,
			tickets.StatusInProgress,
			map[string]interface{}{"reason": "deferred_timeout"},
		)

		if recipientID > 0 {
			result = append(result, DeferredStatusActivation{
				TicketID:        ticket.ID,
				RecipientUserID: recipientID,
			})
		}
	}

	return result, nil
}

func parseDeferredUntil(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("для статуса \"Отложено\" необходимо указать дату и время")
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04",
	}

	var parsed time.Time
	var err error
	for _, layout := range layouts {
		parsed, err = time.Parse(layout, value)
		if err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("некорректный формат deferred_until")
	}
	if parsed.Before(time.Now().Add(-10 * time.Second)) {
		return time.Time{}, fmt.Errorf("время отложенного статуса должно быть в будущем")
	}

	return parsed.UTC(), nil
}

func (s *ticketServiceImpl) RecordConnectionCopy(
	ctx context.Context,
	ticketID string,
	label string,
	value string,
	entityType string,
	entityID string,
	connectionField string,
	userID uint,
) error {
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

	meta := map[string]interface{}{
		"entity_type": strings.TrimSpace(entityType),
		"entity_id":   strings.TrimSpace(entityID),
		"field":       strings.TrimSpace(connectionField),
	}
	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionConnectionCopied, tickets.HistoryFieldConnection, tickets.HistorySourceUI, "", line+": "+val, meta)
	s.recordEntityConnectionCopy(ctx, entityType, entityID, connectionField, userID)
	return nil
}

func (s *ticketServiceImpl) GetConnectionCopyStats(ctx context.Context, ticketID string) ([]tickets.ConnectionCopyStat, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
	}
	if s.ownerHistoryRepo == nil {
		return []tickets.ConnectionCopyStat{}, nil
	}

	entityTypes := []string{tickets.AssetTypeServer, tickets.AssetTypeWorkstation}
	entityIDs := make([]string, 0, 256)
	seen := make(map[string]struct{}, 256)
	appendID := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		entityIDs = append(entityIDs, value)
	}

	if strings.TrimSpace(ticket.CompanyID) != "" {
		ownerIDs := []string{ticket.CompanyID}
		servers, serverErr := s.serverRepo.FindByOwnerIDs(ctx, ownerIDs)
		if serverErr == nil {
			for _, item := range servers {
				appendID(item.ID)
			}
		}
		workstations, wsErr := s.workstationRepo.FindByOwnerIDs(ctx, ownerIDs)
		if wsErr == nil {
			for _, item := range workstations {
				appendID(item.ID)
			}
		}
	}

	if len(entityIDs) == 0 {
		return []tickets.ConnectionCopyStat{}, nil
	}

	events, err := s.ownerHistoryRepo.ListByEntitiesAndSources(
		ctx,
		entityTypes,
		entityIDs,
		[]string{
			models.OwnerChangeSourceConnCopyRemoteID,
			models.OwnerChangeSourceConnCopyServerIP,
		},
		5000,
	)
	if err != nil {
		return nil, err
	}

	statsMap := make(map[string]*tickets.ConnectionCopyStat, len(entityIDs))
	for _, event := range events {
		entityType := strings.TrimSpace(event.EntityType)
		entityID := strings.TrimSpace(event.EntityID)
		if entityType == "" || entityID == "" {
			continue
		}
		key := entityType + ":" + entityID
		row, exists := statsMap[key]
		if !exists {
			row = &tickets.ConnectionCopyStat{
				EntityType: entityType,
				EntityID:   entityID,
			}
			statsMap[key] = row
		}
		row.CopyCount++
		if row.LastCopiedAt == nil || event.CreatedAt.After(*row.LastCopiedAt) {
			ts := event.CreatedAt
			row.LastCopiedAt = &ts
		}
	}

	result := make([]tickets.ConnectionCopyStat, 0, len(statsMap))
	for _, item := range statsMap {
		result = append(result, *item)
	}
	return result, nil
}

func (s *ticketServiceImpl) recordEntityConnectionCopy(
	ctx context.Context,
	entityType string,
	entityID string,
	connectionField string,
	userID uint,
) {
	entityType = strings.TrimSpace(entityType)
	entityID = strings.TrimSpace(entityID)
	if entityType == "" || entityID == "" || s.ownerHistoryRepo == nil {
		return
	}
	if entityType != tickets.AssetTypeServer && entityType != tickets.AssetTypeWorkstation {
		return
	}

	normalizedField := strings.ToLower(strings.TrimSpace(connectionField))
	source := models.OwnerChangeSourceConnCopyRemoteID
	comment := "Копирование ID удалённого подключения"
	if entityType == tickets.AssetTypeServer && (normalizedField == "ip" || normalizedField == "address") {
		source = models.OwnerChangeSourceConnCopyServerIP
		comment = "Копирование адреса сервера"
	}
	if normalizedField != "" {
		comment = comment + ": " + normalizedField
	}

	var ownerID string
	switch entityType {
	case tickets.AssetTypeServer:
		srv, err := s.serverRepo.GetByID(ctx, entityID)
		if err != nil || srv == nil || srv.OwnerID == nil {
			return
		}
		ownerID = strings.TrimSpace(*srv.OwnerID)
	case tickets.AssetTypeWorkstation:
		ws, err := s.workstationRepo.GetByID(ctx, entityID)
		if err != nil || ws == nil || ws.OwnerID == nil {
			return
		}
		ownerID = strings.TrimSpace(*ws.OwnerID)
	}
	if ownerID == "" {
		return
	}

	userIDText := fmt.Sprintf("%d", userID)
	_ = s.ownerHistoryRepo.Create(ctx, &models.OwnerChangeHistory{
		EntityType:      entityType,
		EntityID:        entityID,
		ToOwnerID:       ownerID,
		ChangeSource:    source,
		ChangedByUserID: &userIDText,
		Comment:         &comment,
	})
}

// GetDetails возвращает детали тикета, историю Рё вложения.
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
	s.enrichPyrusTaskLink(ctx, ticket)

	// Загрузка истории
	history, _ := s.ticketRepo.GetHistory(ctx, ticketID)
	s.enrichTicketHistoryUsers(ctx, history)

	// Загрузка вложений
	attachments, _ := s.ticketRepo.GetAttachments(ctx, ticketID)

	var contact *telephony.Contact
	if s.telephonyRepo != nil && ticket.ContactID != nil {
		contact, err = s.telephonyRepo.GetContactByID(ctx, *ticket.ContactID)
		if err != nil {
			return nil, err
		}
	}

	details := &tickets.TicketDetails{
		Metadata: *ticket,
		// CompanyName: ticket.CompanyName, // Если это поле есть РІ структуре (gorm ->)
		Contact:     contact,
		Calls:       make([]tickets.TicketCall, 0),
		History:     history,
		Attachments: attachments,
		Comments:    make([]tickets.Comment, 0),
	}

	if s.telephonyRepo != nil {
		callItems, callErr := s.telephonyRepo.ListCallsByTicketID(ctx, ticketID)
		if callErr != nil {
			return nil, callErr
		}
		if len(callItems) > 0 {
			employees, employeeErr := loadMegafonIntegratedEmployees(ctx, s.telephonyRepo, s.userRepo)
			if employeeErr != nil {
				return nil, employeeErr
			}
			employeesByLogin := make(map[string]TelephonyLineEmployeeView, len(employees))
			employeesByUserID := make(map[uint]TelephonyLineEmployeeView, len(employees))
			for _, item := range employees {
				employeesByLogin[item.Login] = item
				if item.UserID != nil {
					employeesByUserID[*item.UserID] = item
				}
			}

			contactsByPhone := make(map[string]*telephony.Contact, len(callItems))
			details.Calls = make([]tickets.TicketCall, 0, len(callItems))
			for _, callItem := range callItems {
				var callContact *telephony.Contact
				if phone := strings.TrimSpace(safeTelephonyString(callItem.ClientPhone)); phone != "" {
					if cached, ok := contactsByPhone[phone]; ok {
						callContact = cached
					} else {
						callContact, callErr = s.telephonyRepo.GetContactByPhone(ctx, phone)
						if callErr != nil {
							return nil, callErr
						}
						contactsByPhone[phone] = callContact
					}
				}

				callView := tickets.TicketCall{
					Call:    callItem,
					Contact: callContact,
				}
				if callItem.EmployeeUserID != nil {
					if employee, ok := employeesByUserID[*callItem.EmployeeUserID]; ok {
						callView.EmployeeName = employee.Name
						callView.EmployeeState = employee.Status
					}
				}
				if callView.EmployeeName == "" && callItem.EmployeeLogin != nil {
					if employee, ok := employeesByLogin[strings.TrimSpace(*callItem.EmployeeLogin)]; ok {
						callView.EmployeeName = employee.Name
						callView.EmployeeState = employee.Status
					}
				}
				details.Calls = append(details.Calls, callView)
			}
		}
	}

	// Комментарии из локальной БД (офлайн/сидер)
	localComments, _ := s.ticketRepo.GetComments(ctx, ticketID)
	if len(localComments) > 0 {
		for _, c := range localComments {
			commentUUID := strings.TrimSpace(c.ServiceDeskUUID)
			if commentUUID == "" {
				commentUUID = strings.TrimSpace(c.ID)
			}
			details.Comments = append(details.Comments, tickets.Comment{
				UUID:          commentUUID,
				Text:          c.Text,
				AuthorName:    c.AuthorName,
				AuthorUserID:  c.AuthorUserID,
				CreationDate:  c.CreationDate,
				IsInternal:    c.IsInternal,
				IsPrivate:     c.IsPrivate,
				ReplyToClient: c.ReplyToClient,
			})
		}
	}

	// Попытка получить описание из SD для легаси тикетов
	if s.isServiceDeskEnabledForReads() && ticket.ServiceDeskUUID != "" && len(localComments) == 0 {
		sdData, err := s.sdClient.FetchEntityDetails(ctx, ticket.ServiceDeskUUID, "Ticket")
		if err == nil {
			if desc, ok := sdData["descriptionRTF"].(string); ok {
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

func (s *ticketServiceImpl) enrichPyrusTaskLinks(ctx context.Context, items []tickets.Ticket) {
	if len(items) == 0 {
		return
	}
	ticketIDs := make([]string, 0, len(items))
	for i := range items {
		if value := strings.TrimSpace(items[i].ID); value != "" {
			ticketIDs = append(ticketIDs, value)
		}
	}
	if len(ticketIDs) == 0 || s.pyrusRepo == nil {
		for i := range items {
			s.applyPyrusTaskLink(&items[i], nil)
		}
		return
	}

	linksByTicketID, err := s.pyrusRepo.GetTicketLinksByTicketIDs(ctx, ticketIDs)
	if err != nil {
		for i := range items {
			s.applyPyrusTaskLink(&items[i], nil)
		}
		return
	}
	for i := range items {
		link, ok := linksByTicketID[strings.TrimSpace(items[i].ID)]
		if ok {
			s.applyPyrusTaskLink(&items[i], &link)
			continue
		}
		s.applyPyrusTaskLink(&items[i], nil)
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

func (s *ticketServiceImpl) enrichPyrusTaskLink(ctx context.Context, ticket *tickets.Ticket) {
	if ticket == nil {
		return
	}
	if s.pyrusRepo == nil {
		s.applyPyrusTaskLink(ticket, nil)
		return
	}
	link, err := s.pyrusRepo.GetTicketLinkByTicketID(ctx, ticket.ID)
	if err != nil {
		s.applyPyrusTaskLink(ticket, nil)
		return
	}
	s.applyPyrusTaskLink(ticket, link)
}

func (s *ticketServiceImpl) applyPyrusTaskLink(ticket *tickets.Ticket, link *pyrus.TicketLink) {
	if ticket == nil {
		return
	}
	ticket.PyrusTaskID = nil
	ticket.PyrusTaskURL = ""

	taskID := int64(0)
	switch {
	case link != nil && link.PyrusTaskID > 0:
		taskID = link.PyrusTaskID
	default:
		taskID = parsePyrusTaskIDFromServiceDeskUUID(ticket.ServiceDeskUUID)
	}
	if taskID <= 0 {
		return
	}
	ticket.PyrusTaskID = &taskID
	ticket.PyrusTaskURL = s.buildPyrusTaskURL(taskID)
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

func (s *ticketServiceImpl) buildPyrusTaskURL(taskID int64) string {
	if taskID <= 0 {
		return ""
	}
	base := s.pyrusPortalBaseURL()
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/t#id%d", base, taskID)
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

func (s *ticketServiceImpl) pyrusPortalBaseURL() string {
	if s.cfg == nil {
		return "https://pyrus.com"
	}
	base := strings.TrimSpace(s.cfg.PyrusAPIBaseURL)
	if base == "" {
		return "https://pyrus.com"
	}
	parsed, err := url.Parse(base)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		host := parsed.Host
		if rest, ok := strings.CutPrefix(host, "api."); ok && strings.TrimSpace(rest) != "" {
			host = rest
		}
		return strings.TrimRight(parsed.Scheme+"://"+host, "/")
	}
	return "https://pyrus.com"
}

func parsePyrusTaskIDFromServiceDeskUUID(serviceDeskUUID string) int64 {
	value := strings.TrimSpace(serviceDeskUUID)
	if value == "" {
		return 0
	}
	rest, ok := strings.CutPrefix(value, "pyrus:task:")
	if !ok {
		return 0
	}
	taskID, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil || taskID <= 0 {
		return 0
	}
	return taskID
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
	if isTicketLockedByManagerFlow(ticket) {
		return nil, fmt.Errorf("тикет передан менеджеру: доступно только добавление комментариев")
	}

	oldValue := ticket.Description
	ticket.Description = description
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}
	if err := s.bindTicketTelephonyByText(ctx, ticket, description, userID); err != nil {
		s.logger.Warn("не удалось привязать телефонию по описанию тикета", "ticket_id", ticket.ID, "error", err)
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldDescription, tickets.HistorySourceUI, oldValue, description, nil)
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)
	return ticket, nil
}

func (s *ticketServiceImpl) bindTicketTelephonyByText(ctx context.Context, ticket *tickets.Ticket, text string, actorID uint) error {
	if s == nil || s.telephonyRepo == nil || s.ticketRepo == nil || ticket == nil {
		return nil
	}
	enabled, err := s.ticketPhoneParsingEnabledForUser(ctx, actorID)
	if err != nil || !enabled {
		return err
	}
	phone := extractFirstPhoneFromTicketText(text)
	if phone == "" {
		return nil
	}
	contact, err := s.telephonyRepo.EnsureContact(ctx, phone, phone)
	if err != nil {
		return err
	}
	if contact != nil {
		if ticket.ContactID == nil || *ticket.ContactID != contact.ID {
			ticket.ContactID = &contact.ID
			if err = s.ticketRepo.Update(ctx, ticket); err != nil {
				return err
			}
		}
		if strings.TrimSpace(ticket.CompanyID) != "" {
			if err = s.telephonyRepo.UpsertContactCompanyLink(ctx, contact.ID, ticket.CompanyID, time.Now()); err != nil {
				return err
			}
		}
	}

	calls, _, err := s.telephonyRepo.ListCalls(ctx, telephony.CallListFilter{
		ClientPhone:       phone,
		OnlyWithoutTicket: true,
		Limit:             20,
	})
	if err != nil {
		return err
	}
	for i := range calls {
		if strings.TrimSpace(safeTelephonyString(calls[i].ClientPhone)) != phone {
			continue
		}
		if err = s.telephonyRepo.UpsertCallTicketLink(ctx, &telephony.CallTicketLink{
			TelephonyCallID: calls[i].ID,
			TicketID:        ticket.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func extractFirstPhoneFromTicketText(text string) string {
	plain := toPlainText(text)
	for _, match := range ticketTextPhoneRegex.FindAllStringSubmatch(plain, -1) {
		if len(match) < 3 {
			continue
		}
		raw := match[2]
		phone := normalizeMegafonPhone(raw)
		if isSupportedTicketTextPhone(phone) {
			return phone
		}
	}
	return ""
}

func (s *ticketServiceImpl) ticketPhoneParsingEnabledForUser(ctx context.Context, userID uint) (bool, error) {
	if s == nil || s.userRepo == nil || userID == 0 {
		return true, nil
	}
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || u == nil {
		return true, err
	}
	configMap := mapProfileConfig(u.ProfileConfig)
	ticketsConfig, ok := configMap["tickets"].(map[string]interface{})
	if !ok {
		return true, nil
	}
	value, exists := ticketsConfig["parse_phone_from_description"]
	if !exists {
		return true, nil
	}
	enabled, ok := value.(bool)
	if !ok {
		return true, nil
	}
	return enabled, nil
}

func isSupportedTicketTextPhone(phone string) bool {
	phone = strings.TrimSpace(phone)
	return len(phone) == 11 && strings.HasPrefix(phone, "79")
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

func (s *ticketServiceImpl) requireTicketAdmin(ctx context.Context, actorID uint, roles []string) (*user.User, error) {
	if actorID == 0 {
		return nil, ErrTicketForbidden
	}
	actor, err := s.userRepo.GetByID(ctx, actorID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить пользователя: %w", err)
	}
	if actor == nil {
		return nil, ErrTicketForbidden
	}
	if isAdminUser(actor) || hasUserRole(roles, user.RoleAdmin) {
		return actor, nil
	}
	return actor, ErrTicketForbidden
}

func (s *ticketServiceImpl) unlinkTicketBitrixData(ctx context.Context, ticket *tickets.Ticket) (bool, error) {
	if ticket == nil {
		return false, nil
	}

	hadBitrixBinding := ticket.SyncWithBitrix ||
		ticket.BitrixServicePointID != nil ||
		strings.TrimSpace(ticket.BitrixDealTitle) != "" ||
		strings.HasPrefix(strings.TrimSpace(ticket.ServiceDeskUUID), "b24:deal:")

	dealID, err := s.resolveBitrixDealIDForTicket(ctx, ticket)
	if err != nil {
		return false, err
	}
	if dealID != nil && *dealID > 0 {
		hadBitrixBinding = true
	}

	if s.bitrixRepo != nil {
		if dealID != nil && *dealID > 0 {
			if err := s.bitrixRepo.UpsertIgnoredDeal(ctx, &bitrix.IgnoredDeal{
				B24DealID: *dealID,
				TicketID:  ticket.ID,
			}); err != nil {
				return false, err
			}
		}
		if err := s.bitrixRepo.DeleteDealLinkByTicketID(ctx, ticket.ID); err != nil {
			return false, err
		}
		if err := s.bitrixRepo.DeleteCommentLinksByTicketID(ctx, ticket.ID); err != nil {
			return false, err
		}
	}

	ticket.SyncWithBitrix = false
	ticket.BitrixServicePointID = nil
	ticket.BitrixDealTitle = ""
	if _, ok := parseBitrixDealIDFromServiceDeskUUID(ticket.ServiceDeskUUID); ok {
		ticket.ServiceDeskUUID = ""
	}

	return hadBitrixBinding, nil
}

func (s *ticketServiceImpl) resolveBitrixDealIDForTicket(ctx context.Context, ticket *tickets.Ticket) (*int64, error) {
	if ticket == nil {
		return nil, nil
	}
	if ticket.BitrixDealID != nil && *ticket.BitrixDealID > 0 {
		dealID := *ticket.BitrixDealID
		return &dealID, nil
	}
	if s.bitrixRepo != nil {
		link, err := s.bitrixRepo.GetDealLinkByTicketID(ctx, ticket.ID)
		if err != nil {
			return nil, err
		}
		if link != nil && link.B24DealID > 0 {
			dealID := link.B24DealID
			return &dealID, nil
		}
	}
	if dealID, ok := parseBitrixDealIDFromServiceDeskUUID(ticket.ServiceDeskUUID); ok {
		return &dealID, nil
	}
	return nil, nil
}

func parseBitrixDealIDFromServiceDeskUUID(sdUUID string) (int64, bool) {
	value := strings.TrimSpace(sdUUID)
	if !strings.HasPrefix(value, "b24:deal:") {
		return 0, false
	}
	dealID, err := strconv.ParseInt(strings.TrimPrefix(value, "b24:deal:"), 10, 64)
	if err != nil || dealID <= 0 {
		return 0, false
	}
	return dealID, true
}

func (s *ticketServiceImpl) collectTicketStorageKeys(ctx context.Context, ticketID string) ([]string, error) {
	links, err := s.ticketRepo.GetTicketFileLinksByRelation(ctx, ticketID, nil)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, nil
	}

	result := make([]string, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		fileID := strings.TrimSpace(link.FileID)
		if fileID == "" {
			continue
		}
		asset, assetErr := s.ticketRepo.GetFileAssetByID(ctx, fileID)
		if assetErr != nil {
			return nil, assetErr
		}
		if asset == nil {
			continue
		}
		storageKey := strings.TrimSpace(asset.StorageKey)
		if storageKey == "" {
			continue
		}
		if _, exists := seen[storageKey]; exists {
			continue
		}
		seen[storageKey] = struct{}{}
		result = append(result, storageKey)
	}
	return result, nil
}

func (s *ticketServiceImpl) cleanupTicketFiles(ticketID string, storageKeys []string) {
	if s.cfg == nil || strings.TrimSpace(s.cfg.TicketStoragePath) == "" {
		return
	}

	for _, storageKey := range storageKeys {
		normalized := strings.TrimSpace(storageKey)
		if normalized == "" {
			continue
		}
		absPath := filepath.Join(s.cfg.TicketStoragePath, filepath.FromSlash(normalized))
		if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) && s.logger != nil {
			s.logger.Warn("Не удалось удалить файл тикета", "ticket_id", ticketID, "path", absPath, "error", err)
		}
	}

	ticketDir := filepath.Join(s.cfg.TicketStoragePath, ticketID)
	if err := os.RemoveAll(ticketDir); err != nil && s.logger != nil {
		s.logger.Warn("Не удалось удалить директорию тикета", "ticket_id", ticketID, "path", ticketDir, "error", err)
	}
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
	// 1. Получаем заявку, чтобы узнать, какой компании принадлежит
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("failed to get ticket: %w", err)
	}
	if ticket == nil {
		return fmt.Errorf("заявка не найдена")
	}

	// 2. Проверяем существование актива Рё совпадение владельца
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
	// Если у оборудования нет владельца (пустая строка), считаем это СЂРёСЃРєРѕРј, РЅРѕ разрешаем (или запрещаем, зависит РѕС‚ бизнес-логики).
	// Р’ данном случае запретим привязку Рє "чужому" оборудованию.
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

func isTicketLockedByManagerFlow(ticket *tickets.Ticket) bool {
	return ticket != nil && strings.TrimSpace(ticket.Status) == tickets.StatusToManager
}

func (s *ticketServiceImpl) enrichTicketHistoryUsers(ctx context.Context, history []tickets.TicketHistory) {
	if len(history) == 0 || s.userRepo == nil {
		return
	}

	userNames := make(map[uint]string, len(history))
	for i := range history {
		if history[i].UserID == nil || *history[i].UserID == 0 {
			continue
		}

		userID := *history[i].UserID
		if cachedName, ok := userNames[userID]; ok {
			history[i].UserName = cachedName
			continue
		}

		userItem, err := s.userRepo.GetByID(ctx, userID)
		if err != nil || userItem == nil {
			userNames[userID] = ""
			continue
		}

		fullName := strings.TrimSpace(userItem.FullName)
		if fullName == "" {
			fullName = strings.TrimSpace(userItem.Username)
		}
		userNames[userID] = fullName
		history[i].UserName = fullName
	}
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

// processHtmlContent ищет ссылки РЅР° файлы Naumen, скачивает РёС… Рё заменяет РЅР° локальные URL.
// sdUUID - внешний UUID заявки (например, serviceCall$123), используется для РіСЂСѓРїРїРёСЂРѕРІРєРё файлов РІ папке.
func (s *ticketServiceImpl) processHtmlContent(sdUUID string, htmlContent string) string {
	// Ищем все вхождения uuid=file$XXXXX
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
		// Простой вариант: РёРјСЏ файла = UUID. Браузеры часто умеют определять тип РїРѕ контенту,
		// РЅРѕ лучше сохранять расширение. Пока сохраняем как есть.

		if _, err := os.Stat(localFilePath); os.IsNotExist(err) {
			// 2. РФайла нет - скачиваем
			err := s.downloadFileFromNaumen(fileUUID, localFilePath)
			if err != nil {
				s.logger.Error("Failed to download file from Naumen", "fileUUID", fileUUID, "error", err)
				continue // Пропускаем замену, если не удалось скачать
			}
		}

		// 3. Заменяем ссылку РІ HTML
		// Исходная: ... src="./download?uuid=file$13205558" ...
		// Целевая:  ... src="/api/static/tickets/serviceCall$123/file$13205558" ...

		// Находим полный кусок "./download?uuid=file$XXXX" Рё заменяем его
		// Регулярка ищет только uuid=..., поэтому заменим грубо, РЅРѕ надежно для Naumen:
		// "./download?uuid=" + fileUUID -> "/api/static/tickets/" + sdUUID + "/" + fileUUID

		oldLink := fmt.Sprintf("./download?uuid=%s", fileUUID)
		newLink := fmt.Sprintf("/api/static/tickets/%s/%s", sdUUID, fileUUID)
		processedHtml = strings.ReplaceAll(processedHtml, oldLink, newLink)

		// На случай, если ссылка без точки РІ начале (бывает РїРѕ-разному)
		oldLink2 := fmt.Sprintf("/download?uuid=%s", fileUUID)
		processedHtml = strings.ReplaceAll(processedHtml, oldLink2, newLink)
	}

	return processedHtml
}

// downloadFileFromNaumen выполняет запрос Рє API Naumen Рё сохраняет файл.
func (s *ticketServiceImpl) downloadFileFromNaumen(fileUUID, destPath string) error {
	// URL: <baseURL>/services/rest/get-file/file$123?accessKey=<accessKey>
	// Базовый URL РІ конфиге может быть с /sd или без, нужно аккуратно собрать.
	// Обычно cfg.ServiceDeskBaseURL = "https://myhoreca.itsm365.com/sd"

	// Убираем trailing slash
	baseURL := strings.TrimRight(s.cfg.ServiceDeskBaseURL, "/")
	// РФормируем URL для скачивания
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
	if strings.TrimSpace(u.Position) == user.RoleAdmin {
		return true
	}
	for _, role := range u.Roles {
		if strings.TrimSpace(role.Name) == user.RoleAdmin {
			return true
		}
	}
	return false
}

func hasUserRole(roles []string, target string) bool {
	needle := strings.TrimSpace(target)
	if needle == "" {
		return false
	}
	for _, role := range roles {
		if strings.TrimSpace(role) == needle {
			return true
		}
	}
	return false
}

func shouldSoftDeleteComment(comment *tickets.TicketComment) bool {
	if comment == nil {
		return false
	}
	sdUUID := strings.TrimSpace(comment.ServiceDeskUUID)
	if sdUUID == "" {
		return false
	}
	if strings.HasPrefix(sdUUID, "serviceCall$") || strings.HasPrefix(sdUUID, "b24-") {
		return true
	}
	return sdUUID != strings.TrimSpace(comment.ID)
}

func userDisplayNameForComment(u *user.User) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.FullName) != "" {
		return strings.TrimSpace(u.FullName)
	}
	return strings.TrimSpace(strings.Join([]string{u.LastName, u.FirstName}, " "))
}

func isCommentAuthor(actor *user.User, comment *tickets.TicketComment) bool {
	if actor == nil || comment == nil {
		return false
	}
	if comment.AuthorUserID != nil && *comment.AuthorUserID > 0 {
		return actor.ID == *comment.AuthorUserID
	}
	return strings.EqualFold(
		strings.TrimSpace(userDisplayNameForComment(actor)),
		strings.TrimSpace(comment.AuthorName),
	)
}

func canEditTicketComment(_ *tickets.Ticket, actor *user.User, comment *tickets.TicketComment, _ []string) bool {
	return isCommentAuthor(actor, comment)
}

func canDeleteTicketComment(_ *tickets.Ticket, actor *user.User, comment *tickets.TicketComment, _ []string) bool {
	return isCommentAuthor(actor, comment)
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

func (s *ticketServiceImpl) syncCompanyContractFromBitrixPoint(ctx context.Context, companyID string, bitrixServicePointID *int64) (*company.Company, error) {
	if s.contractService == nil || s.bitrixRepo == nil || !isValidBitrixServicePointID(bitrixServicePointID) {
		return nil, nil
	}

	skip, err := isBitrixTemporaryServicePoint(ctx, s.cfg, s.bitrixRepo, *bitrixServicePointID)
	if err != nil {
		return nil, err
	}
	if skip {
		return nil, nil
	}

	point, err := s.bitrixRepo.GetServicePointByID(ctx, *bitrixServicePointID)
	if err != nil {
		return nil, err
	}
	if point == nil {
		return nil, fmt.Errorf("точка обслуживания Bitrix24 не найдена")
	}

	snapshot := contractsvc.BuildDailySnapshotFromBitrixServicePoint(companyID, *point)
	snapshot.SourceHash = buildBitrixPointContractSourceHash(*point)
	if err := s.contractService.SyncDailySnapshots(ctx, []contract.DailyCompanyContractSnapshot{snapshot}); err != nil {
		return nil, err
	}

	return s.companyRepo.GetByID(ctx, companyID)
}

func isValidBitrixServicePointID(value *int64) bool {
	return value != nil && *value > 0
}

func buildBitrixPointContractSourceHash(point bitrix.ServicePoint) string {
	return fmt.Sprintf("bitrix-service-point:%d:%d", point.B24ElementID, point.UpdatedAt.UTC().UnixNano())
}

func (s *ticketServiceImpl) resolveMappedBitrixServicePointID(ctx context.Context, companyID string) (*int64, error) {
	if s.bitrixRepo == nil {
		return nil, nil
	}
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return nil, nil
	}
	mapping, err := s.bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if mapping == nil || mapping.BitrixServicePointID <= 0 {
		return nil, nil
	}
	skip, err := isBitrixTemporaryServicePoint(ctx, s.cfg, s.bitrixRepo, mapping.BitrixServicePointID)
	if err != nil {
		return nil, err
	}
	if skip {
		return nil, nil
	}
	mappedID := mapping.BitrixServicePointID
	return &mappedID, nil
}

func (s *ticketServiceImpl) persistBitrixCompanyServicePointMapping(ctx context.Context, companyID string, bitrixServicePointID *int64) {
	if s.bitrixRepo == nil || bitrixServicePointID == nil || *bitrixServicePointID <= 0 {
		return
	}
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return
	}
	skip, err := isBitrixTemporaryServicePoint(ctx, s.cfg, s.bitrixRepo, *bitrixServicePointID)
	if err != nil {
		s.logger.Error("Не удалось проверить правила сохранения сопоставления точки Bitrix24", "company_id", companyID, "bitrix_service_point_id", *bitrixServicePointID, "error", err)
		return
	}
	if skip {
		s.clearBitrixCompanyServicePointMapping(ctx, companyID, *bitrixServicePointID)
		return
	}
	if err := s.bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            companyID,
		BitrixServicePointID: *bitrixServicePointID,
	}); err != nil {
		s.logger.Error("Не удалось обновить сопоставление компании с точкой Bitrix24", "company_id", companyID, "bitrix_service_point_id", *bitrixServicePointID, "error", err)
	}
}

func (s *ticketServiceImpl) clearBitrixCompanyServicePointMapping(ctx context.Context, companyID string, bitrixServicePointID int64) {
	if s.bitrixRepo == nil {
		return
	}
	companyID = strings.TrimSpace(companyID)
	if companyID != "" {
		if err := s.bitrixRepo.DeleteCompanyServicePointMappingByCompanyID(ctx, companyID); err != nil {
			s.logger.Error("Не удалось очистить сопоставление компании с точкой Bitrix24", "company_id", companyID, "bitrix_service_point_id", bitrixServicePointID, "error", err)
		}
	}
	if bitrixServicePointID > 0 {
		if err := s.bitrixRepo.DeleteCompanyServicePointMappingByPointID(ctx, bitrixServicePointID); err != nil {
			s.logger.Error("Не удалось очистить сопоставление тестовой точки Bitrix24", "bitrix_service_point_id", bitrixServicePointID, "error", err)
		}
	}
}

func normalizePyrusTicketStatus(rawStatus string) string {
	switch strings.TrimSpace(strings.ToLower(rawStatus)) {
	case tickets.StatusResolved, tickets.StatusClosed:
		return tickets.StatusResolved
	case tickets.StatusInProgress, tickets.StatusPending, tickets.StatusDeferred:
		return strings.TrimSpace(strings.ToLower(rawStatus))
	default:
		return tickets.StatusNew
	}
}

func normalizePyrusTicketType(rawType string) string {
	if strings.TrimSpace(rawType) == "" {
		return tickets.TypeIncident
	}
	return strings.TrimSpace(rawType)
}

func isPyrusTicket(ticket *tickets.Ticket) bool {
	if ticket == nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(ticket.ServiceDeskUUID), "pyrus:task:")
}
