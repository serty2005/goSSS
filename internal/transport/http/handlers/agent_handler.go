package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AgentHandler обрабатывает HTTP-запросы от агентов.
type AgentHandler struct {
	agentService services.AgentService
}

// NewAgentHandler создает новый экземпляр обработчика.
func NewAgentHandler(agentService services.AgentService) *AgentHandler {
	return &AgentHandler{
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
	log := middleware.GetLogger(r.Context())
	log.Info("Получен запрос на регистрацию агента", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr)

	var dto api.RegistrationRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Debug("Ошибка декодирования тела запроса регистрации агента", "error", err)
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	log.Debug("Декодирован запрос регистрации агента", "uuid", dto.AgentUUID, "hostname", dto.Hostname)

	// TODO: Добавить валидацию DTO

	_, err := h.agentService.RegisterAgent(r.Context(), &dto)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			log.Info("Попытка повторной регистрации существующего агента", "uuid", dto.AgentUUID)
			response.RespondWithError(w, http.StatusConflict, "Агент с таким UUID уже зарегистрирован")
			return
		}
		log.Error("register failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	log.Info("Регистрация агента успешно принята в обработку", "uuid", dto.AgentUUID)

	// В соответствии с протоколом, отвечаем 202 Accepted.
	// Агент поймет, что его запрос принят в обработку.
	w.WriteHeader(http.StatusAccepted)
	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "регистрация принята в обработку"})
}

// getAgentConfig возвращает конфигурацию для агента.
func (h *AgentHandler) getAgentConfig(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	log.Info("Получен запрос на получение конфигурации агента", "method", r.Method, "path", r.URL.Path)

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		log.Warn("Запрос конфигурации агента без указания UUID", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	log.Debug("Извлечен UUID агента из запроса", "uuid", uuid)

	config, err := h.agentService.GetAgentConfig(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			log.Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		log.Error("get config failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	log.Info("Конфигурация агента успешно отправлена", "uuid", uuid)
	response.RespondWithJSON(w, http.StatusOK, config)
}

// postAgentData принимает и обрабатывает оперативные данные от агента.
func (h *AgentHandler) postAgentData(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	log.Info("Получен запрос с данными от агента", "method", r.Method, "path", r.URL.Path)

	uuid := chi.URLParam(r, "uuid")
	if uuid == "" {
		log.Warn("Запрос данных от агента без указания UUID", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusBadRequest, "UUID агента не указан")
		return
	}

	log.Debug("Извлечен UUID агента из запроса данных", "uuid", uuid)

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Debug("Ошибка декодирования тела запроса с данными агента", "uuid", uuid, "error", err)
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	log.Debug("Декодированы данные от агента", "uuid", uuid, "data_type", "AgentDataDTO")

	err := h.agentService.ProcessData(r.Context(), uuid, &dto)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			log.Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		log.Error("process data failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	log.Info("Данные от агента успешно обработаны", "uuid", uuid)
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "данные приняты"})
}
