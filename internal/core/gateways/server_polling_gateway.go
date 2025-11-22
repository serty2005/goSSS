package gateways

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/iiko"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/pkg/eventbus"
	"net"
	"regexp"
	"strings"
	"time"
)

// ServerPollingGateway отвечает за фоновый опрос статусов серверов и публикацию результатов.
type ServerPollingGateway interface {
	Start(ctx context.Context)
}

type serverPollingGatewayImpl struct {
	cfg        *config.Config
	logger     logger.LoggerInterface
	serverRepo server.Repository
	rmsClient  iiko.IikoClient
	bus        eventbus.EventBus
}

func NewServerPollingGateway(cfg *config.Config, logger logger.LoggerInterface, serverRepo server.Repository, rmsClient iiko.IikoClient, bus eventbus.EventBus) ServerPollingGateway {
	return &serverPollingGatewayImpl{cfg, logger, serverRepo, rmsClient, bus}
}

// Start запускает сервис в фоновом режиме.
func (g *serverPollingGatewayImpl) Start(ctx context.Context) {
	g.bus.Subscribe(events.ServerPollingRequested, g.handlePollingRequest) // Подписываемся на ручной запуск
	g.logger.Info("Запуск шлюза опроса статусов серверов", "interval", 1*time.Minute)
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
	// Payload.ServerUUID здесь уже является внутренним ID
	log := g.logger.With("request_id", payload.ServerUUID, "trigger", "manual")
	log.Info("Обработка ручного запроса на опрос сервера")

	// Используем новый метод GetByID
	server, err := g.serverRepo.GetByID(ctx, payload.ServerUUID)
	if err != nil || server == nil {
		log.Error("Не удалось найти сервер для ручного опроса", "error", err)
		return
	}
	go g.processServer(context.Background(), *server)
}

// runCycle выполняет один цикл фоновой работы воркера.
func (g *serverPollingGatewayImpl) runCycle(ctx context.Context) {
	g.logger.Info("Начало нового цикла опроса статусов серверов...")
	servers, err := g.serverRepo.FindForPolling(ctx, g.cfg.ServerPollingBatchSize, g.cfg.ServerPollingInterval)
	if err != nil || len(servers) == 0 {
		if err != nil {
			g.logger.Error("Не удалось получить список серверов для опроса", "error", err)
		} else {
			g.logger.Info("Не найдено серверов, подлежащих опросу.")
		}
		return
	}
	g.logger.Info("Найдено серверов для обработки", "count", len(servers))
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
func (g *serverPollingGatewayImpl) processServer(ctx context.Context, server server.Server) {
	serverIP := utils.SafeStringDereference(server.IP)
	requestID := server.ID
	if serverIP != "" {
		requestID = server.ID + "@" + serverIP
	}
	log := g.logger.With("request_id", requestID)
	if server.IP == nil || *server.IP == "" {
		log.Warn("IP-адрес сервера отсутствует, устанавливаем статус undefined")
		g.bus.Publish(eventbus.Event{
			Type: events.ServerPollingFailed,
			Payload: events.ServerPollingFailedPayload{
				ServerUUID:   server.ID,
				RequestID:    requestID,
				NewStatus:    "undefined",
				ErrorMessage: "IP-адрес сервера отсутствует",
				LastPolledAt: time.Now(),
			},
		})
		return
	}

	var url string
	parts := strings.SplitN(*server.IP, ":", 2)
	host := parts[0]

	// Проверяем, является ли host IP-адресом и локальным
	if net.ParseIP(host) != nil {
		isPrivate, err := utils.IsPrivateIP(host)
		if err != nil {
			log.Warn("Не удалось проверить IP-адрес на приватность", "error", err)
			g.bus.Publish(eventbus.Event{
				Type: events.ServerPollingFailed,
				Payload: events.ServerPollingFailedPayload{
					ServerUUID:   server.ID,
					RequestID:    requestID,
					NewStatus:    "undefined",
					ErrorMessage: "Ошибка проверки IP-адреса: " + err.Error(),
					LastPolledAt: time.Now(),
				},
			})
			return
		}
		if isPrivate {
			log.Warn("IP-адрес сервера является локальным, устанавливаем статус undefined")
			g.bus.Publish(eventbus.Event{
				Type: events.ServerPollingFailed,
				Payload: events.ServerPollingFailedPayload{
					ServerUUID:   server.ID,
					RequestID:    requestID,
					NewStatus:    "undefined",
					ErrorMessage: "IP-адрес сервера является локальным",
					LastPolledAt: time.Now(),
				},
			})
			return
		}
	}

	if len(parts) == 2 && (parts[1] == "443" || strings.Contains(*server.IP, "iiko.it") || strings.Contains(*server.IP, "syrve.online")) {
		url = "https://" + host
	} else {
		url = "http://" + *server.IP
	}

	info, err := g.rmsClient.GetServerMonitoringInfo(ctx, url)
	if err != nil {
		log.Warn("Не удалось получить информацию о сервере", "url", url, "error", err)
		status := "undefined"
		if strings.Contains(err.Error(), "no such host") {
			status = "archived"
		}
		g.bus.Publish(eventbus.Event{
			Type: events.ServerPollingFailed,
			Payload: events.ServerPollingFailedPayload{
				ServerUUID:   server.ID, // ИЗМЕНЕНИЕ: Используем внутренний ID
				RequestID:    requestID,
				NewStatus:    status,
				ErrorMessage: err.Error(),
				LastPolledAt: time.Now(),
			},
		})
	} else {
		log.Info("Информация о сервере успешно получена", "state", info.ServerState)
		g.bus.Publish(eventbus.Event{
			Type: events.ServerPollingSucceeded,
			Payload: events.ServerPollingSucceededPayload{
				ServerUUID:    server.ID, // ИЗМЕНЕНИЕ: Используем внутренний ID
				RequestID:     requestID,
				ServerName:    info.ServerName,
				ServerEdition: info.Edition,
				ServerVersion: shortenVersion(info.Version),
				NewStatus:     mapServerStateToStatus(info.ServerState),
				LastPolledAt:  time.Now(),
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
