package gateways

import (
	"context"
	"etalon-server/internal/core/integrations"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"time"
)

type ContractGateway interface {
	Start(ctx context.Context)
}

type contractGatewayImpl struct {
	cfg             *config.Config
	logger          logger.LoggerInterface
	manager         *integrations.Manager
	contractService contract.Service
}

func NewContractGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	manager *integrations.Manager,
	contractService contract.Service,
) ContractGateway {
	return &contractGatewayImpl{
		cfg:             cfg,
		logger:          logger,
		manager:         manager,
		contractService: contractService,
	}
}

func (g *contractGatewayImpl) Start(ctx context.Context) {
	interval := g.cfg.ContractSyncInterval
	if interval < 1*time.Minute {
		interval = 30 * time.Minute
	}

	g.logger.Info("Запуск шлюза синхронизации контрактов", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.sync(ctx)

	for {
		select {
		case <-ticker.C:
			g.sync(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза контрактов.")
			return
		}
	}
}

func (g *contractGatewayImpl) sync(ctx context.Context) {
	g.logger.Info("Начало цикла синхронизации контрактов...")

	providers := g.manager.GetContractProviders()
	for _, provider := range providers {
		g.processProvider(ctx, provider)
	}

	g.logger.Info("Цикл синхронизации контрактов завершен.")
}

func (g *contractGatewayImpl) processProvider(ctx context.Context, provider integration.ContractProvider) {
	log := g.logger.With("system", provider.SystemName())

	// Получаем MAP контрактов (ExternalID -> Model)
	contracts, err := provider.GetContracts(ctx)
	if err != nil {
		log.Error("Не удалось получить список контрактов", "error", err)
		return
	}

	log.Info("Получено контрактов от провайдера", "count", len(contracts))

	// Передаем карту в сервис (сервис уже обновлен и принимает map)
	if err := g.contractService.SyncContracts(ctx, contracts); err != nil {
		log.Error("Ошибка при синхронизации контрактов в сервисе", "error", err)
	}
}
