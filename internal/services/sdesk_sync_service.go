// internal/services/sdesk_sync_service.go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
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
	ContractInfo     datatypes.JSON
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

	agreementCache := make(map[string]*AgreementDetailsDTO)
	cycleCtx := context.WithValue(ctx, agreementCacheKey, agreementCache)

	s.syncEntityType(cycleCtx, "ou$company")
	s.syncEntityType(cycleCtx, "objectBase$Server")
	s.syncEntityType(cycleCtx, "objectBase$Workstation")
	s.syncEntityType(cycleCtx, "objectBase$FR")

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

	// ИЗМЕНЕНИЕ: Запрашиваем из локальной БД расширенную информацию
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

		localEntity, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
			continue
		}

		// --- НАЧАЛО НОВОЙ ЛОГИКИ ПРИНЯТИЯ РЕШЕНИЯ ОБ ОБНОВЛЕНИИ ---
		needsUpdate := false
		remoteLMD := utils.ParseServiceDeskTime(remoteItem["lastModifiedDate"].(string))

		// 1. Проверяем, не была ли сущность удалена у нас, а в SD снова появилась
		if localEntity.DeletedAt.Valid {
			needsUpdate = true
		}

		// 2. Стандартная проверка по дате модификации
		if !needsUpdate && remoteLMD != nil && localEntity.LastModifiedDate != nil && remoteLMD.After(*localEntity.LastModifiedDate) {
			needsUpdate = true
		}

		// 3. Специальная проверка для компаний по составу контрактов
		if !needsUpdate && metaClass == "ou$company" {
			remoteAgreements := getAgreementUUIDsFromRemote(remoteItem)
			localAgreements := getAgreementUUIDsFromLocal(localEntity.ContractInfo)
			if !areStringSlicesEqual(remoteAgreements, localAgreements) {
				log.Debug("Обнаружено изменение в составе контрактов, требуется обновление", zap.String("uuid", remoteUUID))
				needsUpdate = true
			}
		}
		// --- КОНЕЦ НОВОЙ ЛОГИКИ ---

		if needsUpdate {
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
			s.resolveDeletionTask(ctx, uuid, log)
		}
	}
}

