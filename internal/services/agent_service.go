package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"fmt"
	"time"

	"gorm.io/gorm"
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
	db          *gorm.DB
	bus         eventbus.EventBus
	obsService  AgentObservationService
}

func NewAgentService(logger logger.LoggerInterface, agentRepo repositories.AgentRepo, companyRepo company.Repository, db *gorm.DB, bus eventbus.EventBus, obsService AgentObservationService) AgentService {
	return &agentServiceImpl{
		logger:      logger,
		agentRepo:   agentRepo,
		companyRepo: companyRepo,
		db:          db,
		bus:         bus,
		obsService:  obsService,
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

	payload := events.AgentDataPayload{Source: req.AgentUUID, Data: req.InitialData}
	s.bus.Publish(eventbus.Event{Type: events.AgentDataReceived, Payload: payload})
	s.logger.Info("Новый агент зарегистрирован", "uuid", req.AgentUUID)
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
		agent = &models.Agent{
			UUID:          targetUUID,
			Type:          agentType,
			Status:        models.StatusActive,
			LastHeartbeat: time.Now(),
			Hostname:      data.Hostname,
			Version:       data.AgentVersion,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := s.agentRepo.Create(ctx, agent); err != nil {
			return nil, fmt.Errorf("не удалось создать агента при авто-регистрации: %w", err)
		}
	} else {
		agent.LastHeartbeat = time.Now()
		if data.AgentVersion != "" {
			agent.Version = data.AgentVersion
		}
		if data.AgentType != "" && agent.Type != data.AgentType {
			agent.Type = data.AgentType
		}
		if data.Hostname != "" {
			agent.Hostname = data.Hostname
		}
		if err := s.agentRepo.Update(ctx, agent); err != nil {
			s.logger.Error("Не удалось обновить heartbeat агента", "uuid", targetUUID, "error", err)
		}
	}

	if s.obsService != nil {
		if _, err := s.obsService.ApplyObservation(ctx, targetUUID, data); err != nil {
			s.logger.Error("Не удалось применить наблюдение агента", "uuid", targetUUID, "error", err)
		}
	}

	response := &api.AgentHeartbeatResponseDTO{Status: "ok", Tasks: make([]api.AgentTaskDTO, 0)}
	if agentType == "sssruner" {
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
