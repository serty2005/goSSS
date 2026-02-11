package services

import (
	"context"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupObsService(t *testing.T) (*gorm.DB, AgentObservationService) {
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
	return db, NewAgentObservationService(logger.New("", "test", "error", true), db)
}

func TestApplyObservation_UnknownServer_StagesCandidate(t *testing.T) {
	db, svc := setupObsService(t)
	payload := &api.AgentDataDTO{
		Hostname:      "ws-1",
		URLRms:        "example.com:8080",
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "123 456 789",
		LitemanagerID: "LM-1",
		CRMID:         "CRM-X",
	}
	obs, err := svc.ApplyObservation(context.Background(), "agent-1", payload)
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusStaged, obs.Status)
	require.NotNil(t, obs.CandidateID)

	var c models.Candidate
	require.NoError(t, db.First(&c, *obs.CandidateID).Error)
	require.Equal(t, models.CandidateStatusNew, c.Status)
}

func TestApplyObservation_AnydeskRebind(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws1 := workstation.Workstation{IdentityHash: strRef(identityHash("111222333", "LM-1")), Teamviewer: strRef("111222333"), Litemanager: strRef("LM-1"), OwnerID: &owner}
	ws2 := workstation.Workstation{Anydesk: strRef("AD-1"), OwnerID: &owner}
	require.NoError(t, db.Create(&ws1).Error)
	require.NoError(t, db.Create(&ws2).Error)

	_, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{
		Hostname:      "ws-1",
		URLRms:        "example.com:8080",
		CRMID:         crm,
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "111222333",
		LitemanagerID: "LM-1",
		AnydeskID:     "AD-1",
	})
	require.NoError(t, err)

	var got1, got2 workstation.Workstation
	require.NoError(t, db.First(&got1, "id = ?", ws1.ID).Error)
	require.NoError(t, db.First(&got2, "id = ?", ws2.ID).Error)
	require.NotNil(t, got1.Anydesk)
	require.Equal(t, "AD-1", *got1.Anydesk)
	require.Nil(t, got2.Anydesk)
}

func TestApplyObservation_FRRebindBetweenWorkstations(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws1 := workstation.Workstation{IdentityHash: strRef(identityHash("111", "LM-1")), Teamviewer: strRef("111"), Litemanager: strRef("LM-1"), OwnerID: &owner}
	ws2 := workstation.Workstation{IdentityHash: strRef(identityHash("222", "LM-2")), Teamviewer: strRef("222"), Litemanager: strRef("LM-2"), OwnerID: &owner}
	require.NoError(t, db.Create(&ws1).Error)
	require.NoError(t, db.Create(&ws2).Error)

	_, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{Hostname: "ws1", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 10:00:00", TeamviewerID: "111", LitemanagerID: "LM-1", SerialNumber: " SN 100 "})
	require.NoError(t, err)
	_, err = svc.ApplyObservation(context.Background(), "agent-2", &api.AgentDataDTO{Hostname: "ws2", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 11:00:00", TeamviewerID: "222", LitemanagerID: "LM-2", SerialNumber: "sn100"})
	require.NoError(t, err)

	var fr fiscal.FiscalRegister
	require.NoError(t, db.First(&fr, "fr_serial_normalized = ?", "SN100").Error)
	require.NotNil(t, fr.WorkstationID)
	require.Equal(t, ws2.ID, *fr.WorkstationID)
}

func TestApplyObservation_StaleIgnored(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	lmDate := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	ws := workstation.Workstation{IdentityHash: strRef(identityHash("111", "LM-1")), Teamviewer: strRef("111"), Litemanager: strRef("LM-1"), OwnerID: &owner, LastModifiedDate: &lmDate}
	require.NoError(t, db.Create(&ws).Error)

	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{Hostname: "ws", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 10:00:00", TeamviewerID: "111", LitemanagerID: "LM-1"})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusIgnoredStale, obs.Status)
}

func TestApplyObservation_OwnershipConflictTaskCreated(t *testing.T) {
	db, svc := setupObsService(t)
	ownerSrv := "cmp-2"
	ownerWS := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &ownerSrv, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws := workstation.Workstation{IdentityHash: strRef(identityHash("111", "LM-1")), Teamviewer: strRef("111"), Litemanager: strRef("LM-1"), OwnerID: &ownerWS}
	require.NoError(t, db.Create(&ws).Error)

	_, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{Hostname: "ws", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 10:00:00", TeamviewerID: "111", LitemanagerID: "LM-1"})
	require.NoError(t, err)

	var wsAfter workstation.Workstation
	require.NoError(t, db.First(&wsAfter, "id = ?", ws.ID).Error)
	require.NotNil(t, wsAfter.OwnerID)
	require.Equal(t, ownerWS, *wsAfter.OwnerID)

	var task models.ReconciliationTask
	require.NoError(t, db.First(&task, "task_type = ? AND entity_uuid = ?", "ownership_conflict_ws", ws.ID).Error)
	require.Equal(t, "new", task.Status)
}

func TestApplyObservation_KnownServerAppliesToExistingWorkstation(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)
	ws := workstation.Workstation{IdentityHash: strRef(identityHash("111", "LM-1")), Teamviewer: strRef("111"), Litemanager: strRef("LM-1")}
	require.NoError(t, db.Create(&ws).Error)

	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{Hostname: "ws", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 10:00:00", TeamviewerID: "111", LitemanagerID: "LM-1"})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusApplied, obs.Status)

	var wsAfter workstation.Workstation
	require.NoError(t, db.First(&wsAfter, "id = ?", ws.ID).Error)
	require.NotNil(t, wsAfter.ServerID)
	require.Equal(t, srv.ID, *wsAfter.ServerID)
	require.NotNil(t, wsAfter.OwnerID)
	require.Equal(t, owner, *wsAfter.OwnerID)
}

func strRef(v string) *string {
	return &v
}
