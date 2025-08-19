package config

import (
	"log"
	"os"
	"strconv"
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
	FTPHost           string
	FTPUser           string
	FTPPassword       string
	FTPPort           string
	FTPPath           string
	FTPCachePath      string
	ReconcileInterval time.Duration

	// Секция для Zabbix
	ZabbixAPIURL   string
	ZabbixAPIToken string

	// Секция для CRMid Worker
	RMSLogin             string
	RMSPassword1         string
	RMSPassword2         string
	CRMidWorkerInterval  time.Duration
	CRMidWorkerBatchSize int

	// Секция настроек синхронизации с SD
	SDeskSyncInterval time.Duration
}

// New загружает конфигурацию из файла .env и переменных окружения.
func New() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

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

		// Новые параметры
		FTPHost:           getEnv("FTP_HOST", "localhost"),
		FTPUser:           getEnv("FTP_USER", "user"),
		FTPPassword:       getEnv("FTP_PASSWORD", "password"),
		FTPPort:           getEnv("FTP_PORT", "21"),
		FTPPath:           getEnv("FTP_PATH", "/"),
		FTPCachePath:      getEnv("FTP_CACHE_PATH", "./ftp_cache"),
		ReconcileInterval: time.Duration(getEnvAsInt("RECONCILE_INTERVAL_MIN", 60)) * time.Minute,

		// Параметры Zabbix
		ZabbixAPIURL:   getEnv("ZABBIX_API_URL", ""),
		ZabbixAPIToken: getEnv("ZABBIX_API_TOKEN", ""),

		// Параметры CRMid Worker
		RMSLogin:             getEnv("RMS_LOGIN", ""),
		RMSPassword1:         getEnv("RMS_PASSWORD_1", ""),
		RMSPassword2:         getEnv("RMS_PASSWORD_2", ""),
		CRMidWorkerInterval:  time.Duration(getEnvAsInt("CRMID_WORKER_INTERVAL_MIN", 60)) * time.Minute,
		CRMidWorkerBatchSize: getEnvAsInt("CRMID_WORKER_BATCH_SIZE", 100),

		// Параметры синхронизации с SD
		SDeskSyncInterval: time.Duration(getEnvAsInt("SDESK_SYNC_INTERVAL_MIN", 10)) * time.Minute,
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
