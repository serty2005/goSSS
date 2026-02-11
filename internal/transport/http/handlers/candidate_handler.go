package handlers

import (
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

// CandidateHandler обслуживает API раздела "Принятие на АО".
type CandidateHandler struct {
	db     *gorm.DB
	obsSrv services.AgentObservationService
}

// NewCandidateHandler создает обработчик для операций с кандидатами.
func NewCandidateHandler(db *gorm.DB, obsSrv services.AgentObservationService) *CandidateHandler {
	return &CandidateHandler{db: db, obsSrv: obsSrv}
}

// List возвращает список кандидатов с фильтром по статусу.
func (h *CandidateHandler) List(w http.ResponseWriter, r *http.Request) {
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "ACTIVE"
	}
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 100)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0)
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	tx := h.db.WithContext(r.Context()).Model(&models.Candidate{})
	switch status {
	case "ACTIVE":
		tx = tx.Where("status IN ?", []string{models.CandidateStatusNew, models.CandidateStatusInReview})
	case "ALL":
		// Без дополнительного фильтра.
	default:
		tx = tx.Where("status = ?", status)
	}

	var items []models.Candidate
	if err := tx.Order("updated_at desc").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить список кандидатов", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, items)
}

// Get возвращает карточку кандидата вместе с staged-данными по станциям и ФР.
func (h *CandidateHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseCandidateID(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный идентификатор кандидата")
		return
	}

	var candidate models.Candidate
	if err := h.db.WithContext(r.Context()).Where("id = ?", id).First(&candidate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Кандидат не найден")
			return
		}
		middleware.GetLogger(r.Context()).Error("не удалось получить кандидата", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	var ws []models.CandidateWorkstationStaging
	if err := h.db.WithContext(r.Context()).Where("candidate_id = ?", id).Order("observed_at desc, id desc").Find(&ws).Error; err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить staged-станции кандидата", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	var fr []models.CandidateFiscalStaging
	if err := h.db.WithContext(r.Context()).Where("candidate_id = ?", id).Order("observed_at desc, id desc").Find(&fr).Error; err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить staged-ФР кандидата", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":                  candidate.ID,
		"server_key":          candidate.ServerKey,
		"server_crm_id":       candidate.ServerCRMID,
		"server_url":          candidate.ServerURL,
		"status":              candidate.Status,
		"ticket_id":           candidate.TicketID,
		"approved_company_id": candidate.ApprovedCompanyID,
		"approved_server_id":  candidate.ApprovedServerID,
		"created_at":          candidate.CreatedAt,
		"updated_at":          candidate.UpdatedAt,
		"staged_workstations": ws,
		"staged_fiscals":      fr,
	})
}

// Approve подтверждает кандидата и запускает применение всех staged-наблюдений.
func (h *CandidateHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := parseCandidateID(r)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный идентификатор кандидата")
		return
	}

	var req candidateApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}

	input := services.CandidateApproveInput{CandidateID: id, Comment: strPtrOrNil(req.Comment)}
	if req.CompanyID != nil {
		input.CompanyID = strings.TrimSpace(*req.CompanyID)
	}
	if req.Company != nil {
		input.CompanyTitle = strFromValue(req.Company.Title)
		input.CompanyAddress = strPtrOrNil(req.Company.Address)
		input.CompanyAdditionalName = strPtrOrNil(req.Company.AdditionalName)
		input.CompanyParentID = strPtrOrNil(req.Company.ParentID)
	}
	if req.Server != nil {
		input.ServerID = strPtrOrNil(req.Server.ServerID)
		input.ServerCRMID = strPtrOrNil(req.Server.CRMID)
		input.ServerURL = strPtrOrNil(req.Server.URLRms)
		input.ServerName = strPtrOrNil(req.Server.DeviceName)
		input.ServerDesc = strPtrOrNil(req.Server.Description)
	}
	if len(req.Workstations) > 0 {
		input.Workstations = make([]services.CandidateWorkstationInput, 0, len(req.Workstations))
		for _, ws := range req.Workstations {
			input.Workstations = append(input.Workstations, services.CandidateWorkstationInput{
				StagingID:       ws.StagingID,
				WorkstationUUID: strPtrOrNil(ws.WorkstationUUID),
				Name:            strings.TrimSpace(ws.Name),
			})
		}
	}

	updated, err := h.obsSrv.ApproveCandidate(r.Context(), input)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось подтвердить кандидата", "candidate_id", id, "error", err)
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, updated)
}

// candidateApproveRequest описывает входной JSON для подтверждения кандидата.
type candidateApproveRequest struct {
	CompanyID *string `json:"company_id"`
	Company   *struct {
		Title          string  `json:"title"`
		Address        *string `json:"address"`
		AdditionalName *string `json:"additional_name"`
		ParentID       *string `json:"parent_id"`
	} `json:"company"`
	Server *struct {
		Mode        string  `json:"mode"`
		ServerID    *string `json:"server_id"`
		CRMID       *string `json:"crm_id"`
		URLRms      *string `json:"url_rms"`
		DeviceName  *string `json:"device_name"`
		Description *string `json:"description"`
	} `json:"server"`
	Workstations []struct {
		StagingID       *uint   `json:"staging_id"`
		WorkstationUUID *string `json:"workstation_uuid"`
		Name            string  `json:"name"`
	} `json:"workstations"`
	Comment *string `json:"comment"`
}

// parseCandidateID извлекает ID кандидата из URL.
func parseCandidateID(r *http.Request) (uint, error) {
	rawID := strings.TrimSpace(chi.URLParam(r, "id"))
	id64, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(id64), nil
}

// parseIntOrDefault преобразует строку в int с fallback-значением.
func parseIntOrDefault(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

// strPtrOrNil возвращает trimmed-строку как указатель или nil.
func strPtrOrNil(v *string) *string {
	if v == nil {
		return nil
	}
	t := strings.TrimSpace(*v)
	if t == "" {
		return nil
	}
	return &t
}

// strFromValue возвращает указатель на trimmed-строку или nil.
func strFromValue(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
