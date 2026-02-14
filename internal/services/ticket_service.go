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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Regex РґР»СЏ РїРѕРёСЃРєР° UUID С„Р°Р№Р»РѕРІ РІ СЃСЃС‹Р»РєР°С… Naumen (./download?uuid=file$123...)
var naumenFileRegex = regexp.MustCompile(`uuid=(file\$[0-9]+)`)

type TicketService interface {
	// Р§С‚РµРЅРёРµ
	List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error)
	GetLastComments(ctx context.Context, ticketIDs []string) (map[string]tickets.LastCommentInfo, error)
	GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error)
	GetDashboardStats(ctx context.Context) (*tickets.DashboardStats, error)
	GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error)

	// Р”РµР№СЃС‚РІРёСЏ
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

var ErrReporterNotFound = errors.New("РїРѕР»СЊР·РѕРІР°С‚РµР»СЊ-Р°РІС‚РѕСЂ РЅРµ РЅР°Р№РґРµРЅ")

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

// List РІРѕР·РІСЂР°С‰Р°РµС‚ СЃРїРёСЃРѕРє Р·Р°СЏРІРѕРє СЃ С„РёР»СЊС‚СЂР°С†РёРµР№.
func (s *ticketServiceImpl) List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error) {
	items, err := s.ticketRepo.Find(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("service: list tickets: %w", err)
	}
	s.applyCommonContractFlag(items)
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

// CreateInternal СЃРѕР·РґР°РµС‚ РІРЅСѓС‚СЂРµРЅРЅРёР№ С‚РёРєРµС‚.
func (s *ticketServiceImpl) CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error) {
	author, err := s.userRepo.GetByID(ctx, authorID)
	if err != nil {
		return nil, fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РїРѕР»СѓС‡РёС‚СЊ Р°РІС‚РѕСЂР° С‚РёРєРµС‚Р°: %w", err)
	}
	if author == nil {
		return nil, ErrReporterNotFound
	}
	if dto.AssigneeID == nil || *dto.AssigneeID == 0 {
		return nil, fmt.Errorf("РЅРµ РІС‹Р±СЂР°РЅ РёСЃРїРѕР»РЅРёС‚РµР»СЊ")
	}
	assignee, err := s.userRepo.GetByID(ctx, *dto.AssigneeID)
	if err != nil {
		return nil, fmt.Errorf("РЅРµ СѓРґР°Р»РѕСЃСЊ РїРѕР»СѓС‡РёС‚СЊ РёСЃРїРѕР»РЅРёС‚РµР»СЏ: %w", err)
	}
	if assignee == nil {
		return nil, fmt.Errorf("РёСЃРїРѕР»РЅРёС‚РµР»СЊ РЅРµ РЅР°Р№РґРµРЅ")
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
		return nil, fmt.Errorf("РЅРµ Р·Р°РїРѕР»РЅРµРЅ Р·Р°РіРѕР»РѕРІРѕРє СЃРґРµР»РєРё Bitrix24")
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
		return nil, fmt.Errorf("РєРѕРјРїР°РЅРёСЏ РЅРµ РЅР°Р№РґРµРЅР°")
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
	// Р’Р°Р»РёРґР°С†РёСЏ РїРѕР»РµР№
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

	// Р—Р°РїРёСЃСЊ РІ РёСЃС‚РѕСЂРёСЋ
	s.recordHistory(ctx, ticket.ID, &authorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, "", tickets.StatusNew)

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

// ChangeStatus РјРµРЅСЏРµС‚ СЃС‚Р°С‚СѓСЃ С‚РёРєРµС‚Р° Рё РїРёС€РµС‚ РёСЃС‚РѕСЂРёСЋ.
func (s *ticketServiceImpl) ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	oldStatus := ticket.Status
	if oldStatus == status {
		return ticket, nil // РЎС‚Р°С‚СѓСЃ РЅРµ РёР·РјРµРЅРёР»СЃСЏ
	}

	comment = strings.TrimSpace(comment)
	if (status == tickets.StatusResolved || status == tickets.StatusClosed) && comment != "" {
		oldResult := ticket.Result
		ticket.Result = comment
		if oldResult != ticket.Result {
			s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldResult, oldResult, ticket.Result)
		}
	}

	ticket.Status = status
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	// Р—Р°РїРёСЃСЊ РІ РёСЃС‚РѕСЂРёСЋ
	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, oldStatus, status)

	// Р•СЃР»Рё РµСЃС‚СЊ РєРѕРјРјРµРЅС‚Р°СЂРёР№, РґРѕР±Р°РІР»СЏРµРј РµРіРѕ РєР°Рє РёСЃС‚РѕСЂРёСЋ РёР»Рё РѕС‚РґРµР»СЊРЅРѕ
	if comment != "" {
		// Р”Р»СЏ РїСЂРѕСЃС‚РѕС‚С‹ РёСЃРїРѕР»СЊР·СѓРµРј Р»РµРіР°СЃРё СЃС‚СЂСѓРєС‚СѓСЂСѓ Comment, РµСЃР»Рё С„СЂРѕРЅС‚ РµС‘ Р¶РґРµС‚,
		// РЅРѕ Р»СѓС‡С€Рµ РїРёСЃР°С‚СЊ РІ History СЃ РїРѕР»РµРј "comment"
		// Р РµР°Р»РёР·СѓРµРј С‡РµСЂРµР· History РєР°Рє "comment_added"
		s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, "", comment)
	}

	// Р•СЃР»Рё Р·Р°СЏРІРєР° СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°РЅР° СЃ Naumen, РЅСѓР¶РЅРѕ РѕС‚РїСЂР°РІРёС‚СЊ РѕР±РЅРѕРІР»РµРЅРёРµ С‚СѓРґР°
	if ticket.ServiceDeskUUID != "" {
		// s.sdClient.UpdateEntity(...) // TODO: Р РµР°Р»РёР·РѕРІР°С‚СЊ РѕР±СЂР°С‚РЅСѓСЋ СЃРёРЅС…СЂРѕРЅРёР·Р°С†РёСЋ СЃС‚Р°С‚СѓСЃР°
	}

	return ticket, nil
}

