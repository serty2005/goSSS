package workers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// StatusActualityWorker периодически проверяет актуальность проблемных статусов оборудования.
type StatusActualityWorker interface {
	Start(ctx context.Context)
}

type statusActualityWorkerImpl struct {
	cfg             *config.Config
	logger          logger.LoggerInterface
	db              *gorm.DB
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewStatusActualityWorker создает новый экземпляр воркера.
func NewStatusActualityWorker(
	cfg *config.Config,
	logger logger.LoggerInterface,
	db *gorm.DB,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) StatusActualityWorker {
	return &statusActualityWorkerImpl{
		cfg:             cfg,
		logger:          logger,
		db:              db,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start запускает воркер в фоновом режиме.
func (w *statusActualityWorkerImpl) Start(ctx context.Context) {
	w.logger.Info("Запуск воркера проверки актуальности статусов (StatusActualityWorker)", "interval", w.cfg.StatusWorkerInterval)
	ticker := time.NewTicker(w.cfg.StatusWorkerInterval)
	defer ticker.Stop()

	w.runCheckCycle(ctx)

	for {
		select {
		case <-ticker.C:
			w.runCheckCycle(ctx)
		case <-ctx.Done():
			w.logger.Info("Остановка воркера StatusActualityWorker.")
			return
		}
	}
}

// runCheckCycle выполняет один полный цикл проверки.
func (w *statusActualityWorkerImpl) runCheckCycle(ctx context.Context) {
	log := w.logger.With("cycle", "status_actuality")
	log.Info("Начало нового цикла проверки актуальности статусов оборудования.")

	err := w.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, "tx", tx)

		// Проверка серверов
		var servers []models.Server
		if err := tx.Where("health_status = ?", "attention_required").Find(&servers).Error; err != nil {
			return err
		}
		for _, server := range servers {
			if resolved, err := w.checkProblem(txCtx, &server, server.StatusDetails); err == nil && resolved {
				if err := w.updateStatusAndLog(tx, "Server", server.ID, server.HealthStatus, server.StatusDetails, "status_worker"); err != nil {
					log.Error("Не удалось сбросить статус для сервера", "id", server.ID, "error", err)
				}
			}
		}

		// Проверка рабочих станций
		var workstations []models.Workstation
		if err := tx.Where("health_status = ?", "attention_required").Find(&workstations).Error; err != nil {
			return err
		}
		for _, ws := range workstations {
			if resolved, err := w.checkProblem(txCtx, &ws, ws.StatusDetails); err == nil && resolved {
				if err := w.updateStatusAndLog(tx, "Workstation", ws.ID, ws.HealthStatus, ws.StatusDetails, "status_worker"); err != nil {
					log.Error("Не удалось сбросить статус для рабочей станции", "id", ws.ID, "error", err)
				}
			}
		}

		// Проверка ФР
		var frs []models.FiscalRegister
		if err := tx.Where("health_status = ?", "attention_required").Find(&frs).Error; err != nil {
			return err
		}
		for _, fr := range frs {
			if resolved, err := w.checkProblem(txCtx, &fr, fr.StatusDetails); err == nil && resolved {
				if err := w.updateStatusAndLog(tx, "FiscalRegister", fr.ID, fr.HealthStatus, fr.StatusDetails, "status_worker"); err != nil {
					log.Error("Не удалось сбросить статус для ФР", "id", fr.ID, "error", err)
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Цикл проверки актуальности статусов завершился с ошибкой", "error", err)
	} else {
		log.Info("Цикл проверки актуальности статусов успешно завершен.")
	}
}

// checkProblem маршрутизирует проверку в зависимости от типа проблемы.
func (w *statusActualityWorkerImpl) checkProblem(ctx context.Context, entity interface{}, detailsJSON datatypes.JSON) (bool, error) {
	var details map[string]interface{}
	if err := json.Unmarshal(detailsJSON, &details); err != nil {
		return false, fmt.Errorf("ошибка десериализации StatusDetails: %w", err)
	}

	problemType, ok := details["type"].(string)
	if !ok {
		return false, fmt.Errorf("в StatusDetails отсутствует поле 'type'")
	}

	switch problemType {
	case "duplicate_found":
		return w.checkForDuplicates(ctx, entity, details)
	case "owner_mismatch":
		return w.checkOwnerMismatch(ctx, entity, details)
	}

	return false, nil
}

// checkForDuplicates проверяет, устранены ли дубликаты.
func (w *statusActualityWorkerImpl) checkForDuplicates(ctx context.Context, entity interface{}, details map[string]interface{}) (bool, error) {
	field, okField := details["field"].(string)
	value, okValue := details["value"].(string)
	if !okField || !okValue {
		return false, fmt.Errorf("некорректные детали для проверки дубликатов")
	}

	tx := ctx.Value("tx").(*gorm.DB)
	var count int64
	var tableName string
	var entityType string

	switch entity.(type) {
	case *models.Server:
		tableName = "servers"
		entityType = "Server"
	case *models.Workstation:
		tableName = "workstations"
		entityType = "Workstation"
	default:
		return false, fmt.Errorf("неподдерживаемый тип сущности для проверки дубликатов: %T", entity)
	}

	err := tx.Table(tableName).Where(fmt.Sprintf("%s = ?", field), value).Count(&count).Error
	if err != nil {
		w.logger.Error("Ошибка при подсчете дубликатов", "entityType", entityType, "field", field, "value", value, "error", err)
		return false, err
	}

	return count <= 1, nil
}

// checkOwnerMismatch проверяет, устранен ли конфликт владельцев.
func (w *statusActualityWorkerImpl) checkOwnerMismatch(ctx context.Context, entity interface{}, details map[string]interface{}) (bool, error) {
	conflictingOwnerID, ok := details["conflicting_owner_id"].(string)
	if !ok {
		return false, fmt.Errorf("некорректные детали для проверки конфликта владельцев")
	}

	var currentOwnerID string
	switch e := entity.(type) {
	case *models.Server:
		if e.OwnerID != nil {
			currentOwnerID = *e.OwnerID
		}
	// Добавить другие типы сущностей при необходимости
	default:
		return false, fmt.Errorf("неподдерживаемый тип сущности для проверки конфликта владельцев: %T", entity)
	}

	if currentOwnerID == "" {
		return false, nil // Проблема не решена, если владелец отсутствует
	}

	currentOwnerParents, _ := w.companyRepo.GetAllParentIDs(ctx, currentOwnerID)
	conflictingOwnerParents, _ := w.companyRepo.GetAllParentIDs(ctx, conflictingOwnerID)

	return areCompaniesRelated(currentOwnerID, conflictingOwnerID, currentOwnerParents, conflictingOwnerParents), nil
}

// updateStatusAndLog атомарно обновляет статус и создает запись в логе.
func (w *statusActualityWorkerImpl) updateStatusAndLog(tx *gorm.DB, entityType, entityID, oldStatus string, oldDetails datatypes.JSON, changedBy string) error {
	updates := map[string]interface{}{
		"health_status":  "ok",
		"status_details": nil,
	}

	// Обновляем саму сущность
	switch entityType {
	case "Server":
		if res := tx.Model(&models.Server{}).Where("id = ?", entityID).Updates(updates); res.Error != nil {
			return res.Error
		}
	case "Workstation":
		if res := tx.Model(&models.Workstation{}).Where("id = ?", entityID).Updates(updates); res.Error != nil {
			return res.Error
		}
	case "FiscalRegister":
		if res := tx.Model(&models.FiscalRegister{}).Where("id = ?", entityID).Updates(updates); res.Error != nil {
			return res.Error
		}
	default:
		return fmt.Errorf("неизвестный тип сущности для обновления статуса: %s", entityType)
	}

	// Создаем запись в логе
	logEntry := models.EquipmentStatusLog{
		EntityType:      entityType,
		EntityID:        entityID,
		OldHealthStatus: oldStatus,
		NewHealthStatus: "ok",
		Details:         oldDetails,
		ChangedBy:       changedBy,
		Timestamp:       time.Now(),
	}

	return tx.Create(&logEntry).Error
}

// areCompaniesRelated проверяет, связаны ли две компании (являются родителями/дочерними друг для друга или имеют общего родителя).
func areCompaniesRelated(owner1, owner2 string, parents1, parents2 []string) bool {
	if owner1 == owner2 {
		return true
	}
	for _, p1 := range parents1 {
		if p1 == owner2 {
			return true
		}
	}
	for _, p2 := range parents2 {
		if p2 == owner1 {
			return true
		}
	}
	parents1Set := make(map[string]struct{})
	for _, p1 := range parents1 {
		parents1Set[p1] = struct{}{}
	}
	for _, p2 := range parents2 {
		if _, ok := parents1Set[p2]; ok {
			return true
		}
	}
	return false
}
