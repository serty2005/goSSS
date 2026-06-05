package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/transport/http/handlers"

	"github.com/go-chi/chi/v5"
)

func TestTelephonyTicketContactRouteAvailableWhenMegafonDisabled(t *testing.T) {
	module := newTelephonyModule(
		&config.Config{EnableMegafonVATS: false},
		nil,
		nil,
		nil,
		nil,
		handlers.NewTelephonyHandler(nil),
		nil,
	)
	router := chi.NewRouter()
	module.registerProtectedRoutes(router)

	request := httptest.NewRequest(http.MethodPatch, "/telephony/tickets/ticket-1/contact", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code == http.StatusNotFound {
		t.Fatalf("маршрут контакта тикета должен быть доступен без включенной телефонии Мегафон")
	}
}

func TestTelephonyRoutesRegisterWhenMegafonEnabled(t *testing.T) {
	module := newTelephonyModule(
		&config.Config{EnableMegafonVATS: true},
		nil,
		nil,
		nil,
		nil,
		handlers.NewTelephonyHandler(nil),
		nil,
	)
	router := chi.NewRouter()

	module.registerProtectedRoutes(router)

	contactRequest := httptest.NewRequest(http.MethodPatch, "/telephony/tickets/ticket-1/contact", nil)
	contactRecorder := httptest.NewRecorder()
	router.ServeHTTP(contactRecorder, contactRequest)
	if contactRecorder.Code == http.StatusNotFound {
		t.Fatalf("маршрут контакта тикета должен быть доступен при включенной телефонии Мегафон")
	}

	lineRequest := httptest.NewRequest(http.MethodGet, "/telephony/line", nil)
	lineRecorder := httptest.NewRecorder()
	router.ServeHTTP(lineRecorder, lineRequest)
	if lineRecorder.Code == http.StatusNotFound {
		t.Fatalf("маршрут линии телефонии должен быть доступен при включенной телефонии Мегафон")
	}
}
