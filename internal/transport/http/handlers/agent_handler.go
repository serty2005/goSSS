package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/services"
	"etalon-server/internal/services/agentauth"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// AgentHandler обрабатывает HTTP-запросы от агентов.
// Важно: старые пассивные агенты работают через /api/submit_json и не зависят от новых токенов.
type AgentHandler struct {
	agentService services.AgentService
	agentAuth    agentauth.Service
	apiKey       string
}

func NewAgentHandler(agentService services.AgentService, agentAuth agentauth.Service, apiKey string) *AgentHandler {
	return &AgentHandler{
		agentService: agentService,
		agentAuth:    agentAuth,
		apiKey:       apiKey,
	}
}

func (h *AgentHandler) RegisterRoutes(r chi.Router) {
	r.Post("/register", h.registerAgent)
	r.Post("/auth/refresh", h.refreshAgentToken)
	r.Get("/{uuid}/config", h.getAgentConfig)
	r.Post("/{uuid}/data", h.postAgentData)

	// Совместимость со старыми "простыми" агентами/скриптами.
	r.Post("/report", h.handleAgentReport)
}

// registerAgent — регистрация нового активного агента (sssruner).
// Использует bootstrap API key и выдает access/refresh токены.
func (h *AgentHandler) registerAgent(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	rawBody, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Не удалось прочитать тело запроса")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(rawBody))

	var dto api.RegistrationRequestDTO
	parseErr := json.Unmarshal(rawBody, &dto)
	meta := agentauth.RegistrationAttemptMeta{
		RemoteAddr: r.RemoteAddr,
		RawPayload: rawBody,
	}

	if authError := h.validateBootstrapKey(r); authError != "" {
		_ = h.agentAuth.RecordRegistrationAttempt(r.Context(), registrationRequestOrNil(parseErr, &dto), meta, agentauth.RegistrationAttemptStatusUnauthorized, authError)
		response.RespondWithError(w, http.StatusUnauthorized, authError)
		return
	}

	if parseErr != nil {
		_ = h.agentAuth.RecordRegistrationAttempt(r.Context(), nil, meta, agentauth.RegistrationAttemptStatusInvalidRequest, "Неверный формат тела запроса")
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}
	if strings.TrimSpace(dto.AgentUUID) == "" {
		_ = h.agentAuth.RecordRegistrationAttempt(r.Context(), &dto, meta, agentauth.RegistrationAttemptStatusInvalidRequest, "Поле agent_uuid обязательно")
		response.RespondWithError(w, http.StatusBadRequest, "Поле agent_uuid обязательно")
		return
	}
	if dto.InitialData.AgentType == "" {
		dto.InitialData.AgentType = "sssruner"
	}

	respDTO, err := h.agentAuth.RegisterAndIssueTokens(r.Context(), &dto, meta)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			response.RespondWithError(w, http.StatusConflict, "Агент с таким UUID уже зарегистрирован")
			return
		}
		log.Error("register failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}

	log.Info("Регистрация агента выполнена", "uuid", dto.AgentUUID, "agent_type", dto.InitialData.AgentType)
	response.RespondWithRawJSON(w, http.StatusOK, respDTO)
}

func (h *AgentHandler) refreshAgentToken(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	var dto api.AgentTokenRefreshRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

	respDTO, err := h.agentAuth.RefreshTokens(r.Context(), &dto)
	if err != nil {
		switch {
		case errors.Is(err, agentauth.ErrInvalidToken):
			response.RespondWithError(w, http.StatusUnauthorized, "Неверный refresh token")
		case errors.Is(err, agentauth.ErrTokenExpired):
			response.RespondWithError(w, http.StatusUnauthorized, "Refresh token просрочен")
		default:
			log.Error("refresh token failed", "error", err)
			response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		}
		return
	}

	response.RespondWithRawJSON(w, http.StatusOK, respDTO)
}

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

	response.RespondWithRawJSON(w, http.StatusOK, config)
}

