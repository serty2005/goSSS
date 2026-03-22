package services

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	api "etalon-server/internal/transport/http/dtos"
	"slices"
	"strings"

	"gorm.io/datatypes"
)

type heartbeatFingerprintResult struct {
	Fingerprint string
	StateJSON   datatypes.JSON
}

type normalizedHeartbeatState struct {
	Host          normalizedHeartbeatHost            `json:"host"`
	Inventory     normalizedHeartbeatInventory       `json:"inventory,omitzero"`
	Fiscal        normalizedHeartbeatFiscal          `json:"fiscal,omitzero"`
	AdapterStatus []normalizedHeartbeatAdapterStatus `json:"adapter_statuses,omitzero"`
}

type normalizedHeartbeatHost struct {
	AgentType string                      `json:"agent_type"`
	Hostname  string                      `json:"hostname"`
	AgentVer  string                      `json:"agent_version"`
	URLRMS    string                      `json:"url_rms"`
	CRMID     string                      `json:"crm_id"`
	RemoteIDs normalizedHeartbeatRemoteID `json:"remote_ids,omitzero"`
	OS        string                      `json:"os"`
	Arch      string                      `json:"arch"`
}

type normalizedHeartbeatRemoteID struct {
	Teamviewer  string `json:"teamviewer_id"`
	Anydesk     string `json:"anydesk_id"`
	Litemanager string `json:"litemanager_id"`
	Rustdesk    string `json:"rustdesk_id"`
}

type normalizedHeartbeatInventory struct {
	HostInfo        normalizedHeartbeatHostInfo         `json:"host_info,omitzero"`
	KnownComponents []normalizedHeartbeatKnownComponent `json:"known_components,omitzero"`
	COMPorts        []normalizedHeartbeatCOMPort        `json:"com_ports,omitzero"`
}

type normalizedHeartbeatHostInfo struct {
	CashServerProduct string `json:"cash_server_product"`
	CashServerURL     string `json:"cash_server_url"`
	CashServerConfig  string `json:"cash_server_config"`
}

type normalizedHeartbeatKnownComponent struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

type normalizedHeartbeatCOMPort struct {
	Name             string `json:"name"`
	SignatureKey     string `json:"signature_key"`
	VendorID         string `json:"vendor_id"`
	ProductID        string `json:"product_id"`
	DeviceType       string `json:"device_type"`
	Label            string `json:"label"`
	Confidence       string `json:"confidence"`
	Source           string `json:"source"`
	SuggestedAdapter string `json:"suggested_adapter"`
}

type normalizedHeartbeatFiscal struct {
	ModelName        string   `json:"model_name"`
	SerialNumber     string   `json:"serial_number"`
	RNM              string   `json:"rnm"`
	INN              string   `json:"inn"`
	FNSerial         string   `json:"fn_serial"`
	FFDVersion       string   `json:"ffd_version"`
	FNExecution      string   `json:"fn_execution"`
	OrganizationName string   `json:"organization_name"`
	DateTimeReg      string   `json:"datetime_reg"`
	DateTimeEnd      string   `json:"datetime_end"`
	Address          string   `json:"address"`
	OFDName          string   `json:"ofd_name"`
	AttributeExcise  string   `json:"attribute_excise"`
	AttributeMarked  string   `json:"attribute_marked"`
	Licenses         []string `json:"licenses,omitzero"`
}

type normalizedHeartbeatAdapterStatus struct {
	AdapterID       string `json:"adapter_id"`
	AdapterType     string `json:"adapter_type"`
	Version         string `json:"version"`
	TargetOS        string `json:"target_os"`
	TargetArch      string `json:"target_arch"`
	ProtocolVersion string `json:"protocol_version"`
	Status          string `json:"status"`
	SHA256          string `json:"sha256"`
	LastError       string `json:"last_error"`
}

func buildHeartbeatFingerprint(data *api.AgentDataDTO) (heartbeatFingerprintResult, error) {
	state := normalizedHeartbeatState{}
	if data != nil {
		state = normalizedHeartbeatState{
			Host:          normalizeHeartbeatHost(data),
			Inventory:     normalizeHeartbeatInventory(data),
			Fiscal:        normalizeHeartbeatFiscal(data),
			AdapterStatus: normalizeHeartbeatAdapterStatuses(data.AdapterStatuses),
		}
	}

	raw, err := json.Marshal(state)
	if err != nil {
		return heartbeatFingerprintResult{}, err
	}

	sum := sha256.Sum256(raw)
	return heartbeatFingerprintResult{
		Fingerprint: hex.EncodeToString(sum[:]),
		StateJSON:   datatypes.JSON(raw),
	}, nil
}

