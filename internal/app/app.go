// Файл: internal/app/app.go
package app

import (
	"context"
	"etalon-server/internal/core/gateways"
	"etalon-server/internal/core/processing"
	"etalon-server/internal/core/workers"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/db"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/iiko"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/plugins/naumen"
	"etalon-server/internal/pkg/seeder"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/handlers"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/pkg/eventbus"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"gorm.io/gorm"
)

// Application хранит все зависимости приложения (DI-контейнер).
type Application struct {
	Config   *config.Config
	Logger   logger.LoggerInterface
	DB       *gorm.DB
	EventBus eventbus.EventBus
	Seeder   *seeder.Seeder

	// Gateways & Workers
	SDeskGateway      gateways.ServiceDeskGateway
	DuplicatesGateway gateways.DuplicatesGateway
	PollingGateway    gateways.ServerPollingGateway
	AgentFTPGateway   gateways.AgentFTPGateway
	FRUpdateFounder   workers.FRUpdateFounder
	SDEditor          workers.SDEditorWorker

	// Handlers
	CrudHandler          *handlers.CrudHandler
	SearchHandler        *handlers.SearchHandler
	SyncHandler          *handlers.SyncHandler
	TaskHandler          *handlers.TaskHandler
	AgentHandler         *handlers.AgentHandler
	ServerActionsHandler *handlers.ServerActionsHandler
	AuthHandler          *handlers.AuthHandler
	ContractHandler      *handlers.ContractHandler
	UserHandler          *handlers.UserHandler
	DebugHandler         *handlers.DebugHandler
}

// New создает и инициализирует новый экземпляр Application.
func New() (*Application, error) {
	app := &Application{}
	var err error

	app.Config = config.New()
	app.Logger = logger.NewSlogLogger(app.Config.LogDir, "app", app.Config.LogLevel, app.Config.DisableFileLogging)
	app.Logger.Info("Инициализация приложения etalon-server...")

	if err = os.MkdirAll(app.Config.FTPCachePath, 0755); err != nil {
		app.Logger.Fatal("Не удалось создать директорию для кэша FTP", "error", err)
	}

	app.DB, err = setupDatabase(app.Config, app.Logger)
	if err != nil {
		return nil, err
	}

	app.EventBus = eventbus.NewInMemoryEventBus(1000)

	// Инициализация слоев
	repos := setupRepositories(app.DB)
	clients := setupExternalClients(app.Config, app.Logger, app.DB, repos.LinkRepo)

	app.Seeder = seeder.NewSeeder(app.Logger, app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.ContractRepo)

	services := setupServices(app, repos, clients)
	setupBackgroundServices(app, repos, clients, services)
	setupHandlers(app, repos, services)

	return app, nil
}

// Run запускает приложение (HTTP-сервер и фоновые службы).
func (a *Application) Run() {
	server := &http.Server{
		Addr:    ":" + a.Config.ServerPort,
		Handler: a.setupRouter(),
	}

	mainCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	// Запуск фоновых служб
	a.runBackgroundServices(mainCtx, &wg)

	// Запуск HTTP-сервера
	wg.Add(1)
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
	}

	wg.Wait()
	a.Logger.Info("Приложение успешно завершило работу.")
}

// SeedDBAndExit выполняет наполнение БД и завершает работу.
func (a *Application) SeedDBAndExit() {
	a.Logger.Info("Запуск в режиме наполнения базы данных (seeding)...")
	mockClient := seeder.NewMockServiceDeskClient(a.Logger, "./tools/seeder/mock_data")
	if err := a.Seeder.SeedDatabase(mockClient); err != nil {
		a.Logger.Fatal("Ошибка при наполнении базы данных", "error", err)
	}
	a.Logger.Info("Наполнение базы данных успешно завершено. Программа завершает работу.")
	os.Exit(0)
}

// --- Функции-строители для декомпозиции New() ---

func setupDatabase(cfg *config.Config, log logger.LoggerInterface) (*gorm.DB, error) {
	database, err := db.NewConnection(cfg)
	if err != nil {
		log.Fatal("Не удалось подключиться к базе данных", "error", err)
	}
	log.Info("Подключение к базе данных установлено")

	log.Info("Запуск миграций базы данных...")
	if err := db.Migrate(database); err != nil {
		log.Fatal("Не удалось выполнить миграцию схемы БД", "error", err)
	}
	log.Info("Миграции базы данных успешно завершены.")

	if err := db.SeedAdminUser(cfg, database, log); err != nil {
		log.Fatal("Не удалось создать пользователя-администратора", "error", err)
	}

	return database, nil
}

