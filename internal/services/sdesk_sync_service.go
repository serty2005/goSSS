package services

import (
	"context"
	"errors"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SDeskSyncService отвечает за фоновую инкрементальную синхронизацию с ServiceDesk.
type SDeskSyncService interface {
	Start(ctx context.Context)
}

type sdeskSyncServiceImpl struct {
	cfg             *config.Config
	sdClient        ServiceDeskClient
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	logger          *zap.Logger
	db              *gorm.DB
	mu              sync.Mutex
	isSyncing       bool
}

// localEntityInfo - внутренняя структура для хранения минимально необходимых данных из локальной БД для сравнения.
type localEntityInfo struct {
	LastModifiedDate *time.Time
	DeletedAt        gorm.DeletedAt
}

// NewSDeskSyncService создает новый экземпляр сервиса синхронизации.
func NewSDeskSyncService(
	cfg *config.Config,
	db *gorm.DB,
	sdClient ServiceDeskClient,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	logger *zap.Logger,
) SDeskSyncService {
	return &sdeskSyncServiceImpl{
		cfg:             cfg,
		db:              db,
		sdClient:        sdClient,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		logger:          logger,
	}
}

// Start запускает воркер в фоновом режиме.
func (s *sdeskSyncServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера синхронизации с ServiceDesk", zap.Duration("interval", s.cfg.SDeskSyncInterval))
	ticker := time.NewTicker(s.cfg.SDeskSyncInterval)
	defer ticker.Stop()

	s.runSyncCycle(ctx)

	for {
		select {
		case <-ticker.C:
			s.runSyncCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Остановка воркера синхронизации с ServiceDesk...")
			return
		}
	}
}

func (s *sdeskSyncServiceImpl) runSyncCycle(ctx context.Context) {
	s.mu.Lock()
	if s.isSyncing {
		s.logger.Warn("Цикл синхронизации уже запущен. Пропуск.")
		s.mu.Unlock()
		return
	}
	s.isSyncing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isSyncing = false
		s.mu.Unlock()
	}()

	s.logger.Info("Начало нового цикла синхронизации с ServiceDesk.")

	s.syncEntityType(ctx, "ou$company")
	s.syncEntityType(ctx, "objectBase$Server")
	s.syncEntityType(ctx, "objectBase$Workstation")
	s.syncEntityType(ctx, "objectBase$FR")

	s.logger.Info("Цикл синхронизации с ServiceDesk завершен.")
}

// syncEntityType выполняет инкрементальную синхронизацию для одного типа сущности.
func (s *sdeskSyncServiceImpl) syncEntityType(ctx context.Context, metaClass string) {
	log := s.logger.With(zap.String("metaClass", metaClass))
	log.Info("Начало синхронизации типа сущности")

	remoteList, err := s.sdClient.FetchEntityList(ctx, metaClass, false)
	if err != nil {
		log.Error("Не удалось получить список сущностей из ServiceDesk", zap.Error(err))
		return
	}

	localMap, err := s.getLocalEntities(ctx, metaClass)
	if err != nil {
		log.Error("Не удалось получить локальные сущности", zap.Error(err))
		return
	}

	remoteUUIDs := make(map[string]struct{}, len(remoteList))
	for _, item := range remoteList {
		if uuid, ok := item["UUID"].(string); ok {
			remoteUUIDs[uuid] = struct{}{}
		}
	}

	var toCreate, toUpdate, toDelete []string

	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteLMD := utils.ParseServiceDeskTime(remoteItem["lastModifiedDate"].(string))
		if remoteLMD == nil {
			continue
		}
		localEntity, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
		} else if localEntity.DeletedAt.Valid || (localEntity.LastModifiedDate != nil && remoteLMD.After(*localEntity.LastModifiedDate)) {
			toUpdate = append(toUpdate, remoteUUID)
		}
	}

	for uuid, localInfo := range localMap {
		if _, exists := remoteUUIDs[uuid]; !exists && !localInfo.DeletedAt.Valid {
			toDelete = append(toDelete, uuid)
		}
	}

	log.Info("Сравнение завершено",
		zap.Int("to_create", len(toCreate)),
		zap.Int("to_update", len(toUpdate)),
		zap.Int("to_delete", len(toDelete)))

	// Вызываем отдельные, чистые функции для каждого типа операции.
	if len(toCreate) > 0 {
		s.processCreationsInParallel(ctx, metaClass, toCreate, log)
	}
	if len(toUpdate) > 0 {
		s.processUpdatesInParallel(ctx, metaClass, toUpdate, log)
	}
	if len(toDelete) > 0 {
		s.processDeletions(ctx, metaClass, toDelete, log)
	}

	if len(toCreate) == 0 && len(toUpdate) == 0 && len(toDelete) == 0 {
		log.Info("Нет сущностей для создания, обновления или удаления.")
	}
}

