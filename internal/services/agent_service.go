package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

var (
	ErrAgentNotFound      = domain.ErrNotFound
	ErrAgentAlreadyExists = domain.ErrAlreadyExists
	ErrOwnerNotDetermined = errors.New("не удалось определить владельца для агента")
)

type AgentService interface {
	RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error)
	ProcessData(ctx context.Context, agentUUID string, data *api.AgentDataDTO) (*api.AgentHeartbeatResponseDTO, error)
	GetAgentConfig(ctx context.Context, uuid string) (*api.AgentConfigDTO, error)
}

type agentServiceImpl struct {
	logger      logger.LoggerInterface
	agentRepo   repositories.AgentRepo
	companyRepo company.Repository
	bus         eventbus.EventBus
}

func NewAgentService(logger logger.LoggerInterface, agentRepo repositories.AgentRepo, companyRepo company.Repository, bus eventbus.EventBus) AgentService {
	return &agentServiceImpl{
		logger:      logger,
		agentRepo:   agentRepo,
		companyRepo: companyRepo,
		bus:         bus,
	}
}

func (s *agentServiceImpl) RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error) {
	existingAgent, err := s.agentRepo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		return nil, fmt.Errorf("ошибка проверки существования агента: %w", err)
	}
	if existingAgent != nil {
		return nil, ErrAgentAlreadyExists
	}

	agent := &models.Agent{
		UUID:          req.AgentUUID,
		Hostname:      req.Hostname,
		Version:       req.AgentVersion,
		LastHeartbeat: time.Now(),
		Type:          "workstation",
		Status:        models.StatusPendingOwner,
	}
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, fmt.Errorf("не удалось создать агента в БД: %w", err)
	}

	traceID := contextkeys.GetTraceID(ctx)
	if traceID == "" {
		traceID = uuid.New().String()
	}

	payload := events.AgentDataPayload{
		TraceID: traceID,
		Source:  req.AgentUUID,
		Data:    req.InitialData,
	}
	s.bus.Publish(eventbus.Event{Type: events.AgentDataReceived, Payload: payload})
	s.logger.Info("Новый агент зарегистрирован",
		"trace_id", traceID,
		"operation", "register_agent",
		"source", req.AgentUUID,
	)
	return agent, nil
}

func (s *agentServiceImpl) ProcessData(ctx context.Context, agentUUID string, data *api.AgentDataDTO) (*api.AgentHeartbeatResponseDTO, error) {
	targetUUID := agentUUID
	if targetUUID == "" {
		targetUUID = data.AgentUUID
	}

	agentType := data.AgentType
	if agentType == "" {
		agentType = "workstation"
	}

	agent, err := s.agentRepo.GetByUUID(ctx, targetUUID)
	if err != nil {
		return nil, fmt.Errorf("ошибка поиска агента: %w", err)
	}
	if agent == nil {
		observedAt := parseAgentObservedAt(data.CurrentTime)
		agent = &models.Agent{
			UUID:                    targetUUID,
			Type:                    agentType,
			Status:                  models.StatusActive,
			LastHeartbeat:           time.Now(),
			LastObservedAt:          &observedAt,
			Hostname:                data.Hostname,
			Version:                 data.AgentVersion,
			LatestInventorySnapshot: marshalAgentJSON(data.Inventory),
			LatestAdapterStatuses:   marshalAgentJSON(data.AdapterStatuses),
			CreatedAt:               time.Now(),
			UpdatedAt:               time.Now(),
		}
		if err := s.agentRepo.Create(ctx, agent); err != nil {
			return nil, fmt.Errorf("не удалось создать агента при авто-регистрации: %w", err)
		}
	} else {
		agent.LastHeartbeat = time.Now()
		observedAt := parseAgentObservedAt(data.CurrentTime)
		if agent.LastObservedAt == nil || observedAt.After(*agent.LastObservedAt) {
			agent.LastObservedAt = &observedAt
		}
		if data.AgentVersion != "" {
			agent.Version = data.AgentVersion
		}
		if data.AgentType != "" && agent.Type != data.AgentType {
			agent.Type = data.AgentType
		}
		if data.Hostname != "" {
			agent.Hostname = data.Hostname
		}
		if data.Inventory != nil {
			agent.LatestInventorySnapshot = marshalAgentJSON(data.Inventory)
		}
		if data.AdapterStatuses != nil {
			agent.LatestAdapterStatuses = marshalAgentJSON(data.AdapterStatuses)
		}
		if err := s.agentRepo.Update(ctx, agent); err != nil {
			s.logger.Error("Не удалось обновить heartbeat агента", "uuid", targetUUID, "error", err)
		}
	}

	traceID := contextkeys.GetTraceID(ctx)
	if traceID == "" {
		traceID = uuid.New().String()
	}

	s.bus.Publish(eventbus.Event{
		Type: events.AgentObservationRequested,
		Payload: events.AgentObservationPayload{
			TraceID: traceID,
			Source:  targetUUID,
			Data:    *data,
		},
	})

	response := &api.AgentHeartbeatResponseDTO{Status: "ok", Tasks: make([]api.AgentTaskDTO, 0)}
	if agentType == "sssruner" {
		manifests := make([]api.AdapterManifestDTO, 0)
		response.AdapterManifests = &manifests

		manifests, err := adapterManifestsFromConfig(agent)
		if err != nil {
			s.logger.Warn("Не удалось прочитать adapter_manifests из конфигурации агента", "uuid", targetUUID, "error", err)
		} else {
			response.AdapterManifests = &manifests
		}

		commands, err := s.agentRepo.GetPendingCommands(ctx, targetUUID)
		if err == nil && len(commands) > 0 {
			var commandIDs []uint
			for _, cmd := range commands {
				response.Tasks = append(response.Tasks, api.AgentTaskDTO{ID: cmd.ID, Type: cmd.Type, Payload: json.RawMessage(cmd.Payload), CreatedAt: cmd.CreatedAt})
				commandIDs = append(commandIDs, cmd.ID)
			}
			_ = s.agentRepo.MarkCommandsAsSent(ctx, commandIDs)
		}
	}

	return response, nil
}

func adapterManifestsFromConfig(agent *models.Agent) ([]api.AdapterManifestDTO, error) {
	if agent == nil || len(agent.Config) == 0 {
		return []api.AdapterManifestDTO{}, nil
	}

	var config api.AgentConfigDTO
	if err := json.Unmarshal(agent.Config, &config); err != nil {
		return []api.AdapterManifestDTO{}, fmt.Errorf("не удалось распарсить конфигурацию агента: %w", err)
	}
	if len(config.AdapterManifests) == 0 {
		return []api.AdapterManifestDTO{}, nil
	}

	return slices.Clone(config.AdapterManifests), nil
}

func marshalAgentJSON(value any) datatypes.JSON {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return datatypes.JSON(raw)
}

func parseAgentObservedAt(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC()
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func (s *agentServiceImpl) GetAgentConfig(ctx context.Context, uuid string) (*api.AgentConfigDTO, error) {
	agent, err := s.agentRepo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения агента: %w", err)
	}
	if agent == nil || agent.Status != models.StatusActive {
		return nil, ErrAgentNotFound
	}

	var configDTO api.AgentConfigDTO
	if agent.Config != nil {
		if err := json.Unmarshal(agent.Config, &configDTO); err != nil {
			return nil, fmt.Errorf("не удалось распарсить конфигурацию агента из БД: %w", err)
		}
	} else {
		return nil, errors.New("у активного агента отсутствует конфигурация")
	}
	return &configDTO, nil
}
