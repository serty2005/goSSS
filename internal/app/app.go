package app

import (
	"context"
	"etalon-server/internal/core/gateways"
	"etalon-server/internal/core/integrations"
	"etalon-server/internal/core/processing"
	"etalon-server/internal/core/workers"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/task"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/db"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/iiko"
	"etalon-server/internal/infra/logger"
	bitrixplugin "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/plugins/naumen"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/pkg/seeder"
	"etalon-server/internal/services"
	companySvc "etalon-server/internal/services/company"
	contractSvc "etalon-server/internal/services/contract"
	fiscalSvc "etalon-server/internal/services/fiscal"
	serverSvc "etalon-server/internal/services/server"
	taskSvc "etalon-server/internal/services/task"
	workstationSvc "etalon-server/internal/services/workstation"
	"etalon-server/internal/transport/http/handlers"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/pkg/eventbus"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chi_middleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
	httpSwagger "github.com/swaggo/http-swagger"
	"gorm.io/gorm"
)

// Application хранит все зависимости приложения (DI-контейнер).
type Application struct {
	Config   *config.Config
	Logger   logger.LoggerInterface
	DB       *gorm.DB
	EventBus eventbus.EventBus
	Seeder   *seeder.Seeder

	// Integration Manager
	IntegrationManager *integrations.Manager

	// Gateways & Workers
	SDeskGateway          gateways.ServiceDeskGateway
	DuplicatesGateway     gateways.DuplicatesGateway
	PollingGateway        gateways.ServerPollingGateway
	AgentFTPGateway       gateways.AgentFTPGateway
	FRUpdateFounder       workers.FRUpdateFounder
	SDEditor              workers.SDEditorWorker
	StatusActualityWorker workers.StatusActualityWorker
	TicketGateway         gateways.TicketGateway
	ContractGateway       gateways.ContractGateway
	BitrixModule          *bitrixModule

	// Handlers
	CompanyHandler          *handlers.CompanyHandler
	SearchHandler           *handlers.SearchHandler
	SyncHandler             *handlers.SyncHandler
	TaskHandler             *handlers.TaskHandler
	AgentHandler            *handlers.AgentHandler
	ServerActionsHandler    *handlers.ServerActionsHandler
	AuthHandler             *handlers.AuthHandler
	ContractHandler         *handlers.ContractHandler
	UserHandler             *handlers.UserHandler
	DebugHandler            *handlers.DebugHandler
	SSEHandler              *handlers.SSEHandler
	TicketHandler           *handlers.TicketHandler
	ServerHandler           *handlers.ServerHandler
	WorkstationHandler      *handlers.WSHandler
	FiscalHandler           *handlers.FiscalHandler
	BitrixHandler           *handlers.BitrixHandler
	BitrixWebhookHandler    *handlers.BitrixWebhookHandler
	CandidateHandler        *handlers.CandidateHandler
	NetworkCandidateHandler *handlers.NetworkCandidateHandler
	OwnerHistoryHandler     *handlers.OwnerHistoryHandler
	AgentObservationFeed    *handlers.AgentObservationFeedHandler
}

// New создает и инициализирует новый экземпляр Application.
func New() (*Application, error) {
	app := &Application{}
	var err error

	app.Config = config.New()
	app.Logger = logger.NewSlogLogger(app.Config.LogDir, "app", app.Config.LogLevel, app.Config.DisableFileLogging)
	app.Logger.Info("Запуск приложения etalon-server...")

	if err = os.MkdirAll(app.Config.FTPCachePath, 0755); err != nil {
		app.Logger.Fatal("Не удалось создать директорию для кэша FTP", "error", err)
	}

	app.DB, err = setupDatabase(app.Config, app.Logger)
	if err != nil {
		return nil, err
	}

	app.EventBus = eventbus.NewInMemoryEventBus(10000)

	// Запуск слоев
	repos := setupRepositories(app.DB)
	clients := setupExternalClients(app.Config, app.Logger, app.DB, repos.LinkRepo)

	app.Seeder = seeder.NewSeeder(app.Logger, app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.ContractRepo)

	services := setupServices(app, repos, clients)
	setupBackgroundServices(app, repos, clients, services)
	setupHandlers(app, repos, services)
	setupIntegrationModules(app, services)

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
		a.Logger.Error("Принудительная остановка сервера", "error", err)
	}

	wg.Wait()
	a.Logger.Info("Приложение успешно завершило работу.")
}

