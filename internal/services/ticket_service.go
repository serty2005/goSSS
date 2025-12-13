package services

import (
	"context"
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
	GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error)

	// Действия
	CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error)
	ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error)
	Assign(ctx context.Context, ticketID string, assigneeID *uint, actorID uint) (*tickets.Ticket, error)
	LinkToAsset(ctx context.Context, ticketID string, assetID string, assetType string) error
}

type ticketServiceImpl struct {
	logger          logger.LoggerInterface
	ticketRepo      tickets.TicketRepository
	userRepo        user.Repository
	sdClient        external.ExternalSystemClient
	cfg             *config.Config
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

func NewTicketService(
	logger logger.LoggerInterface,
	ticketRepo tickets.TicketRepository,
	userRepo user.Repository,
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
	count, err := s.ticketRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("service: count tickets: %w", err)
	}
	return items, count, nil
}

// CreateInternal создает внутренний тикет.
func (s *ticketServiceImpl) CreateInternal(ctx context.Context, dto api.TicketCreateInternalDTO, authorID uint) (*tickets.Ticket, error) {
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
	s.recordHistory(ctx, ticket.ID, &authorID, "status", "", tickets.StatusNew)

	return ticket, nil
}

// ChangeStatus меняет статус тикета и пишет историю.
func (s *ticketServiceImpl) ChangeStatus(ctx context.Context, ticketID string, status string, comment string, userID uint) (*tickets.Ticket, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, fmt.Errorf("ticket not found")
	}

	oldStatus := ticket.Status
	if oldStatus == status {
		return ticket, nil // Статус не изменился
	}

	ticket.Status = status
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	// Запись в историю
	s.recordHistory(ctx, ticket.ID, &userID, "status", oldStatus, status)

	// Если есть комментарий, добавляем его как историю или отдельно
	if comment != "" {
		// Для простоты используем легаси структуру Comment, если фронт её ждет,
		// но лучше писать в History с полем "comment"
		// Реализуем через History как "comment_added"
		s.recordHistory(ctx, ticket.ID, &userID, "comment", "", comment)
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
		return nil, fmt.Errorf("ticket not found")
	}

	var oldAssigneeName, newAssigneeName string

	// Получаем имена для истории
	if ticket.Assignee != nil {
		oldAssigneeName = ticket.Assignee.FullName
	} else {
		oldAssigneeName = "Unassigned"
	}

	if assigneeID != nil {
		newAssignee, err := s.userRepo.GetByID(ctx, *assigneeID)
		if err != nil || newAssignee == nil {
			return nil, fmt.Errorf("assignee user not found")
		}
		newAssigneeName = newAssignee.FullName
	} else {
		newAssigneeName = "Unassigned"
	}

	ticket.AssigneeID = assigneeID
	// Если назначаем, переводим в InProgress, если он был New
	if assigneeID != nil && ticket.Status == tickets.StatusNew {
		ticket.Status = tickets.StatusInProgress
	}

	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return nil, err
	}

	s.recordHistory(ctx, ticket.ID, &actorID, "assignee", oldAssigneeName, newAssigneeName)
	return ticket, nil
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

	// Попытка получить данные из SD для легаси тикетов
	if ticket.ServiceDeskUUID != "" {
		sdData, err := s.sdClient.FetchEntityDetails(ctx, ticket.ServiceDeskUUID, "Ticket")
		if err == nil {
			if desc, ok := sdData["descriptionRTF"].(string); ok {
				// В идеале description должен быть в БД, но для легаси берем из SD
				if ticket.Description == "" {
					details.Metadata.Description = s.processHtmlContent(ticket.ServiceDeskUUID, desc)
				}
			}
		}

		// Комментарии из SD
		rawComments, _ := s.sdClient.FetchComments(ctx, ticket.ServiceDeskUUID)
		for _, rawC := range rawComments {
			comment, err := s.sdClient.Mapper().DataToComment(rawC)
			if err == nil {
				comment.Text = s.processHtmlContent(ticket.ServiceDeskUUID, comment.Text)
				details.Comments = append(details.Comments, *comment)
			}
		}
	}

	return details, nil
}

func (s *ticketServiceImpl) LinkToAsset(ctx context.Context, ticketID string, assetID string, assetType string) error {
	// 1. Получаем заявку, чтобы узнать, какой компании она принадлежит
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return fmt.Errorf("failed to get ticket: %w", err)
	}
	if ticket == nil {
		return fmt.Errorf("ticket not found")
	}

	// 2. Проверяем существование актива и совпадение владельца
	var assetOwnerID string

	switch assetType {
	case tickets.AssetTypeServer:
		asset, err := s.serverRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("server not found")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	case tickets.AssetTypeFiscalRegister:
		asset, err := s.frRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("fiscal register not found")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	case tickets.AssetTypeWorkstation:
		asset, err := s.workstationRepo.GetByID(ctx, assetID)
		if err != nil || asset == nil {
			return fmt.Errorf("workstation not found")
		}
		assetOwnerID = utils.SafeStringDereference(asset.OwnerID)

	default:
		return fmt.Errorf("unsupported asset type: %s", assetType)
	}

	// 3. Сравниваем владельцев
	// Если у оборудования нет владельца (пустая строка), считаем это риском, но разрешаем (или запрещаем, зависит от бизнес-логики).
	// В данном случае запретим привязку к "чужому" оборудованию.
	if assetOwnerID != "" && assetOwnerID != ticket.CompanyID {
		return fmt.Errorf("conflict: asset belongs to company %s, but ticket belongs to %s", assetOwnerID, ticket.CompanyID)
	}

	// 4. Сохраняем привязку
	return s.ticketRepo.AssociateAsset(ctx, ticketID, assetID, assetType)
}

// recordHistory - вспомогательный метод для записи аудита.
func (s *ticketServiceImpl) recordHistory(ctx context.Context, ticketID string, userID *uint, field, oldVal, newVal string) {
	h := &tickets.TicketHistory{
		TicketID:  ticketID,
		UserID:    userID,
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
		// Целевая:  ... src="/static/tickets/serviceCall$123/file$13205558" ...

		// Находим полный кусок "./download?uuid=file$XXXX" и заменяем его
		// Регулярка ищет только uuid=..., поэтому заменим грубо, но надежно для Naumen:
		// "./download?uuid=" + fileUUID -> "/static/tickets/" + sdUUID + "/" + fileUUID

		oldLink := fmt.Sprintf("./download?uuid=%s", fileUUID)
		newLink := fmt.Sprintf("/static/tickets/%s/%s", sdUUID, fileUUID)
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