// Assign РЅР°Р·РЅР°С‡Р°РµС‚ РёР»Рё СЃРЅРёРјР°РµС‚ РёСЃРїРѕР»РЅРёС‚РµР»СЏ.
func (s *ticketServiceImpl) Assign(ctx context.Context, ticketID string, assigneeID *uint, actorID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	var oldAssigneeName, newAssigneeName string

	// РџРѕР»СѓС‡Р°РµРј РёРјРµРЅР° РґР»СЏ РёСЃС‚РѕСЂРёРё
	if ticket.Assignee != nil {
		oldAssigneeName = ticket.Assignee.FullName
	} else {
		oldAssigneeName = "РќРµ РЅР°Р·РЅР°С‡РµРЅ"
	}

	if assigneeID != nil {
		newAssignee, err := s.userRepo.GetByID(ctx, *assigneeID)
		if err != nil || newAssignee == nil {
			return nil, fmt.Errorf("РїРѕР»СЊР·РѕРІР°С‚РµР»СЊ-РёСЃРїРѕР»РЅРёС‚РµР»СЊ РЅРµ РЅР°Р№РґРµРЅ")
		}
		newAssigneeName = newAssignee.FullName
	} else {
		newAssigneeName = "РќРµ РЅР°Р·РЅР°С‡РµРЅ"
	}

	ticket.AssigneeID = assigneeID
	// Р•СЃР»Рё РЅР°Р·РЅР°С‡Р°РµРј, РїРµСЂРµРІРѕРґРёРј РІ InProgress, РµСЃР»Рё РѕРЅ Р±С‹Р» New
	if assigneeID != nil && ticket.Status == tickets.StatusNew {
		ticket.Status = tickets.StatusInProgress
	}

	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAssignee, oldAssigneeName, newAssigneeName)
	return ticket, nil
}

