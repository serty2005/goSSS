package handlers

import (
	"encoding/json"
	"errors"
	domainrepos "etalon-server/internal/domain/repositories"
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
	candidateRepo domainrepos.CandidateRepo
	obsSrv        services.AgentObservationService
}

// NewCandidateHandler создает обработчик для операций с кандидатами.
func NewCandidateHandler(candidateRepo domainrepos.CandidateRepo, obsSrv services.AgentObservationService) *CandidateHandler {
	return &CandidateHandler{candidateRepo: candidateRepo, obsSrv: obsSrv}
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

	items, err := h.candidateRepo.List(r.Context(), status, limit, offset)
	if err != nil {
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

	candidate, err := h.candidateRepo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.RespondWithError(w, http.StatusNotFound, "Кандидат не найден")
			return
		}
		middleware.GetLogger(r.Context()).Error("не удалось получить кандидата", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	ws, err := h.candidateRepo.ListWorkstationStaging(r.Context(), id)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить staged-станции кандидата", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	fr, err := h.candidateRepo.ListFiscalStaging(r.Context(), id)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить staged-ФР кандидата", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":                  candidate.ID,
		"server_key":          candidate.ServerKey,
		"server_crm_id":       candidate.ServerCRMID,
		"server_url":          candidate.ServerURL,
		"existing_server_id":  candidate.ExistingServerID,
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
		input.ContractMode = strPtrOrNil(req.Company.ContractMode)
		input.ContractType = strPtrOrNil(req.Company.ContractType)
	}
	if req.Server != nil {
		input.ServerID = strPtrOrNil(req.Server.ServerID)
		input.ServerCRMID = strPtrOrNil(req.Server.CRMID)
		input.ServerURL = strPtrOrNil(req.Server.URLRms)
		input.ServerUniqueID = strPtrOrNil(req.Server.UniqueID)
		input.ServerCabinetLink = strPtrOrNil(req.Server.CabinetLink)
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
		ContractMode   *string `json:"contract_mode"`
		ContractType   *string `json:"contract_type"`
	} `json:"company"`
	Server *struct {
		Mode        string  `json:"mode"`
		ServerID    *string `json:"server_id"`
		CRMID       *string `json:"crm_id"`
		URLRms      *string `json:"url_rms"`
		UniqueID    *string `json:"unique_id"`
		CabinetLink *string `json:"cabinet_link"`
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
