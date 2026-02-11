package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type TicketHandler struct {
	service       services.TicketService
	bitrixService services.BitrixSyncService
}

func NewTicketHandler(service services.TicketService, bitrixService services.BitrixSyncService) *TicketHandler {
	return &TicketHandler{service: service, bitrixService: bitrixService}
}

func (h *TicketHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/filters", h.Filters)
	r.Get("/stats/dashboard", h.DashboardStats)
	r.Post("/", h.Create) // Р РЋР С•Р В·Р Т‘Р В°Р Р…Р С‘Р Вµ Р Р†Р Р…РЎС“РЎвЂљРЎР‚Р ВµР Р…Р Р…Р ВµР С–Р С• РЎвЂљР С‘Р С”Р ВµРЎвЂљР В°
	r.Get("/{id}", h.GetDetails)
	r.Post("/{id}/link", h.LinkAsset)
	r.Post("/{id}/attachments", h.UploadAttachments)
	r.Post("/{id}/comments", h.AddComment)
	r.Post("/{id}/refresh-comments", h.RefreshCommentsFromServiceDesk)
	r.Post("/{id}/connection-copy", h.RecordConnectionCopy)
	r.Patch("/{id}/status", h.ChangeStatus)
	r.Patch("/{id}/description", h.UpdateDescription)
	r.Patch("/{id}/assign", h.Assign)
	r.Patch("/{id}/company", h.ChangeCompany)
	r.Patch("/{id}/bitrix", h.UpdateBitrixFields)
}
func (h *TicketHandler) DashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.service.GetDashboardStats(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "РћС€РёР±РєР° РїРѕР»СѓС‡РµРЅРёСЏ СЃС‚Р°С‚РёСЃС‚РёРєРё")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, stats)
}

// Create РЎРѓР С•Р В·Р Т‘Р В°Р ВµРЎвЂљ Р Р…Р С•Р Р†РЎвЂ№Р в„– РЎвЂљР С‘Р С”Р ВµРЎвЂљ (Р Р†Р Р…РЎС“РЎвЂљРЎР‚Р ВµР Р…Р Р…Р С‘Р в„–).
func (h *TicketHandler) Create(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	var dto api.TicketCreateInternalDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	// Р СџР С•Р В»РЎС“РЎвЂЎР В°Р ВµР С ID РЎвЂљР ВµР С”РЎС“РЎвЂ°Р ВµР С–Р С• Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ
	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р… Р Р† Р С”Р С•Р Р…РЎвЂљР ВµР С”РЎРѓРЎвЂљР Вµ")
		return
	}

	ticket, err := h.service.CreateInternal(r.Context(), dto, userID)
	if err != nil {
		if errors.Is(err, services.ErrReporterNotFound) {
			response.RespondWithError(w, http.StatusUnauthorized, "Р СџР С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЉ Р С‘Р В· РЎРѓР ВµРЎРѓРЎРѓР С‘Р С‘ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р…, Р Р†РЎвЂ№Р С—Р С•Р В»Р Р…Р С‘РЎвЂљР Вµ Р Р†РЎвЂ¦Р С•Р Т‘ Р В·Р В°Р Р…Р С•Р Р†Р С•")
			return
		}
		log.Error("Failed to create ticket", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Failed to create ticket")
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncTicketByID(r.Context(), ticket.ID); err != nil {
			log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ С‚РёРєРµС‚ СЃ Bitrix24", "ticket_id", ticket.ID, "error", err)
		}
	}
	response.RespondWithJSON(w, http.StatusCreated, ticket)
}

