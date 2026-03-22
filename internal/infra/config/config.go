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
	BrandName        string
	SeederKey        string
	JWTSecret        string
	JWTExpirationMin int
	AdminUsername    string
	AdminPassword    string
	AdminFullName    string

	AgentAdapterS3Enabled         bool
	AgentAdapterS3Endpoint        string
	AgentAdapterS3Region          string
	AgentAdapterS3Bucket          string
	AgentAdapterS3AccessKey       string
	AgentAdapterS3SecretKey       string
	AgentAdapterPublicBaseURL     string
	AgentAdapterCatalogKey        string
	AgentAdapterSyncInterval      time.Duration
	AgentAdapterDefaultChannel    string

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
	ContractIMAPHost      string
	ContractIMAPPort      int
	ContractIMAPUsername  string
	ContractIMAPPassword  string
	ContractIMAPMailbox   string
	ContractZipMaxBytes   int

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

	EnableBitrixGateway         bool
	BitrixBaseURL               string
	BitrixOriginatorID          string
	BitrixCategoryID            int
	BitrixRateLimitPerMin       int
	BitrixRateLimitBurst        int
	BitrixSyncInterval          time.Duration
	BitrixDictionarySyncEvery   time.Duration
	BitrixServicePointsIBlockID int
	EtalonTicketBaseURL         string
	BitrixWebhookEnabled        bool
	BitrixWebhookAppToken       string
	BitrixEventsStreamName      string
	BitrixEventsConsumerGroup   string
	BitrixIncomingParallelism   int
	BitrixIncomingRetryBase     time.Duration
	BitrixIncomingRetryMax      time.Duration
	BitrixIncomingMaxAttempts   int
	BitrixSuppressTTL           time.Duration
	BitrixIntegrationUserID     int64

	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

