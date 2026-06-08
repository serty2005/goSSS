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
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/task"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/adapterstore"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/db"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/iiko"
	"etalon-server/internal/infra/logger"
	bitrixplugin "etalon-server/internal/infra/plugins/bitrix"
	megafonvatsplugin "etalon-server/internal/infra/plugins/megafonvats"
	"etalon-server/internal/infra/plugins/naumen"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/pkg/seeder"
	"etalon-server/internal/services"
	agentauthsvc "etalon-server/internal/services/agentauth"
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
	DeferredTicketWorker  workers.DeferredTicketWorker
	TicketGateway         gateways.TicketGateway
	ContractGateway       gateways.ContractGateway
	BitrixModule          *bitrixModule
	PyrusModule           *pyrusModule
	TelephonyModule       *telephonyModule

	// Handlers
	CompanyHandler            *handlers.CompanyHandler
	SearchHandler             *handlers.SearchHandler
	SyncHandler               *handlers.SyncHandler
	TaskHandler               *handlers.TaskHandler
	AgentHandler              *handlers.AgentHandler
	ServerActionsHandler      *handlers.ServerActionsHandler
	AuthHandler               *handlers.AuthHandler
	ContractHandler           *handlers.ContractHandler
	UserHandler               *handlers.UserHandler
	DebugHandler              *handlers.DebugHandler
	SSEHandler                *handlers.SSEHandler
	TicketHandler             *handlers.TicketHandler
	ServerHandler             *handlers.ServerHandler
	WorkstationHandler        *handlers.WSHandler
	FiscalHandler             *handlers.FiscalHandler
	BitrixHandler             *handlers.BitrixHandler
	PyrusHandler              *handlers.PyrusHandler
	MegafonVATSHandler        *handlers.MegafonVATSHandler
	TelephonyHandler          *handlers.TelephonyHandler
	BitrixWebhookHandler      *handlers.BitrixWebhookHandler
	PyrusWebhookHandler       *handlers.PyrusWebhookHandler
	MegafonVATSWebhookHandler *handlers.MegafonVATSWebhookHandler
	CandidateHandler          *handlers.CandidateHandler
	NetworkCandidateHandler   *handlers.NetworkCandidateHandler
	OwnerHistoryHandler       *handlers.OwnerHistoryHandler
	AgentObservationFeed      *handlers.AgentObservationFeedHandler
	AgentDiagnosticsHandler   *handlers.AgentDiagnosticsHandler
	ReportHandler             *handlers.ReportHandler
	EntityDeletionHandler     *handlers.EntityDeletionHandler
	IntegrationSyncHandler    *handlers.IntegrationSyncHandler
	MaterialHandler           *handlers.MaterialHandler
	ArticleHandler            *handlers.ArticleHandler
	TranslationsHandler       *handlers.TranslationsHandler

	AgentAdapterCatalogSync services.AgentAdapterCatalogSyncService
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
	clients, err := setupExternalClients(app.Config, app.Logger, app.DB, repos.LinkRepo)
	if err != nil {
		return nil, err
	}

	app.Seeder = seeder.NewSeeder(app.Logger, app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.ContractRepo)

	services := setupServices(app, repos, clients)
	app.AgentAdapterCatalogSync = services.AgentAdapterCatalogSync
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
// Используется при запуске с флагом --seed-ftp-cache для обработки ранее скачанных файлов
// без обращения к FTP-серверу.
func (a *Application) SeedFromFTPCacheAndExit() {
	a.Logger.Info("Запуск в режиме инициализации из FTP-кэша...")
	ctx := context.Background()

	// 1. Инициализируем записи в БД из существующих файлов кэша
	if err := a.AgentFTPGateway.InitializeDBFromCache(ctx); err != nil {
		a.Logger.Warn("Ошибка инициализации БД из кэша", "error", err)
	}

	// 2. Загружаем данные агентов из кэша
	processedCount, err := a.AgentFTPGateway.LoadAgentDataFromCache(ctx)
	if err != nil {
		a.Logger.Warn("Ошибка загрузки данных из кэша", "error", err)
	}

	a.Logger.Info("Инициализация из FTP-кэша завершена", "processed_files", processedCount)
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
	if err := db.Migrate(cfg, database); err != nil {
		log.Fatal("Не удалось выполнить миграцию схемы БД", "error", err)
	}
	log.Info("Миграции базы данных успешно завершены.")

	if err := services.EnsureFiscalSerialUniqueness(context.Background(), database); err != nil {
		log.Fatal("Не удалось выполнить нормализацию и дедупликацию ФР", "error", err)
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
	PyrusRepo            pyrus.Repository
	TelephonyRepo        telephony.Repository
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
		PyrusRepo:            infraRepos.NewPyrusRepo(db),
		TelephonyRepo:        infraRepos.NewTelephonyRepo(db),
		NetworkCandidateRepo: infraRepos.NewNetworkCandidateRepo(db),
		OwnerHistoryRepo:     infraRepos.NewOwnerHistoryRepo(db),
	}
}

