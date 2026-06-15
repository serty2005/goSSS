package eventbus

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
)

// legacyDomainStreamNames — имена стримов из предыдущей (неверной) версии шины,
// в которой события группировались по доменным стримам с overlapping subject-ами.
// JetStream запрещает overlap, поэтому при старте шины эти стримы удаляются,
// если найдены, чтобы миграция прошла чисто.
var legacyDomainStreamNames = []string{
	"sss_agent", "sss_integration", "sss_processing", "sss_domain",
}

// callbackSub описывает подписку callback-обработчика воркера.
type callbackSub struct {
	subject string
	handler EventHandler
}

// consumerHandle связывает остановку потребителя с его ConsumeContext.
type consumerHandle struct {
	cancel context.CancelFunc
	cc     jetstream.ConsumeContext
}

// NATSEventBus реализует EventBus поверх NATS JetStream.
// Заменяет InMemoryEventBus при распределённом развёртывании сервисов.
//
// Модель: один стрим ловит все события шины по subject-префиксу (напр. "sss.>"),
// а разделение на группы потребителей идёт через FilterSubject в consumer-ах.
// Subject-префикс изолирует события шины от прочего трафика того же NATS-аккаунта
// и обязателен: JetStream запрещает стрим с subjects=">" в обычном режиме
// (err_code=10052 — capture-all требует work-queue/no-ack, что несовместимо
// с конкурентными потребителями).
type NATSEventBus struct {
	cfg           config.NATSConfig
	prefix        string // санитизированный префикс для имён стрима/consumer-ов (без точки)
	subjectPrefix string // префикс NATS-subject с точкой на конце (напр. "sss.")
	streamName    string

	nc *nats.Conn
	js jetstream.JetStream

	log logger.LoggerInterface

	mu         sync.Mutex
	callbacks  []callbackSub                  // накапливаются до Start()
	consumers  []consumerHandle               // для корректной остановки
	chanSubs   map[chan Event]*chanSubscriber // channel-подписчики (SSE)
	instanceID string                         // уникальный идентификатор процесса
	closeOnce  sync.Once
}

// NewNATSEventBus создаёт распределённую шину событий на NATS JetStream.
// Подключение выполняется сразу, создание стрима и потребителей — в Start.
func NewNATSEventBus(cfg config.NATSConfig) (*NATSEventBus, error) {
	if !cfg.Enabled || len(cfg.URLs) == 0 {
		return nil, fmt.Errorf("NATS-шина требует EVENT_BUS_BACKEND=nats и непустой NATS_URLS")
	}

	opts := []nats.Option{
		nats.Name("etalon-eventbus"),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.Timeout(5 * time.Second),
	}
	if cfg.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(cfg.CredsFile))
	}

	nc, err := nats.Connect(strings.Join(cfg.URLs, ","), opts...)
	if err != nil {
		return nil, fmt.Errorf("подключение к NATS %s: %w", cfg.URLs, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("создание JetStream context: %w", err)
	}

	rawPrefix := cfg.StreamPrefix
	if rawPrefix == "" {
		rawPrefix = "sss"
	}
	// subjectPrefix сохраняет точку как разделитель токенов (напр. "sss."),
	// а prefix (для имён стрима/consumer-ов) санитизируется — NATS запрещает
	// точку в этих идентификаторах.
	subjectPrefix := strings.TrimRight(rawPrefix, ".") + "."
	prefix := sanitizeNATSName(rawPrefix)

	return &NATSEventBus{
		cfg:           cfg,
		prefix:        prefix,
		subjectPrefix: subjectPrefix,
		streamName:    prefix + "_events",
		nc:            nc,
		js:            js,
		chanSubs:      make(map[chan Event]*chanSubscriber),
		instanceID:    fmt.Sprintf("p%d", time.Now().UnixNano()),
	}, nil
}

