package handlers

import (
	"errors"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/response"
	"io"
	"net/http"
	"strings"
)

type PyrusWebhookHandler struct {
	service services.PyrusIncomingService
}

func NewPyrusWebhookHandler(service services.PyrusIncomingService) *PyrusWebhookHandler {
	return &PyrusWebhookHandler{service: service}
}

func (h *PyrusWebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "входящая синхронизация Pyrus недоступна")
		return
	}
	if r.Method != http.MethodPost {
		response.RespondWithError(w, http.StatusMethodNotAllowed, "метод не поддерживается")
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if !strings.HasPrefix(contentType, "application/json") {
		response.RespondWithError(w, http.StatusBadRequest, "ожидается content-type application/json")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "не удалось прочитать payload")
		return
	}

	if err = h.service.HandleWebhook(r.Context(), rawBody, r.Header.Get("X-Pyrus-Sig")); err != nil {
		switch {
		case errors.Is(err, services.ErrPyrusWebhookUnauthorized):
			response.RespondWithError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, services.ErrPyrusWebhookBadRequest):
			response.RespondWithError(w, http.StatusBadRequest, err.Error())
		default:
			response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
