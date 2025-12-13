package seeder

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const batchSize = 100

type Seeder struct {
	logger          logger.LoggerInterface
	db              *gorm.DB
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	contractRepo    contract.Repository
}

func NewSeeder(
	logger logger.LoggerInterface, db *gorm.DB, companyRepo company.Repository,
	serverRepo server.Repository, workstationRepo workstation.Repository,
	frRepo fiscal.Repository, contractRepo contract.Repository,
) *Seeder {
	return &Seeder{
		logger, db, companyRepo, serverRepo, workstationRepo, frRepo, contractRepo,
	}
}

// relationsMap хранит временные связи: InternalID -> ExternalUUID родителя/владельца
type relationsMap map[string]string

func (s *Seeder) SeedDatabase(sdClient external.ExternalSystemClient) error {
	s.logger.Info("Начало процесса наполнения базы данных...")

	if err := s.clearDatabase(); err != nil {
		return err
	}

	s.logger.Info("Создание схемы базы данных через AutoMigrate...")
	err := s.db.AutoMigrate(
		&user.User{}, &user.Role{},
		&tickets.Ticket{}, &tickets.TicketHistory{}, &tickets.Attachment{},
		&company.Company{}, &server.Server{}, &workstation.Workstation{},
		&fiscal.FiscalRegister{}, &contract.Contract{},
		&models.AgentFile{}, &models.ReconciliationTask{},
		&models.Agent{}, &models.CompanyContract{}, &models.ExternalSystemLink{}, &models.EquipmentStatusLog{},
	)
	if err != nil {
		s.logger.Error("Не удалось выполнить миграцию схемы БД", "error", err)
		return err
	}
	s.logger.Info("Схема базы данных успешно создана.")

	ctx := context.Background()
	mapperCtx := (*external.MapperContext)(nil) // Для сидера контекст маппера не нужен, так как связи строим вручную

	// Получаем сырые данные
	companyData, _ := sdClient.FetchEntityList(ctx, "Company")
	serverData, _ := sdClient.FetchEntityList(ctx, "Server")
	wsData, _ := sdClient.FetchEntityList(ctx, "Workstation")
	frData, _ := sdClient.FetchEntityList(ctx, "FiscalRegister")
	contractData, _ := sdClient.FetchEntityList(ctx, "Contract")

	// Карты для связывания
	extToIntID := make(map[string]string) // ExternalUUID -> InternalID
	parentRelations := make(relationsMap) // CompanyInternalID -> ParentExternalUUID
	ownerRelations := make(relationsMap)  // EntityInternalID -> OwnerExternalUUID

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 1. Создание Компаний
		s.logger.Info("Создание Компаний...", "count", len(companyData))
		for _, data := range companyData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}
			comp, _ := sdClient.Mapper().DataToCompany(ctx, mapperCtx, data)
			if err := tx.Create(comp).Error; err != nil {
				continue
			}
			extToIntID[extID] = comp.ID
			tx.Create(&models.ExternalSystemLink{InternalID: comp.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Company", LastSyncedAt: time.Now()})

			// Сохраняем связь с родителем, если есть
			if parentData, ok := data["parent"].(map[string]interface{}); ok {
				if parentExtID, ok := parentData["UUID"].(string); ok && parentExtID != "" {
					parentRelations[comp.ID] = parentExtID
				}
			}
		}

		// 2. Создание Серверов (с отложенным связыванием владельца)
		s.logger.Info("Создание Серверов...", "count", len(serverData))
		for _, data := range serverData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			srv, _ := sdClient.Mapper().DataToServer(ctx, mapperCtx, data)
			srv.OwnerID = nil // Владельца проставим позже

			if err := tx.Create(srv).Error; err != nil {
				continue
			}

			extToIntID[extID] = srv.ID
			tx.Create(&models.ExternalSystemLink{InternalID: srv.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Server", LastSyncedAt: time.Now()})

			// Запоминаем владельца
			if ownerData, ok := data["owner"].(map[string]interface{}); ok {
				if ownerExtID, ok := ownerData["UUID"].(string); ok {
					ownerRelations[srv.ID] = ownerExtID
				}
			}
		}

		// 3. Создание Рабочих станций
		s.logger.Info("Создание Рабочих станций...", "count", len(wsData))
		for _, data := range wsData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			ws, _ := sdClient.Mapper().DataToWorkstation(ctx, mapperCtx, data)
			ws.OwnerID = nil

			if err := tx.Create(ws).Error; err != nil {
				continue
			}

			extToIntID[extID] = ws.ID
			tx.Create(&models.ExternalSystemLink{InternalID: ws.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Workstation", LastSyncedAt: time.Now()})

			if ownerData, ok := data["owner"].(map[string]interface{}); ok {
				if ownerExtID, ok := ownerData["UUID"].(string); ok {
					ownerRelations[ws.ID] = ownerExtID
				}
			}
		}

		// 4. Создание ФР
		s.logger.Info("Создание ФР...", "count", len(frData))
		for _, data := range frData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			fr, _ := sdClient.Mapper().DataToFiscalRegister(ctx, mapperCtx, data)
			fr.OwnerID = nil

			if err := tx.Create(fr).Error; err != nil {
				continue
			}

			extToIntID[extID] = fr.ID
			tx.Create(&models.ExternalSystemLink{InternalID: fr.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "FiscalRegister", LastSyncedAt: time.Now()})

			if ownerData, ok := data["owner"].(map[string]interface{}); ok {
				if ownerExtID, ok := ownerData["UUID"].(string); ok {
					ownerRelations[fr.ID] = ownerExtID
				}
			}
		}

		// --- LINKING PHASE ---
		s.logger.Info("Выполнение связывания сущностей (Linking Phase)...")

		// Связывание Родительских компаний
		for childID, parentExtID := range parentRelations {
			if parentIntID, ok := extToIntID[parentExtID]; ok {
				tx.Model(&company.Company{}).Where("id = ?", childID).Update("parent_id", parentIntID)
			}
		}

		// Связывание Оборудования с Владельцами
		// Можно оптимизировать батчами, но для сидера сойдет поштучно
		for entityID, ownerExtID := range ownerRelations {
			if ownerIntID, ok := extToIntID[ownerExtID]; ok {
				// Пытаемся обновить во всех таблицах (ID уникальны UUID, так что это безопасно, но не оптимально)
				// Лучше было бы разделить ownerRelations по типам, но для упрощения сделаем так:
				tx.Model(&server.Server{}).Where("id = ?", entityID).Update("owner_id", ownerIntID)
				tx.Model(&workstation.Workstation{}).Where("id = ?", entityID).Update("owner_id", ownerIntID)
				tx.Model(&fiscal.FiscalRegister{}).Where("id = ?", entityID).Update("owner_id", ownerIntID)
			}
		}

		// 5. Создание Контрактов
		s.logger.Info("Создание Контрактов и связей...", "count", len(contractData))
		var linksToCreate []models.CompanyContract

		for _, data := range contractData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}

			c, err := sdClient.Mapper().DataToContract(ctx, mapperCtx, data)
			if err != nil {
				continue
			}

			if err := tx.Create(c).Error; err != nil {
				continue
			}
			extToIntID[extID] = c.ID
			tx.Create(&models.ExternalSystemLink{InternalID: c.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Contract", LastSyncedAt: time.Now()})

			// Связи с компаниями
			companyExtIDs := sdClient.Mapper().GetCompanyUUIDsFromContract(data)
			for _, compExtID := range companyExtIDs {
				if compIntID, ok := extToIntID[compExtID]; ok {
					linksToCreate = append(linksToCreate, models.CompanyContract{CompanyID: compIntID, ContractID: c.ID})
				}
			}
		}

		if len(linksToCreate) > 0 {
			if err := tx.Table("company_contracts").CreateInBatches(linksToCreate, batchSize).Error; err != nil {
				return err
			}
		}

		// 6. Пересчет статусов (упрощенный, без сложных проверок сервиса)
		// Просто смотрим: если у компании есть active контракт -> active_contract = true
		s.logger.Info("Финализация статусов...")
		tx.Exec(`
			UPDATE companies 
			SET active_contract = true 
			WHERE id IN (
				SELECT cc.company_id 
				FROM company_contracts cc 
				JOIN contracts c ON c.id = cc.contract_id 
				WHERE c.state = 'active'
			)
		`)

		return nil
	})
}

// clearDatabase удаляет все таблицы для полного пересоздания базы.
func (s *Seeder) clearDatabase() error {
	// Порядок удаления важен из-за Foreign Keys
	tables := []string{
		"equipment_status_logs", "external_system_links", "company_contracts",
		"user_roles", "roles", // Users tables
		"server_additional_owners",
		"ticket_histories", "attachments", "tickets", // Ticket tables
		"reconciliation_tasks", "agent_files", "fiscal_registers", "workstations",
		"servers", "contracts", "companies", "users", "agents",
	}
	s.logger.Info("Удаление существующих таблиц...")
	for _, table := range tables {
		if err := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)).Error; err != nil {
			if !strings.Contains(err.Error(), "does not exist") {
				s.logger.Warn("Не удалось удалить таблицу", "table", table, "error", err)
			}
		}
	}
	return nil
}