type ExternalClients struct {
	SDClient                external.ExternalSystemClient
	FTPClient               services.FTPClient
	IikoClient              iiko.IikoClient
	BitrixClient            *bitrixplugin.Client
	PyrusClient             *pyrusplugin.Client
	MegafonVATSClient       *megafonvatsplugin.Client
	ContractMailbox         contractSvc.ContractMailboxClient
	RedisClient             *redis.Client
	AgentAdapterStore       services.AgentAdapterObjectStore
	TelephonyRecordingStore services.TelephonyRecordingObjectStore
}

func setupExternalClients(cfg *config.Config, log logger.LoggerInterface, db *gorm.DB, linkRepo repositories.LinkRepo) (ExternalClients, error) {
	var redisClient *redis.Client
	if strings.TrimSpace(cfg.RedisAddr) != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
	}
	agentAdapterStore, err := adapterstore.NewAgentAdapterObjectStore(context.Background(), cfg)
	if err != nil {
		return ExternalClients{}, err
	}
	telephonyRecordingStore, err := adapterstore.NewTelephonyRecordingObjectStore(context.Background(), cfg)
	if err != nil {
		return ExternalClients{}, err
	}
	return ExternalClients{
		SDClient:                naumen.NewNaumenClient(cfg, log.With("component", "naumen_client"), db, linkRepo),
		FTPClient:               services.NewFTPClient(cfg, log.With("component", "ftp_client")),
		IikoClient:              iiko.NewIikoClient(cfg.RequestTimeout, log.With("component", "iiko_client")),
		BitrixClient:            bitrixplugin.NewClient(cfg, log.With("component", "bitrix_client")),
		PyrusClient:             pyrusplugin.NewClient(cfg, log.With("component", "pyrus_client")),
		MegafonVATSClient:       megafonvatsplugin.NewClient(cfg, log.With("component", "megafon_vats_client")),
		ContractMailbox:         contractSvc.NewContractMailboxClient(cfg, log.With("component", "contract_mailbox_client")),
		RedisClient:             redisClient,
		AgentAdapterStore:       agentAdapterStore,
		TelephonyRecordingStore: telephonyRecordingStore,
	}, nil
}

