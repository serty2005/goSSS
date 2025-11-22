package gateways

import (
	"context"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"time"
)

// ContractGateway отвечает за периодическую синхронизацию контрактов.
type ContractGateway interface {
	Start(ctx context.Context)
}

type contractGatewayImpl struct {
	cfg             *config.Config
	logger          logger.LoggerInterface
	sdClient        external.ExternalSystemClient
	contractService contract.Service
}

// NewContractGateway создает новый экземпляр шлюза контрактов.
func NewContractGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	sdClient external.ExternalSystemClient,
	contractService contract.Service,
) ContractGateway {
	return &contractGatewayImpl{
		cfg:             cfg,
		logger:          logger,
		sdClient:        sdClient,
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

	// Запускаем первый раз немедленно
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

	// Запрашиваем список контрактов из внешней системы (Naumen)
	// Используем entityType="Contract", маппер клиента знает, что это "agreement$agreement"
	rawContracts, err := g.sdClient.FetchEntityList(ctx, "Contract")
	if err != nil {
		g.logger.Error("Не удалось получить список контрактов из ServiceDesk", "error", err)
		return
	}

	g.logger.Info("Получено контрактов из ServiceDesk", "count", len(rawContracts))

	// Передаем данные в сервис для обработки
	if err := g.contractService.SyncContracts(ctx, rawContracts); err != nil {
		g.logger.Error("Ошибка при синхронизации контрактов в сервисе", "error", err)
	} else {
		g.logger.Info("Цикл синхронизации контрактов успешно завершен.")
	}
}
