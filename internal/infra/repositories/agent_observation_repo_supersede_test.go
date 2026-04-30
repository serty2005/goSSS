package repositories

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	domainservices "etalon-server/internal/domain/services"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type testHubDetector struct{}

func (testHubDetector) IsNetworkHub(context.Context, string) (bool, error) {
	return true, nil
}

func (testHubDetector) IsNetworkHubServer(context.Context, *server.Server) (bool, error) {
	return true, nil
}

func (testHubDetector) ClearCache() {}

func setupObservationRepo(t *testing.T, opts ...AgentObservationRepoOption) (*gorm.DB, *agentObservationRepo) {
	t.Helper()

	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&company.Company{},
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
		&models.OwnerChangeHistory{},
		&models.NetworkCandidate{},
		&models.NetworkCandidateGroup{},
		&models.NetworkCandidateWSStaging{},
		&models.NetworkCandidateFRStaging{},
	))

	repo := NewAgentObservationRepo(logger.New("", "test", "error", true), db, opts...)
	return db, repo
}

func TestApplyObservation_SupersedesPreviousCandidateState(t *testing.T) {
	db, repo := setupObservationRepo(t)
	ctx := context.Background()
	agentUUID := "11111111-1111-1111-1111-111111111111"

	firstObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     agentUUID,
		Hostname:      "ws-old",
		URLRms:        "old.example.local:8080",
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "TV-OLD",
		LitemanagerID: "LM-OLD",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusStaged, firstObs.Status)
	require.NotNil(t, firstObs.CandidateID)
	firstCandidateID := *firstObs.CandidateID

	var firstTask models.ReconciliationTask
	require.NoError(t, db.Where("task_type = ? AND entity_uuid = ?", "candidate_connection", fmt.Sprintf("candidate:%d", firstCandidateID)).First(&firstTask).Error)
	require.Equal(t, "new", firstTask.Status)

	secondObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     agentUUID,
		Hostname:      "ws-new",
		URLRms:        "new.example.local:8080",
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "TV-NEW",
		LitemanagerID: "LM-NEW",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusStaged, secondObs.Status)
	require.NotNil(t, secondObs.CandidateID)
	require.NotEqual(t, firstCandidateID, *secondObs.CandidateID)

	var outdatedObservation models.AgentObservation
	require.NoError(t, db.First(&outdatedObservation, firstObs.ID).Error)
	require.Equal(t, models.AgentObservationStatusSuperseded, outdatedObservation.Status)
	require.Nil(t, outdatedObservation.CandidateID)

	var outdatedCandidate models.Candidate
	require.NoError(t, db.First(&outdatedCandidate, firstCandidateID).Error)
	require.Equal(t, models.CandidateStatusSuperseded, outdatedCandidate.Status)
	require.Equal(t, models.SystemDeactivationReasonSupersededByObservation, ptrValue(outdatedCandidate.DeactivationReason))

	var wsCount int64
	require.NoError(t, db.Model(&models.CandidateWorkstationStaging{}).Where("candidate_id = ?", firstCandidateID).Count(&wsCount).Error)
	require.Zero(t, wsCount)

	var task models.ReconciliationTask
	require.NoError(t, db.Where("task_type = ? AND entity_uuid = ?", "candidate_connection", fmt.Sprintf("candidate:%d", firstCandidateID)).First(&task).Error)
	require.Equal(t, "resolved", task.Status)
	require.Contains(t, task.Comment, "более свежим наблюдением")

	_, err = repo.RejectCandidate(ctx, CandidateRejectInput{CandidateID: firstCandidateID})
	require.Error(t, err)
	require.Contains(t, err.Error(), "не актуален")

	_, err = repo.ApproveCandidate(ctx, CandidateApproveInput{CandidateID: firstCandidateID, CompanyID: "cmp-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "не актуален")
}

func TestApplyObservation_StalePayloadKeepsActualCandidateState(t *testing.T) {
	db, repo := setupObservationRepo(t)
	ctx := context.Background()
	agentUUID := "22222222-2222-2222-2222-222222222222"

	freshObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     agentUUID,
		Hostname:      "ws-fresh",
		URLRms:        "fresh.example.local:8080",
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "TV-FRESH",
		LitemanagerID: "LM-FRESH",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusStaged, freshObs.Status)
	require.NotNil(t, freshObs.CandidateID)

	staleObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     agentUUID,
		Hostname:      "ws-stale",
		URLRms:        "stale.example.local:8080",
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "TV-STALE",
		LitemanagerID: "LM-STALE",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusIgnoredStale, staleObs.Status)

	var actualObservation models.AgentObservation
	require.NoError(t, db.First(&actualObservation, freshObs.ID).Error)
	require.Equal(t, models.AgentObservationStatusStaged, actualObservation.Status)
	require.NotNil(t, actualObservation.CandidateID)

	var actualCandidate models.Candidate
	require.NoError(t, db.First(&actualCandidate, *freshObs.CandidateID).Error)
	require.Equal(t, models.CandidateStatusNew, actualCandidate.Status)

	var wsCount int64
	require.NoError(t, db.Model(&models.CandidateWorkstationStaging{}).Where("candidate_id = ?", *freshObs.CandidateID).Count(&wsCount).Error)
	require.EqualValues(t, 1, wsCount)
}

