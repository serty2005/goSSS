package gateways

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AgentFTPGateway отвечает за чтение данных от агентов с FTP и публикацию событий.
type AgentFTPGateway interface {
	Start(ctx context.Context)
}

type agentFTPGatewayImpl struct {
	cfg       *config.Config
	logger    *zap.Logger
	db        *gorm.DB
	ftpClient services.FTPClient
	bus       eventbus.EventBus
}

func NewAgentFTPGateway(cfg *config.Config, logger *zap.Logger, db *gorm.DB, ftpClient services.FTPClient, bus eventbus.EventBus) AgentFTPGateway {
	return &agentFTPGatewayImpl{cfg, logger, db, ftpClient, bus}
}

func (g *agentFTPGatewayImpl) Start(ctx context.Context) {
	g.logger.Info("Запуск шлюза агентов (FTP)", zap.Duration("interval", g.cfg.AgentFTPInterval))
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

func (g *agentFTPGatewayImpl) runReconciliationCycle(ctx context.Context) {
	g.logger.Info("Начало нового цикла сверки данных с FTP...")
	if err := g.syncLocalCacheWithFTP(ctx); err != nil {
		g.logger.Error("Ошибка синхронизации кэша с FTP, цикл прерван", zap.Error(err))
		return
	}
	localFiles, err := os.ReadDir(g.cfg.FTPCachePath)
	if err != nil {
		g.logger.Error("Не удалось прочитать директорию с кэшем, цикл прерван", zap.Error(err))
		return
	}
	publishedEvents := 0
	for _, file := range localFiles {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
			if g.processFile(ctx, file.Name()) {
				publishedEvents++
			}
		}
	}
	g.logger.Info("Цикл сверки данных с FTP завершен.", zap.Int("published_events", publishedEvents))
}

// processFile обрабатывает один файл из кэша.
// Возвращает true, если было опубликовано событие, иначе false.
func (g *agentFTPGatewayImpl) processFile(ctx context.Context, fileName string) bool {
	log := g.logger.With(zap.String("file", fileName))
	localFilePath := filepath.Join(g.cfg.FTPCachePath, fileName)

	// 1. Получаем актуальную информацию о файле
	fileInfo, err := os.Stat(localFilePath)
	if err != nil {
		log.Error("Не удалось получить информацию о файле в кэше", zap.Error(err))
		return false
	}

	// 2. Получаем предыдущее сохраненное состояние файла из БД
	var previousState models.AgentFile
	err = g.db.WithContext(ctx).First(&previousState, "file_name = ?", fileName).Error
	isNewRecordInDB := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !isNewRecordInDB {
		log.Error("Ошибка получения состояния файла из БД", zap.Error(err))
		return false
	}

	// 3. Главная оптимизация: если файл не менялся с последней проверки, молча выходим
	if !isNewRecordInDB && previousState.LastProcessedModTime.Equal(fileInfo.ModTime()) && previousState.LastProcessedFileSize == fileInfo.Size() {
		return false
	}

	// 4. Файл новый или обновлен. Читаем и парсим его.
	fileData, err := os.ReadFile(localFilePath)
	if err != nil {
		log.Error("Не удалось прочитать файл из кэша", zap.Error(err))
		return false
	}

	var data api.AgentDataDTO
	if err := json.Unmarshal(fileData, &data); err != nil {
		log.Error("Не удалось распарсить JSON, файл будет пропущен до следующего изменения", zap.Error(err))
		// Не обновляем статус в БД, чтобы в следующий раз попытаться снова, если файл изменится.
		return false
	}

	// 5. Сравниваем текущую иерархию с предыдущей
	currentFRSerial := data.SerialNumber
	currentRMSUrl := data.URLRms
	hierarchyHasChanged := isNewRecordInDB ||
		(utils.SafeStringDereference(previousState.LastSeenFRSerial) != currentFRSerial) ||
		(utils.SafeStringDereference(previousState.LastSeenRMSUrl) != currentRMSUrl)

	// 6. Обновляем состояние файла в БД с помощью UPSERT
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
		log.Error("Не удалось обновить статус файла в БД", zap.Error(err))
		return false // Не публикуем событие, если не смогли сохранить состояние
	}

	// 7. Если иерархия изменилась, публикуем событие
	if hierarchyHasChanged {
		log.Info("Обнаружен новый файл или изменение в иерархии объектов (FR/RMS). Публикация события...")
		g.bus.Publish(eventbus.Event{
			Type:    events.AgentDataReceived,
			Payload: data,
		})
		return true
	}

	log.Debug("Файл обновлен, но иерархия объектов не изменилась. Событие не публикуется.")
	return false
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
			s.logger.Info("Обнаружен новый/обновленный файл, скачивание...", zap.String("file", ftpFile.Name))
			ftpFilePath := path.Join(s.cfg.FTPPath, ftpFile.Name)
			fileData, err := s.ftpClient.DownloadFile(ftpFilePath)
			if err != nil {
				s.logger.Error("Не удалось скачать файл", zap.String("file", ftpFile.Name), zap.Error(err))
				continue
			}

			localFilePath := filepath.Join(s.cfg.FTPCachePath, ftpFile.Name)
			if err := os.WriteFile(localFilePath, fileData, 0644); err != nil {
				s.logger.Error("Не удалось сохранить файл в кэш", zap.String("file", localFilePath), zap.Error(err))
				continue
			}
			os.Chtimes(localFilePath, ftpFile.Time, ftpFile.Time)
		}
	}
	s.logger.Info("Синхронизация локального кэша завершена.")
	return nil
}
