package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultConfigFileName = "agent-config.json"
	ConfigPathEnv         = "ETALON_AGENT_CONFIG"

	defaultAgentType              = "sssruner"
	defaultHeartbeatInterval      = 15 * time.Second
	defaultUpdateCheckInterval    = 60 * time.Second
	defaultInventoryInterval      = 5 * time.Minute
	defaultAccessTokenGracePeriod = 2 * time.Minute
	defaultConnectivityBaseRetry  = 15 * time.Second
	defaultConnectivityMaxRetry   = 10 * time.Minute
	defaultRegistrationCooldown   = 30 * time.Minute
	defaultAuthorizationCooldown  = 24 * time.Hour
	defaultRateLimitCooldown      = 5 * time.Minute
	defaultConfigErrorCooldown    = time.Hour
	defaultProtocolErrorCooldown  = 30 * time.Minute
	defaultRetryJitterFactor      = 0.2
)

var ErrDefaultConfigCreated = errors.New("создан шаблон конфигурации агента")

type DefaultConfigCreatedError struct {
	Path string
}

func (e *DefaultConfigCreatedError) Error() string {
	return fmt.Sprintf("не найден %s рядом с исполняемым файлом; создан шаблон конфига %s", DefaultConfigFileName, e.Path)
}

func (e *DefaultConfigCreatedError) Unwrap() error {
	return ErrDefaultConfigCreated
}

type Config struct {
	ConfigSource           string
	BrandName              string
	BrandCompany           string
	BrandProduct           string
	AgentProcessName       string
	AgentVersion           string
	RegistryPath           string
	DataDir                string
	AdapterDir             string
	ServerURL              string
	BootstrapAPIKey        string
	AgentType              string
	HeartbeatInterval      time.Duration
	UpdateCheckInterval    time.Duration
	InventoryInterval      time.Duration
	AccessTokenGracePeriod time.Duration
	ConnectivityBaseRetry  time.Duration
	ConnectivityMaxRetry   time.Duration
	RegistrationCooldown   time.Duration
	AuthorizationCooldown  time.Duration
	RateLimitCooldown      time.Duration
	ConfigErrorCooldown    time.Duration
	ProtocolErrorCooldown  time.Duration
	RetryJitterFactor      float64
}

type LoadOptions struct {
	ConfigPath     string
	ExecutablePath string
	LookupEnv      func(string) string
}

type configPathSource int

const (
	configPathSourceExplicit configPathSource = iota
	configPathSourceEnv
	configPathSourceExecutableDefault
)

type resolvedConfigPath struct {
	Path   string
	Source configPathSource
}

type fileConfig struct {
	BrandName              string   `json:"brand_name"`
	AgentProcessName       string   `json:"agent_process_name,omitempty"`
	RegistryPath           string   `json:"registry_path,omitempty"`
	DataDir                string   `json:"data_dir,omitempty"`
	AdapterDir             string   `json:"adapter_dir,omitempty"`
	ServerURL              string   `json:"server_url"`
	BootstrapAPIKey        string   `json:"bootstrap_api_key"`
	AgentType              string   `json:"agent_type,omitempty"`
	HeartbeatInterval      string   `json:"heartbeat_interval,omitempty"`
	UpdateCheckInterval    string   `json:"update_check_interval,omitempty"`
	InventoryInterval      string   `json:"inventory_interval,omitempty"`
	AccessTokenGracePeriod string   `json:"access_token_grace_period,omitempty"`
	ConnectivityBaseRetry  string   `json:"connectivity_base_retry,omitempty"`
	ConnectivityMaxRetry   string   `json:"connectivity_max_retry,omitempty"`
	RegistrationCooldown   string   `json:"registration_cooldown,omitempty"`
	AuthorizationCooldown  string   `json:"authorization_cooldown,omitempty"`
	RateLimitCooldown      string   `json:"rate_limit_cooldown,omitempty"`
	ConfigErrorCooldown    string   `json:"config_error_cooldown,omitempty"`
	ProtocolErrorCooldown  string   `json:"protocol_error_cooldown,omitempty"`
	RetryJitterFactor      *float64 `json:"retry_jitter_factor,omitempty"`
}

func Load(version string, options LoadOptions) (Config, error) {
	resolvedPath, err := resolveConfigPath(options)
	if err != nil {
		return Config{}, err
	}

	raw, err := os.ReadFile(resolvedPath.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && resolvedPath.Source == configPathSourceExecutableDefault {
			if createErr := createDefaultConfigTemplate(resolvedPath.Path); createErr != nil {
				return Config{}, fmt.Errorf("не удалось создать шаблон конфига агента %s: %w", resolvedPath.Path, createErr)
			}
			return Config{}, &DefaultConfigCreatedError{Path: resolvedPath.Path}
		}
		return Config{}, fmt.Errorf("не удалось прочитать конфиг агента %s: %w", resolvedPath.Path, err)
	}

	var fileCfg fileConfig
	if err := json.Unmarshal(raw, &fileCfg); err != nil {
		return Config{}, fmt.Errorf("не удалось разобрать конфиг агента %s: %w", resolvedPath.Path, err)
	}

	return buildConfig(strings.TrimSpace(version), resolvedPath.Path, fileCfg)
}

