package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AgentDiagnosticsHandler struct {
	db           *gorm.DB
	operatorFlow services.AgentOperatorFlowService
}

func NewAgentDiagnosticsHandler(db *gorm.DB, operatorFlow services.AgentOperatorFlowService) *AgentDiagnosticsHandler {
	return &AgentDiagnosticsHandler{
		db:           db,
		operatorFlow: operatorFlow,
	}
}

func (h *AgentDiagnosticsHandler) RegisterRoutes(r chi.Router) {
	r.Get("/agent-diagnostics", h.ListAgents)
	r.Get("/agent-diagnostics/{uuid}", h.GetAgent)
	r.Post("/agent-diagnostics/{uuid}/approve-registration", h.ApproveRegistration)
	r.Post("/agent-diagnostics/{uuid}/adapter-selection", h.SaveAdapterSelection)
	r.Post("/agent-diagnostics/{uuid}/signature-rules", h.UpsertCOMSignatureRule)
}

func (h *AgentDiagnosticsHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	term := strings.TrimSpace(r.URL.Query().Get("term"))
	registrationStatus := strings.TrimSpace(r.URL.Query().Get("registration_status"))

	query := h.db.WithContext(r.Context()).Model(&models.Agent{})
	if term != "" {
		likeTerm := "%" + term + "%"
		query = query.Where(
			"LOWER(uuid) LIKE LOWER(?) OR LOWER(hostname) LIKE LOWER(?) OR LOWER(machine_fingerprint) LIKE LOWER(?) OR LOWER(owner_id) LIKE LOWER(?)",
			likeTerm,
			likeTerm,
			likeTerm,
			likeTerm,
		)
	}
	if registrationStatus != "" {
		query = query.Where("last_registration_status = ?", registrationStatus)
	}

	var items []models.Agent
	if err := query.
		Order("CASE WHEN last_registration_at IS NULL THEN 1 ELSE 0 END, last_registration_at DESC, last_heartbeat DESC, updated_at DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить список агентов для диагностики")
		return
	}

	out := make([]api.AgentDiagnosticsListItemDTO, 0, len(items))
	for i := range items {
		out = append(out, buildAgentDiagnosticsListItem(items[i]))
	}

	response.RespondWithJSON(w, http.StatusOK, out)
}

func (h *AgentDiagnosticsHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	agentUUID := strings.TrimSpace(chi.URLParam(r, "uuid"))
	if agentUUID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "UUID агента обязателен")
		return
	}

	out, err := h.buildAgentDiagnosticsDetails(r.Context(), agentUUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить данные агента")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, out)
}

