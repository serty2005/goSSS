package gateways

import (
	"context"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"time"
)

type BitrixGateway interface {
	Start(ctx context.Context)
}

type bitrixGatewayImpl struct {
	cfg     *config.Config
	log     logger.LoggerInterface
	service services.BitrixSyncService
}

func NewBitrixGateway(cfg *config.Config, log logger.LoggerInterface, service services.BitrixSyncService) BitrixGateway {
	return &bitrixGatewayImpl{
		cfg:     cfg,
		log:     log,
		service: service,
	}
}

func (g *bitrixGatewayImpl) Start(ctx context.Context) {
	if g.service == nil || !g.service.IsEnabled() {
		g.log.Info("Интеграция Bitrix24 отключена")
		return
	}

	syncInterval := g.cfg.BitrixSyncInterval
	if syncInterval < time.Minute {
		syncInterval = 5 * time.Minute
	}
	dictInterval := g.cfg.BitrixDictionarySyncEvery
	if dictInterval < time.Hour {
		dictInterval = 24 * time.Hour
	}

	syncTicker := time.NewTicker(syncInterval)
	defer syncTicker.Stop()
	dictTicker := time.NewTicker(dictInterval)
	defer dictTicker.Stop()

	g.refreshDictionaries(ctx)
	g.pull(ctx)

	for {
		select {
		case <-ctx.Done():
			g.log.Info("Bitrix24 gateway остановлен")
			return
		case <-syncTicker.C:
			g.pull(ctx)
		case <-dictTicker.C:
			g.refreshDictionaries(ctx)
		}
	}
}

func (g *bitrixGatewayImpl) refreshDictionaries(ctx context.Context) {
	points, err := g.service.RefreshServicePoints(ctx)
	if err != nil {
		g.log.Error("Не удалось обновить справочник точек Bitrix24", "error", err)
	} else {
		g.log.Info("Обновлен справочник точек Bitrix24", "count", points)
	}

	users, err := g.service.RefreshUsers(ctx)
	if err != nil {
		g.log.Error("Не удалось обновить пользователей Bitrix24", "error", err)
	} else {
		g.log.Info("Обновлен справочник пользователей Bitrix24", "count", users)
	}
}

func (g *bitrixGatewayImpl) pull(ctx context.Context) {
	deals, comments, err := g.service.PullFromBitrix(ctx)
	if err != nil {
		g.log.Error("Ошибка pull-синхронизации из Bitrix24", "error", err)
		return
	}
	g.log.Info("Pull-синхронизация из Bitrix24 завершена", "deals_updated", deals, "comments_imported", comments)
}
