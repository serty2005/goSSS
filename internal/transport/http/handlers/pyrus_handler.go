package handlers

import (
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/response"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type PyrusHandler struct {
	service services.PyrusSyncService
}

func NewPyrusHandler(service services.PyrusSyncService) *PyrusHandler {
	return &PyrusHandler{service: service}
}

func (h *PyrusHandler) RegisterRoutes(r chi.Router) {
	r.Get("/users/suggest", h.SuggestUser)
	r.Post("/users/refresh", h.RefreshUsers)
}

func (h *PyrusHandler) SuggestUser(w http.ResponseWriter, r *http.Request) {
	if h.service == nil || !h.service.IsEnabled() {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}

	firstName := strings.TrimSpace(r.URL.Query().Get("first_name"))
	lastName := strings.TrimSpace(r.URL.Query().Get("last_name"))
	fullName := strings.TrimSpace(r.URL.Query().Get("full_name"))
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" && (firstName == "" || lastName == "") {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}

	members, err := h.service.ListMembers(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}
	suggestion := services.FindPyrusUserSuggestionByIdentity(firstName, lastName, fullName, email, members)
	if suggestion == nil {
		response.RespondWithJSON(w, http.StatusOK, map[string]any{"suggestion": nil})
		return
	}
	response.RespondWithJSON(w, http.StatusOK, map[string]any{
		"suggestion": &api.PyrusUserSuggestionDTO{
			PyrusUserID: suggestion.PyrusUserID,
			Name:        suggestion.Name,
			Email:       suggestion.Email,
		},
	})
}

func (h *PyrusHandler) RefreshUsers(w http.ResponseWriter, r *http.Request) {
	if h.service == nil || !h.service.IsEnabled() {
		response.RespondWithJSON(w, http.StatusOK, api.PyrusUsersRefreshDTO{
			Status: "ok",
			Count:  0,
			Users:  []api.PyrusDirectoryUserDTO{},
		})
		return
	}

	members, err := h.service.ListMembers(r.Context())
	if err != nil {
		response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response.RespondWithJSON(w, http.StatusOK, api.PyrusUsersRefreshDTO{
		Status: "ok",
		Count:  len(members),
		Users:  mapPyrusMembersToDTO(members),
	})
}

func mapPyrusMembersToDTO(items []pyrusplugin.Member) []api.PyrusDirectoryUserDTO {
	result := make([]api.PyrusDirectoryUserDTO, 0, len(items))
	for _, item := range items {
		result = append(result, api.PyrusDirectoryUserDTO{
			PyrusUserID:     item.ID,
			Name:            item.DisplayName(),
			FirstName:       item.FirstName,
			LastName:        item.LastName,
			Email:           item.Email,
			Position:        item.Position,
			Type:            item.Type,
			Status:          item.Status,
			Banned:          item.Banned,
			Fired:           item.Fired,
			MobilePhone:     item.MobilePhone,
			Phone:           item.Phone,
			Location:        item.Location,
			PersonnelNumber: item.PersonnelNumber,
		})
	}
	return result
}
