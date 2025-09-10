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
	"etalon-server/internal/plugins/naumen"
	"etalon-server/internal/processing"
	"etalon-server/internal/repositories"
	"etalon-server/internal/seeder"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/internal/workers"
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
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Application хранит все зависимости приложения (DI-контейнер).
type Application struct {
	Config               *config.Config
	Logger               logger.LoggerInterface
	DB                   *gorm.DB
	ProcessingEngine     processing.ProcessingEngine
	EventBus             eventbus.EventBus
	Orchestrator         *processing.Orchestrator
	SDeskGateway         gateways.ServiceDeskGateway
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
	FRUpdateFounder      workers.FRUpdateFounder
	DebugHandler         *handlers.DebugHandler
}

// New создает и инициализирует новый экземпляр Application.
func New() (*Application, error) {
	cfg := config.New()

	// Создаем основной логгер на основе slog
	mainLogger := logger.NewSlogLogger(cfg.LogDir, "app", cfg.LogLevel, cfg.DisableFileLogging)
	mainLogger.Info("Инициализация приложения etalon-server...")

	if err := os.MkdirAll(cfg.FTPCachePath, 0755); err != nil {
		mainLogger.Error("Не удалось создать директорию для кэша FTP", "error", err)
		os.Exit(1)
	}

	database, err := db.NewConnection(cfg)
	if err != nil {
		mainLogger.Error("Не удалось подключиться к базе данных", "error", err)
		return nil, err
	}
	mainLogger.Info("Подключение к базе данных установлено")

	mainLogger.Info("Запуск миграций базы данных...")
	err = database.AutoMigrate(
		&models.Company{}, &models.Server{}, &models.Workstation{},
		&models.FiscalRegister{}, &models.AgentFile{}, &models.ReconciliationTask{},
		&models.Agent{}, &models.Contract{}, &models.CompanyContract{},
		&models.User{}, &models.ExternalSystemLink{},
	)
	if err != nil {
		mainLogger.Error("Не удалось выполнить миграцию схемы БД", "error", err)
		return nil, err
	}
	mainLogger.Info("Миграции базы данных успешно завершены.")

	if err := seedAdminUser(cfg, database, mainLogger); err != nil {
		mainLogger.Error("Не удалось создать пользователя-администратора", "error", err)
		return nil, err
	}

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
	linkRepo := repositories.NewLinkRepo(database)

	// Создаем логеры с контекстом от основного логгера
	sdeskGatewayLogger := mainLogger.With("component", "sdesk_gateway")
	orchestratorLogger := mainLogger.With("component", "orchestrator")
	serverPollingLogger := mainLogger.With("component", "server_polling")
	reconcilerLogger := mainLogger.With("component", "reconciler")
	duplicatesLogger := mainLogger.With("component", "duplicates_gateway")
	sdEditorLogger := mainLogger.With("component", "sdesk_editor")
	frUpdateFounderLogger := mainLogger.With("component", "fr_update_founder")

	// Сервисы, шлюзы и оркестратор
	sdClient := naumen.NewNaumenClient(cfg, mainLogger, database, linkRepo)
	ftpClient := services.NewFTPClient(cfg, mainLogger)
	rmsClient := utils.NewRMSClient(cfg.RequestTimeout, mainLogger)
	agentService := services.NewAgentService(mainLogger, agentRepo, companyRepo, database, bus)
	authService := services.NewAuthService(cfg, userRepo, mainLogger.With("component", "auth_service"))
	taskResolutionService := services.NewTaskResolutionService(mainLogger, database, bus, taskRepo, serverRepo, workstationRepo, frRepo)
	dbSeeder := seeder.NewSeeder(mainLogger, database, companyRepo, serverRepo, workstationRepo, frRepo, contractRepo)
	sdEditorService := services.NewSDEditorService(sdEditorLogger, database, bus, sdClient, taskRepo, linkRepo, companyRepo, serverRepo, workstationRepo, frRepo)
	processingEngine := processing.NewProcessingEngine(mainLogger, serverRepo, workstationRepo, frRepo, companyRepo, taskRepo, services.NewEntityMatcherService(mainLogger, serverRepo, workstationRepo, frRepo))

	sdeskGateway := gateways.NewServiceDeskGateway(cfg, sdClient, bus, sdeskGatewayLogger, database, companyRepo, serverRepo, workstationRepo, frRepo)
	duplicatesGateway := gateways.NewDuplicatesGateway(cfg, database, bus, duplicatesLogger)
	pollingGateway := gateways.NewServerPollingGateway(cfg, serverPollingLogger, serverRepo, rmsClient, bus)
	agentFTPGateway := gateways.NewAgentFTPGateway(cfg, reconcilerLogger, database, ftpClient, bus)

	// ВАЖНО: Конструктор Оркестратора пока остается старым! Мы отрефакторим его на следующем шаге.
	orchestrator := processing.NewOrchestrator(orchestratorLogger, database, bus, sdClient, companyRepo, serverRepo, workstationRepo, frRepo, taskRepo, linkRepo, processingEngine)

	serverActionsSvc := services.NewServerActionsService(mainLogger.With("component", "server_actions"), bus, serverRepo, companyRepo, database)
	frUpdateFounder := workers.NewFRUpdateFounder(cfg, frUpdateFounderLogger, bus, frRepo, linkRepo, sdClient)

	// Обработчики
	crudHandler := handlers.NewCrudHandler(mainLogger.With("component", "crud_handler"), database, companyRepo, serverRepo, workstationRepo, frRepo)
	searchHandler := handlers.NewSearchHandler(mainLogger.With("component", "search_handler"), companyRepo, serverRepo, workstationRepo, frRepo, linkRepo)
	syncHandler := handlers.NewSyncHandler(mainLogger.With("component", "sync_handler"), dbSeeder, cfg.SeederKey)
	taskHandler := handlers.NewTaskHandler(mainLogger.With("component", "task_handler"), database, taskResolutionService, sdEditorService, serverRepo, workstationRepo, frRepo, linkRepo)
	agentHandler := handlers.NewAgentHandler(mainLogger.With("component", "agent_handler"), agentService)
	serverActionsHandler := handlers.NewServerActionsHandler(mainLogger.With("component", "server_actions_handler"), serverActionsSvc)
	authHandler := handlers.NewAuthHandler(mainLogger.With("component", "auth_handler"), authService)
	debugHandler := handlers.NewDebugHandler(mainLogger.With("component", "debug_handler"), bus)

	return &Application{
		Config:               cfg,
		Logger:               mainLogger,
		DB:                   database,
		ProcessingEngine:     processingEngine,
		EventBus:             bus,
		Orchestrator:         orchestrator,
		SDeskGateway:         sdeskGateway,
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
		FRUpdateFounder:      frUpdateFounder,
		DebugHandler:         debugHandler,
	}, nil
}