func TestReconcileActualAgentObservations_SupersedesLegacyCandidateState(t *testing.T) {
	db, repo := setupObservationRepo(t)
	ctx := context.Background()
	agentUUID := "55555555-5555-5555-5555-555555555555"

	oldPayload := &api.AgentDataDTO{
		AgentUUID:     agentUUID,
		Hostname:      "legacy-old",
		URLRms:        "legacy.example.local:8080",
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "TV-OLD",
		LitemanagerID: "LM-OLD",
	}
	oldHash, oldPayloadJSON, err := payloadDigest(oldPayload)
	require.NoError(t, err)

	newPayload := &api.AgentDataDTO{
		AgentUUID:     agentUUID,
		Hostname:      "legacy-new",
		URLRms:        "legacy.example.local:8080",
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "TV-NEW",
		LitemanagerID: "LM-NEW",
	}
	newHash, newPayloadJSON, err := payloadDigest(newPayload)
	require.NoError(t, err)

	oldCandidate := models.Candidate{Status: models.CandidateStatusNew, ServerURL: strPtr(oldPayload.URLRms)}
	newCandidate := models.Candidate{Status: models.CandidateStatusNew, ServerURL: strPtr(newPayload.URLRms)}
	require.NoError(t, db.Create(&oldCandidate).Error)
	require.NoError(t, db.Create(&newCandidate).Error)

	oldObservedAt := parseObservedAt(oldPayload.CurrentTime)
	newObservedAt := parseObservedAt(newPayload.CurrentTime)
	oldObservation := models.AgentObservation{
		ObservationUID: uuid.NewString(),
		Source:         "legacy-feed",
		ObservedAt:     oldObservedAt,
		PayloadJSON:    oldPayloadJSON,
		PayloadHash:    oldHash,
		Status:         models.AgentObservationStatusStaged,
		CandidateID:    &oldCandidate.ID,
	}
	newObservation := models.AgentObservation{
		ObservationUID: uuid.NewString(),
		Source:         "legacy-feed",
		ObservedAt:     newObservedAt,
		PayloadJSON:    newPayloadJSON,
		PayloadHash:    newHash,
		Status:         models.AgentObservationStatusStaged,
		CandidateID:    &newCandidate.ID,
	}
	require.NoError(t, db.Create(&oldObservation).Error)
	require.NoError(t, db.Create(&newObservation).Error)

	require.NoError(t, db.Create(&models.CandidateWorkstationStaging{
		CandidateID:   oldCandidate.ID,
		ObservationID: oldObservation.ID,
		ObservedAt:    oldObservedAt,
		Hostname:      strPtr(oldPayload.Hostname),
		AgentUUID:     strPtr(agentUUID),
		TeamviewerID:  strPtr(oldPayload.TeamviewerID),
		LitemanagerID: strPtr(oldPayload.LitemanagerID),
		URLRms:        strPtr(oldPayload.URLRms),
	}).Error)
	require.NoError(t, db.Create(&models.CandidateWorkstationStaging{
		CandidateID:   newCandidate.ID,
		ObservationID: newObservation.ID,
		ObservedAt:    newObservedAt,
		Hostname:      strPtr(newPayload.Hostname),
		AgentUUID:     strPtr(agentUUID),
		TeamviewerID:  strPtr(newPayload.TeamviewerID),
		LitemanagerID: strPtr(newPayload.LitemanagerID),
		URLRms:        strPtr(newPayload.URLRms),
	}).Error)
	require.NoError(t, db.Create(&models.ReconciliationTask{
		TaskType:   "candidate_connection",
		EntityUUID: fmt.Sprintf("candidate:%d", oldCandidate.ID),
		Status:     "new",
	}).Error)

	result, err := repo.ReconcileActualAgentObservations(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.AgentsChecked)
	require.EqualValues(t, 2, result.AgentUUIDBackfilled)
	require.EqualValues(t, 1, result.ObservationsSuperseded)
	require.EqualValues(t, 1, result.CandidateRecalculation.Reprocessed)

	var outdatedObservation models.AgentObservation
	require.NoError(t, db.First(&outdatedObservation, oldObservation.ID).Error)
	require.Equal(t, models.AgentObservationStatusSuperseded, outdatedObservation.Status)
	require.Equal(t, agentUUID, ptrValue(outdatedObservation.AgentUUID))
	require.Nil(t, outdatedObservation.CandidateID)

	var actualObservation models.AgentObservation
	require.NoError(t, db.First(&actualObservation, newObservation.ID).Error)
	require.Equal(t, models.AgentObservationStatusStaged, actualObservation.Status)
	require.Equal(t, agentUUID, ptrValue(actualObservation.AgentUUID))
	require.NotNil(t, actualObservation.CandidateID)

	var outdatedCandidate models.Candidate
	require.NoError(t, db.First(&outdatedCandidate, oldCandidate.ID).Error)
	require.Equal(t, models.CandidateStatusSuperseded, outdatedCandidate.Status)
	require.Equal(t, models.SystemDeactivationReasonSupersededByObservation, ptrValue(outdatedCandidate.DeactivationReason))

	var outdatedTask models.ReconciliationTask
	require.NoError(t, db.Where("task_type = ? AND entity_uuid = ?", "candidate_connection", fmt.Sprintf("candidate:%d", oldCandidate.ID)).First(&outdatedTask).Error)
	require.Equal(t, "resolved", outdatedTask.Status)
}

