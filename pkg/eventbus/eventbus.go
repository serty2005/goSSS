package eventbus

import (
	"context"
	"etalon-server/internal/infra/logger"
	"sync"
)

// Event представляет собой событие, передаваемое по шине.
type Event struct {
	Type    string      // Тип события, например, "servicedesk.entity.updated"
	Payload interface{} // Полезная нагрузка события
}

// EventHandler - это функция-обработчик для определенного типа события (Legacy callback approach).
type EventHandler func(ctx context.Context, event Event)

// DebugInfo содержит отладочную информацию о состоянии шины.
type DebugInfo struct {
	QueueLength      int            `json:"queue_length"`
	QueueCapacity    int            `json:"queue_capacity"`
	CallbackSubs     map[string]int `json:"callback_subscribers"` // Карта: eventType -> count of handlers
	ChannelSubsCount int            `json:"channel_subscribers_count"`
}

// EventBus определяет интерфейс для асинхронной шины событий.
type EventBus interface {
	Publish(event Event)

	// Subscribe подписывает функцию-обработчик (для внутренних воркеров).
	Subscribe(eventType string, handler EventHandler)

	// SubscribeChannel создает канал и подписывает его на указанные типы событий (для SSE/Streaming).
	// Канал будет закрыт автоматически при отмене контекста.
	SubscribeChannel(ctx context.Context, bufferSize int, eventTypes ...string) <-chan Event

	Start(ctx context.Context, logger logger.LoggerInterface)
	GetDebugInfo() DebugInfo
}

// chanSubscriber хранит информацию о подписчике через канал.
type chanSubscriber struct {
	ch     chan Event
	topics map[string]struct{} // Set of event types this subscriber is interested in
}

// InMemoryEventBus - это реализация EventBus в памяти.
type InMemoryEventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]EventHandler      // Старые callback-подписчики
	chanSubs    map[chan Event]*chanSubscriber // Новые channel-подписчики
	events      chan Event
	logger      logger.LoggerInterface
}

// NewInMemoryEventBus создает новый экземпляр InMemoryEventBus.
func NewInMemoryEventBus(bufferSize int) *InMemoryEventBus {
	return &InMemoryEventBus{
		subscribers: make(map[string][]EventHandler),
		chanSubs:    make(map[chan Event]*chanSubscriber),
		events:      make(chan Event, bufferSize),
	}
}

// Publish отправляет событие в шину. Метод неблокирующий.
func (b *InMemoryEventBus) Publish(event Event) {
	if b.logger != nil {
		// Логируем только debug, чтобы не засорять прод логи
		b.logger.Debug("Публикация события в шину", "type", event.Type)
	}

	// Используем select-default для отправки в главную очередь, чтобы не блокировать паблишера,
	// если шина переполнена.
	select {
	case b.events <- event:
	default:
		if b.logger != nil {
			b.logger.Error("Шина событий переполнена! Событие сброшено.", "type", event.Type)
		}
	}
}

// Subscribe подписывает обработчик-функцию.
func (b *InMemoryEventBus) Subscribe(eventType string, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers[eventType] = append(b.subscribers[eventType], handler)
}

// SubscribeChannel возвращает канал для чтения событий.
func (b *InMemoryEventBus) SubscribeChannel(ctx context.Context, bufferSize int, eventTypes ...string) <-chan Event {
	// Создаем канал
	ch := make(chan Event, bufferSize)

	// Создаем set топиков для быстрого поиска
	topics := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		topics[t] = struct{}{}
	}

	b.mu.Lock()
	b.chanSubs[ch] = &chanSubscriber{
		ch:     ch,
		topics: topics,
	}
	b.mu.Unlock()

	// Запускаем горутину для очистки при отмене контекста клиента
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.chanSubs, ch)
		b.mu.Unlock()
		close(ch)
		if b.logger != nil {
			b.logger.Debug("Channel subscriber disconnected and removed")
		}
	}()

	return ch
}

// GetDebugInfo возвращает статистику.
func (b *InMemoryEventBus) GetDebugInfo() DebugInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs := make(map[string]int)
	for eventType, handlers := range b.subscribers {
		subs[eventType] = len(handlers)
	}

	return DebugInfo{
		QueueLength:      len(b.events),
		QueueCapacity:    cap(b.events),
		CallbackSubs:     subs,
		ChannelSubsCount: len(b.chanSubs),
	}
}

// Start запускает основной цикл обработки событий.
func (b *InMemoryEventBus) Start(ctx context.Context, logger logger.LoggerInterface) {
	b.logger = logger
	logger.Info("Шина событий запущена и готова к обработке.")
	for {
		select {
		case event := <-b.events:
			b.processEvent(ctx, event)
		case <-ctx.Done():
			logger.Info("Шина событий останавливается. Закрытие каналов подписчиков...")
			b.closeAllChannels() // Закрываем каналы при остановке контекста
			return
		}
	}
}

// closeAllChannels закрывает все каналы стриминговых подписчиков.
func (b *InMemoryEventBus) closeAllChannels() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.chanSubs {
		close(ch) // Это заставит SSEHandler выйти из цикла (ok станет false)
	}
	// Очищаем карту, чтобы сборщик мусора сделал своё дело
	b.chanSubs = make(map[chan Event]*chanSubscriber)
}

func (b *InMemoryEventBus) processEvent(ctx context.Context, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 1. Обработка Callback subscribers (Воркеры)
	// Запускаем в отдельных горутинах
	if handlers, ok := b.subscribers[event.Type]; ok {
		for _, handler := range handlers {
			go handler(ctx, event)
		}
	}

	// 2. Обработка Channel subscribers (SSE / Websockets)
	for ch, sub := range b.chanSubs {
		// Проверяем, подписан ли этот канал на данный тип события или на wildcard "*"
		_, specific := sub.topics[event.Type]
		_, all := sub.topics["*"]

		if specific || all {
			// Non-blocking send. Если клиент медленный (канал полон), пропускаем сообщение.
			select {
			case ch <- event:
			default:
				// Можно логировать дропы, но осторожно, чтобы не спамить
				b.logger.Debug("Сброс события для медленного подписчика канала", "type", event.Type)
			}
		}
	}
}
