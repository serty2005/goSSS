package adapters

import "time"

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
	LocalPath   string    `json:"local_path,omitempty"`
	FileSize    int64     `json:"file_size,omitempty"`
	Status      string    `json:"status,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	InstalledAt time.Time `json:"installed_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

type Status struct {
	AdapterID       string    `json:"adapter_id"`
	AdapterType     string    `json:"adapter_type,omitempty"`
	Version         string    `json:"version,omitempty"`
	TargetOS        string    `json:"target_os,omitempty"`
	TargetArch      string    `json:"target_arch,omitempty"`
	ProtocolVersion string    `json:"protocol_version,omitempty"`
	Status          string    `json:"status,omitempty"`
	LocalPath       string    `json:"local_path,omitempty"`
	FileSize        int64     `json:"file_size,omitempty"`
	SHA256          string    `json:"sha256,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	InstalledAt     time.Time `json:"installed_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}