func (h *AgentDiagnosticsHandler) ApproveRegistration(w http.ResponseWriter, r *http.Request) {
	agentUUID := strings.TrimSpace(chi.URLParam(r, "uuid"))
	if agentUUID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "UUID агента обязателен")
		return
	}

	var agent models.Agent
	if err := h.db.WithContext(r.Context()).Where("uuid = ?", agentUUID).First(&agent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить данные агента")
		return
	}

	if agent.RegistrationApprovedAt == nil {
		requiresApproval := agent.Status == models.StatusPendingRegistration || agent.LastRegistrationStatus == models.AgentRegistrationStatusPendingApproval
		if !requiresApproval {
			response.RespondWithError(w, http.StatusConflict, "Подтверждение регистрации для этого агента не требуется")
			return
		}

		now := time.Now().UTC()
		agent.RegistrationApprovedAt = &now
		if userID, ok := r.Context().Value(contextkeys.UserIDContextKey).(string); ok {
			agent.RegistrationApprovedBy = strings.TrimSpace(userID)
		}
		if agent.Status == models.StatusRegistrationFailed || strings.TrimSpace(agent.Status) == "" {
			agent.Status = models.StatusPendingRegistration
		}
		if agent.LastRegistrationStatus == models.AgentRegistrationStatusPendingApproval {
			agent.LastRegistrationError = "Регистрация подтверждена оператором. Ожидается повторный запрос агента для выдачи токенов."
		}
		if err := h.db.WithContext(r.Context()).Save(&agent).Error; err != nil {
			response.RespondWithError(w, http.StatusInternalServerError, "Не удалось подтвердить регистрацию агента")
			return
		}
	}

	out, err := h.buildAgentDiagnosticsDetails(r.Context(), agentUUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить обновленные данные агента")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}

func (h *AgentDiagnosticsHandler) buildAgentDiagnosticsDetails(ctx context.Context, agentUUID string) (api.AgentDiagnosticsDetailsDTO, error) {
	var agent models.Agent
	if err := h.db.WithContext(ctx).Where("uuid = ?", agentUUID).First(&agent).Error; err != nil {
		return api.AgentDiagnosticsDetailsDTO{}, err
	}

	var attempts []models.AgentRegistrationAttempt
	if err := h.db.WithContext(ctx).
		Where("agent_uuid = ?", agentUUID).
		Order("created_at DESC, id DESC").
		Limit(20).
		Find(&attempts).Error; err != nil {
		return api.AgentDiagnosticsDetailsDTO{}, err
	}

	out := api.AgentDiagnosticsDetailsDTO{
		Agent:                  buildAgentDiagnosticsListItem(agent),
		RegistrationPayload:    decodeJSONValue(agent.RegistrationPayload),
		RegistrationSystemInfo: decodeJSONValue(agent.RegistrationSystemInfo),
		LatestInventory:        decodeJSONValue(agent.LatestInventorySnapshot),
		LatestAdapterStatuses:  decodeJSONValue(agent.LatestAdapterStatuses),
		RecentRegistrations:    make([]api.AgentRegistrationAttemptDTO, 0, len(attempts)),
	}

	for i := range attempts {
		out.RecentRegistrations = append(out.RecentRegistrations, api.AgentRegistrationAttemptDTO{
			ID:                 attempts[i].ID,
			AgentUUID:          attempts[i].AgentUUID,
			Status:             attempts[i].Status,
			ErrorText:          attempts[i].ErrorText,
			MachineFingerprint: attempts[i].MachineFingerprint,
			SystemInfo:         decodeJSONValue(attempts[i].SystemInfo),
			Payload:            decodeJSONValue(attempts[i].Payload),
			RemoteAddr:         attempts[i].RemoteAddr,
			CreatedAt:          attempts[i].CreatedAt,
		})
	}

	if h.operatorFlow != nil {
		operatorFlow, err := h.operatorFlow.BuildOperatorFlow(ctx, &agent)
		if err != nil {
			return api.AgentDiagnosticsDetailsDTO{}, err
		}
		out.OperatorFlow = operatorFlow
	}
	return out, nil
}

func (h *AgentDiagnosticsHandler) SaveAdapterSelection(w http.ResponseWriter, r *http.Request) {
	agentUUID := strings.TrimSpace(chi.URLParam(r, "uuid"))
	if agentUUID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "UUID агента обязателен")
		return
	}
	if h.operatorFlow == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Сервис operator flow не настроен")
		return
	}

	var dto api.SaveAgentAdapterSelectionRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	actor := strings.TrimSpace(userIDFromContext(r.Context()))
	if err := h.operatorFlow.SaveAdapterSelection(r.Context(), agentUUID, dto, actor); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	out, err := h.buildAgentDiagnosticsDetails(r.Context(), agentUUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить обновлённый список адаптеров агента")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}

func (h *AgentDiagnosticsHandler) UpsertCOMSignatureRule(w http.ResponseWriter, r *http.Request) {
	agentUUID := strings.TrimSpace(chi.URLParam(r, "uuid"))
	if agentUUID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "UUID агента обязателен")
		return
	}
	if h.operatorFlow == nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Сервис operator flow не настроен")
		return
	}

	var dto api.UpsertAgentCOMSignatureRuleRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Неверный формат тела запроса")
		return
	}

	if err := h.db.WithContext(r.Context()).Where("uuid = ?", agentUUID).First(&models.Agent{}).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить данные агента")
		return
	}

	actor := strings.TrimSpace(userIDFromContext(r.Context()))
	if err := h.operatorFlow.SaveCOMSignatureRule(r.Context(), dto, actor); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	out, err := h.buildAgentDiagnosticsDetails(r.Context(), agentUUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Агент не найден")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить обновлённую диагностику агента")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}

func buildAgentDiagnosticsListItem(agent models.Agent) api.AgentDiagnosticsListItemDTO {
	return api.AgentDiagnosticsListItemDTO{
		UUID:                   agent.UUID,
		Hostname:               agent.Hostname,
		Type:                   agent.Type,
		Status:                 agent.Status,
		OwnerID:                agent.OwnerID,
		WorkstationID:          agent.WorkstationID,
		LastObservedAt:         agent.LastObservedAt,
		LastHeartbeat:          agent.LastHeartbeat,
		LastRegistrationAt:     agent.LastRegistrationAt,
		LastRegistrationStatus: agent.LastRegistrationStatus,
		LastRegistrationError:  agent.LastRegistrationError,
		RegistrationApprovedAt: agent.RegistrationApprovedAt,
		RegistrationApprovedBy: agent.RegistrationApprovedBy,
		MachineFingerprint:     agent.MachineFingerprint,
		HasLatestInventory:     hasJSONPayload(agent.LatestInventorySnapshot),
		HasAdapterStatuses:     hasJSONPayload(agent.LatestAdapterStatuses),
	}
}

func hasJSONPayload(raw datatypes.JSON) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func decodeJSONValue(raw datatypes.JSON) any {
	if len(raw) == 0 {
		return nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{
			"raw": string(raw),
		}
	}
	return value
}

func userIDFromContext(ctx context.Context) string {
	if userID, ok := ctx.Value(contextkeys.UserIDContextKey).(string); ok {
		return strings.TrimSpace(userID)
	}
	return ""
}
