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
	"github.com/google/uuid"
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
		Status:    models.StatusPendingRegistration,
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

func TestRegisterAndIssueTokens_ДоПодтвержденияВозвращаетPendingApprovalИНеСоздаетТокены(t *testing.T) {
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
	require.Equal(t, models.AgentRegistrationStatusPendingApproval, firstResp.Status)
	require.Equal(t, registrationPendingApprovalText, firstResp.Message)
	require.Empty(t, firstResp.AccessToken)
	require.Empty(t, firstResp.RefreshToken)

	var agent models.Agent
	require.NoError(t, db.Where("uuid = ?", req.AgentUUID).First(&agent).Error)
	require.Equal(t, models.StatusPendingRegistration, agent.Status)
	require.Equal(t, models.AgentRegistrationStatusPendingApproval, agent.LastRegistrationStatus)
	require.Equal(t, registrationPendingApprovalText, agent.LastRegistrationError)
	require.Equal(t, "fingerprint-1", agent.MachineFingerprint)
	require.NotNil(t, agent.LastRegistrationAt)
	require.Nil(t, agent.RegistrationApprovedAt)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(agent.RegistrationPayload, &payload))
	require.Equal(t, "agent-phase1-success", payload["agent_uuid"])

	var attempts []models.AgentRegistrationAttempt
	require.NoError(t, db.Order("id asc").Find(&attempts).Error)
	require.Len(t, attempts, 1)
	require.Equal(t, models.AgentRegistrationStatusPendingApproval, attempts[0].Status)
	require.Equal(t, "10.20.30.40", attempts[0].RemoteAddr)

	var tokenCount int64
	require.NoError(t, db.Model(&models.AgentSessionToken{}).Count(&tokenCount).Error)
	require.Zero(t, tokenCount)
}

func TestRegisterAndIssueTokens_ПослеПодтвержденияВыдаетТокеныИОстаетсяИдемпотентным(t *testing.T) {
	db, repo := setupAgentAuthDB(t)
	svc := NewService(db, logger.New("", "test", "error", true), repo, &testRegistrar{repo: repo})

	req := &api.RegistrationRequestDTO{
		AgentUUID:          "agent-phase1-approved",
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
		RawPayload: []byte(`{"agent_uuid":"agent-phase1-approved","hostname":"ws-phase1"}`),
	}

	firstResp, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	require.Equal(t, models.AgentRegistrationStatusPendingApproval, firstResp.Status)

	approvedAt := time.Date(2026, 3, 21, 11, 0, 0, 0, time.UTC)
	require.NoError(t, db.Model(&models.Agent{}).
		Where("uuid = ?", req.AgentUUID).
		Updates(map[string]any{
			"registration_approved_at": approvedAt,
			"registration_approved_by": "user-1",
		}).Error)

	secondResp, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	require.Equal(t, "ok", secondResp.Status)
	require.NotEmpty(t, secondResp.AccessToken)
	require.NotEmpty(t, secondResp.RefreshToken)

	thirdResp, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	require.Equal(t, "ok", thirdResp.Status)
	require.NotEmpty(t, thirdResp.AccessToken)
	require.NotEqual(t, secondResp.AccessToken, thirdResp.AccessToken)

	var agent models.Agent
	require.NoError(t, db.Where("uuid = ?", req.AgentUUID).First(&agent).Error)
	require.Equal(t, models.AgentRegistrationStatusSuccess, agent.LastRegistrationStatus)
	require.Empty(t, agent.LastRegistrationError)
	require.Equal(t, models.StatusPendingOwner, agent.Status)
	require.NotNil(t, agent.RegistrationApprovedAt)
	require.Equal(t, approvedAt.UTC(), agent.RegistrationApprovedAt.UTC())
	require.Equal(t, "user-1", agent.RegistrationApprovedBy)

	var attempts []models.AgentRegistrationAttempt
	require.NoError(t, db.Order("id asc").Find(&attempts).Error)
	require.Len(t, attempts, 3)
	require.Equal(t, models.AgentRegistrationStatusPendingApproval, attempts[0].Status)
	require.Equal(t, models.AgentRegistrationStatusSuccess, attempts[1].Status)
	require.Equal(t, models.AgentRegistrationStatusSuccess, attempts[2].Status)

	var tokenCount int64
	require.NoError(t, db.Model(&models.AgentSessionToken{}).Count(&tokenCount).Error)
	require.EqualValues(t, 4, tokenCount)
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

	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentSessionToken{}, &models.AgentRegistrationAttempt{}))

	repo := infraRepos.NewAgentRepo(db)
	return db, repo
}
