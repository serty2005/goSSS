package dtos

import (
	"encoding/json"
	"testing"

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
