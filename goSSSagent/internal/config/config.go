package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerURL           string
	APIKey              string
	AgentVersion        string
	AgentType           string
	HeartbeatInterval   time.Duration
	UpdateCheckInterval time.Duration
	DataDir             string
	HostnameOverride    string
}

func LoadFromEnv(version string) (Config, error) {
	cfg := Config{
		ServerURL:           strings.TrimRight(getenv("GOSSS_AGENT_SERVER_URL", "http://localhost:8080"), "/"),
		APIKey:              strings.TrimSpace(os.Getenv("GOSSS_AGENT_API_KEY")),
		AgentVersion:        version,
		AgentType:           getenv("GOSSS_AGENT_TYPE", "workstation"),
		HeartbeatInterval:   durationFromEnvSeconds("GOSSS_AGENT_HEARTBEAT_SEC", 15*time.Second),
		UpdateCheckInterval: durationFromEnvSeconds("GOSSS_AGENT_UPDATE_CHECK_SEC", 60*time.Second),
		HostnameOverride:    strings.TrimSpace(os.Getenv("GOSSS_AGENT_HOSTNAME")),
	}

	dataDir := strings.TrimSpace(os.Getenv("GOSSS_AGENT_DATA_DIR"))
	if dataDir == "" {
		exePath, err := os.Executable()
		if err != nil {
			return Config{}, fmt.Errorf("не удалось определить каталог агента: %w", err)
		}
		dataDir = filepath.Dir(exePath)
	}
	cfg.DataDir = dataDir

	if cfg.APIKey == "" {
		return Config{}, fmt.Errorf("не задан GOSSS_AGENT_API_KEY")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func durationFromEnvSeconds(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return time.Duration(n) * time.Second
}