func (a *Application) SeedDBAndExit() {
	a.Logger.Info("Запуск в режиме наполнения базы данных (seeding)...")
	mockClient := seeder.NewMockServiceDeskClient(a.Logger, "./tools/seeder/mock_data")
	if err := a.Seeder.SeedDatabase(mockClient); err != nil {
		a.Logger.Fatal("Ошибка при наполнении базы данных", "error", err)
	}
	a.Logger.Info("Наполнение базы данных успешно завершено. Программа завершает работу.")
	os.Exit(0)
}

// SeedFromFTPCacheAndExit инициализирует БД из локального кэша FTP и загружает данные агентов.
// РСЃРїРѕР»СЊР·СѓРµС‚СЃСЏ при запуске с флагом --seed-ftp-cache для обработки ранее скачанных файлов
// без обращения к FTP-серверу.
func (a *Application) SeedFromFTPCacheAndExit() {
	a.Logger.Info("Запуск в режиме инициализации из FTP-кэша...")
	ctx := context.Background()

	// 1. РРЅРёС†РёР°Р»РёР·РёСЂСѓРµРј записи в БД из существующих файлов кэша
	if err := a.AgentFTPGateway.InitializeDBFromCache(ctx); err != nil {
		a.Logger.Warn("Ошибка инициализации БД из кэша", "error", err)
	}

	// 2. Загружаем данные агентов из кэша
	processedCount, err := a.AgentFTPGateway.LoadAgentDataFromCache(ctx)
	if err != nil {
		a.Logger.Warn("Ошибка загрузки данных из кэша", "error", err)
	}

	a.Logger.Info("РРЅРёС†РёР°Р»РёР·Р°С†РёСЏ из FTP-кэша завершена", "processed_files", processedCount)
	a.Logger.Info("Программа завершает работу.")
	os.Exit(0)
}

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

	if err := services.EnsureFiscalSerialUniqueness(context.Background(), database); err != nil {
		log.Fatal("Не удалось выполнить нормализацию и дедупликацию Р¤Р ", "error", err)
	}
	if err := db.SeedAdminUser(cfg, database, log); err != nil {
		log.Fatal("Не удалось создать пользователя-администратора", "error", err)
	}

	return database, nil
}

type Repositories struct {
	CompanyRepo          company.Repository
	ContractRepo         contract.Repository
	TicketRepo           tickets.TicketRepository
	ServerRepo           server.Repository
	WorkstationRepo      workstation.Repository
	FRRepo               fiscal.Repository
	AgentRepo            repositories.AgentRepo
	CandidateRepo        repositories.CandidateRepo
	TaskRepo             repositories.TaskRepo
	UserRepo             user.Repository
	LinkRepo             repositories.LinkRepo
	BitrixRepo           bitrix.Repository
	NetworkCandidateRepo repositories.NetworkCandidateRepo
	OwnerHistoryRepo     repositories.OwnerHistoryRepo
}

func setupRepositories(db *gorm.DB) Repositories {
	return Repositories{
		TicketRepo:           infraRepos.NewTicketRepo(db),
		CompanyRepo:          infraRepos.NewCompanyRepo(db),
		ContractRepo:         infraRepos.NewContractRepo(db),
		ServerRepo:           infraRepos.NewServerRepo(db),
		WorkstationRepo:      infraRepos.NewWorkstationRepo(db),
		FRRepo:               infraRepos.NewFiscalRegisterRepo(db),
		AgentRepo:            infraRepos.NewAgentRepo(db),
		CandidateRepo:        infraRepos.NewCandidateRepo(db),
		TaskRepo:             infraRepos.NewTaskRepo(db),
		UserRepo:             infraRepos.NewUserRepo(db),
		LinkRepo:             infraRepos.NewLinkRepo(db),
		BitrixRepo:           infraRepos.NewBitrixRepo(db),
		NetworkCandidateRepo: infraRepos.NewNetworkCandidateRepo(db),
		OwnerHistoryRepo:     infraRepos.NewOwnerHistoryRepo(db),
	}
}

type ExternalClients struct {
	SDClient     external.ExternalSystemClient
	FTPClient    services.FTPClient
	IikoClient   iiko.IikoClient
	BitrixClient *bitrixplugin.Client
	RedisClient  *redis.Client
}

