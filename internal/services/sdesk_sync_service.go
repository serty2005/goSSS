package services

import (
	"context"
	"errors"
	"etalon-server/internal/config"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	db              *gorm.DB // Для транзакций
	mu              sync.Mutex
	isSyncing       bool
}

// ИСПРАВЛЕНИЕ: Внутренняя структура для хранения данных из локальной БД
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

	// Первый запуск сразу
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

	// Синхронизируем сущности последовательно для соблюдения целостности внешних ключей.
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

	// 1. Получаем минимальные данные от ServiceDesk
	remoteList, err := s.sdClient.FetchEntityList(ctx, metaClass, false)
	if err != nil {
		log.Error("Не удалось получить список сущностей из ServiceDesk", zap.Error(err))
		return
	}
	log.Info("Получен список из ServiceDesk", zap.Int("count", len(remoteList)))

	// 2. Получаем локальные данные (включая удаленные)
	localMap, err := s.getLocalEntities(ctx, metaClass)
	if err != nil {
		log.Error("Не удалось получить локальные сущности", zap.Error(err))
		return
	}
	log.Info("Получен список из локальной БД", zap.Int("count", len(localMap)))

	var toCreate, toUpdate []string

	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteLMDStr, _ := remoteItem["lastModifiedDate"].(string)
		remoteLMD := utils.ParseServiceDeskTime(remoteLMDStr)
		if remoteLMD == nil {
			log.Warn("Пропуск сущности из-за отсутствия даты модификации", zap.String("uuid", remoteUUID))
			continue
		}

		localEntity, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
		} else {
			// ИСПРАВЛЕНИЕ: Используем поля из новой структуры localEntityInfo
			if localEntity.DeletedAt.Valid || (localEntity.LastModifiedDate != nil && remoteLMD.After(*localEntity.LastModifiedDate)) {
				toUpdate = append(toUpdate, remoteUUID)
			}
		}
	}
	log.Info("Сравнение завершено", zap.Int("to_create", len(toCreate)), zap.Int("to_update", len(toUpdate)))

	// 3. Используем воркер-пул для параллельной загрузки деталей
	if len(toCreate) > 0 || len(toUpdate) > 0 {
		s.processEntitiesInParallel(ctx, metaClass, toCreate, toUpdate, log)
	} else {
		log.Info("Нет сущностей для создания или обновления.")
	}
}

// getLocalEntities - вспомогательная функция для получения локальных данных.
func (s *sdeskSyncServiceImpl) getLocalEntities(ctx context.Context, metaClass string) (map[string]localEntityInfo, error) {
	infoMap := make(map[string]localEntityInfo)
	var err error

	switch metaClass {
	case "ou$company":
		entities, e := s.companyRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{
					LastModifiedDate: entity.LastModifiedDate,
					DeletedAt:        entity.DeletedAt,
				}
			}
		}
	case "objectBase$Server":
		entities, e := s.serverRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{
					LastModifiedDate: entity.LastModifiedDate,
					DeletedAt:        entity.DeletedAt,
				}
			}
		}
	case "objectBase$Workstation":
		entities, e := s.workstationRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{
					LastModifiedDate: entity.LastModifiedDate,
					DeletedAt:        entity.DeletedAt,
				}
			}
		}
	case "objectBase$FR":
		entities, e := s.frRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{
					LastModifiedDate: entity.LastModifiedDate,
					DeletedAt:        entity.DeletedAt,
				}
			}
		}
	default:
		return nil, fmt.Errorf("неизвестный metaClass: %s", metaClass)
	}
	return infoMap, err
}

