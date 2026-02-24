package protocol

import (
	"encoding/json"
	"time"
)

type AgentDataDTO struct {
	Hostname     string `json:"hostname"`
	CurrentTime  string `json:"current_time"`
	AgentVersion string `json:"agent_version"`
	AgentUUID    string `json:"uuid,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
}

type RegistrationRequestDTO struct {
	AgentUUID    string       `json:"agent_uuid"`
	Hostname     string       `json:"hostname"`
	AgentVersion string       `json:"agent_version"`
	InitialData  AgentDataDTO `json:"initial_data"`
}

type AgentTaskDTO struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type HeartbeatResponseDTO struct {
	Status string         `json:"status"`
	Tasks  []AgentTaskDTO `json:"tasks,omitempty"`
}

type SelfUpdateTaskPayload struct {
	Version     string   `json:"version"`
	DownloadURL string   `json:"download_url"`
	SHA256      string   `json:"sha256,omitempty"`
	FileName    string   `json:"file_name,omitempty"`
	Restart     *bool    `json:"restart,omitempty"`
	Args        []string `json:"args,omitempty"`
}
