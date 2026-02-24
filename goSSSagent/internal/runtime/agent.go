package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/protocol"
	"etalon-agent/internal/services"
	"etalon-agent/internal/updater"
	"etalon-agent/internal/workflows"
)

type workflow interface {
	Type() string
	Run(ctx context.Context, payload []byte) error
}

type Agent struct {
	cfg       config.Config
	uuid      *services.UUIDService
	client    *client.ServiceDeskClient
	scheduler *services.Scheduler
	workflows map[string]workflow
}

func NewAgent(cfg config.Config, uuidSvc *services.UUIDService, cli *client.ServiceDeskClient) (*Agent, error) {
	if uuidSvc == nil || cli == nil {
		return nil, fmt.Errorf("не заданы обязательные зависимости агента")
	}

	a := &Agent{
		cfg:       cfg,
		uuid:      uuidSvc,
		client:    cli,
		scheduler: services.NewScheduler(),
		workflows: make(map[string]workflow),
	}
	a.registerWorkflow(workflows.NewSelfUpdateWorkflow(cfg.AgentVersion, updater.NewService(cfg.DataDir, cli)))
	return a, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.register(ctx); err != nil {
		log.Printf("Регистрация при старте не удалась: %v", err)
	}

	a.scheduler.AddTask("heartbeat", a.cfg.HeartbeatInterval, func(ctx context.Context) {
		if err := a.heartbeat(ctx); err != nil {
			log.Printf("Ошибка heartbeat: %v", err)
		}
	})

	if a.cfg.UpdateCheckInterval != a.cfg.HeartbeatInterval {
		a.scheduler.AddTask("update-poll", a.cfg.UpdateCheckInterval, func(ctx context.Context) {
			if err := a.heartbeat(ctx); err != nil {
				log.Printf("Ошибка проверки обновления через heartbeat: %v", err)
			}
		})
	}

	return a.scheduler.Run(ctx)
}

func (a *Agent) register(ctx context.Context) error {
	host := a.hostname()
	req := protocol.RegistrationRequestDTO{
		AgentUUID:    a.uuid.Get(),
		Hostname:     host,
		AgentVersion: a.cfg.AgentVersion,
		InitialData: protocol.AgentDataDTO{
			Hostname:     host,
			CurrentTime:  time.Now().Format(time.RFC3339),
			AgentVersion: a.cfg.AgentVersion,
			AgentUUID:    a.uuid.Get(),
			AgentType:    a.cfg.AgentType,
		},
	}
	if err := a.client.Register(ctx, req); err != nil {
		return err
	}
	log.Printf("Регистрация агента выполнена (uuid=%s)", a.uuid.Get())
	return nil
}

func (a *Agent) heartbeat(ctx context.Context) error {
	payload := protocol.AgentDataDTO{
		Hostname:     a.hostname(),
		CurrentTime:  time.Now().Format(time.RFC3339),
		AgentVersion: a.cfg.AgentVersion,
		AgentUUID:    a.uuid.Get(),
		AgentType:    a.cfg.AgentType,
	}
	resp, err := a.client.SendHeartbeat(ctx, a.uuid.Get(), payload)
	if err != nil {
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			if regErr := a.register(ctx); regErr != nil {
				return fmt.Errorf("heartbeat вернул 404 и повторная регистрация не удалась: %w", regErr)
			}
			resp, err = a.client.SendHeartbeat(ctx, a.uuid.Get(), payload)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	log.Printf("Heartbeat отправлен: status=%s tasks=%d", resp.Status, len(resp.Tasks))
	for _, task := range resp.Tasks {
		if err := a.executeTask(ctx, task); err != nil {
			log.Printf("Задача id=%d type=%s завершилась с ошибкой: %v", task.ID, task.Type, err)
		}
	}
	return nil
}

func (a *Agent) executeTask(ctx context.Context, task protocol.AgentTaskDTO) error {
	wf, ok := a.workflows[task.Type]
	if !ok {
		log.Printf("Неподдерживаемая задача от сервера: id=%d type=%s", task.ID, task.Type)
		return nil
	}
	log.Printf("Выполнение задачи id=%d type=%s", task.ID, task.Type)
	return wf.Run(ctx, task.Payload)
}

func (a *Agent) registerWorkflow(w workflow) {
	a.workflows[w.Type()] = w
}

func (a *Agent) hostname() string {
	if a.cfg.HostnameOverride != "" {
		return a.cfg.HostnameOverride
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown-host"
	}
	return host
}