func (h *TicketHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var dto api.TicketStatusChangeDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	userID := getUserIDFromContext(r)
	ticket, err := h.service.ChangeStatus(r.Context(), id, dto.Status, dto.Comment, userID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncTicketByID(r.Context(), ticket.ID); err != nil {
			log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ СЃС‚Р°С‚СѓСЃ С‚РёРєРµС‚Р° СЃ Bitrix24", "ticket_id", ticket.ID, "error", err)
		}
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

func (h *TicketHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var dto api.TicketAddCommentDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р… Р Р† Р С”Р С•Р Р…РЎвЂљР ВµР С”РЎРѓРЎвЂљР Вµ")
		return
	}
	if strings.TrimSpace(dto.Comment) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Р С™Р С•Р СР СР ВµР Р…РЎвЂљР В°РЎР‚Р С‘Р в„– Р С•Р В±РЎРЏР В·Р В°РЎвЂљР ВµР В»Р ВµР Р…")
		return
	}

	comment, err := h.service.AddComment(r.Context(), id, dto.Comment, dto.IsPrivate, userID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncComment(r.Context(), id, comment, userID); err != nil {
			log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ РєРѕРјРјРµРЅС‚Р°СЂРёР№ СЃ Bitrix24", "ticket_id", id, "comment_id", comment.ID, "error", err)
		}
	}
	response.RespondWithJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (h *TicketHandler) UploadAttachments(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.RespondWithError(w, http.StatusBadRequest, "ID Р·Р°СЏРІРєРё РѕР±СЏР·Р°С‚РµР»РµРЅ")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "РќРµРєРѕСЂСЂРµРєС‚РЅС‹Р№ multipart Р·Р°РїСЂРѕСЃ")
		return
	}

	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		files = append(files, r.MultipartForm.File["files"]...)
		files = append(files, r.MultipartForm.File["file"]...)
	}
	if len(files) == 0 {
		response.RespondWithError(w, http.StatusBadRequest, "РќРµ РІС‹Р±СЂР°РЅС‹ С„Р°Р№Р»С‹")
		return
	}

	items, err := h.service.UploadAttachments(r.Context(), id, files)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusCreated, map[string]interface{}{"items": items})
}

func (h *TicketHandler) RefreshCommentsFromServiceDesk(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	added, err := h.service.RefreshCommentsFromServiceDesk(r.Context(), id)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"added":  added,
	})
}

func (h *TicketHandler) RecordConnectionCopy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var dto struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р… Р Р† Р С”Р С•Р Р…РЎвЂљР ВµР С”РЎРѓРЎвЂљР Вµ")
		return
	}
	if strings.TrimSpace(dto.Value) == "" {
		response.RespondWithError(w, http.StatusBadRequest, "Р вЂ”Р Р…Р В°РЎвЂЎР ВµР Р…Р С‘Р Вµ Р С•Р В±РЎРЏР В·Р В°РЎвЂљР ВµР В»РЎРЉР Р…Р С•")
		return
	}

	if err := h.service.RecordConnectionCopy(r.Context(), id, dto.Label, dto.Value, userID); err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

// UpdateDescription Р С•Р В±Р Р…Р С•Р Р†Р В»РЎРЏР ВµРЎвЂљ Р С•Р С—Р С‘РЎРѓР В°Р Р…Р С‘Р Вµ РЎвЂљР С‘Р С”Р ВµРЎвЂљР В°.
func (h *TicketHandler) UpdateDescription(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var dto struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р… Р Р† Р С”Р С•Р Р…РЎвЂљР ВµР С”РЎРѓРЎвЂљР Вµ")
		return
	}

	ticket, err := h.service.UpdateDescription(r.Context(), id, dto.Description, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Р СњР Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р…Р С•")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncTicketByID(r.Context(), ticket.ID); err != nil {
			log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ РѕРїРёСЃР°РЅРёРµ С‚РёРєРµС‚Р° СЃ Bitrix24", "ticket_id", ticket.ID, "error", err)
		}
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

