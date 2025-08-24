// internal/services/contract_sync_service.go
package services

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ContractSyncService отвечает за фоновую синхронизацию контрактов с ServiceDesk.
type ContractSyncService interface {
	Start(ctx context.Context)
	RunSyncCycle(ctx context.Context) error
}

type contractSyncServiceImpl struct {
	cfg          *config.Config
	sdClient     ServiceDeskClient
	contractRepo repositories.ContractRepo
	logger       *zap.Logger
	db           *gorm.DB
	mu           sync.Mutex
	isSyncing    bool
}

// NewContractSyncService создает новый экземпляр сервиса.
func NewContractSyncService(
	cfg *config.Config,
	db *gorm.DB,
	sdClient ServiceDeskClient,
	contractRepo repositories.ContractRepo,
	logger *zap.Logger,
) ContractSyncService {
	return &contractSyncServiceImpl{
		cfg:          cfg,
		db:           db,
		sdClient:     sdClient,
		contractRepo: contractRepo,
		logger:       logger,
	}
}

// Start запускает воркер в фоновом режиме.
func (s *contractSyncServiceImpl) Start(ctx context.Context) {
	interval := s.cfg.ContractSyncInterval
	s.logger.Info("Запуск воркера синхронизации контрактов", zap.Duration("interval", interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := s.RunSyncCycle(ctx); err != nil {
		s.logger.Error("Первый запуск синхронизации контрактов завершился с ошибкой", zap.Error(err))
	}

	for {
		select {
		case <-ticker.C:
			if err := s.RunSyncCycle(ctx); err != nil {
				s.logger.Error("Цикл синхронизации контрактов завершился с ошибкой", zap.Error(err))
			}
		case <-ctx.Done():
			s.logger.Info("Остановка воркера синхронизации контрактов...")
			return
		}
	}
}

// RunSyncCycle выполняет один цикл синхронизации.
func (s *contractSyncServiceImpl) RunSyncCycle(ctx context.Context) error {
	s.mu.Lock()
	if s.isSyncing {
		s.logger.Warn("Цикл синхронизации контрактов уже запущен. Пропуск.")
		s.mu.Unlock()
		return nil
	}
	s.isSyncing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isSyncing = false
		s.mu.Unlock()
	}()

	s.logger.Info("Начало нового цикла синхронизации контрактов.")

	// Шаг 1: Синхронизируем сами сущности контрактов
	err := s.syncContractEntities(ctx)
	if err != nil {
		return err
	}

	// Шаг 2: Пересчитываем статусы активности для всех компаний на основе свежих данных о контрактах
	err = s.recalculateAllCompanyStatuses(ctx)
	if err != nil {
		return err
	}

	s.logger.Info("Цикл синхронизации контрактов завершен.")
	return nil
}

// syncContractEntities синхронизирует локальную таблицу контрактов с ServiceDesk.
func (s *contractSyncServiceImpl) syncContractEntities(ctx context.Context) error {
	metaClass := "agreement$agreement"
	log := s.logger.With(zap.String("metaClass", metaClass))

	remoteList, err := s.sdClient.FetchEntityList(ctx, metaClass, true)
	if err != nil {
		log.Error("Не удалось получить список контрактов из ServiceDesk", zap.Error(err))
		return err
	}

	localMap, err := s.contractRepo.GetAllUUIDsAndDates(ctx)
	if err != nil {
		log.Error("Не удалось получить локальные контракты", zap.Error(err))
		return err
	}

	for _, remoteData := range remoteList {
		uuid, _ := remoteData["UUID"].(string)
		if uuid == "" {
			continue
		}

		contract, err := DataToContract(remoteData)
		if err != nil {
			log.Warn("Ошибка маппинга контракта, пропуск", zap.String("uuid", uuid), zap.Error(err))
			continue
		}

		localContract, exists := localMap[uuid]
		if !exists {
			// Создаем новый контракт
			if err := s.contractRepo.Create(ctx, nil, contract); err != nil {
				log.Error("Не удалось создать контракт", zap.String("uuid", uuid), zap.Error(err))
			}
		} else {
			// Обновляем существующий, если дата новее или он был удален
			remoteLMD := utils.ParseServiceDeskTime(remoteData["lastModifiedDate"].(string))
			if localContract.DeletedAt.Valid || (remoteLMD != nil && localContract.LastModifiedDate != nil && remoteLMD.After(*localContract.LastModifiedDate)) {
				updateData := map[string]interface{}{
					"state":              contract.State,
					"state_start_time":   contract.StateStartTime,
					"services":           contract.Services,
					"recipients":         contract.Recipients,
					"last_modified_date": contract.LastModifiedDate,
					"deleted_at":         gorm.Expr("NULL"),
				}
				if _, err := s.contractRepo.Update(ctx, nil, uuid, updateData); err != nil {
					log.Error("Не удалось обновить контракт", zap.String("uuid", uuid), zap.Error(err))
				}
			}
		}
	}
	return nil
}

// recalculateAllCompanyStatuses пересчитывает статусы ActiveContract для всех компаний.
func (s *contractSyncServiceImpl) recalculateAllCompanyStatuses(ctx context.Context) error {
	s.logger.Info("Пересчет статусов ActiveContract для всех компаний...")

	// Шаг 1: Получаем все контракты из SD (они уже должны быть синхронизированы локально, но для надежности берем из SD)
	remoteContracts, err := s.sdClient.FetchEntityList(ctx, "agreement$agreement", true)
	if err != nil {
		s.logger.Error("Не удалось получить контракты для пересчета статусов", zap.Error(err))
		return err
	}

	// Шаг 2: Собираем сет UUID всех компаний с активными контрактами
	activeCompanyUUIDs := make(map[string]struct{})
	for _, contractData := range remoteContracts {
		state, _ := contractData["state"].(string)
		if state == "active" {
			companyUUIDs := GetCompanyUUIDsFromContract(contractData)
			for _, uuid := range companyUUIDs {
				activeCompanyUUIDs[uuid] = struct{}{}
			}
		}
	}

	if len(activeCompanyUUIDs) == 0 {
		s.logger.Warn("Не найдено ни одной компании с активным контрактом. Все компании будут помечены как неактивные.")
	}

	// Шаг 3: Выполняем массовые обновления в транзакции
	return s.db.Transaction(func(tx *gorm.DB) error {
		activeUUIDsList := make([]string, 0, len(activeCompanyUUIDs))
		for uuid := range activeCompanyUUIDs {
			activeUUIDsList = append(activeUUIDsList, uuid)
		}

		// Обновляем на TRUE тех, кто есть в списке
		if len(activeUUIDsList) > 0 {
			res := tx.WithContext(ctx).Model(&models.Company{}).Where("service_desk_uuid IN ?", activeUUIDsList).Update("active_contract", true)
			if res.Error != nil {
				return res.Error
			}
			s.logger.Info("Установлен статус ActiveContract=true для компаний", zap.Int("count", int(res.RowsAffected)))
		}

		// Обновляем на FALSE всех остальных
		res := tx.WithContext(ctx).Model(&models.Company{}).Where("service_desk_uuid NOT IN ?", activeUUIDsList).Update("active_contract", false)
		if res.Error != nil {
			return res.Error
		}
		s.logger.Info("Установлен статус ActiveContract=false для компаний", zap.Int("count", int(res.RowsAffected)))

		return nil
	})
}
