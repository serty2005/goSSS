package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/common"
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
		&models.AgentCOMSignatureRule{},
		&models.AgentObservation{},
		&models.Candidate{},
		&models.CandidateStatusHistory{},
		&models.CandidateWorkstationStaging{},
		&models.CandidateFiscalStaging{},
		&models.ReconciliationTask{},
		&models.OwnerChangeHistory{},
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

func TestApplyObservation_FRRebindBetweenWorkstationsWhenAgentChanged(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	agentOne := "11111111-1111-1111-1111-111111111111"
	agentTwo := "22222222-2222-2222-2222-222222222222"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws1 := workstation.Workstation{IdentityHash: strRef(identityHash("111", "LM-1")), Teamviewer: strRef("111"), Litemanager: strRef("LM-1"), OwnerID: &owner}
	ws2 := workstation.Workstation{IdentityHash: strRef(identityHash("222", "LM-2")), Teamviewer: strRef("222"), Litemanager: strRef("LM-2"), OwnerID: &owner}
	require.NoError(t, db.Create(&ws1).Error)
	require.NoError(t, db.Create(&ws2).Error)

	_, err := svc.ApplyObservation(context.Background(), agentOne, &api.AgentDataDTO{Hostname: "ws1", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 10:00:00", TeamviewerID: "111", LitemanagerID: "LM-1", SerialNumber: " SN 100 "})
	require.NoError(t, err)
	_, err = svc.ApplyObservation(context.Background(), agentTwo, &api.AgentDataDTO{Hostname: "ws2", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 11:00:00", TeamviewerID: "222", LitemanagerID: "LM-2", SerialNumber: "sn100"})
	require.NoError(t, err)

	var fr fiscal.FiscalRegister
	require.NoError(t, db.First(&fr, "fr_serial_normalized = ?", "SN100").Error)
	require.NotNil(t, fr.WorkstationID)
	require.Equal(t, ws2.ID, *fr.WorkstationID)
}

func TestApplyObservation_FRKeepsWorkstationWhenAgentSame(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	agentUUID := "33333333-3333-3333-3333-333333333333"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws1 := workstation.Workstation{Base: common.Base{LastUpdatedBy: agentUUID}, IdentityHash: strRef(identityHash("111", "LM-1")), Teamviewer: strRef("111"), Litemanager: strRef("LM-1"), OwnerID: &owner}
	ws2 := workstation.Workstation{Base: common.Base{LastUpdatedBy: agentUUID}, IdentityHash: strRef(identityHash("222", "LM-2")), Teamviewer: strRef("222"), Litemanager: strRef("LM-2"), OwnerID: &owner}
	require.NoError(t, db.Create(&ws1).Error)
	require.NoError(t, db.Create(&ws2).Error)

	_, err := svc.ApplyObservation(context.Background(), agentUUID, &api.AgentDataDTO{Hostname: "ws1", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 10:00:00", TeamviewerID: "111", LitemanagerID: "LM-1", SerialNumber: " SN 200 "})
	require.NoError(t, err)
	_, err = svc.ApplyObservation(context.Background(), agentUUID, &api.AgentDataDTO{Hostname: "ws2", URLRms: "example.com", CRMID: crm, CurrentTime: "2026-01-10 11:00:00", TeamviewerID: "222", LitemanagerID: "LM-2", SerialNumber: "sn200"})
	require.NoError(t, err)

	var fr fiscal.FiscalRegister
	require.NoError(t, db.First(&fr, "fr_serial_normalized = ?", "SN200").Error)
	require.NotNil(t, fr.WorkstationID)
	require.Equal(t, ws1.ID, *fr.WorkstationID)
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

func TestApplyObservation_OwnerTransferredToServerOwner(t *testing.T) {
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
	require.Equal(t, ownerSrv, *wsAfter.OwnerID)
	require.NotNil(t, wsAfter.ServerID)
	require.Equal(t, srv.ID, *wsAfter.ServerID)

	var taskCount int64
	require.NoError(t, db.Model(&models.ReconciliationTask{}).Where("task_type = ? AND entity_uuid = ?", "ownership_conflict_ws", ws.ID).Count(&taskCount).Error)
	require.EqualValues(t, 0, taskCount)
}

func TestApplyObservation_StaleByAgentStreamIgnored(t *testing.T) {
	db, svc := setupObsService(t)
	ownerSrv := "cmp-2"
	ownerWS := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &ownerSrv, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	lastModified := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	ws := workstation.Workstation{
		IdentityHash:     strRef(identityHash("111", "LM-1")),
		Teamviewer:       strRef("111"),
		Litemanager:      strRef("LM-1"),
		OwnerID:          &ownerWS,
		LastModifiedDate: &lastModified,
	}
	require.NoError(t, db.Create(&ws).Error)

	agentUUID := "11111111-1111-1111-1111-111111111111"
	agentLastObservedAt := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	agent := models.Agent{
		UUID:           agentUUID,
		Type:           "workstation",
		Status:         models.StatusActive,
		WorkstationID:  &ws.ID,
		LastObservedAt: &agentLastObservedAt,
	}
	require.NoError(t, db.Create(&agent).Error)

	obs, err := svc.ApplyObservation(context.Background(), agentUUID, &api.AgentDataDTO{
		Hostname:      "ws",
		URLRms:        "example.com",
		CRMID:         crm,
		CurrentTime:   "2026-01-10 11:00:00",
		TeamviewerID:  "111",
		LitemanagerID: "LM-1",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusIgnoredStale, obs.Status)

	var wsAfter workstation.Workstation
	require.NoError(t, db.First(&wsAfter, "id = ?", ws.ID).Error)
	require.NotNil(t, wsAfter.OwnerID)
	require.Equal(t, ownerWS, *wsAfter.OwnerID)
	require.Nil(t, wsAfter.ServerID)
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

func TestApplyObservation_KnownServerCreatesNewWorkstationWithoutCandidate(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{
		Hostname:      "ws-new",
		URLRms:        "example.com",
		CRMID:         crm,
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "777",
		LitemanagerID: "LM-777",
		SerialNumber:  "SN-777",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusApplied, obs.Status)
	require.Nil(t, obs.CandidateID)
	require.NotNil(t, obs.WorkstationID)

	var ws workstation.Workstation
	require.NoError(t, db.First(&ws, "id = ?", *obs.WorkstationID).Error)
	require.True(t, ws.IsNew)
	require.NotNil(t, ws.OwnerID)
	require.Equal(t, owner, *ws.OwnerID)
	require.NotNil(t, ws.ServerID)
	require.Equal(t, srv.ID, *ws.ServerID)

	var fr fiscal.FiscalRegister
	require.NoError(t, db.First(&fr, "fr_serial_normalized = ?", "SN-777").Error)
	require.NotNil(t, fr.OwnerID)
	require.Equal(t, owner, *fr.OwnerID)
	require.NotNil(t, fr.WorkstationID)
	require.Equal(t, ws.ID, *fr.WorkstationID)
}

func TestApplyObservation_DoesNotOverwriteManualWorkstationName(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws := workstation.Workstation{
		IdentityHash: strRef(identityHash("555", "LM-555")),
		Teamviewer:   strRef("555"),
		Litemanager:  strRef("LM-555"),
		OwnerID:      &owner,
		DeviceName:   strRef("Ручное имя"),
		IsNew:        false,
	}
	require.NoError(t, db.Create(&ws).Error)

	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{
		Hostname:      "auto-hostname",
		URLRms:        "example.com",
		CRMID:         crm,
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "555",
		LitemanagerID: "LM-555",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusApplied, obs.Status)

	var wsAfter workstation.Workstation
	require.NoError(t, db.First(&wsAfter, "id = ?", ws.ID).Error)
	require.NotNil(t, wsAfter.DeviceName)
	require.Equal(t, "Ручное имя", *wsAfter.DeviceName)
	require.False(t, wsAfter.IsNew)
}

func TestApplyObservation_FiscalOwnerTransferredToServerOwner(t *testing.T) {
	db, svc := setupObsService(t)
	ownerSrv := "cmp-2"
	ownerFR := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &ownerSrv, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)

	ws := workstation.Workstation{
		IdentityHash: strRef(identityHash("333", "LM-333")),
		Teamviewer:   strRef("333"),
		Litemanager:  strRef("LM-333"),
		OwnerID:      &ownerSrv,
	}
	require.NoError(t, db.Create(&ws).Error)

	sn := "SN-333"
	normalizedSN := "SN-333"
	fr := fiscal.FiscalRegister{
		OwnerID:            &ownerFR,
		WorkstationID:      &ws.ID,
		FRSerialNumber:     &sn,
		FRSerialNormalized: &normalizedSN,
	}
	require.NoError(t, db.Create(&fr).Error)

	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{
		Hostname:      "ws",
		URLRms:        "example.com",
		CRMID:         crm,
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "333",
		LitemanagerID: "LM-333",
		SerialNumber:  "SN-333",
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusApplied, obs.Status)
	require.NotNil(t, obs.FRID)
	require.Equal(t, fr.ID, *obs.FRID)

	var frAfter fiscal.FiscalRegister
	require.NoError(t, db.First(&frAfter, "id = ?", fr.ID).Error)
	require.NotNil(t, frAfter.OwnerID)
	require.Equal(t, ownerSrv, *frAfter.OwnerID)
	require.NotNil(t, frAfter.WorkstationID)
	require.Equal(t, ws.ID, *frAfter.WorkstationID)

	var taskCount int64
	require.NoError(t, db.Model(&models.ReconciliationTask{}).Where("task_type = ? AND entity_uuid = ?", "ownership_conflict_fr", fr.ID).Count(&taskCount).Error)
	require.EqualValues(t, 0, taskCount)
}

func TestApplyObservation_FiscalStoresLegacyLicensesAndAttributes(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)
	ws := workstation.Workstation{IdentityHash: strRef(identityHash("991", "LM-991")), Teamviewer: strRef("991"), Litemanager: strRef("LM-991"), OwnerID: &owner}
	require.NoError(t, db.Create(&ws).Error)

	attrExcise := "true"
	attrMarked := "false"
	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{
		Hostname:        "ws",
		URLRms:          "example.com",
		CRMID:           crm,
		CurrentTime:     "2026-01-10 10:00:00",
		TeamviewerID:    "991",
		LitemanagerID:   "LM-991",
		SerialNumber:    "SN-991",
		AttributeExcise: &attrExcise,
		AttributeMarked: &attrMarked,
		OFDName:         "ООО \"Ярус\"",
		FNExecution:     "  ФН-1.2 исполнение Ин15-4  ",
		Licenses:        api.LicensesField{Legacy: "Подписка до 3 квартала 2026 года"},
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusApplied, obs.Status)

	var fr fiscal.FiscalRegister
	require.NoError(t, db.First(&fr, "fr_serial_normalized = ?", "SN-991").Error)
	require.NotNil(t, fr.AttributeExcise)
	require.True(t, *fr.AttributeExcise)
	require.NotNil(t, fr.AttributeMarked)
	require.False(t, *fr.AttributeMarked)
	require.NotNil(t, fr.OFDName)
	require.Equal(t, "ООО \"Ярус\"", *fr.OFDName)
	require.NotNil(t, fr.FNExecution)
	require.Equal(t, "ФН-1.2 исполнение Ин15-4", *fr.FNExecution)

	var licensesValue string
	require.NoError(t, json.Unmarshal(fr.Licenses, &licensesValue))
	require.Equal(t, "2026:3", licensesValue)
}

func TestApplyObservation_FiscalStoresStructuredLicenses(t *testing.T) {
	db, svc := setupObsService(t)
	owner := "cmp-1"
	crm := "CRM-1"
	srv := server.Server{OwnerID: &owner, CRMid: &crm}
	require.NoError(t, db.Create(&srv).Error)
	ws := workstation.Workstation{IdentityHash: strRef(identityHash("992", "LM-992")), Teamviewer: strRef("992"), Litemanager: strRef("LM-992"), OwnerID: &owner}
	require.NoError(t, db.Create(&ws).Error)

	obs, err := svc.ApplyObservation(context.Background(), "agent-1", &api.AgentDataDTO{
		Hostname:      "ws",
		URLRms:        "example.com",
		CRMID:         crm,
		CurrentTime:   "2026-01-10 10:00:00",
		TeamviewerID:  "992",
		LitemanagerID: "LM-992",
		SerialNumber:  "SN-992",
		Licenses: api.LicensesField{
			Structured: map[string]api.LicenseInfo{
				"17": {DateUntil: "2038-01-19 03:14:07"},
				"19": {DateUntil: "2039-01-19 03:14:07"},
				"2":  {DateUntil: "2000-01-01 00:00:00"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, models.AgentObservationStatusApplied, obs.Status)

	var fr fiscal.FiscalRegister
	require.NoError(t, db.First(&fr, "fr_serial_normalized = ?", "SN-992").Error)

	var licensesValue string
	require.NoError(t, json.Unmarshal(fr.Licenses, &licensesValue))
	require.Equal(t, "17:2038-01-19 03:14:07;19:2039-01-19 03:14:07", licensesValue)
}

func strRef(v string) *string {
	return &v
}
