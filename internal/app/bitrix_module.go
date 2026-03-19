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
	"time"

	"github.com/go-chi/chi/v5"
)

// bitrixModule инкапсулирует подключение Bitrix24 в приложение.
type bitrixModule struct {
	cfg             *config.Config
	log             logger.LoggerInterface
	eventBus        eventbus.EventBus
	syncService     services.BitrixSyncService
	incomingService services.BitrixIncomingService
	bitrixHandler   *handlers.BitrixHandler
	webhookHandler  *handlers.BitrixWebhookHandler
}

func newBitrixModule(
	cfg *config.Config,
	log logger.LoggerInterface,
	bus eventbus.EventBus,
	syncService services.BitrixSyncService,
	incomingService services.BitrixIncomingService,
	bitrixHandler *handlers.BitrixHandler,
	webhookHandler *handlers.BitrixWebhookHandler,
) *bitrixModule {
	return &bitrixModule{
		cfg:             cfg,
		log:             log,
		eventBus:        bus,
		syncService:     syncService,
		incomingService: incomingService,
		bitrixHandler:   bitrixHandler,
		webhookHandler:  webhookHandler,
	}
}

func (m *bitrixModule) Enabled() bool {
	return m != nil && m.cfg != nil && m.cfg.EnableBitrixGateway
}

func (m *bitrixModule) registerEventHandlers() {
	if !m.Enabled() || m.eventBus == nil || m.syncService == nil {
		return
	}
	coreIntegrations.RegisterBitrixEventHandlers(m.cfg, m.log.With("component", "bitrix_event_bridge"), m.eventBus, m.syncService)
}

func (m *bitrixModule) registerPublicRoutes(r chi.Router) {
	if !m.Enabled() || m.webhookHandler == nil {
		return
	}
	r.Post("/api/integrations/bitrix/webhook", m.webhookHandler.HandleWebhook)
}

func (m *bitrixModule) registerCompanyRoutes(r chi.Router, companyHandler *handlers.CompanyHandler) {
	if !m.Enabled() || companyHandler == nil {
		return
	}
	r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/bitrix-service-point-mappings", companyHandler.ListBitrixMappings)
	r.With(middleware.RequireAnyRole(user.RoleAdmin)).Put("/bitrix-service-point-mappings", companyHandler.UpdateBitrixMapping)
	r.With(middleware.RequireAnyRole(user.RoleAdmin)).Delete("/bitrix-service-point-mappings", companyHandler.ClearBitrixMapping)
}

func (m *bitrixModule) registerProtectedRoutes(r chi.Router) {
	if !m.Enabled() || m.bitrixHandler == nil {
		return
	}
	r.Route("/bitrix", func(r chi.Router) {
		r.With(middleware.RequireAnyRole(user.RoleAdmin, user.RoleSupportSpecialist)).Get("/service-points", m.bitrixHandler.ListServicePoints)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/service-points/contract-sync/state", m.bitrixHandler.GetContractSyncState)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/service-points/contract-sync/refresh", m.bitrixHandler.RefreshContractSyncState)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/service-points/contract-sync/execute", m.bitrixHandler.ExecuteContractSync)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Get("/users/suggest", m.bitrixHandler.SuggestUser)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/users/refresh", m.bitrixHandler.RefreshUsers)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/service-points/refresh", m.bitrixHandler.RefreshServicePoints)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/service-points/import/preview", m.bitrixHandler.PreviewServicePointsImport)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/service-points/import/sync-preview", m.bitrixHandler.PreviewServicePointsSync)
		r.With(middleware.RequireAnyRole(user.RoleAdmin)).Post("/service-points/import/apply", m.bitrixHandler.ImportServicePoints)
	})
}

func (m *bitrixModule) registerProfileRoutes(r chi.Router, userHandler *handlers.UserHandler) {
	if !m.Enabled() || userHandler == nil {
		return
	}
	r.Post("/integrations/bitrix/sync-suggestion", userHandler.ApplyMyBitrixSuggestion)
}

func (m *bitrixModule) start(ctx context.Context, wg *sync.WaitGroup) {
	if !m.Enabled() {
		return
	}
	if m.syncService != nil && m.syncService.IsEnabled() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.startDictionarySyncLoop(ctx)
		}()
	}
	if m.cfg.BitrixWebhookEnabled && m.incomingService != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.incomingService.Start(ctx)
		}()
	}
	if m.log != nil {
		m.log.Info("Bitrix24 работает в событийном режиме")
	}
}

func (m *bitrixModule) startDictionarySyncLoop(ctx context.Context) {
	if m.syncService == nil {
		return
	}
	interval := m.cfg.BitrixDictionarySyncEvery
	if interval < time.Minute {
		interval = 24 * time.Hour
	}

	m.refreshBitrixDictionaries(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refreshBitrixDictionaries(ctx)
		}
	}
}

func (m *bitrixModule) refreshBitrixDictionaries(ctx context.Context) {
	points, err := m.syncService.RefreshServicePoints(ctx)
	if err != nil {
		m.log.Error("Bitrix24: не удалось обновить точки обслуживания", "error", err)
	} else {
		m.log.Info("Bitrix24: обновлены точки обслуживания", "count", points)
	}

	users, err := m.syncService.RefreshUsers(ctx)
	if err != nil {
		m.log.Error("Bitrix24: не удалось обновить пользователей", "error", err)
	} else {
		m.log.Info("Bitrix24: обновлены пользователи", "count", users)
	}
}
