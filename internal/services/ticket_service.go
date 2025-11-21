package services

import (
	"context"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Regex для поиска UUID файлов в ссылках Naumen (./download?uuid=file$123...)
var naumenFileRegex = regexp.MustCompile(`uuid=(file\$[0-9]+)`)

type TicketService interface {
	List(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, int64, error)
	GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error)
	LinkToAsset(ctx context.Context, ticketID string, assetID string, assetType string) error
}

type ticketServiceImpl struct {
	logger     logger.LoggerInterface
	ticketRepo tickets.TicketRepository
	sdClient   external.ExternalSystemClient
	cfg        *config.Config
	// Добавляем репозитории для валидации
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

func NewTicketService(
	logger logger.LoggerInterface,
	ticketRepo tickets.TicketRepository,
	sdClient external.ExternalSystemClient,
	cfg *config.Config,
	// Добавляем аргументы
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) TicketService {
	return &ticketServiceImpl{
		logger:          logger,
		ticketRepo:      ticketRepo,
		sdClient:        sdClient,
		cfg:             cfg,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

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

func (s *ticketServiceImpl) GetDetails(ctx context.Context, ticketID string) (*tickets.TicketDetails, error) {
	// 1. Метаданные из БД
	ticket, err := s.ticketRepo.GetByID(ctx, ticketID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ticket metadata: %w", err)
	}
	if ticket == nil {
		return nil, nil
	}

	details := &tickets.TicketDetails{
		Metadata: *ticket,
		Comments: make([]tickets.Comment, 0),
	}

	// 2. Получаем HTML описание из Naumen
	sdData, err := s.sdClient.FetchEntityDetails(ctx, ticket.ServiceDeskUUID, "Ticket")
	if err != nil {
		s.logger.Error("Failed to fetch ticket details from SD", "uuid", ticket.ServiceDeskUUID, "error", err)
		details.DescriptionRTF = "<div>Не удалось загрузить описание из внешней системы.</div>"
	} else {
		if desc, ok := sdData["descriptionRTF"].(string); ok {
			// Обрабатываем картинки: скачиваем и заменяем ссылки
			details.DescriptionRTF = s.processHtmlContent(ticket.ServiceDeskUUID, desc)
		}
	}

	// 3. Получаем комментарии и обрабатываем их
	rawComments, err := s.sdClient.FetchComments(ctx, ticket.ServiceDeskUUID)
	if err != nil {
		s.logger.Error("Failed to fetch comments from SD", "uuid", ticket.ServiceDeskUUID, "error", err)
	} else {
		for _, rawC := range rawComments {
			comment, err := s.sdClient.Mapper().DataToComment(rawC)
			if err == nil {
				// Обрабатываем картинки в комментариях
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
