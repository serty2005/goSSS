package contract

import (
	"context"
	"encoding/json"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/db"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type serviceImpl struct {
	logger          logger.LoggerInterface
	tm              interfaces.Transactor
	sdClient        external.ExternalSystemClient
	contractRepo    contract.Repository
	companyRepo     company.Repository
	linkRepo        repositories.LinkRepo
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

// NewService создает новый экземпляр сервиса контрактов.
func NewService(
	logger logger.LoggerInterface,
	tm interfaces.Transactor,
	sdClient external.ExternalSystemClient,
	contractRepo contract.Repository,
	companyRepo company.Repository,
	linkRepo repositories.LinkRepo,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
) contract.Service {
	return &serviceImpl{
		logger:          logger,
		tm:              tm,
		sdClient:        sdClient,
		contractRepo:    contractRepo,
		companyRepo:     companyRepo,
		linkRepo:        linkRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// SyncContracts выполняет полную синхронизацию контрактов и пересчет статусов.
func (s *serviceImpl) SyncContracts(ctx context.Context, rawData []map[string]interface{}) error {
	s.logger.Info("Начало синхронизации контрактов", "count", len(rawData))

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		// Получаем текущую транзакцию для MapperContext
		tx := db.ExtractDB(txCtx, nil)
		mapperCtx := &external.MapperContext{
			DB:       tx,
			LinkRepo: s.linkRepo,
			Logger:   s.logger,
		}

		// Сет для хранения ID компаний, затронутых изменениями, чтобы потом пересчитать их статус
		affectedCompanyIDs := make(map[string]struct{})

		for _, data := range rawData {
			// 1. Маппинг данных
			contractModel, err := s.sdClient.Mapper().DataToContract(txCtx, mapperCtx, data)
			if err != nil {
				s.logger.Error("Ошибка маппинга контракта", "error", err)
				continue
			}

			// Извлекаем UUID из данных (Naumen specific field usually)
			extUUID, _ := data["UUID"].(string)
			if extUUID == "" {
				continue
			}

			// 2. Upsert контракта
			// Пытаемся найти существующий контракт по внешнему ID
			existing, _ := s.contractRepo.GetByServiceDeskUUID(txCtx, extUUID)
			if existing != nil {
				contractModel.ID = existing.ID // Сохраняем внутренний ID для обновления
				// Обновляем поля через репозиторий (можно оптимизировать update map, но пока save)
				// Для простоты используем Create, который в GORM работает как Upsert если ID задан,
				// но лучше использовать явный Update или Save в репо.
				// В данном случае contractRepo.Create делает просто Create.
				// Реализуем Upsert логику здесь или полагаемся на то, что Create с ID обновит запись.
				// Для надежности удалим системные поля и обновим.
				updates := map[string]interface{}{
					"state":              contractModel.State,
					"state_start_time":   contractModel.StateStartTime,
					"services":           contractModel.Services,
					"recipients":         contractModel.Recipients,
					"service_level":      contractModel.ServiceLevel,
					"last_modified_date": contractModel.LastModifiedDate,
					"last_updated_by":    "contract_sync",
				}
				if _, err := s.contractRepo.Update(txCtx, existing.ID, updates); err != nil {
					return fmt.Errorf("ошибка обновления контракта %s: %w", existing.ID, err)
				}
			} else {
				// Создаем новый
				if err := s.contractRepo.Create(txCtx, contractModel); err != nil {
					return fmt.Errorf("ошибка создания контракта: %w", err)
				}
				// Создаем связь
				link := &models.ExternalSystemLink{
					InternalID:      contractModel.ID,
					SystemName:      "naumen",
					ServiceDeskUUID: extUUID,
					EntityType:      "Contract",
					LastSyncedAt:    time.Now(),
				}
				if err := s.linkRepo.Create(txCtx, tx, link); err != nil {
					return fmt.Errorf("ошибка создания связи для контракта: %w", err)
				}
			}

			// 3. Обновление связей с компаниями (Recipients)
			companyExtUUIDs := s.sdClient.Mapper().GetCompanyUUIDsFromContract(data)
			var companyIntIDs []string

			for _, compExtID := range companyExtUUIDs {
				internalID, err := s.linkRepo.FindInternalIDByExternalID(txCtx, tx, "naumen", compExtID)
				if err == nil && internalID != "" {
					companyIntIDs = append(companyIntIDs, internalID)
					affectedCompanyIDs[internalID] = struct{}{}
				}
			}

			if len(companyIntIDs) > 0 {
				s.logger.Debug("Контракт связан с компаниями", "contract_id", contractModel.ID, "companies_count", len(companyIntIDs))
			}

			// Заменяем связи Many-to-Many
			if err := s.contractRepo.ReplaceCompanyLinks(txCtx, contractModel, companyIntIDs); err != nil {
				s.logger.Error("Ошибка обновления связей контракта с компаниями", "contract_id", contractModel.ID, "error", err)
				return err
			}
		}

		// 4. Пересчет статусов для затронутых компаний
		if len(affectedCompanyIDs) > 0 {
			s.logger.Info("Пересчет статусов для компаний", "count", len(affectedCompanyIDs))
			for compID := range affectedCompanyIDs {
				if err := s.recalculateCompanyStatus(txCtx, tx, compID); err != nil {
					s.logger.Error("Ошибка пересчета статуса компании", "company_id", compID, "error", err)
					// Не прерываем транзакцию из-за одной компании, но логируем
				}
			}
		}

		return nil
	})
}

func (s *serviceImpl) GetContract(ctx context.Context, id string) (*contract.Contract, error) {
	return s.contractRepo.GetByID(ctx, id)
}

func (s *serviceImpl) CreateContract(ctx context.Context, dto *api.ContractCreateDTO) (*contract.Contract, error) {
	// Маппинг DTO в доменную модель
	// Примечание: для JSON полей (Services, Recipients) здесь нужна конвертация,
	// если DTO содержит map/slice. В данном примере предполагаем упрощенный маппинг
	// или используем helpers.

	// Сериализация JSON полей (упрощенно, лучше вынести в helper)

	contractModel := &contract.Contract{
		State:          dto.State,
		StateStartTime: dto.StateStartTime,
		ServiceLevel:   dto.ServiceLevel,
	}

	// Конвертация Services и Recipients в JSON
	// (предполагается импорт "encoding/json" и "gorm.io/datatypes")
	if dto.Services != nil {
		if b, err := json.Marshal(dto.Services); err == nil {
			contractModel.Services = datatypes.JSON(b)
		}
	}
	if dto.Recipients != nil {
		if b, err := json.Marshal(dto.Recipients); err == nil {
			contractModel.Recipients = datatypes.JSON(b)
		}
	}

	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		// 1. Создаем сам контракт
		if err := s.contractRepo.Create(txCtx, contractModel); err != nil {
			return err
		}

		// 2. Создаем связи с компаниями, если переданы ID
		if len(dto.CompanyIDs) > 0 {
			// Используем метод репозитория для замены связей
			if err := s.contractRepo.ReplaceCompanyLinks(txCtx, contractModel, dto.CompanyIDs); err != nil {
				return err
			}
		}

		// 3. (Опционально) Пересчет статусов, если контракт создается сразу активным
		if contractModel.State != nil && *contractModel.State == "active" {
			for _, compID := range dto.CompanyIDs {
				// Здесь нужен tx из контекста, но recalculateCompanyStatus принимает *gorm.DB
				// Нам нужно извлечь DB из txCtx.
				tx := db.ExtractDB(txCtx, nil)
				_ = s.recalculateCompanyStatus(txCtx, tx, compID)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return contractModel, nil
}

func (s *serviceImpl) UpdateContract(ctx context.Context, id string, data map[string]interface{}) error {
	// Очистка системных полей
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.contractRepo.Update(txCtx, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}

		// В идеале здесь тоже нужно проверять, изменился ли статус на 'active'/'closed'
		// и запускать recalculateCompanyStatus.
		// Пока оставим базовый апдейт.

		return nil
	})
}

func (s *serviceImpl) DeleteContract(ctx context.Context, id string) error {
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		deleted, err := s.contractRepo.Delete(txCtx, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return nil
	})
}