func (h *TicketHandler) Assign(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var dto api.TicketAssignDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	userID := getUserIDFromContext(r)
	ticket, err := h.service.Assign(r.Context(), id, dto.AssigneeID, userID)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncTicketByID(r.Context(), ticket.ID); err != nil {
			log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ РЅР°Р·РЅР°С‡РµРЅРёРµ РёСЃРїРѕР»РЅРёС‚РµР»СЏ РІ Bitrix24", "ticket_id", ticket.ID, "error", err)
		}
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

func (h *TicketHandler) ChangeCompany(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var dto api.TicketChangeCompanyDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	companyID := strings.TrimSpace(dto.CompanyID)
	if companyID == "" {
		response.RespondWithError(w, http.StatusBadRequest, "company_id Р С•Р В±РЎРЏР В·Р В°РЎвЂљР ВµР В»Р ВµР Р…")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р… Р Р† Р С”Р С•Р Р…РЎвЂљР ВµР С”РЎРѓРЎвЂљР Вµ")
		return
	}

	ticket, err := h.service.ChangeCompany(r.Context(), id, companyID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Р СњР Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р…Р С•")
			return
		}
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncTicketByID(r.Context(), ticket.ID); err != nil {
			log.Error("РќРµ СѓРґР°Р»РѕСЃСЊ СЃРёРЅС…СЂРѕРЅРёР·РёСЂРѕРІР°С‚СЊ С‚РёРєРµС‚ СЃ Bitrix24 РїРѕСЃР»Рµ СЃРјРµРЅС‹ РєРѕРјРїР°РЅРёРё", "ticket_id", ticket.ID, "error", err)
		}
	}
	response.RespondWithJSON(w, http.StatusOK, ticket)
}

func (h *TicketHandler) UpdateBitrixFields(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())
	id := chi.URLParam(r, "id")
	var dto api.TicketBitrixFieldsUpdateDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}

	userID := getUserIDFromContext(r)
	if userID == 0 {
		response.RespondWithError(w, http.StatusUnauthorized, "ID Р С—Р С•Р В»РЎРЉР В·Р С•Р Р†Р В°РЎвЂљР ВµР В»РЎРЏ Р Р…Р Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р… Р Р† Р С”Р С•Р Р…РЎвЂљР ВµР С”РЎРѓРЎвЂљР Вµ")
		return
	}

	ticket, err := h.service.UpdateBitrixFields(r.Context(), id, dto.BitrixServicePointID, dto.BitrixDealTitle, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Р СњР Вµ Р Р…Р В°Р в„–Р Т‘Р ВµР Р…Р С•")
			return
		}
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.bitrixService != nil && h.bitrixService.IsEnabled() {
		if err := h.bitrixService.SyncTicketByID(r.Context(), ticket.ID); err != nil {
			log.Error("Р СњР Вµ РЎС“Р Т‘Р В°Р В»Р С•РЎРѓРЎРЉ РЎРѓР С‘Р Р…РЎвЂ¦РЎР‚Р С•Р Р…Р С‘Р В·Р С‘РЎР‚Р С•Р Р†Р В°РЎвЂљРЎРЉ B24-Р С—Р С•Р В»РЎРЏ РЎвЂљР С‘Р С”Р ВµРЎвЂљР В° РЎРѓ Bitrix24", "ticket_id", ticket.ID, "error", err)
		}
	}

	response.RespondWithJSON(w, http.StatusOK, ticket)
}

