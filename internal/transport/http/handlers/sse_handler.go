package handlers

import (
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"etalon-server/pkg/eventbus"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// SSEHandler управляет Server-Sent Events соединениями.
type SSEHandler struct {
	bus eventbus.EventBus
}

// NewSSEHandler создает новый экземпляр.
func NewSSEHandler(bus eventbus.EventBus) *SSEHandler {
	return &SSEHandler{bus: bus}
}

// RegisterRoutes регистрирует маршруты.
func (h *SSEHandler) RegisterRoutes(r chi.Router) {
	r.Get("/events", h.ServeEvents)
}

// ServeEvents обрабатывает подписку клиента на поток событий.
func (h *SSEHandler) ServeEvents(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	// 1. Проверка поддержки стриминга клиентом/сервером
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.RespondWithError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// 2. Установка заголовков SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*") // CORS, если не обработан middleware

	// 3. Подписка на события
	// Мы подписываемся на события, интересные фронтенду
	relevantEvents := []string{
		events.ServerPollingSucceeded, // Обновление статуса сервера
		events.ServerPollingFailed,
		events.TicketUpdated,              // Обновление тикета
		events.ServiceDeskCreateRequested, // Задача ушла в работу
		events.ServiceDeskUpdateRequested,
		events.DuplicatesFound, // Найдены дубликаты
		// Можно добавить events.AgentDataReceived, если фронт хочет видеть сырой поток
	}

	// Создаем канал через EventBus. Он закроется автоматически при r.Context().Done()
	eventChan := h.bus.SubscribeChannel(r.Context(), 10, relevantEvents...)

	log.Info("SSE клиент подключен", "remote", r.RemoteAddr)

	// Отправляем комментарий для инициализации соединения (некоторые прокси требуют данных)
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	// Тикер для keep-alive пингов (каждые 15 секунд)
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()

	// 4. Основной цикл
	for {
		select {
		case <-r.Context().Done():
			log.Info("SSE клиент отключился", "remote", r.RemoteAddr)
			return

		case <-pingTicker.C:
			// Отправка keep-alive комментария
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()

		case event, ok := <-eventChan:
			if !ok {
				return
			}
			// Формирование SSE сообщения
			// Format:
			// event: event_type
			// data: json_payload
			// \n

			dataBytes, err := json.Marshal(event.Payload)
			if err != nil {
				log.Error("Ошибка маршалинга события для SSE", "type", event.Type, "error", err)
				continue
			}

			fmt.Fprintf(w, "event: %s\n", event.Type)
			fmt.Fprintf(w, "data: %s\n\n", string(dataBytes))
			flusher.Flush()
		}
	}
}
