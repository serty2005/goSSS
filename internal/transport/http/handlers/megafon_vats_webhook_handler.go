package handlers

import (
	"errors"
	"etalon-server/internal/infra/plugins/megafonvats"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/middleware"
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
	log := middleware.GetLogger(r.Context())
	requestURL := buildMegafonWebhookRequestURL(r)
	logResponse := func(statusCode int, body string) {
		log.Debug(
			"Мегафон ВАТС webhook ответ",
			"method", r.Method,
			"url", requestURL,
			"status_code", statusCode,
			"body", body,
		)
	}

	if h == nil || h.service == nil {
		log.Warn("Мегафон ВАТС webhook недоступен", "method", r.Method, "url", requestURL)
		logResponse(http.StatusServiceUnavailable, "входящая синхронизация Мегафон ВАТС недоступна")
		response.RespondWithError(w, http.StatusServiceUnavailable, "входящая синхронизация Мегафон ВАТС недоступна")
		return
	}
	if r.Method != http.MethodPost {
		logResponse(http.StatusMethodNotAllowed, "метод не поддерживается")
		response.RespondWithError(w, http.StatusMethodNotAllowed, "метод не поддерживается")
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if !strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		log.Debug(
			"Мегафон ВАТС webhook запрос",
			"method", r.Method,
			"url", requestURL,
			"content_type", contentType,
		)
		logResponse(http.StatusBadRequest, "ожидается content-type application/x-www-form-urlencoded")
		response.RespondWithError(w, http.StatusBadRequest, "ожидается content-type application/x-www-form-urlencoded")
		return
	}

	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Error("Мегафон ВАТС webhook: не удалось прочитать payload", "method", r.Method, "url", requestURL, "error", err)
		logResponse(http.StatusBadRequest, "не удалось прочитать payload")
		response.RespondWithError(w, http.StatusBadRequest, "не удалось прочитать payload")
		return
	}
	log.Debug(
		"Мегафон ВАТС webhook запрос",
		"method", r.Method,
		"url", requestURL,
		"content_type", contentType,
		"body", megafonvats.RedactFormPayloadForLog(string(rawBody)),
	)
	form, err := url.ParseQuery(string(rawBody))
	if err != nil {
		logResponse(http.StatusBadRequest, "некорректный form-urlencoded payload")
		response.RespondWithError(w, http.StatusBadRequest, "некорректный form-urlencoded payload")
		return
	}

	if err = h.service.HandleWebhook(r.Context(), rawBody, form); err != nil {
		switch {
		case errors.Is(err, services.ErrMegafonVATSWebhookUnauthorized):
			log.Warn("Мегафон ВАТС webhook отклонён", "method", r.Method, "url", requestURL, "error", err)
			logResponse(http.StatusUnauthorized, err.Error())
			response.RespondWithError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, services.ErrMegafonVATSWebhookBadRequest):
			log.Warn("Мегафон ВАТС webhook отклонён", "method", r.Method, "url", requestURL, "error", err)
			logResponse(http.StatusBadRequest, err.Error())
			response.RespondWithError(w, http.StatusBadRequest, err.Error())
		default:
			log.Error("Мегафон ВАТС webhook завершился ошибкой", "method", r.Method, "url", requestURL, "error", err)
			logResponse(http.StatusInternalServerError, err.Error())
			response.RespondWithError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	logResponse(http.StatusAccepted, "{\"status\":\"accepted\"}")
	response.RespondWithJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func buildMegafonWebhookRequestURL(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}

	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return megafonvats.RedactURLForLog(r.URL.String())
	}

	fullURL := *r.URL
	fullURL.Scheme = scheme
	fullURL.Host = host
	return megafonvats.RedactURLForLog(fullURL.String())
}
