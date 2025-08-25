// internal/seeder/seeder.go
package seeder

import (
	"context"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const batchSize = 100 // Размер пакета для вставки в БД

// Seeder отвечает за наполнение базы данных.
type Seeder struct {
	logger          *zap.Logger
	db              *gorm.DB
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	contractRepo    repositories.ContractRepo
}

// NewSeeder создает новый экземпляр Seeder.
func NewSeeder(
	logger *zap.Logger,
	db *gorm.DB,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	contractRepo repositories.ContractRepo,
) *Seeder {
	return &Seeder{
		logger:          logger,
		db:              db,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		contractRepo:    contractRepo,
	}
}

// SeedDatabase выполняет полный цикл наполнения.
func (s *Seeder) SeedDatabase(sdClient services.ServiceDeskClient) error {
	s.logger.Info("Начало процесса наполнения базы данных...")

	s.logger.Info("Шаг 1: Очистка таблиц...")
	if err := s.clearDatabase(); err != nil {
		return err
	}
	s.logger.Info("База данных успешно очищена.")

	ctx := context.Background()

	s.logger.Info("Шаг 2: Загрузка и вставка Компаний (в 2 прохода)...")
	if err := s.seedCompanies(ctx, sdClient); err != nil {
		return err
	}

	s.logger.Info("Шаг 3: Загрузка и вставка Контрактов и их связей...")
	if err := s.seedContractsAndLinks(ctx, sdClient); err != nil {
		return err
	}

	s.logger.Info("Шаг 4: Пересчет статусов ActiveContract для всех компаний...")
	if err := s.recalculateAllCompanyStatuses(ctx, sdClient); err != nil {
		return err
	}

	s.logger.Info("Получение UUID всех загруженных компаний для проверки связей оборудования...")
	companyUUIDs, err := s.getAllCompanyUUIDs()
	if err != nil {
		return err
	}
	s.logger.Info("UUID компаний получены", zap.Int("count", len(companyUUIDs)))

	s.logger.Info("Шаг 5: Загрузка и вставка Серверов...")
	s.seedServers(ctx, sdClient, companyUUIDs)

	s.logger.Info("Шаг 6: Загрузка и вставка Рабочих станций...")
	s.seedWorkstations(ctx, sdClient, companyUUIDs)

	s.logger.Info("Шаг 7: Загрузка и вставка Фискальных регистраторов...")
	s.seedFiscalRegisters(ctx, sdClient, companyUUIDs)

	s.logger.Info("Процесс наполнения базы данных завершен.")
	return nil
}

// clearDatabase удаляет все данные из таблиц в правильном порядке.
func (s *Seeder) clearDatabase() error {
	tables := []string{
		"company_contracts",
		"reconciliation_tasks",
		"agent_files",
		"fiscal_registers",
		"workstations",
		"servers",
		"contracts",
		"companies",
	}

	for _, table := range tables {
		s.logger.Info("Очистка таблицы...", zap.String("table", table))
		if err := s.db.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
			return fmt.Errorf("ошибка при очистке таблицы %s: %w", table, err)
		}
	}
	return nil
}

// seedCompanies загружает и сохраняет компании в два прохода.
func (s *Seeder) seedCompanies(ctx context.Context, sdClient services.ServiceDeskClient) error {
	remoteList, err := sdClient.FetchEntityList(ctx, "ou$company", true)
	if err != nil {
		s.logger.Error("Не удалось получить список компаний из мок-данных", zap.Error(err))
		return err
	}

	var companiesWithParent, companiesWithoutParent []models.Company
	for _, data := range remoteList {
		company, err := services.DataToCompany(ctx, data, s.logger)
		if err != nil {
			s.logger.Warn("Пропуск компании из-за ошибки маппинга", zap.Error(err))
			continue
		}
		if company.ParentServiceDeskUUID != nil && *company.ParentServiceDeskUUID != "" {
			companiesWithParent = append(companiesWithParent, *company)
		} else {
			companiesWithoutParent = append(companiesWithoutParent, *company)
		}
	}

	// 1. Вставляем компании БЕЗ родителей
	if len(companiesWithoutParent) > 0 {
		if err := s.db.CreateInBatches(companiesWithoutParent, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке компаний без родителей", zap.Error(err))
			return err
		}
		s.logger.Info("Успешно вставлено компаний без родителей", zap.Int("count", len(companiesWithoutParent)))
	}

	// 2. Вставляем компании С родителями
	if len(companiesWithParent) > 0 {
		if err := s.db.CreateInBatches(companiesWithParent, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке компаний с родителями", zap.Error(err))
			return err
		}
		s.logger.Info("Успешно вставлено компаний с родителями", zap.Int("count", len(companiesWithParent)))
	}
	return nil
}