// List Р Р†Р С•Р В·Р Р†РЎР‚Р В°РЎвЂ°Р В°Р ВµРЎвЂљ РЎРѓР С—Р С‘РЎРѓР С•Р С” Р В·Р В°РЎРЏР Р†Р С•Р С” РЎРѓ РЎвЂћР С‘Р В»РЎРЉРЎвЂљРЎР‚Р В°РЎвЂ Р С‘Р ВµР в„–.
func (h *TicketHandler) List(w http.ResponseWriter, r *http.Request) {
	log := middleware.GetLogger(r.Context())

	// Р СџР С•Р В»РЎС“РЎвЂЎР В°Р ВµР С Р С—Р В°РЎР‚Р В°Р СР ВµРЎвЂљРЎР‚РЎвЂ№ Р С—Р В°Р С–Р С‘Р Р…Р В°РЎвЂ Р С‘Р С‘.
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}

	// Р РЋР С•Р В±Р С‘РЎР‚Р В°Р ВµР С РЎвЂћР С‘Р В»РЎРЉРЎвЂљРЎР‚РЎвЂ№.
	filter := tickets.TicketFilter{
		Limit:       limit,
		Offset:      offset,
		CompanyID:   r.URL.Query().Get("company_id"),
		SearchQuery: r.URL.Query().Get("search"),
		SortBy:      r.URL.Query().Get("sort_by"),
	}

	// Р В¤Р С‘Р В»РЎРЉРЎвЂљРЎР‚ Р С—Р С• РЎРѓРЎвЂљР В°РЎвЂљРЎС“РЎРѓР В°Р С.
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = expandStatuses(strings.Split(statusStr, ","))
	}
	if assetID := r.URL.Query().Get("asset_id"); assetID != "" {
		filter.AssetID = &assetID
	}

	items, total, err := h.service.List(r.Context(), filter)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Р С›РЎв‚¬Р С‘Р В±Р С”Р В° Р С—Р С•Р В»РЎС“РЎвЂЎР ВµР Р…Р С‘РЎРЏ РЎРѓР С—Р С‘РЎРѓР С”Р В° Р В·Р В°РЎРЏР Р†Р С•Р С”")
		return
	}

	// Р СџР С•Р В»РЎС“РЎвЂЎР В°Р ВµР С Р С”Р С•Р СР СР ВµР Р…РЎвЂљР В°РЎР‚Р С‘Р С‘ (Р Т‘Р В»РЎРЏ Р Р†РЎвЂ№Р Р†Р С•Р Т‘Р В° Р Р† РЎРѓР С—Р С‘РЎРѓР С”Р Вµ).
	ticketIDs := make([]string, 0, len(items))
	for _, item := range items {
		ticketIDs = append(ticketIDs, item.ID)
	}
	lastComments, err := h.service.GetLastComments(r.Context(), ticketIDs)
	if err != nil {
		log.Error("Failed to get last comments", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Р С›РЎв‚¬Р С‘Р В±Р С”Р В° Р С—Р С•Р В»РЎС“РЎвЂЎР ВµР Р…Р С‘РЎРЏ РЎРѓР С—Р С‘РЎРѓР С”Р В° Р В·Р В°РЎРЏР Р†Р С•Р С”")
		return
	}

	// Р СџРЎР‚Р ВµР С•Р В±РЎР‚Р В°Р В·РЎС“Р ВµР С Р Р† TicketListDTO.
	dtos := make([]api.TicketListDTO, len(items))
	for i, item := range items {
		var assignee *struct {
			ID       uint   `json:"id"`
			FullName string `json:"fullName"`
		}
		if item.Assignee != nil {
			assignee = &struct {
				ID       uint   `json:"id"`
				FullName string `json:"fullName"`
			}{
				ID:       item.Assignee.ID,
				FullName: item.Assignee.FullName,
			}
		}
		dtos[i] = api.TicketListDTO{
			ID:                   item.ID,
			Number:               item.Number,
			ServiceDeskUUID:      item.ServiceDeskUUID,
			Status:               item.Status,
			Subject:              item.Subject,
			Description:          item.Description,
			LastComment:          lastComments[item.ID].Text,
			LastCommentAuthor:    lastComments[item.ID].AuthorName,
			LastCommentIsPrivate: lastComments[item.ID].IsPrivate,
			CompanyID:            item.CompanyID,
			CompanyName:          item.CompanyName,
			ContractID:           item.ContractID,
			IsCommonContract:     item.IsCommonContract,
			SyncWithBitrix:       item.SyncWithBitrix,
			BitrixPointID:        item.BitrixServicePointID,
			BitrixDealTitle:      item.BitrixDealTitle,
			Assignee:             assignee,
			// LastActivityDate is basically UpdatedAt or CreatedAt
			LastActivityDate: item.UpdatedAt,
			CreatedAt:        item.CreatedAt,
		}
	}

	hasNext := int64(offset+limit) < total
	hasPrev := offset > 0
	response.RespondWithJSON(w, http.StatusOK, api.PaginatedResponse{
		Data:    dtos,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasNext: hasNext,
		HasPrev: hasPrev,
	})
}

// GetDetails Р Р†Р С•Р В·Р Р†РЎР‚Р В°РЎвЂ°Р В°Р ВµРЎвЂљ Р С—Р С•Р В»Р Р…РЎС“РЎР‹ Р С‘Р Р…РЎвЂћР С•РЎР‚Р СР В°РЎвЂ Р С‘РЎР‹ Р С• Р В·Р В°РЎРЏР Р†Р С”Р Вµ.