// Publish публикует событие в NATS. Subject равен типу события.
func (b *NATSEventBus) Publish(event Event) {
	if b.log != nil {
		b.log.Debug("Публикация события в NATS", "type", event.Type)
	}

	data, err := encodeEvent(event)
	if err != nil {
		if b.log != nil {
			b.log.Error("CRITICAL: не удалось сериализовать событие", "type", event.Type, "error", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Публикуем под subject-префиксом шины (напр. "sss.agent.data.received"),
	// чтобы стрим ловил только свои события по фильтру "sss.>".
	subject := b.subjectPrefix + event.Type
	_, err = b.js.Publish(ctx, subject, data,
		jetstream.WithRetryAttempts(3),
		jetstream.WithRetryWait(500*time.Millisecond),
	)
	if err != nil {
		if b.log != nil {
			b.log.Error("CRITICAL: публикация в NATS не удалась", "type", event.Type, "error", err)
		}
	}
}

// Subscribe регистрирует callback-обработчик. Реальная подписка — в Start.
func (b *NATSEventBus) Subscribe(eventType string, handler EventHandler) {
	b.mu.Lock()
	b.callbacks = append(b.callbacks, callbackSub{subject: eventType, handler: handler})
	b.mu.Unlock()
}

// SubscribeChannel создаёт канал и подписывает его на указанные типы событий
// (для SSE). Подписка выполняется в Start; канал живёт до отмены ctx.
func (b *NATSEventBus) SubscribeChannel(ctx context.Context, bufferSize int, eventTypes ...string) <-chan Event {
	ch := make(chan Event, bufferSize)

	topics := make(map[string]struct{}, len(eventTypes))
	for _, t := range eventTypes {
		topics[t] = struct{}{}
	}

	b.mu.Lock()
	b.chanSubs[ch] = &chanSubscriber{ch: ch, topics: topics}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.chanSubs, ch)
		b.mu.Unlock()
		if b.log != nil {
			b.log.Debug("Channel subscriber отключён")
		}
	}()

	return ch
}

// GetDebugInfo возвращает упрощённую диагностику состояния шины.
func (b *NATSEventBus) GetDebugInfo() DebugInfo {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := make(map[string]int)
	for _, c := range b.callbacks {
		subs[c.subject]++
	}
	return DebugInfo{
		CallbackSubs:     subs,
		ChannelSubsCount: len(b.chanSubs),
	}
}

// Start создаёт стрим, потребителей и блокирует до отмены ctx.
func (b *NATSEventBus) Start(ctx context.Context, log logger.LoggerInterface) {
	b.log = log
	log.Info("Запуск распределённой шины событий (NATS JetStream)...")

	if err := b.ensureStream(ctx); err != nil {
		log.Fatal("Не удалось подготовить JetStream-стрим", "error", err)
		return
	}

	if err := b.startCallbackConsumers(ctx); err != nil {
		log.Fatal("Не удалось запустить callback-потребителей NATS", "error", err)
		return
	}

	if err := b.startChannelConsumers(ctx); err != nil {
		log.Fatal("Не удалось запустить channel-потребителей NATS (SSE)", "error", err)
		return
	}

	log.Info("Шина событий NATS JetStream запущена",
		"stream", b.streamName, "callbacks", len(b.consumers))

	<-ctx.Done()
	log.Info("Шина событий NATS останавливается...")
	b.shutdown()
}

// ensureStream подготавливает единый стрим для всех событий шины и удаляет
// legacy overlapping-стримы из предыдущей версии, чтобы миграция прошла чисто.
func (b *NATSEventBus) ensureStream(ctx context.Context) error {
	// Удаляем legacy-стримы, если они есть от прошлой версии шины. Иначе их
	// overlapping subject-ы помешают создать единый стрим. Ошибки удаления
	// (стрим не найден) игнорируем.
	for _, name := range legacyDomainStreamNames {
		if err := b.js.DeleteStream(ctx, name); err == nil && b.log != nil {
			b.log.Info("Удалён legacy-стрим NATS (миграция)", "stream", name)
		}
	}

	// Стрим ловит все события шины по subject-префиксу (напр. "sss.>"). Не
	// используем ">" (capture-all): JetStream запрещает его в обычном режиме
	// (err_code=10052), т.к. capture-all требует work-queue-ретеншн, несовместимый
	// с конкурентными потребителями. Префикс также изолирует наши события.
	streamSubject := b.subjectPrefix + ">"
	_, err := b.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      b.streamName,
		Subjects:  []string{streamSubject},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    b.cfg.MaxAge,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("создание стрима %s: %w", b.streamName, err)
	}
	return nil
}