// postAgentData — heartbeat и данные нового активного агента.
// Для этого маршрута требуются access token агента.
func (h *AgentHandler) postAgentData(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	uuid := chi.URLParam(r, "uuid")
	if !h.authorizeAgentAccessToken(w, r, uuid) {
		return
	}

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат JSON")
		return
	}

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

	response.RespondWithRawJSON(w, http.StatusOK, respData)
}

// handleAgentReport принимает данные через /report?key=TOKEN.
// Поддерживает простые пассивные агенты и совместимый JSON-репортинг.
func (h *AgentHandler) handleAgentReport(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	requestKey := r.URL.Query().Get("key")
	if h.apiKey != "" && requestKey != h.apiKey {
		log.Warn("Неверный API ключ в запросе /report", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusUnauthorized, "Invalid API Key")
		return
	}

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Warn("Ошибка декодирования JSON в /report", "error", err)
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if dto.AgentUUID == "" {
		if val, ok := dto.AdditionalProperties["uuid"]; ok {
			if strVal, ok := val.(string); ok {
				dto.AgentUUID = strVal
			}
		}
	}

	if dto.AgentUUID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Field 'uuid' or 'agent_uuid' is required")
		return
	}

	if dto.AgentType == "" {
		dto.AgentType = "getad"
	}

	respData, err := h.agentService.ProcessData(r.Context(), dto.AgentUUID, &dto)
	if err != nil {
		log.Error("handleAgentReport failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal processing error")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, respData)
}

func (h *AgentHandler) HandleSubmitJSON(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	clientKey := r.Header.Get("X-API-Key")
	if h.apiKey != "" && clientKey != h.apiKey {
		log.Warn("Неверный X-API-Key в запросе /submit_json", "remote_addr", r.RemoteAddr)
		response.RespondWithError(w, http.StatusUnauthorized, "Invalid API Key")
		return
	}

	var dto api.AgentDataDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		log.Warn("Ошибка декодирования JSON в /submit_json", "error", err)
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if dto.AgentUUID == "" {
		if val, ok := dto.AdditionalProperties["uuid"]; ok {
			if strVal, ok := val.(string); ok {
				dto.AgentUUID = strVal
			}
		}
	}

	if dto.AgentUUID == "" {
		log.Warn("В запросе submit_json не найден uuid")
		response.RespondWithError(w, http.StatusBadRequest, "Field 'uuid' is required")
		return
	}

	dto.AgentType = "getad"

	respData, err := h.agentService.ProcessData(r.Context(), dto.AgentUUID, &dto)
	if err != nil {
		log.Error("handleSubmitJSON process failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal processing error")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, respData)
}

func (h *AgentHandler) validateBootstrapKey(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return "Отсутствует заголовок Authorization"
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "Неверный формат заголовка Authorization"
	}
	if h.apiKey == "" || parts[1] != h.apiKey {
		return "Неверный bootstrap API key агента"
	}
	return ""
}

func (h *AgentHandler) authorizeAgentAccessToken(w http.ResponseWriter, r *http.Request, agentUUID string) bool {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		response.RespondWithError(w, http.StatusUnauthorized, "Отсутствует access token агента")
		return false
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		response.RespondWithError(w, http.StatusUnauthorized, "Неверный формат заголовка Authorization")
		return false
	}
	if h.agentAuth == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Сервис авторизации агента не настроен")
		return false
	}
	if err := h.agentAuth.ValidateAccessToken(r.Context(), agentUUID, parts[1]); err != nil {
		if errors.Is(err, agentauth.ErrTokenExpired) {
			response.RespondWithError(w, http.StatusUnauthorized, "Access token агента просрочен")
			return false
		}
		response.RespondWithError(w, http.StatusUnauthorized, "Неверный access token агента")
		return false
	}
	return true
}

func registrationRequestOrNil(parseErr error, dto *api.RegistrationRequestDTO) *api.RegistrationRequestDTO {
	if parseErr != nil || dto == nil {
		return nil
	}
	return dto
}
