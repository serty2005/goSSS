package services

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerateSagaID_Детерминированность(t *testing.T) {
	// Одинаковые входы дают одинаковый результат в одни сутки.
	id1 := generateSagaID("agent-001", "fiscal-atol")
	id2 := generateSagaID("agent-001", "fiscal-atol")
	require.Equal(t, id1, id2)

	require.NotNil(t, id1)
	require.Contains(t, *id1, "agent-001")
	require.Contains(t, *id1, "fiscal-atol")
	require.Contains(t, *id1, time.Now().UTC().Format(sagaIDDateFormat))
}

func TestGenerateSagaID_РазныеАгенты_РазныеИдентификаторы(t *testing.T) {
	id1 := generateSagaID("agent-001", "fiscal-atol")
	id2 := generateSagaID("agent-002", "fiscal-atol")
	require.NotEqual(t, id1, id2)
}

func TestGenerateSagaID_РазныеАдаптеры_РазныеИдентификаторы(t *testing.T) {
	id1 := generateSagaID("agent-001", "fiscal-atol")
	id2 := generateSagaID("agent-001", "iiko-syrve")
	require.NotEqual(t, id1, id2)
}

func TestGenerateSagaID_Формат(t *testing.T) {
	id := generateSagaID("uuid-123", "adapter-xyz")
	require.NotNil(t, id)
	// Формат: {agent_uuid}/{adapter_id}/{date}
	parts := strings.SplitN(*id, "/", 3)
	require.Len(t, parts, 3)
	require.Equal(t, "uuid-123", parts[0])
	require.Equal(t, "adapter-xyz", parts[1])
	_, err := time.Parse(sagaIDDateFormat, parts[2])
	require.NoError(t, err, "третья часть должна быть валидной датой")
}
