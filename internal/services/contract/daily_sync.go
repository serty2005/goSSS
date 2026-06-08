package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/infra/db"

	"gorm.io/datatypes"
)

const contractMailSyncUpdatedBy = "contract_mail_sync"

func (s *serviceImpl) SyncDailySnapshot(ctx context.Context, snapshot contract.DailyCompanyContractSnapshot) error {
	return s.syncDailySnapshots(ctx, []contract.DailyCompanyContractSnapshot{snapshot}, false)
}

// SyncDailySnapshots применяет полный итоговый снимок контрактов по уже существующим mapping компаний к точкам.
func (s *serviceImpl) SyncDailySnapshots(ctx context.Context, snapshots []contract.DailyCompanyContractSnapshot) error {
	return s.syncDailySnapshots(ctx, snapshots, true)
}

func (s *serviceImpl) syncDailySnapshots(ctx context.Context, snapshots []contract.DailyCompanyContractSnapshot, deactivateMissing bool) error {
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := db.ExtractDB(txCtx, nil)
		affectedCompanyIDs := make(map[string]struct{}, len(snapshots))
		snapshotByContractID := make(map[string]contract.DailyCompanyContractSnapshot, len(snapshots))
		upsertedContracts := 0
		deactivatedContracts := 0
		skippedCompanies := 0

		for _, snapshot := range snapshots {
			snapshot.CompanyID = strings.TrimSpace(snapshot.CompanyID)
			if snapshot.CompanyID == "" {
				continue
			}
			if _, err := s.companyRepo.GetByID(txCtx, snapshot.CompanyID); err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					skippedCompanies++
					continue
				}
				return fmt.Errorf("не удалось проверить компанию %s перед применением почтового контракта: %w", snapshot.CompanyID, err)
			}
			contractID := mailManagedContractID(snapshot.CompanyID)
			snapshotByContractID[contractID] = snapshot
			affectedCompanyIDs[snapshot.CompanyID] = struct{}{}

			if err := s.upsertDailySnapshot(txCtx, contractID, snapshot); err != nil {
				return err
			}
			upsertedContracts++
		}

		if deactivateMissing {
			existingManaged, err := s.contractRepo.ListByLastUpdatedBy(txCtx, contractMailSyncUpdatedBy)
			if err != nil {
				return err
			}
			for _, item := range existingManaged {
				if _, exists := snapshotByContractID[item.ID]; exists {
					continue
				}

				updates := map[string]interface{}{
					"state":           "inactive",
					"last_updated_by": contractMailSyncUpdatedBy,
				}
				if _, err := s.contractRepo.Update(txCtx, item.ID, updates); err != nil {
					return fmt.Errorf("не удалось деактивировать устаревший контракт %s: %w", item.ID, err)
				}
				deactivatedContracts++
				for _, companyItem := range item.Companies {
					if companyItem.ID != "" {
						affectedCompanyIDs[companyItem.ID] = struct{}{}
					}
				}
			}
		}

		for companyID := range affectedCompanyIDs {
			if err := s.recalculateCompanyStatus(txCtx, tx, companyID); err != nil {
				return err
			}
		}
		s.logger.Info(
			"Почтовый снимок контрактов применён в доменном сервисе",
			"snapshots_received", len(snapshots),
			"contracts_upserted", upsertedContracts,
			"contracts_deactivated", deactivatedContracts,
			"companies_skipped", skippedCompanies,
			"companies_recalculated", len(affectedCompanyIDs),
		)

		return nil
	})
}

