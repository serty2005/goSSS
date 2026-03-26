package app

import (
	"context"
	coreIntegrations "etalon-server/internal/core/integrations"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/internal/transport/http/handlers"
	"etalon-server/internal/transport/http/middleware"
	"etalon-server/pkg/eventbus"
	"sync"

	"github.com/go-chi/chi/v5"
)

type pyrusModule struct {
	cfg             *config.Config
	log             logger.LoggerInterface
	eventBus        eventbus.EventBus
	syncService     services.PyrusSyncService
	incomingService services.PyrusIncomingService
	pyrusHandler    *handlers.PyrusHandler
	webhookHandler  *handlers.PyrusWebhookHandler
}

func newPyrusModule(
	cfg *config.Config,
	log logger.LoggerInterface,
	bus eventbus.EventBus,
	syncService services.PyrusSyncService,
	incomingService services.PyrusIncomingService,
	pyrusHandler *handlers.PyrusHandler,
	webhookHandler *handlers.PyrusWebhookHandler,
) *pyrusModule {
	return &pyrusModule{
		cfg:             cfg,
		log:             log,
		eventBus:        bus,
		syncService:     syncService,
		incomingService: incomingService,
		pyrusHandler:    pyrusHandler,
		webhookHandler:  webhookHandler,
	}
}

func (m *pyrusModule) Enabled() bool {
	return m != nil && m.cfg != nil && m.cfg.EnablePyrusGateway
}

func (m *pyrusModule) registerEventHandlers() {
	if !m.Enabled() || m.eventBus == nil || m.syncService == nil {
		return
	}
	coreIntegrations.RegisterPyrusEventHandlers(m.cfg, m.log.With("component", "pyrus_event_bridge"), m.eventBus, m.syncService)
}

func (m *pyrusModule) registerPublicRoutes(r chi.Router) {
	if !m.Enabled() || m.webhookHandler == nil {
		return
	}
	r.Post("/api/integrations/pyrus/webhook", m.webhookHandler.HandleWebhook)
}

func (m *pyrusModule) registerProtectedRoutes(r chi.Router) {
	if !m.Enabled() || m.pyrusHandler == nil {
		return
	}
	r.Route("/pyrus", func(r chi.Router) {
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/users/suggest", m.pyrusHandler.SuggestUser)
	})
}

func (m *pyrusModule) registerProfileRoutes(r chi.Router, userHandler *handlers.UserHandler) {
	if !m.Enabled() || userHandler == nil {
		return
	}
	r.Post("/integrations/pyrus/sync-suggestion", userHandler.ApplyMyPyrusSuggestion)
}

func (m *pyrusModule) start(ctx context.Context, wg *sync.WaitGroup) {
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
	if m.cfg.PyrusWebhookEnabled && m.incomingService != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.incomingService.Start(ctx)
		}()
	}
	if m.log != nil {
		m.log.Info("Pyrus работает в событийном режиме")
	}
}
