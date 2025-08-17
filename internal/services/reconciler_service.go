package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"fmt"
	"net"
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

// agentDataDTO определяет структуру данных, получаемых из JSON-файла от агента.
type agentDataDTO struct {
	ModelName     string `json:"modelName"`
	SerialNumber  string `json:"serialNumber"`
	RNM           string `json:"RNM"`
	FNSerial      string `json:"fn_serial"`
	DateTimeEnd   string `json:"dateTime_end"`
	FFDVersion    string `json:"ffdVersion"`
	Hostname      string `json:"hostname"`
	URLRms        string `json:"url_rms"`
	TeamviewerID  string `json:"teamviewer_id"`
	AnydeskID     string `json:"anydesk_id"`
	LitemanagerID string `json:"litemanager_id"`
	CurrentTime   string `json:"current_time"`
}

// ReconcilerService определяет публичный интерфейс для сервиса сверки.
type ReconcilerService interface {
	Start(ctx context.Context)
}

// reconcilerServiceImpl является конкретной реализацией интерфейса ReconcilerService.
// ИЗМЕНЕНИЕ: Имя структуры изменено, чтобы избежать конфликта с именем интерфейса.
type reconcilerServiceImpl struct {
	cfg       *config.Config
	logger    *zap.Logger
	db        *gorm.DB
	ftpClient FTPClient
}

// NewReconcilerService создает новый экземпляр сервиса сверки.
func NewReconcilerService(cfg *config.Config, logger *zap.Logger, db *gorm.DB, ftpClient FTPClient) ReconcilerService {
	// ИЗМЕНЕНИЕ: Мы создаем экземпляр структуры *reconcilerServiceImpl, а возвращаем его как интерфейс ReconcilerService.
	return &reconcilerServiceImpl{cfg: cfg, logger: logger, db: db, ftpClient: ftpClient}
}

// Start запускает сервис в фоновом режиме с периодическими циклами сверки.
// ИЗМЕНЕНИЕ: Ресивер теперь указывает на *reconcilerServiceImpl
func (s *reconcilerServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск сервиса сверки (ReconcilerService)", zap.Duration("interval", s.cfg.ReconcileInterval))
	ticker := time.NewTicker(s.cfg.ReconcileInterval)
	defer ticker.Stop()
	s.runReconciliationCycle(ctx) // Первый запуск сразу, не дожидаясь тикера
	for {
		select {
		case <-ticker.C:
			s.runReconciliationCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Остановка сервиса сверки (ReconcilerService)...")
			return
		}
	}
}

// ИЗМЕНЕНИЕ: Все последующие методы теперь имеют ресивер *reconcilerServiceImpl
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
			s.logger.Info("Контекст отменен, прерывание цикла обработки файлов.")
			return
		default:
			s.processFile(ctx, file.Name())
		}
	}
	s.logger.Info("Цикл сверки данных завершен.")
}

func (s *reconcilerServiceImpl) syncLocalCacheWithFTP(ctx context.Context) error {
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

	var data agentDataDTO
	if err := json.Unmarshal(fileData, &data); err != nil {
		log.Error("Не удалось распарсить JSON", zap.Error(err))
		return
	}

	log.Info("Обработка файла из кэша...")

	bestOwnerID, candidateUUID, method, votes, err := s.findBestCandidate(ctx, &data)
	if err != nil {
		log.Warn("Ошибка при поиске кандидата", zap.Error(err))
	}

	if votes >= 2 {
		log.Info("Владелец определен надежно", zap.String("ownerID", bestOwnerID), zap.Int("votes", votes), zap.String("method", method))
		s.reconcileEntities(ctx, &data, bestOwnerID, method, log)
	} else {
		log.Warn("Недостаточно совпадений для автоматической сверки. Создание задачи 'new_client'.", zap.Int("votes", votes), zap.String("reason", method))

		taskDetails := make(map[string]interface{})
		agentDataJSON, _ := json.Marshal(data)
		taskDetails["agent_data"] = json.RawMessage(agentDataJSON)

		if candidateUUID != "" {
			taskDetails["potential_match_uuid"] = candidateUUID
		}

		details, _ := json.Marshal(taskDetails)
		s.createReconciliationTask(ctx, "new_client", "", "", datatypes.JSON(details), method)
	}
	s.updateAgentFileStatus(ctx, fileName)
}