// processDeletions выполняет "мягкое удаление" и закрывает связанные задачи.
func (s *sdeskSyncServiceImpl) processDeletions(ctx context.Context, metaClass string, toDelete []string, log *zap.Logger) {
	log.Info("Запуск процесса 'мягкого удаления' для устаревших записей", zap.Int("count", len(toDelete)))

	for _, uuid := range toDelete {
		var deleted bool
		var err error

		err = s.db.Transaction(func(tx *gorm.DB) error {
			switch metaClass {
			case "ou$company":
				deleted, err = s.companyRepo.Delete(ctx, tx, uuid)
			case "objectBase$Server":
				deleted, err = s.serverRepo.Delete(ctx, tx, uuid)
			case "objectBase$Workstation":
				deleted, err = s.workstationRepo.Delete(ctx, tx, uuid)
			case "objectBase$FR":
				deleted, err = s.frRepo.Delete(ctx, tx, uuid)
			default:
				return fmt.Errorf("неизвестный metaClass для удаления: %s", metaClass)
			}
			return err
		})

		if err != nil {
			log.Error("Ошибка при 'мягком удалении' сущности", zap.String("uuid", uuid), zap.Error(err))
			continue
		}

		if deleted {
			log.Info("Сущность успешно помечена как удаленная", zap.String("uuid", uuid))
			// После успешного удаления пытаемся закрыть задачу
			s.resolveDeletionTask(ctx, uuid, log)
		}
	}
}

// НОВАЯ ФУНКЦИЯ: resolveDeletionTask находит и закрывает задачу на удаление в SD.
func (s *sdeskSyncServiceImpl) resolveDeletionTask(ctx context.Context, entityUUID string, log *zap.Logger) {
	result := s.db.WithContext(ctx).Model(&models.ReconciliationTask{}).
		Where("entity_uuid = ? AND task_type = ? AND status = ?", entityUUID, "delete_from_servicedesk", "new").
		Update("status", "resolved")

	if result.Error != nil {
		log.Error("Ошибка при поиске и обновлении задачи на удаление", zap.String("uuid", entityUUID), zap.Error(result.Error))
		return
	}

	if result.RowsAffected > 0 {
		log.Info("Найдена и закрыта связанная задача на удаление из ServiceDesk", zap.String("uuid", entityUUID))
	}
}

// getLocalEntities извлекает из БД мапу с минимальной информацией о локальных сущностях.
func (s *sdeskSyncServiceImpl) getLocalEntities(ctx context.Context, metaClass string) (map[string]localEntityInfo, error) {
	infoMap := make(map[string]localEntityInfo)
	var err error
	switch metaClass {
	case "ou$company":
		entities, e := s.companyRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$Server":
		entities, e := s.serverRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$Workstation":
		entities, e := s.workstationRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$FR":
		entities, e := s.frRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	default:
		return nil, fmt.Errorf("неизвестный metaClass: %s", metaClass)
	}
	return infoMap, err
}

// processCreationsInParallel создает воркер-пул для создания сущностей.
func (s *sdeskSyncServiceImpl) processCreationsInParallel(ctx context.Context, metaClass string, toCreate []string, log *zap.Logger) {
	var wg sync.WaitGroup
	tasks := make(chan string, len(toCreate))

	for i := 0; i < s.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
				// Проверяем контекст перед каждой долгой операцией
				select {
				case <-ctx.Done():
					return
				default:
					details, err := s.sdClient.FetchEntityDetails(ctx, uuid, metaClass)
					if err != nil {
						if !errors.Is(err, context.Canceled) {
							log.Error("Не удалось получить детали для новой сущности", zap.String("uuid", uuid), zap.Error(err))
						}
						continue
					}
					s.createEntity(ctx, metaClass, details, log)
				}
			}
		}()
	}

	for _, uuid := range toCreate {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
}

