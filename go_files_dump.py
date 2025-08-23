cmd/etalon-server/main.go
===== START main.go =====
go `
package main

import (
	"etalon-server/internal/app"
	"flag"
	"log"
)

func main() {
	// Обработка флагов командной строки
	seedFlag := flag.Bool("seed", false, "Наполнить базу данных тестовыми данными из файлов и выйти.")
	flag.Parse()

	// Инициализация всего приложения
	application, err := app.New()
	if err != nil {
		log.Fatalf("Не удалось инициализировать приложение: %v", err)
	}

	// Если передан флаг --seed, запускаем наполнение и выходим
	if *seedFlag {
		application.SeedDBAndExit()
	}

	// Запуск сервера и фоновых сервисов в обычном режиме
	application.Run()
}

go `
===== END main.go =====

internal/api/dtos.go
===== START dtos.go =====
go `
package api

import (
	"encoding/json"
	"time"
)

// --- DTO для CRUD, Задач и других операций ---

// CompanyCreateDTO - DTO для создания/обновления компании.
type CompanyCreateDTO struct {
	Title                 *string `json:"title"`
	Address               *string `json:"address"`
	AdditionalName        *string `json:"additional_name"`
	ParentServiceDeskUUID *string `json:"parent_uuid"`
}

// ResolveTaskRequestDTO - тело запроса для решения задачи.
type ResolveTaskRequestDTO struct {
	Status  string `json:"status"`
	Comment string `json:"comment,omitempty"`
}

// DuplicateGroupDTO представляет группу дубликатов для ответа API.
type DuplicateGroupDTO struct {
	Field      string        `json:"field"`
	Value      string        `json:"value"`
	EntityType string        `json:"entity_type"`
	MainRecord interface{}   `json:"main_record"`
	Duplicates []interface{} `json:"duplicates"`
}

// ErrorResponseDTO стандартизированный ответ с ошибкой.
type ErrorResponseDTO struct {
	Error string `json:"error"`
}

// --- DTO для взаимодействия с Агентами ---

// AgentDataDTO определяет структуру данных, получаемых от агента.
type AgentDataDTO struct {
	ModelName       string `json:"modelName"`
	SerialNumber    string `json:"serialNumber"`
	RNM             string `json:"RNM"`
	INN             string `json:"INN"`
	FNSerial        string `json:"fn_serial"`
	DateTimeEnd     string `json:"dateTime_end"`
	FFDVersion      string `json:"ffdVersion"`
	Hostname        string `json:"hostname"`
	URLRms          string `json:"url_rms"`
	CRMID           string `json:"crmId"`
	TeamviewerID    string `json:"teamviewer_id"`
	AnydeskID       string `json:"anydesk_id"`
	LitemanagerID   string `json:"litemanager_id"`
	CurrentTime     string `json:"current_time"`
	AgentVersion    string `json:"agent_version"`
	InstalledDriver string `json:"installed_driver,omitempty"`

	AdditionalProperties map[string]interface{} `json:"-"`
}

// UnmarshalJSON для кастомной обработки JSON, чтобы собирать все неописанные поля.
func (a *AgentDataDTO) UnmarshalJSON(data []byte) error {
	type Alias AgentDataDTO
	alias := &struct{ *Alias }{Alias: (*Alias)(a)}
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Удаляем из мапы те поля, которые уже были распарсены в основные поля структуры.
	delete(raw, "modelName")
	delete(raw, "serialNumber")
	delete(raw, "RNM")
	delete(raw, "INN")
	delete(raw, "fn_serial")
	delete(raw, "dateTime_end")
	delete(raw, "ffdVersion")
	delete(raw, "hostname")
	delete(raw, "url_rms")
	delete(raw, "crmId")
	delete(raw, "teamviewer_id")
	delete(raw, "anydesk_id")
	delete(raw, "litemanager_id")
	delete(raw, "current_time")
	delete(raw, "agent_version")
	delete(raw, "installed_driver")

	a.AdditionalProperties = raw
	return nil
}

// RegistrationRequestDTO - тело запроса для регистрации нового агента.
type RegistrationRequestDTO struct {
	AgentUUID    string       `json:"agent_uuid"`
	Hostname     string       `json:"hostname"`
	AgentVersion string       `json:"agent_version"`
	InitialData  AgentDataDTO `json:"initial_data"`
}

// AgentConfigDTO - структура конфигурации, отправляемая агенту.
type AgentConfigDTO struct {
	EtalonServerURL string            `json:"etalon_server_url"`
	Mode            string            `json:"mode"`
	Intervals       IntervalsDTO      `json:"intervals"`
	Zabbix          ZabbixConfigDTO   `json:"zabbix"`
	Workstation     WorkstationCfgDTO `json:"workstation,omitempty"`
}

type IntervalsDTO struct {
	Heartbeat        int `json:"heartbeat_seconds"`
	ConfigCheck      int `json:"config_check_seconds"`
	WorkstationCheck int `json:"workstation_check_seconds"`
	UpdateCheck      int `json:"update_check_seconds"`
}

type ZabbixConfigDTO struct {
	ServerHost string `json:"server_host"`
	ServerPort int    `json:"server_port"`
	Hostname   string `json:"hostname"`
}

type WorkstationCfgDTO struct {
	PrimaryJSONPath   string `json:"primary_json_path"`
	CashServerLogPath string `json:"cash_server_log_path"`
}

// --- НОВЫЕ DTO для UI-ориентированного поиска ---

// FinalSearchResponseDTO - корневой объект для нового ответа поиска.
type FinalSearchResponseDTO struct {
	SearchResults []SearchGroupDTO `json:"search_results"`
}

// SearchGroupDTO представляет одну группу в результатах поиска (сущности, сгруппированные по владельцу).
type SearchGroupDTO struct {
	Owner         OwnerFullDTO     `json:"owner"`
	FoundEntities []FoundEntityDTO `json:"found_entities"`
}

// OwnerFullDTO содержит расширенную информацию о компании-владельце.
type OwnerFullDTO struct {
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	Address        *string `json:"address,omitempty"`
	ActiveContract *bool   `json:"active_contract,omitempty"`
	AdditionalInfo *string `json:"additional_info,omitempty"`
}

// FoundEntityDTO представляет одну найденную сущность внутри группы.
type FoundEntityDTO struct {
	EntityType string      `json:"entity_type"`
	Data       interface{} `json:"data"`
}

// --- DTO для данных внутри FoundEntityDTO ---

// ServerRichDTO содержит полный набор полей Сервера для UI.
type ServerRichDTO struct {
	UUID         string  `json:"uuid"`
	DeviceName   *string `json:"device_name,omitempty"`
	IP           *string `json:"ip,omitempty"`
	Status       string  `json:"status,omitempty"`
	Anydesk      *string `json:"anydesk,omitempty"`
	Teamviewer   *string `json:"teamviewer,omitempty"`
	RDP          *string `json:"rdp,omitempty"`
	Litemanager  *string `json:"litemanager,omitempty"`
	UniqueID     *string `json:"unique_id,omitempty"`
	PartnersLink *string `json:"partners_link,omitempty"`
}

// WorkstationRichDTO содержит полный набор полей Рабочей станции для UI.
type WorkstationRichDTO struct {
	UUID        string  `json:"uuid"`
	DeviceName  *string `json:"device_name,omitempty"`
	Status      *string `json:"status,omitempty"`
	Anydesk     *string `json:"anydesk,omitempty"`
	Teamviewer  *string `json:"teamviewer,omitempty"`
	Litemanager *string `json:"litemanager,omitempty"`
}

// FiscalRegisterRichDTO содержит полный набор полей Фискального регистратора для UI.
type FiscalRegisterRichDTO struct {
	UUID               string     `json:"uuid"`
	RNKKT              *string    `json:"rn_kkt,omitempty"`
	ModelKKT           *string    `json:"model_kkt,omitempty"`
	FNExpireDate       *time.Time `json:"fn_expire_date,omitempty"`
	FNRegistrationDate *time.Time `json:"fn_registration_date,omitempty"`
	DriverVersion      *string    `json:"driver_version,omitempty"`
	FirmwareVersion    *string    `json:"firmware_version,omitempty"`
}

go `
===== END dtos.go =====

internal/app/app.go
===== START app.go =====
go `
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

go `
===== END app.go =====

internal/config/config.go
===== START config.go =====
go `
// internal/config/config.go
package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config хранит всю конфигурацию приложения.
// Значения считываются из переменных окружения или из файла .env.
type Config struct {
	DatabaseURL        string
	ServiceDeskBaseURL string
	ServiceDeskKey     string
	ServerPort         string
	LogPath            string
	DisableFileLogging bool
	RateLimit          int
	WorkerCount        int
	MaxRetries         int
	RequestTimeout     time.Duration
	SeederKey          string
	AgentAPIKey        string

	// Новая секция для FTP
	FTPHost                string
	FTPUser                string
	FTPPassword            string
	FTPPort                string
	FTPPath                string
	FTPCachePath           string
	ReconcileInterval      time.Duration
	EnableReconcilerWorker bool

	// Секция для Zabbix
	ZabbixAPIURL   string
	ZabbixAPIToken string

	// Секция для Server Polling Worker
	RMSLogin                  string
	RMSPassword1              string
	RMSPassword2              string
	ServerPollingInterval     time.Duration
	ServerPollingBatchSize    int
	EnableServerPollingWorker bool

	// Секция настроек синхронизации с SD
	SDeskSyncInterval     time.Duration
	EnableSDeskSyncWorker bool

	//Список разрешенных CORS origins
	AllowedOrigins []string
}

// New загружает конфигурацию из файла .env и переменных окружения.
func New() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Читаем origins из .env как строку, разделенную запятыми
	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "http://localhost:5173")

	return &Config{
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/etalon_db?sslmode=disable"),
		ServiceDeskBaseURL: getEnv("BASE_URL", "https://servicedesk.example.com"),
		ServiceDeskKey:     getEnv("SDKEY", ""),
		ServerPort:         getEnv("PORT", "8080"),
		LogPath:            getEnv("LOG_PATH", "./logs/app.log"),
		DisableFileLogging: getEnvAsBool("DISABLE_FILE_LOGGING", false),
		RateLimit:          getEnvAsInt("RATE_LIMIT", 45),
		WorkerCount:        getEnvAsInt("WORKER_COUNT", 10),
		MaxRetries:         getEnvAsInt("MAX_RETRIES", 3),
		RequestTimeout:     time.Duration(getEnvAsInt("REQUEST_TIMEOUT_SEC", 15)) * time.Second,
		SeederKey:          getEnv("SEEDER_KEY", "super-secret-key-for-seeding"),
		AgentAPIKey:        getEnv("AGENT_API_KEY", ""),

		// Параметры Reconciler
		EnableReconcilerWorker: getEnvAsBool("ENABLE_RECONCILER_WORKER", true),
		FTPHost:                getEnv("FTP_HOST", "localhost"),
		FTPUser:                getEnv("FTP_USER", "user"),
		FTPPassword:            getEnv("FTP_PASSWORD", "password"),
		FTPPort:                getEnv("FTP_PORT", "21"),
		FTPPath:                getEnv("FTP_PATH", "/"),
		FTPCachePath:           getEnv("FTP_CACHE_PATH", "./ftp_cache"),
		ReconcileInterval:      time.Duration(getEnvAsInt("RECONCILE_INTERVAL_MIN", 60)) * time.Minute,

		// Параметры Zabbix
		ZabbixAPIURL:   getEnv("ZABBIX_API_URL", ""),
		ZabbixAPIToken: getEnv("ZABBIX_API_TOKEN", ""),

		// Параметры Server Polling Worker (ранее CRMid Worker)
		EnableServerPollingWorker: getEnvAsBool("ENABLE_SERVER_POLLING_WORKER", true), // ПЕРЕИМЕНОВАНО
		RMSLogin:                  getEnv("RMS_LOGIN", ""),
		RMSPassword1:              getEnv("RMS_PASSWORD_1", ""),
		RMSPassword2:              getEnv("RMS_PASSWORD_2", ""),
		ServerPollingInterval:     time.Duration(getEnvAsInt("SERVER_POLLING_INTERVAL_HOURS", 12)) * time.Hour, // ПЕРЕИМЕНОВАНО и изменено на часы
		ServerPollingBatchSize:    getEnvAsInt("SERVER_POLLING_BATCH_SIZE", 50),                                // ПЕРЕИМЕНОВАНО

		// Параметры синхронизации с SD
		EnableSDeskSyncWorker: getEnvAsBool("ENABLE_SDESK_SYNC_WORKER", true),
		SDeskSyncInterval:     time.Duration(getEnvAsInt("SDESK_SYNC_INTERVAL_MIN", 10)) * time.Minute,

		AllowedOrigins: strings.Split(allowedOriginsStr, ","),
	}
}

// Вспомогательная функция для получения переменной окружения с значением по умолчанию.
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// Вспомогательная функция для получения int из переменной окружения.
func getEnvAsInt(key string, fallback int) int {
	if valueStr := getEnv(key, ""); valueStr != "" {
		if value, err := strconv.Atoi(valueStr); err == nil {
			return value
		}
	}
	return fallback
}

// Вспомогательная функция для получения bool из переменной окружения.
func getEnvAsBool(key string, fallback bool) bool {
	if valueStr := getEnv(key, ""); valueStr != "" {
		if value, err := strconv.ParseBool(valueStr); err == nil {
			return value
		}
	}
	return fallback
}

go `
===== END config.go =====

internal/db/db.go
===== START db.go =====
go `
package db

import (
	"etalon-server/internal/config"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewConnection создает и возвращает новое подключение к базе данных.
func NewConnection(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	return db, nil
}

go `
===== END db.go =====

internal/handlers/agent_handler.go
===== START agent_handler.go =====
go `
package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// AgentHandler обрабатывает HTTP-запросы от агентов.
type AgentHandler struct {
	logger       *zap.Logger
	agentService services.AgentService
}

// NewAgentHandler создает новый экземпляр обработчика.
func NewAgentHandler(logger *zap.Logger, agentService services.AgentService) *AgentHandler {
	return &AgentHandler{
		logger:       logger,
		agentService: agentService,
	}
}

// RegisterRoutes регистрирует все роуты для агентов.
func (h *AgentHandler) RegisterRoutes(r chi.Router) {
	r.Post("/register", h.registerAgent)
	r.Get("/{uuid}/config", h.getAgentConfig)
	r.Post("/{uuid}/data", h.postAgentData)
}

// registerAgent обрабатывает запрос на первичную регистрацию агента.
func (h *AgentHandler) registerAgent(w http.ResponseWriter, r *http.Request) {
	var dto api.RegistrationRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	// TODO: Добавить валидацию DTO

	_, err := h.agentService.RegisterAgent(r.Context(), &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentAlreadyExists):
			RespondWithError(w, http.StatusConflict, "Агент с таким UUID уже зарегистрирован")
		default:
			h.logger.Error("Ошибка регистрации агента", zap.String("uuid", dto.AgentUUID), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера при регистрации агента")
		}
		return
	}

	// В соответствии с протоколом, отвечаем 202 Accepted.
	// Агент поймет, что его запрос принят в обработку.
	w.WriteHeader(http.StatusAccepted)
	RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "регистрация принята в обработку"})
}

// getAgentConfig возвращает конфигурацию для агента.
func (h *AgentHandler) getAgentConfig(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	config, err := h.agentService.GetAgentConfig(r.Context(), uuid)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentNotFound):
			// Это штатная ситуация для агента, который еще не прошел регистрацию до конца.
			RespondWithError(w, http.StatusNotFound, "Агент не найден или его регистрация еще не завершена")
		default:
			h.logger.Error("Ошибка получения конфигурации агента", zap.String("uuid", uuid), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, config)
}

// postAgentData принимает и обрабатывает оперативные данные от агента.
func (h *AgentHandler) postAgentData(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	err := h.agentService.ProcessData(r.Context(), uuid, &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentNotFound):
			RespondWithError(w, http.StatusNotFound, "Агент не найден")
		default:
			h.logger.Error("Ошибка обработки данных от агента", zap.String("uuid", uuid), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка при обработке данных")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "данные приняты"})
}

go `
===== END agent_handler.go =====

internal/handlers/crud_handlers.go
===== START crud_handlers.go =====
go `
package handlers

import (
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// CrudHandler обрабатывает CRUD-запросы.
type CrudHandler struct {
	logger          *zap.Logger
	db              *gorm.DB
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewCrudHandler создает новый экземпляр обработчика.
func NewCrudHandler(logger *zap.Logger, db *gorm.DB, companyRepo repositories.CompanyRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) *CrudHandler {
	return &CrudHandler{logger, db, companyRepo, serverRepo, workstationRepo, frRepo}
}

// RegisterRoutes регистрирует CRUD роуты.
func (h *CrudHandler) RegisterRoutes(r chi.Router) {
	r.Route("/companies", func(r chi.Router) {
		r.Get("/{uuid}", h.GetCompany)
		r.Post("/", h.CreateCompany)
		r.Put("/{uuid}", h.UpdateCompany)
		r.Delete("/{uuid}", h.DeleteCompany)
	})
	// Аналогичные роуты для других сущностей могут быть добавлены здесь
}

func (h *CrudHandler) GetCompany(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	company, err := h.companyRepo.GetByUUID(r.Context(), uuid)
	if err != nil {
		h.logger.Error("Failed to get company", zap.String("uuid", uuid), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to retrieve company")
		return
	}
	if company == nil {
		RespondWithError(w, http.StatusNotFound, "Company not found")
		return
	}
	RespondWithJSON(w, http.StatusOK, company)
}

func (h *CrudHandler) CreateCompany(w http.ResponseWriter, r *http.Request) {
	var dto api.CompanyCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// TODO: Добавить валидацию DTO
	company := &models.Company{
		Title:                 dto.Title,
		Address:               dto.Address,
		AdditionalName:        dto.AdditionalName,
		ParentServiceDeskUUID: dto.ParentServiceDeskUUID,
	}
	company.MetaClass = "ou$company"

	err := h.db.Transaction(func(tx *gorm.DB) error {
		return h.companyRepo.Create(r.Context(), tx, company)
	})
	if err != nil {
		h.logger.Error("Failed to create company", zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to create company")
		return
	}
	RespondWithJSON(w, http.StatusCreated, company)
}

func (h *CrudHandler) UpdateCompany(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	var updateData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	// Удаляем поля, которые не должны обновляться вручную через API
	delete(updateData, "uuid")
	delete(updateData, "id")
	delete(updateData, "meta_class")
	delete(updateData, "created_at")
	delete(updateData, "updated_at")
	delete(updateData, "deleted_at")

	var updated bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		updated, txErr = h.companyRepo.Update(r.Context(), tx, uuid, updateData)
		return txErr
	})
	if err != nil {
		h.logger.Error("Failed to update company", zap.String("uuid", uuid), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to update company")
		return
	}
	if !updated {
		RespondWithError(w, http.StatusNotFound, "Company not found or no changes applied")
		return
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "company updated successfully"})
}

func (h *CrudHandler) DeleteCompany(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	var deleted bool
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		deleted, txErr = h.companyRepo.Delete(r.Context(), tx, uuid)
		return txErr
	})

	if err != nil {
		h.logger.Error("Failed to delete company", zap.String("uuid", uuid), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Failed to delete company")
		return
	}
	if !deleted {
		RespondWithError(w, http.StatusNotFound, "Company not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ИЗМЕНЕНИЕ: Вспомогательные функции respondWithError и respondWithJSON полностью удалены из этого файла.

go `
===== END crud_handlers.go =====

internal/handlers/middleware.go
===== START middleware.go =====
go `
package handlers

import (
	"net/http"
	"strings"
)

// AgentAuthMiddleware проверяет наличие и правильность Bearer токена для агентов.
func AgentAuthMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				// Если ключ не задан на сервере, считаем это ошибкой конфигурации.
				RespondWithError(w, http.StatusInternalServerError, "Сервер не настроен для аутентификации агентов")
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				RespondWithError(w, http.StatusUnauthorized, "Отсутствует заголовок Authorization")
				return
			}

			headerParts := strings.Split(authHeader, " ")
			if len(headerParts) != 2 || strings.ToLower(headerParts[0]) != "bearer" {
				RespondWithError(w, http.StatusUnauthorized, "Неверный формат заголовка Authorization")
				return
			}

			token := headerParts[1]
			if token != apiKey {
				RespondWithError(w, http.StatusUnauthorized, "Неверный ключ API агента")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

go `
===== END middleware.go =====

internal/handlers/response.go
===== START response.go =====
go `
package handlers

import (
	"encoding/json"
	"etalon-server/internal/api"
	"net/http"
)

// RespondWithError отправляет стандартизированный JSON с ошибкой.
func RespondWithError(w http.ResponseWriter, code int, message string) {
	RespondWithJSON(w, code, api.ErrorResponseDTO{Error: message})
}

// RespondWithJSON отправляет JSON-ответ с указанным кодом и данными.
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		// В случае ошибки маршалинга, отправляем внутреннюю ошибку сервера
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

go `
===== END response.go =====

