package processing_test

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/core/processing"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type noopProcessingEngine struct{}

func (n *noopProcessingEngine) ProcessAgentData(_ context.Context, _ string, _ *api.AgentDataDTO) *processing.ProcessingResult {
	return &processing.ProcessingResult{}
}

func (n *noopProcessingEngine) ProcessDuplicates(_ context.Context, _ events.DuplicatesFoundPayload) *processing.ProcessingResult {
	return &processing.ProcessingResult{}
}

func (n *noopProcessingEngine) ProcessServiceDeskUpdate(_ context.Context, _ bool, _ string, _, _ interface{}) (*processing.ProcessingResult, error) {
	return &processing.ProcessingResult{}, nil
}

func (n *noopProcessingEngine) CompareModelsForUpdate(_ string, _, _ interface{}) (map[string]interface{}, error) {
	return nil, nil
}

type flowSnapshot struct {
	observationStatus string
	hasCandidate      bool
	observations      int64
	candidates        int64
	servers           int64
	workstations      int64
	fiscals           int64
	agents            int64
}

func setupObsService(t *testing.T) (*gorm.DB, services.AgentObservationService) {
	t.Helper()
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	err = db.AutoMigrate(
		&server.Server{},
		&workstation.Workstation{},
		&fiscal.FiscalRegister{},
		&models.Agent{},
		&models.AgentObservation{},
		&models.Candidate{},
		&models.CandidateStatusHistory{},
		&models.CandidateWorkstationStaging{},
		&models.CandidateFiscalStaging{},
		&models.ReconciliationTask{},
	)
	require.NoError(t, err)
	return db, services.NewAgentObservationService(logger.New("", "test", "error", true), db)
}

func collectFlowSnapshot(t *testing.T, db *gorm.DB, dbCtx context.Context) flowSnapshot {
	t.Helper()
	conn := db.WithContext(dbCtx)

	var obs models.AgentObservation
	require.NoError(t, conn.Order("id desc").First(&obs).Error)

	count := func(model interface{}) int64 {
		var c int64
		require.NoError(t, conn.Model(model).Count(&c).Error)
		return c
	}

	return flowSnapshot{
		observationStatus: obs.Status,
		hasCandidate:      obs.CandidateID != nil,
		observations:      count(&models.AgentObservation{}),
		candidates:        count(&models.Candidate{}),
		servers:           count(&server.Server{}),
		workstations:      count(&workstation.Workstation{}),
		fiscals:           count(&fiscal.FiscalRegister{}),
		agents:            count(&models.Agent{}),
	}
}

func TestAgentDataFlow_ЧерезOrchestratorЭквивалентенПрямомуApplyObservation(t *testing.T) {
	log := logger.New("", "test", "error", true)
	payload := &api.AgentDataDTO{
		AgentUUID:     "agent-1",
		Hostname:      "ws-1",
		URLRms:        "example.com:8080",
		CRMID:         "CRM-X",
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "123456",
		LitemanagerID: "LM-1",
	}

	dbEvent, obsEventSvc := setupObsService(t)
	agentRepo := infraRepos.NewAgentRepo(dbEvent)
	bus := eventbus.NewInMemoryEventBus(64)
	agentSvc := services.NewAgentService(log, agentRepo, nil, bus)
	orchestrator := processing.NewOrchestrator(log, dbEvent, bus, nil, nil, nil, nil, nil, nil, nil, &noopProcessingEngine{}, obsEventSvc)
	orchestrator.Start(context.Background())

	eventCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go bus.Start(eventCtx, log)

	_, err := agentSvc.ProcessData(eventCtx, payload.AgentUUID, payload)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		var c int64
		_ = dbEvent.WithContext(eventCtx).Model(&models.AgentObservation{}).Count(&c).Error
		return c > 0
	}, 3*time.Second, 20*time.Millisecond)

	eventSnapshot := collectFlowSnapshot(t, dbEvent, eventCtx)

	dbDirect, obsDirectSvc := setupObsService(t)
	directRepo := infraRepos.NewAgentRepo(dbDirect)
	err = emulateLegacyProcessData(context.Background(), directRepo, obsDirectSvc, payload.AgentUUID, payload)
	require.NoError(t, err)
	directSnapshot := collectFlowSnapshot(t, dbDirect, context.Background())

	require.Equal(t, directSnapshot.observationStatus, eventSnapshot.observationStatus)
	require.Equal(t, directSnapshot.hasCandidate, eventSnapshot.hasCandidate)
	require.Equal(t, directSnapshot.observations, eventSnapshot.observations)
	require.Equal(t, directSnapshot.candidates, eventSnapshot.candidates)
	require.Equal(t, directSnapshot.servers, eventSnapshot.servers)
	require.Equal(t, directSnapshot.workstations, eventSnapshot.workstations)
	require.Equal(t, directSnapshot.fiscals, eventSnapshot.fiscals)
	require.Equal(t, directSnapshot.agents, eventSnapshot.agents)
}

func emulateLegacyProcessData(ctx context.Context, repo repositories.AgentRepo, obsSvc services.AgentObservationService, agentUUID string, data *api.AgentDataDTO) error {
	targetUUID := agentUUID
	if targetUUID == "" {
		targetUUID = data.AgentUUID
	}
	agentType := data.AgentType
	if agentType == "" {
		agentType = "workstation"
	}

	agent, err := repo.GetByUUID(ctx, targetUUID)
	if err != nil {
		return err
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
		if err := repo.Create(ctx, agent); err != nil {
			return err
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
		if err := repo.Update(ctx, agent); err != nil {
			return err
		}
	}

	_, err = obsSvc.ApplyObservation(ctx, targetUUID, data)
	return err
}