func TestApplyObservation_NetworkFlowRefreshesSingleActiveGroup(t *testing.T) {
	db, repo := setupObservationRepo(t, WithHubDetector(testHubDetector{}))
	ctx := context.Background()
	hubOwnerID := "hub-company-1"
	crmID := "CRM-HUB-1"
	srv := server.Server{OwnerID: strPtr(hubOwnerID), CRMid: strPtr(crmID)}
	require.NoError(t, db.Create(&srv).Error)

	firstObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     "33333333-3333-3333-3333-333333333333",
		Hostname:      "hub-ws",
		URLRms:        "hub.example.local:8080",
		CRMID:         crmID,
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "TV-1",
		LitemanagerID: "LM-1",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusStaged, firstObs.Status)
	require.NotNil(t, firstObs.NetworkCandidateID)

	secondObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     "33333333-3333-3333-3333-333333333333",
		Hostname:      "hub-ws-updated",
		URLRms:        "hub.example.local:8080",
		CRMID:         crmID,
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "TV-2",
		LitemanagerID: "LM-2",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusStaged, secondObs.Status)
	require.NotNil(t, secondObs.NetworkCandidateID)
	require.Equal(t, *firstObs.NetworkCandidateID, *secondObs.NetworkCandidateID)

	var outdatedObservation models.AgentObservation
	require.NoError(t, db.First(&outdatedObservation, firstObs.ID).Error)
	require.Equal(t, models.AgentObservationStatusSuperseded, outdatedObservation.Status)

	var activeGroup models.NetworkCandidateGroup
	require.NoError(t, db.Where("candidate_id = ? AND status = ?", *secondObs.NetworkCandidateID, models.NetworkCandidateGroupStatusActive).First(&activeGroup).Error)
	require.Equal(t, secondObs.ID, activeGroup.ObservationID)

	var totalGroups int64
	require.NoError(t, db.Model(&models.NetworkCandidateGroup{}).Where("candidate_id = ?", *secondObs.NetworkCandidateID).Count(&totalGroups).Error)
	require.EqualValues(t, 1, totalGroups)

	var wsStage models.NetworkCandidateWSStaging
	require.NoError(t, db.Where("group_id = ?", activeGroup.ID).First(&wsStage).Error)
	require.Equal(t, "TV-2", ptrValue(wsStage.TeamviewerID))
	require.Equal(t, "LM-2", ptrValue(wsStage.LitemanagerID))
}

