package handlers

import (
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strings"
)

type MegafonVATSHandler struct {
	service services.MegafonVATSSyncService
}

func NewMegafonVATSHandler(service services.MegafonVATSSyncService) *MegafonVATSHandler {
	return &MegafonVATSHandler{service: service}
}

func (h *MegafonVATSHandler) RefreshUsers(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "синхронизация сотрудников Мегафон ВАТС недоступна")
		return
	}

	count, err := h.service.RefreshEmployees(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	users, err := h.service.ListCachedEmployees(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]api.MegafonVATSEmployeeDTO, 0, len(users))
	for i := range users {
		items = append(items, api.MegafonVATSEmployeeDTO{
			Login:      users[i].EmployeeLogin,
			Name:       users[i].EmployeeName,
			Ext:        megafonStringValue(users[i].Ext),
			Telnum:     megafonStringValue(users[i].Telnum),
			Status:     megafonStringValue(users[i].Status),
			LastSeenAt: &users[i].LastSeenAt,
			UpdatedAt:  users[i].UpdatedAt,
		})
	}

	response.RespondWithJSON(w, http.StatusOK, api.MegafonVATSEmployeesRefreshDTO{
		Status:    "ok",
		Count:     count,
		Employees: items,
	})
}

func (h *MegafonVATSHandler) SuggestUser(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil || !h.service.IsEnabled() {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}

	firstName := strings.TrimSpace(r.URL.Query().Get("first_name"))
	lastName := strings.TrimSpace(r.URL.Query().Get("last_name"))
	fullName := strings.TrimSpace(r.URL.Query().Get("full_name"))
	if firstName == "" || lastName == "" {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}

	items, err := h.service.SearchEmployeesByName(r.Context(), firstName, lastName, fullName)
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(items) == 0 {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}

	response.RespondWithJSON(w, http.StatusOK, map[string]any{
		"suggestion": &api.MegafonVATSUserSuggestionDTO{
			Login: items[0].EmployeeLogin,
			Name:  items[0].EmployeeName,
		},
	})
}

func megafonStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
