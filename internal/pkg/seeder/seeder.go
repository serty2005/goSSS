package seeder

import (
	"context"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
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

func (s *Seeder) SeedDatabase(sdClient external.ExternalSystemClient) error {
	s.logger.Info("Начало процесса наполнения базы данных...")

	if err := s.clearDatabase(); err != nil {
		return err
	}

	s.logger.Info("Создание схемы базы данных через AutoMigrate...")
	err := s.db.AutoMigrate(
		&company.Company{}, &server.Server{}, &workstation.Workstation{},
		&fiscal.FiscalRegister{}, &models.AgentFile{}, &models.ReconciliationTask{},
		&models.Agent{}, &contract.Contract{}, &models.CompanyContract{},
		&models.User{}, &models.ExternalSystemLink{},
	)
	if err != nil {
		s.logger.Error("Не удалось выполнить миграцию схемы БД", "error", err)
		return err
	}
	s.logger.Info("Схема базы данных успешно создана.")

	ctx := context.Background()
	mapperCtx := (*external.MapperContext)(nil)

	companyData, err := sdClient.FetchEntityList(ctx, "Company")
	if err != nil {
		return err
	}
	serverData, err := sdClient.FetchEntityList(ctx, "Server")
	if err != nil {
		return err
	}
	wsData, err := sdClient.FetchEntityList(ctx, "Workstation")
	if err != nil {
		return err
	}
	frData, err := sdClient.FetchEntityList(ctx, "FiscalRegister")
	if err != nil {
		return err
	}
	contractData, err := sdClient.FetchEntityList(ctx, "Contract")
	if err != nil {
		return err
	}

	extToIntID := make(map[string]string)

	return s.db.Transaction(func(tx *gorm.DB) error {
		s.logger.Info("Создание Компаний...")
		for _, data := range companyData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}
			company, _ := sdClient.Mapper().DataToCompany(ctx, mapperCtx, data)
			if err := tx.Create(company).Error; err != nil {
				continue
			}
			extToIntID[extID] = company.ID
			tx.Create(&models.ExternalSystemLink{InternalID: company.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Company", LastSyncedAt: time.Now()})
		}

		s.logger.Info("Установка родительских связей для Компаний...")
		for _, data := range companyData {
			extID, _ := data["UUID"].(string)
			companyModel, _ := sdClient.Mapper().DataToCompany(ctx, mapperCtx, data)
			parentExtID := companyModel.MetaClass
			if parentExtID != string(domain.MetaClassCompany) {
				childIntID := extToIntID[extID]
				parentIntID := extToIntID[parentExtID]
				if childIntID != "" && parentIntID != "" {
					tx.Model(&company.Company{}).Where("id = ?", childIntID).Update("parent_id", parentIntID)
				}
			}
			tx.Model(&company.Company{}).Where("id = ?", extToIntID[extID]).Update("meta_class", string(domain.MetaClassCompany))
		}

		s.logger.Info("Создание Оборудования...")
		for _, data := range serverData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}
			server, _ := sdClient.Mapper().DataToServer(ctx, mapperCtx, data)
			if server != nil {
				if server.OwnerID != nil && *server.OwnerID != "" {
					if ownerIntID, ok := extToIntID[*server.OwnerID]; ok {
						server.OwnerID = &ownerIntID
					} else {
						server.OwnerID = nil
					}
				} else {
					server.OwnerID = nil
				}

				tx.Create(server)
				extToIntID[extID] = server.ID
				// ИСПРАВЛЕНИЕ: Добавляем создание связи
				tx.Create(&models.ExternalSystemLink{InternalID: server.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Server", LastSyncedAt: time.Now()})
			}
		}
		for _, data := range wsData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}
			ws, _ := sdClient.Mapper().DataToWorkstation(ctx, mapperCtx, data)
			if ws != nil {
				if ws.OwnerID != nil && *ws.OwnerID != "" {
					if ownerIntID, ok := extToIntID[*ws.OwnerID]; ok {
						ws.OwnerID = &ownerIntID
					} else {
						ws.OwnerID = nil
					}
				} else {
					ws.OwnerID = nil
				}

				tx.Create(ws)
				extToIntID[extID] = ws.ID
				// ИСПРАВЛЕНИЕ: Добавляем создание связи
				tx.Create(&models.ExternalSystemLink{InternalID: ws.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "Workstation", LastSyncedAt: time.Now()})
			}
		}
		for _, data := range frData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				continue
			}
			fr, _ := sdClient.Mapper().DataToFiscalRegister(ctx, mapperCtx, data)
			if fr != nil {
				if fr.OwnerID != nil && *fr.OwnerID != "" {
					if ownerIntID, ok := extToIntID[*fr.OwnerID]; ok {
						fr.OwnerID = &ownerIntID
					} else {
						fr.OwnerID = nil
					}
				} else {
					fr.OwnerID = nil
				}

				tx.Create(fr)
				extToIntID[extID] = fr.ID
				// ИСПРАВЛЕНИЕ: Добавляем создание связи
				tx.Create(&models.ExternalSystemLink{InternalID: fr.ID, SystemName: "naumen", ServiceDeskUUID: extID, EntityType: "FiscalRegister", LastSyncedAt: time.Now()})
			}
		}

		s.logger.Info("Создание Контрактов и связей...")
		var linksToCreate []models.CompanyContract
		contractsCreated := 0

		for i, data := range contractData {
			extID, _ := data["UUID"].(string)
			if extID == "" {
				s.logger.Warn("Пропуск контракта: отсутствует UUID", "index", i)
				continue
			}
			contractModel, err := sdClient.Mapper().DataToContract(ctx, mapperCtx, data)
			if err != nil {
				s.logger.Error("Ошибка маппинга контракта", "error", err, "index", i)
				continue
			}

			if err := tx.Create(contractModel).Error; err != nil {
				s.logger.Error("CRITICAL: Ошибка создания контракта", "error", err, "uuid", extID)
				continue
			}

			contractsCreated++
			extToIntID[extID] = contractModel.ID

			link := &models.ExternalSystemLink{
				InternalID:      contractModel.ID,
				SystemName:      "naumen",
				ServiceDeskUUID: extID,
				EntityType:      "Contract",
				LastSyncedAt:    time.Now(),
			}

			if err := tx.Create(link).Error; err != nil {
				s.logger.Error("Ошибка создания ExternalSystemLink для контракта", "uuid", extID, "error", err)
				return err
			}

			companyExtIDs := sdClient.Mapper().GetCompanyUUIDsFromContract(data)
			s.logger.Debug("Создание связей для контракта", "uuid", extID, "count", len(companyExtIDs))
			for _, compExtID := range companyExtIDs {
				if compIntID, ok := extToIntID[compExtID]; ok {
					linksToCreate = append(linksToCreate, models.CompanyContract{CompanyID: compIntID, ContractID: contractModel.ID})
				} else {
					s.logger.Warn("Пропуск связи: компания не найдена", "contractUUID", extID, "companyUUID", compExtID)
				}
			}
		}

		s.logger.Info("Контракты обработаны", "created_count", contractsCreated, "links_to_create", len(linksToCreate))

		if len(linksToCreate) > 0 {
			s.logger.Info("Массовая вставка связей CompanyContract...", "count", len(linksToCreate))
			if err := tx.Table("company_contracts").CreateInBatches(linksToCreate, batchSize).Error; err != nil {
				s.logger.Error("CRITICAL: Ошибка вставки связей company_contracts", "error", err)
				return err
			}
		}

		s.logger.Info("Пересчет статусов контрактов...")
		activeCompanyExtIDs := make(map[string]struct{})
		for _, data := range contractData {
			if state, _ := data["state"].(string); state == "active" {
				for _, compExtID := range sdClient.Mapper().GetCompanyUUIDsFromContract(data) {
					activeCompanyExtIDs[compExtID] = struct{}{}
				}
			}
		}

		var activeCompanyIntIDs []string
		for extID, intID := range extToIntID {
			if _, isActive := activeCompanyExtIDs[extID]; isActive {
				var count int64
				tx.Model(&company.Company{}).Where("id = ?", intID).Count(&count)
				if count > 0 {
					activeCompanyIntIDs = append(activeCompanyIntIDs, intID)
				}
			}
		}

		if err := tx.Model(&company.Company{}).Session(&gorm.Session{AllowGlobalUpdate: true}).Update("active_contract", false).Error; err != nil {
			return err
		}
		if len(activeCompanyIntIDs) > 0 {
			if err := tx.Model(&company.Company{}).Where("id IN ?", activeCompanyIntIDs).Update("active_contract", true).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// clearDatabase удаляет все таблицы для полного пересоздания базы.
func (s *Seeder) clearDatabase() error {
	tables := []string{
		"server_additional_owners", "company_contracts", "external_system_links",
		"reconciliation_tasks", "agent_files", "fiscal_registers", "workstations",
		"servers", "contracts", "companies", "users",
	}
	s.logger.Info("Удаление существующих таблиц...")
	for _, table := range tables {
		if err := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)).Error; err != nil {
			// Мы не считаем ошибкой, если таблицы не существует, но логируем другие ошибки
			if !strings.Contains(err.Error(), "does not exist") {
				s.logger.Warn("Не удалось удалить таблицу (возможно, ее не было)", "table", table, "error", err)
			}
		}
	}
	return nil
}

func (s *Seeder) getModelForType(entityType string) interface{} {
	switch entityType {
	case "Server":
		return &server.Server{}
	case "Workstation":
		return &workstation.Workstation{}
	case "FiscalRegister":
		return &fiscal.FiscalRegister{}
	default:
		return nil
	}
}
