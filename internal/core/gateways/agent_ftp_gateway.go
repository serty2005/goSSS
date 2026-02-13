// internal/core/gateways/agent_ftp_gateway.go
package gateways

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AgentFTPGateway определяет интерфейс шлюза для получения данных от агентов через FTP.
// Реализации отвечают за периодическое чтение JSON-файлов с FTP-сервера,
// их парсинг, валидацию и отправку данных в ProcessingEngine.
type AgentFTPGateway interface {
	// Start запускает цикл опроса FTP-сервера.
	// Метод блокирующий, работает до отмены контекста.
	Start(ctx context.Context)
}

// agentFTPGatewayImpl реализует AgentFTPGateway.
// Отвечает за синхронизацию локального кэша с FTP, обработку файлов агентов
// и публикацию событий об изменениях в данных.
type agentFTPGatewayImpl struct {
	cfg       *config.Config                // Конфигурация приложения
	logger    logger.LoggerInterface        // Логгер
	db        *gorm.DB                      // Подключение к БД для хранения состояния файлов
	ftpClient services.FTPClient            // Клиент для работы с FTP
	obsSvc    services.AgentObservationService // Сервис применения наблюдений
}

// NewAgentFTPGateway создаёт новый экземпляр FTP-шлюза.
//
// Параметры:
//   - cfg: конфигурация с настройками FTP (путь, интервал опроса, путь к кэшу)
//   - logger: логгер для записи событий обработки
//   - db: база данных для хранения состояния обработанных файлов (таблица agent_files)
//   - ftpClient: клиент для подключения к FTP-серверу
//   - obsSvc: сервис для применения наблюдений (создание/обновление сущностей)
//
// Возвращает готовый к использованию шлюз.
func NewAgentFTPGateway(cfg *config.Config, logger logger.LoggerInterface, db *gorm.DB, ftpClient services.FTPClient, obsSvc services.AgentObservationService) AgentFTPGateway {
	return &agentFTPGatewayImpl{cfg, logger, db, ftpClient, obsSvc}
}

