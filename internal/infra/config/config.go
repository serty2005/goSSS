// internal/config/config.go
package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config хранит всю конфигурацию приложения.
type Config struct {
	ServerPort         string
	DatabaseURL        string
	LogDir             string
	LogLevel           string
	DisableFileLogging bool
	RequestTimeout     time.Duration
	AllowedOrigins     []string

	AgentAPIKey      string
	SeederKey        string
	JWTSecret        string
	JWTExpirationMin int
	AdminUsername    string
	AdminPassword    string
	AdminFullName    string

	ServiceDeskBaseURL string
	ServiceDeskKey     string
	RateLimit          int
	MaxRetries         int
	ConcurrentRequests int
	ServiceDeskDryRun  bool

	EnableSDeskGateway bool
	SDeskSyncInterval  time.Duration
	TicketStoragePath  string

	EnableContractGateway bool
	ContractSyncInterval  time.Duration

	CommonContractID string

	EnablePollingGateway   bool
	ServerPollingInterval  time.Duration
	ServerPollingBatchSize int
	RMSLogin               string
	RMSPassword1           string
	RMSPassword2           string

	EnableAgentFTPGateway bool
	AgentFTPInterval      time.Duration
	FTPHost               string
	FTPUser               string
	FTPPassword           string
	FTPPort               string
	FTPPath               string
	FTPCachePath          string

	EnableDuplicatesGateway  bool
	DuplicatesSearchInterval time.Duration

	EnableFRDiscrepancyFinder  bool
	FRDiscrepancyCheckInterval time.Duration

	EnableStatusWorker   bool
	StatusWorkerInterval time.Duration
}

func New() *Config {
	if err := loadEnv(); err != nil {
		log.Printf("Failed to load .env (%v). Using environment variables.", err)
	}

	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "http://localhost:5173")

	return &Config{
		ServerPort:         getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/etalon_db?sslmode=disable"),
		LogDir:             getEnv("LOG_DIR", "./logs"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		DisableFileLogging: getEnvAsBool("DISABLE_FILE_LOGGING", false),
		RequestTimeout:     time.Duration(getEnvAsInt("REQUEST_TIMEOUT_SEC", 15)) * time.Second,
		AllowedOrigins:     strings.Split(allowedOriginsStr, ","),

		AgentAPIKey:      getEnv("AGENT_API_KEY", ""),
		SeederKey:        getEnv("SEEDER_KEY", "super-secret-key-for-seeding"),
		JWTSecret:        getEnv("JWT_SECRET", "mhrcadmin994525"),
		JWTExpirationMin: getEnvAsInt("JWT_EXPIRATION_MIN", 1440),
		AdminUsername:    getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:    getEnv("ADMIN_PASSWORD", "mhrcadmin994525"),
		AdminFullName:    getEnv("ADMIN_FULLNAME", "Главный"),

		ServiceDeskBaseURL: getEnv("BASE_URL", "https://servicedesk.example.com"),
		ServiceDeskKey:     getEnv("SDKEY", ""),
		RateLimit:          getEnvAsInt("RATE_LIMIT", 45),
		MaxRetries:         getEnvAsInt("MAX_RETRIES", 3),
		ConcurrentRequests: getEnvAsInt("CONCURRENT_REQUESTS", 10),
		ServiceDeskDryRun:  getEnvAsBool("SD_DRY_RUN", false),

		EnableSDeskGateway: getEnvAsBool("ENABLE_SDESK_GATEWAY", true),
		SDeskSyncInterval:  time.Duration(getEnvAsInt("SDESK_SYNC_INTERVAL_MIN", 10)) * time.Minute,
		TicketStoragePath:  getEnv("TICKET_STORAGE_PATH", "./storage/tickets"),

		EnableContractGateway: getEnvAsBool("ENABLE_CONTRACT_GATEWAY", true),
		ContractSyncInterval:  time.Duration(getEnvAsInt("CONTRACT_SYNC_INTERVAL_MIN", 30)) * time.Minute,

		CommonContractID: getEnv("COMMON_CONTRACT_ID", "common-contract"),

		EnablePollingGateway:   getEnvAsBool("ENABLE_POLLING_GATEWAY", true),
		ServerPollingInterval:  time.Duration(getEnvAsInt("SERVER_POLLING_INTERVAL_HOURS", 12)) * time.Hour,
		ServerPollingBatchSize: getEnvAsInt("SERVER_POLLING_BATCH_SIZE", 50),
		RMSLogin:               getEnv("RMS_LOGIN", ""),
		RMSPassword1:           getEnv("RMS_PASSWORD_1", ""),
		RMSPassword2:           getEnv("RMS_PASSWORD_2", ""),

		EnableAgentFTPGateway: getEnvAsBool("ENABLE_AGENT_FTP_GATEWAY", true),
		AgentFTPInterval:      time.Duration(getEnvAsInt("AGENT_FTP_INTERVAL_MIN", 60)) * time.Minute,
		FTPHost:               getEnv("FTP_HOST", "localhost"),
		FTPUser:               getEnv("FTP_USER", "user"),
		FTPPassword:           getEnv("FTP_PASSWORD", "password"),
		FTPPort:               getEnv("FTP_PORT", "21"),
		FTPPath:               getEnv("FTP_PATH", "/"),
		FTPCachePath:          getEnv("FTP_CACHE_PATH", "./ftp_cache"),

		EnableDuplicatesGateway:  getEnvAsBool("ENABLE_DUPLICATES_GATEWAY", true),
		DuplicatesSearchInterval: time.Duration(getEnvAsInt("DUPLICATES_SEARCH_INTERVAL_HOURS", 24)) * time.Hour,

		EnableFRDiscrepancyFinder:  getEnvAsBool("ENABLE_FR_DISCREPANCY_FINDER", true),
		FRDiscrepancyCheckInterval: time.Duration(getEnvAsInt("FR_DISCREPANCY_CHECK_INTERVAL_HOURS", 6)) * time.Hour,

		EnableStatusWorker:   getEnvAsBool("ENABLE_STATUS_WORKER", true),
		StatusWorkerInterval: time.Duration(getEnvAsInt("STATUS_WORKER_INTERVAL_MIN", 2)) * time.Minute,
	}
}

func loadEnv() error {
	envPath, err := findEnvPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}

	if bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}

	envMap, err := godotenv.UnmarshalBytes(data)
	if err != nil {
		return err
	}

	for key, value := range envMap {
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}

	log.Printf("Loaded .env from %s", envPath)
	return nil
}

func findEnvPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		envPath := filepath.Join(cwd, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	return "", os.ErrNotExist
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if valueStr := getEnv(key, ""); valueStr != "" {
		if value, err := strconv.Atoi(valueStr); err == nil {
			return value
		}
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	if valueStr := getEnv(key, ""); valueStr != "" {
		if value, err := strconv.ParseBool(valueStr); err == nil {
			return value
		}
	}
	return fallback
}