internal/handlers/search_handler.go
===== START search_handler.go =====
go `
package handlers

import (
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SearchHandler обрабатывает поисковые запросы.
type SearchHandler struct {
	logger          *zap.Logger
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewSearchHandler создает новый экземпляр обработчика.
func NewSearchHandler(
	logger *zap.Logger,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) *SearchHandler {
	return &SearchHandler{logger, companyRepo, serverRepo, workstationRepo, frRepo}
}

// RegisterRoutes регистрирует роут для поиска.
func (h *SearchHandler) RegisterRoutes(r chi.Router) {
	r.Get("/search", h.Search)
}

// Search выполняет финальный, UI-ориентированный, owner-centric поиск.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		RespondWithError(w, http.StatusBadRequest, "Поисковый запрос не может быть пустым")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	ctx := r.Context()
	log := h.logger.With(zap.String("search_term", term))

	// --- Шаг 1: Найти все сущности, напрямую совпадающие с поисковым запросом ---
	var wg sync.WaitGroup
	var initialCompanies []models.Company
	var initialServers []models.Server
	var initialWorkstations []models.Workstation
	var initialFRs []models.FiscalRegister

	wg.Add(4)
	go func() { defer wg.Done(); initialCompanies, _ = h.companyRepo.Search(ctx, term, true, limit, 0) }()
	go func() { defer wg.Done(); initialServers, _ = h.serverRepo.Search(ctx, term, limit, 0) }()
	go func() { defer wg.Done(); initialWorkstations, _ = h.workstationRepo.Search(ctx, term, limit, 0) }()
	go func() { defer wg.Done(); initialFRs, _ = h.frRepo.Search(ctx, term, limit, 0) }()
	wg.Wait()

	// --- Шаг 2: Собрать уникальный список ID всех затронутых владельцев ---
	ownerUUIDs := make(map[string]bool)
	for _, company := range initialCompanies {
		ownerUUIDs[*company.ServiceDeskUUID] = true
	}
	for _, server := range initialServers {
		if server.OwnerServiceDeskUUID != nil {
			ownerUUIDs[*server.OwnerServiceDeskUUID] = true
		}
	}
	for _, ws := range initialWorkstations {
		if ws.OwnerServiceDeskUUID != nil {
			ownerUUIDs[*ws.OwnerServiceDeskUUID] = true
		}
	}
	for _, fr := range initialFRs {
		if fr.OwnerServiceDeskUUID != nil {
			ownerUUIDs[*fr.OwnerServiceDeskUUID] = true
		}
	}

	if len(ownerUUIDs) == 0 {
		log.Info("Не найдено совпадений или связанных владельцев.")
		RespondWithJSON(w, http.StatusOK, api.FinalSearchResponseDTO{SearchResults: []api.SearchGroupDTO{}})
		return
	}

	uuids := make([]string, 0, len(ownerUUIDs))
	for uuid := range ownerUUIDs {
		uuids = append(uuids, uuid)
	}

	// --- Шаг 3: Загрузить ВСЕ данные для найденных владельцев ---
	var allOwnerCompanies []models.Company
	var allOwnerServers []models.Server
	var allOwnerWorkstations []models.Workstation
	var allOwnerFRs []models.FiscalRegister

	wg.Add(4)
	go func() { defer wg.Done(); allOwnerCompanies, _ = h.companyRepo.GetByUUIDs(ctx, uuids) }()
	go func() { defer wg.Done(); allOwnerServers, _ = h.serverRepo.FindByOwnerUUIDs(ctx, uuids) }()
	go func() { defer wg.Done(); allOwnerWorkstations, _ = h.workstationRepo.FindByOwnerUUIDs(ctx, uuids) }()
	go func() { defer wg.Done(); allOwnerFRs, _ = h.frRepo.FindByOwnerUUIDs(ctx, uuids) }()
	wg.Wait()

	// --- Шаг 4: Сформировать финальную сгруппированную структуру ---

	// Преобразуем оборудование в мапы для быстрого доступа
	serversByOwner := groupServersByOwner(allOwnerServers)
	workstationsByOwner := groupWorkstationsByOwner(allOwnerWorkstations)
	frsByOwner := groupFRsByOwner(allOwnerFRs)

	finalResponse := api.FinalSearchResponseDTO{}

	// Создаем группу для каждой найденной компании-владельца
	for _, owner := range allOwnerCompanies {
		ownerID := *owner.ServiceDeskUUID

		group := api.SearchGroupDTO{
			Owner: api.OwnerFullDTO{
				UUID:           ownerID,
				Name:           utils.SafeStringDereference(owner.Title),
				Address:        owner.Address,
				ActiveContract: owner.ActiveContract,
				AdditionalInfo: owner.AdditionalName,
			},
			FoundEntities: []api.FoundEntityDTO{},
		}

		// Добавляем все оборудование, принадлежащее этому владельцу
		if servers, ok := serversByOwner[ownerID]; ok {
			group.FoundEntities = append(group.FoundEntities, servers...)
		}
		if workstations, ok := workstationsByOwner[ownerID]; ok {
			group.FoundEntities = append(group.FoundEntities, workstations...)
		}
		if frs, ok := frsByOwner[ownerID]; ok {
			group.FoundEntities = append(group.FoundEntities, frs...)
		}

		finalResponse.SearchResults = append(finalResponse.SearchResults, group)
	}

	RespondWithJSON(w, http.StatusOK, finalResponse)
}

// --- Вспомогательные функции-группировщики ---

func groupServersByOwner(servers []models.Server) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, s := range servers {
		if s.OwnerServiceDeskUUID != nil {
			ownerID := *s.OwnerServiceDeskUUID

			// Формируем ссылку на партнерский кабинет
			var partnersLink *string
			clientIdStr := utils.SafeStringDereference(s.CabinetLink)
			// Проверяем, что clientIdStr не пустой и не содержит 'N/A'
			if clientIdStr != "" && clientIdStr != "N/A" {
				var link string
				ipStr := utils.SafeStringDereference(s.IP)
				if strings.Contains(strings.ToLower(ipStr), "syrve") {
					link = fmt.Sprintf("https://pp.syrve.com/en/cabinet/client-area/index.html?clientId=%s", clientIdStr)
				} else {
					link = fmt.Sprintf("https://pp.iiko.ru/ru/cabinet/client-area/index.html?clientId=%s", clientIdStr)
				}
				partnersLink = &link
			}

			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Server",
				Data: api.ServerRichDTO{
					UUID:         *s.ServiceDeskUUID,
					DeviceName:   s.DeviceName,
					IP:           s.IP,
					Status:       s.Status,
					Anydesk:      s.Anydesk,
					Teamviewer:   s.Teamviewer,
					RDP:          s.RDP,
					Litemanager:  s.Litemanager,
					UniqueID:     s.UniqueID,
					PartnersLink: partnersLink,
				},
			})
		}
	}
	return result
}

func groupWorkstationsByOwner(workstations []models.Workstation) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, ws := range workstations {
		if ws.OwnerServiceDeskUUID != nil {
			ownerID := *ws.OwnerServiceDeskUUID
			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "Workstation",
				Data: api.WorkstationRichDTO{
					UUID: *ws.ServiceDeskUUID, DeviceName: ws.DeviceName, Status: ws.Status,
					Anydesk: ws.Anydesk, Teamviewer: ws.Teamviewer, Litemanager: ws.Litemanager,
				},
			})
		}
	}
	return result
}

func groupFRsByOwner(frs []models.FiscalRegister) map[string][]api.FoundEntityDTO {
	result := make(map[string][]api.FoundEntityDTO)
	for _, fr := range frs {
		if fr.OwnerServiceDeskUUID != nil {
			ownerID := *fr.OwnerServiceDeskUUID
			result[ownerID] = append(result[ownerID], api.FoundEntityDTO{
				EntityType: "FiscalRegister",
				Data: api.FiscalRegisterRichDTO{
					UUID: *fr.ServiceDeskUUID, RNKKT: fr.RNKKT, ModelKKT: fr.ModelKKT,
					FNExpireDate: fr.FNExpireDate, FNRegistrationDate: fr.KKTRegDate,
					DriverVersion: fr.DriverVersion, FirmwareVersion: fr.FRDownloader,
				},
			})
		}
	}
	return result
}

go `
===== END search_handler.go =====

internal/handlers/server_actions_handler.go
===== START server_actions_handler.go =====
go `
// internal/handlers/server_actions_handler.go
package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ServerActionsHandler обрабатывает специфичные действия над серверами.
type ServerActionsHandler struct {
	logger     *zap.Logger
	pollingSvc services.ServerPollingService
}

// NewServerActionsHandler создает новый экземпляр обработчика.
func NewServerActionsHandler(logger *zap.Logger, pollingSvc services.ServerPollingService) *ServerActionsHandler {
	return &ServerActionsHandler{
		logger:     logger,
		pollingSvc: pollingSvc,
	}
}

// RegisterRoutes регистрирует роуты для действий с серверами.
func (h *ServerActionsHandler) RegisterRoutes(r chi.Router) {
	r.Post("/servers/{uuid}/install_license", h.installLicense)
	r.Post("/servers/{uuid}/poll", h.pollServerStatus) // НОВЫЙ РОУТ
}

type installLicenseRequestDTO struct {
	UniqueID string `json:"uniqueId"`
}

// installLicense обрабатывает запрос на запуск установки лицензии.
func (h *ServerActionsHandler) installLicense(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		RespondWithError(w, http.StatusBadRequest, "UUID сервера не указан")
		return
	}

	var dto installLicenseRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	if dto.UniqueID == "" {
		RespondWithError(w, http.StatusBadRequest, "Поле 'uniqueId' обязательно для заполнения")
		return
	}

	err := h.pollingSvc.InstallLicense(r.Context(), uuid, dto.UniqueID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			RespondWithError(w, http.StatusNotFound, "Сервер с указанным UUID не найден")
		} else {
			h.logger.Error("Ошибка при вызове заглушки установки лицензии",
				zap.String("uuid", uuid),
				zap.String("uniqueId", dto.UniqueID),
				zap.Error(err),
			)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Команда на установку лицензии отправлена успешно"})
}

// pollServerStatus обрабатывает запрос на принудительный асинхронный опрос статуса сервера.
func (h *ServerActionsHandler) pollServerStatus(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		RespondWithError(w, http.StatusBadRequest, "UUID сервера не указан")
		return
	}

	err := h.pollingSvc.PollSingleServer(r.Context(), uuid)

	if err != nil {
		switch {
		case errors.Is(err, services.ErrRateLimitExceeded):
			RespondWithError(w, http.StatusTooManyRequests, "Превышен лимит запросов на опрос статуса для этого сервера (не более 3 раз в 2 минуты)")
		case errors.Is(err, gorm.ErrRecordNotFound):
			RespondWithError(w, http.StatusNotFound, "Сервер с указанным UUID не найден")
		default:
			h.logger.Error("Ошибка при запуске принудительного опроса", zap.String("uuid", uuid), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	RespondWithJSON(w, http.StatusAccepted, map[string]string{"message": "Задача на опрос статуса сервера принята в обработку"})
}

go `
===== END server_actions_handler.go =====

internal/handlers/sync_handlers.go
===== START sync_handlers.go =====
go `
package handlers

import (
	"etalon-server/internal/seeder"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SyncHandler обрабатывает запросы, связанные с синхронизацией и наполнением базы.
type SyncHandler struct {
	logger    *zap.Logger
	seeder    *seeder.Seeder
	seederKey string
}

// NewSyncHandler создает новый обработчик синхронизации.
func NewSyncHandler(logger *zap.Logger, seeder *seeder.Seeder, seederKey string) *SyncHandler {
	return &SyncHandler{
		logger:    logger,
		seeder:    seeder,
		seederKey: seederKey,
	}
}

// RegisterRoutes регистрирует роуты для этого обработчика.
func (h *SyncHandler) RegisterRoutes(router chi.Router) {
	router.Post("/seed", h.TriggerSeed)
}

// TriggerSeed запускает фоновое наполнение базы данных из мок-файлов.
func (h *SyncHandler) TriggerSeed(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" || key != h.seederKey {
		RespondWithError(w, http.StatusUnauthorized, "Неверный или отсутствует ключ доступа")
		return
	}

	go func() {
		h.logger.Info("Запуск наполнения БД через API...")
		mockClient := seeder.NewMockServiceDeskClient(h.logger, "./tools/seeder/mock_data")
		if err := h.seeder.SeedDatabase(mockClient); err != nil {
			h.logger.Error("Процесс наполнения БД завершился с ошибкой", zap.Error(err))
		} else {
			h.logger.Info("Процесс наполнения БД, запущенный через API, успешно завершен.")
		}
	}()

	RespondWithJSON(w, http.StatusAccepted, map[string]string{
		"message": "Наполнение базы данных запущено в фоновом режиме",
	})
}

go `
===== END sync_handlers.go =====

internal/handlers/task_handler.go
===== START task_handler.go =====
go `
package handlers

import (
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TaskHandler обрабатывает запросы, связанные с задачами сверки и поиском дубликатов.
type TaskHandler struct {
	logger *zap.Logger
	db     *gorm.DB
}

// NewTaskHandler создает новый экземпляр обработчика.
func NewTaskHandler(logger *zap.Logger, db *gorm.DB) *TaskHandler {
	return &TaskHandler{
		logger: logger,
		db:     db,
	}
}

// RegisterRoutes регистрирует роуты для задач и дубликатов.
func (h *TaskHandler) RegisterRoutes(r chi.Router) {
	r.Get("/tasks", h.GetTasks)
	r.Post("/tasks/{id}/resolve", h.ResolveTask)
	r.Get("/duplicates", h.GetDuplicates)
}

// GetTasks возвращает список задач сверки с фильтрацией и пагинацией.
func (h *TaskHandler) GetTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil || offset < 0 {
		offset = 0
	}

	var tasks []models.ReconciliationTask
	query := h.db.Model(&models.ReconciliationTask{})

	if status != "" {
		query = query.Where("status = ?", status)
	}

	err = query.Limit(limit).Offset(offset).Order("created_at desc").Find(&tasks).Error
	if err != nil {
		h.logger.Error("Не удалось получить задачи из БД", zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Ошибка получения списка задач")
		return
	}

	RespondWithJSON(w, http.StatusOK, tasks)
}

// ResolveTask изменяет статус задачи.
func (h *TaskHandler) ResolveTask(w http.ResponseWriter, r *http.Request) {
	taskIDStr := chi.URLParam(r, "id")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 32)
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "Некорректный ID задачи")
		return
	}

	var req api.ResolveTaskRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Некорректное тело запроса")
		return
	}

	if req.Status == "" {
		RespondWithError(w, http.StatusBadRequest, "Поле 'status' обязательно для заполнения")
		return
	}

	updates := map[string]interface{}{"status": req.Status}
	if req.Comment != "" {
		updates["comment"] = gorm.Expr("comment || '\n' || ?", req.Comment)
	}

	var task models.ReconciliationTask
	err = h.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.ReconciliationTask{}).Where("id = ?", taskID).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.First(&task, taskID).Error
	})

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			RespondWithError(w, http.StatusNotFound, "Задача не найдена")
		} else {
			h.logger.Error("Ошибка обновления задачи", zap.Uint64("taskID", taskID), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, task)
}

// GetDuplicates находит и возвращает группы дубликатов в формате JSON.
func (h *TaskHandler) GetDuplicates(w http.ResponseWriter, r *http.Request) {
	var allGroups []api.DuplicateGroupDTO

	wsFields := []string{"anydesk", "teamviewer", "litemanager"}
	for _, field := range wsFields {
		groups, err := h.findDuplicateGroups(field, "Workstation")
		if err != nil {
			h.logger.Error("Ошибка поиска дубликатов Workstation", zap.String("field", field), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Ошибка поиска дубликатов")
			return
		}
		allGroups = append(allGroups, groups...)
	}

	serverGroups, err := h.findDuplicateGroups("ip", "Server")
	if err != nil {
		h.logger.Error("Ошибка поиска дубликатов Server", zap.String("field", "ip"), zap.Error(err))
		RespondWithError(w, http.StatusInternalServerError, "Ошибка поиска дубликатов")
		return
	}
	allGroups = append(allGroups, serverGroups...)

	RespondWithJSON(w, http.StatusOK, allGroups)
}

func (h *TaskHandler) findDuplicateGroups(field string, entityType string) ([]api.DuplicateGroupDTO, error) {
	var results []struct {
		Value string
		Count int
	}
	model := h.getModel(entityType)
	if model == nil {
		return nil, fmt.Errorf("неизвестный тип сущности: %s", entityType)
	}

	err := h.db.Model(model).
		Select(fmt.Sprintf("%s as value, count(*) as count", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&results).Error

	if err != nil {
		return nil, err
	}

	var groups []api.DuplicateGroupDTO
	for _, res := range results {
		var records []interface{}
		if entityType == "Workstation" {
			var wsRecords []models.Workstation
			h.db.Where(fmt.Sprintf("%s = ?", field), res.Value).Find(&wsRecords)
			for i := range wsRecords {
				records = append(records, wsRecords[i])
			}
		} else if entityType == "Server" {
			var srvRecords []models.Server
			h.db.Where(fmt.Sprintf("%s = ?", field), res.Value).Find(&srvRecords)
			for i := range srvRecords {
				records = append(records, srvRecords[i])
			}
		}

		if len(records) < 2 {
			continue
		}

		sort.Slice(records, func(i, j int) bool {
			dateI := getLMDFromInterface(records[i])
			dateJ := getLMDFromInterface(records[j])
			if dateI == nil {
				return false
			}
			if dateJ == nil {
				return true
			}
			return dateI.After(*dateJ)
		})

		groups = append(groups, api.DuplicateGroupDTO{
			Field:      field,
			Value:      res.Value,
			MainRecord: records[0],
			Duplicates: records[1:],
			EntityType: entityType,
		})
	}
	return groups, nil
}

func (h *TaskHandler) getModel(entityType string) interface{} {
	switch entityType {
	case "Workstation":
		return &models.Workstation{}
	case "Server":
		return &models.Server{}
	default:
		return nil
	}
}

func getLMDFromInterface(record interface{}) *time.Time {
	switch v := record.(type) {
	case models.Workstation:
		return v.LastModifiedDate
	case models.Server:
		return v.LastModifiedDate
	case models.FiscalRegister:
		return v.LastModifiedDate
	default:
		return nil
	}
}

go `
===== END task_handler.go =====

internal/logger/logger.go
===== START logger.go =====
go `
package logger

import (
	"os"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New инициализирует логгер zap.
// Он может писать логи как в консоль, так и в файл с ротацией.
func New(logPath string, disableFileLogging bool) *zap.Logger {
	// Конфигурация для логгирования в консоль
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.Lock(os.Stdout),
		zap.InfoLevel,
	)

	cores := []zapcore.Core{consoleCore}

	// Конфигурация для логгирования в файл с ротацией
	if !disableFileLogging {
		fileWriter := zapcore.AddSync(&lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    10, // megabytes
			MaxBackups: 3,
			MaxAge:     28, // days
			Compress:   true,
		})

		fileCore := zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			fileWriter,
			zap.InfoLevel,
		)
		cores = append(cores, fileCore)
	}

	// Объединяем ядра для вывода в несколько мест
	core := zapcore.NewTee(cores...)

	// Создаем логгер с опцией AddCaller для вывода имени файла и строки
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger
}

go `
===== END logger.go =====

internal/models/models.go
===== START models.go =====
go `
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Константы для статусов агента
const (
	StatusPendingOwner       = "pending_owner"
	StatusPendingZabbix      = "pending_zabbix_registration"
	StatusActive             = "active"
	StatusRegistrationFailed = "registration_failed"
)

// Base содержит общие поля для всех моделей.
type Base struct {
	ID              string  `gorm:"primaryKey;type:text"`
	MetaClass       string  `gorm:"type:text"`
	ServiceDeskUUID *string `gorm:"type:text;unique"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate будет вызван GORM перед созданием записи.
// Он генерирует новый UUID для поля ID.
func (base *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if base.ID == "" {
		base.ID = uuid.New().String()
	}
	return
}

// Company представляет сущность компании.
type Company struct {
	Base
	Address               *string        `gorm:"type:text"`
	Title                 *string        `gorm:"type:text"`
	ActiveContract        *bool          `gorm:"type:boolean"`
	LastModifiedDate      *time.Time     `json:"last_modified_date"`
	AdditionalName        *string        `gorm:"type:text"`
	ParentServiceDeskUUID *string        `gorm:"type:text"`
	ContractInfo          datatypes.JSON `gorm:"type:jsonb"`
	Parent                *Company       `gorm:"foreignKey:ParentServiceDeskUUID;references:ServiceDeskUUID"`

	Servers         []Server         `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
	Workstations    []Workstation    `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
	FiscalRegisters []FiscalRegister `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
}

// Server представляет сущность сервера.
type Server struct {
	Base
	UniqueID             *string    `gorm:"type:text"`
	CRMid                *string    `gorm:"column:crm_id;type:text;index"`
	Teamviewer           *string    `gorm:"type:text"`
	RDP                  *string    `gorm:"type:text"`
	Anydesk              *string    `gorm:"type:text"`
	IP                   *string    `gorm:"type:text"`
	CabinetLink          *string    `gorm:"type:text"`
	DeviceName           *string    `gorm:"type:text;index"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	Litemanager          *string    `gorm:"type:text"`
	ServerVersion        *string    `gorm:"type:text"`
	Description          *string    `gorm:"type:text"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"` // Ссылка на Company.UUID

	// Новые поля для CRMid воркера
	ServerName    *string    `gorm:"type:text"`
	ServerEdition *string    `gorm:"type:varchar(50)"`
	LastPolledAt  *time.Time `gorm:"column:last_polled_at"`
	Status        string     `gorm:"type:varchar(50);default:'unknown';index"` // ОБНОВЛЕНО: 'active', 'inactive', 'to_delete', 'offline', 'license', 'starting', 'unknown', 'archived'
}

// Workstation представляет сущность рабочей станции.
type Workstation struct {
	Base
	Teamviewer           *string    `gorm:"type:text"`
	Anydesk              *string    `gorm:"type:text"`
	Litemanager          *string    `gorm:"type:text"`
	DeviceName           *string    `gorm:"type:text"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	Description          *string    `gorm:"type:text"`
	Status               *string    `gorm:"type:varchar(50);default:'offline'"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"` // Ссылка на Company.UUID
}

// FiscalRegister представляет сущность фискального регистратора.
type FiscalRegister struct {
	Base
	ModelKKT             *string    `gorm:"type:text"`
	FFD                  *string    `gorm:"type:text"`
	RNKKT                *string    `gorm:"column:rn_kkt;type:text;index"`
	LegalName            *string    `gorm:"type:text"`
	INN                  *string    `gorm:"column:inn;type:text;index"`
	FRSerialNumber       *string    `gorm:"type:text;index"`
	FNNumber             *string    `gorm:"type:text"`
	KKTRegDate           *time.Time `json:"kkt_reg_date"`
	FNExpireDate         *time.Time `json:"fn_expire_date"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	FRDownloader         *string    `gorm:"type:text"`
	DriverVersion        *string    `gorm:"type:varchar(50)"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"` // Ссылка на Company.UUID
}

// Agent представляет экземпляр агента, установленного на машине клиента.
type Agent struct {
	UUID                 string         `gorm:"primaryKey;type:text"`      // UUID, который генерирует сам агент
	Type                 string         `gorm:"type:varchar(50);not null"` // 'workstation' или 'server'
	OwnerServiceDeskUUID string         `gorm:"type:text;index"`           // UUID сущности (Workstation или Server), к которой он привязан
	Config               datatypes.JSON `gorm:"type:jsonb"`                // Конфигурация агента в формате JSON
	LastHeartbeat        time.Time      // Время последнего heartbeat'а (будет обновляться)
	Version              string         `gorm:"type:varchar(50)"`       // Версия бинарного файла агента
	Hostname             string         `gorm:"type:text"`              // Имя хоста, переданное агентом
	ZabbixHostname       string         `gorm:"type:text"`              // Имя хоста, сгенерированное для Zabbix
	Status               string         `gorm:"type:varchar(50);index"` // Статус регистрации агента
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// AgentFile хранит информацию о последней обработке файла с FTP.
type AgentFile struct {
	FileName              string    `gorm:"primaryKey;type:text"`
	LastProcessedModTime  time.Time `gorm:"not null"`
	LastProcessedFileSize int64     `gorm:"not null"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ReconciliationTask представляет задачу для ручного разбора администратором.
type ReconciliationTask struct {
	ID         uint           `gorm:"primarykey"`
	TaskType   string         `gorm:"type:varchar(50);not null;index"`      // 'owner_mismatch', 'new_client', 'delete_duplicate', 'delete_from_servicedesk', 'data_conflict'
	EntityType string         `gorm:"type:varchar(50)"`                     // 'FiscalRegister', 'Workstation', 'Server'
	EntityUUID string         `gorm:"type:text"`                            // UUID сущности, с которой связана задача
	Details    datatypes.JSON `gorm:"type:jsonb"`                           // Детали задачи, например, старый и новый владелец
	Status     string         `gorm:"type:varchar(50);default:'new';index"` // 'new', 'resolved'
	Comment    string         `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

go `
===== END models.go =====

internal/repositories/agent_repo.go
===== START agent_repo.go =====
go `
package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// AgentRepo определяет интерфейс для работы с хранилищем агентов.
type AgentRepo interface {
	GetByUUID(ctx context.Context, uuid string) (*models.Agent, error)
	Create(ctx context.Context, agent *models.Agent) error
	Update(ctx context.Context, agent *models.Agent) error
	CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error)
}

type agentRepo struct {
	db *gorm.DB
}

// NewAgentRepo создает новый экземпляр репозитория агентов.
func NewAgentRepo(db *gorm.DB) AgentRepo {
	return &agentRepo{db: db}
}

func (r *agentRepo) GetByUUID(ctx context.Context, uuid string) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&agent).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil // Это не ошибка, а нормальный случай "не найдено"
	}
	return &agent, err
}

func (r *agentRepo) Create(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *agentRepo) Update(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

// CountByOwnerUUID подсчитывает количество агентов, принадлежащих одной компании.
func (r *agentRepo) CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Agent{}).Where("owner_service_desk_uuid = ?", ownerUUID).Count(&count).Error
	return count, err
}

go `
===== END agent_repo.go =====

internal/repositories/company_repo.go
===== START company_repo.go =====
go `
package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// CompanyRepo определяет интерфейс для работы с хранилищем компаний.
type CompanyRepo interface {
	Create(ctx context.Context, tx *gorm.DB, company *models.Company) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Company, error)
	GetByUUIDs(ctx context.Context, uuids []string) ([]models.Company, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Company, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error)
	Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error)
}

// companyRepo реализует интерфейс CompanyRepo.
type companyRepo struct {
	db *gorm.DB
}

// NewCompanyRepo создает новый экземпляр репозитория компаний.
func NewCompanyRepo(db *gorm.DB) CompanyRepo {
	return &companyRepo{db: db}
}