// Start запускает периодический опрос FTP-сервера для получения данных агентов.
//
// Метод работает в бесконечном цикле до отмены контекста:
//  1. Сразу выполняет первый цикл сверки (без ожидания таймера)
//  2. Затем периодически запускает циклы по интервалу из конфигурации
//
// Каждый цикл включает:
//   - Синхронизацию локального кэша с FTP
//   - Обработку всех JSON-файлов в кэше
//   - Публикацию событий для файлов с изменениями
//
// Параметры:
//   - ctx: контекст для graceful shutdown
func (g *agentFTPGatewayImpl) Start(ctx context.Context) {
	g.logger.Info("Запуск шлюза агентов (FTP)", "interval", g.cfg.AgentFTPInterval)
	ticker := time.NewTicker(g.cfg.AgentFTPInterval)
	defer ticker.Stop()

	// Запускаем первый раз сразу, но проверяем контекст перед этим
	if ctx.Err() == nil {
		g.logger.Debug("Выполняем первый цикл сверки сразу после запуска")
		g.runReconciliationCycle(ctx)
	}

	for {
		select {
		case <-ticker.C:
			g.logger.Debug("Сработал таймер, запуск очередного цикла сверки")
			g.runReconciliationCycle(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза агентов (FTP)", "reason", ctx.Err().Error())
			return
		}
	}
}

// isFileNameNumeric проверяет, состоит ли имя файла только из цифр (без расширения).
// Числовые имена файлов (например, "123456.json") используются для данных
// фискальных регистраторов, где имя файла = серийный номер ФР.
//
// Параметры:
//   - fileName: имя файла с расширением (например, "123456.json")
//
// Возвращает true, если имя файла (без .json) состоит только из цифр.
func isFileNameNumeric(fileName string) bool {
	// Убираем расширение .json
	nameWithoutExt := strings.TrimSuffix(fileName, ".json")
	// Проверяем, что оставшаяся часть состоит только из цифр
	for _, r := range nameWithoutExt {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(nameWithoutExt) > 0
}

// getFileTypeDescription возвращает человекочитаемое описание типа файла для логирования.
// Используется для различения файлов с данными ФР и файлов с данными серверов.
//
// Параметры:
//   - fileName: имя файла для определения типа
//
// Возвращает:
//   - "данные ФР" для числовых имён (серийные номера ФР)
//   - "данные по id/url сервера" для остальных файлов
func getFileTypeDescription(fileName string) string {
	if isFileNameNumeric(fileName) {
		return "данные ФР"
	}
	return "данные по id/url сервера"
}

// min возвращает минимальное из двух целых чисел.
// Используется для безопасного получения среза при превью содержимого файла.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isLocalAddress проверяет, является ли адрес локальным (недоступным извне).
// Такие адреса не могут использоваться для идентификации сервера в CMDB.
//
// Проверяемые диапазоны:
//   - localhost (127.0.0.1, 127.x.x.x)
//   - частные сети (10.x.x.x, 192.168.x.x, 172.16-31.x.x)
//   - link-local (169.254.x.x)
//
// Параметры:
//   - address: IP-адрес или hostname для проверки
//
// Возвращает true, если адрес является локальным/частным.
func isLocalAddress(address string) bool {
	// Проверяем localhost
	if strings.ToLower(address) == "localhost" || address == "127.0.0.1" || strings.HasPrefix(address, "127.") {
		return true
	}

	// Проверяем локальные сети
	localNetworks := []string{
		"10.", "192.168.", "172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
		"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
		"169.254.", // Link-local
	}

	for _, network := range localNetworks {
		if strings.HasPrefix(address, network) {
			return true
		}
	}

	return false
}

// validateAgentData проверяет корректность данных агента и наличие полезной информации.
// Валидация включает несколько этапов:
//  1. Проверка обязательных полей (hostname, url_rms)
//  2. Нормализация и валидация URL сервера
//  3. Проверка на локальные адреса (недоступные для идентификации)
//  4. Проверка наличия полезных данных (serial_number, crm_id, remote_ids)
//
// Параметры:
//   - data: DTO с данными агента для валидации
//   - log: логгер с контекстом для записи результатов валидации
//
// Возвращает:
//   - nil, если данные валидны
//   - error с описанием причины невалидности
//
// Возможные ошибки:
//   - "отсутствует обязательное поле hostname"
//   - "отсутствует обязательное поле url_rms"
//   - "некорректный формат URL сервера"
//   - "обнаружен локальный адрес сервера"
//   - "файл не содержит полезных данных для идентификации"
func validateAgentData(data *api.AgentDataDTO, log logger.LoggerInterface) error {
	// Проверка обязательных полей
	if data.Hostname == "" {
		return fmt.Errorf("отсутствует обязательное поле hostname")
	}

	if data.URLRms == "" {
		return fmt.Errorf("отсутствует обязательное поле url_rms")
	}

	// Валидация и нормализация URL сервера
	normalizedURL := validators.ValidateIPAddress(data.URLRms)
	if normalizedURL == nil {
		return fmt.Errorf("некорректный формат URL сервера: %s", data.URLRms)
	}

	// Проверка на локальные адреса
	if isLocalAddress(*normalizedURL) {
		return fmt.Errorf("обнаружен локальный адрес сервера, который не может быть использован для идентификации: %s", *normalizedURL)
	}

	// Проверка наличия полезных данных для идентификации
	hasUsefulData := false

	// Файл должен содержать либо серийный номер ФР, либо ID для идентификации
	if data.SerialNumber != "" {
		hasUsefulData = true
		log.Debug("Найден серийный номер ФР", "serial", data.SerialNumber)
	}

	if data.CRMID != "" {
		hasUsefulData = true
		log.Debug("Найден CRM ID", "crm_id", data.CRMID)
	}

	// Проверка ID удаленного доступа
	validRemoteIDs := 0
	if data.TeamviewerID != "" && data.TeamviewerID != "None" {
		validRemoteIDs++
		hasUsefulData = true
	}
	if data.LitemanagerID != "" && data.LitemanagerID != "None" {
		validRemoteIDs++
		hasUsefulData = true
	}
	if data.AnydeskID != "" && data.AnydeskID != "None" {
		validRemoteIDs++
		hasUsefulData = true
	}

	// Если файл не содержит полезных данных для идентификации, пропускаем его
	if !hasUsefulData {
		return fmt.Errorf("файл не содержит полезных данных для идентификации (нет серийного номера, CRM ID или ID удаленного доступа)")
	}

	// Проверка формата времени
	if data.CurrentTime != "" {
		if _, err := time.Parse("2006-01-02 15:04:05", data.CurrentTime); err != nil {
			log.Warn("Некорректный формат времени current_time", "time", data.CurrentTime, "error", err)
		}
	}

	log.Info("Валидация данных агента завершена успешно",
		"hostname", data.Hostname,
		"normalized_url", *normalizedURL,
		"has_serial", data.SerialNumber != "",
		"has_crm_id", data.CRMID != "",
		"remote_ids_count", validRemoteIDs,
		"has_useful_data", hasUsefulData)

	return nil
}

// syncLocalCacheWithFTP скачивает новые или обновленные файлы с FTP-сервера в локальный кэш.
//
// Алгоритм работы:
//  1. Получение списка файлов с FTP-сервера
//  2. Чтение списка уже скачанных файлов в локальном кэше
//  3. Для каждого файла на FTP:
//     - Пропускаем не-JSON файлы и пустые файлы
//     - Скачиваем, если файл новый или изменён (по mod_time)
//  4. Сохранение скачанных файлов с сохранением оригинального времени модификации
//
// Параметры:
//   - ctx: контекст для возможности прерывания операции
//
// Возвращает:
//   - nil при успешной синхронизации
//   - error при ошибке подключения к FTP или чтения кэша
//
// Примечание: Ошибки скачивания отдельных файлов логируются, но не прерывают синхронизацию.
func (s *agentFTPGatewayImpl) syncLocalCacheWithFTP(ctx context.Context) error {
	s.logger.Info("Синхронизация локального кэша с FTP...")
	syncStartTime := time.Now()

	// Проверка контекста перед началом операции
	select {
	case <-ctx.Done():
		s.logger.Debug("Синхронизация прервана: контекст отменён до начала")
		return ctx.Err()
	default:
	}

	ftpFiles, err := s.ftpClient.ListFiles(s.cfg.FTPPath)
	if err != nil {
		s.logger.Error("Не удалось получить список файлов с FTP", "error", err, "ftp_path", s.cfg.FTPPath)
		return fmt.Errorf("не удалось получить список файлов с FTP: %w", err)
	}
	s.logger.Debug("Получен список файлов с FTP", "count", len(ftpFiles), "ftp_path", s.cfg.FTPPath)

	localFileInfos := make(map[string]os.FileInfo)
	cachedFiles, err := os.ReadDir(s.cfg.FTPCachePath)
	if err != nil {
		s.logger.Error("Не удалось прочитать кэш-директорию", "error", err, "cache_path", s.cfg.FTPCachePath)
		return fmt.Errorf("не удалось прочитать кэш-директорию: %w", err)
	}

	for _, f := range cachedFiles {
		if info, err := f.Info(); err == nil {
			localFileInfos[f.Name()] = info
		}
	}
	s.logger.Debug("Прочитан локальный кэш", "cached_files", len(localFileInfos), "cache_path", s.cfg.FTPCachePath)

	downloadedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, ftpFile := range ftpFiles {
		// Проверка отмены контекста внутри цикла скачивания
		select {
		case <-ctx.Done():
			s.logger.Info("Прерывание синхронизации FTP (получен сигнал остановки)",
				"downloaded", downloadedCount,
				"skipped", skippedCount,
				"errors", errorCount)
			return ctx.Err()
		default:
		}

		// Пропускаем не-JSON файлы, директории и пустые файлы
		if ftpFile.Type != ftp.EntryTypeFile || !strings.HasSuffix(strings.ToLower(ftpFile.Name), ".json") || ftpFile.Size == 0 {
			skippedCount++
			s.logger.Debug("Пропуск файла: не JSON или пустой",
				"file", ftpFile.Name,
				"type", ftpFile.Type,
				"size", ftpFile.Size)
			continue
		}

		localInfo, found := localFileInfos[ftpFile.Name]
		if !found || ftpFile.Time.After(localInfo.ModTime()) {
			s.logger.Info("Обнаружен новый/обновленный файл, скачивание...",
				"file", ftpFile.Name,
				"size", ftpFile.Size,
				"ftp_time", ftpFile.Time.Format(time.RFC3339),
				"is_new", !found)

			ftpFilePath := path.Join(s.cfg.FTPPath, ftpFile.Name)
			fileData, err := s.ftpClient.DownloadFile(ftpFilePath)
			if err != nil {
				s.logger.Error("Не удалось скачать файл", "file", ftpFile.Name, "error", err)
				errorCount++
				continue
			}

			localFilePath := filepath.Join(s.cfg.FTPCachePath, ftpFile.Name)
			if err := os.WriteFile(localFilePath, fileData, 0644); err != nil {
				s.logger.Error("Не удалось сохранить файл в кэш", "file", localFilePath, "error", err)
				errorCount++
				continue
			}

			// Сохраняем оригинальное время модификации для идемпотентной обработки
			os.Chtimes(localFilePath, ftpFile.Time, ftpFile.Time)
			downloadedCount++

			s.logger.Debug("Файл успешно скачан в кэш",
				"file", ftpFile.Name,
				"local_path", localFilePath,
				"size", len(fileData))
		} else {
			skippedCount++
			s.logger.Debug("Файл не изменился, пропуск",
				"file", ftpFile.Name,
				"local_mod_time", localInfo.ModTime().Format(time.RFC3339),
				"ftp_mod_time", ftpFile.Time.Format(time.RFC3339))
		}
	}

	syncDuration := time.Since(syncStartTime)
	s.logger.Info("Синхронизация локального кэша завершена",
		"downloaded", downloadedCount,
		"skipped", skippedCount,
		"errors", errorCount,
		"duration", syncDuration)
	return nil
}

// runReconciliationCycle выполняет один цикл сверки данных с FTP.
//
// Цикл включает:
//  1. Синхронизацию локального кэша с FTP-сервером
//  2. Обход всех JSON-файлов в кэше
//  3. Обработку каждого файла (парсинг, валидация, публикация события)
//
// Параметры:
//   - ctx: контекст для возможности прерывания цикла
//
// Логирует статистику цикла: количество обработанных файлов,
// опубликованных событий, пропущенных файлов и длительность.
func (g *agentFTPGatewayImpl) runReconciliationCycle(ctx context.Context) {
	cycleStartTime := time.Now()
	g.logger.Info("Начало нового цикла сверки данных с FTP...")

	// Шаг 1: Синхронизация локального кэша с FTP
	if err := g.syncLocalCacheWithFTP(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			g.logger.Info("Цикл FTP прерван из-за остановки приложения")
			return
		}
		g.logger.Error("Ошибка синхронизации кэша с FTP, цикл прерван", "error", err)
		return
	}

	// Шаг 2: Получение списка файлов для обработки
	localFiles, err := os.ReadDir(g.cfg.FTPCachePath)
	if err != nil {
		g.logger.Error("Не удалось прочитать директорию с кэшем, цикл прерван", "error", err)
		return
	}
	g.logger.Debug("Прочитана директория кэша", "total_entries", len(localFiles))

	// Шаг 3: Обработка каждого файла
	publishedEvents := 0
	processedFiles := 0
	skippedFiles := 0
	frFiles := 0
	serverFiles := 0
	totalFileSize := int64(0)

	for _, file := range localFiles {
		// Пропускаем директории и не-JSON файлы
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}

		// Классификация файла по типу
		if isFileNameNumeric(file.Name()) {
			frFiles++
		} else {
			serverFiles++
		}

		if info, err := file.Info(); err == nil {
			totalFileSize += info.Size()
		}

		// Проверка отмены контекста
		select {
		case <-ctx.Done():
			g.logger.Info("Цикл обработки файлов прерван из-за остановки приложения",
				"processed", processedFiles,
				"published", publishedEvents)
			return
		default:
			processedFiles++
			if g.processFile(ctx, file.Name()) {
				publishedEvents++
			} else {
				skippedFiles++
			}
		}
	}

	// Логирование статистики цикла
	cycleDuration := time.Since(cycleStartTime)
	avgFileSize := int64(0)
	if processedFiles > 0 {
		avgFileSize = totalFileSize / int64(processedFiles)
	}

	g.logger.Info("Цикл сверки данных с FTP завершен",
		"published_events", publishedEvents,
		"processed_files", processedFiles,
		"skipped_files", skippedFiles,
		"fr_files", frFiles,
		"server_files", serverFiles,
		"total_file_size", totalFileSize,
		"avg_file_size", avgFileSize,
		"cycle_duration", cycleDuration)
}