// upsertDailySnapshot создает или обновляет управляемый почтовым воркером контракт конкретной компании.
func (s *serviceImpl) upsertDailySnapshot(ctx context.Context, contractID string, snapshot contract.DailyCompanyContractSnapshot) error {
	snapshot.ContractType = NormalizeServicePointContractType(snapshot.ContractType)
	state := "inactive"
	if snapshot.Active || IsServicePointContractActive(nil, snapshot.ContractType) {
		state = "active"
	}

	servicesPayload := []string{}
	if strings.TrimSpace(snapshot.ContractType) != "" {
		servicesPayload = append(servicesPayload, strings.TrimSpace(snapshot.ContractType))
	}
	servicesJSON, err := json.Marshal(servicesPayload)
	if err != nil {
		return err
	}
	recipientsJSON, err := json.Marshal([]string{snapshot.CompanyID})
	if err != nil {
		return err
	}
	attributesJSON, err := buildSnapshotAttributes(snapshot)
	if err != nil {
		return err
	}

	existing, err := s.contractRepo.GetByID(ctx, contractID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if errors.Is(err, domain.ErrNotFound) {
		existing, err = s.contractRepo.GetByIDUnscoped(ctx, contractID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if errors.Is(err, domain.ErrNotFound) {
			existing = nil
		}
	}

	updates := map[string]interface{}{
		"state":              state,
		"state_start_time":   firstNonNilTime(snapshot.StartDate, snapshot.EndDate, time.Now().UTC()),
		"services":           datatypes.JSON(servicesJSON),
		"recipients":         datatypes.JSON(recipientsJSON),
		"last_modified_date": firstNonNilTime(snapshot.EndDate, snapshot.StartDate, time.Now().UTC()),
		"last_updated_by":    contractMailSyncUpdatedBy,
		"attributes":         attributesJSON,
	}

	if existing == nil {
		model := &contract.Contract{
			Base: common.Base{
				ID:            contractID,
				LastUpdatedBy: contractMailSyncUpdatedBy,
				Attributes:    attributesJSON,
			},
			State:            &state,
			StateStartTime:   firstNonNilTime(snapshot.StartDate, snapshot.EndDate, time.Now().UTC()),
			Services:         datatypes.JSON(servicesJSON),
			Recipients:       datatypes.JSON(recipientsJSON),
			LastModifiedDate: firstNonNilTime(snapshot.EndDate, snapshot.StartDate, time.Now().UTC()),
		}
		if err := s.contractRepo.Create(ctx, model); err != nil {
			return fmt.Errorf("не удалось создать контракт для компании %s: %w", snapshot.CompanyID, err)
		}
		if err := s.contractRepo.ReplaceCompanyLinks(ctx, model, []string{snapshot.CompanyID}); err != nil {
			return fmt.Errorf("не удалось привязать контракт к компании %s: %w", snapshot.CompanyID, err)
		}
		return nil
	}

	var updated bool
	if existing.DeletedAt.Valid {
		updated, err = s.contractRepo.Restore(ctx, contractID, updates)
	} else {
		updated, err = s.contractRepo.Update(ctx, contractID, updates)
	}
	if err != nil {
		return fmt.Errorf("не удалось обновить контракт %s: %w", contractID, err)
	}
	if !updated {
		return fmt.Errorf("не удалось обновить контракт %s: %w", contractID, domain.ErrNotFound)
	}
	existing.ID = contractID
	existing.DeletedAt.Valid = false
	if err := s.contractRepo.ReplaceCompanyLinks(ctx, existing, []string{snapshot.CompanyID}); err != nil {
		return fmt.Errorf("не удалось обновить привязку контракта %s: %w", contractID, err)
	}
	return nil
}

// buildSnapshotAttributes кодирует служебные атрибуты контракта для трассировки источника данных.
func buildSnapshotAttributes(snapshot contract.DailyCompanyContractSnapshot) (datatypes.JSON, error) {
	payload := map[string]any{
		"service_point_id":   snapshot.ServicePointID,
		"service_point_name": snapshot.ServicePointName,
		"service_point_code": snapshot.ServicePointCode,
		"contractor_id":      snapshot.ContractorID,
		"client_order":       snapshot.ClientOrder,
		"source_hash":        snapshot.SourceHash,
	}
	if snapshot.StartDate != nil {
		payload["contract_start"] = snapshot.StartDate.Format(time.RFC3339)
	}
	if snapshot.EndDate != nil {
		payload["contract_end"] = snapshot.EndDate.Format(time.RFC3339)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(encoded), nil
}

// firstNonNilTime возвращает первую непустую дату из набора значений и указателей.
func firstNonNilTime(candidates ...any) *time.Time {
	for _, candidate := range candidates {
		switch value := candidate.(type) {
		case time.Time:
			if value.IsZero() {
				continue
			}
			normalized := value
			return &normalized
		case *time.Time:
			if value == nil || value.IsZero() {
				continue
			}
			normalized := *value
			return &normalized
		}
	}
	return nil
}

// mailManagedContractID формирует стабильный ID контракта, управляемого почтовым воркером.
func mailManagedContractID(companyID string) string {
	return "mail-contract:" + strings.TrimSpace(companyID)
}