func (s *reconcilerServiceImpl) findBestCandidate(ctx context.Context, data *agentDataDTO) (ownerID, entityUUID, method string, votes int, err error) {
	ownerVotes := make(map[string]int)
	ownerSource := make(map[string]string)
	var methods []string

	addVote := func(id, idType string, foundOwnerID, foundEntityUUID *string) {
		if foundOwnerID != nil && *foundOwnerID != "" {
			ownerVotes[*foundOwnerID]++
			if ownerSource[*foundOwnerID] == "" && foundEntityUUID != nil {
				ownerSource[*foundOwnerID] = *foundEntityUUID
			}
			methods = append(methods, fmt.Sprintf("%s(%s)", idType, id))
		}
	}

	if id := data.AnydeskID; id != "" && id != "None" {
		var ws models.Workstation
		if s.db.WithContext(ctx).Where("anydesk = ?", id).Order("last_modified_date DESC").First(&ws).Error == nil {
			addVote(id, "Anydesk", ws.OwnerServiceDeskUUID, ws.ServiceDeskUUID)
		}
	}
	if id := data.TeamviewerID; id != "" && id != "None" {
		var ws models.Workstation
		if s.db.WithContext(ctx).Where("teamviewer = ?", id).Order("last_modified_date DESC").First(&ws).Error == nil {
			addVote(id, "Teamviewer", ws.OwnerServiceDeskUUID, ws.ServiceDeskUUID)
		}
	}
	if id := data.LitemanagerID; id != "" && id != "None" {
		var ws models.Workstation
		if s.db.WithContext(ctx).Where("litemanager = ?", id).Order("last_modified_date DESC").First(&ws).Error == nil {
			addVote(id, "Litemanager", ws.OwnerServiceDeskUUID, ws.ServiceDeskUUID)
		}
	}
	if id := data.SerialNumber; id != "" {
		var fr models.FiscalRegister
		if s.db.WithContext(ctx).Where("fr_serial_number = ?", id).Order("last_modified_date DESC").First(&fr).Error == nil {
			addVote(id, "FR_SN", fr.OwnerServiceDeskUUID, fr.ServiceDeskUUID)
		}
	}
	if url := data.URLRms; url != "" {
		if matches := rmsUrlRegex.FindStringSubmatch(url); len(matches) > 2 {
			domain := matches[2]
			var server models.Server
			if s.db.WithContext(ctx).Where("ip LIKE ?", domain+"%").Order("last_modified_date DESC").First(&server).Error == nil {
				addVote(domain, "ServerIP", server.OwnerServiceDeskUUID, server.ServiceDeskUUID)
			}
		}
	}

	var bestOwnerID string
	maxVotes := 0
	for oID, v := range ownerVotes {
		if v > maxVotes {
			maxVotes, bestOwnerID = v, oID
		}
	}

	return bestOwnerID, ownerSource[bestOwnerID], strings.Join(methods, ", "), maxVotes, nil
}

func (s *reconcilerServiceImpl) reconcileEntities(ctx context.Context, data *agentDataDTO, ownerID string, method string, log *zap.Logger) {
	if data.SerialNumber != "" {
		var fr models.FiscalRegister
		err := s.db.WithContext(ctx).Where("fr_serial_number = ?", data.SerialNumber).First(&fr).Error
		if err == nil {
			log.Info("Найден ФР по серийному номеру, сверка...", zap.String("serial", data.SerialNumber), zap.String("uuid", *fr.ServiceDeskUUID))
			s.reconcileSingleFiscalRegister(ctx, &fr, data, ownerID, method, log)
		} else if err == gorm.ErrRecordNotFound {
			if data.RNM != "" {
				errRnm := s.db.WithContext(ctx).Where("rn_kkt = ?", data.RNM).First(&fr).Error
				if errRnm == nil {
					log.Info("Найден ФР по РНМ, сверка...", zap.String("rnm", data.RNM), zap.String("uuid", *fr.ServiceDeskUUID))
					s.reconcileSingleFiscalRegister(ctx, &fr, data, ownerID, method, log)
				}
			}
		} else {
			log.Error("Ошибка поиска фискального регистратора", zap.String("serial", data.SerialNumber), zap.Error(err))
		}
	}

	if data.AnydeskID != "" || data.TeamviewerID != "" || data.LitemanagerID != "" {
		var ws models.Workstation
		query := s.db.WithContext(ctx)
		if data.AnydeskID != "" && data.AnydeskID != "None" {
			query = query.Or("anydesk = ?", data.AnydeskID)
		}
		if data.TeamviewerID != "" && data.TeamviewerID != "None" {
			query = query.Or("teamviewer = ?", data.TeamviewerID)
		}
		if data.LitemanagerID != "" && data.LitemanagerID != "None" {
			query = query.Or("litemanager = ?", data.LitemanagerID)
		}
		err := query.Order("last_modified_date DESC").First(&ws).Error
		if err == nil {
			s.reconcileSingleWorkstation(ctx, &ws, data, ownerID, method, log)
		} else if err != gorm.ErrRecordNotFound {
			log.Error("Ошибка поиска рабочей станции", zap.Error(err))
		}
	}

	if data.URLRms != "" {
		matches := rmsUrlRegex.FindStringSubmatch(data.URLRms)
		if len(matches) > 2 {
			domain := matches[2]
			var server models.Server
			err := s.db.WithContext(ctx).Where("ip LIKE ?", domain+"%").First(&server).Error
			if err == nil {
				s.reconcileSingleServer(ctx, &server, data, ownerID, log)
			} else if err != gorm.ErrRecordNotFound {
				log.Error("Ошибка поиска сервера", zap.String("domain", domain), zap.Error(err))
			}
		}
	}
}