type Repositories struct {
	CompanyRepo     repositories.CompanyRepo
	ServerRepo      repositories.ServerRepo
	WorkstationRepo repositories.WorkstationRepo
	FRRepo          repositories.FiscalRegisterRepo
	AgentRepo       repositories.AgentRepo
	ContractRepo    repositories.ContractRepo
	TaskRepo        repositories.TaskRepo
	UserRepo        repositories.UserRepo
	LinkRepo        repositories.LinkRepo
}

func setupRepositories(db *gorm.DB) Repositories {
	return Repositories{
		CompanyRepo:     repositories.NewCompanyRepo(db),
		ServerRepo:      repositories.NewServerRepo(db),
		WorkstationRepo: repositories.NewWorkstationRepo(db),
		FRRepo:          repositories.NewFiscalRegisterRepo(db),
		AgentRepo:       repositories.NewAgentRepo(db),
		ContractRepo:    repositories.NewContractRepo(db),
		TaskRepo:        repositories.NewTaskRepo(db),
		UserRepo:        repositories.NewUserRepo(db),
		LinkRepo:        repositories.NewLinkRepo(db),
	}
}

type ExternalClients struct {
	SDClient   external.ExternalSystemClient
	FTPClient  services.FTPClient
	IikoClient iiko.IikoClient
}

func setupExternalClients(cfg *config.Config, log logger.LoggerInterface, db *gorm.DB, linkRepo repositories.LinkRepo) ExternalClients {
	return ExternalClients{
		SDClient:   naumen.NewNaumenClient(cfg, log.With("component", "naumen_client"), db, linkRepo),
		FTPClient:  services.NewFTPClient(cfg, log.With("component", "ftp_client")),
		IikoClient: iiko.NewIikoClient(cfg.RequestTimeout, log.With("component", "iiko_client")),
	}
}

type Services struct {
	AuthService           services.AuthService
	AgentService          services.AgentService
	TaskResolutionService services.TaskResolutionService
	ServerActionsService  services.ServerActionsService
	EntityMatcherService  services.EntityMatcherService
}