// dbOrTx возвращает переданную транзакцию или основное подключение к БД.
func (r *companyRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *companyRepo) Create(ctx context.Context, tx *gorm.DB, company *models.Company) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(company).Error
}

func (r *companyRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Company{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *companyRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Company{})
	return res.RowsAffected > 0, res.Error
}

func (r *companyRepo) GetByUUID(ctx context.Context, uuid string) (*models.Company, error) {
	var company models.Company
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&company).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &company, err
}

// GetByUUIDs находит компании по списку их ServiceDesk UUID.
func (r *companyRepo) GetByUUIDs(ctx context.Context, uuids []string) ([]models.Company, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	var companies []models.Company
	err := r.db.WithContext(ctx).Where("service_desk_uuid IN ?", uuids).Find(&companies).Error
	return companies, err
}

func (r *companyRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Company, error) {
	var company models.Company
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&company).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &company, err
}

func (r *companyRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error) {
	var companies []*models.Company
	err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&companies).Error
	if err != nil {
		return nil, err
	}
	companyMap := make(map[string]*models.Company, len(companies))
	for _, c := range companies {
		if c.ServiceDeskUUID != nil {
			companyMap[*c.ServiceDeskUUID] = c
		}
	}
	return companyMap, nil
}

func (r *companyRepo) Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error) {
	var companies []models.Company
	query := r.db.WithContext(ctx).
		Where("title ILIKE ? OR address ILIKE ? OR additional_name ILIKE ?", "%"+term+"%", "%"+term+"%", "%"+term+"%")

	if !showInactive {
		query = query.Where("active_contract = ?", true)
	}

	err := query.Limit(limit).Offset(offset).Find(&companies).Error
	return companies, err
}

go `
===== END company_repo.go =====

internal/repositories/fr_repo.go
===== START fr_repo.go =====
go `
package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// FiscalRegisterRepo определяет интерфейс для работы с хранилищем фискальных регистраторов.
type FiscalRegisterRepo interface {
	Create(ctx context.Context, tx *gorm.DB, fr *models.FiscalRegister) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.FiscalRegister, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.FiscalRegister, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.FiscalRegister, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.FiscalRegister, error)
	FindBySerialNumber(ctx context.Context, sn string) (*models.FiscalRegister, error)
	FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.FiscalRegister, error)
}

// frRepo реализует интерфейс FiscalRegisterRepo.
type frRepo struct {
	db *gorm.DB
}

// NewFiscalRegisterRepo создает новый экземпляр репозитория.
func NewFiscalRegisterRepo(db *gorm.DB) FiscalRegisterRepo {
	return &frRepo{db: db}
}

func (r *frRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *frRepo) Create(ctx context.Context, tx *gorm.DB, fr *models.FiscalRegister) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(fr).Error
}

func (r *frRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.FiscalRegister{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" фискального регистратора по его ServiceDesk UUID.
func (r *frRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.FiscalRegister{})
	return res.RowsAffected > 0, res.Error
}

func (r *frRepo) GetByUUID(ctx context.Context, uuid string) (*models.FiscalRegister, error) {
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

// GetByUUIDUnscoped находит запись по UUID, включая "мягко удаленные".
func (r *frRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.FiscalRegister, error) {
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

func (r *frRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.FiscalRegister, error) {
	var frs []*models.FiscalRegister
	if err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&frs).Error; err != nil {
		return nil, err
	}
	frMap := make(map[string]*models.FiscalRegister, len(frs))
	for _, fr := range frs {
		if fr.ServiceDeskUUID != nil {
			frMap[*fr.ServiceDeskUUID] = fr
		}
	}
	return frMap, nil
}

func (r *frRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.FiscalRegister, error) {
	var frs []models.FiscalRegister
	err := r.db.WithContext(ctx).
		Where("rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&frs).Error
	return frs, err
}

// FindBySerialNumber ищет фискальный регистратор по серийному номеру.
func (r *frRepo) FindBySerialNumber(ctx context.Context, sn string) (*models.FiscalRegister, error) {
	if sn == "" {
		return nil, nil
	}
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Where("fr_serial_number = ?", sn).Order("last_modified_date DESC").First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

// FindByOwnerUUIDs находит все фискальные регистраторы, принадлежащие указанным владельцам.
func (r *frRepo) FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.FiscalRegister, error) {
	if len(ownerUUIDs) == 0 {
		return nil, nil
	}
	var frs []models.FiscalRegister
	err := r.db.WithContext(ctx).Where("owner_service_desk_uuid IN ?", ownerUUIDs).Find(&frs).Error
	return frs, err
}

go `
===== END fr_repo.go =====

internal/repositories/server_repo.go
===== START server_repo.go =====
go `
// internal/repositories/server_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/models"
	"time"

	"gorm.io/gorm"
)

// ServerRepo определяет интерфейс для работы с хранилищем серверов.
type ServerRepo interface {
	Create(ctx context.Context, tx *gorm.DB, server *models.Server) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Server, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Server, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Server, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error)
	FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*models.Server, error)
	FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Server, error)
	FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]models.Server, error) // НОВЫЙ МЕТОД
}

type serverRepo struct {
	db *gorm.DB
}

// NewServerRepo создает новый экземпляр репозитория.
func NewServerRepo(db *gorm.DB) ServerRepo {
	return &serverRepo{db: db}
}

func (r *serverRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *serverRepo) Create(ctx context.Context, tx *gorm.DB, server *models.Server) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(server).Error
}

func (r *serverRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Server{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" сервера по его ServiceDesk UUID.
func (r *serverRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Server{})
	return res.RowsAffected > 0, res.Error
}

func (r *serverRepo) GetByUUID(ctx context.Context, uuid string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}
func (r *serverRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}

func (r *serverRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Server, error) {
	var servers []*models.Server
	if err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&servers).Error; err != nil {
		return nil, err
	}
	serverMap := make(map[string]*models.Server, len(servers))
	for _, s := range servers {
		if s.ServiceDeskUUID != nil {
			serverMap[*s.ServiceDeskUUID] = s
		}
	}
	return serverMap, nil
}

func (r *serverRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error) {
	var servers []models.Server
	err := r.db.WithContext(ctx).
		Where("device_name ILIKE ? OR ip ILIKE ? OR unique_id ILIKE ? OR description ILIKE ? OR server_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&servers).Error
	return servers, err
}

// FindForPolling находит серверы, которые необходимо опросить.
// Выбирает серверы, которые еще не опрашивались или чья последняя проверка была раньше, чем `interval` назад.
func (r *serverRepo) FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]models.Server, error) {
	var servers []models.Server
	threshold := time.Now().Add(-interval)

	err := r.db.WithContext(ctx).
		Where("ip IS NOT NULL AND ip != ''").
		Where("status != ?", "archived").
		Where("last_polled_at IS NULL OR last_polled_at < ?", threshold).
		Limit(limit).
		Order("last_polled_at ASC"). // Начинаем с самых старых
		Find(&servers).Error
	return servers, err
}

// FindByCRMidOrIP ищет сервер по CRMid (приоритет) или по IP.
func (r *serverRepo) FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*models.Server, error) {
	var server models.Server

	// CRMid является более надежным идентификатором
	if crmid != "" {
		err := r.db.WithContext(ctx).Where("crm_id = ?", crmid).First(&server).Error
		if err == nil {
			return &server, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}

	// Если по CRMid не нашли, ищем по IP с точным совпадением
	if ip != "" {
		err := r.db.WithContext(ctx).Where("ip = ?", ip).First(&server).Error
		if err == gorm.ErrRecordNotFound {
			return nil, nil // Явно возвращаем nil, если не найдено
		}
		return &server, err
	}

	return nil, nil
}

// FindByOwnerUUIDs находит все серверы, принадлежащие указанным владельцам.
func (r *serverRepo) FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Server, error) {
	if len(ownerUUIDs) == 0 {
		return nil, nil
	}
	var servers []models.Server
	err := r.db.WithContext(ctx).Where("owner_service_desk_uuid IN ?", ownerUUIDs).Find(&servers).Error
	return servers, err
}

go `
===== END server_repo.go =====

internal/repositories/workstation_repo.go
===== START workstation_repo.go =====
go `
package repositories

import (
	"context"
	"etalon-server/internal/models"
	"strings"

	"gorm.io/gorm"
)

// WorkstationRepo определяет интерфейс для работы с хранилищем рабочих станций.
type WorkstationRepo interface {
	Create(ctx context.Context, tx *gorm.DB, workstation *models.Workstation) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Workstation, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Workstation, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Workstation, error)
	FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*models.Workstation, error)
	FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Workstation, error)
}

// workstationRepo реализует интерфейс WorkstationRepo.
type workstationRepo struct {
	db *gorm.DB
}

// NewWorkstationRepo создает новый экземпляр репозитория.
func NewWorkstationRepo(db *gorm.DB) WorkstationRepo {
	return &workstationRepo{db: db}
}

func (r *workstationRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *workstationRepo) Create(ctx context.Context, tx *gorm.DB, workstation *models.Workstation) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(workstation).Error
}

func (r *workstationRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Workstation{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" рабочей станции по ее ServiceDesk UUID.
func (r *workstationRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Workstation{})
	return res.RowsAffected > 0, res.Error
}
func (r *workstationRepo) GetByUUID(ctx context.Context, uuid string) (*models.Workstation, error) {
	var workstation models.Workstation
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&workstation).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &workstation, err
}

func (r *workstationRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Workstation, error) {
	var workstation models.Workstation
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&workstation).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &workstation, err
}

func (r *workstationRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error) {
	var workstations []*models.Workstation
	if err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&workstations).Error; err != nil {
		return nil, err
	}
	workstationMap := make(map[string]*models.Workstation, len(workstations))
	for _, ws := range workstations {
		if ws.ServiceDeskUUID != nil {
			workstationMap[*ws.ServiceDeskUUID] = ws
		}
	}
	return workstationMap, nil
}

func (r *workstationRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.Workstation, error) {
	var workstations []models.Workstation
	err := r.db.WithContext(ctx).
		Where("device_name ILIKE ? OR description ILIKE ?", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&workstations).Error
	return workstations, err
}

// FindByRemoteIDs ищет рабочую станцию по любому из ID удаленного доступа.
func (r *workstationRepo) FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*models.Workstation, error) {
	var ws models.Workstation
	query := r.db.WithContext(ctx)

	// Динамически строим запрос, добавляя условия только для валидных ID
	var conditions []string
	var values []interface{}

	if tv != "" && tv != "None" {
		conditions = append(conditions, "teamviewer = ?")
		values = append(values, tv)
	}
	if ad != "" && ad != "None" {
		conditions = append(conditions, "anydesk = ?")
		values = append(values, ad)
	}
	if lm != "" && lm != "None" {
		conditions = append(conditions, "litemanager = ?")
		values = append(values, lm)
	}

	// Если ни одного валидного ID не предоставлено, ничего не ищем
	if len(conditions) == 0 {
		return nil, nil
	}

	// Объединяем условия через OR
	query = query.Where(strings.Join(conditions, " OR "), values...)

	// Ищем самую свежую запись, если их несколько
	err := query.Order("last_modified_date DESC").First(&ws).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &ws, err
}

// FindByOwnerUUIDs находит все рабочие станции, принадлежащие указанным владельцам.
func (r *workstationRepo) FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Workstation, error) {
	if len(ownerUUIDs) == 0 {
		return nil, nil
	}
	var workstations []models.Workstation
	err := r.db.WithContext(ctx).Where("owner_service_desk_uuid IN ?", ownerUUIDs).Find(&workstations).Error
	return workstations, err
}

go `
===== END workstation_repo.go =====

internal/seeder/mock_client.go
===== START mock_client.go =====
go `
// internal/seeder/mock_client.go
package seeder

import (
	"context"
	"encoding/json"
	"etalon-server/internal/models"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// MockServiceDeskClient имитирует клиент ServiceDesk для чтения данных из локальных файлов.
type MockServiceDeskClient struct {
	logger   *zap.Logger
	dataPath string
}

// NewMockServiceDeskClient создает новый мок-клиент.
func NewMockServiceDeskClient(logger *zap.Logger, dataPath string) services.ServiceDeskClient {
	return &MockServiceDeskClient{
		logger:   logger,
		dataPath: dataPath,
	}
}

// FetchEntityList читает список сущностей из JSON-файла.
func (m *MockServiceDeskClient) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	var fileName string
	switch metaClass {
	case "ou$company":
		fileName = "companies.json"
	case "objectBase$Server":
		fileName = "servers.json"
	case "objectBase$Workstation":
		fileName = "workstations.json"
	case "objectBase$FR":
		fileName = "fiscal_registers.json"
	default:
		return nil, fmt.Errorf("неизвестный metaClass для мок-клиента: %s", metaClass)
	}

	fullPath := filepath.Join(m.dataPath, fileName)
	m.logger.Info("Чтение мок-данных из файла", zap.String("path", fullPath))

	file, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл с мок-данными %s: %w", fullPath, err)
	}

	var responseList []map[string]interface{}
	if err := json.Unmarshal(file, &responseList); err != nil {
		return nil, fmt.Errorf("не удалось декодировать JSON из файла %s: %w", fullPath, err)
	}

	return responseList, nil
}

// FetchAgreementDetails для мок-клиента всегда возвращает активный контракт.
// Это упрощает логику наполнения.
func (m *MockServiceDeskClient) FetchAgreementDetails(ctx context.Context, agreementUUID string) (*services.AgreementDetailsDTO, error) {
	return &services.AgreementDetailsDTO{
		State: "active",
	}, nil
}

// FetchEntityDetails не используется в режиме наполнения, но является частью интерфейса.
func (m *MockServiceDeskClient) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("метод FetchEntityDetails не реализован для мок-клиента")
}

// DataToCompanyForSeeder - это специальная версия маппера для seeder'а.
func DataToCompanyForSeeder(ctx context.Context, data map[string]interface{}, sdClient services.ServiceDeskClient, logger *zap.Logger) (*models.Company, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("в данных компании отсутствует UUID")
	}

	company := &models.Company{}
	company.ServiceDeskUUID = &uuid
	company.MetaClass = "ou$company"

	if title, ok := data["title"].(string); ok {
		company.Title = &title
	}
	if address, ok := data["adress"].(string); ok {
		company.Address = &address
	}
	if addName, ok := data["additionalName"].(string); ok {
		if addName != "" {
			company.AdditionalName = &addName
		}
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		company.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	if parent, ok := data["parent"].(map[string]interface{}); ok && parent != nil {
		if parentUUID, p_ok := parent["UUID"].(string); p_ok {
			company.ParentServiceDeskUUID = &parentUUID
		}
	}

	// ИЗМЕНЕНИЕ: Логика адаптирована под новый интерфейс
	active := false
	if agreements, ok := data["recipientAgreements"].([]interface{}); ok {
		for _, agr := range agreements {
			if agrMap, agrOk := agr.(map[string]interface{}); agrOk {
				if metaClass, mcOk := agrMap["metaClass"].(string); mcOk && metaClass == "agreement$agreement" {
					if agrUUID, uuidOk := agrMap["UUID"].(string); uuidOk {
						details, err := sdClient.FetchAgreementDetails(ctx, agrUUID)
						if err != nil {
							logger.Warn("Ошибка при проверке статуса договора (мок)", zap.String("agreementUUID", agrUUID), zap.Error(err))
							continue
						}
						if details.State == "active" {
							active = true
							break
						}
					}
				}
			}
		}
	}
	company.ActiveContract = &active

	return company, nil
}

go `
===== END mock_client.go =====

internal/seeder/seeder.go
===== START seeder.go =====
go `
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
}

// NewSeeder создает новый экземпляр Seeder.
func NewSeeder(
	logger *zap.Logger,
	db *gorm.DB,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) *Seeder {
	return &Seeder{
		logger:          logger,
		db:              db,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// SeedDatabase выполняет полный цикл наполнения: очистка и заполнение всех сущностей.
func (s *Seeder) SeedDatabase(sdClient services.ServiceDeskClient) error {
	s.logger.Info("Начало процесса наполнения базы данных...")

	s.logger.Info("Шаг 1: Очистка таблиц...")
	if err := s.clearDatabase(); err != nil {
		s.logger.Error("Не удалось очистить базу данных", zap.Error(err))
		return err
	}
	s.logger.Info("База данных успешно очищена.")

	ctx := context.Background()

	s.logger.Info("Шаг 2: Загрузка и вставка Компаний...")
	s.seedCompanies(ctx, sdClient)

	s.logger.Info("Получение UUID всех загруженных компаний для проверки связей...")
	companyUUIDs, err := s.getAllCompanyUUIDs()
	if err != nil {
		s.logger.Error("Не удалось получить UUID компаний из БД, дальнейшее наполнение невозможно", zap.Error(err))
		return err
	}
	s.logger.Info("UUID компаний получены", zap.Int("count", len(companyUUIDs)))

	s.logger.Info("Шаг 3: Загрузка и вставка Серверов...")
	s.seedServers(ctx, sdClient, companyUUIDs)

	s.logger.Info("Шаг 4: Загрузка и вставка Рабочих станций...")
	s.seedWorkstations(ctx, sdClient, companyUUIDs)

	s.logger.Info("Шаг 5: Загрузка и вставка Фискальных регистраторов...")
	s.seedFiscalRegisters(ctx, sdClient, companyUUIDs)

	s.logger.Info("Процесс наполнения базы данных завершен.")
	return nil
}

// clearDatabase удаляет все данные из таблиц в правильном порядке.
// ИСПОЛЬЗУЕМ DELETE вместо TRUNCATE для большей надежности и независимости от прав БД.
func (s *Seeder) clearDatabase() error {
	// Порядок важен для соблюдения foreign key constraints.
	// Сначала удаляем из служебных таблиц, затем из зависимых, в конце - из основных.
	tables := []string{
		"reconciliation_tasks",
		"agent_files",
		"fiscal_registers",
		"workstations",
		"servers",
		"companies",
	}

	for _, table := range tables {
		s.logger.Info("Очистка таблицы...", zap.String("table", table))
		// Используем Exec с "DELETE FROM ..." - это стандартный SQL, который работает всегда,
		// в отличие от TRUNCATE, который может быть ограничен правами.
		// Where("1 = 1") нужен, чтобы GORM сформировал запрос DELETE без условий.
		if err := s.db.Exec(fmt.Sprintf("DELETE FROM %s", table)).Error; err != nil {
			// Если произошла ошибка, возможно, таблицы еще не существует (первый запуск).
			// Мы можем проигнорировать ошибку "table does not exist", но для простоты пока оставим так.
			return fmt.Errorf("ошибка при очистке таблицы %s: %w", table, err)
		}
	}
	return nil
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

// seedCompanies загружает и сохраняет компании в два прохода для соблюдения внешних ключей.
func (s *Seeder) seedCompanies(ctx context.Context, sdClient services.ServiceDeskClient) {
	remoteList, err := sdClient.FetchEntityList(ctx, "ou$company", true)
	if err != nil {
		s.logger.Error("Не удалось получить список компаний из мок-данных", zap.Error(err))
		return
	}

	var companiesWithParent, companiesWithoutParent []models.Company

	// 1. Разделяем все компании на два списка: с родителями и без.
	for _, data := range remoteList {
		company, err := DataToCompanyForSeeder(ctx, data, sdClient, s.logger)
		if err != nil {
			uuid, _ := data["UUID"].(string)
			s.logger.Warn("Пропуск компании из-за ошибки маппинга", zap.String("uuid", uuid), zap.Error(err))
			continue
		}
		if company.ParentServiceDeskUUID != nil && *company.ParentServiceDeskUUID != "" {
			companiesWithParent = append(companiesWithParent, *company)
		} else {
			companiesWithoutParent = append(companiesWithoutParent, *company)
		}
	}

	// 2. Первый проход: вставляем компании без родителей.
	if len(companiesWithoutParent) > 0 {
		if err := s.db.CreateInBatches(companiesWithoutParent, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке компаний без родителей", zap.Error(err))
			// Если корневые компании не вставились, продолжать нет смысла.
			return
		} else {
			s.logger.Info("Успешно вставлено компаний без родителей", zap.Int("count", len(companiesWithoutParent)))
		}
	}

	// 3. Второй проход: вставляем компании с родителями.
	if len(companiesWithParent) > 0 {
		if err := s.db.CreateInBatches(companiesWithParent, batchSize).Error; err != nil {
			s.logger.Error("Ошибка при пакетной вставке компаний с родителями", zap.Error(err))
		} else {
			s.logger.Info("Успешно вставлено компаний с родителями", zap.Int("count", len(companiesWithParent)))
		}
	}
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

go `
===== END seeder.go =====

internal/services/agent_service.go
===== START agent_service.go =====
go `
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"fmt"
	"regexp"
	"strings"
	"time"

	"unicode"

	"github.com/asaskevich/govalidator"
	"go.uber.org/zap"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrAgentNotFound      = errors.New("агент не найден")
	ErrAgentAlreadyExists = errors.New("агент с таким UUID уже существует")
	ErrOwnerNotDetermined = errors.New("не удалось определить владельца для агента")
)

// AgentService определяет интерфейс для бизнес-логики управления агентами.
type AgentService interface {
	RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error)
	ProcessData(ctx context.Context, agentUUID string, data *api.AgentDataDTO) error
	GetAgentConfig(ctx context.Context, uuid string) (*api.AgentConfigDTO, error)
}

type agentServiceImpl struct {
	logger        *zap.Logger
	agentRepo     repositories.AgentRepo
	companyRepo   repositories.CompanyRepo
	reconcilerSvc ReconcilerService
	db            *gorm.DB // Для транзакций и создания задач
}

// NewAgentService создает новый экземпляр сервиса агентов.
func NewAgentService(logger *zap.Logger, agentRepo repositories.AgentRepo, companyRepo repositories.CompanyRepo, reconcilerSvc ReconcilerService, db *gorm.DB) AgentService {
	return &agentServiceImpl{
		logger:        logger,
		agentRepo:     agentRepo,
		companyRepo:   companyRepo,
		reconcilerSvc: reconcilerSvc,
		db:            db,
	}
}

// RegisterAgent обрабатывает запрос на регистрацию нового агента.
// RegisterAgent обрабатывает запрос на регистрацию нового агента.
func (s *agentServiceImpl) RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error) {
	// 1. Проверяем, не существует ли уже такой агент
	existingAgent, err := s.agentRepo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		return nil, fmt.Errorf("ошибка проверки существования агента: %w", err)
	}
	if existingAgent != nil {
		return nil, ErrAgentAlreadyExists
	}

	// 2. Вызываем ReconcilerService для определения владельца
	// ИСПРАВЛЕНИЕ: Сигнатура вызова изменена (убраны votes).
	ownerUUID, _, _, err := s.reconcilerSvc.ProcessAgentData(ctx, &req.InitialData)
	if err != nil {
		s.logger.Error("Ошибка при обработке данных для определения владельца", zap.Error(err))
	}

	agent := &models.Agent{
		UUID:          req.AgentUUID,
		Hostname:      req.Hostname,
		Version:       req.AgentVersion,
		LastHeartbeat: time.Now(),
		Type:          "workstation", // Пока хардкод, в будущем можно определять
	}

	// 3. Логика на основе результата определения владельца
	if ownerUUID == "" {
		s.logger.Warn("Владелец для нового агента не определен. Создание задачи.", zap.String("agent_uuid", req.AgentUUID))
		agent.Status = models.StatusPendingOwner
		if err := s.createTaskForUndefinedOwner(ctx, req); err != nil {
			return nil, err
		}
	} else {
		s.logger.Info("Владелец для нового агента определен", zap.String("agent_uuid", req.AgentUUID), zap.String("owner_uuid", ownerUUID))
		agent.Status = models.StatusPendingZabbix
		agent.OwnerServiceDeskUUID = ownerUUID

		// Генерируем предварительное имя для Zabbix
		zabbixHostname, err := s.generateZabbixHostname(ctx, ownerUUID, req.InitialData.Hostname)
		if err != nil {
			return nil, fmt.Errorf("ошибка генерации имени хоста Zabbix: %w", err)
		}
		agent.ZabbixHostname = zabbixHostname
	}

	// 4. Сохраняем агента в БД
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, fmt.Errorf("не удалось создать агента в БД: %w", err)
	}

	return agent, nil
}

