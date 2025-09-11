package processing

import (
	"context"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/logger"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm"
)

// --- CRUD-хелперы ---

// createEntity создает сущность и возвращает ее ID. Тип определяется через рефлексию.
func (o *Orchestrator) createEntity(ctx context.Context, entity interface{}) (string, error) {
	tx := ctx.Value(transactionKey).(*gorm.DB)
	var id string
	var err error
	switch v := entity.(type) {
	case *models.Company:
		err = o.companyRepo.Create(ctx, tx, v)
		id = v.ID
	case *models.Server:
		err = o.serverRepo.Create(ctx, tx, v)
		id = v.ID
	case *models.Workstation:
		err = o.workstationRepo.Create(ctx, tx, v)
		id = v.ID
	case *models.FiscalRegister:
		err = o.frRepo.Create(ctx, tx, v)
		id = v.ID
	default:
		return "", fmt.Errorf("неподдерживаемый тип для создания: %T", entity)
	}
	return id, err
}

func (o *Orchestrator) performUpdate(ctx context.Context, tx *gorm.DB, entityType, internalID string, updates map[string]interface{}) error {
	var err error
	switch entityType {
	case "Company":
		_, err = o.companyRepo.Update(ctx, tx, internalID, updates)
	case "Server":
		_, err = o.serverRepo.Update(ctx, tx, internalID, updates)
	case "Workstation":
		_, err = o.workstationRepo.Update(ctx, tx, internalID, updates)
	case "FiscalRegister":
		_, err = o.frRepo.Update(ctx, tx, internalID, updates)
	default:
		return fmt.Errorf("неподдерживаемый тип для обновления: %s", entityType)
	}
	return err
}

func (o *Orchestrator) performDelete(ctx context.Context, tx *gorm.DB, entityType, internalID string) error {
	var err error
	switch entityType {
	case "Company":
		_, err = o.companyRepo.Delete(ctx, tx, internalID)
	case "Server":
		_, err = o.serverRepo.Delete(ctx, tx, internalID)
	case "Workstation":
		_, err = o.workstationRepo.Delete(ctx, tx, internalID)
	case "FiscalRegister":
		_, err = o.frRepo.Delete(ctx, tx, internalID)
	default:
		return fmt.Errorf("неподдерживаемый тип для удаления: %s", entityType)
	}
	return err
}

// --- Хелперы для управления оборудованием ---

func (o *Orchestrator) lockEquipment(ctx context.Context, tx *gorm.DB, inactiveIDs []string, log logger.LoggerInterface) error {
	for _, model := range []interface{}{&models.Server{}, &models.Workstation{}, &models.FiscalRegister{}} {
		res := tx.WithContext(ctx).Model(model).Where("owner_id IN ? AND status != ?", inactiveIDs, "locked").
			Updates(map[string]interface{}{"status_before_lock": gorm.Expr("status"), "status": "locked"})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Info("Заморожено единиц оборудования", "count", res.RowsAffected)
		}
	}
	return nil
}

func (o *Orchestrator) unlockEquipment(ctx context.Context, tx *gorm.DB, activeIDs []string, log logger.LoggerInterface) error {
	for _, model := range []interface{}{&models.Server{}, &models.Workstation{}, &models.FiscalRegister{}} {
		res := tx.WithContext(ctx).Model(model).Where("owner_id IN ? AND status = ? AND status_before_lock IS NOT NULL", activeIDs, "locked").
			Updates(map[string]interface{}{"status": gorm.Expr("status_before_lock"), "status_before_lock": nil})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Info("Разморожено единиц оборудования", "count", res.RowsAffected)
		}
	}
	return nil
}

// --- Хелперы для сравнения (diff) ---

func getLMDFromModel(entity interface{}) *time.Time {
	switch v := entity.(type) {
	case *models.Company:
		return v.LastModifiedDate
	case *models.Server:
		return v.LastModifiedDate
	case *models.Workstation:
		return v.LastModifiedDate
	case *models.FiscalRegister:
		return v.LastModifiedDate
	}
	return nil
}

func compareAndLog[T comparable](updates map[string]interface{}, key string, current, new *T) {
	isCurrentNil := current == nil || reflect.ValueOf(current).IsNil()
	isNewNil := new == nil || reflect.ValueOf(new).IsNil()
	if isCurrentNil && isNewNil {
		return
	}
	if isCurrentNil != isNewNil || *current != *new {
		updates[key] = new
	}
}

func getCompanyDiff(current *models.Company, new *models.Company) (map[string]interface{}, []string) {
	updates := make(map[string]interface{})
	compareAndLog(updates, "title", current.Title, new.Title)
	compareAndLog(updates, "address", current.Address, new.Address)
	compareAndLog(updates, "additional_name", current.AdditionalName, new.AdditionalName)
	compareAndLog(updates, "parent_id", current.ParentID, new.ParentID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates, []string{} // Empty diffs for now
}

func getServerDiff(current *models.Server, new *models.Server) (map[string]interface{}, []string) {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	compareAndLog(updates, "unique_id", current.UniqueID, new.UniqueID)
	compareAndLog(updates, "rdp", current.RDP, new.RDP)
	compareAndLog(updates, "server_version", current.ServerVersion, new.ServerVersion)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates, []string{} // Empty diffs for now
}

func getWorkstationDiff(current *models.Workstation, new *models.Workstation) (map[string]interface{}, []string) {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates, []string{} // Empty diffs for now
}

func getFiscalRegisterDiff(current *models.FiscalRegister, new *models.FiscalRegister) (map[string]interface{}, []string) {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates, []string{} // Empty diffs for now
}