// startCallbackConsumers для каждой callback-подписки создаёт durable-consumer
// с FilterSubject по типу события и запускает Consume. Имя durable
// детерминировано по subject — все реплики с одинаковым именем делят сообщения
// (конкурентная обработка: одно сообщение получает только одна реплика группы).
func (b *NATSEventBus) startCallbackConsumers(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Группируем callbacks по subject — несколько обработчиков одного события
	// идут в один consumer и диспасчатся локально.
	grouped := make(map[string][]EventHandler)
	for _, c := range b.callbacks {
		grouped[c.subject] = append(grouped[c.subject], c.handler)
	}

	for subject, handlers := range grouped {
		durableName := b.consumerName(subject)
		if _, err := b.js.CreateOrUpdateConsumer(ctx, b.streamName, jetstream.ConsumerConfig{
			Durable:       durableName,
			FilterSubject: b.subjectPrefix + subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       30 * time.Second,
			MaxDeliver:    5,
		}); err != nil {
			return fmt.Errorf("создание consumer %s: %w", durableName, err)
		}

		consumeCtx, cancel := context.WithCancel(ctx)
		b.startConsume(consumeCtx, cancel, durableName, subject, handlers)
	}
	return nil
}

// startConsume запускает pull-consumer и диспасчит сообщения по обработчикам.
// Использует Consume с callback-паттерном (рекомендованный способ в nats.go).
func (b *NATSEventBus) startConsume(ctx context.Context, cancel context.CancelFunc, durableName, subject string, handlers []EventHandler) {
	cons, err := b.js.Consumer(ctx, b.streamName, durableName)
	if err != nil {
		if b.log != nil {
			b.log.Error("NATS consumer не получен", "consumer", durableName, "error", err)
		}
		cancel()
		return
	}

	handler := func(msg jetstream.Msg) {
		b.dispatchCallback(ctx, msg, handlers, durableName)
	}

	errHandler := func(consumeCtx jetstream.ConsumeContext, err error) {
		if b.log != nil {
			b.log.Warn("Ошибка consumer NATS", "consumer", durableName, "subject", subject, "error", err)
		}
	}

	cc, err := cons.Consume(handler,
		jetstream.ConsumeErrHandler(errHandler),
		jetstream.PullMaxMessages(64),
	)
	if err != nil {
		if b.log != nil {
			b.log.Error("Не удалось запустить consumer NATS", "consumer", durableName, "error", err)
		}
		cancel()
		return
	}

	b.consumers = append(b.consumers, consumerHandle{cancel: cancel, cc: cc})
}

// dispatchCallback декодирует сообщение и диспасчит его по обработчикам.
func (b *NATSEventBus) dispatchCallback(ctx context.Context, msg jetstream.Msg, handlers []EventHandler, durableName string) {
	defer func() {
		if r := recover(); r != nil {
			if b.log != nil {
				b.log.Error("panic в обработчике события NATS", "consumer", durableName, "panic", r)
			}
			_ = msg.Nak()
		}
	}()

	ev, err := decodeEvent(msg.Data())
	if err != nil {
		if b.log != nil {
			b.log.Error("Не удалось декодировать сообщение NATS, терминируем",
				"consumer", durableName, "error", err)
		}
		_ = msg.Term()
		return
	}

	for _, h := range handlers {
		h(ctx, ev)
	}
	if err := msg.Ack(); err != nil && b.log != nil {
		b.log.Warn("Не удалось подтвердить сообщение NATS",
			"consumer", durableName, "error", err)
	}
}