// Filters Р Р†Р С•Р В·Р Р†РЎР‚Р В°РЎвЂ°Р В°Р ВµРЎвЂљ Р В°Р С–РЎР‚Р ВµР С–Р С‘РЎР‚Р С•Р Р†Р В°Р Р…Р Р…РЎвЂ№Р Вµ Р В·Р Р…Р В°РЎвЂЎР ВµР Р…Р С‘РЎРЏ Р Т‘Р В»РЎРЏ РЎвЂћР С‘Р В»РЎРЉРЎвЂљРЎР‚Р С•Р Р†.
func (h *TicketHandler) Filters(w http.ResponseWriter, r *http.Request) {
	filter := tickets.TicketFilter{
		SearchQuery: r.URL.Query().Get("search"),
	}
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Statuses = expandStatuses(strings.Split(statusStr, ","))
	}

	items, err := h.service.GetCompanyFilters(r.Context(), filter)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, "Р С›РЎв‚¬Р С‘Р В±Р С”Р В° Р С—Р С•Р В»РЎС“РЎвЂЎР ВµР Р…Р С‘РЎРЏ РЎвЂћР С‘Р В»РЎРЉРЎвЂљРЎР‚Р С•Р Р† Р В·Р В°РЎРЏР Р†Р С•Р С”")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"companies": items,
	})
}

func expandStatuses(items []string) []string {
	if len(items) == 0 {
		return items
	}
	seen := make(map[string]struct{})
	var out []string
	for _, raw := range items {
		status := strings.TrimSpace(raw)
		if status == "" {
			continue
		}
		legacy := map[string][]string{
			"new":         {"registered"},
			"in_progress": {"inprogress"},
			"pending":     {"wait"},
		}
		if _, ok := seen[status]; !ok {
			seen[status] = struct{}{}
			out = append(out, status)
		}
		if legacyStatuses, ok := legacy[status]; ok {
			for _, legacyStatus := range legacyStatuses {
				if _, ok := seen[legacyStatus]; ok {
					continue
				}
				seen[legacyStatus] = struct{}{}
				out = append(out, legacyStatus)
			}
		}
	}
	return out
}