// ProcessData обрабатывает данные от уже зарегистрированного агента.
func (s *agentServiceImpl) ProcessData(ctx context.Context, agentUUID string, data *api.AgentDataDTO) error {
	agent, err := s.agentRepo.GetByUUID(ctx, agentUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения агента: %w", err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	// Обновляем heartbeat
	agent.LastHeartbeat = time.Now()
	if data.AgentVersion != "" {
		agent.Version = data.AgentVersion
	}
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		s.logger.Error("Не удалось обновить heartbeat агента", zap.String("uuid", agentUUID), zap.Error(err))
		// Не возвращаем ошибку, чтобы сверка все равно прошла
	}

	// Запускаем сверку данных
	_, _, _, err = s.reconcilerSvc.ProcessAgentData(ctx, data)
	if err != nil {
		return fmt.Errorf("ошибка сверки данных агента: %w", err)
	}

	return nil
}

// GetAgentConfig возвращает конфигурацию для агента, если он активен.
func (s *agentServiceImpl) GetAgentConfig(ctx context.Context, uuid string) (*api.AgentConfigDTO, error) {
	agent, err := s.agentRepo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения агента: %w", err)
	}
	if agent == nil || agent.Status != models.StatusActive {
		// Если агент не найден или его регистрация не завершена, возвращаем ошибку
		return nil, ErrAgentNotFound
	}

	// Распарсим JSON из БД в DTO
	var configDTO api.AgentConfigDTO
	if agent.Config != nil {
		if err := json.Unmarshal(agent.Config, &configDTO); err != nil {
			return nil, fmt.Errorf("не удалось распарсить конфигурацию агента из БД: %w", err)
		}
	} else {
		// Этого быть не должно, если статус active, но на всякий случай
		return nil, errors.New("у активного агента отсутствует конфигурация")
	}

	return &configDTO, nil
}

// createTaskForUndefinedOwner создает задачу для администратора.
func (s *agentServiceImpl) createTaskForUndefinedOwner(ctx context.Context, req *api.RegistrationRequestDTO) error {
	details, _ := json.Marshal(req)
	task := models.ReconciliationTask{
		TaskType:   "agent_owner_required",
		EntityType: "Agent",
		EntityUUID: req.AgentUUID,
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    fmt.Sprintf("Требуется вручную определить и привязать владельца для нового агента с хостом %s.", req.Hostname),
	}
	return s.db.WithContext(ctx).Create(&task).Error
}

// generateZabbixHostname создает имя хоста по формату {$COMPANY_NAME_ENG_SHORT}-{$DeviceNameFromSD}-{$INNER_COMPANY_ID}
func (s *agentServiceImpl) generateZabbixHostname(ctx context.Context, ownerUUID, agentHostname string) (string, error) {
	company, err := s.companyRepo.GetByUUID(ctx, ownerUUID)
	if err != nil || company == nil {
		return "", fmt.Errorf("не удалось найти компанию-владельца по UUID %s", ownerUUID)
	}

	// 1. $COMPANY_NAME_ENG_SHORT
	companyShortName := s.transliterate(*company.Title)

	// 2. $DeviceNameFromSD
	// На этапе регистрации у нас еще нет точной привязки к Workstation, используем hostname агента
	deviceName := agentHostname
	if govalidator.IsDNSName(deviceName) {
		deviceName = strings.Split(deviceName, ".")[0]
	}
	deviceName = strings.ToUpper(deviceName)

	// 3. $INNER_COMPANY_ID
	count, err := s.agentRepo.CountByOwnerUUID(ctx, ownerUUID)
	if err != nil {
		return "", fmt.Errorf("не удалось посчитать агентов для компании %s: %w", ownerUUID, err)
	}
	innerID := fmt.Sprintf("%02d", count+1) // +1, так как текущий агент еще не сохранен

	return fmt.Sprintf("%s-%s-%s", companyShortName, deviceName, innerID), nil
}

// transliterate преобразует кириллический текст в латиницу.
func (s *agentServiceImpl) transliterate(text string) string {
	// Простая транслитерация
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, text)
	result = strings.ToLower(result)

	// Заменяем специфичные русские буквы и символы
	var replacements = map[string]string{
		" ": "-", "ъ": "", "ь": "", "ы": "y", "і": "i", "ї": "i", "є": "e",
		"а": "a", "б": "b", "в": "v", "г": "g", "д": "d", "е": "e", "ё": "e",
		"ж": "zh", "з": "z", "и": "i", "й": "y", "к": "k", "л": "l", "м": "m",
		"н": "n", "о": "o", "п": "p", "р": "r", "с": "s", "т": "t", "у": "u",
		"ф": "f", "х": "h", "ц": "c", "ч": "ch", "ш": "sh", "щ": "sch",
		"ю": "yu", "я": "ya",
	}

	for rus, lat := range replacements {
		result = strings.ReplaceAll(result, rus, lat)
	}

	// Удаляем все неалфавитно-цифровые символы, кроме дефиса
	reg, err := regexp.Compile("[^a-z0-9-]+")
	if err != nil {
		s.logger.Error("Ошибка компиляции regex для транслитерации", zap.Error(err))
		return "unknown"
	}
	return reg.ReplaceAllString(result, "")
}

go `
===== END agent_service.go =====

internal/services/cleanup_service.go
===== START cleanup_service.go =====
go `
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"fmt"
	"sort"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// CleanupService отвечает за задачи по очистке данных в базе.
type CleanupService interface {
	// CleanupFRDuplicates находит и выполняет "мягкое удаление" дубликатов фискальных регистраторов.
	CleanupFRDuplicates(ctx context.Context)
	CleanupServerDuplicatesAndJunk(ctx context.Context)
}

type cleanupServiceImpl struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewCleanupService создает новый экземпляр сервиса очистки.
func NewCleanupService(db *gorm.DB, logger *zap.Logger) CleanupService {
	return &cleanupServiceImpl{
		db:     db,
		logger: logger,
	}
}

// CleanupFRDuplicates реализует основную логику поиска и удаления дублей.
func (s *cleanupServiceImpl) CleanupFRDuplicates(ctx context.Context) {
	log := s.logger.With(zap.String("service", "CleanupService"))
	log.Info("Запуск очистки дубликатов фискальных регистраторов...")

	// Поля, по которым ищем дубликаты
	fields := []string{"fr_serial_number", "rn_kkt"}
	totalDeleted := 0

	for _, field := range fields {
		log.Info("Поиск дубликатов по полю", zap.String("field", field))
		deletedCount, err := s.cleanupByField(ctx, field)
		if err != nil {
			log.Error("Ошибка при очистке дубликатов по полю", zap.String("field", field), zap.Error(err))
			continue
		}
		if deletedCount > 0 {
			log.Info("Удалено дубликатов по полю", zap.String("field", field), zap.Int("count", deletedCount))
			totalDeleted += deletedCount
		}
	}

	log.Info("Очистка дубликатов фискальных регистраторов завершена.", zap.Int("total_deleted", totalDeleted))
}

// cleanupByField находит и удаляет дубликаты для одного конкретного поля.
func (s *cleanupServiceImpl) cleanupByField(ctx context.Context, field string) (int, error) {
	var deletedCount int

	// 1. Находим все значения, которые встречаются более одного раза.
	var duplicateValues []struct {
		Value string
	}
	err := s.db.WithContext(ctx).Model(&models.FiscalRegister{}).
		Select(fmt.Sprintf("%s as value", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&duplicateValues).Error
	if err != nil {
		return 0, err
	}

	if len(duplicateValues) == 0 {
		return 0, nil
	}

	// 2. Для каждого дублирующегося значения...
	for _, item := range duplicateValues {
		var duplicates []models.FiscalRegister
		// ...получаем все записи с этим значением.
		err := s.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", field), item.Value).Find(&duplicates).Error
		if err != nil {
			s.logger.Error("Не удалось получить группу дубликатов", zap.String("field", field), zap.String("value", item.Value), zap.Error(err))
			continue
		}

		// 3. Сортируем их, чтобы самая свежая запись была первой.
		sort.Slice(duplicates, func(i, j int) bool {
			// Если дата nil, считаем ее "старой"
			if duplicates[i].LastModifiedDate == nil {
				return false
			}
			if duplicates[j].LastModifiedDate == nil {
				return true
			}
			return duplicates[i].LastModifiedDate.After(*duplicates[j].LastModifiedDate)
		})

		// 4. Удаляем все записи, кроме первой (самой свежей).
		idsToDelete := make([]string, 0, len(duplicates)-1)
		for _, fr := range duplicates[1:] {
			idsToDelete = append(idsToDelete, fr.ID)
		}

		if len(idsToDelete) > 0 {
			res := s.db.WithContext(ctx).Delete(&models.FiscalRegister{}, "id IN ?", idsToDelete)
			if res.Error != nil {
				s.logger.Error("Ошибка при 'мягком удалении' дубликатов", zap.Strings("ids", idsToDelete), zap.Error(res.Error))
			} else {
				deletedCount += int(res.RowsAffected)
			}
		}
	}

	return deletedCount, nil
}

// CleanupServerDuplicatesAndJunk ищет и удаляет дубликаты по IP и "мусорные" записи серверов.
func (s *cleanupServiceImpl) CleanupServerDuplicatesAndJunk(ctx context.Context) {
	log := s.logger.With(zap.String("service", "CleanupService"))
	log.Info("Запуск очистки дубликатов и мусорных записей серверов...")

	// Этап 1: Очистка дубликатов по полю IP
	duplicatesDeleted, err := s.cleanupServerDuplicates(ctx, log)
	if err != nil {
		log.Error("Ошибка при очистке дубликатов серверов", zap.Error(err))
	} else if duplicatesDeleted > 0 {
		log.Info("Удалено дубликатов серверов", zap.Int("count", duplicatesDeleted))
	}

	// Этап 2: Очистка "мусорных" записей
	junkDeleted, err := s.cleanupJunkServers(ctx, log)
	if err != nil {
		log.Error("Ошибка при очистке мусорных записей серверов", zap.Error(err))
	} else if junkDeleted > 0 {
		log.Info("Удалено мусорных записей серверов", zap.Int("count", junkDeleted))
	}

	log.Info("Очистка серверов завершена.", zap.Int("total_deleted", duplicatesDeleted+junkDeleted))
}

// cleanupServerDuplicates находит и удаляет дубликаты серверов по полю `ip`.
func (s *cleanupServiceImpl) cleanupServerDuplicates(ctx context.Context, log *zap.Logger) (int, error) {
	var deletedCount int

	// 1. Находим все IP, которые встречаются более одного раза.
	var duplicateValues []struct{ Value string }
	err := s.db.WithContext(ctx).Model(&models.Server{}).
		Select("ip as value").
		Where("ip IS NOT NULL AND ip != ''").
		Group("ip").
		Having("count(*) > 1").
		Find(&duplicateValues).Error
	if err != nil {
		return 0, err
	}

	if len(duplicateValues) == 0 {
		return 0, nil
	}

	// 2. Для каждого дублирующегося IP...
	for _, item := range duplicateValues {
		var duplicates []models.Server
		if err := s.db.WithContext(ctx).Where("ip = ?", item.Value).Find(&duplicates).Error; err != nil {
			log.Error("Не удалось получить группу дубликатов серверов", zap.String("ip", item.Value), zap.Error(err))
			continue
		}

		// 3. Сортируем, чтобы самая свежая запись была первой.
		sort.Slice(duplicates, func(i, j int) bool {
			if duplicates[i].LastModifiedDate == nil {
				return false
			}
			if duplicates[j].LastModifiedDate == nil {
				return true
			}
			return duplicates[i].LastModifiedDate.After(*duplicates[j].LastModifiedDate)
		})

		mainRecord := duplicates[0]
		recordsToDelete := duplicates[1:]
		idsToDelete := make([]string, 0, len(recordsToDelete))
		for _, rec := range recordsToDelete {
			idsToDelete = append(idsToDelete, rec.ID)
		}

		// 4. "Мягко" удаляем все, кроме основной записи.
		if len(idsToDelete) > 0 {
			res := s.db.WithContext(ctx).Delete(&models.Server{}, "id IN ?", idsToDelete)
			if res.Error == nil {
				deletedCount += int(res.RowsAffected)
				// 5. Создаем задачи на удаление в ServiceDesk.
				for _, rec := range recordsToDelete {
					s.createServerCleanupTask(ctx, rec, mainRecord, "duplicate")
				}
			}
		}
	}
	return deletedCount, nil
}

// cleanupJunkServers находит и удаляет "мусорные" серверы.
func (s *cleanupServiceImpl) cleanupJunkServers(ctx context.Context, log *zap.Logger) (int, error) {
	var junkServers []models.Server
	// Ищем записи, где все ключевые поля пустые.
	err := s.db.WithContext(ctx).Where(
		"(ip IS NULL OR ip = '') AND " +
			"(teamviewer IS NULL OR teamviewer = '') AND " +
			"(anydesk IS NULL OR anydesk = '') AND " +
			"(litemanager IS NULL OR litemanager = '') AND " +
			"(rdp IS NULL OR rdp = '') AND " +
			"(description IS NULL OR description = '')",
	).Find(&junkServers).Error

	if err != nil {
		return 0, err
	}
	if len(junkServers) == 0 {
		return 0, nil
	}

	idsToDelete := make([]string, 0, len(junkServers))
	for _, server := range junkServers {
		idsToDelete = append(idsToDelete, server.ID)
	}

	res := s.db.WithContext(ctx).Delete(&models.Server{}, "id IN ?", idsToDelete)
	if res.Error == nil {
		for _, server := range junkServers {
			s.createServerCleanupTask(ctx, server, models.Server{}, "junk")
		}
		return int(res.RowsAffected), nil
	}

	return 0, res.Error
}

// createServerCleanupTask создает задачу на удаление сущности из ServiceDesk.
func (s *cleanupServiceImpl) createServerCleanupTask(ctx context.Context, serverToDelete, mainRecord models.Server, reason string) {
	var comment string
	detailsMap := map[string]string{
		"deletedServiceDeskUUID": utils.SafeStringDereference(serverToDelete.ServiceDeskUUID),
		"deletedInternalID":      serverToDelete.ID,
	}

	if reason == "duplicate" {
		comment = fmt.Sprintf("Задача на удаление дубликата из ServiceDesk. Эта запись является дубликатом записи с UUID %s по полю 'ip'.", utils.SafeStringDereference(mainRecord.ServiceDeskUUID))
		detailsMap["mainRecordServiceDeskUUID"] = utils.SafeStringDereference(mainRecord.ServiceDeskUUID)
	} else {
		comment = "Задача на удаление 'мусорной' записи из ServiceDesk. Запись не содержит IP, данных удаленного доступа или описания."
	}

	details, _ := json.Marshal(detailsMap)

	task := models.ReconciliationTask{
		TaskType:   "delete_from_servicedesk",
		EntityType: "Server",
		EntityUUID: utils.SafeStringDereference(serverToDelete.ServiceDeskUUID),
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    comment,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		s.logger.Error("Не удалось создать задачу на удаление из SD", zap.String("uuid", task.EntityUUID), zap.Error(err))
	}
}

go `
===== END cleanup_service.go =====

internal/services/ftp_client.go
===== START ftp_client.go =====
go `
package services

import (
	"bytes"
	"etalon-server/internal/config"
	"fmt"
	"io"
	"time"

	"github.com/jlaffaye/ftp"
	"go.uber.org/zap"
)

// FTPClient определяет интерфейс для работы с FTP-сервером.
type FTPClient interface {
	ListFiles(path string) ([]*ftp.Entry, error)
	DownloadFile(path string) ([]byte, error)
}

type ftpClientImpl struct {
	cfg    *config.Config
	logger *zap.Logger
}

// NewFTPClient создает новый клиент для FTP.
func NewFTPClient(cfg *config.Config, logger *zap.Logger) FTPClient {
	return &ftpClientImpl{
		cfg:    cfg,
		logger: logger,
	}
}

// getConn устанавливает соединение с FTP сервером.
func (f *ftpClientImpl) getConn() (*ftp.ServerConn, error) {
	addr := fmt.Sprintf("%s:%s", f.cfg.FTPHost, f.cfg.FTPPort)
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(15*time.Second))
	if err != nil {
		f.logger.Error("Не удалось подключиться к FTP", zap.String("addr", addr), zap.Error(err))
		return nil, err
	}

	err = c.Login(f.cfg.FTPUser, f.cfg.FTPPassword)
	if err != nil {
		c.Quit()
		f.logger.Error("Не удалось авторизоваться на FTP", zap.String("user", f.cfg.FTPUser), zap.Error(err))
		return nil, err
	}

	return c, nil
}

// ListFiles получает список файлов и их атрибутов из указанной директории.
func (f *ftpClientImpl) ListFiles(path string) ([]*ftp.Entry, error) {
	c, err := f.getConn()
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	entries, err := c.List(path)
	if err != nil {
		f.logger.Error("Не удалось получить список файлов с FTP", zap.String("path", path), zap.Error(err))
		return nil, err
	}

	return entries, nil
}

// DownloadFile скачивает файл с FTP и возвращает его содержимое в виде []byte.
func (f *ftpClientImpl) DownloadFile(path string) ([]byte, error) {
	c, err := f.getConn()
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	r, err := c.Retr(path)
	if err != nil {
		f.logger.Error("Не удалось начать скачивание файла", zap.String("path", path), zap.Error(err))
		return nil, err
	}
	defer r.Close()

	buf, err := io.ReadAll(r)
	if err != nil {
		f.logger.Error("Ошибка во время чтения скачиваемого файла", zap.String("path", path), zap.Error(err))
		return nil, err
	}

	// Для отладки можно использовать bytes.NewReader(buf) если нужно будет передать io.Reader
	_ = bytes.NewReader(buf)

	return buf, nil
}

go `
===== END ftp_client.go =====

internal/services/mappers.go
===== START mappers.go =====
go `
// internal/services/mappers.go
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"gorm.io/datatypes"
)

// ContractInfo represents the structure for storing aggregated contract details in JSON.
type ContractInfo struct {
	Services          []string `json:"services"`
	OtherRecipients   []string `json:"other_recipients_uuids"`
	ActiveContractIDs []string `json:"active_contract_ids"`
}

// DataToCompany преобразует мапу от ServiceDesk в модель Company.
func DataToCompany(ctx context.Context, data map[string]interface{}, sdClient ServiceDeskClient, logger *zap.Logger) (*models.Company, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("company data missing UUID")
	}

	company := &models.Company{}
	company.ServiceDeskUUID = &uuid
	company.MetaClass = "ou$company"

	if title, ok := data["title"].(string); ok {
		company.Title = &title
	}
	if address, ok := data["adress"].(string); ok {
		company.Address = &address
	}
	if addName, ok := data["additionalName"].(string); ok {
		company.AdditionalName = &addName
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		company.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	// Обработка parent
	if parent, ok := data["parent"].(map[string]interface{}); ok {
		if parentUUID, p_ok := parent["UUID"].(string); p_ok {
			company.ParentServiceDeskUUID = &parentUUID
		}
	}

	// Обработка active_contract
	isActiveContract := false
	contractInfo := ContractInfo{
		Services:          []string{},
		OtherRecipients:   []string{},
		ActiveContractIDs: []string{},
	}
	serviceSet := make(map[string]struct{})
	recipientSet := make(map[string]struct{})

	if agreements, ok := data["recipientAgreements"].([]interface{}); ok {
		for _, agr := range agreements {
			agrMap, agrOk := agr.(map[string]interface{})
			if !agrOk {
				continue
			}

			// Пропускаем все контракты, кроме 'agreement$agreement'
			metaClass, _ := agrMap["metaClass"].(string)
			if metaClass != "agreement$agreement" {
				continue
			}

			agrUUID, uuidOk := agrMap["UUID"].(string)
			if !uuidOk {
				continue
			}

			// Получаем детали контракта (с использованием кэша)
			details, err := sdClient.FetchAgreementDetails(ctx, agrUUID)
			if err != nil {
				logger.Warn("Не удалось получить детали контракта", zap.String("agreementUUID", agrUUID), zap.Error(err))
				continue
			}

			if details.State == "active" {
				isActiveContract = true
				contractInfo.ActiveContractIDs = append(contractInfo.ActiveContractIDs, agrUUID)

				// Собираем уникальные сервисы
				for _, service := range details.Services {
					if _, exists := serviceSet[service.Title]; !exists {
						serviceSet[service.Title] = struct{}{}
						contractInfo.Services = append(contractInfo.Services, service.Title)
					}
				}

				// Собираем уникальных получателей, исключая текущую компанию
				for _, recipient := range details.RecipientsOU {
					if recipient.UUID != uuid {
						if _, exists := recipientSet[recipient.UUID]; !exists {
							recipientSet[recipient.UUID] = struct{}{}
							contractInfo.OtherRecipients = append(contractInfo.OtherRecipients, recipient.UUID)
						}
					}
				}
			}
		}
	}

	company.ActiveContract = &isActiveContract
	contractInfoJSON, err := json.Marshal(contractInfo)
	if err != nil {
		logger.Error("Не удалось сериализовать информацию о контракте в JSON", zap.String("companyUUID", uuid), zap.Error(err))
	} else {
		company.ContractInfo = datatypes.JSON(contractInfoJSON)
	}
	// --- КОНЕЦ НОВОЙ ЛОГИКИ ---

	return company, nil
}

// DataToServer преобразует мапу от ServiceDesk в модель Server.
func DataToServer(data map[string]interface{}) (*models.Server, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("server data missing UUID")
	}

	ownerUUID := getOwnerUUID(data)
	if ownerUUID == "" {
		return nil, fmt.Errorf("server with uuid %s has no owner, skipping", uuid)
	}

	server := &models.Server{}
	server.ServiceDeskUUID = &uuid
	server.OwnerServiceDeskUUID = &ownerUUID
	server.MetaClass = "objectBase$Server"

	// 1. Извлекаем все "сырые" строковые значения из данных ServiceDesk.
	rawUniqueID, _ := data["UniqueID"].(string)
	rawTeamviewer, _ := data["Teamviewer"].(string)
	rawRDP, _ := data["RDP"].(string)
	rawAnydesk, _ := data["AnyDesk"].(string)
	rawIP, _ := data["IP"].(string)
	rawDeviceName, _ := data["DeviceName"].(string)
	rawIikoVersion, _ := data["iikoVersion"].(string)
	rawDescription, _ := data["description"].(string)
	rawNameForClient, _ := data["nameforclient"].(string)
	rawLitemanager, _ := data["litemanagerID"].(string)

	// 2. Валидируем и заполняем основные поля модели.
	server.UniqueID = validators.ValidateUniqueID(rawUniqueID)
	server.Teamviewer = validators.ValidateRemoteAccessID(rawTeamviewer)
	server.Anydesk = validators.ValidateRemoteAccessID(rawAnydesk)
	server.IP = validators.ValidateIPAddress(rawIP)

	// Поле RDP сохраняется "как есть", без валидации.
	if rawRDP != "" {
		server.RDP = &rawRDP
	}

	if rawDeviceName != "" {
		server.DeviceName = &rawDeviceName
	}
	if rawIikoVersion != "" {
		server.ServerVersion = &rawIikoVersion
	}

	// 3. Собираем все извлеченные "сырые" данные в единое поле Description.
	var descriptionParts []string
	if rawNameForClient != "" {
		descriptionParts = append(descriptionParts, "Имя для клиента: "+rawNameForClient)
	}
	if rawDescription != "" {
		descriptionParts = append(descriptionParts, "Описание: "+rawDescription)
	}
	if rawUniqueID != "" {
		descriptionParts = append(descriptionParts, "UniqueID: "+rawUniqueID)
	}
	if rawTeamviewer != "" {
		descriptionParts = append(descriptionParts, "Teamviewer: "+rawTeamviewer)
	}
	if rawAnydesk != "" {
		descriptionParts = append(descriptionParts, "AnyDesk: "+rawAnydesk)
	}
	if rawRDP != "" {
		descriptionParts = append(descriptionParts, "RDP: "+rawRDP)
	}
	if rawLitemanager != "" {
		descriptionParts = append(descriptionParts, "Litemanager: "+rawLitemanager)
	}
	if rawIP != "" {
		descriptionParts = append(descriptionParts, "IP/URL: "+rawIP)
	}

	fullDescription := strings.Join(descriptionParts, " | ")
	if fullDescription != "" {
		server.Description = &fullDescription
	}

	// 4. Заполняем остальные поля.
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		server.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	// Litemanager заполняется либо из прямого поля, либо извлекается из описания (как fallback)
	if rawLitemanager != "" && validators.LiteManagerIDRegex.MatchString(rawLitemanager) {
		server.Litemanager = &rawLitemanager
	} else {
		// Если прямого поля нет, ищем в старых полях
		server.Litemanager = validators.ExtractLiteManagerID(data, fullDescription)
	}

	if cl, ok := data["CabinetLink"].(string); ok && server.IP != nil {
		companyType := validators.DetermineCompanyTypeFromIP(*server.IP)
		link := validators.ValidateCabinetLink(cl, companyType)
		server.CabinetLink = &link
	}

	return server, nil
}