// processUpdatesInParallel создает воркер-пул для обновления сущностей.
func (s *sdeskSyncServiceImpl) processUpdatesInParallel(ctx context.Context, metaClass string, toUpdate []string, log *zap.Logger) {
	var wg sync.WaitGroup
	tasks := make(chan string, len(toUpdate))

	for i := 0; i < s.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
				// Проверяем контекст перед каждой долгой операцией
				select {
				case <-ctx.Done():
					return
				default:
					details, err := s.sdClient.FetchEntityDetails(ctx, uuid, metaClass)
					if err != nil {
						if !errors.Is(err, context.Canceled) {
							log.Error("Не удалось получить детали для обновления сущности", zap.String("uuid", uuid), zap.Error(err))
						}
						continue
					}
					s.updateEntityIfNeeded(ctx, metaClass, uuid, details, log)
				}
			}
		}()
	}

	for _, uuid := range toUpdate {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
}

// createEntity маппит и создает новую сущность в БД.
func (s *sdeskSyncServiceImpl) createEntity(ctx context.Context, metaClass string, details map[string]interface{}, log *zap.Logger) {
	uuid, _ := details["UUID"].(string)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		switch metaClass {
		case "ou$company":
			company, mapErr := DataToCompany(ctx, details, s.sdClient, log)
			if mapErr != nil {
				return mapErr
			}
			return s.companyRepo.Create(ctx, tx, company)
		case "objectBase$Server":
			server, mapErr := DataToServer(details)
			if mapErr != nil {
				return mapErr
			}
			return s.serverRepo.Create(ctx, tx, server)
		case "objectBase$Workstation":
			ws, mapErr := DataToWorkstation(details)
			if mapErr != nil {
				return mapErr
			}
			return s.workstationRepo.Create(ctx, tx, ws)
		case "objectBase$FR":
			fr, mapErr := DataToFiscalRegister(details)
			if mapErr != nil {
				return mapErr
			}
			return s.frRepo.Create(ctx, tx, fr)
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка при создании сущности", zap.String("uuid", uuid), zap.Error(err))
	} else {
		log.Info("Сущность успешно создана", zap.String("uuid", uuid))
	}
}