// startChannelConsumers создаёт один broadcast-consumer (на процесс) для
// доставки событий всем channel-подписчикам (SSE). Фильтрация по типам событий
// выполняется локально в fanoutBroadcast — это дешёво и даёт гибкость
// (подписчик может слушать произвольный набор типов).
func (b *NATSEventBus) startChannelConsumers(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.chanSubs) == 0 {
		return nil
	}

	durable := b.broadcastConsumerName()
	if _, err := b.js.CreateOrUpdateConsumer(ctx, b.streamName, jetstream.ConsumerConfig{
		Durable:           durable,
		FilterSubject:     b.subjectPrefix + ">",
		AckPolicy:         jetstream.AckNonePolicy,
		DeliverPolicy:     jetstream.DeliverLastPolicy,
		InactiveThreshold: 10 * time.Minute,
	}); err != nil {
		return fmt.Errorf("создание broadcast-consumer %s: %w", durable, err)
	}

	consumeCtx, cancel := context.WithCancel(ctx)
	cons, err := b.js.Consumer(consumeCtx, b.streamName, durable)
	if err != nil {
		if b.log != nil {
			b.log.Error("NATS broadcast consumer не получен", "consumer", durable, "error", err)
		}
		cancel()
		return err
	}

	handler := func(msg jetstream.Msg) {
		b.fanoutBroadcast(msg)
	}
	errHandler := func(consumeCtx jetstream.ConsumeContext, err error) {
		if b.log != nil {
			b.log.Warn("Ошибка broadcast consumer NATS", "consumer", durable, "error", err)
		}
	}

	cc, err := cons.Consume(handler, jetstream.ConsumeErrHandler(errHandler))
	if err != nil {
		if b.log != nil {
			b.log.Error("Не удалось запустить broadcast consumer NATS", "consumer", durable, "error", err)
		}
		cancel()
		return err
	}

	b.consumers = append(b.consumers, consumerHandle{cancel: cancel, cc: cc})
	return nil
}

// fanoutBroadcast раздаёт одно сообщение всем подходящим channel-подписчикам.
func (b *NATSEventBus) fanoutBroadcast(msg jetstream.Msg) {
	ev, err := decodeEvent(msg.Data())
	if err != nil {
		if b.log != nil {
			b.log.Warn("broadcast: не удалось декодировать событие", "error", err)
		}
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.chanSubs {
		_, specific := sub.topics[ev.Type]
		_, all := sub.topics["*"]
		if !specific && !all {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			if b.log != nil {
				b.log.Debug("broadcast: сброс события для медленного подписчика канала", "type", ev.Type)
			}
		}
	}
}

// consumerName строит детерминированное durable-имя для callback-consumer'а.
// Все реплики с одинаковым именем делят сообщения (конкурентная обработка).
// Точка в именах NATS запрещена, поэтому все разделители заменяем на "_".
func (b *NATSEventBus) consumerName(subject string) string {
	safe := strings.NewReplacer(".", "_", ":", "_", ">", "all", "*", "all").Replace(subject)
	return b.prefix + "_worker_" + safe
}

// broadcastConsumerName строит уникальное на процесс имя broadcast-consumer.
// Каждый под operator-api получает свою копию событий (broadcast).
// InactiveThreshold позволяет NATS автоматически удалять устаревшие consumers.
func (b *NATSEventBus) broadcastConsumerName() string {
	return b.prefix + "_broadcast_" + b.instanceID
}

// shutdown корректно завершает работу: останавливает потребителей и закрывает соединение.
func (b *NATSEventBus) shutdown() {
	b.closeOnce.Do(func() {
		for _, h := range b.consumers {
			if h.cc != nil {
				h.cc.Drain()
			}
			h.cancel()
		}
		b.mu.Lock()
		for ch := range b.chanSubs {
			close(ch)
		}
		b.chanSubs = make(map[chan Event]*chanSubscriber)
		b.mu.Unlock()

		if b.nc != nil {
			b.nc.Close()
		}
	})
}

// subjectMatches проверяет, подходит ли NATS subject-фильтр под тип события.
// Поддерживает wildcard ">" в конце фильтра. Семантика соответствует NATS:
// "agent.>" матчит только "agent.<something>", но не голый "agent".
func subjectMatches(filter, subject string) bool {
	if filter == ">" {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ".>")
		return strings.HasPrefix(subject, prefix+".")
	}
	return filter == subject
}

// sanitizeNATSName приводит строку к допустимому NATS-имени (для имён
// стримов и durable-consumer-ов). Точка, пробел, таб и слэш недопустимы
// и заменяются на "_". Subject-токены (где точка — разделитель) здесь не
// обрабатываются; используйте исходные subject-строки для фильтров.
func sanitizeNATSName(name string) string {
	return strings.NewReplacer(
		".", "_",
		" ", "_",
		"\t", "_",
		"/", "_",
		"\\", "_",
	).Replace(name)
}