// ChangeCompany РјРµРЅСЏРµС‚ РєРѕРјРїР°РЅРёСЋ РІ С‚РёРєРµС‚Рµ Рё РїРµСЂРµСЃС‡РёС‚С‹РІР°РµС‚ РґРѕРіРѕРІРѕСЂ.
func (s *ticketServiceImpl) ChangeCompany(ctx context.Context, ticketID string, companyID string, actorID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	targetCompany, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	if targetCompany == nil {
		return nil, fmt.Errorf("РєРѕРјРїР°РЅРёСЏ РЅРµ РЅР°Р№РґРµРЅР°")
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

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldCompany, oldCompanyName, newCompanyName)
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
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	nextTitle := strings.TrimSpace(bitrixDealTitle)
	if ticket.SyncWithBitrix {
		if bitrixServicePointID == nil || *bitrixServicePointID <= 0 {
			return nil, fmt.Errorf("РЅРµ РІС‹Р±СЂР°РЅР° С‚РѕС‡РєР° РѕР±СЃР»СѓР¶РёРІР°РЅРёСЏ Bitrix24")
		}
		if nextTitle == "" {
			return nil, fmt.Errorf("РЅРµ Р·Р°РїРѕР»РЅРµРЅ Р·Р°РіРѕР»РѕРІРѕРє СЃРґРµР»РєРё Bitrix24")
		}
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

	ticket.BitrixServicePointID = bitrixServicePointID
	ticket.BitrixDealTitle = nextTitle
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	if oldPoint != nextPoint {
		s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, "bitrix_service_point_id", oldPoint, nextPoint)
	}
	if oldTitle != nextTitle {
		s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, "bitrix_deal_title", oldTitle, nextTitle)
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
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	text := strings.TrimSpace(comment)
	if text == "" {
		return nil, fmt.Errorf("РєРѕРјРјРµРЅС‚Р°СЂРёР№ РїСѓСЃС‚РѕР№")
	}

	authorName := "РЎРѕС‚СЂСѓРґРЅРёРє"
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

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, "", text)
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
			s.logger.Error("РќРµ СѓРґР°Р»РѕСЃСЊ Р°РІС‚РѕРјР°С‚РёС‡РµСЃРєРё Р·Р°РєСЂС‹С‚СЊ Р·Р°СЏРІРєСѓ", "ticket_id", ticket.ID, "error", err)
			continue
		}
		s.recordHistory(ctx, ticket.ID, nil, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, tickets.StatusResolved, tickets.StatusClosed)
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
		return fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	line := strings.TrimSpace(label)
	if line == "" {
		line = "РџРѕРґРєР»СЋС‡РµРЅРёРµ"
	}
	val := strings.TrimSpace(value)
	if val == "" {
		return fmt.Errorf("Р·РЅР°С‡РµРЅРёРµ РїРѕРґРєР»СЋС‡РµРЅРёСЏ РїСѓСЃС‚РѕРµ")
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionConnectionCopied, tickets.HistoryFieldConnection, "", line+": "+val)
	return nil
}

// GetDetails РІРѕР·РІСЂР°С‰Р°РµС‚ РґРµС‚Р°Р»Рё С‚РёРєРµС‚Р°, РёСЃС‚РѕСЂРёСЋ Рё РІР»РѕР¶РµРЅРёСЏ.
func (s *ticketServiceImpl) GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, nil
	}
	ticket.IsCommonContract = s.isCommonContractID(ticket.ContractID)

	// Р—Р°РіСЂСѓР·РєР° РёСЃС‚РѕСЂРёРё
	history, _ := s.ticketRepo.GetHistory(ctx, ticketID)

	// Р—Р°РіСЂСѓР·РєР° РІР»РѕР¶РµРЅРёР№
	attachments, _ := s.ticketRepo.GetAttachments(ctx, ticketID)

	details := &tickets.TicketDetails{
		Metadata: *ticket,
		// CompanyName: ticket.CompanyName, // Р•СЃР»Рё СЌС‚Рѕ РїРѕР»Рµ РµСЃС‚СЊ РІ СЃС‚СЂСѓРєС‚СѓСЂРµ (gorm ->)
		History:     history,
		Attachments: attachments,
		Comments:    make([]tickets.Comment, 0),
	}

	// РљРѕРјРјРµРЅС‚Р°СЂРёРё РёР· Р»РѕРєР°Р»СЊРЅРѕР№ Р‘Р” (РѕС„Р»Р°Р№РЅ/СЃРёРґРµСЂ)
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

	// РџРѕРїС‹С‚РєР° РїРѕР»СѓС‡РёС‚СЊ РѕРїРёСЃР°РЅРёРµ РёР· SD РґР»СЏ Р»РµРіР°СЃРё С‚РёРєРµС‚РѕРІ
	if s.isServiceDeskEnabledForReads() && ticket.ServiceDeskUUID != "" && len(localComments) == 0 {
		sdData, err := s.sdClient.FetchEntityDetails(ctx, ticket.ServiceDeskUUID, "Ticket")
		if err == nil {
			if desc, ok := sdData["descriptionRTF"].(string); ok {
				// Р’ РёРґРµР°Р»Рµ description РґРѕР»Р¶РµРЅ Р±С‹С‚СЊ РІ Р‘Р”, РЅРѕ РґР»СЏ Р»РµРіР°СЃРё Р±РµСЂРµРј РёР· SD
				if ticket.Description == "" {
					details.Metadata.Description = s.processHtmlContent(ticket.ServiceDeskUUID, desc)
				}
			}
		}
	}

	return details, nil
}

