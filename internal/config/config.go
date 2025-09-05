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
	// --- Общие настройки ---
	ServerPort         string
	DatabaseURL        string
	LogDir             string
	LogLevel           string
	DisableFileLogging bool
	RequestTimeout     time.Duration
	AllowedOrigins     []string

	// --- Настройки API ---
	AgentAPIKey      string
	SeederKey        string
	JWTSecret        string
	JWTExpirationMin int
	AdminUsername    string
	AdminPassword    string
	AdminFullName    string

	// --- Настройки ServiceDesk Client ---
	ServiceDeskBaseURL string
	ServiceDeskKey     string
	RateLimit          int
	MaxRetries         int
	ConcurrentRequests int
	ServiceDeskDryRun  bool

	// --- Настройки фоновых шлюзов (Gateways) ---

	// Шлюз синхронизации с ServiceDesk (сущности)
	EnableSDeskGateway bool
	SDeskSyncInterval  time.Duration

	// Шлюз синхронизации контрактов
	EnableContractGateway bool
	ContractSyncInterval  time.Duration

	// Шлюз опроса статусов серверов (RMS Polling)
	EnablePollingGateway   bool
	ServerPollingInterval  time.Duration
	ServerPollingBatchSize int
	RMSLogin               string
	RMSPassword1           string
	RMSPassword2           string

	// Шлюз для данных от агентов (FTP)
	EnableAgentFTPGateway bool
	AgentFTPInterval      time.Duration // Бывший ReconcileInterval
	FTPHost               string
	FTPUser               string
	FTPPassword           string
	FTPPort               string
	FTPPath               string
	FTPCachePath          string

	// Шлюз поиска дубликатов
	EnableDuplicatesGateway  bool
	DuplicatesSearchInterval time.Duration
}

// New загружает конфигурацию из файла .env и переменных окружения.
func New() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "http://localhost:5173")

	return &Config{
		// Общие
		ServerPort:         getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/etalon_db?sslmode=disable"),
		LogDir:             getEnv("LOG_DIR", "./logs"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		DisableFileLogging: getEnvAsBool("DISABLE_FILE_LOGGING", false),
		RequestTimeout:     time.Duration(getEnvAsInt("REQUEST_TIMEOUT_SEC", 15)) * time.Second,
		AllowedOrigins:     strings.Split(allowedOriginsStr, ","),

		// API
		AgentAPIKey:      getEnv("AGENT_API_KEY", ""),
		SeederKey:        getEnv("SEEDER_KEY", "super-secret-key-for-seeding"),
		JWTSecret:        getEnv("JWT_SECRET", "mhrcadmin994525"),
		JWTExpirationMin: getEnvAsInt("JWT_EXPIRATION_MIN", 1440), // 24 часа
		AdminUsername:    getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:    getEnv("ADMIN_PASSWORD", "mhrcadmin994525"),
		AdminFullName:    getEnv("ADMIN_FULLNAME", "Главный"),

		// ServiceDesk Client
		ServiceDeskBaseURL: getEnv("BASE_URL", "https://servicedesk.example.com"),
		ServiceDeskKey:     getEnv("SDKEY", ""),
		RateLimit:          getEnvAsInt("RATE_LIMIT", 45),
		MaxRetries:         getEnvAsInt("MAX_RETRIES", 3),
		ConcurrentRequests: getEnvAsInt("CONCURRENT_REQUESTS", 10),
		ServiceDeskDryRun:  getEnvAsBool("SD_DRY_RUN", false),

		// --- Шлюзы ---

		// ServiceDesk Gateway
		EnableSDeskGateway: getEnvAsBool("ENABLE_SDESK_GATEWAY", true),
		SDeskSyncInterval:  time.Duration(getEnvAsInt("SDESK_SYNC_INTERVAL_MIN", 10)) * time.Minute,

		// Contract Gateway
		EnableContractGateway: getEnvAsBool("ENABLE_CONTRACT_GATEWAY", true),
		ContractSyncInterval:  time.Duration(getEnvAsInt("CONTRACT_SYNC_INTERVAL_MIN", 30)) * time.Minute,

		// Server Polling Gateway
		EnablePollingGateway:   getEnvAsBool("ENABLE_POLLING_GATEWAY", true),
		ServerPollingInterval:  time.Duration(getEnvAsInt("SERVER_POLLING_INTERVAL_HOURS", 12)) * time.Hour,
		ServerPollingBatchSize: getEnvAsInt("SERVER_POLLING_BATCH_SIZE", 50),
		RMSLogin:               getEnv("RMS_LOGIN", ""),
		RMSPassword1:           getEnv("RMS_PASSWORD_1", ""),
		RMSPassword2:           getEnv("RMS_PASSWORD_2", ""),

		// Agent FTP Gateway
		EnableAgentFTPGateway: getEnvAsBool("ENABLE_AGENT_FTP_GATEWAY", true),
		AgentFTPInterval:      time.Duration(getEnvAsInt("AGENT_FTP_INTERVAL_MIN", 60)) * time.Minute,
		FTPHost:               getEnv("FTP_HOST", "localhost"),
		FTPUser:               getEnv("FTP_USER", "user"),
		FTPPassword:           getEnv("FTP_PASSWORD", "password"),
		FTPPort:               getEnv("FTP_PORT", "21"),
		FTPPath:               getEnv("FTP_PATH", "/"),
		FTPCachePath:          getEnv("FTP_CACHE_PATH", "./ftp_cache"),

		// Duplicates Gateway
		EnableDuplicatesGateway:  getEnvAsBool("ENABLE_DUPLICATES_GATEWAY", true),
		DuplicatesSearchInterval: time.Duration(getEnvAsInt("DUPLICATES_SEARCH_INTERVAL_HOURS", 24)) * time.Hour, // Раз в сутки по умолчанию
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
