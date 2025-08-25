package gateways

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/internal/services"
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
	for _, file := range localFiles {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
			g.processFile(ctx, file.Name())
		}
	}
	g.logger.Info("Цикл сверки данных с FTP завершен.")
}

func (g *agentFTPGatewayImpl) processFile(ctx context.Context, fileName string) {
	log := g.logger.With(zap.String("file", fileName))
	localFilePath := filepath.Join(g.cfg.FTPCachePath, fileName)

	if processed, err := g.isAlreadyProcessed(ctx, fileName); err != nil {
		log.Error("Ошибка проверки статуса файла в БД", zap.Error(err))
		return
	} else if processed {
		return
	}

	fileData, err := os.ReadFile(localFilePath)
	if err != nil {
		log.Error("Не удалось прочитать файл из кэша", zap.Error(err))
		return
	}

	var data api.AgentDataDTO
	if err := json.Unmarshal(fileData, &data); err != nil {
		log.Error("Не удалось распарсить JSON", zap.Error(err))
		return
	}

	log.Info("Обработка файла из кэша и публикация события...")
	g.bus.Publish(eventbus.Event{
		Type:    events.AgentDataReceived,
		Payload: data,
	})

	g.updateAgentFileStatus(ctx, fileName)
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

// isAlreadyProcessed проверяет, был ли файл с таким же именем, размером и временем модификации уже обработан.
func (s *agentFTPGatewayImpl) isAlreadyProcessed(ctx context.Context, fileName string) (bool, error) {
	var processedFile models.AgentFile
	err := s.db.WithContext(ctx).First(&processedFile, "file_name = ?", fileName).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	fileInfo, err := os.Stat(filepath.Join(s.cfg.FTPCachePath, fileName))
	if err != nil {
		return false, err
	}
	return processedFile.LastProcessedModTime.Equal(fileInfo.ModTime()) && processedFile.LastProcessedFileSize == fileInfo.Size(), nil
}

// updateAgentFileStatus сохраняет в БД информацию об обработанном файле.
func (s *agentFTPGatewayImpl) updateAgentFileStatus(ctx context.Context, fileName string) {
	localPath := filepath.Join(s.cfg.FTPCachePath, fileName)
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		s.logger.Error("Не удалось получить информацию о файле в кэше для обновления статуса", zap.String("file", fileName), zap.Error(err))
		return
	}
	record := models.AgentFile{
		FileName:              fileName,
		LastProcessedModTime:  fileInfo.ModTime(),
		LastProcessedFileSize: fileInfo.Size(),
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_processed_mod_time", "last_processed_file_size", "updated_at"}),
	}).Create(&record).Error
	if err != nil {
		s.logger.Error("Не удалось обновить статус файла в БД", zap.String("file", fileName), zap.Error(err))
	}
}