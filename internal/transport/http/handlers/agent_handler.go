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
	apiKey       string // Ключ для простой авторизации (query param)
}

// NewAgentHandler создает новый экземпляр обработчика.
// ВАЖНО: Мы добавили аргумент apiKey. Обнови вызов в app.go!
func NewAgentHandler(agentService services.AgentService, apiKey string) *AgentHandler {
	return &AgentHandler{
		agentService: agentService,
		apiKey:       apiKey,
	}
}

// RegisterRoutes регистрирует все роуты для агентов.
func (h *AgentHandler) RegisterRoutes(r chi.Router) {
	// Старые роуты (обычно защищены Middleware AgentAuth с заголовком Bearer)
	r.Post("/register", h.registerAgent)
	r.Get("/{uuid}/config", h.getAgentConfig)
	r.Post("/{uuid}/data", h.postAgentData)

	// Новый роут для "тупых" агентов (getad) или скриптов, передающих ключ в URL
	r.Post("/report", h.handleAgentReport)

}

// registerAgent обрабатывает запрос на первичную регистрацию агента.
func (h *AgentHandler) registerAgent(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	var dto api.RegistrationRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	_, err := h.agentService.RegisterAgent(r.Context(), &dto)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			response.RespondWithError(w, http.StatusConflict, "Агент с таким UUID уже зарегистрирован")
			return
		}
		log.Error("register failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	log.Info("Регистрация агента успешно принята", "uuid", dto.AgentUUID)
	w.WriteHeader(http.StatusAccepted)
	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "регистрация принята в обработку"})
}

// getAgentConfig возвращает конфигурацию для агента.
func (h *AgentHandler) getAgentConfig(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	uuid := chi.URLParam(r, "uuid")

	config, err := h.agentService.GetAgentConfig(r.Context(), uuid)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		log.Error("get config failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, config)
}

// postAgentData принимает данные от агента (стандартный путь с UUID в URL).
func (h *AgentHandler) postAgentData(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	uuid := chi.URLParam(r, "uuid")

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	// Вызываем сервис, который теперь возвращает структуру с задачами
	respData, err := h.agentService.ProcessData(r.Context(), uuid, &dto)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден (требуется регистрация)")
			return
		}
		log.Error("process data failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	// Возвращаем JSON с задачами (AgentHeartbeatResponseDTO)
	response.RespondWithJSON(w, http.StatusOK, respData)
}

// handleAgentReport принимает данные через /report?key=TOKEN.
// Поддерживает агентов getad, sssruner и простые curl-скрипты.
func (h *AgentHandler) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	// 1. Проверка ключа (без изменений)
	requestKey := r.URL.Query().Get("key")
	if h.apiKey != "" && requestKey != h.apiKey {
		log.Warn("Неверный API ключ в запросе /report", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusUnauthorized, "Invalid API Key")
		return
	}

	// 2. Декодирование
	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Warn("Ошибка декодирования JSON в /report", "error", err)
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// 3. НОРМАЛИЗАЦИЯ ДАННЫХ (Fix для getad)

	// Если UUID пришел в поле "uuid" (попадает в AdditionalProperties), а не "agent_uuid"
	if dto.AgentUUID == "" {
		if val, ok := dto.AdditionalProperties["uuid"]; ok {
			if strVal, ok := val.(string); ok {
				dto.AgentUUID = strVal
			}
		}
	}

	// Если UUID всё еще пуст, пробуем найти serialNumber или hostname как резервный ID?
	// Пока требуем UUID.
	if dto.AgentUUID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Field 'uuid' or 'agent_uuid' is required")
		return
	}

	// Принудительно ставим тип "getad", так как этот эндпоинт специфичен для простых репортеров,
	// которые не умеют в сложный протокол.
	if dto.AgentType == "" {
		dto.AgentType = "getad"
	}

	// 4. Обработка
	respData, err := h.agentService.ProcessData(r.Context(), dto.AgentUUID, &dto)
	if err != nil {
		log.Error("handleAgentReport failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal processing error")
		return
	}

	// 5. Ответ
	response.RespondWithJSON(w, http.StatusOK, respData)
}

func (h *AgentHandler) HandleSubmitJSON(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	// 1. Авторизация через заголовок X-API-Key
	// Значение заголовка должно совпадать с h.apiKey (наш "стандартный uuid" из конфига)
	clientKey := r.Header.Get("X-API-Key")
	if h.apiKey != "" && clientKey != h.apiKey {
		log.Warn("Неверный X-API-Key в запросе /submit_json", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusUnauthorized, "Invalid API Key")
		return
	}

	// 2. Декодирование JSON
	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Warn("Ошибка декодирования JSON в /submit_json", "error", err)
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	// 3. Нормализация данных (Logic Reuse)
	// getad часто шлет uuid в корневом объекте, который попадает в AdditionalProperties["uuid"]
	if dto.AgentUUID == "" {
		if val, ok := dto.AdditionalProperties["uuid"]; ok {
			if strVal, ok := val.(string); ok {
				dto.AgentUUID = strVal
			}
		}
	}

	if dto.AgentUUID == "" {
		// Логируем тело для отладки, если UUID не найден
		log.Warn("В запросе submit_json не найден uuid")
		response.RespondWithError(w, http.StatusBadRequest, "Field 'uuid' is required")
		return
	}

	// Принудительно выставляем тип getad
	dto.AgentType = "getad"

	// 4. Обработка через сервис (Auto-Registration уже там реализована)
	respData, err := h.agentService.ProcessData(r.Context(), dto.AgentUUID, &dto)
	if err != nil {
		log.Error("handleSubmitJSON process failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal processing error")
		return
	}

	// 5. Успешный ответ
	response.RespondWithJSON(w, http.StatusOK, respData)
}
