package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// AgentHandler обрабатывает HTTP-запросы от агентов.
type AgentHandler struct {
	logger       *zap.Logger
	agentService services.AgentService
}

// NewAgentHandler создает новый экземпляр обработчика.
func NewAgentHandler(logger *zap.Logger, agentService services.AgentService) *AgentHandler {
	return &AgentHandler{
		logger:       logger,
		agentService: agentService,
	}
}

// RegisterRoutes регистрирует все роуты для агентов.
func (h *AgentHandler) RegisterRoutes(r chi.Router) {
	r.Post("/register", h.registerAgent)
	r.Get("/{uuid}/config", h.getAgentConfig)
	r.Post("/{uuid}/data", h.postAgentData)
}

// registerAgent обрабатывает запрос на первичную регистрацию агента.
func (h *AgentHandler) registerAgent(w http.ResponseWriter, r *http.Request) {
	var dto api.RegistrationRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	// TODO: Добавить валидацию DTO

	_, err := h.agentService.RegisterAgent(r.Context(), &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentAlreadyExists):
			RespondWithError(w, http.StatusConflict, "Агент с таким UUID уже зарегистрирован")
		default:
			h.logger.Error("Ошибка регистрации агента", zap.String("uuid", dto.AgentUUID), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера при регистрации агента")
		}
		return
	}

	// В соответствии с протоколом, отвечаем 202 Accepted.
	// Агент поймет, что его запрос принят в обработку.
	w.WriteHeader(http.StatusAccepted)
	RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "регистрация принята в обработку"})
}

// getAgentConfig возвращает конфигурацию для агента.
func (h *AgentHandler) getAgentConfig(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	config, err := h.agentService.GetAgentConfig(r.Context(), uuid)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentNotFound):
			// Это штатная ситуация для агента, который еще не прошел регистрацию до конца.
			RespondWithError(w, http.StatusNotFound, "Агент не найден или его регистрация еще не завершена")
		default:
			h.logger.Error("Ошибка получения конфигурации агента", zap.String("uuid", uuid), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, config)
}

// postAgentData принимает и обрабатывает оперативные данные от агента.
func (h *AgentHandler) postAgentData(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	err := h.agentService.ProcessData(r.Context(), uuid, &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentNotFound):
			RespondWithError(w, http.StatusNotFound, "Агент не найден")
		default:
			h.logger.Error("Ошибка обработки данных от агента", zap.String("uuid", uuid), zap.Error(err))
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка при обработке данных")
		}
		return
	}

	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "данные приняты"})
}
