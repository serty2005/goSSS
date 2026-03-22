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
	RegistrationApprovedAt *time.Time `json:"registration_approved_at,omitzero"`
	RegistrationApprovedBy string     `json:"registration_approved_by"`
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

type AgentHeartbeatMeaningfulStateDTO struct {
	Fingerprint               string     `json:"fingerprint"`
	LastMeaningfulHeartbeatAt *time.Time `json:"last_meaningful_heartbeat_at,omitzero"`
	LastMeaningfulObservedAt  *time.Time `json:"last_meaningful_observed_at,omitzero"`
	LastMeaningfulState       any        `json:"last_meaningful_state,omitzero"`
}

type AgentMachineProfileDTO struct {
	Key         string     `json:"key"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Source      string     `json:"source"`
	ConfirmedAt *time.Time `json:"confirmed_at,omitzero"`
	ConfirmedBy string     `json:"confirmed_by"`
}

type AgentCOMSignatureRuleDTO struct {
	ID               uint      `json:"id"`
	SignatureKey     string    `json:"signature_key"`
	DeviceType       string    `json:"device_type"`
	Label            string    `json:"label"`
	Confidence       string    `json:"confidence"`
	ProfileHint      string    `json:"profile_hint"`
	SuggestedAdapter string    `json:"suggested_adapter"`
	Source           string    `json:"source"`
	Notes            string    `json:"notes"`
	UpdatedAt        time.Time `json:"updated_at"`
	UpdatedBy        string    `json:"updated_by"`
}

type AgentCOMSignatureCandidateDTO struct {
	PortName             string                    `json:"port_name"`
	FriendlyName         string                    `json:"friendly_name"`
	SignatureKey         string                    `json:"signature_key"`
	VendorID             string                    `json:"vendor_id"`
	ProductID            string                    `json:"product_id"`
	ClassificationLabel  string                    `json:"classification_label"`
	ClassificationSource string                    `json:"classification_source"`
	DeviceType           string                    `json:"device_type"`
	SuggestedAdapter     string                    `json:"suggested_adapter"`
	ExistingRule         *AgentCOMSignatureRuleDTO `json:"existing_rule,omitzero"`
}

type PublishedAgentAdapterOptionDTO struct {
	AdapterID      string `json:"adapter_id"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Published      bool   `json:"published"`
	Selectable     bool   `json:"selectable"`
	StatusText     string `json:"status_text"`
	DisabledReason string `json:"disabled_reason,omitzero"`
	Version        string `json:"version"`
	AdapterType    string `json:"adapter_type"`
	TargetOS       string `json:"target_os"`
	TargetArch     string `json:"target_arch"`
}

type AgentOperatorFlowDTO struct {
	MeaningfulHeartbeat         AgentHeartbeatMeaningfulStateDTO `json:"meaningful_heartbeat"`
	AvailableAdapters           []PublishedAgentAdapterOptionDTO `json:"available_adapters,omitzero"`
	SelectedAdapterIDs          []string                         `json:"selected_adapter_ids,omitzero"`
	RecommendedAdapterIDs       []string                         `json:"recommended_adapter_ids,omitzero"`
	RecommendedProfile          AgentMachineProfileDTO           `json:"recommended_profile"`
	RecommendedReasons          []string                         `json:"recommended_reasons,omitzero"`
	RecommendedAdapterManifests []AdapterManifestDTO             `json:"recommended_adapter_manifests,omitzero"`
	SavedProfile                *AgentMachineProfileDTO          `json:"saved_profile,omitzero"`
	SavedReasons                []string                         `json:"saved_reasons,omitzero"`
	SavedAdapterManifests       []AdapterManifestDTO             `json:"saved_adapter_manifests,omitzero"`
	EffectiveAdapterManifests   []AdapterManifestDTO             `json:"effective_adapter_manifests,omitzero"`
	SignatureCandidates         []AgentCOMSignatureCandidateDTO  `json:"signature_candidates,omitzero"`
	Warnings                    []string                         `json:"warnings,omitzero"`
}

type AgentDiagnosticsDetailsDTO struct {
	Agent                  AgentDiagnosticsListItemDTO   `json:"agent"`
	RegistrationPayload    any                           `json:"registration_payload,omitzero"`
	RegistrationSystemInfo any                           `json:"registration_system_info,omitzero"`
	LatestInventory        any                           `json:"latest_inventory,omitzero"`
	LatestAdapterStatuses  any                           `json:"latest_adapter_statuses,omitzero"`
	RecentRegistrations    []AgentRegistrationAttemptDTO `json:"recent_registrations,omitzero"`
	OperatorFlow           *AgentOperatorFlowDTO         `json:"operator_flow,omitzero"`
}

type SaveAgentAdapterSelectionRequestDTO struct {
	SelectedAdapterIDs []string `json:"selected_adapter_ids,omitzero"`
}

type UpsertAgentCOMSignatureRuleRequestDTO struct {
	SignatureKey     string `json:"signature_key"`
	DeviceType       string `json:"device_type"`
	Label            string `json:"label"`
	Confidence       string `json:"confidence"`
	ProfileHint      string `json:"profile_hint"`
	SuggestedAdapter string `json:"suggested_adapter"`
	Notes            string `json:"notes"`
}
