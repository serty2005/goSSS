// internal/app/app.go
package app

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/db"
	"etalon-server/internal/handlers"
	"etalon-server/internal/logger"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/seeder"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
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
	"gorm.io/gorm"
)

// Application хранит все зависимости приложения (DI-контейнер).
type Application struct {
	Config               *config.Config
	Logger               *zap.Logger
	DB                   *gorm.DB
	ReconcilerSvc        services.ReconcilerService
	ServerPollingSvc     services.ServerPollingService
	SDeskSyncSvc         services.SDeskSyncService
	Seeder               *seeder.Seeder
	CrudHandler          *handlers.CrudHandler
	SearchHandler        *handlers.SearchHandler
	SyncHandler          *handlers.SyncHandler
	TaskHandler          *handlers.TaskHandler
	AgentHandler         *handlers.AgentHandler
	ServerActionsHandler *handlers.ServerActionsHandler // НОВЫЙ ОБРАБОТЧИК
	AgentSvc             services.AgentService
}

// New создает и инициализирует новый экземпляр Application.
func New() (*Application, error) {
	cfg := config.New()
	appLogger := logger.New(cfg.LogPath, cfg.DisableFileLogging)

	if err := os.MkdirAll(cfg.FTPCachePath, 0755); err != nil {
		appLogger.Fatal("Не удалось создать директорию для кэша FTP", zap.Error(err))
	}

	appLogger.Info("Инициализация приложения etalon-server...")

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
		&models.Agent{},
	)
	if err != nil {
		appLogger.Fatal("Не удалось выполнить миграцию схемы БД", zap.Error(err))
		return nil, err
	}
	appLogger.Info("Миграции базы данных успешно завершены.")

	cleanupService := services.NewCleanupService(database, appLogger)
	go cleanupService.CleanupFRDuplicates(context.Background())
	go cleanupService.CleanupServerDuplicatesAndJunk(context.Background())

	companyRepo := repositories.NewCompanyRepo(database)
	serverRepo := repositories.NewServerRepo(database)
	workstationRepo := repositories.NewWorkstationRepo(database)
	frRepo := repositories.NewFiscalRegisterRepo(database)
	agentRepo := repositories.NewAgentRepo(database)
	rmsClient := utils.NewRMSClient(cfg.RequestTimeout, appLogger)

	sdClient := services.NewServiceDeskClient(cfg, appLogger)
	sdeskSyncService := services.NewSDeskSyncService(cfg, database, sdClient, companyRepo, serverRepo, workstationRepo, frRepo, appLogger)
	ftpClient := services.NewFTPClient(cfg, appLogger)
	reconcilerService := services.NewReconcilerService(cfg, appLogger, database, ftpClient, serverRepo, workstationRepo, frRepo)
	agentService := services.NewAgentService(appLogger, agentRepo, companyRepo, reconcilerService, database)
	serverPollingService := services.NewServerPollingService(cfg, appLogger, database, serverRepo, rmsClient) // Добавлен db в конструктор
	dbSeeder := seeder.NewSeeder(appLogger, database, companyRepo, serverRepo, workstationRepo, frRepo)

	crudHandler := handlers.NewCrudHandler(appLogger, database, companyRepo, serverRepo, workstationRepo, frRepo)
	searchHandler := handlers.NewSearchHandler(appLogger, companyRepo, serverRepo, workstationRepo, frRepo)
	syncHandler := handlers.NewSyncHandler(appLogger, dbSeeder, cfg.SeederKey)
	taskHandler := handlers.NewTaskHandler(appLogger, database)
	agentHandler := handlers.NewAgentHandler(appLogger, agentService)
	serverActionsHandler := handlers.NewServerActionsHandler(appLogger, serverPollingService) // ИНИЦИАЛИЗАЦИЯ НОВОГО ОБРАБОТЧИКА

	return &Application{
		Config:               cfg,
		Logger:               appLogger,
		DB:                   database,
		ReconcilerSvc:        reconcilerService,
		ServerPollingSvc:     serverPollingService,
		SDeskSyncSvc:         sdeskSyncService,
		Seeder:               dbSeeder,
		CrudHandler:          crudHandler,
		SearchHandler:        searchHandler,
		SyncHandler:          syncHandler,
		TaskHandler:          taskHandler,
		AgentHandler:         agentHandler,
		ServerActionsHandler: serverActionsHandler, // ДОБАВЛЕН НОВЫЙ ОБРАБОТЧИК
		AgentSvc:             agentService,
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

	r.Route("/api", func(r chi.Router) {
		a.CrudHandler.RegisterRoutes(r)
		a.SearchHandler.RegisterRoutes(r)
		a.TaskHandler.RegisterRoutes(r)
		a.ServerActionsHandler.RegisterRoutes(r) // РЕГИСТРАЦИЯ НОВЫХ РОУТОВ
		r.Route("/agents", func(r chi.Router) {
			r.Use(handlers.AgentAuthMiddleware(a.Config.AgentAPIKey))
			a.AgentHandler.RegisterRoutes(r)
		})
	})
	r.Route("/sync", func(r chi.Router) {
		a.SyncHandler.RegisterRoutes(r)
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

	if a.Config.EnableReconcilerWorker {
		wg.Add(1)
		go func() { defer wg.Done(); a.ReconcilerSvc.Start(mainCtx) }()
	} else {
		a.Logger.Info("Воркер Reconciler (FTP) отключен в конфигурации.")
	}

	if a.Config.EnableServerPollingWorker {
		wg.Add(1)
		go func() { defer wg.Done(); a.ServerPollingSvc.Start(mainCtx) }()
	} else {
		a.Logger.Info("Воркер опроса статусов серверов отключен в конфигурации.")
	}

	if a.Config.EnableSDeskSyncWorker {
		wg.Add(1)
		go func() { defer wg.Done(); a.SDeskSyncSvc.Start(mainCtx) }()
	} else {
		a.Logger.Info("Воркер синхронизации с ServiceDesk отключен в конфигурации.")
	}

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