func (s *reconcilerServiceImpl) reconcileSingleFiscalRegister(ctx context.Context, fr *models.FiscalRegister, data *agentDataDTO, ownerID string, method string, log *zap.Logger) {
	updates := make(map[string]interface{})
	if fr.OwnerServiceDeskUUID == nil || *fr.OwnerServiceDeskUUID != ownerID {
		details, _ := json.Marshal(map[string]string{"expectedOwner": ownerID, "currentOwner": utils.SafeStringDereference(fr.OwnerServiceDeskUUID)})
		s.createReconciliationTask(ctx, "owner_mismatch", "FiscalRegister", *fr.ServiceDeskUUID, datatypes.JSON(details), method)
	}

	if (fr.FRSerialNumber == nil || *fr.FRSerialNumber == "") && data.SerialNumber != "" {
		log.Info("Обновление поля", zap.String("entity", "FR"), zap.String("field", "fr_serial_number"), zap.Any("old", fr.FRSerialNumber), zap.Any("new", data.SerialNumber))
		updates["fr_serial_number"] = data.SerialNumber
	}
	if (fr.RNKKT == nil || *fr.RNKKT == "") && data.RNM != "" {
		log.Info("Обновление поля", zap.String("entity", "FR"), zap.String("field", "rn_kkt"), zap.Any("old", fr.RNKKT), zap.Any("new", data.RNM))
		updates["rn_kkt"] = data.RNM
	}

	parsedTime := utils.ParseAgentTime(data.DateTimeEnd)
	if parsedTime != nil {
		if fr.FNExpireDate == nil || !fr.FNExpireDate.Equal(*parsedTime) {
			log.Info("Обновление поля", zap.String("entity", "FR"), zap.String("field", "fn_expire_date"), zap.Any("old", fr.FNExpireDate), zap.Any("new", parsedTime))
			updates["fn_expire_date"] = parsedTime
		}
	} else if data.DateTimeEnd != "" && fr.FNExpireDate != nil {
		log.Warn("Агент прислал пустую дату окончания ФН, но в базе есть дата. Обнуление не производится.", zap.String("uuid", *fr.ServiceDeskUUID), zap.Time("existing_date", *fr.FNExpireDate))
	}
	formattedFFD := utils.FormatFFDVersion(data.FFDVersion)
	if fr.FFD == nil || *fr.FFD != formattedFFD {
		log.Info("Обновление поля", zap.String("entity", "FR"), zap.String("field", "ffd"), zap.Any("old", fr.FFD), zap.Any("new", formattedFFD))
		updates["ffd"] = formattedFFD
	}
	if fr.ModelKKT == nil || *fr.ModelKKT != data.ModelName {
		log.Info("Обновление поля", zap.String("entity", "FR"), zap.String("field", "model_kkt"), zap.Any("old", fr.ModelKKT), zap.Any("new", data.ModelName))
		updates["model_kkt"] = data.ModelName
	}
	if fr.FNNumber == nil || *fr.FNNumber != data.FNSerial {
		log.Info("Обновление поля", zap.String("entity", "FR"), zap.String("field", "fn_number"), zap.Any("old", fr.FNNumber), zap.Any("new", data.FNSerial))
		updates["fn_number"] = data.FNSerial
	}
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		s.db.Model(fr).Updates(updates)
		log.Info("Данные фискального регистратора обновлены", zap.String("uuid", *fr.ServiceDeskUUID))
	}
}

