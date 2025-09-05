// internal/app/app.go
package app

import (
	"context"
	"encoding/json"
	"etalon-server/internal/config"
	"etalon-server/internal/db"
	"etalon-server/internal/gateways"
	"etalon-server/internal/handlers"
	"etalon-server/internal/logger"
	"etalon-server/internal/models"
	"etalon-server/internal/processing"
	"etalon-server/internal/repositories"
	"etalon-server/internal/seeder"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Application хранит все зависимости приложения (DI-контейнер).
type Application struct {
	Config               *config.Config
	Logger               *zap.Logger
	DB                   *gorm.DB
	ProcessingEngine     processing.ProcessingEngine
	EventBus             eventbus.EventBus
	Orchestrator         *processing.Orchestrator
	SDeskGateway         gateways.ServiceDeskGateway
	ContractGateway      gateways.ContractGateway
	DuplicatesGateway    gateways.DuplicatesGateway
	PollingGateway       gateways.ServerPollingGateway
	AgentFTPGateway      gateways.AgentFTPGateway
	Seeder               *seeder.Seeder
	CrudHandler          *handlers.CrudHandler
	SearchHandler        *handlers.SearchHandler
	SyncHandler          *handlers.SyncHandler
	TaskHandler          *handlers.TaskHandler
	AgentHandler         *handlers.AgentHandler
	ServerActionsHandler *handlers.ServerActionsHandler
	AuthHandler          *handlers.AuthHandler
	ServerActionsSvc     services.ServerActionsService
	AuthSvc              services.AuthService
	AgentSvc             services.AgentService
	SDEditorSvc          services.SDEditorService
	DebugHandler         *handlers.DebugHandler
}

// New создает и инициализирует новый экземпляр Application.
func New() (*Application, error) {
	cfg := config.New()

	appLogger := logger.New(cfg.LogDir, "app", cfg.LogLevel, cfg.DisableFileLogging)
	appLogger.Info("Инициализация приложения etalon-server...")

	if err := os.MkdirAll(cfg.FTPCachePath, 0755); err != nil {
		appLogger.Fatal("Не удалось создать директорию для кэша FTP", zap.Error(err))
	}

	database, err := db.NewConnection(cfg)
	if err != nil {
		appLogger.Fatal("Не удалось подключиться к базе данных", zap.Error(err))
		return nil, err
	}
	appLogger.Info("Подключение к базе данных установлено")

	appLogger.Info("Запуск миграций базы данных...")
	err = database.AutoMigrate(
		&models.Company{}, &models.Server{}, &models.Workstation{},
		&models.FiscalRegister{}, &models.AgentFile{}, &models.ReconciliationTask{},
		&models.Agent{}, &models.Contract{}, &models.CompanyContract{},
		&models.User{},
	)
	if err != nil {
		appLogger.Fatal("Не удалось выполнить миграцию схемы БД", zap.Error(err))
		return nil, err
	}
	appLogger.Info("Миграции базы данных успешно завершены.")

	if err := seedAdminUser(cfg, database, appLogger); err != nil {
		appLogger.Fatal("Не удалось создать пользователя-администратора", zap.Error(err))
		return nil, err
	}

	// --- Инициализация компонентов ---
	bus := eventbus.NewInMemoryEventBus(1000)

	// Репозитории
	companyRepo := repositories.NewCompanyRepo(database)
	serverRepo := repositories.NewServerRepo(database)
	workstationRepo := repositories.NewWorkstationRepo(database)
	frRepo := repositories.NewFiscalRegisterRepo(database)
	agentRepo := repositories.NewAgentRepo(database)
	contractRepo := repositories.NewContractRepo(database)
	taskRepo := repositories.NewTaskRepo(database)
	userRepo := repositories.NewUserRepo(database)
	rmsClient := utils.NewRMSClient(cfg.RequestTimeout, appLogger)

	// Создаем отдельные логгеры для каждого воркера/сервиса
	sdeskGatewayLogger := logger.New(cfg.LogDir, "sdesk_gateway", cfg.LogLevel, cfg.DisableFileLogging)    // <-- ИЗМЕНЕНИЕ
	orchestratorLogger := logger.New(cfg.LogDir, "orchestrator", cfg.LogLevel, cfg.DisableFileLogging)     // <-- ИЗМЕНЕНИЕ
	contractSyncLogger := logger.New(cfg.LogDir, "contract_sync", cfg.LogLevel, cfg.DisableFileLogging)    // <-- ИЗМЕНЕНИЕ
	serverPollingLogger := logger.New(cfg.LogDir, "server_polling", cfg.LogLevel, cfg.DisableFileLogging)  // <-- ИЗМЕНЕНИЕ
	reconcilerLogger := logger.New(cfg.LogDir, "reconciler", cfg.LogLevel, cfg.DisableFileLogging)         // <-- ИЗМЕНЕНИЕ
	duplicatesLogger := logger.New(cfg.LogDir, "duplicates_gateway", cfg.LogLevel, cfg.DisableFileLogging) // <-- ИЗМЕНЕНИЕ
	sdEditorLogger := logger.New(cfg.LogDir, "sdesk_editor", cfg.LogLevel, cfg.DisableFileLogging)

	// Сервисы, шлюзы и оркестратор
	sdClient := services.NewServiceDeskClient(cfg, appLogger)
	ftpClient := services.NewFTPClient(cfg, appLogger)
	agentService := services.NewAgentService(appLogger, agentRepo, companyRepo, database, bus)
	authService := services.NewAuthService(cfg, userRepo)
	taskResolutionService := services.NewTaskResolutionService(appLogger, database, taskRepo, serverRepo, workstationRepo, frRepo)
	dbSeeder := seeder.NewSeeder(appLogger, database, companyRepo, serverRepo, workstationRepo, frRepo, contractRepo)
	sdEditorService := services.NewSDEditorService(sdEditorLogger, sdClient, taskRepo)

	// Создаем движок, передавая ему matcher
	processingEngine := processing.NewProcessingEngine(appLogger, serverRepo, workstationRepo, frRepo, companyRepo, taskRepo, services.NewEntityMatcherService(appLogger, serverRepo, workstationRepo, frRepo))
	// --- Новая архитектура ---
	sdeskGateway := gateways.NewServiceDeskGateway(cfg, sdClient, bus, sdeskGatewayLogger, companyRepo, serverRepo, workstationRepo, frRepo)
	contractGateway := gateways.NewContractGateway(cfg, database, sdClient, contractRepo, bus, contractSyncLogger)
	duplicatesGateway := gateways.NewDuplicatesGateway(cfg, database, bus, duplicatesLogger)
	pollingGateway := gateways.NewServerPollingGateway(cfg, serverPollingLogger, serverRepo, rmsClient, bus)
	agentFTPGateway := gateways.NewAgentFTPGateway(cfg, reconcilerLogger, database, ftpClient, bus)
	orchestrator := processing.NewOrchestrator(orchestratorLogger, database, bus, companyRepo, serverRepo, workstationRepo, frRepo, taskRepo, processingEngine)
	serverActionsSvc := services.NewServerActionsService(appLogger, bus, serverRepo, companyRepo, database)

	// Обработчики
	crudHandler := handlers.NewCrudHandler(appLogger, database, companyRepo, serverRepo, workstationRepo, frRepo)
	searchHandler := handlers.NewSearchHandler(appLogger, companyRepo, serverRepo, workstationRepo, frRepo)
	syncHandler := handlers.NewSyncHandler(appLogger, dbSeeder, cfg.SeederKey, contractGateway)
	taskHandler := handlers.NewTaskHandler(appLogger, database, taskResolutionService, sdEditorService, serverRepo, workstationRepo, frRepo)
	agentHandler := handlers.NewAgentHandler(appLogger, agentService)
	serverActionsHandler := handlers.NewServerActionsHandler(appLogger, serverActionsSvc)
	authHandler := handlers.NewAuthHandler(appLogger, authService)
	debugHandler := handlers.NewDebugHandler(appLogger, bus)

	return &Application{
		Config:               cfg,
		Logger:               appLogger,
		DB:                   database,
		ProcessingEngine:     processingEngine,
		EventBus:             bus,
		Orchestrator:         orchestrator,
		SDeskGateway:         sdeskGateway,
		ContractGateway:      contractGateway,
		DuplicatesGateway:    duplicatesGateway,
		PollingGateway:       pollingGateway,
		AgentFTPGateway:      agentFTPGateway,
		Seeder:               dbSeeder,
		CrudHandler:          crudHandler,
		SearchHandler:        searchHandler,
		SyncHandler:          syncHandler,
		TaskHandler:          taskHandler,
		AgentHandler:         agentHandler,
		ServerActionsHandler: serverActionsHandler,
		AuthHandler:          authHandler,
		AuthSvc:              authService,
		ServerActionsSvc:     serverActionsSvc,
		AgentSvc:             agentService,
		SDEditorSvc:          sdEditorService,
		DebugHandler:         debugHandler,
	}, nil
}

// Run запускает приложение (HTTP-сервер и фоновые службы).
func (a *Application) Run() {
	defer a.Logger.Sync()

	r := chi.NewRouter()

	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins:   a.Config.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	})
	r.Use(corsMiddleware.Handler)

	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	// Публичные роуты для аутентификации
	r.Route("/api/auth", func(r chi.Router) {
		a.AuthHandler.RegisterRoutes(r)
	})

	// Роуты для агентов со своей аутентификацией
	r.Route("/api/agents", func(r chi.Router) {
		r.Use(handlers.AgentAuthMiddleware(a.Config.AgentAPIKey))
		a.AgentHandler.RegisterRoutes(r)
	})

	// Защищенная группа роутов для UI
	r.Route("/api", func(r chi.Router) {
		// Применяем middleware для проверки JWT
		r.Use(handlers.JwtAuthMiddleware(a.Config))

		// Регистрируем все защищенные хендлеры
		a.CrudHandler.RegisterRoutes(r)
		a.SearchHandler.RegisterRoutes(r)
		a.TaskHandler.RegisterRoutes(r)
		a.ServerActionsHandler.RegisterRoutes(r)

		// Пример группы роутов только для админов
		r.Route("/users", func(r chi.Router) {
			r.Use(handlers.AdminRequiredMiddleware)
			// Здесь будут регистрироваться роуты для UserHandler
			// a.UserHandler.RegisterRoutes(r)
		})
	})
	r.Route("/sync", func(r chi.Router) {
		a.SyncHandler.RegisterRoutes(r)
	})
	r.Route("/debug", func(r chi.Router) {
		a.DebugHandler.RegisterRoutes(r)
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to Etalon Server"))
	})

	server := &http.Server{
		Addr:    ":" + a.Config.ServerPort,
		Handler: r,
	}

	mainCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)

	// --- Запуск компонентов новой архитектуры ---
	// Шина событий
	go func() { defer wg.Done(); a.EventBus.Start(mainCtx, a.Logger) }()

	// Оркестратор (он только подписывается, активной работы не ведет)
	a.Orchestrator.Start(mainCtx)

	// Шлюз поиска дубликатов
	if a.Config.EnableDuplicatesGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.DuplicatesGateway.Start(mainCtx) }()
	} else {
		a.Logger.Info("Шлюз поиска дубликатов отключен в конфигурации.")
	}

	// Шлюз для данных от агентов (FTP)
	if a.Config.EnableAgentFTPGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.AgentFTPGateway.Start(mainCtx) }()
	} else {
		a.Logger.Info("Шлюз агентов (FTP) отключен в конфигурации.")
	}

	// Шлюз опроса статусов серверов
	if a.Config.EnablePollingGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.PollingGateway.Start(mainCtx) }()
	} else {
		a.Logger.Info("Шлюз опроса статусов серверов отключен в конфигурации.")
	}

	// Шлюз синхронизации сущностей с ServiceDesk
	if a.Config.EnableSDeskGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.SDeskGateway.Start(mainCtx) }()
	} else {
		a.Logger.Info("Шлюз синхронизации сущностей с ServiceDesk отключен в конфигурации.")
	}

	// Шлюз синхронизации контрактов
	if a.Config.EnableContractGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.ContractGateway.Start(mainCtx) }()
	} else {
		a.Logger.Info("Шлюз синхронизации контрактов отключен в конфигурации.")
	}

	// HTTP-сервер
	go func() {
		defer wg.Done()
		a.Logger.Info(fmt.Sprintf("Сервер запущен и слушает порт %s", a.Config.ServerPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Error("Не удалось запустить сервер", zap.String("port", a.Config.ServerPort), zap.Error(err))
			stop()
		}
	}()

	<-mainCtx.Done()

	a.Logger.Info("Получен сигнал завершения. Начинаю остановку...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		a.Logger.Fatal("Принудительная остановка сервера:", zap.Error(err))
	}

	wg.Wait()
	a.Logger.Info("Приложение успешно завершило работу.")
}

