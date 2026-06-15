package eventbus

import (
	"fmt"

	"etalon-server/internal/infra/config"
)

// New создаёт шину событий по выбранному бэкенду из конфигурации.
//
// EVENT_BUS_BACKEND=memory (по умолчанию) — in-process InMemoryEventBus.
// EVENT_BUS_BACKEND=nats — распределённая NATSEventBus поверх JetStream.
//
// Если выбран nats, но NATS недоступен (URLS пуст или подключение не удалось),
// возвращается ошибка — caller (app.New) решает, фаталить или откатиться на memory.
func New(cfg *config.Config) (EventBus, error) {
	switch cfg.EventBusBackend {
	case config.EventBusBackendNATS, "jetstream":
		bus, err := NewNATSEventBus(cfg.NATS)
		if err != nil {
			return nil, fmt.Errorf("создание NATS-шины: %w", err)
		}
		return bus, nil
	case config.EventBusBackendMemory, "":
		return NewInMemoryEventBus(10000), nil
	default:
		return nil, fmt.Errorf("неизвестный EVENT_BUS_BACKEND=%q (допустимо: memory, nats)", cfg.EventBusBackend)
	}
}
