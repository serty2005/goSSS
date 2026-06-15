// Файл: internal/infra/db/db.go
package db

import (
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// NewConnection создает и возвращает новое подключение к базе данных.
func NewConnection(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	return db, nil
}

// Migrate выполняет автомиграцию схемы базы данных.
func Migrate(cfg *config.Config, db *gorm.DB) error {
	if err := cleanupOrphanUserIntegrations(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&user.User{}, &user.Role{}, &user.Integration{},
		&tickets.Ticket{}, &tickets.TicketHistory{}, &tickets.Attachment{}, &tickets.TicketComment{},
		&tickets.FileAsset{}, &tickets.TicketFileLink{}, &tickets.TicketContact{},
		&company.Company{},
		&server.Server{},
		&workstation.Workstation{},
		&fiscal.FiscalRegister{},
		&contract.Contract{},
		&contract.MailImport{},
		&contract.ServicePointSyncRun{},
		&contract.ServicePointSyncConflict{},
		&models.AgentFile{},
		&models.ReconciliationTask{},
		&models.Agent{},
		&models.AgentRegistrationAttempt{},
		&models.AgentSessionToken{},
		&models.AgentCommand{},
		&models.AgentCOMSignatureRule{},
		&models.PublishedAgentAdapter{},
		&models.AgentAdapterRelease{},
		&models.AgentAdapterChannel{},
		&models.AgentObservation{},
		&models.Candidate{},
		&models.CandidateStatusHistory{},
		&models.CandidateWorkstationStaging{},
		&models.CandidateFiscalStaging{},
		&models.EntityDeletionCandidate{},
		&models.OwnerChangeHistory{},
		&models.NetworkCandidate{},
		&models.NetworkCandidateGroup{},
		&models.NetworkCandidateWSStaging{},
		&models.NetworkCandidateFRStaging{},
		&models.CompanyContract{},
		&models.Material{},
		&models.MaterialLink{},
		&models.Article{},
		&models.ArticleLink{},
		&models.ExternalSystemLink{},
		&models.AppLocalization{},
		&models.EquipmentStatusLog{},
		&bitrix.DealLink{},
		&bitrix.CommentLink{},
		&bitrix.IgnoredDeal{},
		&bitrix.UserMap{},
		&bitrix.ServicePoint{},
		&bitrix.UserCache{},
		&bitrix.CompanyServicePointMapping{},
		&bitrix.IncomingEvent{},
		&telephony.ProviderEmployee{},
		&telephony.IncomingEvent{},
		&telephony.Call{},
		&telephony.CallHistorySyncWindow{},
		&telephony.CallEvent{},
		&telephony.CallArtifact{},
		&telephony.CallTicketLink{},
		&telephony.PendingContext{},
		&telephony.Contact{},
		&telephony.ContactCompanyLink{},
		&pyrus.TicketLink{},
		&pyrus.CommentLink{},
		&pyrus.FileLink{},
		&pyrus.UserMap{},
		&pyrus.TicketContext{},
		&pyrus.IncomingEvent{},
		&pyrus.OutgoingEvent{},
	); err != nil {
		return err
	}

	if err := ensureTicketFileIndexes(db); err != nil {
		return err
	}
	if err := migrateLegacyAttachments(db); err != nil {
		return err
	}
	if err := MigrateLegacyPublishedAgentAdapters(db); err != nil {
		return err
	}
	if err := EnsureDefaultAgentAdapterCatalog(cfg, db); err != nil {
		return err
	}
	if err := ensureBitrixCompanyServicePointMappingIndexes(db); err != nil {
		return err
	}
	if err := ensureLegacyTicketContacts(db); err != nil {
		return err
	}
	if err := ensureAgentCommandSagaUniqueIndex(db); err != nil {
		return err
	}

	return nil
}

func ensureLegacyTicketContacts(db *gorm.DB) error {
	type ticketRow struct {
		ID        string
		ContactID uint
	}

	rows := make([]ticketRow, 0)
	if err := db.Model(&tickets.Ticket{}).
		Select("id, contact_id").
		Where("contact_id IS NOT NULL").
		Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" || row.ContactID == 0 {
			continue
		}

		var contact telephony.Contact
		if err := db.First(&contact, row.ContactID).Error; err != nil {
			continue
		}
		name := ""
		if contact.Name != nil {
			name = strings.TrimSpace(*contact.Name)
		}
		display := strings.TrimSpace(contact.PhoneDisplay)
		if display == "" {
			display = strings.TrimSpace(contact.PhoneNormalized)
		}
		contactID := contact.ID
		item := tickets.TicketContact{
			TicketID:           row.ID,
			ContactType:        tickets.ManagerTransferContactPhone,
			TelephonyContactID: &contactID,
			Value:              strings.TrimSpace(contact.PhoneNormalized),
			DisplayValue:       display,
			Name:               name,
			IsPrimary:          true,
			PrimaryMode:        tickets.TicketContactPrimaryModeAuto,
			Source:             tickets.TicketContactSourceManual,
		}
		if item.Value == "" {
			continue
		}
		var count int64
		if err := db.Model(&tickets.TicketContact{}).
			Where("ticket_id = ? AND contact_type = ? AND value = ?", row.ID, tickets.ManagerTransferContactPhone, item.Value).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureBitrixCompanyServicePointMappingIndexes(db *gorm.DB) error {
	if !db.Migrator().HasTable(&bitrix.CompanyServicePointMapping{}) {
		return nil
	}
	if err := dropLegacyUniqueBitrixMappingPointIndexes(db); err != nil {
		return err
	}
	for _, indexName := range []string{
		"idx_bitrix_company_service_point_mappings_bitrix_service_point_id",
		"idx_bitrix_company_service_point_mappings_bitrix_service_point_id_uniq",
		"idx_bitrix_company_service_point_mappings_point_id",
		"idx_bitrix_company_service_point_mappings_bitrix_servic838e2928",
	} {
		if db.Migrator().HasIndex(&bitrix.CompanyServicePointMapping{}, indexName) {
			if err := db.Migrator().DropIndex(&bitrix.CompanyServicePointMapping{}, indexName); err != nil {
				return err
			}
		}
	}
	return db.Migrator().CreateIndex(&bitrix.CompanyServicePointMapping{}, "BitrixServicePointID")
}

func dropLegacyUniqueBitrixMappingPointIndexes(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	type legacyIndex struct {
		SchemaName string `gorm:"column:schema_name"`
		IndexName  string `gorm:"column:index_name"`
	}

	var indexes []legacyIndex
	if err := db.Raw(`
		SELECT ns.nspname AS schema_name, idx.relname AS index_name
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_class tbl ON tbl.oid = i.indrelid
		JOIN pg_namespace ns ON ns.oid = tbl.relnamespace
		JOIN pg_attribute attr ON attr.attrelid = tbl.oid AND attr.attnum = ANY(i.indkey)
		WHERE tbl.relname = 'bitrix_company_service_point_mappings'
			AND i.indisunique = TRUE
			AND i.indisprimary = FALSE
			AND attr.attname = 'bitrix_service_point_id'
		GROUP BY ns.nspname, idx.relname
	`).Scan(&indexes).Error; err != nil {
		return err
	}

	for _, index := range indexes {
		indexRef := quotePostgresIdent(index.SchemaName) + "." + quotePostgresIdent(index.IndexName)
		if err := db.Exec("DROP INDEX IF EXISTS " + indexRef).Error; err != nil {
			return err
		}
	}
	return nil
}

func quotePostgresIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func cleanupOrphanUserIntegrations(db *gorm.DB) error {
	// Для существующих БД удаляем записи интеграций, указывающие на несуществующих пользователей.
	// Иначе AutoMigrate не сможет добавить FK fk_users_integrations.
	if !db.Migrator().HasTable("user_integrations") || !db.Migrator().HasTable("users") {
		return nil
	}

	return db.Exec(`
		DELETE FROM user_integrations ui
		WHERE NOT EXISTS (
			SELECT 1
			FROM users u
			WHERE u.id = ui.user_id
		);
	`).Error
}

// SeedAdminUser создает пользователя-администратора, если он не существует.
func SeedAdminUser(cfg *config.Config, db *gorm.DB, logger logger.LoggerInterface) error {
	if err := ensureSystemRoles(db); err != nil {
		return err
	}

	var count int64
	db.Model(&user.User{}).Where("username = ?", cfg.AdminUsername).Count(&count)

	if count > 0 {
		logger.Info("Пользователь-администратор уже существует, пропуск создания.")
		return nil
	}

	logger.Info("Создание пользователя-администратора по умолчанию...")

	// 1. Создаем роль admin, если её нет
	adminRole := user.Role{Name: user.RoleAdmin, Description: "Администратор системы"}
	db.FirstOrCreate(&adminRole, user.Role{Name: user.RoleAdmin})

	firstName, lastName := splitFullName(cfg.AdminFullName)

	// 2. Создаем пользователя
	admin := &user.User{
		Username:     cfg.AdminUsername,
		FullName:     strings.TrimSpace(cfg.AdminFullName),
		FirstName:    firstName,
		LastName:     lastName,
		Position:     user.RoleAdmin,
		ScheduleType: user.ScheduleFiveTwo,
		IsActive:     true,
		Roles:        []user.Role{adminRole},
	}
	if err := admin.HashPassword(cfg.AdminPassword); err != nil {
		return err
	}

	if err := db.Create(admin).Error; err != nil {
		return err
	}

	logger.Info("Пользователь-администратор успешно создан.", "username", cfg.AdminUsername)
	return nil
}

func ensureSystemRoles(db *gorm.DB) error {
	roles := []user.Role{
		{Name: user.RoleAdmin, Description: "Администратор системы"},
		{Name: user.RoleSupportSpecialist, Description: "Специалист техподдержки"},
		{Name: user.RoleIntern, Description: "Стажёр"},
	}

	for _, role := range roles {
		if err := db.FirstOrCreate(&role, user.Role{Name: role.Name}).Error; err != nil {
			return err
		}
	}

	return nil
}

func splitFullName(fullName string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(fullName))
	if len(parts) == 0 {
		return "Системный", "администратор"
	}
	if len(parts) == 1 {
		return parts[0], ""
	}

	return parts[0], strings.Join(parts[1:], " ")
}

// ensureAgentCommandSagaUniqueIndex создаёт partial unique index для идемпотентности
// scheduled-запусков адаптеров при горизонтальном масштабировании agent-gateway.
// Индекс уникален только для строк, где saga_id IS NOT NULL.
func ensureAgentCommandSagaUniqueIndex(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}

	const indexName = "idx_agent_commands_saga_id_unique"
	var exists bool
	if err := db.Raw(
		"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = ?)", indexName,
	).Scan(&exists).Error; err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Partial unique index: уникальность только для scheduled-команд с saga_id.
	return db.Exec(`
		CREATE UNIQUE INDEX idx_agent_commands_saga_id_unique
		ON agent_commands (agent_uuid, type, saga_id)
		WHERE saga_id IS NOT NULL
	`).Error
}

// MigrateAgentGateway выполняет миграцию только таблиц, необходимых для agent-gateway.
// Используется отдельным бинарником agent-gateway вместо полной Migrate монолита.
func MigrateAgentGateway(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.Agent{},
		&models.AgentRegistrationAttempt{},
		&models.AgentSessionToken{},
		&models.AgentCommand{},
	); err != nil {
		return err
	}
	return ensureAgentCommandSagaUniqueIndex(db)
}