// SeedDBAndExit выполняет наполнение БД и завершает работу.
func (a *Application) SeedDBAndExit() {
	a.Logger.Info("Запуск в режиме наполнения базы данных (seeding)...")
	mockClient := seeder.NewMockServiceDeskClient(a.Logger, "./tools/seeder/mock_data")
	if err := a.Seeder.SeedDatabase(mockClient); err != nil {
		a.Logger.Fatal("Ошибка при наполнении базы данных", zap.Error(err))
	}
	a.Logger.Info("Наполнение базы данных успешно завершено. Программа завершает работу.")
	os.Exit(0)
}

// Новая функция для сидинга админа
func seedAdminUser(cfg *config.Config, db *gorm.DB, logger *zap.Logger) error {
	var count int64
	db.Model(&models.User{}).Where("username = ?", cfg.AdminUsername).Count(&count)

	if count > 0 {
		logger.Info("Пользователь-администратор уже существует, пропуск создания.")
		return nil
	}

	logger.Info("Создание пользователя-администратора по умолчанию...")
	rolesJSON, _ := json.Marshal([]string{"admin"})
	admin := &models.User{
		Username: cfg.AdminUsername,
		FullName: cfg.AdminFullName,
		Roles:    datatypes.JSON(rolesJSON),
	}
	if err := admin.HashPassword(cfg.AdminPassword); err != nil {
		return err
	}

	if err := db.Create(admin).Error; err != nil {
		return err
	}

	logger.Info("Пользователь-администратор успешно создан.", zap.String("username", cfg.AdminUsername))
	return nil
}
