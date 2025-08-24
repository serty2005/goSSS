package handlers

import (
	"etalon-server/pkg/eventbus"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// DebugHandler обрабатывает запросы к отладочным эндпоинтам.
type DebugHandler struct {
	logger *zap.Logger
	bus    eventbus.EventBus
}

// NewDebugHandler создает новый экземпляр обработчика.
func NewDebugHandler(logger *zap.Logger, bus eventbus.EventBus) *DebugHandler {
	return &DebugHandler{
		logger: logger,
		bus:    bus,
	}
}

// RegisterRoutes регистрирует роуты для отладки.
func (h *DebugHandler) RegisterRoutes(r chi.Router) {
	r.Get("/bus", h.getBusStatus)
}

// getBusStatus возвращает текущее состояние шины событий.
func (h *DebugHandler) getBusStatus(w http.ResponseWriter, r *http.Request) {
	debugInfo := h.bus.GetDebugInfo()
	RespondWithJSON(w, http.StatusOK, debugInfo)
}