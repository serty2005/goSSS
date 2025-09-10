package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/logger"
	"etalon-server/internal/services"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AgentHandler обрабатывает HTTP-запросы от агентов.
type AgentHandler struct {
	logger       logger.LoggerInterface
	agentService services.AgentService
}

// NewAgentHandler создает новый экземпляр обработчика.
func NewAgentHandler(logger logger.LoggerInterface, agentService services.AgentService) *AgentHandler {
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
	h.logger.Info("Получен запрос на регистрацию агента", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)

	var dto api.RegistrationRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		h.logger.Debug("Ошибка декодирования тела запроса регистрации агента", "error", err)
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	h.logger.Debug("Декодирован запрос регистрации агента", "uuid", dto.AgentUUID, "hostname", dto.Hostname)

	// TODO: Добавить валидацию DTO

	_, err := h.agentService.RegisterAgent(r.Context(), &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentAlreadyExists):
			h.logger.Info("Попытка повторной регистрации существующего агента", "uuid", dto.AgentUUID)
			RespondWithError(w, http.StatusConflict, "Агент с таким UUID уже зарегистрирован")
		default:
			h.logger.Error("Ошибка регистрации агента", "uuid", dto.AgentUUID, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера при регистрации агента")
		}
		return
	}

	h.logger.Info("Регистрация агента успешно принята в обработку", "uuid", dto.AgentUUID)

	// В соответствии с протоколом, отвечаем 202 Accepted.
	// Агент поймет, что его запрос принят в обработку.
	w.WriteHeader(http.StatusAccepted)
	RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "регистрация принята в обработку"})
}

// getAgentConfig возвращает конфигурацию для агента.
func (h *AgentHandler) getAgentConfig(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Получен запрос на получение конфигурации агента", "method", r.Method, "path", r.URL.Path)

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		h.logger.Warn("Запрос конфигурации агента без указания UUID", "remote_addr", r.RemoteAddr)
		RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	h.logger.Debug("Извлечен UUID агента из запроса", "uuid", uuid)

	config, err := h.agentService.GetAgentConfig(r.Context(), uuid)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentNotFound):
			// Это штатная ситуация для агента, который еще не прошел регистрацию до конца.
			h.logger.Info("Агент не найден при запросе конфигурации", "uuid", uuid)
			RespondWithError(w, http.StatusNotFound, "Агент не найден или его регистрация еще не завершена")
		default:
			h.logger.Error("Ошибка получения конфигурации агента", "uuid", uuid, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		}
		return
	}

	h.logger.Info("Конфигурация агента успешно отправлена", "uuid", uuid)
	RespondWithJSON(w, http.StatusOK, config)
}

// postAgentData принимает и обрабатывает оперативные данные от агента.
func (h *AgentHandler) postAgentData(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Получен запрос с данными от агента", "method", r.Method, "path", r.URL.Path)

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		h.logger.Warn("Запрос данных от агента без указания UUID", "remote_addr", r.RemoteAddr)
		RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	h.logger.Debug("Извлечен UUID агента из запроса данных", "uuid", uuid)

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		h.logger.Debug("Ошибка декодирования тела запроса с данными агента", "uuid", uuid, "error", err)
		RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	h.logger.Debug("Декодированы данные от агента", "uuid", uuid, "data_type", "AgentDataDTO")

	err := h.agentService.ProcessData(r.Context(), uuid, &dto)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAgentNotFound):
			h.logger.Info("Агент не найден при обработке данных", "uuid", uuid)
			RespondWithError(w, http.StatusNotFound, "Агент не найден")
		default:
			h.logger.Error("Ошибка обработки данных от агента", "uuid", uuid, "error", err)
			RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка при обработке данных")
		}
		return
	}

	h.logger.Info("Данные от агента успешно обработаны", "uuid", uuid)
	RespondWithJSON(w, http.StatusOK, map[string]string{"status": "данные приняты"})
}
