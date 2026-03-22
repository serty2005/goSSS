package adapters

import (
	"encoding/json"
	"time"
)

type ManifestItem struct {
	AdapterID       string `json:"adapter_id"`
	AdapterType     string `json:"adapter_type,omitempty"`
	Version         string `json:"version,omitempty"`
	TargetOS        string `json:"target_os,omitempty"`
	TargetArch      string `json:"target_arch,omitempty"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
	FileName        string `json:"file_name,omitempty"`
}

type Descriptor struct {
	ManifestItem
	LocalPath    string     `json:"local_path,omitempty"`
	FileSize     int64      `json:"file_size,omitempty"`
	Status       string     `json:"status,omitempty"`
	RunStatus    string     `json:"run_status,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	LastExitCode *int       `json:"last_exit_code,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	InstalledAt  time.Time  `json:"installed_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at,omitempty"`
}

type Status struct {
	AdapterID       string     `json:"adapter_id"`
	AdapterType     string     `json:"adapter_type,omitempty"`
	Version         string     `json:"version,omitempty"`
	TargetOS        string     `json:"target_os,omitempty"`
	TargetArch      string     `json:"target_arch,omitempty"`
	ProtocolVersion string     `json:"protocol_version,omitempty"`
	Status          string     `json:"status,omitempty"`
	RunStatus       string     `json:"run_status,omitempty"`
	LocalPath       string     `json:"local_path,omitempty"`
	FileSize        int64      `json:"file_size,omitempty"`
	SHA256          string     `json:"sha256,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastExitCode    *int       `json:"last_exit_code,omitempty"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	InstalledAt     time.Time  `json:"installed_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at,omitempty"`
}

type RunRequest struct {
	AdapterID string
	Command   string
	Timeout   time.Duration
	Input     json.RawMessage
}

type RunResult struct {
	AdapterID        string
	Command          string
	StartedAt        time.Time
	CompletedAt      time.Time
	Duration         time.Duration
	ExitCode         *int
	Stdout           string
	Stderr           string
	StructuredResult json.RawMessage
	RunStatus        string
}