// resolveDeletionTask находит и закрывает задачу на удаление в SD.
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
		// ИЗМЕНЕНИЕ: Используем более общий метод, чтобы получить и ContractInfo
		var companies []models.Company
		err = s.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at", "contract_info").Find(&companies).Error
		if err == nil {
			for _, entity := range companies {
				infoMap[*entity.ServiceDeskUUID] = localEntityInfo{
					LastModifiedDate: entity.LastModifiedDate,
					DeletedAt:        entity.DeletedAt,
					ContractInfo:     entity.ContractInfo,
				}
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

// processUpdatesInParallel создает воркер-пул для проверки и создания задач о конфликтах.
func (s *sdeskSyncServiceImpl) processUpdatesInParallel(ctx context.Context, metaClass string, toUpdate []string, log *zap.Logger) {
	var wg sync.WaitGroup
	tasks := make(chan string, len(toUpdate))

	for i := 0; i < s.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
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
					s.checkEntityAndCreateTaskIfNeeded(ctx, metaClass, uuid, details, log)
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

// checkEntityAndCreateTaskIfNeeded выполняет сравнение данных и решает,
// нужно ли восстановить запись, создать задачу о конфликте или ничего не делать.
func (s *sdeskSyncServiceImpl) checkEntityAndCreateTaskIfNeeded(ctx context.Context, metaClass, uuid string, details map[string]interface{}, log *zap.Logger) {
	var updates map[string]interface{}
	var diffLog []zap.Field
	var currentEntity interface{}

	// 1. Получаем текущую и новую версии сущности для сравнения
	switch metaClass {
	case "ou$company":
		newData, mapErr := DataToCompany(ctx, details, s.sdClient, log)
		if mapErr != nil {
			log.Error("Ошибка маппинга компании", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.companyRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущую компанию", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getCompanyDiff(currentData, newData)

	case "objectBase$Server":
		newData, mapErr := DataToServer(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга сервера", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.serverRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущий сервер", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getServerDiff(currentData, newData)

	case "objectBase$Workstation":
		newData, mapErr := DataToWorkstation(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга рабочей станции", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.workstationRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущую станцию", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getWorkstationDiff(currentData, newData)

	case "objectBase$FR":
		newData, mapErr := DataToFiscalRegister(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга ФР", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.frRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущий ФР", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getFiscalRegisterDiff(currentData, newData)

	default:
		log.Warn("Неизвестный metaClass для проверки", zap.String("metaClass", metaClass))
		return
	}

	// 2. Принимаем решение на основе найденных изменений
	if len(updates) == 0 {
		s.resolveConflictTaskIfNeeded(ctx, uuid, log)
		return
	}

	_, isRestorationOnly := updates["deleted_at"]
	if len(updates) == 1 && isRestorationOnly {
		// Сценарий 1: Только восстановление. Выполняем автоматически.
		log.Info("Обнаружена восстановленная в SD сущность. Автоматическое восстановление.", append(diffLog, zap.String("uuid", uuid))...)
		s.performUpdate(ctx, metaClass, uuid, updates, log)
	} else {
		// Сценарий 2: Есть другие расхождения. Создаем задачу.
		log.Warn("Обнаружено расхождение данных между локальной БД и ServiceDesk. Создание задачи.", append(diffLog, zap.String("uuid", uuid))...)
		s.createConflictTask(ctx, metaClass, uuid, currentEntity, details, diffLog, log)
	}
}

// performUpdate выполняет обновление сущности в БД.
func (s *sdeskSyncServiceImpl) performUpdate(ctx context.Context, metaClass, uuid string, updates map[string]interface{}, log *zap.Logger) {
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
		log.Error("Ошибка при автоматическом восстановлении сущности", zap.String("uuid", uuid), zap.Error(err))
	}
}

// createConflictTask создает задачу о расхождении данных.
func (s *sdeskSyncServiceImpl) createConflictTask(ctx context.Context, metaClass, uuid string, currentEntity interface{}, remoteDetails map[string]interface{}, diffLog []zap.Field, log *zap.Logger) {
	detailsMap := make(map[string]interface{})
	diffs := make(map[string]string)
	for _, field := range diffLog {
		if field.Key == "status" && field.String == "deleted -> restored" {
			continue
		}
		diffs[field.Key] = field.String
	}

	if len(diffs) == 0 {
		return
	}

	detailsMap["conflicts"] = diffs
	detailsMap["local_entity"] = currentEntity
	detailsMap["remote_entity"] = remoteDetails
	detailsJSON, _ := json.Marshal(detailsMap)

	entityType := metaClass
	if parts := strings.Split(metaClass, "$"); len(parts) > 1 {
		entityType = parts[1]
	}
	comment := fmt.Sprintf("Обнаружено расхождение данных для сущности '%s' (%s). Требуется ручная сверка.", uuid, entityType)

	task := models.ReconciliationTask{
		TaskType:   "data_conflict",
		EntityType: entityType,
		EntityUUID: uuid,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    comment,
	}

	err := s.db.WithContext(ctx).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", uuid, "data_conflict").
		FirstOrCreate(&task).Error

	if err != nil {
		log.Error("Не удалось создать или найти задачу о конфликте данных", zap.String("uuid", uuid), zap.Error(err))
	}
}

// resolveConflictTaskIfNeeded автоматически закрывает задачу, если конфликт устранен.
func (s *sdeskSyncServiceImpl) resolveConflictTaskIfNeeded(ctx context.Context, uuid string, log *zap.Logger) {
	result := s.db.WithContext(ctx).Model(&models.ReconciliationTask{}).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", uuid, "data_conflict").
		Updates(map[string]interface{}{
			"status":  "resolved",
			"comment": gorm.Expr("comment || ?", "\n[АВТОМАТИЧЕСКИ] Конфликт устранен, данные синхронизированы."),
		})

	if result.Error != nil {
		log.Error("Ошибка при попытке автоматического разрешения задачи о конфликте", zap.String("uuid", uuid), zap.Error(result.Error))
		return
	}

	if result.RowsAffected > 0 {
		log.Info("Конфликт данных устранен. Существующая задача автоматически разрешена.", zap.String("uuid", uuid))
	}
}

// --- Вспомогательные функции для сравнения (diff) ---

// formatDiffValue безопасно форматирует значение (включая nil-указатели) для логирования.
func formatDiffValue(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return "<nil>"
		}
		return fmt.Sprintf("'%v'", val.Elem().Interface())
	}
	return fmt.Sprintf("'%v'", v)
}

// compareAndLog - универсальная функция для сравнения полей и логирования расхождений.
// ИСПРАВЛЕНИЕ: Дженерик-тип изменен на comparable для корректной работы оператора !=
func compareAndLog[T comparable](updates map[string]interface{}, diffs *[]zap.Field, key string, current, new *T) {
	currentVal := reflect.ValueOf(current)
	newVal := reflect.ValueOf(new)

	isCurrentNil := current == nil || currentVal.IsNil()
	isNewNil := new == nil || newVal.IsNil()

	if isCurrentNil && isNewNil {
		return
	}

	if isCurrentNil != isNewNil {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
		return
	}

	if *current != *new {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
	}
}

// compareTimeAndLog - специальная функция для сравнения time.Time.
func compareTimeAndLog(updates map[string]interface{}, diffs *[]zap.Field, key string, current, new *time.Time) {
	if current == nil && new == nil {
		return
	}
	if (current == nil && new != nil) || (current != nil && new == nil) || (current != nil && new != nil && !current.Equal(*new)) {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
	}
}

// canonicalJSON принимает JSON в виде []byte и возвращает его каноническую (компактную) форму.
// Это необходимо для корректного сравнения JSON-объектов, игнорируя пробелы и форматирование.
func canonicalJSON(jsonData []byte) []byte {
	if jsonData == nil {
		return nil
	}
	buffer := new(bytes.Buffer)
	// Compact убирает все незначимые пробелы из JSON
	if err := json.Compact(buffer, jsonData); err != nil {
		// В случае ошибки возвращаем исходный слайс байт
		return jsonData
	}
	return buffer.Bytes()
}

// getCompanyDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Добавлена сверка ContractInfo.
func getCompanyDiff(current *models.Company, new *models.Company) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareAndLog(updates, &diffs, "title", current.Title, new.Title)
	compareAndLog(updates, &diffs, "address", current.Address, new.Address)
	compareAndLog(updates, &diffs, "active_contract", current.ActiveContract, new.ActiveContract)

	// ИЗМЕНЕНИЕ: Сравниваем канонические представления JSON-полей
	currentContractInfo := canonicalJSON(current.ContractInfo)
	newContractInfo := canonicalJSON(new.ContractInfo)

	if !bytes.Equal(currentContractInfo, newContractInfo) {
		updates["contract_info"] = new.ContractInfo
		diffs = append(diffs, zap.String("contract_info", fmt.Sprintf("'%s' -> '%s'", string(current.ContractInfo), string(new.ContractInfo))))
	}

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

// getServerDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только указанные поля.
func getServerDiff(current *models.Server, new *models.Server) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareAndLog(updates, &diffs, "unique_id", current.UniqueID, new.UniqueID)
	compareAndLog(updates, &diffs, "rdp", current.RDP, new.RDP)
	compareAndLog(updates, &diffs, "server_version", current.ServerVersion, new.ServerVersion)

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

// getWorkstationDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только ID удаленного доступа.
func getWorkstationDiff(current *models.Workstation, new *models.Workstation) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareAndLog(updates, &diffs, "teamviewer", current.Teamviewer, new.Teamviewer)
	compareAndLog(updates, &diffs, "anydesk", current.Anydesk, new.Anydesk)
	compareAndLog(updates, &diffs, "litemanager", current.Litemanager, new.Litemanager)

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

// getFiscalRegisterDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только дату окончания ФН.
func getFiscalRegisterDiff(current *models.FiscalRegister, new *models.FiscalRegister) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareTimeAndLog(updates, &diffs, "fn_expire_date", current.FNExpireDate, new.FNExpireDate)

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

func getAgreementUUIDsFromRemote(remoteItem map[string]interface{}) []string {
	var uuids []string
	if agreements, ok := remoteItem["recipientAgreements"].([]interface{}); ok {
		for _, agr := range agreements {
			if agrMap, agrOk := agr.(map[string]interface{}); agrOk {
				if metaClass, mcOk := agrMap["metaClass"].(string); mcOk && metaClass == "agreement$agreement" {
					if agrUUID, uuidOk := agrMap["UUID"].(string); uuidOk {
						uuids = append(uuids, agrUUID)
					}
				}
			}
		}
	}
	return uuids
}

func getAgreementUUIDsFromLocal(contractInfoJSON datatypes.JSON) []string {
	if contractInfoJSON == nil {
		return nil
	}
	var info struct {
		ActiveContractIDs []string `json:"active_contract_ids"`
	}
	if err := json.Unmarshal(contractInfoJSON, &info); err != nil {
		return nil
	}
	return info.ActiveContractIDs
}

func areStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
