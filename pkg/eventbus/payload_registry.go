package eventbus

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// payloadRegistry сопоставляет тип события с Go-типом его полезной нагрузки.
// Используется NATS-реализацией шины для восстановления конкретного типа
// события при десериализации: подписчики выполняют type assertion
// (напр. event.Payload.(events.AgentObservationPayload)), поэтому
// недостаточно отдать map[string]interface{} — нужен экземпляр правильного типа.
//
// Реестр намеренно живёт в pkg/eventbus и ничего не знает о доменных типах:
// типы регистрируются через RegisterPayloadType инициализацией в пакете
// internal/core/events, что исключает цикл импортов.
var (
	payloadMu       sync.RWMutex
	payloadTypes    = make(map[string]reflect.Type) // eventType -> concrete payload type
)

// RegisterPayloadType связывает тип события с Go-типом его полезной нагрузки.
// Передаваемый sample должен быть значением (не указателем) целевого типа payload.
// Повторная регистрация того же типа события перезаписывает предыдущую.
func RegisterPayloadType(eventType string, sample any) {
	payloadMu.Lock()
	defer payloadMu.Unlock()
	payloadTypes[eventType] = reflect.TypeOf(sample)
}

// payloadEnvelope описываетtransport-сообщение шины поверх NATS.
type payloadEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// encodeEvent сериализует событие в envelope для передачи по NATS.
func encodeEvent(event Event) ([]byte, error) {
	var raw json.RawMessage
	if event.Payload != nil {
		data, err := json.Marshal(event.Payload)
		if err != nil {
			return nil, fmt.Errorf("сериализация payload события %q: %w", event.Type, err)
		}
		raw = data
	}
	return json.Marshal(payloadEnvelope{Type: event.Type, Payload: raw})
}

// decodeEvent восстанавливает событие из сообщения NATS.
// Если для типа события зарегистрирован конкретный payload-тип,
// payload десериализуется в экземпляр этого типа (и подписчик сможет
// сделать type assertion). Иначе payload остаётся nil — это безопасно
// для событий без полезной нагрузки (напр. broadcast-уведомлений).
func decodeEvent(data []byte) (Event, error) {
	var env payloadEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Event{}, fmt.Errorf("десериализация envelope события: %w", err)
	}

	ev := Event{Type: env.Type}
	// Пустой payload или JSON null — нет полезной нагрузки.
	trimmed := strings.TrimSpace(string(env.Payload))
	if len(env.Payload) == 0 || trimmed == "null" {
		return ev, nil
	}

	payloadMu.RLock()
	concrete, ok := payloadTypes[env.Type]
	payloadMu.RUnlock()
	if !ok {
		// Тип не зарегистрирован — отдаём generic map, чтобы не терять данные.
		// Подписчики, не делающие type assertion, смогут их прочитать.
		var generic map[string]any
		if err := json.Unmarshal(env.Payload, &generic); err != nil {
			return Event{}, fmt.Errorf("десериализация payload события %q: %w", env.Type, err)
		}
		ev.Payload = generic
		return ev, nil
	}

	instance := reflect.New(concrete).Interface()
	if err := json.Unmarshal(env.Payload, instance); err != nil {
		return Event{}, fmt.Errorf("десериализация payload события %q в %s: %w", env.Type, concrete.String(), err)
	}

	// Регистрируем значение (не указатель): type assertion потребителя
	// обычно идёт по значению, например events.AgentObservationPayload.
	ev.Payload = reflect.ValueOf(instance).Elem().Interface()
	return ev, nil
}
