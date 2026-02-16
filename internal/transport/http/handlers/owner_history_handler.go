package handlers

import (
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type OwnerHistoryHandler struct {
	repo domainrepos.OwnerHistoryRepo
}

func NewOwnerHistoryHandler(repo domainrepos.OwnerHistoryRepo) *OwnerHistoryHandler {
	return &OwnerHistoryHandler{repo: repo}
}

func (h *OwnerHistoryHandler) RegisterRoutes(r chi.Router) {
	r.Get("/owner-history", h.ListByEntity)
}

func (h *OwnerHistoryHandler) ListByEntity(w http.ResponseWriter, r *http.Request) {
	entityType := strings.TrimSpace(r.URL.Query().Get("entity_type"))
	entityID := strings.TrimSpace(r.URL.Query().Get("entity_id"))
	if entityType == "" || entityID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "entity_type и entity_id обязательны")
		return
	}
	limit := 100
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}

	records, err := h.repo.ListByEntity(r.Context(), entityType, entityID, limit)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Не удалось получить историю")
		return
	}

	out := make([]map[string]interface{}, 0, len(records))
	for i := range records {
		item := records[i]
		actorType := "system"
		if item.ChangedByUserID != nil && strings.TrimSpace(*item.ChangedByUserID) != "" {
			actorType = "user"
		} else if item.AgentUUID != nil && strings.TrimSpace(*item.AgentUUID) != "" {
			actorType = "agent"
		}
		out = append(out, map[string]interface{}{
			"id":                 item.ID,
			"entity_type":        item.EntityType,
			"entity_id":          item.EntityID,
			"from_owner_id":      item.FromOwnerID,
			"to_owner_id":        item.ToOwnerID,
			"change_source":      item.ChangeSource,
			"comment":            item.Comment,
			"changed_by_user_id": item.ChangedByUserID,
			"agent_uuid":         item.AgentUUID,
			"observation_id":     item.ObservationID,
			"actor_type":         actorType,
			"created_at":         item.CreatedAt,
			"is_agent_update":    item.ChangeSource == models.OwnerChangeSourceAgentDataUpdate,
		})
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}