func setupServices(app *Application, repos Repositories, clients ExternalClients) Services {
	return Services{
		AuthService:           services.NewAuthService(app.Config, repos.UserRepo, app.Logger.With("component", "auth_service")),
		AgentService:          services.NewAgentService(app.Logger.With("component", "agent_service"), repos.AgentRepo, repos.CompanyRepo, app.DB, app.EventBus),
		TaskResolutionService: services.NewTaskResolutionService(app.Logger.With("component", "task_resolution"), app.DB, app.EventBus, repos.TaskRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
		ServerActionsService:  services.NewServerActionsService(app.Config, app.Logger.With("component", "server_actions"), app.EventBus, repos.ServerRepo, repos.CompanyRepo, app.DB, clients.IikoClient),
		EntityMatcherService:  services.NewEntityMatcherService(app.Logger.With("component", "entity_matcher"), repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
	}
}

func setupBackgroundServices(app *Application, repos Repositories, clients ExternalClients, srvs Services) {
	engine := processing.NewProcessingEngine(app.Logger.With("component", "processing_engine"), repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.CompanyRepo, repos.TaskRepo, srvs.EntityMatcherService)
	orchestrator := processing.NewOrchestrator(app.Logger.With("component", "orchestrator"), app.DB, app.EventBus, clients.SDClient, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.TaskRepo, repos.LinkRepo, engine)
	orchestrator.Start(context.Background()) // Оркестратор только подписывается, активной работы не ведет

	app.SDeskGateway = gateways.NewServiceDeskGateway(app.Config, clients.SDClient, app.EventBus, app.Logger.With("component", "sdesk_gateway"), app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
	app.DuplicatesGateway = gateways.NewDuplicatesGateway(app.Config, app.DB, app.EventBus, app.Logger.With("component", "duplicates_gateway"))
	app.PollingGateway = gateways.NewServerPollingGateway(app.Config, app.Logger.With("component", "iiko_polling_gateway"), repos.ServerRepo, clients.IikoClient, app.EventBus)
	app.AgentFTPGateway = gateways.NewAgentFTPGateway(app.Config, app.Logger.With("component", "agent_ftp_gateway"), app.DB, clients.FTPClient, app.EventBus)
	app.FRUpdateFounder = workers.NewFRUpdateFounder(app.Config, app.Logger.With("component", "fr_update_founder"), app.EventBus, repos.FRRepo, repos.LinkRepo, clients.SDClient)
	app.SDEditor = workers.NewSDEditorWorker(app.Logger.With("component", "sdesk_editor_worker"), app.DB, app.EventBus, clients.SDClient, repos.TaskRepo, repos.LinkRepo, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
}

func setupHandlers(app *Application, repos Repositories, srvs Services) {
	app.CrudHandler = handlers.NewCrudHandler(app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
	app.SearchHandler = handlers.NewSearchHandler(repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo)
	app.SyncHandler = handlers.NewSyncHandler(app.Seeder, app.Config.SeederKey)
	app.TaskHandler = handlers.NewTaskHandler(app.DB, srvs.TaskResolutionService, app.SDEditor, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo)
	app.AgentHandler = handlers.NewAgentHandler(srvs.AgentService)
	app.ServerActionsHandler = handlers.NewServerActionsHandler(srvs.ServerActionsService)
	app.AuthHandler = handlers.NewAuthHandler(srvs.AuthService)
	app.ContractHandler = handlers.NewContractHandler(app.DB, repos.ContractRepo)
	app.UserHandler = handlers.NewUserHandler(app.DB, srvs.AuthService, repos.UserRepo)
	app.DebugHandler = handlers.NewDebugHandler(app.EventBus)
}

// --- Функции-хелперы для Run() ---

func (a *Application) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins:   a.Config.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	})
	r.Use(corsMiddleware.Handler)
	// Сначала RequestID, чтобы он был доступен для логгера
	r.Use(chi_middleware.RequestID)
	// Затем наш LoggerInjector
	r.Use(middleware.LoggerInjector(a.Logger))
	// Стандартные middleware от chi
	r.Use(chi_middleware.RealIP, chi_middleware.Logger, chi_middleware.Recoverer)
	r.Use(chi_middleware.Timeout(60 * time.Second))

	// Публичные роуты
	r.Route("/api/auth", func(r chi.Router) {
		a.AuthHandler.RegisterRoutes(r)
	})
	r.Route("/api/agents", func(r chi.Router) {
		r.Use(middleware.AgentAuthMiddleware(a.Config.AgentAPIKey))
		a.AgentHandler.RegisterRoutes(r)
	})

	// Защищенная группа роутов для UI
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.JwtAuthMiddleware(a.Config))
		a.CrudHandler.RegisterRoutes(r)
		a.SearchHandler.RegisterRoutes(r)
		a.TaskHandler.RegisterRoutes(r)
		a.ServerActionsHandler.RegisterRoutes(r)
		a.ContractHandler.RegisterRoutes(r)

		r.Route("/users", func(r chi.Router) {
			r.Use(middleware.AdminRequiredMiddleware)
			a.UserHandler.RegisterRoutes(r)
		})
	})

	// Системные и отладочные роуты
	r.Route("/sync", func(r chi.Router) {
		a.SyncHandler.RegisterRoutes(r)
	})
	r.Route("/debug", func(r chi.Router) {
		a.DebugHandler.RegisterRoutes(r)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to Etalon Server"))
	})
	return r
}

func (a *Application) runBackgroundServices(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() { defer wg.Done(); a.EventBus.Start(ctx, a.Logger) }()

	a.SDEditor.Start(ctx)

	if a.Config.EnableDuplicatesGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.DuplicatesGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Шлюз поиска дубликатов отключен в конфигурации.")
	}

	if a.Config.EnableFRDiscrepancyFinder {
		wg.Add(1)
		go func() { defer wg.Done(); a.FRUpdateFounder.Start(ctx) }()
	} else {
		a.Logger.Info("Воркер поиска обновлений для ФР (FRUpdateFounder) отключен в конфигурации.")
	}

	if a.Config.EnableAgentFTPGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.AgentFTPGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Шлюз агентов (FTP) отключен в конфигурации.")
	}

	if a.Config.EnablePollingGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.PollingGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Шлюз опроса статусов iiko-серверов отключен в конфигурации.")
	}

	if a.Config.EnableSDeskGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.SDeskGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Шлюз синхронизации сущностей с ServiceDesk отключен в конфигурации.")
	}
}
