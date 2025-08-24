// internal/gateways/server_polling_gateway.go
package gateways

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

// ServerPollingGateway отвечает за фоновый опрос статусов серверов и публикацию результатов.
type ServerPollingGateway interface {
	Start(ctx context.Context)
}

type serverPollingGatewayImpl struct {
	cfg        *config.Config
	logger     *zap.Logger
	serverRepo repositories.ServerRepo
	rmsClient  utils.RMSClient
	bus        eventbus.EventBus
}

func NewServerPollingGateway(cfg *config.Config, logger *zap.Logger, serverRepo repositories.ServerRepo, rmsClient utils.RMSClient, bus eventbus.EventBus) ServerPollingGateway {
	return &serverPollingGatewayImpl{cfg, logger, serverRepo, rmsClient, bus}
}

// Start запускает сервис в фоновом режиме.
func (g *serverPollingGatewayImpl) Start(ctx context.Context) {
	g.bus.Subscribe(events.ServerPollingRequested, g.handlePollingRequest) // Подписываемся на ручной запуск
	g.logger.Info("Запуск шлюза опроса статусов серверов", zap.Duration("interval", 1*time.Minute))
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	g.runCycle(ctx)
	for {
		select {
		case <-ticker.C:
			g.runCycle(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза для опроса статусов серверов...")
			return
		}
	}
}

// handlePollingRequest обрабатывает событие ручного запуска опроса.
func (g *serverPollingGatewayImpl) handlePollingRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingRequestedPayload)
	if !ok {
		return
	}
	log := g.logger.With(zap.String("trigger", "manual"), zap.String("uuid", payload.ServerUUID))
	log.Info("Обработка ручного запроса на опрос сервера")
	server, err := g.serverRepo.GetByUUID(ctx, payload.ServerUUID)
	if err != nil || server == nil {
		log.Error("Не удалось найти сервер для ручного опроса", zap.Error(err))
		return
	}
	// Запускаем обработку в новой горутине, чтобы не блокировать шину
	go g.processServer(context.Background(), *server)
}

// runCycle выполняет один цикл фоновой работы воркера.
func (g *serverPollingGatewayImpl) runCycle(ctx context.Context) {
	g.logger.Info("Начало нового цикла опроса статусов серверов...")
	servers, err := g.serverRepo.FindForPolling(ctx, g.cfg.ServerPollingBatchSize, g.cfg.ServerPollingInterval)
	if err != nil || len(servers) == 0 {
		if err != nil {
			g.logger.Error("Не удалось получить список серверов для опроса", zap.Error(err))
		} else {
			g.logger.Info("Не найдено серверов, подлежащих опросу.")
		}
		return
	}
	g.logger.Info("Найдено серверов для обработки", zap.Int("count", len(servers)))
	for _, server := range servers {
		select {
		case <-ctx.Done():
			return
		default:
			g.processServer(ctx, server)
			time.Sleep(2 * time.Second)
		}
	}
}

// processServer обрабатывает один сервер и публикует событие.
func (g *serverPollingGatewayImpl) processServer(ctx context.Context, server models.Server) {
	log := g.logger.With(zap.String("server_uuid", utils.SafeStringDereference(server.ServiceDeskUUID)), zap.String("server_ip", utils.SafeStringDereference(server.IP)))
	if server.IP == nil || *server.IP == "" {
		return // Пропускаем серверы без IP
	}
	var url string
	parts := strings.SplitN(*server.IP, ":", 2)
	host := parts[0]
	if len(parts) == 2 && (parts[1] == "443" || strings.Contains(*server.IP, "iiko.it") || strings.Contains(*server.IP, "syrve.online")) {
		url = "https://" + host
	} else {
		url = "http://" + *server.IP
	}

	info, err := g.rmsClient.GetServerMonitoringInfo(ctx, url)
	if err != nil {
		log.Warn("Не удалось получить информацию о сервере", zap.String("url", url), zap.Error(err))
		status := "offline"
		if strings.Contains(err.Error(), "no such host") {
			status = "archived"
		}
		g.bus.Publish(eventbus.Event{
			Type: events.ServerPollingFailed,
			Payload: events.ServerPollingFailedPayload{
				ServerUUID:   *server.ServiceDeskUUID,
				NewStatus:    status,
				ErrorMessage: err.Error(),
				LastPolledAt: time.Now(),
			},
		})
	} else {
		log.Info("Информация о сервере успешно получена", zap.String("state", info.ServerState))
		g.bus.Publish(eventbus.Event{
			Type: events.ServerPollingSucceeded,
			Payload: events.ServerPollingSucceededPayload{
				ServerUUID:     *server.ServiceDeskUUID,
				ServerName:     info.ServerName,
				ServerEdition:  info.Edition,
				ServerVersion:  shortenVersion(info.Version),
				NewStatus:      mapServerStateToStatus(info.ServerState),
				LastPolledAt:   time.Now(),
			},
		})
	}
}

// mapServerStateToStatus преобразует статус из ответа сервера в наш внутренний статус.
func mapServerStateToStatus(state string) string {
	switch state {
	case "STARTED_SUCCESSFULLY":
		return "active"
	case "WAITING_LICENSE":
		return "license"
	case "STARTING":
		return "starting"
	default:
		return "unknown"
	}
}


// shortenVersion обрезает версию до формата X.Y.Z
func shortenVersion(fullVersion string) string {
	if fullVersion == "" {
		return ""
	}
	re := regexp.MustCompile(`^(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(fullVersion)
	if len(matches) > 1 {
		return matches[1]
	}
	return fullVersion
}