func normalizeHeartbeatHost(data *api.AgentDataDTO) normalizedHeartbeatHost {
	inventory := data.Inventory
	hostInfo := (*api.InventoryHostInfoDTO)(nil)
	if inventory != nil {
		hostInfo = inventory.HostInfo
	}

	return normalizedHeartbeatHost{
		AgentType: normalizeLower(data.AgentType),
		Hostname:  cmp.Or(normalizeLower(cmp.Or(inventoryHostname(inventory), data.Hostname)), ""),
		AgentVer:  normalizeSpace(data.AgentVersion),
		URLRMS:    normalizeURLLike(data.URLRms),
		CRMID:     normalizeLower(data.CRMID),
		RemoteIDs: normalizedHeartbeatRemoteID{
			Teamviewer:  normalizeDigits(cmp.Or(data.TeamviewerID, hostInfoValue(hostInfo, func(v *api.InventoryHostInfoDTO) string { return v.TeamviewerID }))),
			Anydesk:     normalizeSpace(cmp.Or(data.AnydeskID, hostInfoValue(hostInfo, func(v *api.InventoryHostInfoDTO) string { return v.AnydeskID }))),
			Litemanager: normalizeSpace(cmp.Or(data.LitemanagerID, hostInfoValue(hostInfo, func(v *api.InventoryHostInfoDTO) string { return v.LitemanagerID }))),
			Rustdesk:    normalizeSpace(cmp.Or(data.RustdeskID, hostInfoValue(hostInfo, func(v *api.InventoryHostInfoDTO) string { return v.RustdeskID }))),
		},
		OS:   normalizeLower(inventoryOS(inventory)),
		Arch: normalizeLower(inventoryArch(inventory)),
	}
}

func normalizeHeartbeatInventory(data *api.AgentDataDTO) normalizedHeartbeatInventory {
	if data == nil || data.Inventory == nil {
		return normalizedHeartbeatInventory{}
	}

	inventory := data.Inventory
	out := normalizedHeartbeatInventory{
		HostInfo: normalizedHeartbeatHostInfo{
			CashServerProduct: normalizeLower(hostInfoValue(inventory.HostInfo, func(v *api.InventoryHostInfoDTO) string { return v.CashServerProduct })),
			CashServerURL:     normalizeURLLike(hostInfoValue(inventory.HostInfo, func(v *api.InventoryHostInfoDTO) string { return v.CashServerURL })),
			CashServerConfig:  normalizeSpace(hostInfoValue(inventory.HostInfo, func(v *api.InventoryHostInfoDTO) string { return v.CashServerConfig })),
		},
	}

	for _, component := range inventory.KnownComponents {
		if !component.Detected {
			continue
		}
		key := normalizeLower(component.Key)
		if key == "" {
			continue
		}
		out.KnownComponents = append(out.KnownComponents, normalizedHeartbeatKnownComponent{
			Key:     key,
			Version: normalizeSpace(component.Version),
		})
	}
	slices.SortFunc(out.KnownComponents, func(left, right normalizedHeartbeatKnownComponent) int {
		if cmpKey := strings.Compare(left.Key, right.Key); cmpKey != 0 {
			return cmpKey
		}
		return strings.Compare(left.Version, right.Version)
	})

	for _, port := range inventory.COMPorts {
		item := normalizedHeartbeatCOMPort{
			Name:             normalizeUpper(port.Name),
			SignatureKey:     normalizeLower(port.SignatureKey),
			VendorID:         normalizeLower(port.VendorID),
			ProductID:        normalizeLower(port.ProductID),
			DeviceType:       normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.DeviceType })),
			Label:            normalizeSpace(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.Label })),
			Confidence:       normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.Confidence })),
			Source:           normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.Source })),
			SuggestedAdapter: normalizeLower(classificationValue(port.Classification, func(v *api.InventoryCOMPortClassificationDTO) string { return v.SuggestedAdapter })),
		}
		if item.SignatureKey == "" && item.DeviceType == "" && item.SuggestedAdapter == "" {
			continue
		}
		out.COMPorts = append(out.COMPorts, item)
	}
	slices.SortFunc(out.COMPorts, func(left, right normalizedHeartbeatCOMPort) int {
		for _, cmpValue := range []int{
			strings.Compare(left.SignatureKey, right.SignatureKey),
			strings.Compare(left.DeviceType, right.DeviceType),
			strings.Compare(left.SuggestedAdapter, right.SuggestedAdapter),
			strings.Compare(left.Name, right.Name),
		} {
			if cmpValue != 0 {
				return cmpValue
			}
		}
		return 0
	})

	return out
}