// updateEntityIfNeeded выполняет "умное слияние".
func (s *sdeskSyncServiceImpl) updateEntityIfNeeded(ctx context.Context, metaClass, uuid string, details map[string]interface{}, log *zap.Logger) {
	var updates map[string]interface{}

	switch metaClass {
	case "ou$company":
		newData, mapErr := DataToCompany(ctx, details, s.sdClient, log)
		if mapErr != nil {
			log.Error("Ошибка маппинга компании", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		// ИСПРАВЛЕНИЕ: Используем Unscoped-метод, чтобы найти запись, даже если она удалена.
		currentData, getErr := s.companyRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущую компанию для сравнения", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		updates = getCompanyDiff(currentData, newData)

	case "objectBase$Server":
		newData, mapErr := DataToServer(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга сервера", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		// ИСПРАВЛЕНИЕ: Используем Unscoped-метод.
		currentData, getErr := s.serverRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущий сервер для сравнения", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		updates = getServerDiff(currentData, newData)

	case "objectBase$Workstation":
		newData, mapErr := DataToWorkstation(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга рабочей станции", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		// ИСПРАВЛЕНИЕ: Используем Unscoped-метод.
		currentData, getErr := s.workstationRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущую станцию для сравнения", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		updates = getWorkstationDiff(currentData, newData)

	case "objectBase$FR":
		newData, mapErr := DataToFiscalRegister(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга ФР", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		// ИСПРАВЛЕНИЕ: Используем Unscoped-метод.
		currentData, getErr := s.frRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущий ФР для сравнения", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		updates = getFiscalRegisterDiff(currentData, newData)

	default:
		log.Warn("Неизвестный metaClass для обновления", zap.String("metaClass", metaClass))
		return
	}

	if len(updates) > 0 {
		log.Info("Обнаружены изменения, обновление сущности", zap.String("uuid", uuid), zap.Any("changes", updates))
		err := s.db.Transaction(func(tx *gorm.DB) error {
			switch metaClass {
			case "ou$company":
				_, err := s.companyRepo.Update(ctx, tx, uuid, updates)
				return err
			case "objectBase$Server":
				_, err := s.serverRepo.Update(ctx, tx, uuid, updates)
				return err
			case "objectBase$Workstation":
				_, err := s.workstationRepo.Update(ctx, tx, uuid, updates)
				return err
			case "objectBase$FR":
				_, err := s.frRepo.Update(ctx, tx, uuid, updates)
				return err
			}
			return errors.New("неизвестный metaClass для транзакции обновления")
		})
		if err != nil {
			log.Error("Ошибка при обновлении сущности", zap.String("uuid", uuid), zap.Error(err))
		}
	}
}

// --- Вспомогательные функции для сравнения (diff) ---

func getCompanyDiff(current *models.Company, new *models.Company) map[string]interface{} {
	updates := make(map[string]interface{})
	if utils.SafeStringDereference(current.Title) != utils.SafeStringDereference(new.Title) {
		updates["title"] = new.Title
	}
	if utils.SafeStringDereference(current.Address) != utils.SafeStringDereference(new.Address) {
		updates["address"] = new.Address
	}
	if utils.SafeStringDereference(current.AdditionalName) != utils.SafeStringDereference(new.AdditionalName) {
		updates["additional_name"] = new.AdditionalName
	}
	if utils.SafeStringDereference(current.ParentServiceDeskUUID) != utils.SafeStringDereference(new.ParentServiceDeskUUID) {
		updates["parent_service_desk_uuid"] = new.ParentServiceDeskUUID
	}
	if (current.ActiveContract == nil && new.ActiveContract != nil) || (current.ActiveContract != nil && new.ActiveContract == nil) || (current.ActiveContract != nil && new.ActiveContract != nil && *current.ActiveContract != *new.ActiveContract) {
		updates["active_contract"] = new.ActiveContract
	}

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = nil
		}
	}

	if len(updates) == 0 {
		return nil
	}
	return updates
}

func getServerDiff(current *models.Server, new *models.Server) map[string]interface{} {
	updates := make(map[string]interface{})
	if utils.SafeStringDereference(current.UniqueID) != utils.SafeStringDereference(new.UniqueID) {
		updates["unique_id"] = new.UniqueID
	}
	if utils.SafeStringDereference(current.CRMid) != utils.SafeStringDereference(new.CRMid) {
		updates["crm_id"] = new.CRMid
	}
	if utils.SafeStringDereference(current.Teamviewer) != utils.SafeStringDereference(new.Teamviewer) {
		updates["teamviewer"] = new.Teamviewer
	}
	if utils.SafeStringDereference(current.RDP) != utils.SafeStringDereference(new.RDP) {
		updates["rdp"] = new.RDP
	}
	if utils.SafeStringDereference(current.Anydesk) != utils.SafeStringDereference(new.Anydesk) {
		updates["anydesk"] = new.Anydesk
	}
	if utils.SafeStringDereference(current.IP) != utils.SafeStringDereference(new.IP) {
		updates["ip"] = new.IP
	}
	if utils.SafeStringDereference(current.CabinetLink) != utils.SafeStringDereference(new.CabinetLink) {
		updates["cabinet_link"] = new.CabinetLink
	}
	if utils.SafeStringDereference(current.DeviceName) != utils.SafeStringDereference(new.DeviceName) {
		updates["device_name"] = new.DeviceName
	}
	if utils.SafeStringDereference(current.Litemanager) != utils.SafeStringDereference(new.Litemanager) {
		updates["litemanager"] = new.Litemanager
	}
	if utils.SafeStringDereference(current.ServerVersion) != utils.SafeStringDereference(new.ServerVersion) {
		updates["iiko_version"] = new.ServerVersion
	}
	if utils.SafeStringDereference(current.Description) != utils.SafeStringDereference(new.Description) {
		updates["description"] = new.Description
	}
	if utils.SafeStringDereference(current.OwnerServiceDeskUUID) != utils.SafeStringDereference(new.OwnerServiceDeskUUID) {
		updates["owner_service_desk_uuid"] = new.OwnerServiceDeskUUID
	}

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = nil
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return updates
}

func getWorkstationDiff(current *models.Workstation, new *models.Workstation) map[string]interface{} {
	updates := make(map[string]interface{})
	if utils.SafeStringDereference(current.Teamviewer) != utils.SafeStringDereference(new.Teamviewer) {
		updates["teamviewer"] = new.Teamviewer
	}
	if utils.SafeStringDereference(current.Anydesk) != utils.SafeStringDereference(new.Anydesk) {
		updates["anydesk"] = new.Anydesk
	}
	if utils.SafeStringDereference(current.Litemanager) != utils.SafeStringDereference(new.Litemanager) {
		updates["litemanager"] = new.Litemanager
	}
	if utils.SafeStringDereference(current.DeviceName) != utils.SafeStringDereference(new.DeviceName) {
		updates["device_name"] = new.DeviceName
	}
	if utils.SafeStringDereference(current.Description) != utils.SafeStringDereference(new.Description) {
		updates["description"] = new.Description
	}
	if utils.SafeStringDereference(current.OwnerServiceDeskUUID) != utils.SafeStringDereference(new.OwnerServiceDeskUUID) {
		updates["owner_service_desk_uuid"] = new.OwnerServiceDeskUUID
	}

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = nil
		}
	}

	if len(updates) == 0 {
		return nil
	}
	return updates
}

func getFiscalRegisterDiff(current *models.FiscalRegister, new *models.FiscalRegister) map[string]interface{} {
	updates := make(map[string]interface{})
	if utils.SafeStringDereference(current.ModelKKT) != utils.SafeStringDereference(new.ModelKKT) {
		updates["model_kkt"] = new.ModelKKT
	}
	if utils.SafeStringDereference(current.FFD) != utils.SafeStringDereference(new.FFD) {
		updates["ffd"] = new.FFD
	}
	if utils.SafeStringDereference(current.FRDownloader) != utils.SafeStringDereference(new.FRDownloader) {
		updates["fr_downloader"] = new.FRDownloader
	}
	if utils.SafeStringDereference(current.RNKKT) != utils.SafeStringDereference(new.RNKKT) {
		updates["rn_kkt"] = new.RNKKT
	}
	if utils.SafeStringDereference(current.LegalName) != utils.SafeStringDereference(new.LegalName) {
		updates["legal_name"] = new.LegalName
	}
	if utils.SafeStringDereference(current.INN) != utils.SafeStringDereference(new.INN) {
		updates["inn"] = new.INN
	}
	if utils.SafeStringDereference(current.FRSerialNumber) != utils.SafeStringDereference(new.FRSerialNumber) {
		updates["fr_serial_number"] = new.FRSerialNumber
	}
	if utils.SafeStringDereference(current.FNNumber) != utils.SafeStringDereference(new.FNNumber) {
		updates["fn_number"] = new.FNNumber
	}
	if utils.SafeStringDereference(current.OwnerServiceDeskUUID) != utils.SafeStringDereference(new.OwnerServiceDeskUUID) {
		updates["owner_service_desk_uuid"] = new.OwnerServiceDeskUUID
	}

	if (current.KKTRegDate == nil && new.KKTRegDate != nil) || (current.KKTRegDate != nil && new.KKTRegDate == nil) || (current.KKTRegDate != nil && new.KKTRegDate != nil && !current.KKTRegDate.Equal(*new.KKTRegDate)) {
		updates["kkt_reg_date"] = new.KKTRegDate
	}
	if (current.FNExpireDate == nil && new.FNExpireDate != nil) || (current.FNExpireDate != nil && new.FNExpireDate == nil) || (current.FNExpireDate != nil && new.FNExpireDate != nil && !current.FNExpireDate.Equal(*new.FNExpireDate)) {
		updates["fn_expire_date"] = new.FNExpireDate
	}

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = nil
		}
	}

	if len(updates) == 0 {
		return nil
	}
	return updates
}
