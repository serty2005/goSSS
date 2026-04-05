package handlers

import (
	"errors"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/response"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type MegafonVATSWebhookHandler struct {
	service services.MegafonVATSIncomingService
}

func NewMegafonVATSWebhookHandler(service services.MegafonVATSIncomingService) *MegafonVATSWebhookHandler {
	return &MegafonVATSWebhookHandler{service: service}
}

func (h *MegafonVATSWebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.service == nil {
		response.RespondWithError(w, http.StatusServiceUnavailable, "входящая синхронизация Мегафон ВАТС недоступна")
		return
	}
	if r.Method != http.MethodPost {
		response.RespondWithError(w, http.StatusMethodNotAllowed, "метод не поддерживается")
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		response.RespondWithError(w, http.StatusBadRequest, "ожидается content-type application/x-www-form-urlencoded")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "не удалось прочитать payload")
		return
	}
	form, err := url.ParseQuery(string(rawBody))
	if err != nil {
		response.RespondWithError(w, http.StatusBadRequest, "некорректный form-urlencoded payload")
		return
	}

	if err = h.service.HandleWebhook(r.Context(), rawBody, form); err != nil {
		switch {
		case errors.Is(err, services.ErrMegafonVATSWebhookUnauthorized):
			response.RespondWithError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, services.ErrMegafonVATSWebhookBadRequest):
			response.RespondWithError(w, http.StatusBadRequest, err.Error())
		default:
			response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}
