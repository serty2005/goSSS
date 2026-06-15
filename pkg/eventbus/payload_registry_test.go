package eventbus

import (
	"strings"
	"testing"
)

// TestEncodeDecodeRoundtrip проверяет, что событие с зарегистрированным
// payload-типом корректно сериализуется и восстанавливается с тем же типом,
// так чтобы type assertion у подписчика продолжал работать.
func TestEncodeDecodeRoundtrip(t *testing.T) {
	// Регистрируем тестовый payload-тип под уникальным именем события,
	// чтобы не зависеть от реестра internal/core/events.
	type testPayload struct {
		Field string
		Num   int
	}
	RegisterPayloadType("test.roundtrip.event", testPayload{})

	original := Event{
		Type:    "test.roundtrip.event",
		Payload: testPayload{Field: "значение", Num: 42},
	}

	data, err := encodeEvent(original)
	if err != nil {
		t.Fatalf("encodeEvent: %v", err)
	}

	decoded, err := decodeEvent(data)
	if err != nil {
		t.Fatalf("decodeEvent: %v", err)
	}

	if decoded.Type != original.Type {
		t.Fatalf("type: got %q want %q", decoded.Type, original.Type)
	}

	// Ключевая проверка: type assertion должен работать.
	got, ok := decoded.Payload.(testPayload)
	if !ok {
		t.Fatalf("payload type: ожидаем testPayload, получили %T", decoded.Payload)
	}
	if got.Field != "значение" {
		t.Fatalf("Field: got %q want %q", got.Field, "значение")
	}
	if got.Num != 42 {
		t.Fatalf("Num: got %d want %d", got.Num, 42)
	}
}

// TestDecodeUnknownTypeFallback проверяет, что для незарегистрированного типа
// события payload не теряется, а отдаётся как generic map. Это безопасный
// путь для событий без полезной нагрузки или broadcast-уведомлений.
func TestDecodeUnknownTypeFallback(t *testing.T) {
	RegisterPayloadType("test.unknown.before", struct{ X int }{X: 0})

	original := Event{
		Type:    "test.event.never.registered",
		Payload: map[string]any{"k": "v"},
	}

	data, err := encodeEvent(original)
	if err != nil {
		t.Fatalf("encodeEvent: %v", err)
	}

	decoded, err := decodeEvent(data)
	if err != nil {
		t.Fatalf("decodeEvent: %v", err)
	}

	got, ok := decoded.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type: ожидаем map[string]any, получили %T", decoded.Payload)
	}
	if got["k"] != "v" {
		t.Fatalf("k: got %v want %v", got["k"], "v")
	}
}

// TestDecodeNilPayload проверяет, что событие без payload корректно
// сериализуется и десериализуется (Payload=nil).
func TestDecodeNilPayload(t *testing.T) {
	original := Event{Type: "test.empty.event"}

	data, err := encodeEvent(original)
	if err != nil {
		t.Fatalf("encodeEvent: %v", err)
	}

	decoded, err := decodeEvent(data)
	if err != nil {
		t.Fatalf("decodeEvent: %v", err)
	}

	if decoded.Type != "test.empty.event" {
		t.Fatalf("type: got %q want %q", decoded.Type, "test.empty.event")
	}
	if decoded.Payload != nil {
		t.Fatalf("payload: ожидали nil, получили %T (%v)", decoded.Payload, decoded.Payload)
	}
}

// TestSubjectMatches проверяет правила матчинга subject-фильтров NATS.
func TestSubjectMatches(t *testing.T) {
	cases := []struct {
		filter string
		subject string
		want   bool
	}{
		{">", "anything.here", true},
		{"agent.>", "agent.data.received", true},
		{"agent.>", "agent", false}, // префикс без точки не матчится
		{"agent.>", "integration.other", false},
		{"ticket.updated", "ticket.updated", true},
		{"ticket.updated", "ticket.created", false},
		{"contracts.status.>", "contracts.status.recalculated", true},
	}
	for _, c := range cases {
		got := subjectMatches(c.filter, c.subject)
		if got != c.want {
			t.Errorf("subjectMatches(%q, %q) = %v, want %v", c.filter, c.subject, got, c.want)
		}
	}
}

// TestNATSNamesNoDot гарантирует, что имена стрима и durable-consumer-ов
// никогда не содержат точку — NATS запрещает её в этих идентификаторах
// (точка зарезервирована как разделитель subject-токенов). Регрессионный
// тест: ловит ошибку "invalid stream name: sss.agent".
func TestNATSNamesNoDot(t *testing.T) {
	bus := &NATSEventBus{prefix: sanitizeNATSName("sss"), streamName: "sss_events", instanceID: "p1"}

	// Имя стрима без точки.
	if strings.ContainsRune(bus.streamName, '.') {
		t.Errorf("имя стрима %q содержит точку", bus.streamName)
	}

	// Проверяем callback consumer-имена для событий с разными разделителями
	// (точки, многосегментные subject-ы).
	subjects := []string{
		"agent.data.received",
		"servicedesk.entity.updated",
		"pyrus.ticket.extid.sync.requested",
	}
	for _, subj := range subjects {
		name := bus.consumerName(subj)
		if strings.ContainsRune(name, '.') {
			t.Errorf("consumer name для %q = %q содержит точку", subj, name)
		}
	}

	// Broadcast durable тоже не должен содержать точку.
	broadcast := bus.broadcastConsumerName()
	if strings.ContainsRune(broadcast, '.') {
		t.Errorf("broadcast durable %q содержит точку", broadcast)
	}
}

// TestSanitizeNATSName проверяет базовую санитизацию недопустимых символов.
func TestSanitizeNATSName(t *testing.T) {
	cases := map[string]string{
		"sss":       "sss",
		"sss.agent": "sss_agent",
		"a b\tc":    "a_b_c",
		"a/b\\c":    "a_b_c",
	}
	for in, want := range cases {
		if got := sanitizeNATSName(in); got != want {
			t.Errorf("sanitizeNATSName(%q) = %q, want %q", in, got, want)
		}
	}
}
