package services

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Regex для поиска UUID файлов в ссылках Naumen (./download?uuid=file$123...)
var naumenFileRegex = regexp.MustCompile(`uuid=(file\$[0-9]+)`)

type TicketService interface {
	// Чтение
	List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error)
	GetLastComments(ctx context.Context, ticketIDs []string) (map[string]tickets.LastCommentInfo, error)
	GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error)
	GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error)

	// Действия
	CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error)
	ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error)
	AddComment(ctx context.Context, ticketID string, comment string, userID uint) error
	RecordConnectionCopy(ctx context.Context, ticketID string, label string, value string, userID uint) error
	UpdateDescription(ctx context.Context, ticketID string, description string, userID uint) (*tickets.Ticket, error)
	RefreshCommentsFromServiceDesk(ctx context.Context, ticketID string) (int, error)
	Assign(ctx context.Context, ticketID string, assigneeID *uint, actorID uint) (*tickets.Ticket, error)
	ChangeCompany(ctx context.Context, ticketID string, companyID string, actorID uint) (*tickets.Ticket, error)
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
	}
}

// List возвращает список заявок с фильтрацией.
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

// CreateInternal создает внутренний тикет.
func (s *ticketServiceImpl) CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error) {
	author, err := s.userRepo.GetByID(ctx, authorID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить автора тикета: %w", err)
	}
	if author == nil {
		return nil, ErrReporterNotFound
	}

	ownerCompany, err := s.companyRepo.GetByID(ctx, dto.CompanyID)
	if err != nil {
		return nil, err
	}

	ticket := &tickets.Ticket{
		Subject:     dto.Subject,
		Description: dto.Description,
		Priority:    dto.Priority,
		Type:        dto.Type,
		Status:      tickets.StatusNew,
		CompanyID:   dto.CompanyID,
		ReporterID:  &authorID,
		AssetID:     dto.AssetID,
		AssetType:   dto.AssetType,
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

	// Запись в историю
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

// ChangeStatus меняет статус тикета и пишет историю.
func (s *ticketServiceImpl) ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("заявка не найдена")
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
			s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldResult, oldResult, ticket.Result)
		}
	}

	ticket.Status = status
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	// Запись в историю
	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldStatus, oldStatus, status)

	// Если есть комментарий, добавляем его как историю или отдельно
	if comment != "" {
		// Для простоты используем легаси структуру Comment, если фронт её ждет,
		// но лучше писать в History с полем "comment"
		// Реализуем через History как "comment_added"
		s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, "", comment)
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

	s.recordHistory(ctx, ticket.ID, &actorID, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAssignee, oldAssigneeName, newAssigneeName)
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
		ticket.ContractID = nil
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

func (s *ticketServiceImpl) AddComment(ctx context.Context, ticketID string, comment string, userID uint) error {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return err
	}
	if ticket == nil {
		return fmt.Errorf("заявка не найдена")
	}

	text := strings.TrimSpace(comment)
	if text == "" {
		return fmt.Errorf("комментарий пустой")
	}

	authorName := "Сотрудник"
	u, err := s.userRepo.GetByID(ctx, userID)
	if err == nil && u != nil && strings.TrimSpace(u.FullName) != "" {
		authorName = strings.TrimSpace(u.FullName)
	}

	if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{
		{
			TicketID:     ticket.ID,
			Text:         text,
			AuthorName:   authorName,
			CreationDate: time.Now(),
			IsInternal:   false,
		},
	}); err != nil {
		return err
	}

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionCommentAdded, tickets.HistoryFieldComment, "", text)
	return nil
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

	s.recordHistory(ctx, ticket.ID, &userID, tickets.HistoryActionConnectionCopied, tickets.HistoryFieldConnection, "", line+": "+val)
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

	s.recordHistory(ctx, ticketID, nil, tickets.HistoryActionFieldChanged, tickets.HistoryFieldAsset, oldAsset, newAsset)
	return nil
}

// recordHistory - вспомогательный метод для записи аудита.
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

// processHtmlContent ищет ссылки на файлы Naumen, скачивает их и заменяет на локальные URL.
// sdUUID - внешний UUID заявки (например, serviceCall$123), используется для группировки файлов в папке.
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
		// Исходная: ... src="./download?uuid=file$13205558" ...
		// Целевая:  ... src="/api/static/tickets/serviceCall$123/file$13205558" ...

		// Находим полный кусок "./download?uuid=file$XXXX" и заменяем его
		// Регулярка ищет только uuid=..., поэтому заменим грубо, но надежно для Naumen:
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