// recalculateCompanyStatus проверяет наличие активных контрактов и обновляет статус компании/оборудования.
func (s *serviceImpl) recalculateCompanyStatus(ctx context.Context, tx *gorm.DB, companyID string) error {
	// 1. Получаем текущее состояние компании (нужно для сравнения)
	comp, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return err
	}
	if comp == nil {
		return fmt.Errorf("компания не найдена")
	}

	// 2. Проверяем наличие активных контрактов
	activeContractIDs, err := s.contractRepo.GetActiveContractIDsForCompany(ctx, companyID)
	if err != nil {
		return err
	}

	hasActiveContract := len(activeContractIDs) > 0
	currentStatus := false
	if comp.ActiveContract != nil {
		currentStatus = *comp.ActiveContract
	}

	// 3. Если статус изменился, выполняем действия
	if hasActiveContract != currentStatus {
		s.logger.Info("Изменение статуса контракта компании",
			"company_id", companyID,
			"company_name", *comp.Title,
			"old_status", currentStatus,
			"new_status", hasActiveContract)

		// Обновляем компанию
		_, err := s.companyRepo.Update(ctx, companyID, map[string]interface{}{
			"active_contract": hasActiveContract,
			"last_updated_by": "contract_service",
		})
		if err != nil {
			return err
		}

		// Блокировка / Разблокировка оборудования
		if hasActiveContract {
			// False -> True: Разблокировать
			if err := s.unlockEquipment(ctx, tx, companyID); err != nil {
				return err
			}
		} else {
			// True -> False: Заблокировать
			if err := s.lockEquipment(ctx, tx, companyID); err != nil {
				return err
			}
		}
	}

	s.logger.Debug("Статус компании актуален, изменений не требуется", "company_id", companyID, "status", currentStatus)

	return nil
}

