package gateways

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"etalon-server/pkg/eventbus"
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

// AgentFTPGateway отвечает за чтение данных от агентов с FTP и публикацию событий.
type AgentFTPGateway interface {
	Start(ctx context.Context)
}

type agentFTPGatewayImpl struct {
	cfg       *config.Config
	logger    logger.LoggerInterface
	db        *gorm.DB
	ftpClient services.FTPClient
	bus       eventbus.EventBus
}

func NewAgentFTPGateway(cfg *config.Config, logger logger.LoggerInterface, db *gorm.DB, ftpClient services.FTPClient, bus eventbus.EventBus) AgentFTPGateway {
	return &agentFTPGatewayImpl{cfg, logger, db, ftpClient, bus}
}

func (g *agentFTPGatewayImpl) Start(ctx context.Context) {
	g.logger.Info("Запуск шлюза агентов (FTP)", "interval", g.cfg.AgentFTPInterval)
	ticker := time.NewTicker(g.cfg.AgentFTPInterval)
	defer ticker.Stop()
	g.runReconciliationCycle(ctx)
	for {
		select {
		case <-ticker.C:
			g.runReconciliationCycle(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза агентов (FTP).")
			return
		}
	}
}

// isFileNameNumeric проверяет, состоит ли имя файла только из цифр (без расширения).
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

// getFileTypeDescription возвращает описание типа файла для логирования.
func getFileTypeDescription(fileName string) string {
	if isFileNameNumeric(fileName) {
		return "данные ФР"
	}
	return "данные по id/url сервера"
}

// min возвращает минимальное из двух целых чисел.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// isLocalAddress проверяет, является ли адрес локальным.
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
func (s *agentFTPGatewayImpl) syncLocalCacheWithFTP(_ context.Context) error {
	s.logger.Info("Синхронизация локального кэша с FTP...")
	ftpFiles, err := s.ftpClient.ListFiles(s.cfg.FTPPath)
	if err != nil {
		return fmt.Errorf("не удалось получить список файлов с FTP: %w", err)
	}

	localFileInfos := make(map[string]os.FileInfo)
	cachedFiles, err := os.ReadDir(s.cfg.FTPCachePath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать кэш-директорию: %w", err)
	}

	for _, f := range cachedFiles {
		if info, err := f.Info(); err == nil {
			localFileInfos[f.Name()] = info
		}
	}
	for _, ftpFile := range ftpFiles {
		if ftpFile.Type != ftp.EntryTypeFile || !strings.HasSuffix(strings.ToLower(ftpFile.Name), ".json") || ftpFile.Size == 0 {
			continue
		}
		localInfo, found := localFileInfos[ftpFile.Name]
		if !found || ftpFile.Time.After(localInfo.ModTime()) {
			s.logger.Info("Обнаружен новый/обновленный файл, скачивание...", "file", ftpFile.Name)
			ftpFilePath := path.Join(s.cfg.FTPPath, ftpFile.Name)
			fileData, err := s.ftpClient.DownloadFile(ftpFilePath)
			if err != nil {
				s.logger.Error("Не удалось скачать файл", "file", ftpFile.Name, "error", err)
				continue
			}

			localFilePath := filepath.Join(s.cfg.FTPCachePath, ftpFile.Name)
			if err := os.WriteFile(localFilePath, fileData, 0644); err != nil {
				s.logger.Error("Не удалось сохранить файл в кэш", "file", localFilePath, "error", err)
				continue
			}
			os.Chtimes(localFilePath, ftpFile.Time, ftpFile.Time)
		}
	}
	s.logger.Info("Синхронизация локального кэша завершена.")
	return nil
}

func (g *agentFTPGatewayImpl) runReconciliationCycle(ctx context.Context) {
	cycleStartTime := time.Now()
	g.logger.Info("Начало нового цикла сверки данных с FTP...")
	if err := g.syncLocalCacheWithFTP(ctx); err != nil {
		g.logger.Error("Ошибка синхронизации кэша с FTP, цикл прерван", "error", err)
		return
	}
	localFiles, err := os.ReadDir(g.cfg.FTPCachePath)
	if err != nil {
		g.logger.Error("Не удалось прочитать директорию с кэшем, цикл прерван", "error", err)
		return
	}

	publishedEvents := 0
	processedFiles := 0
	skippedFiles := 0
	frFiles := 0
	serverFiles := 0
	totalFileSize := int64(0)

	for _, file := range localFiles {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}

		if isFileNameNumeric(file.Name()) {
			frFiles++
		} else {
			serverFiles++
		}

		if info, err := file.Info(); err == nil {
			totalFileSize += info.Size()
		}

		select {
		case <-ctx.Done():
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

	cycleDuration := time.Since(cycleStartTime)
	avgFileSize := int64(0)
	if processedFiles > 0 {
		avgFileSize = totalFileSize / int64(processedFiles)
	}

	g.logger.Info("Цикл сверки данных с FTP завершен.",
		"published_events", publishedEvents,
		"processed_files", processedFiles,
		"skipped_files", skippedFiles,
		"fr_files", frFiles,
		"server_files", serverFiles,
		"total_file_size", totalFileSize,
		"avg_file_size", avgFileSize,
		"cycle_duration", cycleDuration)
}

// processFile обрабатывает один файл из кэша.
func (g *agentFTPGatewayImpl) processFile(ctx context.Context, fileName string) bool {
	startTime := time.Now()
	fileType := getFileTypeDescription(fileName)
	log := g.logger.With(
		"request_id", fileName,
		"file_type", fileType,
		"is_fr_data", isFileNameNumeric(fileName),
	)
	log.Debug("Начало обработки файла агента")
	localFilePath := filepath.Join(g.cfg.FTPCachePath, fileName)

	fileInfo, err := os.Stat(localFilePath)
	if err != nil {
		log.Error("Не удалось получить информацию о файле в кэше", "error", err)
		return false
	}

	var previousState models.AgentFile
	err = g.db.WithContext(ctx).First(&previousState, "file_name = ?", fileName).Error
	isNewRecordInDB := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNewRecordInDB {
		log.Error("Ошибка получения состояния файла из БД", "error", err)
		return false
	}

	if !isNewRecordInDB && previousState.LastProcessedModTime.Equal(fileInfo.ModTime()) && previousState.LastProcessedFileSize == fileInfo.Size() {
		return false
	}

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

	var data api.AgentDataDTO
	if err := json.Unmarshal(fileData, &data); err != nil {
		log.Error("Не удалось распарсить JSON, файл будет пропущен до следующего изменения",
			"error", err,
			"file_size", len(fileData),
			"file_content_preview", string(fileData[:min(200, len(fileData))]))
		return false
	}
	log.Info("JSON успешно распарсен", "RMS_url", data.URLRms, "FR_serial", data.SerialNumber)

	// Валидация данных агента
	if err := validateAgentData(&data, log); err != nil {
		log.Error("Валидация данных агента не пройдена, файл будет пропущен", "error", err)
		return false
	}
	log.Info("Валидация данных агента пройдена успешно")

	currentFRSerial := data.SerialNumber
	currentRMSUrl := data.URLRms

	// Проверка дублирования событий - если данные идентичны предыдущим, не публикуем событие
	hierarchyHasChanged := isNewRecordInDB ||
		(utils.SafeStringDereference(previousState.LastSeenFRSerial) != currentFRSerial) ||
		(utils.SafeStringDereference(previousState.LastSeenRMSUrl) != currentRMSUrl)

	// Дополнительная проверка на дублирование: если данные не изменились, но прошло много времени,
	// можно отправить heartbeat событие (опционально)

	if hierarchyHasChanged {
		log.Info("Обнаружено изменение в иерархии объектов",
			"is_new_file", isNewRecordInDB,
			"prev_fr_serial", utils.SafeStringDereference(previousState.LastSeenFRSerial),
			"current_fr_serial", currentFRSerial,
			"prev_rms_url", utils.SafeStringDereference(previousState.LastSeenRMSUrl),
			"current_rms_url", currentRMSUrl)
	}

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

	if hierarchyHasChanged {
		log.Info("Обнаружен новый файл или изменение в иерархии объектов. Публикация события...")

		// ИЗМЕНЕНИЕ: Формируем новую полезную нагрузку
		payload := events.AgentDataPayload{
			Source: fileName,
			Data:   data,
		}

		g.bus.Publish(eventbus.Event{
			Type:    events.AgentDataReceived,
			Payload: payload,
		})
		processingTime := time.Since(startTime)
		log.Info("Событие AgentDataReceived опубликовано",
			"event_type", string(events.AgentDataReceived),
			"processing_time", processingTime)
		return true
	}

	log.Debug("Файл обновлен, но иерархия объектов не изменилась. Событие не публикуется.")
	processingTime := time.Since(startTime)
	log.Info("Обработка файла завершена успешно",
		"event_published", false,
		"processing_time", processingTime)
	return false
}
