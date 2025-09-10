package eventbus

import (
	"context"
	"sync"

	"etalon-server/internal/logger"
)

// Event представляет собой событие, передаваемое по шине.
type Event struct {
	Type    string      // Тип события, например, "servicedesk.entity.updated"
	Payload interface{} // Полезная нагрузка события
}

// EventHandler - это функция-обработчик для определенного типа события.
type EventHandler func(ctx context.Context, event Event)

// DebugInfo содержит отладочную информацию о состоянии шины.
type DebugInfo struct {
	QueueLength   int            `json:"queue_length"`
	QueueCapacity int            `json:"queue_capacity"`
	Subscribers   map[string]int `json:"subscribers"` // Карта: eventType -> count of handlers
}

// EventBus определяет интерфейс для асинхронной шины событий.
type EventBus interface {
	Publish(event Event)
	Subscribe(eventType string, handler EventHandler)
	Start(ctx context.Context, logger logger.LoggerInterface)
	GetDebugInfo() DebugInfo
}

// InMemoryEventBus - это реализация EventBus в памяти с использованием каналов Go.
type InMemoryEventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]EventHandler
	events      chan Event
	logger      logger.LoggerInterface
}

// NewInMemoryEventBus создает новый экземпляр InMemoryEventBus.
func NewInMemoryEventBus(bufferSize int) *InMemoryEventBus {
	return &InMemoryEventBus{
		subscribers: make(map[string][]EventHandler),
		events:      make(chan Event, bufferSize),
	}
}

// Publish отправляет событие в шину. Метод неблокирующий.
func (b *InMemoryEventBus) Publish(event Event) {
	// Логируем событие ПЕРЕД отправкой в канал
	if b.logger != nil {
		b.logger.Debug("Публикация события в шину",
			"type", event.Type,
			"queue_len", len(b.events), // Текущая длина очереди
			"queue_cap", cap(b.events), // Вместимость очереди
		)
	}
	b.events <- event
}

// Subscribe подписывает обработчик на определенный тип события.
func (b *InMemoryEventBus) Subscribe(eventType string, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[eventType] = append(b.subscribers[eventType], handler)
}

// GetDebugInfo возвращает отладочную информацию о текущем состоянии шины.
func (b *InMemoryEventBus) GetDebugInfo() DebugInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs := make(map[string]int)
	for eventType, handlers := range b.subscribers {
		subs[eventType] = len(handlers)
	}

	return DebugInfo{
		QueueLength:   len(b.events),
		QueueCapacity: cap(b.events),
		Subscribers:   subs,
	}
}

// Start запускает основной цикл обработки событий.
func (b *InMemoryEventBus) Start(ctx context.Context, logger logger.LoggerInterface) {
	b.logger = logger // Сохраняем логгер
	logger.Info("Шина событий запущена и готова к обработке.")
	for {
		select {
		case event := <-b.events:
			b.logger.Debug("Шина извлекла событие из очереди для обработки", "type", event.Type)

			b.mu.RLock()
			handlers, ok := b.subscribers[event.Type]
			b.mu.RUnlock()

			if ok {
				for _, handler := range handlers {
					go handler(ctx, event)
				}
			}
		case <-ctx.Done():
			logger.Info("Шина событий останавливается.")
			close(b.events)
			return
		}
	}
}
