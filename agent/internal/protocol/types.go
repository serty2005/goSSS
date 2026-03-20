package protocol

import (
	"encoding/json"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/inventory"
)

type AgentDataDTO struct {
	Hostname        string              `json:"hostname"`
	CurrentTime     string              `json:"current_time"`
	AgentVersion    string              `json:"agent_version"`
	AgentUUID       string              `json:"uuid,omitempty"`
	AgentType       string              `json:"agent_type,omitempty"`
	Inventory       *inventory.Snapshot `json:"inventory,omitempty"`
	AdapterStatuses []adapters.Status   `json:"adapter_statuses,omitempty"`
}

type RegistrationRequestDTO struct {
	AgentUUID          string                 `json:"agent_uuid"`
	Hostname           string                 `json:"hostname"`
	AgentVersion       string                 `json:"agent_version"`
	InitialData        AgentDataDTO           `json:"initial_data"`
	MachineFingerprint string                 `json:"machine_fingerprint,omitempty"`
	SystemInfo         map[string]interface{} `json:"system_info,omitempty"`
}

type AgentTaskDTO struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type HeartbeatResponseDTO struct {
	Status           string                  `json:"status"`
	Tasks            []AgentTaskDTO          `json:"tasks,omitempty"`
	AdapterManifests []adapters.ManifestItem `json:"adapter_manifests,omitempty"`
}

type AgentRegistrationResponseDTO struct {
	Status                string    `json:"status"`
	AgentUUID             string    `json:"agent_uuid"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

type AgentTokenRefreshRequestDTO struct {
	AgentUUID    string `json:"agent_uuid"`
	RefreshToken string `json:"refresh_token"`
}

type AgentTokenRefreshResponseDTO struct {
	Status                string    `json:"status"`
	AgentUUID             string    `json:"agent_uuid"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

type SelfUpdateTaskPayload struct {
	Version     string   `json:"version"`
	DownloadURL string   `json:"download_url"`
	SHA256      string   `json:"sha256,omitempty"`
	FileName    string   `json:"file_name,omitempty"`
	Restart     *bool    `json:"restart,omitempty"`
	Args        []string `json:"args,omitempty"`
}
