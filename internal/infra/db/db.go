// Файл: internal/infra/db/db.go
package db

import (
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
	return db.AutoMigrate(
		// Домен Users & RBAC
		&user.User{}, &user.Role{},

		// Домен Tickets
		&tickets.Ticket{}, &tickets.TicketHistory{}, &tickets.Attachment{}, &tickets.TicketComment{},

		// CMDB (Используют обновленный common.Base без MetaClass)
		&company.Company{},
		&server.Server{},
		&workstation.Workstation{},
		&fiscal.FiscalRegister{},
		&contract.Contract{},

		// Вспомогательные модели
		&models.AgentFile{},
		&models.ReconciliationTask{},
		&models.Agent{},
		&models.CompanyContract{},
		&models.ExternalSystemLink{},
		&models.EquipmentStatusLog{},
	)
}

// SeedAdminUser создает пользователя-администратора, если он не существует.
func SeedAdminUser(cfg *config.Config, db *gorm.DB, logger logger.LoggerInterface) error {
	var count int64
	db.Model(&user.User{}).Where("username = ?", cfg.AdminUsername).Count(&count)

	if count > 0 {
		logger.Info("Пользователь-администратор уже существует, пропуск создания.")
		return nil
	}

	logger.Info("Создание пользователя-администратора по умолчанию...")

	// 1. Создаем роль admin, если её нет
	adminRole := user.Role{Name: "admin", Description: "Super Administrator"}
	db.FirstOrCreate(&adminRole, user.Role{Name: "admin"})

	// 2. Создаем пользователя
	admin := &user.User{
		Username: cfg.AdminUsername,
		FullName: cfg.AdminFullName,
		IsActive: true,
		Roles:    []user.Role{adminRole},
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
