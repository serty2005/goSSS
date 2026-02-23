package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type EntityDeletionHandler struct {
	service services.EntityDeletionService
}

func NewEntityDeletionHandler(service services.EntityDeletionService) *EntityDeletionHandler {
	return &EntityDeletionHandler{service: service}
}

func (h *EntityDeletionHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.RequestDeletion)
	r.Get("/by-entity", h.GetByEntity)
	r.Get("/{id}", h.GetDetails)
	r.Post("/{id}/replay", h.ReplayChoice)
	r.Post("/{id}/confirm", h.ConfirmDeletion)
}

func (h *EntityDeletionHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))

	items, total, err := h.service.ListCandidates(r.Context(), services.EntityDeletionCandidateListFilter{
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить кандидатов на удаление", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения кандидатов на удаление")
		return
	}

	rows := make([]api.EntityDeletionCandidateDTO, 0, len(items))
	for i := range items {
		rows = append(rows, mapEntityDeletionCandidateDTO(items[i]))
	}
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    rows,
		Total:   total,
		Limit:   maxInt(limit, 50),
		Offset:  maxInt(offset, 0),
		HasNext: int64(maxInt(offset, 0)+len(rows)) < total,
		HasPrev: maxInt(offset, 0) > 0,
	})
}

func (h *EntityDeletionHandler) GetByEntity(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")
	item, err := h.service.GetActiveCandidateByEntity(r.Context(), entityType, entityID)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить кандидата на удаление по сущности", "entity_type", entityType, "entity_id", entityID, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения кандидата на удаление")
		return
	}
	if item == nil {
		response.RespondWithJSON(w, http.StatusOK, nil)
		return
	}
	response.RespondWithJSON(w, http.StatusOK, mapEntityDeletionCandidateDTO(*item))
}

func (h *EntityDeletionHandler) RequestDeletion(w http.ResponseWriter, r *http.Request) {
	var req api.EntityDeletionRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	item, err := h.service.RequestDeletion(r.Context(), services.EntityDeletionRequest{
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		Reason:     req.Reason,
		Comment:    req.Comment,
		Source:     models.EntityDeletionSourceManual,
	})
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, services.ErrDeletionEntityNotFound):
			status = http.StatusNotFound
		case errors.Is(err, services.ErrDeletionEntityTypeUnsupported):
			status = http.StatusBadRequest
		}
		middleware.GetLogger(r.Context()).Error("не удалось поставить сущность в кандидаты на удаление", "error", err)
		response.RespondWithError(w, status, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, mapEntityDeletionCandidateDTO(*item))
}

func (h *EntityDeletionHandler) ConfirmDeletion(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID кандидата")
		return
	}
	item, err := h.service.ConfirmDeletion(r.Context(), uint(id))
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, services.ErrDeletionCandidateNotFound):
			status = http.StatusNotFound
		case errors.Is(err, services.ErrDeletionCandidateInvalidState):
			status = http.StatusConflict
		case errors.Is(err, services.ErrDeletionSelfConfirmationForbidden):
			status = http.StatusForbidden
		case errors.Is(err, services.ErrDeletionEntityNotFound):
			status = http.StatusNotFound
		}
		middleware.GetLogger(r.Context()).Error("не удалось подтвердить удаление", "candidate_id", id, "error", err)
		response.RespondWithError(w, status, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, mapEntityDeletionCandidateDTO(*item))
}

func (h *EntityDeletionHandler) GetDetails(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID кандидата")
		return
	}
	details, err := h.service.GetCandidateDetails(r.Context(), uint(id))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, services.ErrDeletionCandidateNotFound) {
			status = http.StatusNotFound
		}
		middleware.GetLogger(r.Context()).Error("не удалось получить детали кандидата на удаление", "candidate_id", id, "error", err)
		response.RespondWithError(w, status, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, mapEntityDeletionCandidateDetailsDTO(details))
}

func (h *EntityDeletionHandler) ReplayChoice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный ID кандидата")
		return
	}
	var req api.EntityDeletionReplayRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	item, err := h.service.ReplayDuplicateChoice(r.Context(), uint(id), req.KeepEntityID, req.DeleteEntityID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, services.ErrDeletionCandidateNotFound):
			status = http.StatusNotFound
		case errors.Is(err, services.ErrDeletionCandidateInvalidState):
			status = http.StatusConflict
		case errors.Is(err, services.ErrDeletionEntityNotFound):
			status = http.StatusBadRequest
		}
		middleware.GetLogger(r.Context()).Error("не удалось переиграть выбор дубля", "candidate_id", id, "error", err)
		response.RespondWithError(w, status, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, mapEntityDeletionCandidateDTO(*item))
}