// seedContractsAndLinks загружает, сохраняет контракты и связи с компаниями.
func (s *Seeder) seedContractsAndLinks(ctx context.Context, sdClient services.ServiceDeskClient) error {
	remoteList, err := sdClient.FetchEntityList(ctx, "agreement$agreement", true)
	if err != nil {
		s.logger.Error("Не удалось получить список контрактов из мок-данных", zap.Error(err))
		return err
	}

	var contractsToCreate []models.Contract
	var linksToCreate []models.CompanyContract

	for _, data := range remoteList {
		contract, err := services.DataToContract(data)
		if err != nil {
			s.logger.Warn("Пропуск контракта из-за ошибки маппинга", zap.Error(err))
			continue
		}
		contractsToCreate = append(contractsToCreate, *contract)

		companyUUIDs := services.GetCompanyUUIDsFromContract(data)
		for _, compUUID := range companyUUIDs {
			linksToCreate = append(linksToCreate, models.CompanyContract{
				CompanyServiceDeskUUID:  compUUID,
				ContractServiceDeskUUID: *contract.ServiceDeskUUID,
			})
		}
	}

	// Вставляем контракты
	if len(contractsToCreate) > 0 {
		if err := s.db.CreateInBatches(contractsToCreate, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке контрактов", zap.Error(err))
			return err
		}
		s.logger.Info("Успешно вставлено контрактов", zap.Int("count", len(contractsToCreate)))
	}

	// Вставляем связи
	if len(linksToCreate) > 0 {
		if err := s.db.Table("company_contracts").CreateInBatches(linksToCreate, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при вставке связей компаний и контрактов", zap.Error(err))
			return err
		}
		s.logger.Info("Успешно вставлено связей компаний и контрактов", zap.Int("count", len(linksToCreate)))
	}
	return nil
}

// recalculateAllCompanyStatuses пересчитывает статусы ActiveContract для всех компаний.
func (s *Seeder) recalculateAllCompanyStatuses(ctx context.Context, sdClient services.ServiceDeskClient) error {
	s.logger.Info("Пересчет статусов ActiveContract для всех компаний...")

	remoteContracts, err := sdClient.FetchEntityList(ctx, "agreement$agreement", true)
	if err != nil {
		return err
	}

	activeCompanyUUIDs := make(map[string]struct{})
	for _, contractData := range remoteContracts {
		state, _ := contractData["state"].(string)
		if state == "active" {
			companyUUIDs := services.GetCompanyUUIDsFromContract(contractData)
			for _, uuid := range companyUUIDs {
				activeCompanyUUIDs[uuid] = struct{}{}
			}
		}
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		activeUUIDsList := make([]string, 0, len(activeCompanyUUIDs))
		for uuid := range activeCompanyUUIDs {
			activeUUIDsList = append(activeUUIDsList, uuid)
		}

		if len(activeUUIDsList) > 0 {
			res := tx.WithContext(ctx).Model(&models.Company{}).Where("service_desk_uuid IN ?", activeUUIDsList).Update("active_contract", true)
			if res.Error != nil {
				return res.Error
			}
			s.logger.Info("Установлен статус ActiveContract=true для компаний", zap.Int("count", int(res.RowsAffected)))
		}

		res := tx.WithContext(ctx).Model(&models.Company{}).Where("service_desk_uuid NOT IN ?", activeUUIDsList).Update("active_contract", false)
		if res.Error != nil {
			return res.Error
		}
		s.logger.Info("Установлен статус ActiveContract=false для компаний", zap.Int("count", int(res.RowsAffected)))

		return nil
	})
}

// getAllCompanyUUIDs извлекает все UUID из таблицы companies в виде map для быстрой проверки.
func (s *Seeder) getAllCompanyUUIDs() (map[string]struct{}, error) {
	var companyUUIDs []string
	result := s.db.Model(&models.Company{}).Pluck("service_desk_uuid", &companyUUIDs)
	if result.Error != nil {
		return nil, result.Error
	}
	uuidSet := make(map[string]struct{}, len(companyUUIDs))
	for _, uuid := range companyUUIDs {
		uuidSet[uuid] = struct{}{}
	}
	return uuidSet, nil
}

