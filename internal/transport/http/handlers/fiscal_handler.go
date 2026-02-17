package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/fiscal"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type FiscalHandler struct {
	service fiscal.Service
}

func NewFiscalHandler(service fiscal.Service) *FiscalHandler {
	return &FiscalHandler{service: service}
}

func (h *FiscalHandler) RegisterRoutes(r chi.Router) {
	r.Route("/fiscals", func(r chi.Router) {
		r.Get("/", h.List)
		r.Get("/{id}", h.Get)
		r.Post("/", h.Create)
		r.Put("/{id}", h.Update)
		r.Delete("/{id}", h.Delete)
	})
}

func (h *FiscalHandler) List(w http.ResponseWriter, r *http.Request) {
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
		items []fiscal.FiscalRegister
		total int64
		err   error
	)
	if term != "" {
		items, total, err = h.service.Search(r.Context(), term, limit, offset)
	} else {
		items, total, err = h.service.List(r.Context(), limit, offset)
	}
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			middleware.GetLogger(r.Context()).Error("не найдена запись", "error", err)
			response.RespondWithError(w, http.StatusNotFound, "Not Found")
			return
		}
		middleware.GetLogger(r.Context()).Error("list failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Internal Error")
		return
	}
	dtos := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toFiscalResponse(item))
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

func (h *FiscalHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	response.RespondWithJSON(w, http.StatusOK, toFiscalResponse(*item))
}

func (h *FiscalHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dto api.FiscalRegisterCreateDTO
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
	response.RespondWithJSON(w, http.StatusCreated, toFiscalResponse(*item))
}

func (h *FiscalHandler) Update(w http.ResponseWriter, r *http.Request) {
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

func (h *FiscalHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.Delete(r.Context(), id); err != nil {
		middleware.GetLogger(r.Context()).Error("delete failed", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Delete Failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toFiscalResponse(item fiscal.FiscalRegister) map[string]interface{} {
	var statusDetails interface{}
	if len(item.StatusDetails) > 0 {
		_ = json.Unmarshal(item.StatusDetails, &statusDetails)
	}

	var licenses interface{}
	if len(item.Licenses) > 0 {
		_ = json.Unmarshal(item.Licenses, &licenses)
		licenses = normalizeLicensesForAPI(licenses)
	}

	return map[string]interface{}{
		"id":                        item.ID,
		"created_at":                item.CreatedAt,
		"updated_at":                item.UpdatedAt,
		"last_updated_by":           item.LastUpdatedBy,
		"deleted_at":                item.DeletedAt,
		"model_kkt":                 item.ModelKKT,
		"ffd":                       item.FFD,
		"rn_kkt":                    item.RNKKT,
		"legal_name":                item.LegalName,
		"inn":                       item.INN,
		"fr_serial_number":          item.FRSerialNumber,
		"fr_serial_normalized":      item.FRSerialNormalized,
		"fn_number":                 item.FNNumber,
		"fn_execution":              item.FNExecution,
		"kkt_reg_date":              item.KKTRegDate,
		"fn_expire_date":            item.FNExpireDate,
		"last_modified_date":        item.LastModifiedDate,
		"fr_downloader":             item.FRDownloader,
		"fr_firmware":               item.FRFirmware,
		"driver_version":            item.DriverVersion,
		"health_status":             item.HealthStatus,
		"status_details":            statusDetails,
		"owner_id":                  item.OwnerID,
		"licenses":                  licenses,
		"address":                   item.Address,
		"attribute_excise":          item.AttributeExcise,
		"attribute_marked":          item.AttributeMarked,
		"ofd_name":                  item.OFDName,
		"workstation_id":            item.WorkstationID,
		"health_status_before_lock": item.HealthStatusBeforeLock,
		"owner_binding_mode":        item.OwnerBindingMode,
	}
}

func normalizeLicensesForAPI(raw interface{}) interface{} {
	asMap, ok := raw.(map[string]interface{})
	if !ok {
		return raw
	}
	out := make(map[string]interface{}, len(asMap))
	for key, value := range asMap {
		item, ok := value.(map[string]interface{})
		if !ok {
			out[key] = value
			continue
		}
		if dateFrom, exists := item["dateFrom"]; exists {
			item["date_from"] = dateFrom
			delete(item, "dateFrom")
		}
		if dateUntil, exists := item["dateUntil"]; exists {
			item["date_until"] = dateUntil
			delete(item, "dateUntil")
		}
		out[key] = item
	}
	return out
}
