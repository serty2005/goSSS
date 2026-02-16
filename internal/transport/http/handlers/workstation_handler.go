package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/workstation"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type WSHandler struct {
	service workstation.Service
}

func NewWSHandler(service workstation.Service) *WSHandler {
	return &WSHandler{service: service}
}

func (h *WSHandler) RegisterRoutes(r chi.Router) {
	r.Route("/workstations", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/", h.Create)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

func (h *WSHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	term := r.URL.Query().Get("term")
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var (
		items []workstation.Workstation
		total int64
		err   error
	)
	if term != "" {
		items, total, err = h.service.Search(r.Context(), term, limit, offset)
	} else {
		items, total, err = h.service.List(r.Context(), limit, offset)
	}
	if err != nil {
		middleware.GetLogger(r.Context()).Error("list failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	dtos := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toWorkstationResponse(item))
	}
	hasPrev := offset > 0
	hasNext := int64(offset+len(items)) < total
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    dtos,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: hasNext,
		HasPrev: hasPrev,
	})
}

func (h *WSHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	item, err := h.service.Get(r.Context(), id)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	if item == nil {
		response.RespondWithError(w, http.StatusNotFound, "Not Found")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, toWorkstationResponse(*item))
}

func (h *WSHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.WorkstationCreateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid Body")
		return
	}
	item, err := h.service.Create(r.Context(), &dto)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("create failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Creation Failed")
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, toWorkstationResponse(*item))
}

func (h *WSHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}
	err := h.service.Update(r.Context(), id, data)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("update failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Update Failed")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *WSHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.Delete(r.Context(), id); err != nil {
		middleware.GetLogger(r.Context()).Error("delete failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Delete Failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toWorkstationResponse(item workstation.Workstation) map[string]interface{} {
	var statusDetails interface{}
	if len(item.StatusDetails) > 0 {
		_ = json.Unmarshal(item.StatusDetails, &statusDetails)
	}
	return map[string]interface{}{
		"id":                 item.ID,
		"created_at":         item.CreatedAt,
		"updated_at":         item.UpdatedAt,
		"last_updated_by":    item.LastUpdatedBy,
		"deleted_at":         item.DeletedAt,
		"identity_hash":      item.IdentityHash,
		"teamviewer":         item.Teamviewer,
		"anydesk":            item.Anydesk,
		"litemanager":        item.Litemanager,
		"device_name":        item.DeviceName,
		"server_id":          item.ServerID,
		"is_new":             item.IsNew,
		"last_modified_date": item.LastModifiedDate,
		"description":        item.Description,
		"health_status":      item.HealthStatus,
		"status_details":     statusDetails,
		"owner_id":           item.OwnerID,
		"owner_binding_mode": item.OwnerBindingMode,
	}
}