// UpdateDescription РѕР±РЅРѕРІР»СЏРµС‚ РѕРїРёСЃР°РЅРёРµ С‚РёРєРµС‚Р°.
func (s *ticketServiceImpl) UpdateDescription(ctx context.Context, ticketID string, description string, userID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	oldValue := ticket.Description
	ticket.Description = description
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldDescription, oldValue, description)
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
		return 0, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
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
		return nil, fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
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
	// 1. РџРѕР»СѓС‡Р°РµРј Р·Р°СЏРІРєСѓ, С‡С‚РѕР±С‹ СѓР·РЅР°С‚СЊ, РєР°РєРѕР№ РєРѕРјРїР°РЅРёРё РѕРЅР° РїСЂРёРЅР°РґР»РµР¶РёС‚
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("failed to get ticket: %w", err)
	}
	if ticket == nil {
		return fmt.Errorf("Р·Р°СЏРІРєР° РЅРµ РЅР°Р№РґРµРЅР°")
	}

	// 2. РџСЂРѕРІРµСЂСЏРµРј СЃСѓС‰РµСЃС‚РІРѕРІР°РЅРёРµ Р°РєС‚РёРІР° Рё СЃРѕРІРїР°РґРµРЅРёРµ РІР»Р°РґРµР»СЊС†Р°
	var assetOwnerID string

	switch assetType {
	case tickets.AssetTypeServer:
		asset, err := s.serverRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("СЃРµСЂРІРµСЂ РЅРµ РЅР°Р№РґРµРЅ")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	case tickets.AssetTypeFiscalRegister:
		asset, err := s.frRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("С„РёСЃРєР°Р»СЊРЅС‹Р№ СЂРµРіРёСЃС‚СЂР°С‚РѕСЂ РЅРµ РЅР°Р№РґРµРЅ")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	case tickets.AssetTypeWorkstation:
		asset, err := s.workstationRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("СЂР°Р±РѕС‡Р°СЏ СЃС‚Р°РЅС†РёСЏ РЅРµ РЅР°Р№РґРµРЅР°")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	default:
		return fmt.Errorf("РЅРµРїРѕРґРґРµСЂР¶РёРІР°РµРјС‹Р№ С‚РёРї РѕР±РѕСЂСѓРґРѕРІР°РЅРёСЏ: %s", assetType)
	}

	// 3. РЎСЂР°РІРЅРёРІР°РµРј РІР»Р°РґРµР»СЊС†РµРІ
	// Р•СЃР»Рё Сѓ РѕР±РѕСЂСѓРґРѕРІР°РЅРёСЏ РЅРµС‚ РІР»Р°РґРµР»СЊС†Р° (РїСѓСЃС‚Р°СЏ СЃС‚СЂРѕРєР°), СЃС‡РёС‚Р°РµРј СЌС‚Рѕ СЂРёСЃРєРѕРј, РЅРѕ СЂР°Р·СЂРµС€Р°РµРј (РёР»Рё Р·Р°РїСЂРµС‰Р°РµРј, Р·Р°РІРёСЃРёС‚ РѕС‚ Р±РёР·РЅРµСЃ-Р»РѕРіРёРєРё).
	// Р’ РґР°РЅРЅРѕРј СЃР»СѓС‡Р°Рµ Р·Р°РїСЂРµС‚РёРј РїСЂРёРІСЏР·РєСѓ Рє "С‡СѓР¶РѕРјСѓ" РѕР±РѕСЂСѓРґРѕРІР°РЅРёСЋ.
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

	s.recordHistory(ctx, ticketID, nil, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAsset, oldAsset, newAsset)
	return nil
}