func setupExternalClients(cfg *config.Config, log logger.LoggerInterface, db *gorm.DB, linkRepo repositories.LinkRepo) ExternalClients {
	var redisClient *redis.Client
	if strings.TrimSpace(cfg.RedisAddr) != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}
	return ExternalClients{
		SDClient:     naumen.NewNaumenClient(cfg, log.With("component", "naumen_client"), db, linkRepo),
		FTPClient:    services.NewFTPClient(cfg, log.With("component", "ftp_client")),
		IikoClient:   iiko.NewIikoClient(cfg.RequestTimeout, log.With("component", "iiko_client")),
		BitrixClient: bitrixplugin.NewClient(cfg, log.With("component", "bitrix_client")),
		RedisClient:  redisClient,
	}
}

type Services struct {
	AuthService             services.AuthService
	AgentService            services.AgentService
	AgentObservation        services.AgentObservationService
	TaskResolutionService   services.TaskResolutionService
	TaskService             task.Service
	ServerActionsService    services.ServerActionsService
	EntityMatcherService    services.EntityMatcherService
	TicketService           services.TicketService
	CompanyService          company.Service
	ContractService         contract.Service
	ServerService           server.Service
	WorkstationService      workstation.Service
	FiscalService           fiscal.Service
	BitrixSyncService       services.BitrixSyncService
	BitrixIncomingService   services.BitrixIncomingService
	NetworkCandidateService services.NetworkCandidateService
}