// DataToWorkstation преобразует мапу в модель Workstation.
func DataToWorkstation(data map[string]interface{}) (*models.Workstation, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("workstation data missing UUID")
	}
	ownerUUID := getOwnerUUID(data)
	if ownerUUID == "" {
		return nil, fmt.Errorf("workstation with uuid %s has no owner, skipping", uuid)
	}

	ws := &models.Workstation{}
	ws.ServiceDeskUUID = &uuid
	ws.OwnerServiceDeskUUID = &ownerUUID
	ws.MetaClass = "objectBase$Workstation"

	if tv, ok := data["Teamviewer"].(string); ok {
		ws.Teamviewer = validators.ValidateRemoteAccessID(tv)
	}
	if ad, ok := data["AnyDesk"].(string); ok {
		ws.Anydesk = validators.ValidateRemoteAccessID(ad)
	}
	if dn, ok := data["DeviceName"].(string); ok {
		ws.DeviceName = &dn
	}
	if desc, ok := data["Commentariy"].(string); ok {
		ws.Description = &desc
		ws.Litemanager = validators.ExtractLiteManagerID(data, desc)
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		ws.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	return ws, nil
}

// Regex для поиска ИНН в строке.
var innRegex = regexp.MustCompile(`ИНН:\s*(\d{10,12})`)

// DataToFiscalRegister преобразует мапу в модель FiscalRegister.
func DataToFiscalRegister(data map[string]interface{}) (*models.FiscalRegister, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("FR data missing UUID")
	}
	ownerUUID := getOwnerUUID(data)
	if ownerUUID == "" {
		return nil, fmt.Errorf("FR with uuid %s has no owner, skipping", uuid)
	}

	fr := &models.FiscalRegister{}
	fr.ServiceDeskUUID = &uuid
	fr.OwnerServiceDeskUUID = &ownerUUID
	fr.MetaClass = "objectBase$FR"

	if val, ok := data["ModelKKT"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.ModelKKT = &title
		}
	} else if val, ok := data["ModelKKT"].(string); ok {
		fr.ModelKKT = &val
	}

	if val, ok := data["FFD"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.FFD = &title
		}
	} else if val, ok := data["FFD"].(string); ok {
		fr.FFD = &val
	}

	if val, ok := data["FRDownloader"].(string); ok {
		fr.FRDownloader = &val
	}
	if val, ok := data["RNKKT"].(string); ok {
		// Нормализуем РН ККТ перед сохранением
		normalizedRNKKT := utils.NormalizeRNKKT(val)
		fr.RNKKT = &normalizedRNKKT
	}
	if val, ok := data["LegalName"].(string); ok {
		// ИЗВЛЕЧЕНИЕ ИНН: Ищем ИНН в юридическом имени
		matches := innRegex.FindStringSubmatch(val)
		if len(matches) > 1 {
			inn := matches[1]
			fr.INN = &inn
			// Очищаем LegalName от найденного ИНН
			cleanName := strings.TrimSpace(innRegex.ReplaceAllString(val, ""))
			fr.LegalName = &cleanName
		} else {
			fr.LegalName = &val
		}
	}
	if val, ok := data["FRSerialNumber"].(string); ok {
		fr.FRSerialNumber = &val
	}
	if val, ok := data["FNNumber"].(string); ok {
		fr.FNNumber = &val
	}
	if val, ok := data["KKTRegDate"].(string); ok {
		fr.KKTRegDate = utils.ParseServiceDeskTime(val)
	}
	if val, ok := data["FNExpireDate"].(string); ok {
		fr.FNExpireDate = utils.ParseServiceDeskTime(val)
	}
	if val, ok := data["lastModifiedDate"].(string); ok {
		fr.LastModifiedDate = utils.ParseServiceDeskTime(val)
	}

	return fr, nil
}

// getOwnerUUID извлекает UUID владельца из данных.
func getOwnerUUID(data map[string]interface{}) string {
	if owner, ok := data["owner"].(map[string]interface{}); ok {
		if oUUID, oOk := owner["UUID"].(string); oOk {
			return oUUID
		}
	}
	return ""
}

go `
===== END mappers.go =====

internal/services/mappers_test.go
===== START mappers_test.go =====
go `
package services

import (
	"context"
	"etalon-server/internal/models"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockServiceDeskClient для тестирования мапперов, которым он нужен
type MockServiceDeskClient struct {
	mock.Mock
}

func (m *MockServiceDeskClient) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	args := m.Called(ctx, metaClass, full)
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *MockServiceDeskClient) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	args := m.Called(ctx, uuid, metaClass)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockServiceDeskClient) FetchAgreementDetails(ctx context.Context, agreementUUID string) (*AgreementDetailsDTO, error) {
	args := m.Called(ctx, agreementUUID)
	val, ok := args.Get(0).(*AgreementDetailsDTO)
	if !ok {
		return nil, args.Error(1)
	}
	return val, args.Error(1)
}

// Вспомогательная функция для парсинга времени, т.к. utils не импортируется
func parseTime(t string) *time.Time {
	layout := "2006.01.02 15:04:05"
	parsed, err := time.Parse(layout, t)
	if err != nil {
		return nil
	}
	return &parsed
}

func TestDataToCompany(t *testing.T) {
	mockClient := new(MockServiceDeskClient)
	logger := zap.NewNop()
	ctx := context.Background()

	// ИЗМЕНЕНИЕ: Настраиваем мок для нового метода
	mockClient.On("FetchAgreementDetails", mock.Anything, "agreement-uuid-3").Return(&AgreementDetailsDTO{State: "active"}, nil)
	mockClient.On("FetchAgreementDetails", mock.Anything, "agreement-uuid-inactive").Return(&AgreementDetailsDTO{State: "inactive"}, nil)

	testCases := []struct {
		name        string
		input       map[string]interface{}
		expectError bool
		expected    *models.Company
	}{
		{
			name: "Полные корректные данные",
			input: map[string]interface{}{
				"UUID":             "company-uuid-1",
				"title":            "ООО Ромашка",
				"adress":           "г. Москва",
				"additionalName":   "Главный офис",
				"lastModifiedDate": "2023.10.30 15:00:00",
				"parent":           map[string]interface{}{"UUID": "parent-uuid-2"},
				"recipientAgreements": []interface{}{
					map[string]interface{}{"UUID": "agreement-uuid-inactive", "metaClass": "agreement$agreement"},
					map[string]interface{}{"UUID": "agreement-uuid-3", "metaClass": "agreement$agreement"},
				},
			},
			expectError: false,
			expected: &models.Company{
				Base:                  models.Base{ServiceDeskUUID: StringPtr("company-uuid-1")},
				Title:                 StringPtr("ООО Ромашка"),
				Address:               StringPtr("г. Москва"),
				AdditionalName:        StringPtr("Главный офис"),
				LastModifiedDate:      parseTime("2023.10.30 15:00:00"),
				ParentServiceDeskUUID: StringPtr("parent-uuid-2"),
				ActiveContract:        BoolPtr(true),
			},
		},
		{
			name: "Данные без UUID",
			input: map[string]interface{}{
				"title": "Компания без UUID",
			},
			expectError: true,
			expected:    nil,
		},
		{
			name: "Частичные данные без контрактов",
			input: map[string]interface{}{
				"UUID":  "company-uuid-4",
				"title": "ООО Василек",
			},
			expectError: false,
			expected: &models.Company{
				Base:           models.Base{ServiceDeskUUID: StringPtr("company-uuid-4")},
				Title:          StringPtr("ООО Василек"),
				ActiveContract: BoolPtr(false),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			company, err := DataToCompany(ctx, tc.input, mockClient, logger)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected.ServiceDeskUUID, company.ServiceDeskUUID)
				assert.Equal(t, tc.expected.Title, company.Title)
				assert.Equal(t, tc.expected.Address, company.Address)
				assert.Equal(t, tc.expected.ParentServiceDeskUUID, company.ParentServiceDeskUUID)
				assert.Equal(t, tc.expected.ActiveContract, company.ActiveContract)
				if tc.expected.LastModifiedDate != nil {
					assert.True(t, tc.expected.LastModifiedDate.Equal(*company.LastModifiedDate))
				}
			}
		})
	}
}

func TestDataToServer(t *testing.T) {
	testCases := []struct {
		name        string
		input       map[string]interface{}
		expectError bool
		expected    *models.Server
	}{
		{
			name: "Полные корректные данные",
			input: map[string]interface{}{
				"UUID":             "server-uuid-1",
				"DeviceName":       "SRV-MAIN",
				"IP":               "192.168.1.10",
				"AnyDesk":          "111 222 333",
				"lastModifiedDate": "2023.10.30 16:00:00",
				"owner":            map[string]interface{}{"UUID": "owner-uuid-1"},
			},
			expectError: false,
			expected: &models.Server{
				Base:                 models.Base{ServiceDeskUUID: StringPtr("server-uuid-1")},
				DeviceName:           StringPtr("SRV-MAIN"),
				IP:                   StringPtr("192.168.1.10:8080"),
				Anydesk:              StringPtr("111222333"),
				LastModifiedDate:     parseTime("2023.10.30 16:00:00"),
				OwnerServiceDeskUUID: StringPtr("owner-uuid-1"),
			},
		},
		{
			name: "Данные без владельца",
			input: map[string]interface{}{
				"UUID": "server-uuid-2",
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server, err := DataToServer(tc.input)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected.ServiceDeskUUID, server.ServiceDeskUUID)
				assert.Equal(t, tc.expected.DeviceName, server.DeviceName)
				assert.Equal(t, tc.expected.IP, server.IP)
				assert.Equal(t, tc.expected.Anydesk, server.Anydesk)
				assert.Equal(t, tc.expected.OwnerServiceDeskUUID, server.OwnerServiceDeskUUID)
			}
		})
	}
}

func TestDataToWorkstation(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]interface{}
		expected *models.Workstation
	}{
		{
			name: "Полные данные",
			input: map[string]interface{}{
				"UUID":        "ws-uuid-1",
				"DeviceName":  "KASSA-1",
				"AnyDesk":     "333 222 111",
				"Teamviewer":  "1234567890",
				"Commentariy": "Основная касса с MH_99999",
				"owner":       map[string]interface{}{"UUID": "owner-uuid-ws-1"},
			},
			expected: &models.Workstation{
				Base: models.Base{
					ServiceDeskUUID: StringPtr("ws-uuid-1"),
					MetaClass:       "objectBase$Workstation",
				},
				DeviceName:           StringPtr("KASSA-1"),
				Anydesk:              StringPtr("333222111"),
				Teamviewer:           StringPtr("1234567890"),
				Litemanager:          StringPtr("MH_99999"),
				Description:          StringPtr("Основная касса с MH_99999"),
				OwnerServiceDeskUUID: StringPtr("owner-uuid-ws-1"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := DataToWorkstation(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, ws)
		})
	}
}

func TestDataToFiscalRegister(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]interface{}
		expected *models.FiscalRegister
	}{
		{
			name: "Полные данные",
			input: map[string]interface{}{
				"UUID":           "fr-uuid-1",
				"ModelKKT":       map[string]interface{}{"title": "ШТРИХ-М-01Ф"},
				"FFD":            "1.2",
				"RNKKT":          "0001234567012345",
				"FRSerialNumber": "123456789",
				"FNNumber":       "987654321",
				"FNExpireDate":   "2025.12.31 23:59:59",
				"owner":          map[string]interface{}{"UUID": "owner-uuid-fr-1"},
			},
			expected: &models.FiscalRegister{
				Base:                 models.Base{ServiceDeskUUID: StringPtr("fr-uuid-1")},
				ModelKKT:             StringPtr("ШТРИХ-М-01Ф"),
				FFD:                  StringPtr("1.2"),
				RNKKT:                StringPtr("0001234567012345"),
				FRSerialNumber:       StringPtr("123456789"),
				FNNumber:             StringPtr("987654321"),
				FNExpireDate:         parseTime("2025.12.31 23:59:59"),
				OwnerServiceDeskUUID: StringPtr("owner-uuid-fr-1"),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fr, err := DataToFiscalRegister(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected.ServiceDeskUUID, fr.ServiceDeskUUID)
			assert.Equal(t, tc.expected.ModelKKT, fr.ModelKKT)
			assert.Equal(t, tc.expected.FFD, fr.FFD)
			assert.Equal(t, tc.expected.RNKKT, fr.RNKKT)
			assert.Equal(t, tc.expected.OwnerServiceDeskUUID, fr.OwnerServiceDeskUUID)
			assert.True(t, fr.FNExpireDate.Equal(*tc.expected.FNExpireDate))
		})
	}
}

// Вспомогательные функции для создания указателей в тестах
func StringPtr(s string) *string { return &s }
func BoolPtr(b bool) *bool       { return &b }

go `
===== END mappers_test.go =====

internal/services/reconciler_service.go
===== START reconciler_service.go =====
go `
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var rmsUrlRegex = regexp.MustCompile(`(?i)(https?://)?([a-zA-Z0-9.-]+)`)

type ReconcilerService interface {
	Start(ctx context.Context)
	ProcessAgentData(ctx context.Context, data *api.AgentDataDTO) (ownerID, entityUUID, method string, err error)
}

