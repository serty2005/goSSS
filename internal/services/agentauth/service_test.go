package agentauth

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/models"
	domainRepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	infraRepos "etalon-server/internal/infra/repositories"
	api "etalon-server/internal/transport/http/dtos"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type testRegistrar struct {
	repo domainRepos.AgentRepo
}

func (r *testRegistrar) RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error) {
	existing, err := r.repo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, domain.ErrAlreadyExists
	}

	agent := &models.Agent{
		UUID:      req.AgentUUID,
		Type:      "sssruner",
		Status:    models.StatusPendingOwner,
		Hostname:  req.Hostname,
		Version:   req.AgentVersion,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := r.repo.Create(ctx, agent); err != nil {
		return nil, err
	}
	return agent, nil
}

func TestRegisterAndIssueTokens_ПовторнаяРегистрацияИдетКакИдемпотентныйУспех(t *testing.T) {
	db, repo := setupAgentAuthDB(t)
	svc := NewService(db, logger.New("", "test", "error", true), repo, &testRegistrar{repo: repo})

	req := &api.RegistrationRequestDTO{
		AgentUUID:          "agent-phase1-success",
		Hostname:           "ws-phase1",
		AgentVersion:       "1.2.3",
		MachineFingerprint: "fingerprint-1",
		SystemInfo: map[string]any{
			"os":   "windows",
			"arch": "amd64",
		},
		InitialData: api.AgentDataDTO{
			AgentType:    "sssruner",
			Hostname:     "ws-phase1",
			AgentVersion: "1.2.3",
		},
	}
	meta := RegistrationAttemptMeta{
		RemoteAddr: "10.20.30.40:9000",
		RawPayload: []byte(`{"agent_uuid":"agent-phase1-success","hostname":"ws-phase1"}`),
	}

	firstResp, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	require.NotEmpty(t, firstResp.AccessToken)
	require.NotEmpty(t, firstResp.RefreshToken)

	secondResp, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	require.NotEmpty(t, secondResp.AccessToken)
	require.NotEqual(t, firstResp.AccessToken, secondResp.AccessToken)

	var agent models.Agent
	require.NoError(t, db.Where("uuid = ?", req.AgentUUID).First(&agent).Error)
	require.Equal(t, models.AgentRegistrationStatusSuccess, agent.LastRegistrationStatus)
	require.Equal(t, "fingerprint-1", agent.MachineFingerprint)
	require.NotNil(t, agent.LastRegistrationAt)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(agent.RegistrationPayload, &payload))
	require.Equal(t, "agent-phase1-success", payload["agent_uuid"])

	var attempts []models.AgentRegistrationAttempt
	require.NoError(t, db.Order("id asc").Find(&attempts).Error)
	require.Len(t, attempts, 2)
	require.Equal(t, models.AgentRegistrationStatusSuccess, attempts[0].Status)
	require.Equal(t, "10.20.30.40", attempts[0].RemoteAddr)
}

func TestRecordRegistrationAttempt_401СоздаетДиагностическийСледДажеДоУспешнойРегистрации(t *testing.T) {
	db, repo := setupAgentAuthDB(t)
	svc := NewService(db, logger.New("", "test", "error", true), repo, &testRegistrar{repo: repo})

	req := &api.RegistrationRequestDTO{
		AgentUUID:          "agent-phase1-unauthorized",
		Hostname:           "ws-auth",
		AgentVersion:       "1.0.0",
		MachineFingerprint: "fingerprint-auth",
		InitialData: api.AgentDataDTO{
			AgentType: "sssruner",
		},
	}

	err := svc.RecordRegistrationAttempt(
		context.Background(),
		req,
		RegistrationAttemptMeta{
			RemoteAddr: "192.168.0.77:7000",
			RawPayload: []byte(`{"agent_uuid":"agent-phase1-unauthorized"}`),
		},
		RegistrationAttemptStatusUnauthorized,
		"Неверный bootstrap API key агента",
	)
	require.NoError(t, err)

	var agent models.Agent
	require.NoError(t, db.Where("uuid = ?", req.AgentUUID).First(&agent).Error)
	require.Equal(t, models.StatusRegistrationFailed, agent.Status)
	require.Equal(t, models.AgentRegistrationStatusUnauthorized, agent.LastRegistrationStatus)
	require.Equal(t, "Неверный bootstrap API key агента", agent.LastRegistrationError)

	var attempt models.AgentRegistrationAttempt
	require.NoError(t, db.Where("agent_uuid = ?", req.AgentUUID).First(&attempt).Error)
	require.Equal(t, models.AgentRegistrationStatusUnauthorized, attempt.Status)
	require.Equal(t, "192.168.0.77", attempt.RemoteAddr)
	require.NotNil(t, attempt.ErrorText)
}

func setupAgentAuthDB(t *testing.T) (*gorm.DB, domainRepos.AgentRepo) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentSessionToken{}, &models.AgentRegistrationAttempt{}))

	repo := infraRepos.NewAgentRepo(db)
	return db, repo
}