func setupServices(app *Application, repos Repositories, clients ExternalClients) Services {
	transactor := db.NewGormTransactor(app.DB)

	// Создаем сервисы для определения владельца network-hub
	ownerResolver := services.NewOwnerResolverService(
		app.Logger.With("component", "owner_resolver"),
		app.DB,
		repos.CompanyRepo,
		repos.WorkstationRepo,
		repos.FRRepo,
	)
	hubDetector := services.NewNetworkHubDetectorService(
		app.Logger.With("component", "hub_detector"),
		app.DB,
		repos.CompanyRepo,
	)

	// Создаем AgentObservationService с внедренными сервисами
	obsService := services.NewAgentObservationServiceWithDeps(
		app.Logger.With("component", "agent_observation_service"),
		app.DB,
		ownerResolver,
		hubDetector,
	)

	return Services{
		AuthService:             services.NewAuthService(app.Config, repos.UserRepo, app.Logger.With("component", "auth_service")),
		AgentObservation:        obsService,
		AgentService:            services.NewAgentService(app.Logger.With("component", "agent_service"), repos.AgentRepo, repos.CompanyRepo, app.EventBus),
		TaskResolutionService:   services.NewTaskResolutionService(app.Logger.With("component", "task_resolution"), transactor, app.EventBus, repos.TaskRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
		TaskService:             taskSvc.NewService(app.Logger.With("component", "task_service"), repos.TaskRepo),
		ServerActionsService:    services.NewServerActionsService(app.Config, app.Logger.With("component", "server_actions"), app.EventBus, repos.ServerRepo, repos.CompanyRepo, clients.IikoClient),
		EntityMatcherService:    services.NewEntityMatcherService(app.Logger.With("component", "entity_matcher"), repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
		TicketService:           services.NewTicketService(app.Logger.With("component", "ticket_service"), repos.TicketRepo, repos.UserRepo, repos.CompanyRepo, repos.ContractRepo, clients.SDClient, app.Config, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.BitrixRepo),
		CompanyService:          companySvc.NewService(app.Logger.With("component", "company_service"), transactor, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo, repos.BitrixRepo),
		ContractService:         contractSvc.NewService(app.Logger.With("component", "contract_service"), transactor, repos.ContractRepo, repos.CompanyRepo, repos.LinkRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
		ServerService:           serverSvc.NewService(app.Logger.With("component", "server_service"), transactor, repos.ServerRepo, repos.OwnerHistoryRepo),
		WorkstationService:      workstationSvc.NewService(app.Logger.With("component", "workstation_service"), transactor, repos.WorkstationRepo, repos.OwnerHistoryRepo),
		FiscalService:           fiscalSvc.NewService(app.Logger.With("component", "fiscal_service"), transactor, repos.FRRepo, repos.OwnerHistoryRepo),
		BitrixSyncService:       services.NewBitrixSyncService(app.Config, app.Logger.With("component", "bitrix_sync_service"), clients.BitrixClient, clients.RedisClient, repos.TicketRepo, repos.UserRepo, repos.BitrixRepo),
		BitrixIncomingService:   services.NewBitrixIncomingService(app.Config, app.Logger.With("component", "bitrix_incoming_service"), clients.BitrixClient, clients.RedisClient, repos.TicketRepo, repos.UserRepo, repos.BitrixRepo, app.EventBus),
		NetworkCandidateService: services.NewNetworkCandidateService(repos.NetworkCandidateRepo),
	}
}

func setupBackgroundServices(app *Application, repos Repositories, clients ExternalClients, srvs Services) {
	// --- 1. РРЅРёС†РёР°Р»РёР·Р°С†РёСЏ Менеджера РРЅС‚РµРіСЂР°С†РёР№ ---
	app.IntegrationManager = integrations.NewManager(app.Logger.With("component", "integration_manager"))

	// --- 2. Настройка Адаптера Naumen ---
	mapperCtx := &external.MapperContext{
		DB:       app.DB,
		LinkRepo: repos.LinkRepo,
		Logger:   app.Logger,
	}
	// РСЃРїРѕР»СЊР·СѓРµРј существующий клиент, оборачивая его в адаптер
	naumenAdapter := naumen.NewNaumenAdapter(clients.SDClient, app.Logger.With("component", "naumen_adapter"), mapperCtx)

	// --- 3. Регистрация Провайдеров ---
	app.IntegrationManager.RegisterInventoryProvider(naumenAdapter)
	app.IntegrationManager.RegisterContractProvider(naumenAdapter)
	app.IntegrationManager.RegisterTicketProvider(naumenAdapter)

	// --- 4. Настройка остальных сервисов ---
	reconciliationEngine := processing.NewReconciliationEngine(repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.TaskRepo, repos.LinkRepo, srvs.EntityMatcherService, app.Logger.With("component", "reconciliation_engine"))
	engine := processing.NewProcessingEngine(app.Logger.With("component", "processing_engine"), repos.TaskRepo, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo, reconciliationEngine, srvs.EntityMatcherService)
	orchestrator := processing.NewOrchestrator(app.Logger.With("component", "orchestrator"), app.DB, app.EventBus, clients.SDClient, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.TaskRepo, repos.LinkRepo, engine, srvs.AgentObservation)
	orchestrator.Start(context.Background())

	// Передаем IntegrationManager вместо SDClient
	app.SDeskGateway = gateways.NewServiceDeskGateway(app.Config, app.IntegrationManager, app.EventBus, app.Logger.With("component", "sdesk_gateway"), app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)

	app.DuplicatesGateway = gateways.NewDuplicatesGateway(app.Config, app.DB, app.EventBus, app.Logger.With("component", "duplicates_gateway"))
	app.PollingGateway = gateways.NewServerPollingGateway(app.Config, app.Logger.With("component", "iiko_polling_gateway"), repos.ServerRepo, clients.IikoClient, app.EventBus)
	app.AgentFTPGateway = gateways.NewAgentFTPGateway(app.Config, app.Logger.With("component", "agent_ftp_gateway"), app.DB, clients.FTPClient, srvs.AgentObservation)
	app.FRUpdateFounder = workers.NewFRUpdateFounder(app.Config, app.Logger.With("component", "fr_update_founder"), app.EventBus, repos.FRRepo, repos.LinkRepo, app.IntegrationManager)
	app.SDEditor = workers.NewSDEditorWorker(app.Logger.With("component", "sdesk_editor_worker"), app.DB, app.EventBus, app.IntegrationManager, repos.TaskRepo, repos.LinkRepo, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
	app.StatusActualityWorker = workers.NewStatusActualityWorker(app.Config, app.Logger.With("component", "status_actuality_worker"), app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
	app.TicketGateway = gateways.NewTicketGateway(app.Config, app.Logger.With("component", "ticket_gateway"), app.IntegrationManager, repos.TicketRepo, app.EventBus, app.DB, repos.LinkRepo)
	app.ContractGateway = gateways.NewContractGateway(app.Config, app.Logger.With("component", "contract_gateway"), app.IntegrationManager, srvs.ContractService)
}

func setupHandlers(app *Application, repos Repositories, srvs Services) {
	app.ServerHandler = handlers.NewServerHandler(srvs.ServerService)
	app.WorkstationHandler = handlers.NewWSHandler(srvs.WorkstationService)
	app.FiscalHandler = handlers.NewFiscalHandler(srvs.FiscalService)
	app.CompanyHandler = handlers.NewCompanyHandler(srvs.CompanyService)
	app.SearchHandler = handlers.NewSearchHandler(repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo)
	app.SyncHandler = handlers.NewSyncHandler(app.Seeder, app.Config.SeederKey)
	app.TaskHandler = handlers.NewTaskHandler(srvs.TaskResolutionService, app.SDEditor, srvs.TaskService, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo)
	app.AgentHandler = handlers.NewAgentHandler(srvs.AgentService, app.Config.AgentAPIKey)
	app.ServerActionsHandler = handlers.NewServerActionsHandler(srvs.ServerActionsService)
	app.AuthHandler = handlers.NewAuthHandler(srvs.AuthService)
	app.ContractHandler = handlers.NewContractHandler(srvs.ContractService)
	app.UserHandler = handlers.NewUserHandler(repos.UserRepo, repos.BitrixRepo)
	app.DebugHandler = handlers.NewDebugHandler(app.EventBus)
	app.SSEHandler = handlers.NewSSEHandler(app.EventBus)
	app.TicketHandler = handlers.NewTicketHandler(srvs.TicketService, app.EventBus)
	app.BitrixHandler = handlers.NewBitrixHandler(srvs.BitrixSyncService)
	app.BitrixWebhookHandler = handlers.NewBitrixWebhookHandler(srvs.BitrixIncomingService)
	app.CandidateHandler = handlers.NewCandidateHandler(repos.CandidateRepo, srvs.AgentObservation, srvs.CompanyService)
	app.NetworkCandidateHandler = handlers.NewNetworkCandidateHandler(srvs.NetworkCandidateService)
	app.OwnerHistoryHandler = handlers.NewOwnerHistoryHandler(repos.OwnerHistoryRepo)
	app.AgentObservationFeed = handlers.NewAgentObservationFeedHandler(app.DB)
}

func setupIntegrationModules(app *Application, srvs Services) {
	app.BitrixModule = newBitrixModule(
		app.Config,
		app.Logger.With("component", "bitrix_module"),
		app.EventBus,
		srvs.BitrixSyncService,
		srvs.BitrixIncomingService,
		app.BitrixHandler,
		app.BitrixWebhookHandler,
	)
	app.BitrixModule.registerEventHandlers()
}

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
	r.Use(chi_middleware.RequestID)
	r.Use(middleware.LoggerInjector(a.Logger))
	r.Use(chi_middleware.RealIP, chi_middleware.Logger, chi_middleware.Recoverer)
	r.Use(chi_middleware.Timeout(60 * time.Second))

	// Без Auth Middleware, авторизация внутри хендлера
	r.Post("/api/submit_json", a.AgentHandler.HandleSubmitJSON)

	r.Route("/api/auth", func(r chi.Router) {
		a.AuthHandler.RegisterRoutes(r)
	})
	if a.BitrixModule != nil {
		a.BitrixModule.registerPublicRoutes(r)
	}

	r.Route("/api/agents", func(r chi.Router) {
		// r.Use(middleware.AgentAuthMiddleware(a.Config.AgentAPIKey))
		a.AgentHandler.RegisterRoutes(r)
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.JwtAuthMiddleware(a.Config))

		r.Route("/companies", func(r chi.Router) {
			r.Get("/", a.CompanyHandler.Search)
			if a.BitrixModule != nil {
				a.BitrixModule.registerCompanyRoutes(r, a.CompanyHandler)
			}
			r.Get("/{id}", a.CompanyHandler.Get)
			r.Get("/{id}/infrastructure", a.CompanyHandler.GetInfrastructure)
			r.Get("/{id}/children", a.CompanyHandler.GetChildren)

			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/", a.CompanyHandler.Create)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Put("/{id}", a.CompanyHandler.Update)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Delete("/{id}", a.CompanyHandler.Delete)
		})

		r.Route("/servers", func(r chi.Router) {
			r.Get("/", a.ServerHandler.List)
			r.Get("/{id}", a.ServerHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/", a.ServerHandler.Create)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Put("/{id}", a.ServerHandler.Update)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Delete("/{id}", a.ServerHandler.Delete)
		})

		r.Route("/workstations", func(r chi.Router) {
			r.Get("/", a.WorkstationHandler.List)
			r.Get("/{id}", a.WorkstationHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/", a.WorkstationHandler.Create)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Put("/{id}", a.WorkstationHandler.Update)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Delete("/{id}", a.WorkstationHandler.Delete)
		})

		r.Route("/fiscals", func(r chi.Router) {
			r.Get("/", a.FiscalHandler.List)
			r.Get("/{id}", a.FiscalHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/", a.FiscalHandler.Create)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Put("/{id}", a.FiscalHandler.Update)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Delete("/{id}", a.FiscalHandler.Delete)
		})

		r.Route("/contracts", func(r chi.Router) {
			r.Get("/{id}", a.ContractHandler.GetContract)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/", a.ContractHandler.CreateContract)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Put("/{id}", a.ContractHandler.UpdateContract)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Delete("/{id}", a.ContractHandler.DeleteContract)
		})

		r.Route("/candidates", func(r chi.Router) {
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/", a.CandidateHandler.List)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/{id}", a.CandidateHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/{id}/observations", a.CandidateHandler.GetObservations)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/approve", a.CandidateHandler.Approve)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/approve-manual", a.CandidateHandler.ApproveManual)
		})

		r.Route("/network-candidates", func(r chi.Router) {
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/", a.NetworkCandidateHandler.List)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/{id}", a.NetworkCandidateHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/approve", a.NetworkCandidateHandler.Approve)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/groups/{groupID}/remove", a.NetworkCandidateHandler.RemoveGroup)
		})

		a.OwnerHistoryHandler.RegisterRoutes(r)
		a.AgentObservationFeed.RegisterRoutes(r)

		a.SearchHandler.RegisterRoutes(r)
		a.TaskHandler.RegisterRoutes(r)
		a.ServerActionsHandler.RegisterRoutes(r)
		a.SSEHandler.RegisterRoutes(r)

		r.Route("/tickets", func(r chi.Router) {
			a.TicketHandler.RegisterRoutes(r)
		})

		if a.BitrixModule != nil {
			a.BitrixModule.registerProtectedRoutes(r)
		}

		r.Route("/profile", func(r chi.Router) {
			r.Get("/assignees", a.UserHandler.ListAssignees)
			r.Get("/me", a.UserHandler.GetMyProfile)
			r.Patch("/credentials", a.UserHandler.UpdateMyCredentials)
			r.Patch("/integrations", a.UserHandler.UpdateMyIntegrations)
			if a.BitrixModule != nil {
				a.BitrixModule.registerProfileRoutes(r, a.UserHandler)
			}
			r.Get("/config", a.UserHandler.GetMyProfileConfig)
			r.Patch("/config", a.UserHandler.UpdateMyProfileConfig)
		})

		r.Route("/users", func(r chi.Router) {
			r.Use(middleware.AdminRequiredMiddleware)
			a.UserHandler.RegisterRoutes(r)
		})
	})

	r.Route("/sync", func(r chi.Router) {
		a.SyncHandler.RegisterRoutes(r)
	})
	r.Route("/debug", func(r chi.Router) {
		a.DebugHandler.RegisterRoutes(r)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Welcome to XenionDesk"))
	})

	// Р оут для Swagger документации
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	fileServer(r, "/static/tickets", http.Dir(a.Config.TicketStoragePath))
	fileServer(r, "/api/static/tickets", http.Dir(a.Config.TicketStoragePath))

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
		a.Logger.Info("Шлюз поиска дубликатов отключен.")
	}

	if a.Config.EnableFRDiscrepancyFinder {
		wg.Add(1)
		go func() { defer wg.Done(); a.FRUpdateFounder.Start(ctx) }()
	} else {
		a.Logger.Info("Воркер поиска обновлений для Р¤Р  отключен.")
	}

	if a.Config.EnableAgentFTPGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.AgentFTPGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Шлюз агентов (FTP) отключен.")
	}

	if a.Config.EnablePollingGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.PollingGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Опрос статусов RMS-серверов отключен.")
	}

	if a.Config.EnableSDeskGateway {
		wg.Add(1)
		go func() { defer wg.Done(); a.SDeskGateway.Start(ctx) }()
		wg.Add(1)
		go func() { defer wg.Done(); a.TicketGateway.Start(ctx) }()
	} else {
		a.Logger.Info("Синхронизация сущностей с ServiceDesk отключена.")
	}
	if a.Config.EnableContractGateway && a.ContractGateway != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.ContractGateway.Start(ctx) }()
	}
	if a.Config.EnableStatusWorker {
		wg.Add(1)
		go func() { defer wg.Done(); a.StatusActualityWorker.Start(ctx) }()
	} else {
		a.Logger.Info("Проверка актуальности статусов отключена.")
	}
	if a.BitrixModule != nil {
		a.BitrixModule.start(ctx, wg)
	}
}

func fileServer(r chi.Router, path string, root http.FileSystem) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit any URL parameters")
	}

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", http.StatusPermanentRedirect).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.RouteContext(r.Context())
		pathPrefix := strings.TrimSuffix(rctx.RoutePattern(), "/*")
		fs := http.StripPrefix(pathPrefix, http.FileServer(root))
		fs.ServeHTTP(w, r)
	})
}