type reconcilerServiceImpl struct {
	cfg             *config.Config
	logger          *zap.Logger
	db              *gorm.DB
	ftpClient       FTPClient
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

func NewReconcilerService(cfg *config.Config, logger *zap.Logger, db *gorm.DB, ftpClient FTPClient, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) ReconcilerService {
	return &reconcilerServiceImpl{
		cfg:             cfg,
		logger:          logger,
		db:              db,
		ftpClient:       ftpClient,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

func (s *reconcilerServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск сервиса сверки (ReconcilerService)", zap.Duration("interval", s.cfg.ReconcileInterval))
	ticker := time.NewTicker(s.cfg.ReconcileInterval)
	defer ticker.Stop()
	s.runReconciliationCycle(ctx)
	for {
		select {
		case <-ticker.C:
			s.runReconciliationCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание воркера сверки")
			return
		}
	}
}

// runReconciliationCycle выполняет один полный цикл сверки: синхронизирует локальный кэш с FTP и обрабатывает каждый файл.
func (s *reconcilerServiceImpl) runReconciliationCycle(ctx context.Context) {
	s.logger.Info("Начало нового цикла сверки данных...")
	if err := s.syncLocalCacheWithFTP(ctx); err != nil {
		s.logger.Error("Ошибка синхронизации кэша с FTP, цикл прерван", zap.Error(err))
		return
	}
	localFiles, err := os.ReadDir(s.cfg.FTPCachePath)
	if err != nil {
		s.logger.Error("Не удалось прочитать директорию с кэшем, цикл прерван", zap.Error(err))
		return
	}
	for _, file := range localFiles {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".json") {
			continue
		}
		select {
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание воркера обработки файлов.")
			return
		default:
			s.processFile(ctx, file.Name())
		}
	}
	s.logger.Info("Цикл сверки данных завершен.")
}

// syncLocalCacheWithFTP скачивает новые или обновленные файлы с FTP-сервера в локальный кэш.
func (s *reconcilerServiceImpl) syncLocalCacheWithFTP(_ context.Context) error {
	s.logger.Info("Синхронизация локального кэша с FTP...")
	ftpFiles, err := s.ftpClient.ListFiles(s.cfg.FTPPath)
	if err != nil {
		return fmt.Errorf("не удалось получить список файлов с FTP: %w", err)
	}

	localFileInfos := make(map[string]os.FileInfo)
	cachedFiles, err := os.ReadDir(s.cfg.FTPCachePath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать кэш-директорию: %w", err)
	}

	for _, f := range cachedFiles {
		if info, err := f.Info(); err == nil {
			localFileInfos[f.Name()] = info
		}
	}
	for _, ftpFile := range ftpFiles {
		if ftpFile.Type != ftp.EntryTypeFile || !strings.HasSuffix(strings.ToLower(ftpFile.Name), ".json") || ftpFile.Size == 0 {
			continue
		}
		localInfo, found := localFileInfos[ftpFile.Name]
		if !found || ftpFile.Time.After(localInfo.ModTime()) {
			s.logger.Info("Обнаружен новый/обновленный файл, скачивание...", zap.String("file", ftpFile.Name))
			ftpFilePath := path.Join(s.cfg.FTPPath, ftpFile.Name)
			fileData, err := s.ftpClient.DownloadFile(ftpFilePath)
			if err != nil {
				s.logger.Error("Не удалось скачать файл", zap.String("file", ftpFile.Name), zap.Error(err))
				continue
			}

			localFilePath := filepath.Join(s.cfg.FTPCachePath, ftpFile.Name)
			if err := os.WriteFile(localFilePath, fileData, 0644); err != nil {
				s.logger.Error("Не удалось сохранить файл в кэш", zap.String("file", localFilePath), zap.Error(err))
				continue
			}
			os.Chtimes(localFilePath, ftpFile.Time, ftpFile.Time)
		}
	}
	s.logger.Info("Синхронизация локального кэша завершена.")
	return nil
}

// processFile обрабатывает один JSON-файл из кэша.
func (s *reconcilerServiceImpl) processFile(ctx context.Context, fileName string) {
	log := s.logger.With(zap.String("file", fileName))
	localFilePath := filepath.Join(s.cfg.FTPCachePath, fileName)

	if processed, err := s.isAlreadyProcessed(ctx, fileName); err != nil {
		log.Error("Ошибка проверки статуса файла в БД", zap.Error(err))
		return
	} else if processed {
		return
	}

	fileData, err := os.ReadFile(localFilePath)
	if err != nil {
		log.Error("Не удалось прочитать файл из кэша", zap.Error(err))
		return
	}

	var data api.AgentDataDTO
	if err := json.Unmarshal(fileData, &data); err != nil {
		log.Error("Не удалось распарсить JSON", zap.Error(err))
		return
	}

	log.Info("Обработка файла из кэша...")

	if _, _, _, err := s.ProcessAgentData(ctx, &data); err != nil {
		log.Warn("Ошибка при обработке данных из файла", zap.Error(err))
	}
	s.updateAgentFileStatus(ctx, fileName)
}

// ProcessAgentData выполняет основную "водопадную" логику сверки данных от агента.
func (s *reconcilerServiceImpl) ProcessAgentData(ctx context.Context, data *api.AgentDataDTO) (ownerID, entityUUID, method string, err error) {
	log := s.logger.With(zap.String("agent_hostname", data.Hostname))
	log.Info("Начало процесса сверки данных от агента")

	// Нормализуем IP-адрес из URL RMS перед поиском
	normalizedIP := validators.ValidateIPAddress(data.URLRms)

	foundServer, _ := s.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, utils.SafeStringDereference(normalizedIP))
	foundWS, _ := s.workstationRepo.FindByRemoteIDs(ctx, data.TeamviewerID, data.AnydeskID, data.LitemanagerID)
	foundFR, _ := s.frRepo.FindBySerialNumber(ctx, data.SerialNumber)

	// Получаем информацию о компаниях-владельцах для обогащения комментариев в задачах
	ownerCompanies := s.getOwnerCompanies(ctx, foundServer, foundWS, foundFR)

	if foundServer != nil {
		log.Info("Приоритет 1: Найдено совпадение по Серверу", zap.String("server_uuid", utils.SafeStringDereference(foundServer.ServiceDeskUUID)))
		ownerID = utils.SafeStringDereference(foundServer.OwnerServiceDeskUUID)
		s.reconcileFromServerContext(ctx, ownerID, data, foundServer, foundWS, foundFR, ownerCompanies, log)
		return ownerID, utils.SafeStringDereference(foundServer.ServiceDeskUUID), "server_match", nil
	}

	if foundWS != nil {
		log.Info("Приоритет 2: Найдено совпадение по Рабочей станции", zap.String("ws_uuid", utils.SafeStringDereference(foundWS.ServiceDeskUUID)))
		ownerID = utils.SafeStringDereference(foundWS.OwnerServiceDeskUUID)
		s.reconcileFromWorkstationContext(ctx, ownerID, data, foundWS, foundFR, ownerCompanies, log)
		return ownerID, utils.SafeStringDereference(foundWS.ServiceDeskUUID), "workstation_match", nil
	}

	if foundFR != nil {
		log.Info("Приоритет 3: Найдено совпадение по Фискальному регистратору", zap.String("fr_uuid", utils.SafeStringDereference(foundFR.ServiceDeskUUID)))
		ownerID = utils.SafeStringDereference(foundFR.OwnerServiceDeskUUID)
		s.reconcileFromFRContext(ctx, ownerID, data, foundFR, ownerCompanies, log)
		return ownerID, utils.SafeStringDereference(foundFR.ServiceDeskUUID), "fr_match", nil
	}

	log.Warn("Не найдено совпадений ни по одному из приоритетов. Создание задачи 'new_client'.")
	s.createTask(ctx, "new_client", "", "", data, "Не удалось идентифицировать оборудование. Требуется создать нового клиента и привязать оборудование.", "")
	return "", "", "no_match", fmt.Errorf("не удалось найти совпадения")
}

// reconcileFromServerContext обрабатывает логику сверки, когда сервер является главной точкой отсчета.
func (s *reconcilerServiceImpl) reconcileFromServerContext(ctx context.Context, ownerID string, data *api.AgentDataDTO, server *models.Server, ws *models.Workstation, fr *models.FiscalRegister, owners map[string]*models.Company, log *zap.Logger) {
	s.reconcileServerData(ctx, server, data, log)

	serverOwnerName := getCompanyName(owners, server.OwnerServiceDeskUUID)
	serverIdentifier := fmt.Sprintf("по серверу '%s' (%s)", utils.SafeStringDereference(server.DeviceName), *server.ServiceDeskUUID)

	if data.AnydeskID != "" || data.TeamviewerID != "" || data.LitemanagerID != "" {
		if ws != nil {
			log.Info("Найдена существующая рабочая станция, выполняется слияние данных.", zap.String("ws_uuid", utils.SafeStringDereference(ws.ServiceDeskUUID)))
			if utils.SafeStringDereference(ws.OwnerServiceDeskUUID) != ownerID {
				currentOwnerName := getCompanyName(owners, ws.OwnerServiceDeskUUID)
				comment := fmt.Sprintf("Несоответствие владельца для рабочей станции '%s' (%s). Текущий владелец: '%s' (%s). Ожидаемый владелец: '%s' (%s), определен %s.",
					utils.SafeStringDereference(ws.DeviceName), *ws.ServiceDeskUUID, currentOwnerName, *ws.OwnerServiceDeskUUID, serverOwnerName, ownerID, serverIdentifier)
				s.createTask(ctx, "owner_mismatch", "Workstation", *ws.ServiceDeskUUID, data, comment, "")
			}
			s.reconcileWorkstationData(ctx, ws, data, log)
		} else {
			comment := fmt.Sprintf("Добавить новую рабочую станцию для владельца '%s' (%s). Владелец определен %s. ID удаленного доступа: %s.",
				serverOwnerName, ownerID, serverIdentifier, formatRemoteIDs(data))
			s.createTask(ctx, "add_equipment", "Workstation", "", data, comment, serverIdentifier)
		}
	}

	if data.SerialNumber != "" {
		if fr != nil {
			if utils.SafeStringDereference(fr.OwnerServiceDeskUUID) != ownerID {
				currentOwnerName := getCompanyName(owners, fr.OwnerServiceDeskUUID)
				comment := fmt.Sprintf("Несоответствие владельца для ФР с СН '%s' (%s). Текущий владелец: '%s' (%s). Ожидаемый владелец: '%s' (%s), определен %s.",
					*fr.FRSerialNumber, *fr.ServiceDeskUUID, currentOwnerName, *fr.OwnerServiceDeskUUID, serverOwnerName, ownerID, serverIdentifier)
				s.createTask(ctx, "owner_mismatch", "FiscalRegister", *fr.ServiceDeskUUID, data, comment, "")
			}
			s.reconcileFiscalRegisterData(ctx, fr, data, log)
		} else {
			comment := fmt.Sprintf("Добавить новый ФР (СН: %s) для владельца '%s' (%s). Владелец определен %s.",
				data.SerialNumber, serverOwnerName, ownerID, serverIdentifier)
			s.createTask(ctx, "add_equipment", "FiscalRegister", "", data, comment, serverIdentifier)
		}
	}
}

// reconcileFromWorkstationContext обрабатывает логику сверки, когда рабочая станция является точкой отсчета.
func (s *reconcilerServiceImpl) reconcileFromWorkstationContext(ctx context.Context, ownerID string, data *api.AgentDataDTO, ws *models.Workstation, fr *models.FiscalRegister, owners map[string]*models.Company, log *zap.Logger) {
	s.reconcileWorkstationData(ctx, ws, data, log)

	wsOwnerName := getCompanyName(owners, ws.OwnerServiceDeskUUID)
	wsIdentifier := fmt.Sprintf("по рабочей станции '%s' (%s)", utils.SafeStringDereference(ws.DeviceName), *ws.ServiceDeskUUID)

	// Проверяем, нужно ли создавать задачу на добавление сервера
	normalizedIP := validators.ValidateIPAddress(data.URLRms)
	if normalizedIP != nil {
		isPrivate, _ := utils.IsPrivateIP(strings.Split(*normalizedIP, ":")[0])
		if !isPrivate {
			comment := fmt.Sprintf("Добавить новый сервер (IP: %s) для владельца '%s' (%s). Владелец определен %s.",
				*normalizedIP, wsOwnerName, ownerID, wsIdentifier)
			s.createTask(ctx, "add_equipment", "Server", "", data, comment, wsIdentifier)
		} else {
			log.Info("IP-адрес сервера является приватным, задача на добавление не создается.", zap.String("ip", *normalizedIP))
		}
	}

	if data.SerialNumber != "" {
		if fr != nil {
			if utils.SafeStringDereference(fr.OwnerServiceDeskUUID) != ownerID {
				currentOwnerName := getCompanyName(owners, fr.OwnerServiceDeskUUID)
				comment := fmt.Sprintf("Несоответствие владельца для ФР с СН '%s' (%s). Текущий владелец: '%s' (%s). Ожидаемый владелец: '%s' (%s), определен %s.",
					*fr.FRSerialNumber, *fr.ServiceDeskUUID, currentOwnerName, *fr.OwnerServiceDeskUUID, wsOwnerName, ownerID, wsIdentifier)
				s.createTask(ctx, "owner_mismatch", "FiscalRegister", *fr.ServiceDeskUUID, data, comment, "")
			}
			s.reconcileFiscalRegisterData(ctx, fr, data, log)
		} else {
			comment := fmt.Sprintf("Добавить новый ФР (СН: %s) для владельца '%s' (%s). Владелец определен %s.",
				data.SerialNumber, wsOwnerName, ownerID, wsIdentifier)
			s.createTask(ctx, "add_equipment", "FiscalRegister", "", data, comment, wsIdentifier)
		}
	}
}

// reconcileFromFRContext обрабатывает логику сверки, когда ФР является точкой отсчета.
func (s *reconcilerServiceImpl) reconcileFromFRContext(ctx context.Context, ownerID string, data *api.AgentDataDTO, fr *models.FiscalRegister, owners map[string]*models.Company, log *zap.Logger) {
	s.reconcileFiscalRegisterData(ctx, fr, data, log)

	frOwnerName := getCompanyName(owners, fr.OwnerServiceDeskUUID)
	frIdentifier := fmt.Sprintf("по ФР с СН '%s' (%s)", *fr.FRSerialNumber, *fr.ServiceDeskUUID)

	normalizedIP := validators.ValidateIPAddress(data.URLRms)
	if normalizedIP != nil {
		isPrivate, _ := utils.IsPrivateIP(strings.Split(*normalizedIP, ":")[0])
		if !isPrivate {
			comment := fmt.Sprintf("Добавить новый сервер (IP: %s) для владельца '%s' (%s). Владелец определен %s.",
				*normalizedIP, frOwnerName, ownerID, frIdentifier)
			s.createTask(ctx, "add_equipment", "Server", "", data, comment, frIdentifier)
		} else {
			log.Info("IP-адрес сервера является приватным, задача на добавление не создается.", zap.String("ip", *normalizedIP))
		}
	}

	if data.AnydeskID != "" || data.TeamviewerID != "" || data.LitemanagerID != "" {
		comment := fmt.Sprintf("Добавить новую рабочую станцию для владельца '%s' (%s). Владелец определен %s. ID удаленного доступа: %s.",
			frOwnerName, ownerID, frIdentifier, formatRemoteIDs(data))
		s.createTask(ctx, "add_equipment", "Workstation", "", data, comment, frIdentifier)
	}
}

// --- Вспомогательные функции для "умного" обновления данных ---

// reconcileServerData обновляет данные сервера, только если поля в БД пусты.
func (s *reconcilerServiceImpl) reconcileServerData(ctx context.Context, server *models.Server, data *api.AgentDataDTO, log *zap.Logger) {
	updates := make(map[string]interface{})
	if (server.CRMid == nil || *server.CRMid == "") && data.CRMID != "" {
		updates["crm_id"] = data.CRMID
	}
	if len(updates) > 0 {
		if _, err := s.serverRepo.Update(ctx, nil, utils.SafeStringDereference(server.ServiceDeskUUID), updates); err != nil {
			log.Error("Не удалось обновить данные сервера", zap.String("uuid", utils.SafeStringDereference(server.ServiceDeskUUID)), zap.Error(err))
		}
	}
}

// reconcileWorkstationData выполняет "умное" слияние данных: обновляет поля, только если они пусты, чтобы не затереть существующую информацию.
func (s *reconcilerServiceImpl) reconcileWorkstationData(ctx context.Context, ws *models.Workstation, data *api.AgentDataDTO, log *zap.Logger) {
	updates := make(map[string]interface{})
	if (ws.Anydesk == nil || *ws.Anydesk == "") && data.AnydeskID != "" && data.AnydeskID != "None" {
		updates["anydesk"] = data.AnydeskID
	}
	if (ws.Teamviewer == nil || *ws.Teamviewer == "") && data.TeamviewerID != "" && data.TeamviewerID != "None" {
		updates["teamviewer"] = data.TeamviewerID
	}
	if (ws.Litemanager == nil || *ws.Litemanager == "") && data.LitemanagerID != "" && data.LitemanagerID != "None" {
		updates["litemanager"] = data.LitemanagerID
	}
	if len(updates) > 0 {
		log.Info("Обновление данных рабочей станции (слияние)", zap.String("ws_uuid", utils.SafeStringDereference(ws.ServiceDeskUUID)), zap.Any("added_ids", updates))
		if _, err := s.workstationRepo.Update(ctx, nil, utils.SafeStringDereference(ws.ServiceDeskUUID), updates); err != nil {
			log.Error("Не удалось обновить данные рабочей станции", zap.String("uuid", utils.SafeStringDereference(ws.ServiceDeskUUID)), zap.Error(err))
		}
	}
}

// reconcileFiscalRegisterData обновляет данные ФР, если они найдены по серийному номеру.
// Логика: мы доверяем данным от агента как самым актуальным и полностью обновляем запись в БД.
func (s *reconcilerServiceImpl) reconcileFiscalRegisterData(ctx context.Context, fr *models.FiscalRegister, data *api.AgentDataDTO, log *zap.Logger) {
	updates := make(map[string]interface{})

	// Собираем все поля из данных агента для полного обновления
	updates["model_kkt"] = data.ModelName
	updates["rn_kkt"] = utils.NormalizeRNKKT(data.RNM)
	updates["fn_number"] = data.FNSerial
	updates["inn"] = strings.TrimSpace(data.INN)
	updates["ffd"] = utils.FormatFFDVersion(data.FFDVersion)
	updates["fn_expire_date"] = utils.ParseAgentTime(data.DateTimeEnd)
	updates["last_modified_date"] = time.Now() // Устанавливаем текущее время как дату последнего обновления
	if data.InstalledDriver != "" {
		updates["driver_version"] = data.InstalledDriver
	}

	log.Info("Обновление фискального регистратора полным набором данных от агента.", zap.String("uuid", utils.SafeStringDereference(fr.ServiceDeskUUID)), zap.Any("changes", updates))
	if _, err := s.frRepo.Update(ctx, nil, utils.SafeStringDereference(fr.ServiceDeskUUID), updates); err != nil {
		log.Error("Не удалось обновить данные ФР", zap.String("uuid", utils.SafeStringDereference(fr.ServiceDeskUUID)), zap.Error(err))
	}
}

// createTask создает задачу для ручного разбора администратором.
func (s *reconcilerServiceImpl) createTask(ctx context.Context, taskType, entityType, entityUUID string, agentData *api.AgentDataDTO, comment, reason string) {
	details, _ := json.Marshal(agentData)
	task := models.ReconciliationTask{
		TaskType:   taskType,
		EntityType: entityType,
		EntityUUID: entityUUID,
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    comment,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		s.logger.Error("Не удалось создать задачу на сверку", zap.Error(err))
	} else {
		s.logger.Info("Создана новая задача на сверку", zap.String("type", taskType), zap.String("entity_uuid", entityUUID), zap.String("reason", reason))
	}
}

// isAlreadyProcessed проверяет, был ли файл с таким же именем, размером и временем модификации уже обработан.
func (s *reconcilerServiceImpl) isAlreadyProcessed(ctx context.Context, fileName string) (bool, error) {
	var processedFile models.AgentFile
	err := s.db.WithContext(ctx).First(&processedFile, "file_name = ?", fileName).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	fileInfo, err := os.Stat(filepath.Join(s.cfg.FTPCachePath, fileName))
	if err != nil {
		return false, err
	}
	return processedFile.LastProcessedModTime.Equal(fileInfo.ModTime()) && processedFile.LastProcessedFileSize == fileInfo.Size(), nil
}

// updateAgentFileStatus сохраняет в БД информацию об обработанном файле.
func (s *reconcilerServiceImpl) updateAgentFileStatus(ctx context.Context, fileName string) {
	localPath := filepath.Join(s.cfg.FTPCachePath, fileName)
	fileInfo, err := os.Stat(localPath)
	if err != nil {
		s.logger.Error("Не удалось получить информацию о файле в кэше для обновления статуса", zap.String("file", fileName), zap.Error(err))
		return
	}
	record := models.AgentFile{
		FileName:              fileName,
		LastProcessedModTime:  fileInfo.ModTime(),
		LastProcessedFileSize: fileInfo.Size(),
	}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_processed_mod_time", "last_processed_file_size", "updated_at"}),
	}).Create(&record).Error
	if err != nil {
		s.logger.Error("Не удалось обновить статус файла в БД", zap.String("file", fileName), zap.Error(err))
	}
}

// getOwnerCompanies загружает из БД информацию о компаниях-владельцах для обогащения логов.
func (s *reconcilerServiceImpl) getOwnerCompanies(ctx context.Context, server *models.Server, ws *models.Workstation, fr *models.FiscalRegister) map[string]*models.Company {
	uuids := make(map[string]struct{})
	if server != nil && server.OwnerServiceDeskUUID != nil {
		uuids[*server.OwnerServiceDeskUUID] = struct{}{}
	}
	if ws != nil && ws.OwnerServiceDeskUUID != nil {
		uuids[*ws.OwnerServiceDeskUUID] = struct{}{}
	}
	if fr != nil && fr.OwnerServiceDeskUUID != nil {
		uuids[*fr.OwnerServiceDeskUUID] = struct{}{}
	}

	if len(uuids) == 0 {
		return nil
	}

	uuidList := make([]string, 0, len(uuids))
	for uuid := range uuids {
		uuidList = append(uuidList, uuid)
	}

	// Используем временный репозиторий для этого запроса
	companyRepo := repositories.NewCompanyRepo(s.db)
	companies, err := companyRepo.GetByUUIDs(ctx, uuidList)
	if err != nil {
		s.logger.Error("Не удалось получить информацию о компаниях-владельцах", zap.Error(err))
		return nil
	}

	companyMap := make(map[string]*models.Company)
	for i := range companies {
		companyMap[*companies[i].ServiceDeskUUID] = &companies[i]
	}
	return companyMap
}

// getCompanyName безопасно извлекает имя компании из мапы.
func getCompanyName(owners map[string]*models.Company, uuid *string) string {
	if uuid == nil || owners == nil {
		return "[Неизвестный владелец]"
	}
	if company, ok := owners[*uuid]; ok {
		return utils.SafeStringDereference(company.Title)
	}
	return "[Неизвестный владелец]"
}

// formatRemoteIDs форматирует строку с ID удаленного доступа для логов/комментариев.
func formatRemoteIDs(data *api.AgentDataDTO) string {
	var parts []string
	if data.TeamviewerID != "" && data.TeamviewerID != "None" {
		parts = append(parts, "TV: "+data.TeamviewerID)
	}
	if data.AnydeskID != "" && data.AnydeskID != "None" {
		parts = append(parts, "AD: "+data.AnydeskID)
	}
	if data.LitemanagerID != "" && data.LitemanagerID != "None" {
		parts = append(parts, "LM: "+data.LitemanagerID)
	}
	if len(parts) == 0 {
		return "не указаны"
	}
	return strings.Join(parts, ", ")
}

go `
===== END reconciler_service.go =====

internal/services/sdesk_sync_service.go
===== START sdesk_sync_service.go =====
go `
// internal/services/sdesk_sync_service.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// SDeskSyncService отвечает за фоновую инкрементальную синхронизацию с ServiceDesk.
type SDeskSyncService interface {
	Start(ctx context.Context)
}

type sdeskSyncServiceImpl struct {
	cfg             *config.Config
	sdClient        ServiceDeskClient
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	logger          *zap.Logger
	db              *gorm.DB
	mu              sync.Mutex
	isSyncing       bool
}

// localEntityInfo - внутренняя структура для хранения минимально необходимых данных из локальной БД для сравнения.
type localEntityInfo struct {
	LastModifiedDate *time.Time
	DeletedAt        gorm.DeletedAt
}

// NewSDeskSyncService создает новый экземпляр сервиса синхронизации.
func NewSDeskSyncService(
	cfg *config.Config,
	db *gorm.DB,
	sdClient ServiceDeskClient,
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	logger *zap.Logger,
) SDeskSyncService {
	return &sdeskSyncServiceImpl{
		cfg:             cfg,
		db:              db,
		sdClient:        sdClient,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		logger:          logger,
	}
}

// Start запускает воркер в фоновом режиме.
func (s *sdeskSyncServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера синхронизации с ServiceDesk", zap.Duration("interval", s.cfg.SDeskSyncInterval))
	ticker := time.NewTicker(s.cfg.SDeskSyncInterval)
	defer ticker.Stop()

	s.runSyncCycle(ctx)

	for {
		select {
		case <-ticker.C:
			s.runSyncCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Остановка воркера синхронизации с ServiceDesk...")
			return
		}
	}
}

func (s *sdeskSyncServiceImpl) runSyncCycle(ctx context.Context) {
	s.mu.Lock()
	if s.isSyncing {
		s.logger.Warn("Цикл синхронизации уже запущен. Пропуск.")
		s.mu.Unlock()
		return
	}
	s.isSyncing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.isSyncing = false
		s.mu.Unlock()
	}()

	s.logger.Info("Начало нового цикла синхронизации с ServiceDesk.")

	agreementCache := make(map[string]*AgreementDetailsDTO)
	cycleCtx := context.WithValue(ctx, agreementCacheKey, agreementCache)

	s.syncEntityType(cycleCtx, "ou$company")
	s.syncEntityType(cycleCtx, "objectBase$Server")
	s.syncEntityType(cycleCtx, "objectBase$Workstation")
	s.syncEntityType(cycleCtx, "objectBase$FR")

	s.logger.Info("Цикл синхронизации с ServiceDesk завершен.")
}

// syncEntityType выполняет инкрементальную синхронизацию для одного типа сущности.
func (s *sdeskSyncServiceImpl) syncEntityType(ctx context.Context, metaClass string) {
	log := s.logger.With(zap.String("metaClass", metaClass))
	log.Info("Начало синхронизации типа сущности")

	remoteList, err := s.sdClient.FetchEntityList(ctx, metaClass, false)
	if err != nil {
		log.Error("Не удалось получить список сущностей из ServiceDesk", zap.Error(err))
		return
	}

	localMap, err := s.getLocalEntities(ctx, metaClass)
	if err != nil {
		log.Error("Не удалось получить локальные сущности", zap.Error(err))
		return
	}

	remoteUUIDs := make(map[string]struct{}, len(remoteList))
	for _, item := range remoteList {
		if uuid, ok := item["UUID"].(string); ok {
			remoteUUIDs[uuid] = struct{}{}
		}
	}

	var toCreate, toUpdate, toDelete []string

	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteLMD := utils.ParseServiceDeskTime(remoteItem["lastModifiedDate"].(string))
		if remoteLMD == nil {
			continue
		}
		localEntity, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
		} else if localEntity.DeletedAt.Valid || (localEntity.LastModifiedDate != nil && remoteLMD.After(*localEntity.LastModifiedDate)) {
			toUpdate = append(toUpdate, remoteUUID)
		}
	}

	for uuid, localInfo := range localMap {
		if _, exists := remoteUUIDs[uuid]; !exists && !localInfo.DeletedAt.Valid {
			toDelete = append(toDelete, uuid)
		}
	}

	log.Info("Сравнение завершено",
		zap.Int("to_create", len(toCreate)),
		zap.Int("to_update", len(toUpdate)),
		zap.Int("to_delete", len(toDelete)))

	if len(toCreate) > 0 {
		s.processCreationsInParallel(ctx, metaClass, toCreate, log)
	}
	if len(toUpdate) > 0 {
		s.processUpdatesInParallel(ctx, metaClass, toUpdate, log)
	}
	if len(toDelete) > 0 {
		s.processDeletions(ctx, metaClass, toDelete, log)
	}

	if len(toCreate) == 0 && len(toUpdate) == 0 && len(toDelete) == 0 {
		log.Info("Нет сущностей для создания, обновления или удаления.")
	}
}

// processDeletions выполняет "мягкое удаление" и закрывает связанные задачи.
func (s *sdeskSyncServiceImpl) processDeletions(ctx context.Context, metaClass string, toDelete []string, log *zap.Logger) {
	log.Info("Запуск процесса 'мягкого удаления' для устаревших записей", zap.Int("count", len(toDelete)))

	for _, uuid := range toDelete {
		var deleted bool
		var err error

		err = s.db.Transaction(func(tx *gorm.DB) error {
			switch metaClass {
			case "ou$company":
				deleted, err = s.companyRepo.Delete(ctx, tx, uuid)
			case "objectBase$Server":
				deleted, err = s.serverRepo.Delete(ctx, tx, uuid)
			case "objectBase$Workstation":
				deleted, err = s.workstationRepo.Delete(ctx, tx, uuid)
			case "objectBase$FR":
				deleted, err = s.frRepo.Delete(ctx, tx, uuid)
			default:
				return fmt.Errorf("неизвестный metaClass для удаления: %s", metaClass)
			}
			return err
		})

		if err != nil {
			log.Error("Ошибка при 'мягком удалении' сущности", zap.String("uuid", uuid), zap.Error(err))
			continue
		}

		if deleted {
			log.Info("Сущность успешно помечена как удаленная", zap.String("uuid", uuid))
			s.resolveDeletionTask(ctx, uuid, log)
		}
	}
}

// resolveDeletionTask находит и закрывает задачу на удаление в SD.
func (s *sdeskSyncServiceImpl) resolveDeletionTask(ctx context.Context, entityUUID string, log *zap.Logger) {
	result := s.db.WithContext(ctx).Model(&models.ReconciliationTask{}).
		Where("entity_uuid = ? AND task_type = ? AND status = ?", entityUUID, "delete_from_servicedesk", "new").
		Update("status", "resolved")

	if result.Error != nil {
		log.Error("Ошибка при поиске и обновлении задачи на удаление", zap.String("uuid", entityUUID), zap.Error(result.Error))
		return
	}

	if result.RowsAffected > 0 {
		log.Info("Найдена и закрыта связанная задача на удаление из ServiceDesk", zap.String("uuid", entityUUID))
	}
}

// getLocalEntities извлекает из БД мапу с минимальной информацией о локальных сущностях.
func (s *sdeskSyncServiceImpl) getLocalEntities(ctx context.Context, metaClass string) (map[string]localEntityInfo, error) {
	infoMap := make(map[string]localEntityInfo)
	var err error
	switch metaClass {
	case "ou$company":
		entities, e := s.companyRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$Server":
		entities, e := s.serverRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$Workstation":
		entities, e := s.workstationRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$FR":
		entities, e := s.frRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	default:
		return nil, fmt.Errorf("неизвестный metaClass: %s", metaClass)
	}
	return infoMap, err
}

// processCreationsInParallel создает воркер-пул для создания сущностей.
func (s *sdeskSyncServiceImpl) processCreationsInParallel(ctx context.Context, metaClass string, toCreate []string, log *zap.Logger) {
	var wg sync.WaitGroup
	tasks := make(chan string, len(toCreate))

	for i := 0; i < s.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
				select {
				case <-ctx.Done():
					return
				default:
					details, err := s.sdClient.FetchEntityDetails(ctx, uuid, metaClass)
					if err != nil {
						if !errors.Is(err, context.Canceled) {
							log.Error("Не удалось получить детали для новой сущности", zap.String("uuid", uuid), zap.Error(err))
						}
						continue
					}
					s.createEntity(ctx, metaClass, details, log)
				}
			}
		}()
	}

	for _, uuid := range toCreate {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
}

// processUpdatesInParallel создает воркер-пул для проверки и создания задач о конфликтах.
func (s *sdeskSyncServiceImpl) processUpdatesInParallel(ctx context.Context, metaClass string, toUpdate []string, log *zap.Logger) {
	var wg sync.WaitGroup
	tasks := make(chan string, len(toUpdate))

	for i := 0; i < s.cfg.WorkerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
				select {
				case <-ctx.Done():
					return
				default:
					details, err := s.sdClient.FetchEntityDetails(ctx, uuid, metaClass)
					if err != nil {
						if !errors.Is(err, context.Canceled) {
							log.Error("Не удалось получить детали для обновления сущности", zap.String("uuid", uuid), zap.Error(err))
						}
						continue
					}
					s.checkEntityAndCreateTaskIfNeeded(ctx, metaClass, uuid, details, log)
				}
			}
		}()
	}

	for _, uuid := range toUpdate {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
}