func (h *TicketHandler) GetDetails(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	details, err := h.service.GetDetails(r.Context(), id)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("Failed to get ticket details", "id", id, "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Ошибка получения деталей заявки")
		return
	}
	if details == nil {
		response.RespondWithError(w, http.StatusNotFound, "Заявка не найдена")
		return
	}

	var assignee *struct {
		ID       uint   `json:"id"`
		FullName string `json:"fullName"`
	}
	if details.Metadata.Assignee != nil {
		assignee = &struct {
			ID       uint   `json:"id"`
			FullName string `json:"fullName"`
		}{
			ID:       details.Metadata.Assignee.ID,
			FullName: details.Metadata.Assignee.FullName,
		}
	}

	type safeMetadataDTO struct {
		ID          string     `json:"id"`
		CreatedAt   time.Time  `json:"created_at"`
		UpdatedAt   time.Time  `json:"updated_at"`
		Number      int        `json:"number"`
		Subject     string     `json:"subject"`
		Description string     `json:"description"`
		Result      string     `json:"result"`
		Status      string     `json:"status"`
		Priority    string     `json:"priority"`
		Type        string     `json:"type"`
		DeadlineAt  *time.Time `json:"deadline_at"`
		AssigneeID  *uint      `json:"assignee_id"`
		Assignee    *struct {
			ID       uint   `json:"id"`
			FullName string `json:"fullName"`
		} `json:"assignee,omitempty"`
		ReporterID           *uint   `json:"reporter_id"`
		ReporterName         string  `json:"reporter_name"`
		ReporterEmail        string  `json:"reporter_email"`
		CompanyID            string  `json:"company_id"`
		CompanyName          string  `json:"company_name,omitempty"`
		ContractID           *string `json:"contract_id,omitempty"`
		IsCommonContract     bool    `json:"is_common_contract,omitempty"`
		ServiceDeskUUID      string  `json:"service_desk_uuid"`
		SyncWithBitrix       bool    `json:"sync_with_bitrix"`
		BitrixServicePointID *int64  `json:"bitrix_service_point_id,omitempty"`
		BitrixDealTitle      string  `json:"bitrix_deal_title"`
	}

	type safeCommentDTO struct {
		UUID         string    `json:"uuid"`
		Text         string    `json:"text"`
		AuthorName   string    `json:"author_name"`
		CreationDate time.Time `json:"creation_date"`
		IsInternal   bool      `json:"is_internal"`
		IsPrivate    bool      `json:"is_private"`
	}

	comments := make([]safeCommentDTO, 0, len(details.Comments))
	for _, item := range details.Comments {
		comments = append(comments, safeCommentDTO{
			UUID:         item.UUID,
			Text:         item.Text,
			AuthorName:   item.AuthorName,
			CreationDate: item.CreationDate,
			IsInternal:   item.IsInternal,
			IsPrivate:    item.IsPrivate,
		})
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"metadata": safeMetadataDTO{
			ID:                   details.Metadata.ID,
			CreatedAt:            details.Metadata.CreatedAt,
			UpdatedAt:            details.Metadata.UpdatedAt,
			Number:               details.Metadata.Number,
			Subject:              details.Metadata.Subject,
			Description:          details.Metadata.Description,
			Result:               details.Metadata.Result,
			Status:               details.Metadata.Status,
			Priority:             details.Metadata.Priority,
			Type:                 details.Metadata.Type,
			DeadlineAt:           details.Metadata.DeadlineAt,
			AssigneeID:           details.Metadata.AssigneeID,
			Assignee:             assignee,
			ReporterID:           details.Metadata.ReporterID,
			ReporterName:         details.Metadata.ReporterName,
			ReporterEmail:        details.Metadata.ReporterEmail,
			CompanyID:            details.Metadata.CompanyID,
			CompanyName:          details.Metadata.CompanyName,
			ContractID:           details.Metadata.ContractID,
			IsCommonContract:     details.Metadata.IsCommonContract,
			ServiceDeskUUID:      details.Metadata.ServiceDeskUUID,
			SyncWithBitrix:       details.Metadata.SyncWithBitrix,
			BitrixServicePointID: details.Metadata.BitrixServicePointID,
			BitrixDealTitle:      details.Metadata.BitrixDealTitle,
		},
		"company_name": details.CompanyName,
		"history":      details.History,
		"attachments":  details.Attachments,
		"comments":     comments,
	})
}

// LinkAssetRequest РЎвЂљР ВµР В»Р С• Р В·Р В°Р С—РЎР‚Р С•РЎРѓР В° Р Т‘Р В»РЎРЏ Р С—РЎР‚Р С‘Р Р†РЎРЏР В·Р С”Р С‘.
type LinkAssetRequest struct {
	AssetID   string `json:"asset_id"`
	AssetType string `json:"asset_type"` // Server, FiscalRegister, Workstation
}

// LinkAsset Р С—РЎР‚Р С‘Р Р†РЎРЏР В·РЎвЂ№Р Р†Р В°Р ВµРЎвЂљ Р В·Р В°РЎРЏР Р†Р С”РЎС“ Р С” Р С•Р В±Р С•РЎР‚РЎС“Р Т‘Р С•Р Р†Р В°Р Р…Р С‘РЎР‹.
func (h *TicketHandler) LinkAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		AssetID   string `json:"asset_id"`
		AssetType string `json:"asset_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Р СњР ВµР С”Р С•РЎР‚РЎР‚Р ВµР С”РЎвЂљР Р…РЎвЂ№Р в„– JSON")
		return
	}
	err := h.service.LinkToAsset(r.Context(), id, req.AssetID, req.AssetType)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]string{"status": "linked"})
}

func getUserIDFromContext(r *http.Request) uint {
	// Р ВРЎРѓР С—Р С•Р В»РЎРЉР В·РЎС“Р ВµР С contextkeys
	userIDStr, ok := r.Context().Value(contextkeys.UserIDContextKey).(string)
	if !ok {
		return 0
	}
	id, _ := strconv.ParseUint(userIDStr, 10, 32)
	return uint(id)
}