func (s *reconcilerServiceImpl) reconcileSingleWorkstation(ctx context.Context, ws *models.Workstation, data *agentDataDTO, ownerID string, method string, log *zap.Logger) {
	updates := make(map[string]interface{})
	if ws.OwnerServiceDeskUUID == nil || *ws.OwnerServiceDeskUUID != ownerID {
		details, _ := json.Marshal(map[string]string{"expectedOwner": ownerID, "currentOwner": utils.SafeStringDereference(ws.OwnerServiceDeskUUID)})
		s.createReconciliationTask(ctx, "owner_mismatch", "Workstation", *ws.ServiceDeskUUID, datatypes.JSON(details), method)
	}
	if data.AnydeskID != "None" && (ws.Anydesk == nil || *ws.Anydesk != data.AnydeskID) {
		log.Info("Обновление поля", zap.String("entity", "WS"), zap.String("field", "anydesk"), zap.Any("old", ws.Anydesk), zap.Any("new", data.AnydeskID))
		updates["anydesk"] = data.AnydeskID
	}
	if data.TeamviewerID != "None" && (ws.Teamviewer == nil || *ws.Teamviewer != data.TeamviewerID) {
		log.Info("Обновление поля", zap.String("entity", "WS"), zap.String("field", "teamviewer"), zap.Any("old", ws.Teamviewer), zap.Any("new", data.TeamviewerID))
		updates["teamviewer"] = data.TeamviewerID
	}
	if data.LitemanagerID != "None" && (ws.Litemanager == nil || *ws.Litemanager != data.LitemanagerID) {
		log.Info("Обновление поля", zap.String("entity", "WS"), zap.String("field", "litemanager"), zap.Any("old", ws.Litemanager), zap.Any("new", data.LitemanagerID))
		updates["litemanager"] = data.LitemanagerID
	}
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		s.db.Model(ws).Updates(updates)
		log.Info("Данные рабочей станции обновлены", zap.String("uuid", *ws.ServiceDeskUUID))
	}
}

func (s *reconcilerServiceImpl) reconcileSingleServer(ctx context.Context, server *models.Server, data *agentDataDTO, ownerID string, log *zap.Logger) {
	updates := make(map[string]interface{})

	matches := rmsUrlRegex.FindStringSubmatch(data.URLRms)
	if len(matches) > 2 {
		domain := matches[2]
		ip := net.ParseIP(domain)
		if (ip == nil || !ip.IsPrivate()) && (server.IP == nil || !strings.Contains(*server.IP, domain)) {
			log.Info("Обновление поля", zap.String("entity", "Server"), zap.String("field", "ip"), zap.Any("old", server.IP), zap.Any("new", data.URLRms))
			updates["ip"] = data.URLRms
		}
	}
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		s.db.Model(server).Updates(updates)
		log.Info("Данные сервера обновлены", zap.String("uuid", *server.ServiceDeskUUID))
	}
}

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

func (s *reconcilerServiceImpl) createReconciliationTask(ctx context.Context, taskType, entityType, entityUUID string, details datatypes.JSON, comment string) {
	if taskType == "owner_mismatch" && entityUUID != "" {
		var existingTask models.ReconciliationTask
		err := s.db.WithContext(ctx).Where("entity_uuid = ? AND task_type = ? AND status = 'new'", entityUUID, taskType).First(&existingTask).Error
		if err == nil {
			s.logger.Info("Открытая задача на смену владельца для сущности уже существует, пропуск создания новой.", zap.String("entity_uuid", entityUUID))
			return
		}
		if err != gorm.ErrRecordNotFound {
			s.logger.Error("Ошибка при проверке существования задачи на сверку", zap.Error(err))
			return
		}
	}
	task := models.ReconciliationTask{
		TaskType: taskType, EntityType: entityType, EntityUUID: entityUUID,
		Details: details, Status: "new", Comment: comment,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		s.logger.Error("Не удалось создать задачу на сверку", zap.Error(err))
	} else {
		s.logger.Info("Создана новая задача на сверку", zap.String("type", taskType), zap.String("entity_uuid", entityUUID))
	}
}

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