func TestNetworkCandidateSupersededObjectsCannotBeApprovedOrMoved(t *testing.T) {
	db, repo := setupObservationRepo(t, WithHubDetector(testHubDetector{}))
	ctx := context.Background()
	hubOwnerID := "hub-company-2"

	serverOne := server.Server{OwnerID: strPtr(hubOwnerID), CRMid: strPtr("CRM-HUB-ONE")}
	serverTwo := server.Server{OwnerID: strPtr(hubOwnerID), CRMid: strPtr("CRM-HUB-TWO")}
	require.NoError(t, db.Create(&serverOne).Error)
	require.NoError(t, db.Create(&serverTwo).Error)

	firstObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     "44444444-4444-4444-4444-444444444444",
		Hostname:      "hub-ws-one",
		URLRms:        "hub-one.example.local:8080",
		CRMID:         "CRM-HUB-ONE",
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "TV-A",
		LitemanagerID: "LM-A",
	})
	require.NoError(t, err)
	require.NotNil(t, firstObs.NetworkCandidateID)

	secondObs, err := repo.ApplyObservation(ctx, "feed-1", &api.AgentDataDTO{
		AgentUUID:     "44444444-4444-4444-4444-444444444444",
		Hostname:      "hub-ws-two",
		URLRms:        "hub-two.example.local:8080",
		CRMID:         "CRM-HUB-TWO",
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "TV-B",
		LitemanagerID: "LM-B",
	})
	require.NoError(t, err)
	require.NotNil(t, secondObs.NetworkCandidateID)
	require.NotEqual(t, *firstObs.NetworkCandidateID, *secondObs.NetworkCandidateID)

	var outdatedCandidate models.NetworkCandidate
	require.NoError(t, db.First(&outdatedCandidate, *firstObs.NetworkCandidateID).Error)
	require.Equal(t, models.NetworkCandidateStatusSuperseded, outdatedCandidate.Status)
	require.Equal(t, models.SystemDeactivationReasonSupersededByObservation, ptrValue(outdatedCandidate.DeactivationReason))

	var outdatedGroup models.NetworkCandidateGroup
	require.NoError(t, db.Where("candidate_id = ?", outdatedCandidate.ID).First(&outdatedGroup).Error)
	require.Equal(t, models.NetworkCandidateGroupStatusSuperseded, outdatedGroup.Status)
	require.Equal(t, models.SystemDeactivationReasonSupersededByObservation, ptrValue(outdatedGroup.DeactivationReason))

	networkRepo := NewNetworkCandidateRepo(db)

	_, err = networkRepo.Approve(ctx, domainrepos.NetworkCandidateApproveInput{CandidateID: outdatedCandidate.ID, ChildCompanyID: "child-1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "не актуален")

	_, err = networkRepo.RemoveGroup(ctx, outdatedCandidate.ID, outdatedGroup.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "не актуален")
}

var _ domainservices.NetworkHubDetector = testHubDetector{}
