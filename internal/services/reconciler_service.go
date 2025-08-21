package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var rmsUrlRegex = regexp.MustCompile(`(?i)(https?://)?([a-zA-Z0-9.-]+)`)

type ReconcilerService interface {
	Start(ctx context.Context)
	ProcessAgentData(ctx context.Context, data *api.AgentDataDTO) (ownerID, entityUUID, method string, err error)
}

type reconcilerServiceImpl struct {
	cfg             *config.Config
	logger          *zap.Logger
	db              *gorm.DB
	ftpClient       FTPClient
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

func NewReconcilerService(cfg *config.Config, logger *zap.Logger, db *gorm.DB, ftpClient FTPClient, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) ReconcilerService {
	return &reconcilerServiceImpl{
		cfg:             cfg,
		logger:          logger,
		db:              db,
		ftpClient:       ftpClient,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

func (s *reconcilerServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск сервиса сверки (ReconcilerService)", zap.Duration("interval", s.cfg.ReconcileInterval))
	ticker := time.NewTicker(s.cfg.ReconcileInterval)
	defer ticker.Stop()
	s.runReconciliationCycle(ctx)
	for {
		select {
		case <-ticker.C:
			s.runReconciliationCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание воркера сверки")
			return
		}
	}
}

// runReconciliationCycle выполняет один полный цикл сверки: синхронизирует локальный кэш с FTP и обрабатывает каждый файл.
func (s *reconcilerServiceImpl) runReconciliationCycle(ctx context.Context) {
	s.logger.Info("Начало нового цикла сверки данных...")
	if err := s.syncLocalCacheWithFTP(ctx); err != nil {
		s.logger.Error("Ошибка синхронизации кэша с FTP, цикл прерван", zap.Error(err))
		return
	}
	localFiles, err := os.ReadDir(s.cfg.FTPCachePath)
	if err != nil {
		s.logger.Error("Не удалось прочитать директорию с кэшем, цикл прерван", zap.Error(err))
		return
	}
	for _, file := range localFiles {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}
		select {
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание воркера обработки файлов.")
			return
		default:
			s.processFile(ctx, file.Name())
		}
	}
	s.logger.Info("Цикл сверки данных завершен.")
}

// syncLocalCacheWithFTP скачивает новые или обновленные файлы с FTP-сервера в локальный кэш.
func (s *reconcilerServiceImpl) syncLocalCacheWithFTP(_ context.Context) error {
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

// processFile обрабатывает один JSON-файл из кэша.
func (s *reconcilerServiceImpl) processFile(ctx context.Context, fileName string) {
	log := s.logger.With(zap.String("file", fileName))
	localFilePath := filepath.Join(s.cfg.FTPCachePath, fileName)

	if processed, err := s.isAlreadyProcessed(ctx, fileName); err != nil {
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

	log.Info("Обработка файла из кэша...")

	if _, _, _, err := s.ProcessAgentData(ctx, &data); err != nil {
		log.Warn("Ошибка при обработке данных из файла", zap.Error(err))
	}
	s.updateAgentFileStatus(ctx, fileName)
}

// ProcessAgentData выполняет основную "водопадную" логику сверки данных от агента.
// Алгоритм работает по принципу приоритетов: сначала ищется совпадение по самому надежному
// признаку (сервер), затем по менее надежному (рабочая станция) и так далее.
func (s *reconcilerServiceImpl) ProcessAgentData(ctx context.Context, data *api.AgentDataDTO) (ownerID, entityUUID, method string, err error) {
	log := s.logger.With(zap.String("agent_hostname", data.Hostname))
	log.Info("Начало процесса сверки данных от агента")

	domain := ""
	if matches := rmsUrlRegex.FindStringSubmatch(data.URLRms); len(matches) > 2 {
		domain = matches[2]
	}
	foundServer, _ := s.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, domain)
	foundWS, _ := s.workstationRepo.FindByRemoteIDs(ctx, data.TeamviewerID, data.AnydeskID, data.LitemanagerID)
	foundFR, _ := s.frRepo.FindBySerialNumber(ctx, data.SerialNumber)

	if foundServer != nil {
		log.Info("Приоритет 1: Найдено совпадение по Серверу", zap.String("server_uuid", utils.SafeStringDereference(foundServer.ServiceDeskUUID)))
		ownerID = utils.SafeStringDereference(foundServer.OwnerServiceDeskUUID)
		s.reconcileFromServerContext(ctx, ownerID, data, foundServer, foundWS, foundFR, log)
		return ownerID, utils.SafeStringDereference(foundServer.ServiceDeskUUID), "server_match", nil
	}

	if foundWS != nil {
		log.Info("Приоритет 2: Найдено совпадение по Рабочей станции", zap.String("ws_uuid", utils.SafeStringDereference(foundWS.ServiceDeskUUID)))
		ownerID = utils.SafeStringDereference(foundWS.OwnerServiceDeskUUID)
		s.reconcileFromWorkstationContext(ctx, ownerID, data, foundWS, foundFR, log)
		return ownerID, utils.SafeStringDereference(foundWS.ServiceDeskUUID), "workstation_match", nil
	}

	if foundFR != nil {
		log.Info("Приоритет 3: Найдено совпадение по Фискальному регистратору", zap.String("fr_uuid", utils.SafeStringDereference(foundFR.ServiceDeskUUID)))
		ownerID = utils.SafeStringDereference(foundFR.OwnerServiceDeskUUID)
		s.reconcileFromFRContext(ctx, ownerID, data, foundFR, log)
		return ownerID, utils.SafeStringDereference(foundFR.ServiceDeskUUID), "fr_match", nil
	}

	log.Warn("Не найдено совпадений ни по одному из приоритетов. Создание задачи 'new_client'.")
	s.createTask(ctx, "new_client", "", "", data, "Не удалось идентифицировать оборудование. Требуется создать нового клиента и привязать оборудование.")
	return "", "", "no_match", fmt.Errorf("не удалось найти совпадения")
}

// reconcileFromServerContext обрабатывает логику сверки, когда сервер является главной точкой отсчета.
// Владелец сервера считается "истинным" владельцем для всего остального оборудования.
func (s *reconcilerServiceImpl) reconcileFromServerContext(ctx context.Context, ownerID string, data *api.AgentDataDTO, server *models.Server, ws *models.Workstation, fr *models.FiscalRegister, log *zap.Logger) {
	s.reconcileServerData(ctx, server, data, log)

	if data.AnydeskID != "" || data.TeamviewerID != "" || data.LitemanagerID != "" {
		if ws != nil {
			// ИСПРАВЛЕНИЕ: Логика изменена. Мы всегда обогащаем найденную станцию.
			log.Info("Найдена существующая рабочая станция, выполняется слияние данных.", zap.String("ws_uuid", utils.SafeStringDereference(ws.ServiceDeskUUID)))
			if utils.SafeStringDereference(ws.OwnerServiceDeskUUID) != ownerID {
				s.createTask(ctx, "owner_mismatch", "Workstation", utils.SafeStringDereference(ws.ServiceDeskUUID), data, fmt.Sprintf("Ожидаемый владелец %s (от сервера), текущий %s", ownerID, utils.SafeStringDereference(ws.OwnerServiceDeskUUID)))
			}
			s.reconcileWorkstationData(ctx, ws, data, log)
		} else {
			log.Info("Рабочая станция с указанными ID не найдена. Создание задачи на добавление.")
			s.createTask(ctx, "add_equipment", "Workstation", "", data, fmt.Sprintf("Добавить новую рабочую станцию для владельца %s", ownerID))
		}
	}

	if data.SerialNumber != "" {
		if fr != nil {
			if utils.SafeStringDereference(fr.OwnerServiceDeskUUID) != ownerID {
				s.createTask(ctx, "owner_mismatch", "FiscalRegister", utils.SafeStringDereference(fr.ServiceDeskUUID), data, fmt.Sprintf("Ожидаемый владелец %s (от сервера), текущий %s", ownerID, utils.SafeStringDereference(fr.OwnerServiceDeskUUID)))
			}
			s.reconcileFiscalRegisterData(ctx, fr, data, log)
		} else {
			s.createTask(ctx, "add_equipment", "FiscalRegister", "", data, fmt.Sprintf("Добавить новый ФР для владельца %s", ownerID))
		}
	}
}

// reconcileFromWorkstationContext обрабатывает логику сверки, когда рабочая станция является точкой отсчета.
func (s *reconcilerServiceImpl) reconcileFromWorkstationContext(ctx context.Context, ownerID string, data *api.AgentDataDTO, ws *models.Workstation, fr *models.FiscalRegister, log *zap.Logger) {
	s.reconcileWorkstationData(ctx, ws, data, log)

	// Сервер по данным агента не был найден на шаге 1 (иначе мы бы не попали в эту функцию).
	// Следовательно, это новый сервер для клиента, определенного по рабочей станции.
	if data.CRMID != "" || data.URLRms != "" {
		s.createTask(ctx, "add_equipment", "Server", "", data, fmt.Sprintf("Добавить новый сервер для владельца %s", ownerID))
	}

	// Проверка фискального регистратора
	if data.SerialNumber != "" {
		if fr != nil {
			if utils.SafeStringDereference(fr.OwnerServiceDeskUUID) != ownerID {
				s.createTask(ctx, "owner_mismatch", "FiscalRegister", utils.SafeStringDereference(fr.ServiceDeskUUID), data, fmt.Sprintf("Ожидаемый владелец %s (от станции), текущий %s", ownerID, utils.SafeStringDereference(fr.OwnerServiceDeskUUID)))
			}
			s.reconcileFiscalRegisterData(ctx, fr, data, log)
		} else {
			s.createTask(ctx, "add_equipment", "FiscalRegister", "", data, fmt.Sprintf("Добавить новый ФР для владельца %s", ownerID))
		}
	}
}

// reconcileFromFRContext обрабатывает логику сверки, когда ФР является точкой отсчета.
func (s *reconcilerServiceImpl) reconcileFromFRContext(ctx context.Context, ownerID string, data *api.AgentDataDTO, fr *models.FiscalRegister, log *zap.Logger) {
	s.reconcileFiscalRegisterData(ctx, fr, data, log)
	// Для сервера и станции создаем задачи на добавление, т.к. они не были найдены на предыдущих шагах.
	if data.CRMID != "" || data.URLRms != "" {
		s.createTask(ctx, "add_equipment", "Server", "", data, fmt.Sprintf("Добавить новый сервер для владельца %s (определен по ФР)", ownerID))
	}
	if data.AnydeskID != "" || data.TeamviewerID != "" || data.LitemanagerID != "" {
		s.createTask(ctx, "add_equipment", "Workstation", "", data, fmt.Sprintf("Добавить новую рабочую станцию для владельца %s (определен по ФР)", ownerID))
	}
}

// --- Вспомогательные функции для "умного" обновления данных ---

// reconcileServerData обновляет данные сервера, только если поля в БД пусты.
func (s *reconcilerServiceImpl) reconcileServerData(ctx context.Context, server *models.Server, data *api.AgentDataDTO, log *zap.Logger) {
	updates := make(map[string]interface{})
	if (server.CRMid == nil || *server.CRMid == "") && data.CRMID != "" {
		updates["crm_id"] = data.CRMID
	}
	if len(updates) > 0 {
		if _, err := s.serverRepo.Update(ctx, nil, utils.SafeStringDereference(server.ServiceDeskUUID), updates); err != nil {
			log.Error("Не удалось обновить данные сервера", zap.String("uuid", utils.SafeStringDereference(server.ServiceDeskUUID)), zap.Error(err))
		}
	}
}

// reconcileWorkstationData выполняет "умное" слияние данных: обновляет поля, только если они пусты, чтобы не затереть существующую информацию.
func (s *reconcilerServiceImpl) reconcileWorkstationData(ctx context.Context, ws *models.Workstation, data *api.AgentDataDTO, log *zap.Logger) {
	updates := make(map[string]interface{})
	if (ws.Anydesk == nil || *ws.Anydesk == "") && data.AnydeskID != "" && data.AnydeskID != "None" {
		updates["anydesk"] = data.AnydeskID
	}
	if (ws.Teamviewer == nil || *ws.Teamviewer == "") && data.TeamviewerID != "" && data.TeamviewerID != "None" {
		updates["teamviewer"] = data.TeamviewerID
	}
	if (ws.Litemanager == nil || *ws.Litemanager == "") && data.LitemanagerID != "" && data.LitemanagerID != "None" {
		updates["litemanager"] = data.LitemanagerID
	}
	if len(updates) > 0 {
		log.Info("Обновление данных рабочей станции (слияние)", zap.String("ws_uuid", utils.SafeStringDereference(ws.ServiceDeskUUID)), zap.Any("added_ids", updates))
		if _, err := s.workstationRepo.Update(ctx, nil, utils.SafeStringDereference(ws.ServiceDeskUUID), updates); err != nil {
			log.Error("Не удалось обновить данные рабочей станции", zap.String("uuid", utils.SafeStringDereference(ws.ServiceDeskUUID)), zap.Error(err))
		}
	}
}

// reconcileFiscalRegisterData обновляет данные ФР, если они найдены по серийному номеру.
// Логика: мы доверяем данным от агента как самым актуальным и полностью обновляем запись в БД.
func (s *reconcilerServiceImpl) reconcileFiscalRegisterData(ctx context.Context, fr *models.FiscalRegister, data *api.AgentDataDTO, log *zap.Logger) {
	updates := make(map[string]interface{})

	// Собираем все поля из данных агента для полного обновления
	updates["model_kkt"] = data.ModelName
	updates["rn_kkt"] = utils.NormalizeRNKKT(data.RNM)
	updates["fn_number"] = data.FNSerial
	updates["inn"] = strings.TrimSpace(data.INN)
	updates["ffd"] = utils.FormatFFDVersion(data.FFDVersion)
	updates["fn_expire_date"] = utils.ParseAgentTime(data.DateTimeEnd)
	updates["last_modified_date"] = time.Now() // Устанавливаем текущее время как дату последнего обновления

	log.Info("Обновление фискального регистратора полным набором данных от агента.", zap.String("uuid", utils.SafeStringDereference(fr.ServiceDeskUUID)), zap.Any("changes", updates))
	if _, err := s.frRepo.Update(ctx, nil, utils.SafeStringDereference(fr.ServiceDeskUUID), updates); err != nil {
		log.Error("Не удалось обновить данные ФР", zap.String("uuid", utils.SafeStringDereference(fr.ServiceDeskUUID)), zap.Error(err))
	}
}

// createTask создает задачу для ручного разбора администратором.
func (s *reconcilerServiceImpl) createTask(ctx context.Context, taskType, entityType, entityUUID string, agentData *api.AgentDataDTO, comment string) {
	details, _ := json.Marshal(agentData)
	task := models.ReconciliationTask{
		TaskType:   taskType,
		EntityType: entityType,
		EntityUUID: entityUUID,
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    comment,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		s.logger.Error("Не удалось создать задачу на сверку", zap.Error(err))
	} else {
		s.logger.Info("Создана новая задача на сверку", zap.String("type", taskType), zap.String("entity_uuid", entityUUID), zap.String("comment", comment))
	}
}

// isAlreadyProcessed проверяет, был ли файл с таким же именем, размером и временем модификации уже обработан.
func (s *reconcilerServiceImpl) isAlreadyProcessed(ctx context.Context, fileName string) (bool, error) {
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
func (s *reconcilerServiceImpl) updateAgentFileStatus(ctx context.Context, fileName string) {
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