func normalizeHeartbeatFiscal(data *api.AgentDataDTO) normalizedHeartbeatFiscal {
	if data == nil {
		return normalizedHeartbeatFiscal{}
	}

	return normalizedHeartbeatFiscal{
		ModelName:        normalizeSpace(data.ModelName),
		SerialNumber:     normalizeUpper(data.SerialNumber),
		RNM:              normalizeSpace(data.RNM),
		INN:              normalizeDigits(data.INN),
		FNSerial:         normalizeUpper(data.FNSerial),
		FFDVersion:       normalizeSpace(data.FFDVersion),
		FNExecution:      normalizeSpace(data.FNExecution),
		OrganizationName: normalizeSpace(data.OrganizationName),
		DateTimeReg:      normalizeSpace(data.DateTimeReg),
		DateTimeEnd:      normalizeSpace(data.DateTimeEnd),
		Address:          normalizeSpace(data.Address),
		OFDName:          normalizeSpace(data.OFDName),
		AttributeExcise:  normalizeLower(ptrValue(data.AttributeExcise)),
		AttributeMarked:  normalizeLower(ptrValue(data.AttributeMarked)),
		Licenses:         normalizeLicenses(data.Licenses),
	}
}

func normalizeHeartbeatAdapterStatuses(statuses []api.AdapterStatusDTO) []normalizedHeartbeatAdapterStatus {
	if len(statuses) == 0 {
		return nil
	}

	out := make([]normalizedHeartbeatAdapterStatus, 0, len(statuses))
	for _, status := range statuses {
		item := normalizedHeartbeatAdapterStatus{
			AdapterID:       normalizeLower(status.AdapterID),
			AdapterType:     normalizeLower(status.AdapterType),
			Version:         normalizeSpace(status.Version),
			TargetOS:        normalizeLower(status.TargetOS),
			TargetArch:      normalizeLower(status.TargetArch),
			ProtocolVersion: normalizeSpace(status.ProtocolVersion),
			Status:          normalizeLower(status.Status),
			SHA256:          normalizeLower(status.SHA256),
			LastError:       normalizeSpace(status.LastError),
		}
		if item.AdapterID == "" && item.AdapterType == "" && item.Status == "" {
			continue
		}
		out = append(out, item)
	}

	slices.SortFunc(out, func(left, right normalizedHeartbeatAdapterStatus) int {
		for _, cmpValue := range []int{
			strings.Compare(left.AdapterID, right.AdapterID),
			strings.Compare(left.AdapterType, right.AdapterType),
			strings.Compare(left.TargetOS, right.TargetOS),
			strings.Compare(left.TargetArch, right.TargetArch),
			strings.Compare(left.Version, right.Version),
			strings.Compare(left.Status, right.Status),
		} {
			if cmpValue != 0 {
				return cmpValue
			}
		}
		return 0
	})

	return out
}

func normalizeLicenses(field api.LicensesField) []string {
	out := make([]string, 0, len(field.Structured)+1)
	if legacy := normalizeSpace(field.Legacy); legacy != "" {
		out = append(out, legacy)
	}
	if len(field.Structured) == 0 {
		return out
	}

	keys := mapsStringKeys(field.Structured)
	slices.Sort(keys)
	for _, key := range keys {
		item := field.Structured[key]
		out = append(out, strings.Join([]string{
			normalizeSpace(key),
			normalizeSpace(item.Name),
			normalizeSpace(item.DateFrom),
			normalizeSpace(item.DateUntil),
		}, "|"))
	}
	return out
}

func mapsStringKeys[V any](in map[string]V) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	return out
}

func hostInfoValue(hostInfo *api.InventoryHostInfoDTO, pick func(*api.InventoryHostInfoDTO) string) string {
	if hostInfo == nil {
		return ""
	}
	return pick(hostInfo)
}

func classificationValue(classification *api.InventoryCOMPortClassificationDTO, pick func(*api.InventoryCOMPortClassificationDTO) string) string {
	if classification == nil {
		return ""
	}
	return pick(classification)
}

func inventoryHostname(inventory *api.InventorySnapshotDTO) string {
	if inventory == nil {
		return ""
	}
	return inventory.Hostname
}

func inventoryOS(inventory *api.InventorySnapshotDTO) string {
	if inventory == nil {
		return ""
	}
	return inventory.OS
}

func inventoryArch(inventory *api.InventorySnapshotDTO) string {
	if inventory == nil {
		return ""
	}
	return inventory.Arch
}

func normalizeSpace(value string) string {
	return strings.TrimSpace(value)
}

func normalizeLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeUpper(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeDigits(value string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func normalizeURLLike(value string) string {
	value = normalizeLower(value)
	value = strings.TrimSuffix(value, "/")
	return value
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
