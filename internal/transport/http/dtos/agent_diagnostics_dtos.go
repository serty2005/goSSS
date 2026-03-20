package dtos

import "time"

type AgentDiagnosticsListItemDTO struct {
	UUID                   string     `json:"uuid"`
	Hostname               string     `json:"hostname"`
	Type                   string     `json:"type"`
	Status                 string     `json:"status"`
	OwnerID                string     `json:"owner_id"`
	WorkstationID          *string    `json:"workstation_id,omitzero"`
	LastObservedAt         *time.Time `json:"last_observed_at,omitzero"`
	LastHeartbeat          time.Time  `json:"last_heartbeat"`
	LastRegistrationAt     *time.Time `json:"last_registration_at,omitzero"`
	LastRegistrationStatus string     `json:"last_registration_status"`
	LastRegistrationError  string     `json:"last_registration_error"`
	MachineFingerprint     string     `json:"machine_fingerprint"`
	HasLatestInventory     bool       `json:"has_latest_inventory"`
	HasAdapterStatuses     bool       `json:"has_adapter_statuses"`
}

type AgentRegistrationAttemptDTO struct {
	ID                 uint      `json:"id"`
	AgentUUID          *string   `json:"agent_uuid,omitzero"`
	Status             string    `json:"status"`
	ErrorText          *string   `json:"error_text,omitzero"`
	MachineFingerprint string    `json:"machine_fingerprint"`
	SystemInfo         any       `json:"system_info,omitzero"`
	Payload            any       `json:"payload,omitzero"`
	RemoteAddr         string    `json:"remote_addr"`
	CreatedAt          time.Time `json:"created_at"`
}

type AgentDiagnosticsDetailsDTO struct {
	Agent                  AgentDiagnosticsListItemDTO   `json:"agent"`
	RegistrationPayload    any                           `json:"registration_payload,omitzero"`
	RegistrationSystemInfo any                           `json:"registration_system_info,omitzero"`
	LatestInventory        any                           `json:"latest_inventory,omitzero"`
	LatestAdapterStatuses  any                           `json:"latest_adapter_statuses,omitzero"`
	RecentRegistrations    []AgentRegistrationAttemptDTO `json:"recent_registrations,omitzero"`
}
