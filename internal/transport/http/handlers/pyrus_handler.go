package handlers

import (
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
