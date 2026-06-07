package dtos

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentDataDTO_Unmarshal_SupportsAliasesAndStructuredLicenses(t *testing.T) {
	raw := []byte(`{
		"uuid": "agent-1",
		"vc": "2.3.2.2",
		"v_time": "2025-12-30 05:15:35",
		"ofdName": "ООО \"Ярус\"",
		"attribute_excise": true,
		"licenses": {
			"17": { "name": "Лицензия 17", "dateFrom": "2020-01-01 00:00:00", "dateUntil": "2030-01-01 00:00:00" }
		}
	}`)

	var dto AgentDataDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	require.Equal(t, "2.3.2.2", dto.VC)
	require.Equal(t, "2025-12-30 05:15:35", dto.VTime)
	require.Equal(t, "ООО \"Ярус\"", dto.OFDName)
	require.NotNil(t, dto.AttributeExcise)
	require.Equal(t, "true", *dto.AttributeExcise)
	require.Equal(t, "2030-01-01 00:00:00", dto.Licenses.Structured["17"].DateUntil)
}

func TestAgentDataDTO_Unmarshal_LegacyFiscalPayloadБезРегрессии(t *testing.T) {
	raw := []byte(`{
		"uuid": "legacy-agent",
		"agent_type": "getad",
		"hostname": "cash-01",
		"serialNumber": "SN-001",
		"RNM": "RNM-001",
		"fn_serial": "FN-001",
		"licenses": "legacy-license",
		"extra_field": "extra-value"
	}`)

	var dto AgentDataDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	require.Equal(t, "legacy-agent", dto.AgentUUID)
	require.Equal(t, "getad", dto.AgentType)
	require.Equal(t, "cash-01", dto.Hostname)
	require.Equal(t, "SN-001", dto.SerialNumber)
	require.Equal(t, "RNM-001", dto.RNM)
	require.Equal(t, "FN-001", dto.FNSerial)
	require.Equal(t, "legacy-license", dto.Licenses.Legacy)
	require.Nil(t, dto.Inventory)
	require.Empty(t, dto.AdapterStatuses)
	require.NotContains(t, dto.AdditionalProperties, "uuid")
	require.NotContains(t, dto.AdditionalProperties, "agent_type")
	require.Equal(t, "extra-value", dto.AdditionalProperties["extra_field"])
	require.JSONEq(t, string(raw), string(dto.RawPayload))
}

func TestAgentDataDTO_Unmarshal_ПоддерживаетAgentUUIDAlias(t *testing.T) {
	raw := []byte(`{
		"agent_uuid": "agent-alias",
		"hostname": "cash-alias",
		"new_getad_field": "visible"
	}`)

	var dto AgentDataDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	require.Equal(t, "agent-alias", dto.AgentUUID)
	require.Equal(t, "cash-alias", dto.Hostname)
	require.NotContains(t, dto.AdditionalProperties, "agent_uuid")
	require.Equal(t, "visible", dto.AdditionalProperties["new_getad_field"])
	require.JSONEq(t, string(raw), string(dto.RawPayload))
}

func TestAgentDataDTO_Unmarshal_InventoryИAdapterStatusesКакТипизированныеПоля(t *testing.T) {
	raw := []byte(`{
		"uuid": "agent-2",
		"inventory": {
			"collected_at": "2026-03-20T12:30:45Z",
			"hostname": "ws-22",
			"os": "windows",
			"arch": "amd64",
			"network_interfaces": [
				{
					"name": "Ethernet0",
					"index": 7,
					"addresses": ["10.0.0.10/24"],
					"flags": ["up", "broadcast"]
				}
			],
			"com_ports": [
				{"name": "COM3", "device": "USB Serial Device", "source": "registry"}
			],
			"installed_software": [
				{"name": "goSSSagent", "version": "1.2.3", "publisher": "Etalon"}
			],
			"known_components": [
				{
					"key": "kkm_driver",
					"name": "Драйвер ККМ",
					"detected": true,
					"evidence": [{"type": "file", "value": "C:/kkm/driver.exe"}]
				}
			]
		},
		"adapter_statuses": [
			{
				"adapter_id": "atol",
				"adapter_type": "fiscal",
				"version": "0.1.0",
				"status": "ready",
				"local_path": "C:/adapters/atol.exe",
				"file_size": 1234,
				"sha256": "abc",
				"updated_at": "2026-03-20T12:31:00Z"
			}
		],
		"unknown_phase0": "kept"
	}`)

	var dto AgentDataDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	require.NotNil(t, dto.Inventory)
	require.Equal(t, time.Date(2026, 3, 20, 12, 30, 45, 0, time.UTC), dto.Inventory.CollectedAt)
	require.Equal(t, "ws-22", dto.Inventory.Hostname)
	require.Len(t, dto.Inventory.NetworkInterfaces, 1)
	require.Equal(t, "Ethernet0", dto.Inventory.NetworkInterfaces[0].Name)
	require.Len(t, dto.Inventory.COMPorts, 1)
	require.Equal(t, "COM3", dto.Inventory.COMPorts[0].Name)
	require.Len(t, dto.Inventory.InstalledSoftware, 1)
	require.Equal(t, "goSSSagent", dto.Inventory.InstalledSoftware[0].Name)
	require.Len(t, dto.Inventory.KnownComponents, 1)
	require.True(t, dto.Inventory.KnownComponents[0].Detected)
	require.Len(t, dto.AdapterStatuses, 1)
	require.Equal(t, "atol", dto.AdapterStatuses[0].AdapterID)
	require.Equal(t, "ready", dto.AdapterStatuses[0].Status)
	require.Equal(t, int64(1234), dto.AdapterStatuses[0].FileSize)
	require.NotContains(t, dto.AdditionalProperties, "inventory")
	require.NotContains(t, dto.AdditionalProperties, "adapter_statuses")
	require.Equal(t, "kept", dto.AdditionalProperties["unknown_phase0"])
}

func TestAgentHeartbeatResponseDTO_Marshal_СериализуетAdapterManifests(t *testing.T) {
	manifests := []AdapterManifestDTO{
		{
			AdapterID:       "atol",
			AdapterType:     "fiscal",
			Version:         "1.0.0",
			ProtocolVersion: "phase0",
			DownloadURL:     "https://example.test/atol.exe",
			SHA256:          "abc123",
		},
	}

	body, err := json.Marshal(AgentHeartbeatResponseDTO{
		Status:           "ok",
		AdapterManifests: &manifests,
	})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "ok", decoded["status"])
	rawManifests, ok := decoded["adapter_manifests"].([]any)
	require.True(t, ok)
	require.Len(t, rawManifests, 1)
}
