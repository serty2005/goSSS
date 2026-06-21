package contract

import (
	"context"
	"encoding/json"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/db" // Пока нужен только для MapperContext типов, если используются глубоко, но здесь уже нет
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type serviceImpl struct {
	logger logger.LoggerInterface
	tm     interfaces.Transactor
	// sdClient удален, сервис работает с готовыми моделями
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
	// sdClient external.ExternalSystemClient удален
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
		contractRepo:    contractRepo,
		companyRepo:     companyRepo,
		linkRepo:        linkRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// SyncContracts выполняет полную синхронизацию контрактов и пересчет статусов.
func (s *serviceImpl) SyncContracts(ctx context.Context, contracts map[string]*contract.Contract) error {
	s.logger.Info("Начало синхронизации контрактов", "count", len(contracts))

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := db.ExtractDB(txCtx, nil)

		// Сет для хранения ID компаний, затронутых изменениями
		affectedCompanyIDs := make(map[string]struct{})

		for extUUID, contractModel := range contracts {
			// 1. Upsert контракта
			// Ищем существующий контракт по внешнему ID
			existing, _ := s.contractRepo.GetByServiceDeskUUID(txCtx, extUUID)

			if existing != nil {
				contractModel.ID = existing.ID // Сохраняем внутренний ID для обновления

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
					SystemName:      "naumen", // В будущем можно брать из контекста провайдера
					ServiceDeskUUID: extUUID,
					EntityType:      "Contract",
					LastSyncedAt:    time.Now(),
				}
				if err := s.linkRepo.Create(txCtx, tx, link); err != nil {
					return fmt.Errorf("ошибка создания связи для контракта: %w", err)
				}
			}

			// 2. Обновление связей с компаниями (Recipients)
			// contractModel.Recipients - это JSONB, содержащий массив ExternalUUIDs получателей.
			// Нам нужно распарсить его и найти внутренние ID компаний.

			var recipientExtUUIDs []string
			if len(contractModel.Recipients) > 0 {
				if err := json.Unmarshal(contractModel.Recipients, &recipientExtUUIDs); err != nil {
					s.logger.Warn("Не удалось распарсить получателей контракта", "contract_id", contractModel.ID, "error", err)
					continue
				}
			}

			var companyIntIDs []string
			for _, compExtID := range recipientExtUUIDs {
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

		// 3. Пересчет статусов для затронутых компаний
		if len(affectedCompanyIDs) > 0 {
			s.logger.Info("Пересчет статусов для компаний", "count", len(affectedCompanyIDs))
			for compID := range affectedCompanyIDs {
				if err := s.recalculateCompanyStatus(txCtx, tx, compID); err != nil {
					s.logger.Error("Ошибка пересчета статуса компании", "company_id", compID, "error", err)
					// Не прерываем транзакцию из-за одной компании
				}
			}
		}

		return nil
	})
}

// ... Остальные методы (Create/Update/Delete/Get/Recalculate) без изменений ...
// (Вставь сюда остальные методы из предыдущего файла, они не меняются)

func (s *serviceImpl) GetContract(ctx context.Context, id string) (*contract.Contract, error) {
	return s.contractRepo.GetByID(ctx, id)
}

func (s *serviceImpl) ListCompanyContracts(ctx context.Context, companyID string) ([]contract.Contract, error) {
	return s.contractRepo.ListForCompany(ctx, strings.TrimSpace(companyID))
}

func (s *serviceImpl) CreateContract(ctx context.Context, dto *api.ContractCreateDTO) (*contract.Contract, error) {
	contractModel := &contract.Contract{
		State:          dto.State,
		StateStartTime: dto.StateStartTime,
		ServiceLevel:   dto.ServiceLevel,
	}

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
		if err := s.contractRepo.Create(txCtx, contractModel); err != nil {
			return err
		}

		if len(dto.CompanyIDs) > 0 {
			if err := s.contractRepo.ReplaceCompanyLinks(txCtx, contractModel, dto.CompanyIDs); err != nil {
				return err
			}
		}

		if contractModel.State != nil && *contractModel.State == "active" {
			for _, compID := range dto.CompanyIDs {
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
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		existing, err := s.contractRepo.GetByID(txCtx, id)
		if err != nil {
			return err
		}

		if nextState, hasState := extractStateValue(data["state"]); hasState {
			currentState := ""
			if existing.State != nil {
				currentState = *existing.State
			}
			if nextState != currentState {
				data["state_start_time"] = time.Now().UTC()
			}
		}

		companyIDsPatch, hasCompanyIDsPatch := extractCompanyIDsPatch(data)
		normalizeContractJSONFields(data)

		affectedCompanyIDs := make(map[string]struct{}, len(existing.Companies))
		for _, comp := range existing.Companies {
			if comp.ID != "" {
				affectedCompanyIDs[comp.ID] = struct{}{}
			}
		}
		if hasCompanyIDsPatch {
			for _, companyID := range companyIDsPatch {
				affectedCompanyIDs[companyID] = struct{}{}
			}
		}

		updated, err := s.contractRepo.Update(txCtx, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}

		if hasCompanyIDsPatch {
			existing.ID = id
			if err := s.contractRepo.ReplaceCompanyLinks(txCtx, existing, companyIDsPatch); err != nil {
				return err
			}
		}

		tx := db.ExtractDB(txCtx, nil)
		for companyID := range affectedCompanyIDs {
			if err := s.recalculateCompanyStatus(txCtx, tx, companyID); err != nil {
				return err
			}
		}

		return nil
	})
}

func extractCompanyIDsPatch(data map[string]interface{}) ([]string, bool) {
	raw, exists := data["company_ids"]
	if !exists {
		return nil, false
	}
	delete(data, "company_ids")

	switch values := raw.(type) {
	case []string:
		return uniqueTrimmedStrings(values), true
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, item := range values {
			id := strings.TrimSpace(fmt.Sprintf("%v", item))
			if id == "" || id == "<nil>" {
				continue
			}
			result = append(result, id)
		}
		return uniqueTrimmedStrings(result), true
	default:
		id := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if id == "" || id == "<nil>" {
			return []string{}, true
		}
		return []string{id}, true
	}
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func extractStateValue(raw interface{}) (string, bool) {
	if raw == nil {
		return "", true
	}

	switch v := raw.(type) {
	case string:
		return v, true
	case *string:
		if v == nil {
			return "", true
		}
		return *v, true
	default:
		return fmt.Sprint(v), true
	}
}

func normalizeContractJSONFields(data map[string]interface{}) {
	normalizeToJSON := func(field string) {
		raw, exists := data[field]
		if !exists {
			return
		}

		encoded, err := json.Marshal(raw)
		if err != nil {
			return
		}
		data[field] = datatypes.JSON(encoded)
	}

	normalizeToJSON("services")
	normalizeToJSON("recipients")
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

func (s *serviceImpl) recalculateCompanyStatus(ctx context.Context, tx *gorm.DB, companyID string) error {
	comp, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return err
	}
	if comp == nil {
		return fmt.Errorf("компания не найдена")
	}

	mailContract, mailContractErr := s.contractRepo.GetByID(ctx, mailManagedContractID(companyID))
	if mailContractErr != nil && !errors.Is(mailContractErr, domain.ErrNotFound) {
		return mailContractErr
	}
	if mailContractErr == nil && mailContract != nil {
		if err := s.contractRepo.DeactivateActiveContractsExcept(ctx, companyID, mailContract.ID); err != nil {
			return fmt.Errorf("не удалось деактивировать устаревшие контракты компании %s: %w", companyID, err)
		}
	}
	activeContractIDs, err := s.contractRepo.GetActiveContractIDsForCompany(ctx, companyID)
	if err != nil {
		return err
	}

	hasActiveContract := len(activeContractIDs) > 0
	currentStatus := false
	if comp.ActiveContract != nil {
		currentStatus = *comp.ActiveContract
	}

	if hasActiveContract != currentStatus {
		s.logger.Info("Изменение статуса контракта компании",
			"company_id", companyID,
			"company_name", *comp.Title,
			"old_status", currentStatus,
			"new_status", hasActiveContract)

		_, err := s.companyRepo.Update(ctx, companyID, map[string]interface{}{
			"active_contract": hasActiveContract,
			"last_updated_by": "contract_service",
		})
		if err != nil {
			return err
		}

		if hasActiveContract {
			if err := s.unlockEquipment(ctx, tx, companyID); err != nil {
				return err
			}
		} else {
			if err := s.lockEquipment(ctx, tx, companyID); err != nil {
				return err
			}
		}
	}

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