// recordHistory - РІСЃРїРѕРјРѕРіР°С‚РµР»СЊРЅС‹Р№ РјРµС‚РѕРґ РґР»СЏ Р·Р°РїРёСЃРё Р°СѓРґРёС‚Р°.
func (s *ticketServiceImpl) recordHistory(ctx context.Context, ticketID string, userID *uint, action, field, oldVal, newVal string) {
	h := &tickets.TicketHistory{
		TicketID:  ticketID,
		UserID:    userID,
		Action:    action,
		Field:     field,
		OldValue:  oldVal,
		NewValue:  newVal,
		CreatedAt: time.Now(),
	}
	if err := s.ticketRepo.AddHistory(ctx, h); err != nil {
		s.logger.Error("Failed to record ticket history", "ticket_id", ticketID, "error", err)
	}
}

// processHtmlContent РёС‰РµС‚ СЃСЃС‹Р»РєРё РЅР° С„Р°Р№Р»С‹ Naumen, СЃРєР°С‡РёРІР°РµС‚ РёС… Рё Р·Р°РјРµРЅСЏРµС‚ РЅР° Р»РѕРєР°Р»СЊРЅС‹Рµ URL.
// sdUUID - РІРЅРµС€РЅРёР№ UUID Р·Р°СЏРІРєРё (РЅР°РїСЂРёРјРµСЂ, serviceCall$123), РёСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ РґР»СЏ РіСЂСѓРїРїРёСЂРѕРІРєРё С„Р°Р№Р»РѕРІ РІ РїР°РїРєРµ.
func (s *ticketServiceImpl) processHtmlContent(sdUUID string, htmlContent string) string {
	// РС‰РµРј РІСЃРµ РІС…РѕР¶РґРµРЅРёСЏ uuid=file$XXXXX
	matches := naumenFileRegex.FindAllStringSubmatch(htmlContent, -1)
	if len(matches) == 0 {
		return htmlContent
	}

	processedHtml := htmlContent
	// РЎРѕР·РґР°РµРј РґРёСЂРµРєС‚РѕСЂРёСЋ РґР»СЏ Р·Р°СЏРІРєРё: ./storage/tickets/serviceCall$123/
	ticketDir := filepath.Join(s.cfg.TicketStoragePath, sdUUID)
	if err := os.MkdirAll(ticketDir, 0755); err != nil {
		s.logger.Error("Failed to create storage dir for ticket", "dir", ticketDir, "error", err)
		return htmlContent // Р’РѕР·РІСЂР°С‰Р°РµРј РєР°Рє РµСЃС‚СЊ, РµСЃР»Рё РЅРµ РјРѕР¶РµРј СЃРѕС…СЂР°РЅРёС‚СЊ
	}

	for _, match := range matches {
		// match[0] = "uuid=file$12345"
		// match[1] = "file$12345" (ID С„Р°Р№Р»Р°)
		fileUUID := match[1]

		// 1. РџСЂРѕРІРµСЂСЏРµРј, СЃРєР°С‡Р°РЅ Р»Рё С„Р°Р№Р»
		localFilePath := filepath.Join(ticketDir, fileUUID) // РЎРѕС…СЂР°РЅСЏРµРј Р±РµР· СЂР°СЃС€РёСЂРµРЅРёСЏ РёР»Рё РїС‹С‚Р°РµРјСЃСЏ СѓРіР°РґР°С‚СЊ
		// РџСЂРѕСЃС‚РѕР№ РІР°СЂРёР°РЅС‚: РёРјСЏ С„Р°Р№Р»Р° = UUID. Р‘СЂР°СѓР·РµСЂС‹ С‡Р°СЃС‚Рѕ СѓРјРµСЋС‚ РѕРїСЂРµРґРµР»СЏС‚СЊ С‚РёРї РїРѕ РєРѕРЅС‚РµРЅС‚Сѓ,
		// РЅРѕ Р»СѓС‡С€Рµ СЃРѕС…СЂР°РЅСЏС‚СЊ СЂР°СЃС€РёСЂРµРЅРёРµ. РџРѕРєР° СЃРѕС…СЂР°РЅСЏРµРј РєР°Рє РµСЃС‚СЊ.

		if _, err := os.Stat(localFilePath); os.IsNotExist(err) {
			// 2. Р¤Р°Р№Р»Р° РЅРµС‚ - СЃРєР°С‡РёРІР°РµРј
			err := s.downloadFileFromNaumen(fileUUID, localFilePath)
			if err != nil {
				s.logger.Error("Failed to download file from Naumen", "fileUUID", fileUUID, "error", err)
				continue // РџСЂРѕРїСѓСЃРєР°РµРј Р·Р°РјРµРЅСѓ, РµСЃР»Рё РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРєР°С‡Р°С‚СЊ
			}
		}

		// 3. Р—Р°РјРµРЅСЏРµРј СЃСЃС‹Р»РєСѓ РІ HTML
		// РСЃС…РѕРґРЅР°СЏ: ... src="./download?uuid=file$13205558" ...
		// Р¦РµР»РµРІР°СЏ:  ... src="/api/static/tickets/serviceCall$123/file$13205558" ...

		// РќР°С…РѕРґРёРј РїРѕР»РЅС‹Р№ РєСѓСЃРѕРє "./download?uuid=file$XXXX" Рё Р·Р°РјРµРЅСЏРµРј РµРіРѕ
		// Р РµРіСѓР»СЏСЂРєР° РёС‰РµС‚ С‚РѕР»СЊРєРѕ uuid=..., РїРѕСЌС‚РѕРјСѓ Р·Р°РјРµРЅРёРј РіСЂСѓР±Рѕ, РЅРѕ РЅР°РґРµР¶РЅРѕ РґР»СЏ Naumen:
		// "./download?uuid=" + fileUUID -> "/api/static/tickets/" + sdUUID + "/" + fileUUID

		oldLink := fmt.Sprintf("./download?uuid=%s", fileUUID)
		newLink := fmt.Sprintf("/api/static/tickets/%s/%s", sdUUID, fileUUID)
		processedHtml = strings.ReplaceAll(processedHtml, oldLink, newLink)

		// РќР° СЃР»СѓС‡Р°Р№, РµСЃР»Рё СЃСЃС‹Р»РєР° Р±РµР· С‚РѕС‡РєРё РІ РЅР°С‡Р°Р»Рµ (Р±С‹РІР°РµС‚ РїРѕ-СЂР°Р·РЅРѕРјСѓ)
		oldLink2 := fmt.Sprintf("/download?uuid=%s", fileUUID)
		processedHtml = strings.ReplaceAll(processedHtml, oldLink2, newLink)
	}

	return processedHtml
}