// createEntity маппит и создает новую сущность в БД.
func (s *sdeskSyncServiceImpl) createEntity(ctx context.Context, metaClass string, details map[string]interface{}, log *zap.Logger) {
	uuid, _ := details["UUID"].(string)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		switch metaClass {
		case "ou$company":
			company, mapErr := DataToCompany(ctx, details, s.sdClient, log)
			if mapErr != nil {
				return mapErr
			}
			return s.companyRepo.Create(ctx, tx, company)
		case "objectBase$Server":
			server, mapErr := DataToServer(details)
			if mapErr != nil {
				return mapErr
			}
			return s.serverRepo.Create(ctx, tx, server)
		case "objectBase$Workstation":
			ws, mapErr := DataToWorkstation(details)
			if mapErr != nil {
				return mapErr
			}
			return s.workstationRepo.Create(ctx, tx, ws)
		case "objectBase$FR":
			fr, mapErr := DataToFiscalRegister(details)
			if mapErr != nil {
				return mapErr
			}
			return s.frRepo.Create(ctx, tx, fr)
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка при создании сущности", zap.String("uuid", uuid), zap.Error(err))
	} else {
		log.Info("Сущность успешно создана", zap.String("uuid", uuid))
	}
}

// checkEntityAndCreateTaskIfNeeded выполняет сравнение данных и решает,
// нужно ли восстановить запись, создать задачу о конфликте или ничего не делать.
func (s *sdeskSyncServiceImpl) checkEntityAndCreateTaskIfNeeded(ctx context.Context, metaClass, uuid string, details map[string]interface{}, log *zap.Logger) {
	var updates map[string]interface{}
	var diffLog []zap.Field
	var currentEntity interface{}

	// 1. Получаем текущую и новую версии сущности для сравнения
	switch metaClass {
	case "ou$company":
		newData, mapErr := DataToCompany(ctx, details, s.sdClient, log)
		if mapErr != nil {
			log.Error("Ошибка маппинга компании", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.companyRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущую компанию", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getCompanyDiff(currentData, newData)

	case "objectBase$Server":
		newData, mapErr := DataToServer(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга сервера", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.serverRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущий сервер", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getServerDiff(currentData, newData)

	case "objectBase$Workstation":
		newData, mapErr := DataToWorkstation(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга рабочей станции", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.workstationRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущую станцию", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getWorkstationDiff(currentData, newData)

	case "objectBase$FR":
		newData, mapErr := DataToFiscalRegister(details)
		if mapErr != nil {
			log.Error("Ошибка маппинга ФР", zap.String("uuid", uuid), zap.Error(mapErr))
			return
		}
		currentData, getErr := s.frRepo.GetByUUIDUnscoped(ctx, uuid)
		if getErr != nil || currentData == nil {
			log.Error("Не удалось получить текущий ФР", zap.String("uuid", uuid), zap.Error(getErr))
			return
		}
		currentEntity = currentData
		updates, diffLog = getFiscalRegisterDiff(currentData, newData)

	default:
		log.Warn("Неизвестный metaClass для проверки", zap.String("metaClass", metaClass))
		return
	}

	// 2. Принимаем решение на основе найденных изменений
	if len(updates) == 0 {
		s.resolveConflictTaskIfNeeded(ctx, uuid, log)
		return
	}

	_, isRestorationOnly := updates["deleted_at"]
	if len(updates) == 1 && isRestorationOnly {
		// Сценарий 1: Только восстановление. Выполняем автоматически.
		log.Info("Обнаружена восстановленная в SD сущность. Автоматическое восстановление.", append(diffLog, zap.String("uuid", uuid))...)
		s.performUpdate(ctx, metaClass, uuid, updates, log)
	} else {
		// Сценарий 2: Есть другие расхождения. Создаем задачу.
		log.Warn("Обнаружено расхождение данных между локальной БД и ServiceDesk. Создание задачи.", append(diffLog, zap.String("uuid", uuid))...)
		s.createConflictTask(ctx, metaClass, uuid, currentEntity, details, diffLog, log)
	}
}

// performUpdate выполняет обновление сущности в БД.
func (s *sdeskSyncServiceImpl) performUpdate(ctx context.Context, metaClass, uuid string, updates map[string]interface{}, log *zap.Logger) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		switch metaClass {
		case "ou$company":
			_, err := s.companyRepo.Update(ctx, tx, uuid, updates)
			return err
		case "objectBase$Server":
			_, err := s.serverRepo.Update(ctx, tx, uuid, updates)
			return err
		case "objectBase$Workstation":
			_, err := s.workstationRepo.Update(ctx, tx, uuid, updates)
			return err
		case "objectBase$FR":
			_, err := s.frRepo.Update(ctx, tx, uuid, updates)
			return err
		}
		return errors.New("неизвестный metaClass для транзакции обновления")
	})
	if err != nil {
		log.Error("Ошибка при автоматическом восстановлении сущности", zap.String("uuid", uuid), zap.Error(err))
	}
}

// createConflictTask создает задачу о расхождении данных.
func (s *sdeskSyncServiceImpl) createConflictTask(ctx context.Context, metaClass, uuid string, currentEntity interface{}, remoteDetails map[string]interface{}, diffLog []zap.Field, log *zap.Logger) {
	detailsMap := make(map[string]interface{})
	diffs := make(map[string]string)
	for _, field := range diffLog {
		if field.Key == "status" && field.String == "deleted -> restored" {
			continue
		}
		diffs[field.Key] = field.String
	}

	if len(diffs) == 0 {
		return
	}

	detailsMap["conflicts"] = diffs
	detailsMap["local_entity"] = currentEntity
	detailsMap["remote_entity"] = remoteDetails
	detailsJSON, _ := json.Marshal(detailsMap)

	entityType := metaClass
	if parts := strings.Split(metaClass, "$"); len(parts) > 1 {
		entityType = parts[1]
	}
	comment := fmt.Sprintf("Обнаружено расхождение данных для сущности '%s' (%s). Требуется ручная сверка.", uuid, entityType)

	task := models.ReconciliationTask{
		TaskType:   "data_conflict",
		EntityType: entityType,
		EntityUUID: uuid,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    comment,
	}

	err := s.db.WithContext(ctx).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", uuid, "data_conflict").
		FirstOrCreate(&task).Error

	if err != nil {
		log.Error("Не удалось создать или найти задачу о конфликте данных", zap.String("uuid", uuid), zap.Error(err))
	}
}

// resolveConflictTaskIfNeeded автоматически закрывает задачу, если конфликт устранен.
func (s *sdeskSyncServiceImpl) resolveConflictTaskIfNeeded(ctx context.Context, uuid string, log *zap.Logger) {
	result := s.db.WithContext(ctx).Model(&models.ReconciliationTask{}).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", uuid, "data_conflict").
		Updates(map[string]interface{}{
			"status":  "resolved",
			"comment": gorm.Expr("comment || ?", "\n[АВТОМАТИЧЕСКИ] Конфликт устранен, данные синхронизированы."),
		})

	if result.Error != nil {
		log.Error("Ошибка при попытке автоматического разрешения задачи о конфликте", zap.String("uuid", uuid), zap.Error(result.Error))
		return
	}

	if result.RowsAffected > 0 {
		log.Info("Конфликт данных устранен. Существующая задача автоматически разрешена.", zap.String("uuid", uuid))
	}
}

// --- Вспомогательные функции для сравнения (diff) ---

// formatDiffValue безопасно форматирует значение (включая nil-указатели) для логирования.
func formatDiffValue(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return "<nil>"
		}
		return fmt.Sprintf("'%v'", val.Elem().Interface())
	}
	return fmt.Sprintf("'%v'", v)
}

// compareAndLog - универсальная функция для сравнения полей и логирования расхождений.
// ИСПРАВЛЕНИЕ: Дженерик-тип изменен на comparable для корректной работы оператора !=
func compareAndLog[T comparable](updates map[string]interface{}, diffs *[]zap.Field, key string, current, new *T) {
	currentVal := reflect.ValueOf(current)
	newVal := reflect.ValueOf(new)

	isCurrentNil := current == nil || currentVal.IsNil()
	isNewNil := new == nil || newVal.IsNil()

	if isCurrentNil && isNewNil {
		return
	}

	if isCurrentNil != isNewNil {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
		return
	}

	if *current != *new {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
	}
}

// compareTimeAndLog - специальная функция для сравнения time.Time.
func compareTimeAndLog(updates map[string]interface{}, diffs *[]zap.Field, key string, current, new *time.Time) {
	if current == nil && new == nil {
		return
	}
	if (current == nil && new != nil) || (current != nil && new == nil) || (current != nil && new != nil && !current.Equal(*new)) {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
	}
}

// getCompanyDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только UI-значимые поля.
func getCompanyDiff(current *models.Company, new *models.Company) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareAndLog(updates, &diffs, "title", current.Title, new.Title)
	compareAndLog(updates, &diffs, "address", current.Address, new.Address)
	compareAndLog(updates, &diffs, "active_contract", current.ActiveContract, new.ActiveContract)

	// Сравниваем JSON-поля как строки
	if string(current.ContractInfo) != string(new.ContractInfo) {
		updates["contract_info"] = new.ContractInfo
		diffs = append(diffs, zap.String("contract_info", fmt.Sprintf("'%s' -> '%s'", string(current.ContractInfo), string(new.ContractInfo))))
	}
	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

// getServerDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только указанные поля.
func getServerDiff(current *models.Server, new *models.Server) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareAndLog(updates, &diffs, "unique_id", current.UniqueID, new.UniqueID)
	compareAndLog(updates, &diffs, "rdp", current.RDP, new.RDP)
	compareAndLog(updates, &diffs, "server_version", current.ServerVersion, new.ServerVersion)

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

// getWorkstationDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только ID удаленного доступа.
func getWorkstationDiff(current *models.Workstation, new *models.Workstation) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareAndLog(updates, &diffs, "teamviewer", current.Teamviewer, new.Teamviewer)
	compareAndLog(updates, &diffs, "anydesk", current.Anydesk, new.Anydesk)
	compareAndLog(updates, &diffs, "litemanager", current.Litemanager, new.Litemanager)

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

// getFiscalRegisterDiff - ОБНОВЛЕННАЯ ВЕРСИЯ. Сверяет только дату окончания ФН.
func getFiscalRegisterDiff(current *models.FiscalRegister, new *models.FiscalRegister) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)

	compareTimeAndLog(updates, &diffs, "fn_expire_date", current.FNExpireDate, new.FNExpireDate)

	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

go `
===== END sdesk_sync_service.go =====

internal/services/sdesk_sync_service_test.go
===== START sdesk_sync_service_test.go =====
go `
package services

import (
	"context"
	"etalon-server/internal/models"

	"github.com/stretchr/testify/mock"
	"gorm.io/gorm"
)

// MockCompanyRepo для тестирования SyncService
type MockCompanyRepo struct {
	mock.Mock
}

func (m *MockCompanyRepo) Create(ctx context.Context, tx *gorm.DB, company *models.Company) error {
	args := m.Called(ctx, tx, company)
	return args.Error(0)
}
func (m *MockCompanyRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, data map[string]interface{}) (bool, error) {
	args := m.Called(ctx, tx, uuid, data)
	return args.Bool(0), args.Error(1)
}
func (m *MockCompanyRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	return false, nil
}
func (m *MockCompanyRepo) GetByUUID(ctx context.Context, uuid string) (*models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) GetByUUIDs(ctx context.Context, uuids []string) ([]models.Company, error) {
	// В данном наборе тестов этот метод не вызывается,
	// поэтому мы можем просто вернуть пустые значения.
	args := m.Called(ctx, uuids)
	val, ok := args.Get(0).([]models.Company)
	if !ok {
		return nil, args.Error(1)
	}
	return val, args.Error(1)
}
func (m *MockCompanyRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error) {
	args := m.Called(ctx)
	// ВАЖНО: Тест теперь устарел, т.к. возвращаемый тип изменился.
	// Оставляем как есть для компиляции, но тест требует переписывания.
	val, ok := args.Get(0).(map[string]*models.Company)
	if !ok {
		return nil, args.Error(1)
	}
	return val, args.Error(1)
}

go `
===== END sdesk_sync_service_test.go =====

internal/services/server_polling_service.go
===== START server_polling_service.go =====
go `
// internal/services/server_polling_service.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	// ErrRateLimitExceeded возвращается, когда превышен лимит запросов на опрос для одного сервера.
	ErrRateLimitExceeded = errors.New("слишком много запросов на опрос сервера")
)

const (
	rateLimitCount  = 3
	rateLimitWindow = 2 * time.Minute
)

// ServerPollingService определяет интерфейс для фонового воркера опроса статусов серверов.
type ServerPollingService interface {
	Start(ctx context.Context)
	InstallLicense(ctx context.Context, serverUUID, uniqueID string) error
	PollSingleServer(ctx context.Context, serverUUID string) error // НОВЫЙ МЕТОД
}

type serverPollingServiceImpl struct {
	cfg        *config.Config
	logger     *zap.Logger
	db         *gorm.DB
	serverRepo repositories.ServerRepo
	rmsClient  utils.RMSClient

	// Поля для in-memory rate limiter'а
	rateLimiter   *sync.Mutex
	requestStamps map[string][]time.Time
}

// NewServerPollingService создает новый экземпляр сервиса.
func NewServerPollingService(cfg *config.Config, logger *zap.Logger, db *gorm.DB, serverRepo repositories.ServerRepo, rmsClient utils.RMSClient) ServerPollingService {
	return &serverPollingServiceImpl{
		cfg:           cfg,
		logger:        logger,
		db:            db,
		serverRepo:    serverRepo,
		rmsClient:     rmsClient,
		rateLimiter:   &sync.Mutex{},
		requestStamps: make(map[string][]time.Time),
	}
}

// PollSingleServer запускает асинхронную задачу опроса для одного сервера с проверкой rate limit.
func (s *serverPollingServiceImpl) PollSingleServer(ctx context.Context, serverUUID string) error {
	if !s.checkRateLimit(serverUUID) {
		return ErrRateLimitExceeded
	}

	server, err := s.serverRepo.GetByUUID(ctx, serverUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера из БД: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	s.logger.Info("Получен ручной запрос на опрос сервера", zap.String("uuid", serverUUID))

	// Запускаем реальную обработку в отдельной горутине, чтобы не блокировать ответ API.
	go func() {
		// Используем новый контекст, так как родительский контекст запроса может быть отменен после ответа.
		s.processServer(context.Background(), *server)
	}()

	return nil
}

// checkRateLimit проверяет, можно ли выполнить запрос для данного serverUUID.
func (s *serverPollingServiceImpl) checkRateLimit(serverUUID string) bool {
	s.rateLimiter.Lock()
	defer s.rateLimiter.Unlock()

	now := time.Now()
	limitWindowStart := now.Add(-rateLimitWindow)

	// Получаем историю запросов для этого UUID
	stamps := s.requestStamps[serverUUID]

	// Очищаем старые временные метки
	recentStamps := make([]time.Time, 0, len(stamps))
	for _, stamp := range stamps {
		if stamp.After(limitWindowStart) {
			recentStamps = append(recentStamps, stamp)
		}
	}

	// Проверяем лимит
	if len(recentStamps) >= rateLimitCount {
		s.logger.Warn("Превышен лимит запросов на опрос для сервера", zap.String("uuid", serverUUID))
		s.requestStamps[serverUUID] = recentStamps // Сохраняем очищенный список
		return false
	}

	// Добавляем текущую метку и разрешаем запрос
	recentStamps = append(recentStamps, now)
	s.requestStamps[serverUUID] = recentStamps
	return true
}

// Start запускает сервис в фоновом режиме.
// ИЗМЕНЕНИЕ: Переделано на тикер для корректного прерывания.
func (s *serverPollingServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера для опроса статусов серверов", zap.Duration("interval", 1*time.Minute))
	ticker := time.NewTicker(1 * time.Minute) // Пауза между циклами
	defer ticker.Stop()

	// Первый запуск сразу, не дожидаясь тикера
	s.runCycle(ctx)

	for {
		select {
		case <-ticker.C:
			s.runCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Остановка воркера для опроса статусов серверов...")
			return
		}
	}
}

// runCycle выполняет один цикл работы воркера.
func (s *serverPollingServiceImpl) runCycle(ctx context.Context) {
	s.logger.Info("Начало нового цикла опроса статусов серверов...")

	servers, err := s.serverRepo.FindForPolling(ctx, s.cfg.ServerPollingBatchSize, s.cfg.ServerPollingInterval)
	if err != nil {
		s.logger.Error("Не удалось получить список серверов для опроса", zap.Error(err))
		return
	}

	if len(servers) == 0 {
		s.logger.Info("Не найдено серверов, подлежащих опросу. Цикл завершен.")
		return
	}

	s.logger.Info("Найдено серверов для обработки", zap.Int("count", len(servers)))

	for _, server := range servers {
		select {
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание цикла опроса серверов.")
			return
		default:
			s.processServer(ctx, server)
			time.Sleep(2 * time.Second)
		}
	}
	s.logger.Info("Цикл опроса статусов серверов завершен.")
}

// processServer обрабатывает один сервер.
func (s *serverPollingServiceImpl) processServer(ctx context.Context, server models.Server) {
	log := s.logger.With(zap.String("server_uuid", utils.SafeStringDereference(server.ServiceDeskUUID)), zap.String("server_ip", utils.SafeStringDereference(server.IP)))

	if server.IP == nil || *server.IP == "" {
		log.Warn("У сервера отсутствует IP-адрес, опрос невозможен.")
		updates := map[string]interface{}{"last_polled_at": time.Now()}
		s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updates)
		return
	}

	var url string
	parts := strings.SplitN(*server.IP, ":", 2)
	host := parts[0]
	if len(parts) == 2 && (parts[1] == "443" || strings.Contains(*server.IP, "iiko.it") || strings.Contains(*server.IP, "syrve.online")) {
		url = "https://" + host
	} else {
		url = "http://" + *server.IP
	}

	info, err := s.rmsClient.GetServerMonitoringInfo(ctx, url)

	updates := make(map[string]interface{})
	updates["last_polled_at"] = time.Now()

	if err != nil {
		log.Warn("Не удалось получить информацию о сервере", zap.String("url", url), zap.Error(err))
		// Проверяем на специфическую ошибку DNS
		if strings.Contains(err.Error(), "no such host") {
			log.Info("Обнаружена ошибка DNS lookup. Сервер будет архивирован.")
			updates["status"] = "archived"
		} else {
			// Для всех остальных ошибок просто ставим 'offline'
			updates["status"] = "offline"
		}
	} else {
		log.Info("Информация о сервере успешно получена", zap.String("state", info.ServerState), zap.String("version", info.Version))
		updates["server_name"] = info.ServerName
		updates["server_edition"] = info.Edition
		updates["server_version"] = shortenVersion(info.Version)
		status := mapServerStateToStatus(info.ServerState)
		updates["status"] = status

		if status == "license" {
			s.createLicenseTask(ctx, server)
		}
	}

	_, updateErr := s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updates)
	if updateErr != nil {
		log.Error("Не удалось обновить информацию о сервере в базе данных", zap.Error(updateErr))
	} else {
		log.Info("Информация о сервере успешно сохранена в базе данных")
	}
}

// createLicenseTask создает задачу для администратора на установку лицензии.
func (s *serverPollingServiceImpl) createLicenseTask(ctx context.Context, server models.Server) {
	log := s.logger.With(zap.String("server_uuid", *server.ServiceDeskUUID))
	taskType := "license_installation_required"
	entityUUID := *server.ServiceDeskUUID

	var existingTask models.ReconciliationTask
	err := s.db.WithContext(ctx).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", entityUUID, taskType).
		First(&existingTask).Error

	if err == nil {
		return
	}
	if err != gorm.ErrRecordNotFound {
		log.Error("Ошибка при поиске существующей задачи на установку лицензии", zap.Error(err))
		return
	}

	detailsMap := map[string]string{
		"serverName":      utils.SafeStringDereference(server.ServerName),
		"serverUUID":      entityUUID,
		"suggestedUnique": utils.SafeStringDereference(server.UniqueID),
	}
	detailsJSON, _ := json.Marshal(detailsMap)

	task := models.ReconciliationTask{
		TaskType:   taskType,
		EntityType: "Server",
		EntityUUID: entityUUID,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    fmt.Sprintf("Сервер '%s' ожидает установку лицензии. Предлагаемый UniqueID: %s", utils.SafeStringDereference(server.ServerName), utils.SafeStringDereference(server.UniqueID)),
	}
	if createErr := s.db.WithContext(ctx).Create(&task).Error; createErr != nil {
		log.Error("Не удалось создать задачу на установку лицензии", zap.Error(createErr))
	} else {
		log.Info("Создана новая задача на установку лицензии")
	}
}

