package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Эти значения задают привязку агента к конкретному инстансу ServiceDesk.
// Для тестового билда URL фиксирован по требованию.
var (
	BrandName          = "MyHoreca_Xenion"
	BootstrapServerURL = "http://10.25.1.125:8080"
	BootstrapAPIKey    = "92a077f5-bea8-4773-a23e-9d8f1450a81c"
)

type Config struct {
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
}

func Load(version string) (Config, error) {
	company, product, err := splitBrand(BrandName)
	if err != nil {
		return Config{}, err
	}

	agentName := product + "Agent"
	dataDir := filepath.Join(os.Getenv("ProgramData"), company, agentName)
	if strings.TrimSpace(dataDir) == "" || strings.EqualFold(dataDir, `\`+company+`\`+agentName) {
		dataDir = filepath.Join(`C:\ProgramData`, company, agentName)
	}
	return Config{
		BrandName:              BrandName,
		BrandCompany:           company,
		BrandProduct:           product,
		AgentProcessName:       agentName,
		AgentVersion:           strings.TrimSpace(version),
		RegistryPath:           `Software\` + company + `\` + agentName,
		DataDir:                dataDir,
		AdapterDir:             filepath.Join(dataDir, "adapters"),
		ServerURL:              strings.TrimRight(BootstrapServerURL, "/"),
		BootstrapAPIKey:        BootstrapAPIKey,
		AgentType:              "sssruner",
		HeartbeatInterval:      15 * time.Second,
		UpdateCheckInterval:    60 * time.Second,
		InventoryInterval:      5 * time.Minute,
		AccessTokenGracePeriod: 2 * time.Minute,
	}, nil
}

func splitBrand(brand string) (company, product string, err error) {
	parts := strings.Split(strings.TrimSpace(brand), "_")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("BRAND_NAME должен быть в формате Company_Product, получено: %s", brand)
	}
	company = strings.TrimSpace(parts[0])
	product = strings.TrimSpace(parts[1])
	if company == "" || product == "" {
		return "", "", fmt.Errorf("BRAND_NAME содержит пустую часть: %s", brand)
	}
	return company, product, nil
}