type Services struct {
	AuthService                services.AuthService
	AgentService               services.AgentService
	AgentObservation           services.AgentObservationService
	AgentOperatorFlow          services.AgentOperatorFlowService
	TaskResolutionService      services.TaskResolutionService
	TaskService                task.Service
	ServerActionsService       services.ServerActionsService
	EntityMatcherService       services.EntityMatcherService
	TicketService              services.TicketService
	CompanyService             company.Service
	ContractService            contract.Service
	ServerService              server.Service
	WorkstationService         workstation.Service
	FiscalService              fiscal.Service
	BitrixSyncService          services.BitrixSyncService
	BitrixIncomingService      services.BitrixIncomingService
	PyrusSyncService           services.PyrusSyncService
	PyrusIncomingService       services.PyrusIncomingService
	MegafonVATSSyncService     services.MegafonVATSSyncService
	MegafonVATSIncomingService services.MegafonVATSIncomingService
	TelephonyService           services.TelephonyService
	IntegrationSyncControl     services.IntegrationSyncControlService
	NetworkCandidateService    services.NetworkCandidateService
	EntityDeletionService      services.EntityDeletionService
	AgentAdapterCatalogSync    services.AgentAdapterCatalogSyncService
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
	agentOperatorFlow := services.NewAgentOperatorFlowService(app.DB, app.Config.AgentAdapterCatalog.DefaultChannel)

	agentAdapterCatalogSync := services.NewAgentAdapterCatalogSyncService(
		app.DB,
		app.Logger.With("component", "agent_adapter_catalog_sync"),
		clients.AgentAdapterStore,
		app.Config,
	)
	megafonVATSRecordingService := services.NewMegafonVATSRecordingService(
		app.Config,
		app.Logger.With("component", "megafon_vats_recording_service"),
		repos.TelephonyRepo,
		clients.TelephonyRecordingStore,
	)
	contractService := contractSvc.NewService(
		app.Logger.With("component", "contract_service"),
		transactor,
		repos.ContractRepo,
		repos.CompanyRepo,
		repos.LinkRepo,
		repos.ServerRepo,
		repos.WorkstationRepo,
		repos.FRRepo,
	)
	companyService := companySvc.NewService(
		app.Logger.With("component", "company_service"),
		app.Config,
		transactor,
		repos.CompanyRepo,
		repos.ServerRepo,
		repos.WorkstationRepo,
		repos.FRRepo,
		repos.LinkRepo,
		repos.BitrixRepo,
		contractService,
	)

	ticketService := services.NewTicketService(
		app.Logger.With("component", "ticket_service"),
		repos.TicketRepo,
		repos.UserRepo,
		repos.CompanyRepo,
		repos.ContractRepo,
		clients.SDClient,
		app.Config,
		repos.ServerRepo,
		repos.WorkstationRepo,
		repos.FRRepo,
		repos.BitrixRepo,
		repos.PyrusRepo,
		repos.TelephonyRepo,
		repos.OwnerHistoryRepo,
		contractService,
	)
	pyrusSyncService := services.NewPyrusSyncService(
		app.Config,
		app.Logger.With("component", "pyrus_sync_service"),
		clients.PyrusClient,
		clients.RedisClient,
		repos.TicketRepo,
		repos.UserRepo,
		repos.PyrusRepo,
	)
	pyrusIncomingService := services.NewPyrusIncomingService(
		app.Config,
		app.Logger.With("component", "pyrus_incoming_service"),
		clients.PyrusClient,
		clients.RedisClient,
		repos.TicketRepo,
		ticketService,
		repos.UserRepo,
		repos.ServerRepo,
		repos.PyrusRepo,
		app.EventBus,
	)
	megafonVATSIncomingService := services.NewMegafonVATSIncomingService(
		app.Config,
		app.Logger.With("component", "megafon_vats_incoming_service"),
		clients.RedisClient,
		repos.TelephonyRepo,
		repos.UserRepo,
		repos.TicketRepo,
		app.EventBus,
		megafonVATSRecordingService,
	)
	megafonVATSSyncService := services.NewMegafonVATSSyncService(
		app.Config,
		app.Logger.With("component", "megafon_vats_sync_service"),
		clients.MegafonVATSClient,
		repos.TelephonyRepo,
		repos.TicketRepo,
		repos.UserRepo,
		app.EventBus,
		megafonVATSRecordingService,
	)
	bitrixSyncService := services.NewBitrixSyncService(
		app.Config,
		app.Logger.With("component", "bitrix_sync_service"),
		clients.BitrixClient,
		clients.RedisClient,
		repos.TicketRepo,
		repos.ServerRepo,
		repos.WorkstationRepo,
		repos.UserRepo,
		repos.BitrixRepo,
		repos.CompanyRepo,
		repos.TelephonyRepo,
	)
	telephonyService := services.NewTelephonyService(
		app.Logger.With("component", "telephony_service"),
		repos.TelephonyRepo,
		repos.TicketRepo,
		repos.CompanyRepo,
		repos.UserRepo,
		app.EventBus,
		megafonVATSSyncService,
		bitrixSyncService,
	)
	integrationSyncControl := services.NewIntegrationSyncControlService(
		repos.PyrusRepo,
		pyrusIncomingService,
		repos.TelephonyRepo,
		megafonVATSIncomingService,
	)

	return Services{
		AuthService:                services.NewAuthService(app.Config, repos.UserRepo, app.Logger.With("component", "auth_service")),
		AgentObservation:           obsService,
		AgentService:               services.NewAgentService(app.Logger.With("component", "agent_service"), repos.AgentRepo, repos.CompanyRepo, app.EventBus, agentOperatorFlow),
		AgentOperatorFlow:          agentOperatorFlow,
		TaskResolutionService:      services.NewTaskResolutionService(app.Logger.With("component", "task_resolution"), transactor, app.EventBus, repos.TaskRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
		TaskService:                taskSvc.NewService(app.Logger.With("component", "task_service"), repos.TaskRepo),
		ServerActionsService:       services.NewServerActionsService(app.Config, app.Logger.With("component", "server_actions"), app.EventBus, repos.ServerRepo, repos.CompanyRepo, repos.OwnerHistoryRepo, clients.IikoClient),
		EntityMatcherService:       services.NewEntityMatcherService(app.Logger.With("component", "entity_matcher"), repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo),
		TicketService:              ticketService,
		CompanyService:             companyService,
		ContractService:            contractService,
		ServerService:              serverSvc.NewService(app.Logger.With("component", "server_service"), transactor, repos.ServerRepo, repos.OwnerHistoryRepo),
		WorkstationService:         workstationSvc.NewService(app.Logger.With("component", "workstation_service"), transactor, repos.WorkstationRepo, repos.OwnerHistoryRepo),
		FiscalService:              fiscalSvc.NewService(app.Logger.With("component", "fiscal_service"), transactor, repos.FRRepo, repos.OwnerHistoryRepo),
		BitrixSyncService:          bitrixSyncService,
		BitrixIncomingService:      services.NewBitrixIncomingService(app.Config, app.Logger.With("component", "bitrix_incoming_service"), clients.BitrixClient, clients.RedisClient, repos.TicketRepo, repos.UserRepo, repos.BitrixRepo, app.EventBus),
		PyrusSyncService:           pyrusSyncService,
		PyrusIncomingService:       pyrusIncomingService,
		MegafonVATSSyncService:     megafonVATSSyncService,
		MegafonVATSIncomingService: megafonVATSIncomingService,
		TelephonyService:           telephonyService,
		IntegrationSyncControl:     integrationSyncControl,
		NetworkCandidateService:    services.NewNetworkCandidateService(repos.NetworkCandidateRepo),
		EntityDeletionService:      services.NewEntityDeletionService(app.Logger.With("component", "entity_deletion_service"), app.DB, transactor, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.CompanyRepo, repos.ContractRepo, repos.OwnerHistoryRepo),
		AgentAdapterCatalogSync:    agentAdapterCatalogSync,
	}
}

func setupBackgroundServices(app *Application, repos Repositories, clients ExternalClients, srvs Services) {
	// --- 1. Инициализация Менеджера Интеграций ---
	app.IntegrationManager = integrations.NewManager(app.Logger.With("component", "integration_manager"))

	// --- 2. Настройка Адаптера Naumen ---
	mapperCtx := &external.MapperContext{
		DB:       app.DB,
		LinkRepo: repos.LinkRepo,
		Logger:   app.Logger,
	}
	// Используем существующий клиент, оборачивая его в адаптер
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

	app.DuplicatesGateway = gateways.NewDuplicatesGateway(app.Config, app.DB, app.EventBus, app.Logger.With("component", "duplicates_gateway"), srvs.EntityDeletionService)
	app.PollingGateway = gateways.NewServerPollingGateway(app.Config, app.Logger.With("component", "iiko_polling_gateway"), repos.ServerRepo, clients.IikoClient, app.EventBus)
	app.AgentFTPGateway = gateways.NewAgentFTPGateway(app.Config, app.Logger.With("component", "agent_ftp_gateway"), app.DB, clients.FTPClient, srvs.AgentObservation)
	app.FRUpdateFounder = workers.NewFRUpdateFounder(app.Config, app.Logger.With("component", "fr_update_founder"), app.EventBus, repos.FRRepo, repos.LinkRepo, app.IntegrationManager)
	app.SDEditor = workers.NewSDEditorWorker(app.Logger.With("component", "sdesk_editor_worker"), app.DB, app.EventBus, app.IntegrationManager, repos.TaskRepo, repos.LinkRepo, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
	app.StatusActualityWorker = workers.NewStatusActualityWorker(app.Config, app.Logger.With("component", "status_actuality_worker"), app.DB, repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo)
	app.DeferredTicketWorker = workers.NewDeferredTicketWorker(app.Logger.With("component", "deferred_ticket_worker"), srvs.TicketService, app.EventBus, time.Minute)
	app.TicketGateway = gateways.NewTicketGateway(app.Config, app.Logger.With("component", "ticket_gateway"), app.IntegrationManager, repos.TicketRepo, app.EventBus, app.DB, repos.LinkRepo)
	app.ContractGateway = gateways.NewContractGateway(
		app.Config,
		app.Logger.With("component", "contract_gateway"),
		clients.ContractMailbox,
		srvs.BitrixSyncService,
		repos.BitrixRepo,
		srvs.ContractService,
		repos.ContractRepo,
	)
}

func setupHandlers(app *Application, repos Repositories, srvs Services) {
	app.ServerHandler = handlers.NewServerHandler(srvs.ServerService, srvs.EntityDeletionService)
	app.WorkstationHandler = handlers.NewWSHandler(srvs.WorkstationService, srvs.EntityDeletionService)
	app.FiscalHandler = handlers.NewFiscalHandler(srvs.FiscalService, srvs.EntityDeletionService)
	app.CompanyHandler = handlers.NewCompanyHandler(srvs.CompanyService)
	app.SearchHandler = handlers.NewSearchHandler(repos.CompanyRepo, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo, repos.TicketRepo)
	app.SyncHandler = handlers.NewSyncHandler(app.Seeder, app.Config.SeederKey)
	app.TaskHandler = handlers.NewTaskHandler(srvs.TaskResolutionService, app.SDEditor, srvs.TaskService, repos.ServerRepo, repos.WorkstationRepo, repos.FRRepo, repos.LinkRepo)
	agentAuthService := agentauthsvc.NewService(app.DB, app.Logger.With("component", "agent_auth_service"), repos.AgentRepo, srvs.AgentService)
	app.AgentHandler = handlers.NewAgentHandler(srvs.AgentService, agentAuthService, app.Config.AgentAPIKey)
	app.ServerActionsHandler = handlers.NewServerActionsHandler(srvs.ServerActionsService)
	app.AuthHandler = handlers.NewAuthHandler(srvs.AuthService)
	app.ContractHandler = handlers.NewContractHandler(srvs.ContractService)
	app.UserHandler = handlers.NewUserHandler(repos.UserRepo, repos.BitrixRepo, repos.PyrusRepo, repos.TelephonyRepo, srvs.PyrusSyncService, app.Config)
	app.DebugHandler = handlers.NewDebugHandler(app.EventBus)
	app.SSEHandler = handlers.NewSSEHandler(app.EventBus)
	app.TicketHandler = handlers.NewTicketHandler(srvs.TicketService, app.EventBus, repos.PyrusRepo)
	app.BitrixHandler = handlers.NewBitrixHandler(srvs.BitrixSyncService, repos.ContractRepo, app.ContractGateway, repos.UserRepo, app.Config)
	app.PyrusHandler = handlers.NewPyrusHandler(srvs.PyrusSyncService)
	app.MegafonVATSHandler = handlers.NewMegafonVATSHandler(srvs.MegafonVATSSyncService)
	app.TelephonyHandler = handlers.NewTelephonyHandler(srvs.TelephonyService)
	app.BitrixWebhookHandler = handlers.NewBitrixWebhookHandler(srvs.BitrixIncomingService)
	app.PyrusWebhookHandler = handlers.NewPyrusWebhookHandler(srvs.PyrusIncomingService)
	app.MegafonVATSWebhookHandler = handlers.NewMegafonVATSWebhookHandler(srvs.MegafonVATSIncomingService)
	app.CandidateHandler = handlers.NewCandidateHandler(repos.CandidateRepo, srvs.AgentObservation, srvs.CompanyService)
	app.NetworkCandidateHandler = handlers.NewNetworkCandidateHandler(srvs.NetworkCandidateService)
	app.OwnerHistoryHandler = handlers.NewOwnerHistoryHandler(repos.OwnerHistoryRepo)
	app.AgentObservationFeed = handlers.NewAgentObservationFeedHandler(app.DB)
	app.AgentDiagnosticsHandler = handlers.NewAgentDiagnosticsHandler(app.DB, srvs.AgentOperatorFlow, srvs.AgentAdapterCatalogSync)
	app.ReportHandler = handlers.NewReportHandler(app.DB)
	app.EntityDeletionHandler = handlers.NewEntityDeletionHandler(srvs.EntityDeletionService)
	app.IntegrationSyncHandler = handlers.NewIntegrationSyncHandler(srvs.IntegrationSyncControl)
	app.MaterialHandler = handlers.NewMaterialHandler(app.DB, repos.UserRepo)
	app.ArticleHandler = handlers.NewArticleHandler(app.DB, repos.UserRepo, app.Config.ArticleWebhookKey)
	app.TranslationsHandler = handlers.NewTranslationsHandler(app.DB)
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

	app.PyrusModule = newPyrusModule(
		app.Config,
		app.Logger.With("component", "pyrus_module"),
		app.EventBus,
		srvs.PyrusSyncService,
		srvs.PyrusIncomingService,
		app.PyrusHandler,
		app.PyrusWebhookHandler,
	)
	app.PyrusModule.registerEventHandlers()

	app.TelephonyModule = newTelephonyModule(
		app.Config,
		app.Logger.With("component", "telephony_module"),
		srvs.MegafonVATSSyncService,
		srvs.MegafonVATSIncomingService,
		app.MegafonVATSHandler,
		app.TelephonyHandler,
		app.MegafonVATSWebhookHandler,
	)
}

func (a *Application) setupRouter() *chi.Mux {
	r := chi.NewRouter()

	corsMiddleware := cors.New(cors.Options{
		AllowedOrigins:   a.Config.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-API-Key"},
		AllowCredentials: true,
		MaxAge:           300,
	})
	r.Use(corsMiddleware.Handler)
	r.Use(chi_middleware.RequestID)
	r.Use(chi_middleware.RealIP)
	r.Use(middleware.LoggerInjector(a.Logger))
	r.Use(middleware.RequestLoggingMiddleware())
	r.Use(middleware.Recoverer())
	r.Use(middleware.TimeoutUnless(60*time.Second, func(r *http.Request) bool {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/servers/") && strings.HasSuffix(r.URL.Path, "/license") {
			return true
		}
		return r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/events")
	}))

	// Без Auth Middleware, авторизация внутри хендлера
	r.Post("/api/submit_json", a.AgentHandler.HandleSubmitJSON)
	r.Post("/api/articles/webhook", a.ArticleHandler.CreateFromWebhook)

	r.Route("/api/auth", func(r chi.Router) {
		a.AuthHandler.RegisterRoutes(r)
	})
	if a.BitrixModule != nil {
		a.BitrixModule.registerPublicRoutes(r)
	}
	if a.PyrusModule != nil {
		a.PyrusModule.registerPublicRoutes(r)
	}
	if a.TelephonyModule != nil {
		a.TelephonyModule.registerPublicRoutes(r)
	}

	r.Route("/api/agents", func(r chi.Router) {
		// r.Use(middleware.AgentAuthMiddleware(a.Config.AgentAPIKey))
		a.AgentHandler.RegisterRoutes(r)
	})

	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.JwtAuthMiddleware(a.Config))

		r.Route("/companies", func(r chi.Router) {
			r.Get("/", a.CompanyHandler.Search)
			r.Get("/parents", a.CompanyHandler.ListParents)
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
			r.Get("/filter-options", a.FiscalHandler.FilterOptions)
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

		r.Route("/materials", func(r chi.Router) {
			r.Get("/", a.MaterialHandler.List)
			r.Get("/{id}", a.MaterialHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/", a.MaterialHandler.Create)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Put("/{id}", a.MaterialHandler.Update)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Delete("/{id}", a.MaterialHandler.Delete)
		})
		r.Route("/articles", func(r chi.Router) {
			r.Get("/", a.ArticleHandler.List)
			r.Get("/featured", a.ArticleHandler.Featured)
			r.Get("/{id}", a.ArticleHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/", a.ArticleHandler.Create)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Put("/{id}", a.ArticleHandler.Update)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Delete("/{id}", a.ArticleHandler.Delete)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Patch("/{id}/publish", a.ArticleHandler.Publish)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Patch("/{id}/archive", a.ArticleHandler.Archive)
		})

		r.Route("/candidates", func(r chi.Router) {
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/", a.CandidateHandler.List)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/recalculate", a.CandidateHandler.Recalculate)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/{id}", a.CandidateHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/{id}/observations", a.CandidateHandler.GetObservations)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/approve", a.CandidateHandler.Approve)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/reject", a.CandidateHandler.Reject)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/approve-manual", a.CandidateHandler.ApproveManual)
		})

		r.Route("/network-candidates", func(r chi.Router) {
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/", a.NetworkCandidateHandler.List)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/{id}", a.NetworkCandidateHandler.Get)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/approve", a.NetworkCandidateHandler.Approve)
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Post("/{id}/groups/{groupID}/remove", a.NetworkCandidateHandler.RemoveGroup)
		})

		r.Route("/deletion-candidates", func(r chi.Router) {
			r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/by-entity", a.EntityDeletionHandler.GetByEntity)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/", a.EntityDeletionHandler.List)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/", a.EntityDeletionHandler.RequestDeletion)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/{id}", a.EntityDeletionHandler.GetDetails)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/{id}/replay", a.EntityDeletionHandler.ReplayChoice)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/{id}/confirm", a.EntityDeletionHandler.ConfirmDeletion)
		})

		a.OwnerHistoryHandler.RegisterRoutes(r)
		a.AgentObservationFeed.RegisterRoutes(r)
		r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Group(func(r chi.Router) {
			a.AgentDiagnosticsHandler.RegisterRoutes(r)
			a.ReportHandler.RegisterRoutes(r)
		})

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
		if a.PyrusModule != nil {
			a.PyrusModule.registerProtectedRoutes(r)
		}
		if a.TelephonyModule != nil {
			a.TelephonyModule.registerProtectedRoutes(r)
		}
		if a.IntegrationSyncHandler != nil {
			r.Route("/integrations", func(r chi.Router) {
				r.Use(middleware.RequireAnyRole(user.RoleAdmin))
				a.IntegrationSyncHandler.RegisterRoutes(r)
			})
		}

		r.Route("/profile", func(r chi.Router) {
			r.Get("/assignees", a.UserHandler.ListAssignees)
			r.Get("/me", a.UserHandler.GetMyProfile)
			r.Patch("/credentials", a.UserHandler.UpdateMyCredentials)
			r.Patch("/integrations", a.UserHandler.UpdateMyIntegrations)
			if a.BitrixModule != nil {
				a.BitrixModule.registerProfileRoutes(r, a.UserHandler)
			}
			if a.PyrusModule != nil {
				a.PyrusModule.registerProfileRoutes(r, a.UserHandler)
			}
			r.Get("/config", a.UserHandler.GetMyProfileConfig)
			r.Patch("/config", a.UserHandler.UpdateMyProfileConfig)
		})

		r.Route("/translations", func(r chi.Router) {
			r.Get("/", a.TranslationsHandler.GetCatalog)
			r.With(middleware.RequireAnyRole(user.RoleAdmin)).Patch("/", a.TranslationsHandler.UpdateCatalog)
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

	// Роут для Swagger документации
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
		a.Logger.Info("Воркер поиска обновлений для ФР отключен.")
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
	if a.DeferredTicketWorker != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.DeferredTicketWorker.Start(ctx) }()
	}
	if a.AgentAdapterCatalogSync != nil {
		wg.Add(1)
		go func() { defer wg.Done(); a.AgentAdapterCatalogSync.Start(ctx) }()
	}
	if a.BitrixModule != nil {
		a.BitrixModule.start(ctx, wg)
	}
	if a.PyrusModule != nil {
		a.PyrusModule.start(ctx, wg)
	}
	if a.TelephonyModule != nil {
		a.TelephonyModule.start(ctx, wg)
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
