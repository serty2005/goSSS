// Файл: internal/infra/db/db.go
package db

import (
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
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
func Migrate(db *gorm.DB) error {
	if err := cleanupOrphanUserIntegrations(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&user.User{}, &user.Role{}, &user.Integration{},
		&tickets.Ticket{}, &tickets.TicketHistory{}, &tickets.Attachment{}, &tickets.TicketComment{},
		&tickets.FileAsset{}, &tickets.TicketFileLink{},
		&company.Company{},
		&server.Server{},
		&workstation.Workstation{},
		&fiscal.FiscalRegister{},
		&contract.Contract{},
		&contract.MailImport{},
		&contract.ServicePointSyncConflict{},
		&models.AgentFile{},
		&models.ReconciliationTask{},
		&models.Agent{},
		&models.AgentRegistrationAttempt{},
		&models.AgentSessionToken{},
		&models.AgentCOMSignatureRule{},
		&models.PublishedAgentAdapter{},
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
		&models.ExternalSystemLink{},
		&models.EquipmentStatusLog{},
		&bitrix.DealLink{},
		&bitrix.CommentLink{},
		&bitrix.IgnoredDeal{},
		&bitrix.UserMap{},
		&bitrix.ServicePoint{},
		&bitrix.UserCache{},
		&bitrix.CompanyServicePointMapping{},
		&bitrix.IncomingEvent{},
	); err != nil {
		return err
	}

	if err := ensureTicketFileIndexes(db); err != nil {
		return err
	}
	if err := migrateLegacyAttachments(db); err != nil {
		return err
	}
	if err := EnsureDefaultPublishedAgentAdapters(db); err != nil {
		return err
	}

	return nil
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