// processEntitiesInParallel создает воркер-пул для обработки UUID.
func (s *sdeskSyncServiceImpl) processEntitiesInParallel(ctx context.Context, metaClass string, toCreate, toUpdate []string, log *zap.Logger) {
	tasks := make(chan string, len(toCreate)+len(toUpdate))
	var wg sync.WaitGroup

	log.Info("Запуск воркеров для загрузки деталей", zap.Int("worker_count", s.cfg.WorkerCount))

	for i := 0; i < s.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for uuid := range tasks {
				select {
				case <-ctx.Done():
					return // Прерываем работу, если контекст отменен
				default:
					details, err := s.sdClient.FetchEntityDetails(ctx, uuid, metaClass)
					if err != nil {
						log.Error("Не удалось получить детали сущности", zap.String("uuid", uuid), zap.Int("worker", workerID), zap.Error(err))
						continue
					}
					s.upsertEntity(ctx, metaClass, uuid, details, log)
				}
			}
		}(i)
	}

	for _, uuid := range toCreate {
		tasks <- uuid
	}
	for _, uuid := range toUpdate {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
	log.Info("Загрузка деталей и обновление БД завершены.")
}

// upsertEntity создает или обновляет сущность в БД, используя OnConflict (UPSERT).
func (s *sdeskSyncServiceImpl) upsertEntity(ctx context.Context, metaClass, uuid string, details map[string]interface{}, log *zap.Logger) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		switch metaClass {
		case "ou$company":
			company, mapErr := DataToCompany(ctx, details, s.sdClient, s.logger)
			if mapErr != nil {
				return mapErr
			}
			// Устанавливаем deleted_at в NULL для "восстановления" записи
			company.DeletedAt = gorm.DeletedAt{}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "service_desk_uuid"}}, DoUpdates: clause.AssignmentColumns(getCompanyUpdateFields())}).Create(company).Error
		case "objectBase$Server":
			server, mapErr := DataToServer(details)
			if mapErr != nil {
				return mapErr
			}
			server.DeletedAt = gorm.DeletedAt{}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "service_desk_uuid"}}, DoUpdates: clause.AssignmentColumns(getServerUpdateFields())}).Create(server).Error
		case "objectBase$Workstation":
			workstation, mapErr := DataToWorkstation(details)
			if mapErr != nil {
				return mapErr
			}
			workstation.DeletedAt = gorm.DeletedAt{}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "service_desk_uuid"}}, DoUpdates: clause.AssignmentColumns(getWorkstationUpdateFields())}).Create(workstation).Error
		case "objectBase$FR":
			fr, mapErr := DataToFiscalRegister(details)
			if mapErr != nil {
				return mapErr
			}
			fr.DeletedAt = gorm.DeletedAt{}
			return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "service_desk_uuid"}}, DoUpdates: clause.AssignmentColumns(getFiscalRegisterUpdateFields())}).Create(fr).Error
		default:
			return errors.New("неизвестный metaClass для upsert")
		}
	})

	if err != nil {
		log.Error("Ошибка при создании/обновлении сущности", zap.String("uuid", uuid), zap.Error(err))
	} else {
		log.Debug("Сущность успешно создана/обновлена", zap.String("uuid", uuid))
	}
}

// Вспомогательные функции для получения списка полей для обновления в OnConflict.
func getCompanyUpdateFields() []string {
	return []string{"title", "address", "active_contract", "last_modified_date", "additional_name", "parent_service_desk_uuid", "updated_at", "deleted_at"}
}

func getServerUpdateFields() []string {
	return []string{"unique_id", "crm_id", "teamviewer", "rdp", "anydesk", "ip", "cabinet_link", "device_name", "last_modified_date", "litemanager", "iiko_version", "description", "owner_service_desk_uuid", "updated_at", "deleted_at"}
}

func getWorkstationUpdateFields() []string {
	return []string{"teamviewer", "anydesk", "litemanager", "device_name", "last_modified_date", "description", "owner_service_desk_uuid", "updated_at", "deleted_at"}
}

func getFiscalRegisterUpdateFields() []string {
	return []string{"model_kkt", "ffd", "fr_downloader", "rn_kkt", "legal_name", "fr_serial_number", "fn_number", "kkt_reg_date", "fn_expire_date", "last_modified_date", "owner_service_desk_uuid", "updated_at", "deleted_at"}
}