func (s *serviceImpl) lockEquipment(ctx context.Context, tx *gorm.DB, ownerID string) error {
	s.logger.Info("Блокировка оборудования компании (нет активного контракта)", "owner_id", ownerID)

	if err := s.serverRepo.LockByOwner(ctx, tx, ownerID); err != nil {
		return fmt.Errorf("ошибка блокировки серверов: %w", err)
	}
	if err := s.workstationRepo.LockByOwner(ctx, tx, ownerID); err != nil {
		return fmt.Errorf("ошибка блокировки рабочих станций: %w", err)
	}
	if err := s.frRepo.LockByOwner(ctx, tx, ownerID); err != nil {
		return fmt.Errorf("ошибка блокировки ФР: %w", err)
	}
	return nil
}

func (s *serviceImpl) unlockEquipment(ctx context.Context, tx *gorm.DB, ownerID string) error {
	s.logger.Info("Разблокировка оборудования компании (появился активный контракт)", "owner_id", ownerID)

	if err := s.serverRepo.UnlockByOwner(ctx, tx, ownerID); err != nil {
		return fmt.Errorf("ошибка разблокировки серверов: %w", err)
	}
	if err := s.workstationRepo.UnlockByOwner(ctx, tx, ownerID); err != nil {
		return fmt.Errorf("ошибка разблокировки рабочих станций: %w", err)
	}
	if err := s.frRepo.UnlockByOwner(ctx, tx, ownerID); err != nil {
		return fmt.Errorf("ошибка разблокировки ФР: %w", err)
	}
	return nil
}