// downloadFileFromNaumen РІС‹РїРѕР»РЅСЏРµС‚ Р·Р°РїСЂРѕСЃ Рє API Naumen Рё СЃРѕС…СЂР°РЅСЏРµС‚ С„Р°Р№Р».
func (s *ticketServiceImpl) downloadFileFromNaumen(fileUUID, destPath string) error {
	// URL: <baseURL>/services/rest/get-file/file$123?accessKey=<accessKey>
	// Р‘Р°Р·РѕРІС‹Р№ URL РІ РєРѕРЅС„РёРіРµ РјРѕР¶РµС‚ Р±С‹С‚СЊ СЃ /sd РёР»Рё Р±РµР·, РЅСѓР¶РЅРѕ Р°РєРєСѓСЂР°С‚РЅРѕ СЃРѕР±СЂР°С‚СЊ.
	// РћР±С‹С‡РЅРѕ cfg.ServiceDeskBaseURL = "https://myhoreca.itsm365.com/sd"

	// РЈР±РёСЂР°РµРј trailing slash
	baseURL := strings.TrimRight(s.cfg.ServiceDeskBaseURL, "/")
	// Р¤РѕСЂРјРёСЂСѓРµРј URL РґР»СЏ СЃРєР°С‡РёРІР°РЅРёСЏ
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