func resolveConfigPath(options LoadOptions) (resolvedConfigPath, error) {
	if explicitPath := strings.TrimSpace(options.ConfigPath); explicitPath != "" {
		return resolvedConfigPath{Path: normalizePath(explicitPath), Source: configPathSourceExplicit}, nil
	}

	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.Getenv
	}
	if envPath := strings.TrimSpace(lookupEnv(ConfigPathEnv)); envPath != "" {
		return resolvedConfigPath{Path: normalizePath(envPath), Source: configPathSourceEnv}, nil
	}

	executablePath := strings.TrimSpace(options.ExecutablePath)
	if executablePath == "" {
		var err error
		executablePath, err = os.Executable()
		if err != nil {
			return resolvedConfigPath{}, fmt.Errorf("не удалось определить путь к исполняемому файлу для поиска %s: %w", DefaultConfigFileName, err)
		}
	}

	return resolvedConfigPath{
		Path:   normalizePath(filepath.Join(filepath.Dir(executablePath), DefaultConfigFileName)),
		Source: configPathSourceExecutableDefault,
	}, nil
}

func normalizePath(path string) string {
	if absPath, err := filepath.Abs(path); err == nil {
		return filepath.Clean(absPath)
	}
	return filepath.Clean(path)
}

func buildConfig(version string, configPath string, fileCfg fileConfig) (Config, error) {
	brandName := strings.TrimSpace(fileCfg.BrandName)
	if brandName == "" {
		return Config{}, fmt.Errorf("поле brand_name обязательно")
	}
	company, product, err := splitBrand(brandName)
	if err != nil {
		return Config{}, err
	}

	serverURL := strings.TrimRight(strings.TrimSpace(fileCfg.ServerURL), "/")
	if serverURL == "" {
		return Config{}, fmt.Errorf("поле server_url обязательно")
	}

	bootstrapAPIKey := strings.TrimSpace(fileCfg.BootstrapAPIKey)
	if bootstrapAPIKey == "" {
		return Config{}, fmt.Errorf("поле bootstrap_api_key обязательно")
	}

	agentName := firstNonEmpty(strings.TrimSpace(fileCfg.AgentProcessName), product+"Agent")
	dataDir := resolveDataDir(strings.TrimSpace(fileCfg.DataDir), company, agentName)
	adapterDir := firstNonEmpty(strings.TrimSpace(fileCfg.AdapterDir), filepath.Join(dataDir, "adapters"))

	heartbeatInterval, err := parseDuration(fileCfg.HeartbeatInterval, defaultHeartbeatInterval, "heartbeat_interval")
	if err != nil {
		return Config{}, err
	}
	updateCheckInterval, err := parseDuration(fileCfg.UpdateCheckInterval, defaultUpdateCheckInterval, "update_check_interval")
	if err != nil {
		return Config{}, err
	}
	inventoryInterval, err := parseDuration(fileCfg.InventoryInterval, defaultInventoryInterval, "inventory_interval")
	if err != nil {
		return Config{}, err
	}
	accessTokenGracePeriod, err := parseDuration(fileCfg.AccessTokenGracePeriod, defaultAccessTokenGracePeriod, "access_token_grace_period")
	if err != nil {
		return Config{}, err
	}
	connectivityBaseRetry, err := parseDuration(fileCfg.ConnectivityBaseRetry, defaultConnectivityBaseRetry, "connectivity_base_retry")
	if err != nil {
		return Config{}, err
	}
	connectivityMaxRetry, err := parseDuration(fileCfg.ConnectivityMaxRetry, defaultConnectivityMaxRetry, "connectivity_max_retry")
	if err != nil {
		return Config{}, err
	}
	registrationCooldown, err := parseDuration(fileCfg.RegistrationCooldown, defaultRegistrationCooldown, "registration_cooldown")
	if err != nil {
		return Config{}, err
	}
	authorizationCooldown, err := parseDuration(fileCfg.AuthorizationCooldown, defaultAuthorizationCooldown, "authorization_cooldown")
	if err != nil {
		return Config{}, err
	}
	rateLimitCooldown, err := parseDuration(fileCfg.RateLimitCooldown, defaultRateLimitCooldown, "rate_limit_cooldown")
	if err != nil {
		return Config{}, err
	}
	configErrorCooldown, err := parseDuration(fileCfg.ConfigErrorCooldown, defaultConfigErrorCooldown, "config_error_cooldown")
	if err != nil {
		return Config{}, err
	}
	protocolErrorCooldown, err := parseDuration(fileCfg.ProtocolErrorCooldown, defaultProtocolErrorCooldown, "protocol_error_cooldown")
	if err != nil {
		return Config{}, err
	}

	retryJitterFactor := defaultRetryJitterFactor
	if fileCfg.RetryJitterFactor != nil {
		retryJitterFactor = *fileCfg.RetryJitterFactor
	}
	if retryJitterFactor < 0 || retryJitterFactor > 0.9 {
		return Config{}, fmt.Errorf("поле retry_jitter_factor должно быть в диапазоне [0, 0.9]")
	}

	return Config{
		ConfigSource:           configPath,
		BrandName:              brandName,
		BrandCompany:           company,
		BrandProduct:           product,
		AgentProcessName:       agentName,
		AgentVersion:           version,
		RegistryPath:           normalizeRegistryPath(firstNonEmpty(strings.TrimSpace(fileCfg.RegistryPath), `Software\`+company+`\`+agentName)),
		DataDir:                dataDir,
		AdapterDir:             adapterDir,
		ServerURL:              serverURL,
		BootstrapAPIKey:        bootstrapAPIKey,
		AgentType:              firstNonEmpty(strings.TrimSpace(fileCfg.AgentType), defaultAgentType),
		HeartbeatInterval:      heartbeatInterval,
		UpdateCheckInterval:    updateCheckInterval,
		InventoryInterval:      inventoryInterval,
		AccessTokenGracePeriod: accessTokenGracePeriod,
		ConnectivityBaseRetry:  connectivityBaseRetry,
		ConnectivityMaxRetry:   connectivityMaxRetry,
		RegistrationCooldown:   registrationCooldown,
		AuthorizationCooldown:  authorizationCooldown,
		RateLimitCooldown:      rateLimitCooldown,
		ConfigErrorCooldown:    configErrorCooldown,
		ProtocolErrorCooldown:  protocolErrorCooldown,
		RetryJitterFactor:      retryJitterFactor,
	}, nil
}

func createDefaultConfigTemplate(configPath string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	template := fileConfig{
		BrandName:              "Company_Product",
		ServerURL:              "",
		BootstrapAPIKey:        "",
		AgentType:              defaultAgentType,
		HeartbeatInterval:      defaultHeartbeatInterval.String(),
		UpdateCheckInterval:    defaultUpdateCheckInterval.String(),
		InventoryInterval:      defaultInventoryInterval.String(),
		AccessTokenGracePeriod: defaultAccessTokenGracePeriod.String(),
		ConnectivityBaseRetry:  defaultConnectivityBaseRetry.String(),
		ConnectivityMaxRetry:   defaultConnectivityMaxRetry.String(),
		RegistrationCooldown:   defaultRegistrationCooldown.String(),
		AuthorizationCooldown:  defaultAuthorizationCooldown.String(),
		RateLimitCooldown:      defaultRateLimitCooldown.String(),
		ConfigErrorCooldown:    defaultConfigErrorCooldown.String(),
		ProtocolErrorCooldown:  defaultProtocolErrorCooldown.String(),
		RetryJitterFactor:      float64Ptr(defaultRetryJitterFactor),
	}

	raw, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

func resolveDataDir(rawPath, company, agentName string) string {
	if strings.TrimSpace(rawPath) != "" {
		return strings.TrimSpace(rawPath)
	}

	programData := strings.TrimSpace(os.Getenv("ProgramData"))
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, company, agentName)
}

func parseDuration(raw string, fallback time.Duration, fieldName string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("поле %s содержит некорректную duration %q: %w", fieldName, value, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("поле %s должно быть больше нуля", fieldName)
	}
	return duration, nil
}

func normalizeRegistryPath(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(strings.ToUpper(value), `HKLM\`):
		value = value[5:]
	case strings.HasPrefix(strings.ToUpper(value), `HKEY_LOCAL_MACHINE\`):
		value = value[len(`HKEY_LOCAL_MACHINE\`):]
	}
	return strings.TrimLeft(value, `\`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func float64Ptr(value float64) *float64 {
	return &value
}

func splitBrand(brand string) (company, product string, err error) {
	parts := strings.Split(strings.TrimSpace(brand), "_")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("brand_name должен быть в формате Company_Product, получено: %s", brand)
	}
	company = strings.TrimSpace(parts[0])
	product = strings.TrimSpace(parts[1])
	if company == "" || product == "" {
		return "", "", fmt.Errorf("brand_name содержит пустую часть: %s", brand)
	}
	return company, product, nil
}
