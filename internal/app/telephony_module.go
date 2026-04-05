package app

import (
	"context"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/handlers"
	"etalon-server/internal/transport/http/middleware"
	"sync"

	"github.com/go-chi/chi/v5"
)

type telephonyModule struct {
	cfg              *config.Config
	log              logger.LoggerInterface
	syncService      services.MegafonVATSSyncService
	incomingService  services.MegafonVATSIncomingService
	telephonyHandler *handlers.MegafonVATSHandler
	webhookHandler   *handlers.MegafonVATSWebhookHandler
}

func newTelephonyModule(
	cfg *config.Config,
	log logger.LoggerInterface,
	syncService services.MegafonVATSSyncService,
	incomingService services.MegafonVATSIncomingService,
	telephonyHandler *handlers.MegafonVATSHandler,
	webhookHandler *handlers.MegafonVATSWebhookHandler,
) *telephonyModule {
	return &telephonyModule{
		cfg:              cfg,
		log:              log,
		syncService:      syncService,
		incomingService:  incomingService,
		telephonyHandler: telephonyHandler,
		webhookHandler:   webhookHandler,
	}
}

func (m *telephonyModule) Enabled() bool {
	return m != nil && m.cfg != nil && m.cfg.EnableMegafonVATS
}

func (m *telephonyModule) registerPublicRoutes(r chi.Router) {
	if !m.Enabled() || m.webhookHandler == nil {
		return
	}
	r.Post("/api/integrations/megafon-vats/webhook", m.webhookHandler.HandleWebhook)
}

func (m *telephonyModule) registerProtectedRoutes(r chi.Router) {
	if !m.Enabled() || m.telephonyHandler == nil {
		return
	}
	r.Route("/megafon-vats", func(r chi.Router) {
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/users/refresh", m.telephonyHandler.RefreshUsers)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/users/suggest", m.telephonyHandler.SuggestUser)
	})
}

func (m *telephonyModule) start(ctx context.Context, wg *sync.WaitGroup) {
	if !m.Enabled() {
		return
	}
	if m.syncService != nil && m.syncService.IsEnabled() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.syncService.Start(ctx)
		}()
	}
	if m.incomingService != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.incomingService.Start(ctx)
		}()
	}
	if m.log != nil {
		m.log.Info("Модуль телефонии Мегафон ВАТС подключен")
	}
}
