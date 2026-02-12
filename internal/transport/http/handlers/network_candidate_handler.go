package handlers

import (
	"encoding/json"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type NetworkCandidateHandler struct {
	service services.NetworkCandidateService
}

func NewNetworkCandidateHandler(service services.NetworkCandidateService) *NetworkCandidateHandler {
	return &NetworkCandidateHandler{service: service}
}

func (h *NetworkCandidateHandler) List(w http.ResponseWriter, r *http.Request) {
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "ACTIVE"
	}
	limit := parseIntOrDefault(r.URL.Query().Get("limit"), 100)
	offset := parseIntOrDefault(r.URL.Query().Get("offset"), 0)
	items, err := h.service.List(r.Context(), status, limit, offset)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось получить список network-кандидатов", "error", err)
		response.RespondWithError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, items)
}

func (h *NetworkCandidateHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(chi.URLParam(r, "id"))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный идентификатор кандидата")
		return
	}
	item, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		response.RespondWithError(w, http.StatusNotFound, "Кандидат не найден")
		return
	}
	response.RespondWithJSON(w, http.StatusOK, item)
}

type networkCandidateApproveRequest struct {
	ChildCompanyID *string `json:"child_company_id"`
	ChildCompany   *struct {
		Title   string  `json:"title"`
		Address *string `json:"address"`
	} `json:"child_company"`
	Comment *string `json:"comment"`
}

func (h *NetworkCandidateHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, err := parseUintParam(chi.URLParam(r, "id"))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный идентификатор кандидата")
		return
	}
	var req networkCandidateApproveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный JSON")
		return
	}
	in := domainrepos.NetworkCandidateApproveInput{
		CandidateID:    id,
		ChildCompanyID: strings.TrimSpace(ptrValue(req.ChildCompanyID)),
		Comment:        strPtrOrNil(req.Comment),
	}
	if req.ChildCompany != nil {
		in.ChildCompanyTitle = strFromValue(req.ChildCompany.Title)
		in.ChildCompanyAddr = strPtrOrNil(req.ChildCompany.Address)
	}
	out, err := h.service.Approve(r.Context(), in)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось подтвердить network-кандидата", "candidate_id", id, "error", err)
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}

func (h *NetworkCandidateHandler) RemoveGroup(w http.ResponseWriter, r *http.Request) {
	candidateID, err := parseUintParam(chi.URLParam(r, "id"))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный идентификатор кандидата")
		return
	}
	groupID, err := parseUintParam(chi.URLParam(r, "groupID"))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "Некорректный идентификатор группы")
		return
	}
	out, err := h.service.RemoveGroup(r.Context(), candidateID, groupID)
	if err != nil {
		middleware.GetLogger(r.Context()).Error("не удалось перенести группу network-кандидата", "candidate_id", candidateID, "group_id", groupID, "error", err)
		response.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.RespondWithJSON(w, http.StatusOK, out)
}

func parseUintParam(raw string) (uint, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