// InstallLicense - это метод-заглушка для ручного запуска установки лицензии.
func (s *serverPollingServiceImpl) InstallLicense(ctx context.Context, serverUUID, uniqueID string) error {
	server, err := s.serverRepo.GetByUUID(ctx, serverUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	s.logger.Info("ЗАГЛУШКА: Запущена установка лицензии",
		zap.String("server_uuid", serverUUID),
		zap.String("server_name", utils.SafeStringDereference(server.ServerName)),
		zap.String("unique_id", uniqueID),
	)

	return nil
}

// mapServerStateToStatus преобразует статус из ответа сервера в наш внутренний статус.
func mapServerStateToStatus(state string) string {
	switch state {
	case "STARTED_SUCCESSFULLY":
		return "active"
	case "WAITING_LICENSE":
		return "license"
	case "STARTING":
		return "starting"
	default:
		return "unknown"
	}
}

// shortenVersion обрезает версию до формата X.Y.Z
func shortenVersion(fullVersion string) string {
	if fullVersion == "" {
		return ""
	}
	re := regexp.MustCompile(`^(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(fullVersion)
	if len(matches) > 1 {
		return matches[1]
	}
	return fullVersion
}

go `
===== END server_polling_service.go =====

internal/services/servicedesk_client.go
===== START servicedesk_client.go =====
go `
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/config"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Атрибуты для запроса сущностей, как указано в требованиях.
var attrsMap = map[string]string{
	"ou$company":             "adress,UUID,title,lastModifiedDate,additionalName,parent,recipientAgreements",
	"objectBase$Server":      "UniqueID,Teamviewer,RDP,AnyDesk,UUID,IP,CabinetLink,DeviceName,lastModifiedDate,iikoVersion,description,nameforclient,owner,litemanagerID",
	"objectBase$Workstation": "Commentariy,Teamviewer,AnyDesk,DeviceName,litemanagerID,lastModifiedDate,UUID,owner",
	"objectBase$FR":          "UUID,ModelKKT,lastModifiedDate,owner,FFD,FRDownloader,RNKKT,KKTRegDate,FNExpireDate,LegalName,FRSerialNumber,FNNumber",
}

var minimalAttrsMap = map[string]string{
	"ou$company":             "UUID,lastModifiedDate,parent,recipientAgreements",
	"objectBase$Server":      "UUID,lastModifiedDate,owner",
	"objectBase$Workstation": "UUID,lastModifiedDate,owner",
	"objectBase$FR":          "UUID,lastModifiedDate,owner",
}

// AgreementDetailsDTO содержит детали контракта, полученные от ServiceDesk.
type AgreementDetailsDTO struct {
	State          string `json:"state"`
	StateStartTime string `json:"stateStartTime"`
	Services       []struct {
		UUID      string `json:"UUID"`
		Title     string `json:"title"`
		MetaClass string `json:"metaClass"`
	} `json:"services"`
	RecipientsOU []struct {
		UUID      string `json:"UUID"`
		Title     string `json:"title"`
		MetaClass string `json:"metaClass"`
	} `json:"recipientsOU"`
}

// ServiceDeskClient определяет интерфейс для взаимодействия с API ServiceDesk.
type ServiceDeskClient interface {
	FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error)
	FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error)
	FetchAgreementDetails(ctx context.Context, agreementUUID string) (*AgreementDetailsDTO, error)
}

// serviceDeskClientImpl реализует ServiceDeskClient.
type serviceDeskClientImpl struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	limiter    *rate.Limiter
	logger     *zap.Logger
	maxRetries int
}

// NewServiceDeskClient создает новый клиент для ServiceDesk.
func NewServiceDeskClient(cfg *config.Config, logger *zap.Logger) ServiceDeskClient {
	// ИЗМЕНЕНИЕ: Детальная настройка транспорта для http.Client
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // Таймаут на установку TCP-соединения
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &serviceDeskClientImpl{
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.RequestTimeout, // Общий таймаут на весь запрос
		},
		baseURL:    strings.TrimRight(cfg.ServiceDeskBaseURL, "/"),
		apiKey:     cfg.ServiceDeskKey,
		limiter:    rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		logger:     logger,
		maxRetries: cfg.MaxRetries,
	}
}

// AgreementContextKey - тип ключа для передачи кэша через контекст.
type AgreementContextKey string

const agreementCacheKey AgreementContextKey = "agreementCache"

// FetchAgreementDetails получает детальную информацию о контракте по UUID.
// ИЗМЕНЕНИЕ: Логика кэширования теперь работает через контекст.
func (s *serviceDeskClientImpl) FetchAgreementDetails(ctx context.Context, agreementUUID string) (*AgreementDetailsDTO, error) {
	// 1. Проверяем кэш в контексте
	if cache, ok := ctx.Value(agreementCacheKey).(map[string]*AgreementDetailsDTO); ok {
		if cachedDetails, found := cache[agreementUUID]; found {
			s.logger.Debug("Детали контракта взяты из кэша контекста", zap.String("agreementUUID", agreementUUID))
			return cachedDetails, nil
		}
	}

	// 2. Если в кэше нет, делаем запрос
	url := fmt.Sprintf("%s/get/%s?accessKey=%s&attrs=state,stateStartTime,services,recipientsOU", s.baseURL, agreementUUID, s.apiKey)

	var response AgreementDetailsDTO
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response)
	if err != nil {
		return nil, err
	}

	// 3. Сохраняем в кэш контекста, если он есть
	if cache, ok := ctx.Value(agreementCacheKey).(map[string]*AgreementDetailsDTO); ok {
		cache[agreementUUID] = &response
		s.logger.Debug("Детали контракта получены по API и сохранены в кэш контекста", zap.String("agreementUUID", agreementUUID))
	}

	return &response, nil
}

// FetchEntityList получает список сущностей указанного метакласса.
func (s *serviceDeskClientImpl) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	attrs := minimalAttrsMap[metaClass]
	if full {
		attrs = attrsMap[metaClass]
	}

	// Все параметры в URL. Тело запроса будет пустым.
	url := fmt.Sprintf("%s/find/%s?accessKey=%s&attrs=%s", s.baseURL, metaClass, s.apiKey, attrs)

	var responseList []map[string]interface{}

	// Передаем nil в качестве тела запроса.
	err := s.doWithRetry(ctx, http.MethodPost, url, nil, &responseList)
	if err != nil {
		return nil, err
	}

	return responseList, nil
}

// FetchEntityDetails получает детальную информацию о сущности по UUID.
func (s *serviceDeskClientImpl) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	attrs, ok := attrsMap[metaClass]
	if !ok {
		return nil, fmt.Errorf("unknown metaclass: %s", metaClass)
	}

	url := fmt.Sprintf("%s/get/%s?accessKey=%s&attrs=%s", s.baseURL, uuid, s.apiKey, attrs)

	var response map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// CheckAgreementActive проверяет, активен ли договор.
func (s *serviceDeskClientImpl) CheckAgreementActive(ctx context.Context, agreementUUID string) (bool, error) {
	url := fmt.Sprintf("%s/get/%s?accessKey=%s&attrs=state,UUID", s.baseURL, agreementUUID, s.apiKey)

	var response struct {
		State string `json:"state"`
	}

	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response)
	if err != nil {
		return false, err
	}

	return response.State == "active", nil
}

// doWithRetry выполняет HTTP-запрос с политикой повторов.
func (s *serviceDeskClientImpl) doWithRetry(ctx context.Context, method, url string, body io.Reader, target interface{}) error {
	var err error
	for i := 0; i < s.maxRetries; i++ {
		if err = s.limiter.Wait(ctx); err != nil {
			return err // Контекст отменен
		}

		req, reqErr := http.NewRequestWithContext(ctx, method, url, body)
		if reqErr != nil {
			return fmt.Errorf("failed to create request: %w", reqErr)
		}
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, doErr := s.client.Do(req)
		if doErr != nil {
			err = fmt.Errorf("request failed: %w", doErr)
			s.logger.Warn("Request failed, retrying...", zap.Error(err), zap.Int("attempt", i+1))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond) // Экспоненциальная задержка
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			err = fmt.Errorf("service desk api error: status %d, body: %s", resp.StatusCode, string(bodyBytes))
			if resp.StatusCode < 500 { // 4xx ошибки не повторяем
				return err
			}
			s.logger.Warn("Server error from ServiceDesk, retrying...", zap.Error(err), zap.Int("attempt", i+1))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		if decodeErr := json.NewDecoder(resp.Body).Decode(target); decodeErr != nil {
			return fmt.Errorf("failed to decode response: %w", decodeErr)
		}

		return nil // Успех
	}
	return fmt.Errorf("request failed after %d retries: %w", s.maxRetries, err)
}

go `
===== END servicedesk_client.go =====

internal/utils/helpers.go
===== START helpers.go =====
go `
package utils

import (
	"fmt"
	"net"
	"regexp"
	"time"
)

// TimeLayoutServiceDesk определяет формат времени, используемый в ServiceDesk.
const TimeLayoutServiceDesk = "2006.01.02 15:04:05"

// TimeLayoutAgent определяет формат времени, используемый в JSON от агентов.
const TimeLayoutAgent = "2006-01-02 15:04:05"

// Regex для поиска любых символов, кроме цифр.
var nonDigitRegex = regexp.MustCompile(`\D`)

// ParseServiceDeskTime парсит строку времени из ServiceDesk.
// Возвращает nil, если строка пустая или не может быть распарсена.
func ParseServiceDeskTime(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	t, err := time.Parse(TimeLayoutServiceDesk, dateStr)
	if err != nil {
		return nil
	}
	return &t
}

// ParseAgentTime парсит строку времени из файла агента.
func ParseAgentTime(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	t, err := time.Parse(TimeLayoutAgent, dateStr)
	if err != nil {
		// Попробуем также формат ServiceDesk на всякий случай
		t, err2 := time.Parse(TimeLayoutServiceDesk, dateStr)
		if err2 != nil {
			return nil
		}
		return &t
	}
	return &t
}

// FormatFFDVersion преобразует версию ФФД из формата агента в эталонный.
func FormatFFDVersion(rawVersion string) string {
	switch rawVersion {
	case "120":
		return "1.2"
	case "105":
		return "1.05"
	default:
		// Возвращаем как есть, если формат неизвестен
		return rawVersion
	}
}

// SafeStringDereference безопасно разыменовывает указатель на строку.
func SafeStringDereference(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// NormalizeRNKKT очищает регистрационный номер ККТ, оставляя только цифры.
// "0007 2066 3405 9671" -> "0007206634059671"
func NormalizeRNKKT(rnm string) string {
	return nonDigitRegex.ReplaceAllString(rnm, "")
}

// FormatRNKKT форматирует чистый РН ККТ для вывода, добавляя пробелы.
// "0007206634059671" -> "0007 2066 3405 9671"
func FormatRNKKT(rnm string) string {
	if len(rnm) != 16 {
		return rnm // Возвращаем как есть, если длина некорректна
	}
	return rnm[0:4] + " " + rnm[4:8] + " " + rnm[8:12] + " " + rnm[12:16]
}

// IsPrivateIP проверяет, является ли IP-адрес приватным (локальным).
func IsPrivateIP(ipStr string) (bool, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false, fmt.Errorf("некорректный IP-адрес: %s", ipStr)
	}

	// Проверяем на соответствие стандартным диапазонам приватных сетей
	_, private24, _ := net.ParseCIDR("10.0.0.0/8")
	_, private20, _ := net.ParseCIDR("172.16.0.0/12")
	_, private16, _ := net.ParseCIDR("192.168.0.0/16")

	return private24.Contains(ip) || private20.Contains(ip) || private16.Contains(ip), nil
}

go `
===== END helpers.go =====

internal/utils/rms_client.go
===== START rms_client.go =====
go `
// internal/utils/rms_client.go
// internal/utils/rms_client.go
package utils

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ServerInfoXML структура для парсинга XML-ответа от сервера iikoRMS
type ServerInfoXML struct {
	XMLName     xml.Name `xml:"r"`
	ServerName  string   `xml:"serverName"`
	Version     string   `xml:"version"`
	Edition     string   `xml:"edition"`
	ServerState string   `xml:"serverState"`
}

// RMSClient определяет интерфейс для взаимодействия с RMS API.
type RMSClient interface {
	GetServerMonitoringInfo(ctx context.Context, serverURL string) (*ServerInfoXML, error)
}

type rmsClientImpl struct {
	httpClient *http.Client
	logger     *zap.Logger
}

// NewRMSClient создает новый экземпляр клиента для RMS.
func NewRMSClient(timeout time.Duration, logger *zap.Logger) RMSClient {
	return &rmsClientImpl{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// GetServerMonitoringInfo получает статус и информацию о сервере.
// ИСПРАВЛЕНО: Теперь в первую очередь парсит XML.
func (c *rmsClientImpl) GetServerMonitoringInfo(ctx context.Context, serverURL string) (*ServerInfoXML, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/resto/get_server_info.jsp?encoding=UTF-8", serverURL), nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать GET-запрос: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось выполнить GET-запрос для получения информации о сервере: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку при получении информации: %s", resp.Status))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ответ от сервера: %w", err)
	}

	var info ServerInfoXML
	if err := xml.Unmarshal(body, &info); err != nil {
		// Попытка fallback на JSON, если XML не удался
		c.logger.Warn("Не удалось распарсить XML, попытка распарсить как JSON", zap.String("server_url", serverURL), zap.Error(err))
		var jsonInfo struct {
			ServerName  string `json:"serverName"`
			Edition     string `json:"edition"`
			Version     string `json:"version"`
			ServerState string `json:"serverState"`
		}
		if jsonErr := json.Unmarshal(body, &jsonInfo); jsonErr == nil {
			info.ServerName = jsonInfo.ServerName
			info.Edition = jsonInfo.Edition
			info.Version = jsonInfo.Version
			info.ServerState = jsonInfo.ServerState
		} else {
			return nil, fmt.Errorf("не удалось разобрать ответ ни как XML, ни как JSON: %w", err)
		}
	}

	return &info, nil
}

/*
 =================================================================================
  ОТНОСИТСЯ К СТАРОЙ ЛОГИКЕ
  ПОЛУЧЕНИЯ CRMID.
  ОН МОЖЕТ ПОНАДОБИТЬСЯ В БУДУЩЕМ ДЛЯ ДРУГИХ ЗАДАЧ.
 =================================================================================

// Структуры для парсинга XML-ответов от сервера iikoRMS
type ServerInfo struct {
	XMLName xml.Name `xml:"r"`
	Version string   `xml:"version"`
	Edition string   `xml:"edition"`
}

type LicenseInfoResponse struct {
	XMLName           xml.Name `xml:"result"`
	CrmOrganizationId string   `xml:"licenseInfo>licenseData>r>crmOrganizationId"`
	SerialNumber      string   `xml:"licenseInfo>licenseData>r>serialNumber"`
}

// GetCRMid подключается к серверу iikoRMS и возвращает его CRMid.
// Поддерживает попытку с fallbackPassword в случае ошибки аутентификации.
func (c *rmsClientImpl) GetCRMid(ctx context.Context, serverURL, login, password, fallbackPassword string) (string, error) {
	log := c.logger.With(zap.String("server_url", serverURL))

	// Первая попытка с основным паролем
	crmid, err := c.fetchCRMid(ctx, serverURL, login, password, log)
	if err == nil {
		return crmid, nil
	}

	// Проверяем, является ли ошибка ошибкой аутентификации (401/403)
	var httpErr *HttpError
	if asHttpErr, ok := err.(*HttpError); ok {
		httpErr = asHttpErr
	}

	if (httpErr != nil && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)) && fallbackPassword != "" {
		log.Warn("Первая попытка аутентификации не удалась, пробую с запасным паролем.")
		// Вторая попытка с запасным паролем
		return c.fetchCRMid(ctx, serverURL, login, fallbackPassword, log)
	}

	return "", err
}

func (c *rmsClientImpl) fetchCRMid(ctx context.Context, serverURL, login, password string, log *zap.Logger) (string, error) {
	// 1. Получаем информацию о сервере (версия, редакция)
	info, err := c.getServerInfoXML(ctx, serverURL)
	if err != nil {
		return "", err
	}

	// 2. Хэшируем пароль по алгоритму SHA1
	hasher := sha1.New()
	hasher.Write([]byte(password))
	passwordHash := hex.EncodeToString(hasher.Sum(nil))

	// 3. Формируем тело и заголовки для запроса
	endpoint := fmt.Sprintf("%s/resto/services/licensing?methodName=getForceDeveloperSandboxModeInfo&", serverURL)
	xmlBody := `<?xml version="1.0" encoding="utf-8"?><args><entities-version>1</entities-version><client-type>BACK</client-type><enable-warnings>false</enable-warnings><client-call-id>30264dfd-570d-46c0-81b8-6bef9da5a2c9</client-call-id><license-hash>-1938788177</license-hash><restrictions-state-hash>5761</restrictions-state-hash><obtained-license-connections-ids /><request-watchdog-check-results>true</request-watchdog-check-results><use-raw-entities>true</use-raw-entities></args>`

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBufferString(xmlBody))
	if err != nil {
		return "", fmt.Errorf("не удалось создать POST-запрос: %w", err)
	}

	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-Resto-LoginName", login)
	req.Header.Set("X-Resto-PasswordHash", passwordHash)
	req.Header.Set("X-Resto-BackVersion", info.Version)
	req.Header.Set("X-Resto-AuthType", "BACK")
	req.Header.Set("X-Resto-ServerEdition", info.Edition)

	// 4. Отправляем запрос
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при отправке запроса на получение CRMid: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать ответ сервера: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку при получении CRMid: %s. Ответ: %s", resp.Status, string(responseBody)))
	}

	// 5. Парсим ответ
	var licenseInfo LicenseInfoResponse
	cleanXML := strings.ReplaceAll(string(responseBody), "&lt;", "<")
	cleanXML = strings.ReplaceAll(cleanXML, "&gt;", ">")

	if err := xml.Unmarshal([]byte(cleanXML), &licenseInfo); err != nil {
		return "", fmt.Errorf("не удалось разобрать XML-ответ с лицензией: %w. Ответ: %s", err, string(responseBody))
	}

	if licenseInfo.CrmOrganizationId == "" {
		return "", fmt.Errorf("не удалось найти CRMid в ответе сервера. Ответ: %s", string(responseBody))
	}

	return licenseInfo.CrmOrganizationId, nil
}

func (c *rmsClientImpl) getServerInfoXML(ctx context.Context, serverURL string) (*ServerInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/resto/get_server_info.jsp?encoding=UTF-8", serverURL), nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать GET-запрос: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось выполнить GET-запрос для получения информации о сервере: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку при получении информации: %s", resp.Status))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ответ от сервера: %w", err)
	}

	var info ServerInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("не удалось разобрать XML с информацией о сервере: %w", err)
	}

	info.Edition = strings.Replace(info.Edition, "default", "IIKO_RMS", -1)
	info.Edition = strings.Replace(info.Edition, "chain", "IIKO_CHAIN", -1)

	return &info, nil
}
*/

// HttpError специальный тип ошибки для HTTP-ответов
type HttpError struct {
	StatusCode int
	Message    string
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func NewHttpError(code int, message string) *HttpError {
	return &HttpError{StatusCode: code, Message: message}
}

go `
===== END rms_client.go =====

internal/validators/validators.go
===== START validators.go =====
go `
package validators

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	uniqueIDRegex         = regexp.MustCompile(`^\d{3}-\d{3}-\d{3}$`)
	remoteAccessIDRegex   = regexp.MustCompile(`(\d\s*){9,10}`)
	LiteManagerIDRegex    = regexp.MustCompile(`MH_\d{5}`)
	iikoCloudDomainRegex  = regexp.MustCompile(`(?i)(?:https?://)?(?:[a-z0-9-]+\.)?([a-z0-9-]+\.iiko\.it)`)
	syrveCloudDomainRegex = regexp.MustCompile(`(?i)(?:https?://)?(?:[a-z0-9-]+\.)?([a-z0-9-]+\.syrve\.online)`)
)

// ValidateUniqueID проверяет формат UniqueID.
func ValidateUniqueID(uniqueID string) *string {
	if uniqueIDRegex.MatchString(uniqueID) {
		return &uniqueID
	}
	return nil
}

// ValidateRemoteAccessID находит и нормализует ID удаленного доступа (TeamViewer, Anydesk).
func ValidateRemoteAccessID(raw string) *string {
	found := remoteAccessIDRegex.FindString(raw)
	if found == "" {
		return nil
	}
	normalized := strings.ReplaceAll(found, " ", "")
	return &normalized
}

// ExtractLiteManagerID извлекает LiteManager ID из данных.
func ExtractLiteManagerID(data map[string]interface{}, fallback string) *string {
	if id, ok := data["litemanagerID"].(string); ok && LiteManagerIDRegex.MatchString(id) {
		return &id
	}
	found := LiteManagerIDRegex.FindString(fallback)
	if found != "" {
		return &found
	}
	return nil
}

// DetermineCompanyTypeFromIP определяет тип компании ("syrve" или "iiko") по IP/домену.
func DetermineCompanyTypeFromIP(ip string) string {
	if strings.Contains(strings.ToLower(ip), "syrve") {
		return "syrve"
	}
	return "iiko"
}

// ValidateCabinetLink извлекает clientId из ссылки на личный кабинет.
func ValidateCabinetLink(raw string, companyType string) string {
	lastIndex := strings.LastIndex(raw, "=")

	// Если "=" не найден или это последний символ в строке
	if lastIndex == -1 || lastIndex == len(raw)-1 {
		return "N/A"
	}

	// Берем подстроку после последнего "="
	idStr := raw[lastIndex+1:]

	// Дополнительно очищаем от возможных якорей (#) или других параметров (&)
	if anchorIndex := strings.Index(idStr, "#"); anchorIndex != -1 {
		idStr = idStr[:anchorIndex]
	}
	if paramIndex := strings.Index(idStr, "&"); paramIndex != -1 {
		idStr = idStr[:paramIndex]
	}

	// Проверяем, является ли полученная строка числом
	if _, err := strconv.Atoi(idStr); err == nil {
		return idStr
	}

	return "N/A"
}

// ValidateIPAddress валидирует и нормализует IP-адрес или домен.
// НОВАЯ РЕАЛИЗАЦИЯ: Использует пакет net/url для надежного парсинга.
func ValidateIPAddress(raw string) *string {
	if raw == "" {
		return nil
	}
	raw = strings.TrimSpace(raw)

	// 1. Приоритетная проверка на специальные облачные домены.
	if matches := iikoCloudDomainRegex.FindStringSubmatch(raw); len(matches) > 1 {
		res := fmt.Sprintf("%s:443", matches[1])
		return &res
	}
	if matches := syrveCloudDomainRegex.FindStringSubmatch(raw); len(matches) > 1 {
		res := fmt.Sprintf("%s:443", matches[1])
		return &res
	}

	// 2. Используем url.Parse для надежного разбора.
	// Добавляем схему "http://", если она отсутствует, чтобы парсер корректно работал
	// с адресами вида "domain.com:8080".
	parseableURL := raw
	if !strings.Contains(parseableURL, "://") {
		parseableURL = "http://" + parseableURL
	}

	parsedURL, err := url.Parse(parseableURL)
	if err != nil {
		// Если даже с добавленной схемой парсинг не удался, считаем адрес невалидным.
		return nil
	}

	hostname := parsedURL.Hostname()
	port := parsedURL.Port()

	// Если не удалось извлечь хост, адрес невалидный.
	if hostname == "" {
		return nil
	}

	// 3. Формируем итоговую строку.
	var result string
	if port != "" {
		// Если порт был указан в исходной строке, используем его.
		result = fmt.Sprintf("%s:%s", hostname, port)
	} else {
		// Если порт не указан, используем порт по умолчанию 8080.
		// Это стандарт для локальных серверов iikoRMS.
		result = fmt.Sprintf("%s:8080", hostname)
	}

	return &result
}

go `
===== END validators.go =====

internal/validators/validators_test.go
===== START validators_test.go =====
go `
package validators

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRemoteAccessID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Valid ID with spaces", "123 456 789", stringPtr("123456789")},
		{"Valid 10-digit ID", "1234567890", stringPtr("1234567890")},
		{"Invalid short ID", "123 456", nil},
		{"ID with text", "AnyDesk: 987 654 321", stringPtr("987654321")},
		{"Empty string", "", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateRemoteAccessID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateIPAddress(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Valid IP without port", "192.168.1.1", stringPtr("192.168.1.1:8080")},
		{"Valid IP with port", "8.8.8.8:53", stringPtr("8.8.8.8:53")},
		{"Iiko cloud domain", "https://my-res.iiko.it", stringPtr("my-res.iiko.it:443")},
		{"Syrve cloud domain", "https://my-res.syrve.online/api", stringPtr("my-res.syrve.online:443")},
		{"Local domain without port", "localhost", stringPtr("localhost:8080")},
		{"Local domain with port", "db.local:5432", stringPtr("db.local:5432")},
		{"Invalid IP", "999.999.999.999", nil},
		{"Just text", "not an ip", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateIPAddress(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateUniqueID(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected *string
	}{
		{"Корректный ID", "123-456-789", stringPtr("123-456-789")},
		{"Некорректный ID с буквами", "123-abc-789", nil},
		{"Некорректный формат", "123456789", nil},
		{"Пустая строка", "", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateUniqueID(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestExtractLiteManagerID(t *testing.T) {
	testCases := []struct {
		name     string
		data     map[string]interface{}
		fallback string
		expected *string
	}{
		{"ID в поле data", map[string]interface{}{"litemanagerID": "MH_12345"}, "", stringPtr("MH_12345")},
		{"ID в fallback строке", map[string]interface{}{}, "Какой-то текст с MH_54321 внутри", stringPtr("MH_54321")},
		{"ID отсутствует", map[string]interface{}{}, "Просто текст", nil},
		{"Некорректный ID в поле data", map[string]interface{}{"litemanagerID": "MH_123"}, "", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractLiteManagerID(tc.data, tc.fallback)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDetermineCompanyTypeFromIP(t *testing.T) {
	assert.Equal(t, "syrve", DetermineCompanyTypeFromIP("my.syrve.online"))
	assert.Equal(t, "iiko", DetermineCompanyTypeFromIP("my.iiko.it"))
	assert.Equal(t, "iiko", DetermineCompanyTypeFromIP("192.168.1.1"))
}

func TestValidateCabinetLink(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"Стандартный случай с clientId", "https://cabinet?clientId=12345", "12345"},
		{"Случай с параметром id в конце", "https://partners.iiko.ru/ru/cabinet/clients.html?mode=showOne&id=720846", "720846"},
		{"Случай с некорректным ключом (должен вернуть N/A)", "https://cabinet?client=12345", "12345"}, // Оставим это поведение, оно соответствует логике "берем последнее"
		{"URL без знака равно", "https://cabinet/clients", "N/A"},
		{"Параметр не является числом", "https://cabinet?id=abc", "N/A"},
		{"Параметр с якорем", "https://cabinet?id=54321#details", "54321"},
		{"Пустая строка", "", "N/A"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// В этой функции второй параметр companyType не используется, так что можно передать пустую строку
			assert.Equal(t, tc.expected, ValidateCabinetLink(tc.input, ""))
		})
	}
}

// Вспомогательная функция для тестов, чтобы создавать указатели на строки.
func stringPtr(s string) *string {
	return &s
}

go `
===== END validators_test.go =====