// seedServers загружает и сохраняет серверы.
func (s *Seeder) seedServers(ctx context.Context, sdClient services.ServiceDeskClient, companyUUIDs map[string]struct{}) {
	remoteList, err := sdClient.FetchEntityList(ctx, "objectBase$Server", true)
	if err != nil {
		s.logger.Error("Не удалось получить список серверов из мок-данных", zap.Error(err))
		return
	}
	servers := make([]models.Server, 0, len(remoteList))
	for _, data := range remoteList {
		server, err := services.DataToServer(data)
		if err != nil {
			uuid, _ := data["UUID"].(string)
			s.logger.Warn("Пропуск сервера из-за ошибки маппинга", zap.String("uuid", uuid), zap.Error(err))
			continue
		}
		if _, ok := companyUUIDs[*server.OwnerServiceDeskUUID]; !ok {
			s.logger.Warn("Пропуск сервера, т.к. его владелец отсутствует в БД", zap.String("server_uuid", *server.ServiceDeskUUID), zap.String("owner_uuid", *server.OwnerServiceDeskUUID))
			continue
		}
		servers = append(servers, *server)
	}
	if len(servers) > 0 {
		if err := s.db.CreateInBatches(servers, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке серверов", zap.Error(err))
		} else {
			s.logger.Info("Успешно вставлено серверов", zap.Int("count", len(servers)))
		}
	}
}

// seedWorkstations загружает и сохраняет рабочие станции.
func (s *Seeder) seedWorkstations(ctx context.Context, sdClient services.ServiceDeskClient, companyUUIDs map[string]struct{}) {
	remoteList, err := sdClient.FetchEntityList(ctx, "objectBase$Workstation", true)
	if err != nil {
		s.logger.Error("Не удалось получить список рабочих станций из мок-данных", zap.Error(err))
		return
	}
	workstations := make([]models.Workstation, 0, len(remoteList))
	for _, data := range remoteList {
		ws, err := services.DataToWorkstation(data)
		if err != nil {
			uuid, _ := data["UUID"].(string)
			s.logger.Warn("Пропуск рабочей станции из-за ошибки маппинга", zap.String("uuid", uuid), zap.Error(err))
			continue
		}
		if _, ok := companyUUIDs[*ws.OwnerServiceDeskUUID]; !ok {
			s.logger.Warn("Пропуск рабочей станции, т.к. ее владелец отсутствует в БД", zap.String("workstation_uuid", *ws.ServiceDeskUUID), zap.String("owner_uuid", *ws.OwnerServiceDeskUUID))
			continue
		}
		workstations = append(workstations, *ws)
	}
	if len(workstations) > 0 {
		if err := s.db.CreateInBatches(workstations, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке рабочих станций", zap.Error(err))
		} else {
			s.logger.Info("Успешно вставлено рабочих станций", zap.Int("count", len(workstations)))
		}
	}
}

// seedFiscalRegisters загружает и сохраняет фискальные регистраторы.
func (s *Seeder) seedFiscalRegisters(ctx context.Context, sdClient services.ServiceDeskClient, companyUUIDs map[string]struct{}) {
	remoteList, err := sdClient.FetchEntityList(ctx, "objectBase$FR", true)
	if err != nil {
		s.logger.Error("Не удалось получить список ФР из мок-данных", zap.Error(err))
		return
	}
	frs := make([]models.FiscalRegister, 0, len(remoteList))
	for _, data := range remoteList {
		fr, err := services.DataToFiscalRegister(data)
		if err != nil {
			uuid, _ := data["UUID"].(string)
			s.logger.Warn("Пропуск ФР из-за ошибки маппинга", zap.String("uuid", uuid), zap.Error(err))
			continue
		}
		if _, ok := companyUUIDs[*fr.OwnerServiceDeskUUID]; !ok {
			s.logger.Warn("Пропуск ФР, т.к. его владелец отсутствует в БД", zap.String("fr_uuid", *fr.ServiceDeskUUID), zap.String("owner_uuid", *fr.OwnerServiceDeskUUID))
			continue
		}
		frs = append(frs, *fr)
	}
	if len(frs) > 0 {
		if err := s.db.CreateInBatches(frs, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке ФР", zap.Error(err))
		} else {
			s.logger.Info("Успешно вставлено фискальных регистраторов", zap.Int("count", len(frs)))
		}
	}
}