// processFile обрабатывает один JSON-файл из локального кэша.
//
// Алгоритм обработки:
//  1. Проверка файла на изменения (mod_time, size) - идемпотентность
//  2. Чтение и парсинг JSON в AgentDataDTO
//  3. Валидация данных (обязательные поля, полезные данные)
//  4. Проверка изменения иерархии (FR serial, RMS URL)
//  5. Обновление состояния файла в БД
//  6. Применение наблюдения через AgentObservationService (если есть изменения)
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - fileName: имя файла для обработки (без пути)
//
// Возвращает:
//   - true: событие опубликовано (данные изменились и применены)
//   - false: файл пропущен (без изменений, ошибка или невалидные данные)
//
// Файл пропускается в следующих случаях:
//   - Файл не изменился с последней обработки (mod_time и size совпадают)
//   - Пустой файл или слишком маленький для валидных данных
//   - Ошибка парсинга JSON
//   - Невалидные данные (нет hostname, url_rms или полезных идентификаторов)
//   - Иерархия объектов не изменилась (те же FR serial и RMS URL)
func (g *agentFTPGatewayImpl) processFile(ctx context.Context, fileName string) bool {
	startTime := time.Now()
	fileType := getFileTypeDescription(fileName)
	log := g.logger.With(
		"request_id", fileName,
		"file_type", fileType,
		"is_fr_data", isFileNameNumeric(fileName),
	)

	log.Debug("Начало обработки файла агента",
		"file", fileName,
		"file_type", fileType)

	localFilePath := filepath.Join(g.cfg.FTPCachePath, fileName)

	// Шаг 1: Получение информации о файле
	fileInfo, err := os.Stat(localFilePath)
	if err != nil {
		log.Error("Не удалось получить информацию о файле в кэше", "error", err, "path", localFilePath)
		return false
	}
	log.Debug("Информация о файле получена",
		"size", fileInfo.Size(),
		"mod_time", fileInfo.ModTime().Format(time.RFC3339))

	// Шаг 2: Получение предыдущего состояния файла из БД
	var previousState models.AgentFile
	err = g.db.WithContext(ctx).First(&previousState, "file_name = ?", fileName).Error
	isNewRecordInDB := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNewRecordInDB {
		log.Error("Ошибка получения состояния файла из БД", "error", err)
		return false
	}

	// Шаг 3: Проверка на необходимость обработки (идемпотентность)
	if !isNewRecordInDB && previousState.LastProcessedModTime.Equal(fileInfo.ModTime()) && previousState.LastProcessedFileSize == fileInfo.Size() {
		log.Debug("Файл не изменился с последней обработки, пропуск",
			"last_mod_time", previousState.LastProcessedModTime.Format(time.RFC3339),
			"last_size", previousState.LastProcessedFileSize)
		return false
	}

	log.Debug("Файл требует обработки",
		"is_new_file", isNewRecordInDB,
		"prev_mod_time", previousState.LastProcessedModTime.Format(time.RFC3339),
		"current_mod_time", fileInfo.ModTime().Format(time.RFC3339))

	// Шаг 4: Чтение содержимого файла
	fileData, err := os.ReadFile(localFilePath)
	if err != nil {
		log.Error("Не удалось прочитать файл из кэша", "error", err)
		return false
	}

	// Проверка на пустой файл
	if len(fileData) == 0 {
		log.Warn("Файл пустой, пропускаем обработку")
		return false
	}

	// Проверка на минимальный размер (JSON должен содержать хотя бы базовую структуру)
	if len(fileData) < 50 {
		log.Warn("Файл слишком маленький для корректных данных, пропускаем обработку",
			"file_size", len(fileData))
		return false
	}

	log.Debug("Файл прочитан", "file_size", len(fileData))

	// Шаг 5: Парсинг JSON
	var data api.AgentDataDTO
	if err := json.Unmarshal(fileData, &data); err != nil {
		log.Error("Не удалось распарсить JSON, файл будет пропущен до следующего изменения",
			"error", err,
			"file_size", len(fileData),
			"file_content_preview", string(fileData[:min(200, len(fileData))]))
		return false
	}
	log.Info("JSON успешно распарсен",
		"RMS_url", data.URLRms,
		"FR_serial", data.SerialNumber,
		"hostname", data.Hostname,
		"crm_id", data.CRMID)

	// Шаг 6: Валидация данных агента
	if err := validateAgentData(&data, log); err != nil {
		log.Error("Валидация данных агента не пройдена, файл будет пропущен", "error", err)
		return false
	}
	log.Info("Валидация данных агента пройдена успешно")

	// Шаг 7: Проверка изменения иерархии объектов
	currentFRSerial := data.SerialNumber
	currentRMSUrl := data.URLRms

	hierarchyHasChanged := isNewRecordInDB ||
		(utils.SafeStringDereference(previousState.LastSeenFRSerial) != currentFRSerial) ||
		(utils.SafeStringDereference(previousState.LastSeenRMSUrl) != currentRMSUrl)

	if hierarchyHasChanged {
		log.Info("Обнаружено изменение в иерархии объектов",
			"is_new_file", isNewRecordInDB,
			"prev_fr_serial", utils.SafeStringDereference(previousState.LastSeenFRSerial),
			"current_fr_serial", currentFRSerial,
			"prev_rms_url", utils.SafeStringDereference(previousState.LastSeenRMSUrl),
			"current_rms_url", currentRMSUrl)
	} else {
		log.Debug("Иерархия объектов не изменилась")
	}

	// Шаг 8: Сохранение нового состояния файла
	newState := models.AgentFile{
		FileName:              fileName,
		LastProcessedModTime:  fileInfo.ModTime(),
		LastProcessedFileSize: fileInfo.Size(),
	}
	if currentFRSerial != "" {
		newState.LastSeenFRSerial = &currentFRSerial
	}
	if currentRMSUrl != "" {
		newState.LastSeenRMSUrl = &currentRMSUrl
	}

	err = g.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_processed_mod_time", "last_processed_file_size", "last_seen_fr_serial", "last_seen_rms_url", "updated_at"}),
	}).Create(&newState).Error

	if err != nil {
		log.Error("Не удалось обновить статус файла в БД", "error", err)
		return false
	}
	log.Debug("Состояние файла обновлено в БД")

	// Шаг 9: Применение наблюдения (если есть изменения)
	if hierarchyHasChanged {
		log.Info("Применение наблюдения для изменённых данных...")
		if g.obsSvc == nil {
			log.Error("Сервис применения наблюдений не инициализирован")
			return false
		}
		if _, err := g.obsSvc.ApplyObservation(ctx, fileName, &data); err != nil {
			log.Error("Ошибка применения наблюдения", "error", err)
			return false
		}
		processingTime := time.Since(startTime)
		log.Info("Наблюдение успешно применено",
			"processing_time", processingTime,
			"file_size", fileInfo.Size())
		return true
	}

	log.Debug("Файл обновлен, но иерархия объектов не изменилась. Событие не публикуется.")
	processingTime := time.Since(startTime)
	log.Info("Обработка файла завершена успешно",
		"event_published", false,
		"processing_time", processingTime)
	return false
}