// Run запускает приложение (HTTP-сервер и фоновые службы).
func (a *Application) Run() {

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
	a.SDEditorSvc.Start(mainCtx)

	// Шлюз поиска дубликатов
	if a.Config.EnableDuplicatesGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.DuplicatesGateway.Start(mainCtx) }()
	} else {
		a.Logger.Info("Шлюз поиска дубликатов отключен в конфигурации.")
	}

	// Воркер поиска обновлений для ФР
	if a.Config.EnableFRDiscrepancyFinder {
		wg.Add(1)
		go func() { defer wg.Done(); a.FRUpdateFounder.Start(mainCtx) }()
	} else {
		a.Logger.Info("Воркер поиска обновлений для ФР (FRUpdateFounder) отключен в конфигурации.")
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

	// HTTP-сервер
	go func() {
		defer wg.Done()
		a.Logger.Info(fmt.Sprintf("Сервер запущен и слушает порт %s", a.Config.ServerPort))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.Logger.Error("Не удалось запустить сервер", "port", a.Config.ServerPort, "error", err)
			stop()
		}
	}()

	<-mainCtx.Done()

	a.Logger.Info("Получен сигнал завершения. Начинаю остановку...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		a.Logger.Error("Принудительная остановка сервера:", "error", err)
		os.Exit(1)
	}

	wg.Wait()
	a.Logger.Info("Приложение успешно завершило работу.")
}

// SeedDBAndExit выполняет наполнение БД и завершает работу.
func (a *Application) SeedDBAndExit() {
	a.Logger.Info("Запуск в режиме наполнения базы данных (seeding)...")
	mockClient := seeder.NewMockServiceDeskClient(a.Logger, "./tools/seeder/mock_data")
	if err := a.Seeder.SeedDatabase(mockClient); err != nil {
		a.Logger.Error("Ошибка при наполнении базы данных", "error", err)
		os.Exit(1)
	}
	a.Logger.Info("Наполнение базы данных успешно завершено. Программа завершает работу.")
	os.Exit(0)
}

// Новая функция для сидинга админа
func seedAdminUser(cfg *config.Config, db *gorm.DB, logger logger.LoggerInterface) error {
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

	logger.Info("Пользователь-администратор успешно создан.", "username", cfg.AdminUsername)
	return nil
}