func mapEntityDeletionCandidateDTO(item models.EntityDeletionCandidate) api.EntityDeletionCandidateDTO {
	meta := map[string]interface{}{}
	if len(item.Meta) > 0 {
		_ = json.Unmarshal(item.Meta, &meta)
	}
	return api.EntityDeletionCandidateDTO{
		ID:                  item.ID,
		EntityType:          item.EntityType,
		EntityID:            item.EntityID,
		EntityDisplayName:   item.EntityDisplayName,
		Status:              item.Status,
		Reason:              item.Reason,
		Source:              item.Source,
		Comment:             item.Comment,
		RequestedByUserID:   item.RequestedByUserID,
		RequestedAt:         item.RequestedAt,
		ConfirmedByUserID:   item.ConfirmedByUserID,
		ConfirmedAt:         item.ConfirmedAt,
		DuplicateOfEntityID: item.DuplicateOfEntityID,
		DuplicateField:      item.DuplicateField,
		DuplicateValue:      item.DuplicateValue,
		Meta:                meta,
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
	}
}

func mapEntityDeletionCandidateDetailsDTO(details *services.EntityDeletionCandidateDetails) api.EntityDeletionCandidateDetailsDTO {
	if details == nil || details.Candidate == nil {
		return api.EntityDeletionCandidateDetailsDTO{}
	}
	out := api.EntityDeletionCandidateDetailsDTO{
		Candidate:          mapEntityDeletionCandidateDTO(*details.Candidate),
		ReasonText:         details.ReasonText,
		MoreActualEntityID: details.MoreActualEntityID,
		Entities:           make([]api.EntityDeletionCandidateEntityDetailsDTO, 0, len(details.Entities)),
		CascadeEntities:    make([]api.EntityDeletionCandidateEntityDetailsDTO, 0, len(details.CascadeEntities)),
	}
	for _, item := range details.Entities {
		if item == nil || item.Snapshot == nil {
			continue
		}
		mapped := api.EntityDeletionCandidateEntityDetailsDTO{
			EntityType:       item.Snapshot.EntityType,
			EntityID:         item.Snapshot.EntityID,
			DisplayName:      item.Snapshot.DisplayName,
			OwnerID:          item.Snapshot.OwnerID,
			LastUpdatedBy:    item.Snapshot.LastUpdatedBy,
			UpdatedAt:        item.Snapshot.UpdatedAt,
			LastModifiedDate: item.Snapshot.LastModifiedAt,
			Deleted:          item.Snapshot.Deleted,
			IsMoreActual:     item.IsMoreActual,
			Raw:              item.Raw,
		}
		if item.LatestAgentData != nil {
			mapped.LatestAgentData = &api.EntityDeletionCandidateAgentDataDTO{
				ObservationID: item.LatestAgentData.ObservationID,
				ObservedAt:    item.LatestAgentData.ObservedAt,
				PayloadJSON:   item.LatestAgentData.PayloadJSON,
			}
		}
		out.Entities = append(out.Entities, mapped)
		if details.KeepEntity != nil && details.KeepEntity.Snapshot != nil && details.KeepEntity.Snapshot.EntityID == mapped.EntityID {
			copyMapped := mapped
			out.KeepEntity = &copyMapped
		}
		if details.DeleteEntity != nil && details.DeleteEntity.Snapshot != nil && details.DeleteEntity.Snapshot.EntityID == mapped.EntityID {
			copyMapped := mapped
			out.DeleteEntity = &copyMapped
		}
	}
	for _, item := range details.CascadeEntities {
		if item == nil || item.Snapshot == nil {
			continue
		}
		out.CascadeEntities = append(out.CascadeEntities, api.EntityDeletionCandidateEntityDetailsDTO{
			EntityType:       item.Snapshot.EntityType,
			EntityID:         item.Snapshot.EntityID,
			DisplayName:      item.Snapshot.DisplayName,
			OwnerID:          item.Snapshot.OwnerID,
			LastUpdatedBy:    item.Snapshot.LastUpdatedBy,
			UpdatedAt:        item.Snapshot.UpdatedAt,
			LastModifiedDate: item.Snapshot.LastModifiedAt,
			Deleted:          item.Snapshot.Deleted,
			IsMoreActual:     item.IsMoreActual,
			Raw:              item.Raw,
		})
	}
	return out
}

func maxInt(v int, fallback int) int {
	if v < 0 {
		return fallback
	}
	if v == 0 {
		return fallback
	}
	return v
}