func New() *Config {
	if err := loadEnv(); err != nil {
		log.Printf("Failed to load .env (%v). Using environment variables.", err)
	}

	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "http://localhost:5173")
	bitrixBaseURL := strings.TrimSpace(getEnv("BITRIX_BASE_URL", ""))
	bitrixIntegrationUserID := int64(getEnvAsInt("BITRIX_INTEGRATION_USER_ID", 0))
	if bitrixIntegrationUserID <= 0 {
		bitrixIntegrationUserID = detectBitrixIntegrationUserID(bitrixBaseURL)
	}

	contractSyncHours := getEnvAsInt("CONTRACT_SYNC_INTERVAL_HOURS", 0)
	if contractSyncHours <= 0 {
		legacyMinutes := getEnvAsInt("CONTRACT_SYNC_INTERVAL_MIN", 30)
		contractSyncHours = max(1, legacyMinutes/60)
		if legacyMinutes > 0 && contractSyncHours == 0 {
			contractSyncHours = 1
		}
	}

	return &Config{
		ServerPort:         getEnv("PORT", "8080"),
		DatabaseURL:        getEnv("DATABASE_URL", "postgres://user:password@localhost:5432/etalon_db?sslmode=disable"),
		LogDir:             getEnv("LOG_DIR", "./logs"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		DisableFileLogging: getEnvAsBool("DISABLE_FILE_LOGGING", false),
		RequestTimeout:     time.Duration(getEnvAsInt("REQUEST_TIMEOUT_SEC", 15)) * time.Second,
		AllowedOrigins:     strings.Split(allowedOriginsStr, ","),

		AgentAPIKey:      getEnv("AGENT_API_KEY", ""),
		BrandName:        getEnv("BRAND_NAME", "MyHoreca_Xenion"),
		SeederKey:        getEnv("SEEDER_KEY", "super-secret-key-for-seeding"),
		JWTSecret:        getEnv("JWT_SECRET", "mhrcadmin994525"),
		JWTExpirationMin: getEnvAsInt("JWT_EXPIRATION_MIN", 1440),
		AdminUsername:    getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword:    getEnv("ADMIN_PASSWORD", "mhrcadmin994525"),
		AdminFullName:    getEnv("ADMIN_FULLNAME", "Главный"),
		AgentAdapterS3Enabled:      getEnvAsBool("AGENT_ADAPTER_S3_ENABLED", false),
		AgentAdapterS3Endpoint:     strings.TrimSpace(getEnv("AGENT_ADAPTER_S3_ENDPOINT", "")),
		AgentAdapterS3Region:       strings.TrimSpace(getEnv("AGENT_ADAPTER_S3_REGION", "us-east-1")),
		AgentAdapterS3Bucket:       strings.TrimSpace(getEnv("AGENT_ADAPTER_S3_BUCKET", "agents")),
		AgentAdapterS3AccessKey:    strings.TrimSpace(getEnv("AGENT_ADAPTER_S3_ACCESS_KEY", "")),
		AgentAdapterS3SecretKey:    getEnv("AGENT_ADAPTER_S3_SECRET_KEY", ""),
		AgentAdapterPublicBaseURL:  strings.TrimRight(strings.TrimSpace(getEnv("AGENT_ADAPTER_PUBLIC_BASE_URL", "")), "/"),
		AgentAdapterCatalogKey:     strings.TrimSpace(getEnv("AGENT_ADAPTER_CATALOG_KEY", "catalog/index.json")),
		AgentAdapterSyncInterval:   time.Duration(max(1, getEnvAsInt("AGENT_ADAPTER_SYNC_INTERVAL_MIN", 5))) * time.Minute,
		AgentAdapterDefaultChannel: strings.ToLower(strings.TrimSpace(getEnv("AGENT_ADAPTER_DEFAULT_CHANNEL", "stable"))),

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
		ContractSyncInterval:  time.Duration(contractSyncHours) * time.Hour,
		ContractIMAPHost:      strings.TrimSpace(getEnv("CONTRACT_IMAP_HOST", "")),
		ContractIMAPPort:      getEnvAsInt("CONTRACT_IMAP_PORT", 993),
		ContractIMAPUsername:  strings.TrimSpace(getEnv("CONTRACT_IMAP_USERNAME", "")),
		ContractIMAPPassword:  getEnv("CONTRACT_IMAP_PASSWORD", ""),
		ContractIMAPMailbox:   strings.TrimSpace(getEnv("CONTRACT_IMAP_INBOX", "INBOX")),
		ContractZipMaxBytes:   getEnvAsInt("CONTRACT_ZIP_MAX_BYTES", 102400),

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

		EnableBitrixGateway:         getEnvAsBool("ENABLE_BITRIX_GATEWAY", false),
		BitrixBaseURL:               bitrixBaseURL,
		BitrixOriginatorID:          getEnv("BITRIX_ORIGINATOR_ID", "ETALON_SD"),
		BitrixCategoryID:            getEnvAsInt("BITRIX_CATEGORY_ID", 17),
		BitrixRateLimitPerMin:       min(max(1, getEnvAsInt("BITRIX_RATE_LIMIT_PER_MIN", 120)), 300),
		BitrixRateLimitBurst:        max(1, getEnvAsInt("BITRIX_RATE_LIMIT_BURST", 50)),
		BitrixSyncInterval:          time.Duration(getEnvAsInt("BITRIX_SYNC_INTERVAL_MIN", 5)) * time.Minute,
		BitrixDictionarySyncEvery:   time.Duration(getEnvAsInt("BITRIX_DICTIONARY_SYNC_INTERVAL_HOURS", 24)) * time.Hour,
		BitrixServicePointsIBlockID: getEnvAsInt("BITRIX_SERVICE_POINT_IBLOCK_ID", 101),
		EtalonTicketBaseURL:         strings.TrimSpace(getEnv("ETALON_TICKET_BASE_URL", "")),
		BitrixWebhookEnabled:        getEnvAsBool("BITRIX_WEBHOOK_ENABLED", false),
		BitrixWebhookAppToken:       strings.TrimSpace(getEnv("BITRIX_WEBHOOK_APPLICATION_TOKEN", "")),
		BitrixEventsStreamName:      strings.TrimSpace(getEnv("BITRIX_EVENTS_STREAM_NAME", "b24:events")),
		BitrixEventsConsumerGroup:   strings.TrimSpace(getEnv("BITRIX_EVENTS_CONSUMER_GROUP", "b24-workers")),
		BitrixIncomingParallelism:   getEnvAsInt("BITRIX_INCOMING_PARALLELISM", 8),
		BitrixIncomingRetryBase:     time.Duration(getEnvAsInt("BITRIX_INCOMING_RETRY_BASE_MS", 500)) * time.Millisecond,
		BitrixIncomingRetryMax:      time.Duration(getEnvAsInt("BITRIX_INCOMING_RETRY_MAX_MS", 30000)) * time.Millisecond,
		BitrixIncomingMaxAttempts:   getEnvAsInt("BITRIX_INCOMING_MAX_ATTEMPTS", 10),
		BitrixSuppressTTL:           time.Duration(getEnvAsInt("BITRIX_SUPPRESS_TTL_SEC", 20)) * time.Second,
		BitrixIntegrationUserID:     bitrixIntegrationUserID,

		RedisAddr:     strings.TrimSpace(getEnv("REDIS_ADDR", "localhost:6379")),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvAsInt("REDIS_DB", 0),
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

func detectBitrixIntegrationUserID(bitrixBaseURL string) int64 {
	base := strings.TrimSpace(bitrixBaseURL)
	if base == "" {
		return 0
	}
	parts := strings.Split(base, "/")
	for i := 0; i < len(parts)-1; i++ {
		if strings.TrimSpace(parts[i]) != "rest" {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(parts[i+1]), 10, 64)
		if err != nil || id <= 0 {
			return 0
		}
		return id
	}
	return 0
}
