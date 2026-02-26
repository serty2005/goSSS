// internal/core/gateways/agent_ftp_gateway.go
package gateways

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
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

	// InitializeDBFromCache инициализирует записи в БД из существующих файлов кэша.
	// Используется при запуске с флагом --seed для восстановления состояния БД
	// из ранее скачанных файлов без обращения к FTP-серверу.
	InitializeDBFromCache(ctx context.Context) error

	// LoadAgentDataFromCache загружает данные агентов из локального кэша
	// без обращения к FTP-серверу. Возвращает количество успешно обработанных файлов.
	LoadAgentDataFromCache(ctx context.Context) (int, error)
}

// agentFTPGatewayImpl реализует AgentFTPGateway.
// Отвечает за синхронизацию локального кэша с FTP, обработку файлов агентов
// и публикацию событий об изменениях в данных.
type agentFTPGatewayImpl struct {
	cfg       *config.Config                   // Конфигурация приложения
	logger    logger.LoggerInterface           // Логгер
	db        *gorm.DB                         // Подключение к БД для хранения состояния файлов
	ftpClient services.FTPClient               // Клиент для работы с FTP
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
	if data.RustdeskID != "" && data.RustdeskID != "None" {
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
//  1. Получение списка файлов с FTP-сервера через LIST (время модификации уже в листинге)
//  2. Сравнение с сохранённым LastCheckedModTime в БД
//  3. Параллельное скачивание только изменившихся файлов (3 воркера)
//  4. Обновление LastCheckedModTime для всех проверенных файлов
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

	// Шаг 1: Получение списка файлов с FTP
	// Время модификации уже приходит в листинге LIST, MDTM запросы не нужны
	ftpFiles, err := s.ftpClient.ListFiles(s.cfg.FTPPath)
	if err != nil {
		s.logger.Error("Не удалось получить список файлов с FTP", "error", err, "ftp_path", s.cfg.FTPPath)
		return fmt.Errorf("не удалось получить список файлов с FTP: %w", err)
	}
	s.logger.Debug("Получен список файлов с FTP", "count", len(ftpFiles), "ftp_path", s.cfg.FTPPath)

	// Шаг 2: Фильтрация и проверка файлов
	var filesToDownload []downloadTask
	var filesChecked []struct {
		fileName string
		modTime  time.Time
	}
	skippedCount := 0

	for _, ftpFile := range ftpFiles {
		// Проверка отмены контекста
		select {
		case <-ctx.Done():
			s.logger.Info("Прерывание синхронизации FTP (получен сигнал остановки)")
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

		// Используем время модификации из LIST
		// Точность до минуты достаточна для определения изменений
		modTime := ftpFile.Time

		// Проверяем, нужно ли скачивать файл
		if s.checkFileNeedsDownload(ftpFile.Name, modTime, int64(ftpFile.Size)) {
			s.logger.Debug("Файл требует скачивания",
				"file", ftpFile.Name,
				"size", ftpFile.Size,
				"mod_time", modTime.Format(time.RFC3339))

			filesToDownload = append(filesToDownload, downloadTask{
				remotePath: path.Join(s.cfg.FTPPath, ftpFile.Name),
				localPath:  filepath.Join(s.cfg.FTPCachePath, ftpFile.Name),
				fileName:   ftpFile.Name,
				modTime:    modTime,
			})
		} else {
			skippedCount++
			s.logger.Debug("Файл не изменился, пропуск",
				"file", ftpFile.Name,
				"mod_time", modTime.Format(time.RFC3339))
		}

		// Сохраняем информацию о проверенном файле для обновления БД
		filesChecked = append(filesChecked, struct {
			fileName string
			modTime  time.Time
		}{ftpFile.Name, modTime})
	}

	s.logger.Info("Проверка файлов завершена",
		"total_files", len(ftpFiles),
		"files_to_download", len(filesToDownload),
		"skipped", skippedCount)

	// Шаг 4: Параллельное скачивание файлов
	downloadedCount := 0
	errorCount := 0

	if len(filesToDownload) > 0 {
		if err := s.parallelDownload(ctx, filesToDownload); err != nil {
			s.logger.Error("Ошибки при параллельном скачивании", "error", err)
			// Подсчитываем количество ошибок из агрегированного сообщения
			errorCount = strings.Count(err.Error(), "ошибка скачивания")
			downloadedCount = len(filesToDownload) - errorCount
		} else {
			downloadedCount = len(filesToDownload)
		}
	}

	// Шаг 5: Обновление LastCheckedModTime для всех проверенных файлов
	for _, checked := range filesChecked {
		if err := s.updateLastCheckedModTime(checked.fileName, checked.modTime); err != nil {
			s.logger.Warn("Не удалось обновить LastCheckedModTime",
				"file", checked.fileName,
				"error", err)
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

// updateLastCheckedModTime обновляет время последней проверки модификации файла.
// Создаёт новую запись, если файл ещё не зарегистрирован в БД.
//
// Параметры:
//   - fileName: имя файла
//   - modTime: время модификации на FTP
//
// Возвращает ошибку при неудачном обновлении БД.
func (s *agentFTPGatewayImpl) updateLastCheckedModTime(fileName string, modTime time.Time) error {
	// Используем upsert для создания или обновления записи
	state := models.AgentFile{
		FileName:           fileName,
		LastCheckedModTime: &modTime,
	}

	result := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_checked_mod_time", "updated_at"}),
	}).Create(&state)

	return result.Error
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
// Транспортная функция: отвечает только за чтение файла и передачу данных в ApplyObservation.
// Бизнес-валидация выполняется внутри ApplyObservation, а не в шлюзе.
//
// Алгоритм обработки:
//  1. Проверка файла на изменения (mod_time, size) - быстрая идемпотентность
//  2. Чтение содержимого файла и вычисление payload_hash
//  3. Проверка изменения payload_hash - точная идемпотентность
//  4. Парсинг JSON в AgentDataDTO
//  5. Обновление состояния файла в БД
//  6. Передача данных в ApplyObservation (если payload изменился)
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - fileName: имя файла для обработки (без пути)
//
// Возвращает:
//   - true: данные переданы в ApplyObservation
//   - false: файл пропущен (без изменений или ошибка чтения)
//
// Файл пропускается в следующих случаях:
//   - Файл не изменился с последней обработки (mod_time, size и payload_hash совпадают)
//   - Пустой файл или слишком маленький для валидных данных
//   - Ошибка парсинга JSON
func (g *agentFTPGatewayImpl) processFile(ctx context.Context, fileName string) bool {
	startTime := time.Now()
	fileType := getFileTypeDescription(fileName)
	// Генерируем trace_id в начале обработки файла
	traceID := uuid.New().String()

	log := g.logger.With(
		"trace_id", traceID,
		"source", fileName,
		"operation", "process_file",
		"file_type", fileType,
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

	// Шаг 3: Быстрая проверка на необходимость обработки (по mod_time и size)
	if !isNewRecordInDB && previousState.LastProcessedModTime.Equal(fileInfo.ModTime()) && previousState.LastProcessedFileSize == fileInfo.Size() {
		log.Debug("Файл не изменился с последней обработки (mod_time и size совпадают), пропуск",
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

	// Шаг 5: Вычисление payload_hash для точной идемпотентности
	currentPayloadHash := computePayloadHash(fileData)
	log.Debug("Вычислен payload_hash", "hash", currentPayloadHash)

	// Проверка: если payload_hash не изменился, пропускаем обработку
	if !isNewRecordInDB && previousState.PayloadHash == currentPayloadHash {
		log.Debug("Payload не изменился (hash совпадает), пропуск обработки",
			"payload_hash", currentPayloadHash)
		return false
	}

	log.Info("Обнаружен изменённый payload",
		"is_new_file", isNewRecordInDB,
		"prev_hash", previousState.PayloadHash,
		"current_hash", currentPayloadHash)

	// Шаг 6: Парсинг JSON
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

	// Шаг 7: Сохранение нового состояния файла (включая payload_hash)
	currentFRSerial := data.SerialNumber
	currentRMSUrl := data.URLRms

	newState := models.AgentFile{
		FileName:              fileName,
		LastProcessedModTime:  fileInfo.ModTime(),
		LastProcessedFileSize: fileInfo.Size(),
		PayloadHash:           currentPayloadHash,
	}
	if currentFRSerial != "" {
		newState.LastSeenFRSerial = &currentFRSerial
	}
	if currentRMSUrl != "" {
		newState.LastSeenRMSUrl = &currentRMSUrl
	}

	err = g.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_processed_mod_time", "last_processed_file_size", "payload_hash", "last_seen_fr_serial", "last_seen_rms_url", "updated_at"}),
	}).Create(&newState).Error

	if err != nil {
		log.Error("Не удалось обновить статус файла в БД", "error", err)
		return false
	}
	log.Debug("Состояние файла обновлено в БД")

	// Шаг 8: Передача данных в ApplyObservation
	// Шлюз не выполняет бизнес-валидацию - все решения принимает ApplyObservation
	// Передаем trace_id через контекст для сквозной трассировки
	log.Info("Передача данных в ApplyObservation...")
	if g.obsSvc == nil {
		log.Error("Сервис применения наблюдений не инициализирован")
		return false
	}
	ctxWithTrace := contextkeys.WithTraceID(ctx, traceID)
	if _, err := g.obsSvc.ApplyObservation(ctxWithTrace, fileName, &data); err != nil {
		log.Error("Ошибка применения наблюдения", "error", err)
		return false
	}
	processingTime := time.Since(startTime)
	log.Info("Наблюдение успешно передано в обработку",
		"processing_time", processingTime,
		"file_size", fileInfo.Size(),
		"payload_hash", currentPayloadHash)
	return true
}

// computePayloadHash вычисляет SHA256 хеш от содержимого файла.
// Используется для идемпотентности обработки - если хеш не изменился,
// файл не передаётся в ApplyObservation.
func computePayloadHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// Константы для параллельного скачивания
const maxParallelDownloads = 3

// downloadTask представляет задачу на скачивание файла с FTP.
// Используется для передачи параметров в воркер.
type downloadTask struct {
	remotePath string    // Полный путь к файлу на FTP
	localPath  string    // Полный путь к файлу в локальном кэше
	fileName   string    // Имя файла для логирования
	modTime    time.Time // Время модификации файла на FTP
}

// downloadResult представляет результат скачивания файла.
// Используется для сбора ошибок из воркеров.
type downloadResult struct {
	task downloadTask
	err  error
}

// downloadWorker воркер для параллельного скачивания файлов с FTP.
// Читает задачи из канала tasks и отправляет результаты в канал results.
// Завершает работу при закрытии канала tasks или отмене контекста.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - tasks: канал с задачами на скачивание
//   - results: канал для отправки результатов
//   - wg: WaitGroup для синхронизации завершения воркеров
func (g *agentFTPGatewayImpl) downloadWorker(
	ctx context.Context,
	tasks <-chan downloadTask,
	results chan<- downloadResult,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for task := range tasks {
		select {
		case <-ctx.Done():
			results <- downloadResult{task, ctx.Err()}
			return
		default:
			err := g.downloadSingleFile(task)
			results <- downloadResult{task, err}
		}
	}
}

// downloadSingleFile скачивает один файл с FTP в локальный кэш.
// Используется воркерами для параллельного скачивания.
//
// Параметры:
//   - task: задача на скачивание с путями и метаданными
//
// Возвращает ошибку при неудачном скачивании или сохранении.
func (g *agentFTPGatewayImpl) downloadSingleFile(task downloadTask) error {
	fileData, err := g.ftpClient.DownloadFile(task.remotePath)
	if err != nil {
		g.logger.Error("Не удалось скачать файл", "file", task.fileName, "error", err)
		return err
	}

	if err := os.WriteFile(task.localPath, fileData, 0644); err != nil {
		g.logger.Error("Не удалось сохранить файл в кэш", "file", task.localPath, "error", err)
		return err
	}

	// Сохраняем оригинальное время модификации для идемпотентной обработки
	if err := os.Chtimes(task.localPath, task.modTime, task.modTime); err != nil {
		g.logger.Warn("Не удалось установить время модификации файла", "file", task.localPath, "error", err)
	}

	g.logger.Debug("Файл успешно скачан в кэш",
		"file", task.fileName,
		"local_path", task.localPath,
		"size", len(fileData))

	return nil
}

// parallelDownload запускает параллельное скачивание файлов с FTP.
// Использует пул из maxParallelDownloads воркеров для ускорения скачивания.
//
// Алгоритм работы:
//  1. Определение количества воркеров (минимум из maxParallelDownloads и количества файлов)
//  2. Запуск воркеров как горутин
//  3. Отправка задач в канал
//  4. Сбор результатов и агрегация ошибок
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - files: список файлов для скачивания
//
// Возвращает:
//   - nil при успешном скачивании всех файлов
//   - error с агрегированными ошибками при неудачах
func (g *agentFTPGatewayImpl) parallelDownload(ctx context.Context, files []downloadTask) error {
	if len(files) == 0 {
		return nil
	}

	g.logger.Info("Запуск параллельного скачивания", "files_count", len(files))

	tasks := make(chan downloadTask, len(files))
	results := make(chan downloadResult, len(files))
	var wg sync.WaitGroup

	// Определение количества воркеров
	workerCount := maxParallelDownloads
	if len(files) < workerCount {
		workerCount = len(files)
	}

	// Запуск воркеров
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go g.downloadWorker(ctx, tasks, results, &wg)
	}

	// Отправка задач
	for _, file := range files {
		tasks <- file
	}
	close(tasks)

	// Ожидание завершения всех воркеров
	go func() {
		wg.Wait()
		close(results)
	}()

	// Сбор результатов
	var errors []error
	for result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Errorf("ошибка скачивания %s: %w", result.task.fileName, result.err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("ошибки при скачивании: %v", errors)
	}
	return nil
}

// checkFileNeedsDownload определяет, нужно ли скачивать файл с FTP.
// Проверяет: 1) БД, 2) локальный кэш, 3) актуальность по mod_time/size.
//
// Параметры:
//   - fileName: имя файла для проверки
//   - ftpModTime: время модификации файла на FTP
//   - ftpSize: размер файла на FTP
//
// Возвращает:
//   - true: файл нужно скачать (новый или изменённый)
//   - false: файл не изменился, скачивание не требуется
func (g *agentFTPGatewayImpl) checkFileNeedsDownload(fileName string, ftpModTime time.Time, ftpSize int64) bool {
	// 1. Проверка в БД
	var previousState models.AgentFile
	err := g.db.Where("file_name = ?", fileName).First(&previousState).Error
	if err == nil {
		// Файл есть в БД - проверяем актуальность по LastCheckedModTime
		if previousState.LastCheckedModTime != nil {
			// Если время на FTP не изменилось, файл актуален
			if !ftpModTime.After(*previousState.LastCheckedModTime) {
				g.logger.Debug("Файл актуален в БД", "file", fileName, "mod_time", ftpModTime.Format(time.RFC3339))
				return false
			}
		}
		// Время изменилось - нужно скачать
		g.logger.Debug("Файл обновлён на FTP (есть в БД)", "file", fileName,
			"last_checked", previousState.LastCheckedModTime,
			"ftp_mod_time", ftpModTime.Format(time.RFC3339))
		return true
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		// Ошибка при чтении из БД (не "не найдено"), скачиваем для надёжности
		g.logger.Warn("Ошибка при проверке состояния файла в БД, будет скачан", "file", fileName, "error", err)
		return true
	}

	// 2. Файл не найден в БД - проверяем локальный кэш
	localPath := filepath.Join(g.cfg.FTPCachePath, fileName)
	if info, err := os.Stat(localPath); err == nil {
		// Файл есть в кэше - проверяем актуальность по mod_time и size
		// Используем точное сравнение времени (с точностью до секунды)
		localModTime := info.ModTime()
		if localModTime.Equal(ftpModTime) && info.Size() == ftpSize {
			g.logger.Debug("Файл актуален в локальном кэше", "file", fileName,
				"local_mod_time", localModTime.Format(time.RFC3339),
				"ftp_mod_time", ftpModTime.Format(time.RFC3339),
				"size", ftpSize)
			return false
		}
		// Файл в кэше устарел
		g.logger.Debug("Файл обновлён на FTP (есть в кэше)", "file", fileName,
			"local_mod_time", localModTime.Format(time.RFC3339),
			"ftp_mod_time", ftpModTime.Format(time.RFC3339))
		return true
	}

	// 3. Файла нет ни в БД, ни в кэше - нужно скачать
	g.logger.Debug("Файл отсутствует в БД и кэше", "file", fileName)
	return true
}

// InitializeDBFromCache инициализирует записи в БД из существующих файлов кэша.
// Используется при запуске с флагом --seed для восстановления состояния БД
// из ранее скачанных файлов без обращения к FTP-серверу.
//
// Алгоритм работы:
//  1. Читает список файлов в директории кэша
//  2. Для каждого JSON-файла проверяет наличие записи в БД
//  3. Если записи нет - создаёт её с метаданными файла
//
// Параметры:
//   - ctx: контекст для отмены операции
//
// Возвращает ошибку при невозможности прочитать директорию кэша.
func (g *agentFTPGatewayImpl) InitializeDBFromCache(ctx context.Context) error {
	g.logger.Info("Инициализация БД из локального кэша...")

	files, err := os.ReadDir(g.cfg.FTPCachePath)
	if err != nil {
		return fmt.Errorf("ошибка чтения кэша: %w", err)
	}

	initializedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, file := range files {
		// Пропускаем директории и не-JSON файлы
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}

		info, err := file.Info()
		if err != nil {
			g.logger.Warn("Не удалось получить информацию о файле", "file", file.Name(), "error", err)
			errorCount++
			continue
		}

		// Проверяем, есть ли уже запись в БД
		var existing models.AgentFile
		err = g.db.WithContext(ctx).Where("file_name = ?", file.Name()).First(&existing).Error
		if err == nil {
			// Запись уже существует
			skippedCount++
			continue
		}

		if !errors.Is(err, gorm.ErrRecordNotFound) {
			g.logger.Warn("Ошибка при проверке записи в БД", "file", file.Name(), "error", err)
			errorCount++
			continue
		}

		// Создаём новую запись в БД
		modTime := info.ModTime()
		agentFile := models.AgentFile{
			FileName:              file.Name(),
			LastProcessedModTime:  info.ModTime(),
			LastProcessedFileSize: info.Size(),
			LastCheckedModTime:    &modTime,
		}

		if err := g.db.WithContext(ctx).Create(&agentFile).Error; err != nil {
			g.logger.Warn("Не удалось создать запись в БД", "file", file.Name(), "error", err)
			errorCount++
			continue
		}

		initializedCount++
	}

	g.logger.Info("Инициализация БД из кэша завершена",
		"initialized", initializedCount,
		"skipped", skippedCount,
		"errors", errorCount)

	return nil
}

// LoadAgentDataFromCache загружает данные агентов из локального кэша
// без обращения к FTP-серверу. Используется при запуске с флагом --seed
// для обработки ранее скачанных файлов.
//
// Алгоритм работы:
//  1. Читает список файлов в директории кэша
//  2. Для каждого JSON-файла читает содержимое и парсит в AgentDataDTO
//  3. Передаёт данные в ApplyObservation для обработки
//
// Параметры:
//   - ctx: контекст для отмены операции
//
// Возвращает количество успешно обработанных наблюдений и ошибку.
func (g *agentFTPGatewayImpl) LoadAgentDataFromCache(ctx context.Context) (int, error) {
	g.logger.Info("Загрузка данных агентов из локального кэша...")

	files, err := os.ReadDir(g.cfg.FTPCachePath)
	if err != nil {
		return 0, fmt.Errorf("ошибка чтения кэша: %w", err)
	}

	processedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, file := range files {
		// Пропускаем директории и не-JSON файлы
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}

		// Проверка отмены контекста
		select {
		case <-ctx.Done():
			g.logger.Info("Загрузка из кэша прервана", "processed", processedCount)
			return processedCount, ctx.Err()
		default:
		}

		// Используем существующий метод processFile для обработки
		if g.processFile(ctx, file.Name()) {
			processedCount++
		} else {
			skippedCount++
		}
	}

	g.logger.Info("Загрузка данных агентов из кэша завершена",
		"processed", processedCount,
		"skipped", skippedCount,
		"errors", errorCount)

	return processedCount, nil
}
