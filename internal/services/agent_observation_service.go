package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CandidateApproveInput struct {
	CandidateID uint
	CompanyID   string
	ServerID    *string
	ServerCRMID *string
	ServerURL   *string
	ServerName  *string
	ServerDesc  *string
	Comment     *string

	CompanyTitle          *string
	CompanyAddress        *string
	CompanyAdditionalName *string
	CompanyParentID       *string

	Workstations []CandidateWorkstationInput
}

// CandidateWorkstationInput описывает имя станции, заданное оператором при подтверждении кандидата.
type CandidateWorkstationInput struct {
	StagingID       *uint
	WorkstationUUID *string
	Name            string
}

type AgentObservationService interface {
	ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error)
	ApproveCandidate(ctx context.Context, in CandidateApproveInput) (*models.Candidate, error)
}

type agentObservationServiceImpl struct {
	logger logger.LoggerInterface
	db     *gorm.DB
}

func NewAgentObservationService(logger logger.LoggerInterface, db *gorm.DB) AgentObservationService {
	return &agentObservationServiceImpl{logger: logger, db: db}
}

func (s *agentObservationServiceImpl) ApplyObservation(ctx context.Context, source string, data *api.AgentDataDTO) (*models.AgentObservation, error) {
	if data == nil {
		return nil, errors.New("пустой payload")
	}
	s.logger.Info("Начато применение данных агента",
		"source", source,
		"agent_uuid", strings.TrimSpace(data.AgentUUID),
		"hostname", strings.TrimSpace(data.Hostname),
		"crm_id", strings.TrimSpace(data.CRMID),
		"url_rms", strings.TrimSpace(data.URLRms),
		"serial_number", strings.TrimSpace(data.SerialNumber),
	)
	observedAt := parseObservedAt(data.CurrentTime)
	normalizedRMS := normalizeRMS(data.URLRms)
	serverKey := buildServerKey(normalizedRMS)
	hash, payloadJSON, err := payloadDigest(data)
	if err != nil {
		return nil, err
	}

	obs := &models.AgentObservation{}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.AgentObservation{
			Source:      source,
			ObservedAt:  observedAt,
			ServerKey:   strPtr(serverKey),
			ServerCRMID: strPtr(strings.TrimSpace(data.CRMID)),
			PayloadJSON: payloadJSON,
			PayloadHash: hash,
			Status:      models.AgentObservationStatusProcessing,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("payload_hash = ?", hash).First(obs).Error; err != nil {
			return err
		}
		s.logger.Info("Наблюдение зарегистрировано",
			"observation_id", obs.ID,
			"current_status", obs.Status,
			"server_key", ptrValue(obs.ServerKey),
			"server_crm_id", ptrValue(obs.ServerCRMID),
		)
		if obs.Status == models.AgentObservationStatusApplied || obs.Status == models.AgentObservationStatusStaged || obs.Status == models.AgentObservationStatusIgnored || obs.Status == models.AgentObservationStatusIgnoredStale {
			s.logger.Info("Повторное наблюдение пропущено",
				"observation_id", obs.ID,
				"status", obs.Status,
			)
			return nil
		}

		if isLocalRMS(normalizedRMS) {
			msg := "локальный адрес исключен"
			obs.Status = models.AgentObservationStatusIgnored
			obs.ErrorText = &msg
			s.logger.Info("Наблюдение отклонено из-за локального адреса",
				"observation_id", obs.ID,
				"normalized_rms", normalizedRMS,
			)
			return tx.Save(obs).Error
		}

		srv, err := s.findServer(tx, data.CRMID, serverKey)
		if err != nil {
			return err
		}
		if srv != nil {
			s.logger.Info("Сервер найден для наблюдения",
				"observation_id", obs.ID,
				"server_id", srv.ID,
				"server_owner_id", ptrValue(srv.OwnerID),
			)
		} else {
			s.logger.Info("Сервер не найден для наблюдения",
				"observation_id", obs.ID,
				"server_key", serverKey,
				"crm_id", strings.TrimSpace(data.CRMID),
			)
		}
		if srv == nil || !hasRemoteID(data) {
			c, err := s.stage(tx, obs, data, observedAt, normalizedRMS, serverKey)
			if err != nil {
				return err
			}
			obs.CandidateID = &c.ID
			obs.Status = models.AgentObservationStatusStaged
			s.logger.Info("Наблюдение отправлено в staging",
				"observation_id", obs.ID,
				"candidate_id", c.ID,
				"has_remote_id", hasRemoteID(data),
			)
			return tx.Save(obs).Error
		}

		ws, staleWS, err := s.applyWorkstation(tx, srv, data, observedAt, false)
		if err != nil {
			return err
		}
		if ws == nil {
			c, err := s.stage(tx, obs, data, observedAt, normalizedRMS, serverKey)
			if err != nil {
				return err
			}
			obs.CandidateID = &c.ID
			obs.Status = models.AgentObservationStatusStaged
			s.logger.Info("Наблюдение отправлено в staging: станция не сопоставлена",
				"observation_id", obs.ID,
				"candidate_id", c.ID,
			)
			return tx.Save(obs).Error
		}
		obs.WorkstationID = &ws.ID

		if err := s.upsertAgent(tx, source, data, ws.ID, observedAt); err != nil {
			return err
		}

		frApplied := false
		frStale := false
		if strings.TrimSpace(data.SerialNumber) != "" {
			fr, staleFR, err := s.applyFiscal(tx, srv, ws, data, observedAt, false)
			if err != nil {
				return err
			}
			if fr != nil {
				obs.FRID = &fr.ID
				frApplied = true
				frStale = staleFR
			}
		}

		if staleWS && (!frApplied || frStale) {
			obs.Status = models.AgentObservationStatusIgnoredStale
		} else {
			obs.Status = models.AgentObservationStatusApplied
		}
		if err := tx.Save(obs).Error; err != nil {
			return err
		}
		s.logger.Info("Наблюдение применено",
			"observation_id", obs.ID,
			"status", obs.Status,
			"workstation_id", ptrValue(obs.WorkstationID),
			"fr_id", ptrValue(obs.FRID),
			"stale_workstation", staleWS,
			"stale_fiscal", frStale,
		)
		return s.resolveConflicts(tx, obs)
	})
	if err != nil {
		s.logger.Error("ошибка применения наблюдения", "error", err, "source", source, "payload_hash", hash)
		if obs.ID != 0 {
			_ = s.db.WithContext(ctx).Model(&models.AgentObservation{}).Where("id = ?", obs.ID).Updates(map[string]interface{}{"status": models.AgentObservationStatusError, "error_text": err.Error()}).Error
		}
		return nil, err
	}
	return obs, nil
}

func (s *agentObservationServiceImpl) ApproveCandidate(ctx context.Context, in CandidateApproveInput) (*models.Candidate, error) {
	if in.CandidateID == 0 {
		return nil, errors.New("candidate_id обязателен")
	}
	s.logger.Info("Начато подтверждение кандидата",
		"candidate_id", in.CandidateID,
		"company_id", strings.TrimSpace(in.CompanyID),
		"server_id", ptrValue(in.ServerID),
	)

	var out models.Candidate
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", in.CandidateID).First(&out).Error; err != nil {
			return err
		}
		companyID, err := s.ensureCompany(tx, in)
		if err != nil {
			return err
		}
		in.CompanyID = companyID
		s.logger.Info("Подтверждение кандидата: компания определена",
			"candidate_id", in.CandidateID,
			"company_id", companyID,
		)

		srv, err := s.ensureServer(tx, &out, in)
		if err != nil {
			return err
		}
		if err := tx.Model(&server.Server{}).Where("id = ?", srv.ID).Updates(map[string]interface{}{
			"owner_id":    in.CompanyID,
			"server_key":  valOrNil(out.ServerKey),
			"crm_id":      valOrNil(in.ServerCRMID),
			"ip":          valOrNil(in.ServerURL),
			"device_name": valOrNil(in.ServerName),
			"description": valOrNil(in.ServerDesc),
		}).Error; err != nil {
			return err
		}
		s.logger.Info("Подтверждение кандидата: сервер подготовлен",
			"candidate_id", in.CandidateID,
			"server_id", srv.ID,
			"server_crm_id", ptrValue(in.ServerCRMID),
			"server_url", ptrValue(in.ServerURL),
		)

		var staged []models.AgentObservation
		if err := tx.Where("candidate_id = ?", out.ID).Order("observed_at asc").Find(&staged).Error; err != nil {
			return err
		}
		s.logger.Info("Подтверждение кандидата: найдено staged-наблюдений",
			"candidate_id", in.CandidateID,
			"staged_count", len(staged),
		)

		stagingToWS := make(map[uint]string)
		for _, so := range staged {
			var payload api.AgentDataDTO
			if err := json.Unmarshal(so.PayloadJSON, &payload); err != nil {
				continue
			}
			obsAt := so.ObservedAt
			if obsAt.IsZero() {
				obsAt = parseObservedAt(payload.CurrentTime)
			}
			ws, _, err := s.applyWorkstation(tx, srv, &payload, obsAt, true)
			if err != nil {
				return err
			}
			if ws != nil && strings.TrimSpace(payload.SerialNumber) != "" {
				if _, _, err := s.applyFiscal(tx, srv, ws, &payload, obsAt, true); err != nil {
					return err
				}
			}
			if ws != nil {
				var wsStages []models.CandidateWorkstationStaging
				if err := tx.Where("candidate_id = ? AND observation_id = ?", out.ID, so.ID).Find(&wsStages).Error; err != nil {
					return err
				}
				for _, wsStage := range wsStages {
					stagingToWS[wsStage.ID] = ws.ID
				}
			}
			if err := tx.Model(&models.AgentObservation{}).Where("id = ?", so.ID).Updates(map[string]interface{}{"status": models.AgentObservationStatusApplied, "candidate_id": nil}).Error; err != nil {
				return err
			}
			s.logger.Info("Подтверждение кандидата: staged-наблюдение применено",
				"candidate_id", in.CandidateID,
				"observation_id", so.ID,
			)
		}
		if err := s.renameApprovedWorkstations(tx, out.ID, in.Workstations, stagingToWS); err != nil {
			return err
		}

		from := out.Status
		if err := tx.Model(&out).Updates(map[string]interface{}{
			"status":              models.CandidateStatusApproved,
			"approved_company_id": in.CompanyID,
			"approved_server_id":  srv.ID,
		}).Error; err != nil {
			return err
		}
		reason := "подтверждено оператором"
		if msg := strings.TrimSpace(ptrValue(in.Comment)); msg != "" {
			reason = fmt.Sprintf("%s: %s", reason, msg)
		}
		if err := tx.Create(&models.CandidateStatusHistory{CandidateID: out.ID, FromStatus: strPtr(from), ToStatus: models.CandidateStatusApproved, Reason: &reason}).Error; err != nil {
			return err
		}
		return tx.Model(&models.ReconciliationTask{}).
			Where("task_type = ? AND entity_uuid = ? AND status IN ?", "candidate_connection", fmt.Sprintf("candidate:%d", out.ID), []string{"new", "pending_sd_action", "sd_error"}).
			Updates(map[string]interface{}{"status": "resolved", "comment": "Кандидат подтвержден"}).Error
	})
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Where("id = ?", in.CandidateID).First(&out).Error; err != nil {
		return nil, err
	}
	s.logger.Info("Подтверждение кандидата завершено",
		"candidate_id", out.ID,
		"status", out.Status,
		"approved_company_id", ptrValue(out.ApprovedCompanyID),
		"approved_server_id", ptrValue(out.ApprovedServerID),
	)
	return &out, nil
}

// ensureCompany гарантирует наличие компании для подтверждения кандидата.
func (s *agentObservationServiceImpl) ensureCompany(tx *gorm.DB, in CandidateApproveInput) (string, error) {
	if strings.TrimSpace(in.CompanyID) != "" {
		var existing company.Company
		if err := tx.Where("id = ?", strings.TrimSpace(in.CompanyID)).First(&existing).Error; err != nil {
			return "", err
		}
		return existing.ID, nil
	}
	if strings.TrimSpace(ptrValue(in.CompanyTitle)) == "" {
		return "", errors.New("необходимо указать company_id или company.title")
	}
	newCompany := company.Company{
		Title:          strPtr(ptrValue(in.CompanyTitle)),
		Address:        strPtr(ptrValue(in.CompanyAddress)),
		AdditionalName: strPtr(ptrValue(in.CompanyAdditionalName)),
		ParentID:       strPtr(ptrValue(in.CompanyParentID)),
	}
	if err := tx.Create(&newCompany).Error; err != nil {
		return "", err
	}
	return newCompany.ID, nil
}

// renameApprovedWorkstations обновляет имена станций, заданные оператором на форме подтверждения.
func (s *agentObservationServiceImpl) renameApprovedWorkstations(tx *gorm.DB, candidateID uint, rows []CandidateWorkstationInput, stagingToWS map[uint]string) error {
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		var targetID string
		if row.StagingID != nil {
			if mapped, ok := stagingToWS[*row.StagingID]; ok {
				targetID = mapped
			}
			if targetID == "" {
				var stage models.CandidateWorkstationStaging
				if err := tx.Where("id = ? AND candidate_id = ?", *row.StagingID, candidateID).First(&stage).Error; err == nil {
					if stage.WorkstationUUID != nil {
						targetID = strings.TrimSpace(*stage.WorkstationUUID)
					}
				}
			}
		}
		if targetID == "" && row.WorkstationUUID != nil {
			targetID = strings.TrimSpace(*row.WorkstationUUID)
		}
		if targetID == "" {
			continue
		}
		if err := tx.Model(&workstation.Workstation{}).Where("id = ?", targetID).Update("device_name", name).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *agentObservationServiceImpl) ensureServer(tx *gorm.DB, c *models.Candidate, in CandidateApproveInput) (*server.Server, error) {
	var srv server.Server
	if in.ServerID != nil && strings.TrimSpace(*in.ServerID) != "" {
		if err := tx.Where("id = ?", *in.ServerID).First(&srv).Error; err != nil {
			return nil, err
		}
		return &srv, nil
	}
	if in.ServerCRMID != nil && strings.TrimSpace(*in.ServerCRMID) != "" {
		if err := tx.Where("crm_id = ?", strings.TrimSpace(*in.ServerCRMID)).First(&srv).Error; err == nil {
			return &srv, nil
		}
	}
	if c.ServerKey != nil {
		if err := tx.Where("server_key = ?", *c.ServerKey).First(&srv).Error; err == nil {
			return &srv, nil
		}
	}
	srv = server.Server{
		OwnerID:     &in.CompanyID,
		CRMid:       in.ServerCRMID,
		IP:          in.ServerURL,
		DeviceName:  in.ServerName,
		Description: in.ServerDesc,
		ServerKey:   c.ServerKey,
	}
	if err := tx.Create(&srv).Error; err != nil {
		return nil, err
	}
	return &srv, nil
}

func (s *agentObservationServiceImpl) findServer(tx *gorm.DB, crmID, serverKey string) (*server.Server, error) {
	var srv server.Server
	crmID = strings.TrimSpace(crmID)
	if crmID != "" {
		err := tx.Where("crm_id = ?", crmID).First(&srv).Error
		if err == nil {
			return &srv, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if strings.TrimSpace(serverKey) != "" {
		err := tx.Where("server_key = ?", serverKey).First(&srv).Error
		if err == nil {
			return &srv, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	return nil, nil
}

func (s *agentObservationServiceImpl) applyWorkstation(tx *gorm.DB, srv *server.Server, data *api.AgentDataDTO, observedAt time.Time, forceOwner bool) (*workstation.Workstation, bool, error) {
	identity := identityHash(data.TeamviewerID, data.LitemanagerID)
	ws, err := s.findWorkstation(tx, data, identity)
	if err != nil {
		return nil, false, err
	}
	if ws == nil && !forceOwner {
		s.logger.Info("Станция не найдена по идентификаторам, перенос в staging",
			"hostname", strings.TrimSpace(data.Hostname),
			"server_id", srv.ID,
			"teamviewer_id", normRID(data.TeamviewerID),
			"litemanager_id", normRID(data.LitemanagerID),
			"anydesk_id", normRID(data.AnydeskID),
		)
		return nil, false, nil
	}
	if ws == nil {
		ws = &workstation.Workstation{}
	}
	stale := ws.LastModifiedDate != nil && observedAt.Before(*ws.LastModifiedDate)
	if stale {
		s.logger.Info("Рабочая станция не обновлена: получены устаревшие данные",
			"workstation_id", ws.ID,
			"incoming_observed_at", observedAt,
			"current_last_modified_at", ws.LastModifiedDate,
		)
		return ws, true, nil
	}

	if ws.ID == "" {
		ws.OwnerID = srv.OwnerID
		ws.ServerID = &srv.ID
		ws.DeviceName = strPtr(strings.TrimSpace(data.Hostname))
		ws.Teamviewer = normRIDPtr(data.TeamviewerID)
		ws.Litemanager = normRIDPtr(data.LitemanagerID)
		ws.Anydesk = normRIDPtr(data.AnydeskID)
		ws.IdentityHash = strPtr(identity)
		ws.LastModifiedDate = &observedAt
		if err := tx.Create(ws).Error; err != nil {
			return nil, false, err
		}
		s.logger.Info("Создана новая рабочая станция",
			"workstation_id", ws.ID,
			"server_id", srv.ID,
			"owner_id", ptrValue(ws.OwnerID),
			"hostname", ptrValue(ws.DeviceName),
		)
	} else {
		updates := map[string]interface{}{
			"server_id":          srv.ID,
			"last_modified_date": observedAt,
			"device_name":        valOrNil(strPtr(strings.TrimSpace(data.Hostname))),
			"identity_hash":      valOrNil(strPtr(identity)),
		}
		if forceOwner {
			updates["owner_id"] = valOrNil(srv.OwnerID)
		} else if ws.OwnerID == nil && srv.OwnerID != nil {
			updates["owner_id"] = *srv.OwnerID
		} else if ws.OwnerID != nil && srv.OwnerID != nil && *ws.OwnerID != *srv.OwnerID {
			_ = s.createOrRefreshTask(tx, "ownership_conflict_ws", ws.ID, "Конфликт владельца рабочей станции", map[string]interface{}{"workstation_id": ws.ID, "current_owner": *ws.OwnerID, "incoming_owner": *srv.OwnerID})
		}
		if tv := normRIDPtr(data.TeamviewerID); tv != nil {
			updates["teamviewer"] = *tv
		}
		if lm := normRIDPtr(data.LitemanagerID); lm != nil {
			updates["litemanager"] = *lm
		}
		if err := tx.Model(&workstation.Workstation{}).Where("id = ?", ws.ID).Updates(updates).Error; err != nil {
			return nil, false, err
		}
		s.logger.Info("Обновлена рабочая станция",
			"workstation_id", ws.ID,
			"server_id", srv.ID,
			"force_owner", forceOwner,
		)
	}

	if ad := normRIDPtr(data.AnydeskID); ad != nil {
		res := tx.Model(&workstation.Workstation{}).Where("anydesk = ? AND id <> ?", *ad, ws.ID).Update("anydesk", nil)
		if res.Error != nil {
			return nil, false, res.Error
		}
		if err := tx.Model(&workstation.Workstation{}).Where("id = ?", ws.ID).Update("anydesk", *ad).Error; err != nil {
			return nil, false, err
		}
		s.logger.Info("Обновлен AnyDesk-алиас рабочей станции",
			"workstation_id", ws.ID,
			"anydesk_id", *ad,
			"detached_workstations", res.RowsAffected,
		)
	}
	if err := tx.Where("id = ?", ws.ID).First(ws).Error; err != nil {
		return nil, false, err
	}
	return ws, false, nil
}

func (s *agentObservationServiceImpl) applyFiscal(tx *gorm.DB, srv *server.Server, ws *workstation.Workstation, data *api.AgentDataDTO, observedAt time.Time, forceOwner bool) (*fiscal.FiscalRegister, bool, error) {
	sn := normalizeSerial(data.SerialNumber)
	if sn == "" {
		return nil, false, nil
	}
	var fr fiscal.FiscalRegister
	err := tx.Where("fr_serial_normalized = ?", sn).First(&fr).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fr = fiscal.FiscalRegister{
			OwnerID:            srv.OwnerID,
			WorkstationID:      &ws.ID,
			FRSerialNumber:     strPtr(strings.TrimSpace(data.SerialNumber)),
			FRSerialNormalized: &sn,
			ModelKKT:           strPtr(strings.TrimSpace(data.ModelName)),
			RNKKT:              strPtr(strings.TrimSpace(data.RNM)),
			INN:                strPtr(strings.TrimSpace(data.INN)),
			FNNumber:           strPtr(strings.TrimSpace(data.FNSerial)),
			LegalName:          strPtr(strings.TrimSpace(data.OrganizationName)),
			Address:            strPtr(strings.TrimSpace(data.Address)),
			FRDownloader:       strPtr(strings.TrimSpace(data.FNExecution)),
			DriverVersion:      strPtr(strings.TrimSpace(data.InstalledDriver)),
			FRFirmware:         strPtr(strings.TrimSpace(data.BootVersion)),
			LastModifiedDate:   &observedAt,
		}
		if t := parseDate(data.DateTimeEnd); t != nil {
			fr.FNExpireDate = t
		}
		if t := parseDate(data.DateTimeReg); t != nil {
			fr.KKTRegDate = t
		}
		if err := tx.Create(&fr).Error; err != nil {
			return nil, false, err
		}
		s.logger.Info("Создан новый фискальный регистратор",
			"fr_id", fr.ID,
			"serial_normalized", sn,
			"workstation_id", ws.ID,
			"owner_id", ptrValue(fr.OwnerID),
		)
		return &fr, false, nil
	}
	stale := fr.LastModifiedDate != nil && observedAt.Before(*fr.LastModifiedDate)
	if stale {
		s.logger.Info("Фискальный регистратор не обновлен: получены устаревшие данные",
			"fr_id", fr.ID,
			"incoming_observed_at", observedAt,
			"current_last_modified_at", fr.LastModifiedDate,
		)
		return &fr, true, nil
	}
	updates := map[string]interface{}{
		"workstation_id":       ws.ID,
		"fr_serial_number":     strings.TrimSpace(data.SerialNumber),
		"fr_serial_normalized": sn,
		"model_kkt":            valOrNil(strPtr(strings.TrimSpace(data.ModelName))),
		"rn_kkt":               valOrNil(strPtr(strings.TrimSpace(data.RNM))),
		"inn":                  valOrNil(strPtr(strings.TrimSpace(data.INN))),
		"fn_number":            valOrNil(strPtr(strings.TrimSpace(data.FNSerial))),
		"legal_name":           valOrNil(strPtr(strings.TrimSpace(data.OrganizationName))),
		"address":              valOrNil(strPtr(strings.TrimSpace(data.Address))),
		"fr_downloader":        valOrNil(strPtr(strings.TrimSpace(data.FNExecution))),
		"driver_version":       valOrNil(strPtr(strings.TrimSpace(data.InstalledDriver))),
		"fr_firmware":          valOrNil(strPtr(strings.TrimSpace(data.BootVersion))),
		"last_modified_date":   observedAt,
	}
	if t := parseDate(data.DateTimeEnd); t != nil {
		updates["fn_expire_date"] = *t
	}
	if t := parseDate(data.DateTimeReg); t != nil {
		updates["kkt_reg_date"] = *t
	}
	if forceOwner {
		updates["owner_id"] = valOrNil(srv.OwnerID)
	} else if fr.OwnerID == nil && srv.OwnerID != nil {
		updates["owner_id"] = *srv.OwnerID
	} else if fr.OwnerID != nil && srv.OwnerID != nil && *fr.OwnerID != *srv.OwnerID {
		_ = s.createOrRefreshTask(tx, "ownership_conflict_fr", fr.ID, "Конфликт владельца ФР", map[string]interface{}{"fr_id": fr.ID, "current_owner": *fr.OwnerID, "incoming_owner": *srv.OwnerID})
	}
	if err := tx.Model(&fiscal.FiscalRegister{}).Where("id = ?", fr.ID).Updates(updates).Error; err != nil {
		return nil, false, err
	}
	s.logger.Info("Обновлен фискальный регистратор",
		"fr_id", fr.ID,
		"serial_normalized", sn,
		"workstation_id", ws.ID,
		"force_owner", forceOwner,
	)
	if err := tx.Where("id = ?", fr.ID).First(&fr).Error; err != nil {
		return nil, false, err
	}
	return &fr, false, nil
}

func (s *agentObservationServiceImpl) findWorkstation(tx *gorm.DB, data *api.AgentDataDTO, identityHashValue string) (*workstation.Workstation, error) {
	var ws workstation.Workstation
	if identityHashValue != "" {
		if err := tx.Where("identity_hash = ?", identityHashValue).First(&ws).Error; err == nil {
			return &ws, nil
		}
	}
	if tv := normRID(data.TeamviewerID); tv != "" {
		if err := tx.Where("teamviewer = ?", tv).First(&ws).Error; err == nil {
			return &ws, nil
		}
	}
	if lm := normRID(data.LitemanagerID); lm != "" {
		if err := tx.Where("litemanager = ?", lm).First(&ws).Error; err == nil {
			return &ws, nil
		}
	}
	if ad := normRID(data.AnydeskID); ad != "" {
		if err := tx.Where("anydesk = ?", ad).First(&ws).Error; err == nil {
			return &ws, nil
		}
	}
	return nil, nil
}

func (s *agentObservationServiceImpl) stage(tx *gorm.DB, obs *models.AgentObservation, data *api.AgentDataDTO, observedAt time.Time, normalizedRMS, serverKey string) (*models.Candidate, error) {
	c, err := s.findOrCreateCandidate(tx, data.CRMID, serverKey, normalizedRMS)
	if err != nil {
		return nil, err
	}
	wsUUID := workstationUUIDByRemote(data.TeamviewerID, data.LitemanagerID)
	if err := tx.Create(&models.CandidateWorkstationStaging{CandidateID: c.ID, ObservationID: obs.ID, ObservedAt: observedAt, Hostname: strPtr(strings.TrimSpace(data.Hostname)), AgentUUID: strPtr(strings.TrimSpace(data.AgentUUID)), WorkstationUUID: strPtr(wsUUID), TeamviewerID: normRIDPtr(data.TeamviewerID), LitemanagerID: normRIDPtr(data.LitemanagerID), AnydeskID: normRIDPtr(data.AnydeskID), URLRms: strPtr(normalizedRMS)}).Error; err != nil {
		return nil, err
	}
	if sn := strings.TrimSpace(data.SerialNumber); sn != "" {
		if err := tx.Create(&models.CandidateFiscalStaging{CandidateID: c.ID, ObservationID: obs.ID, ObservedAt: observedAt, SerialNumber: strPtr(sn), SerialNormalized: strPtr(normalizeSerial(sn)), RNKKT: strPtr(strings.TrimSpace(data.RNM)), ModelName: strPtr(strings.TrimSpace(data.ModelName)), INN: strPtr(strings.TrimSpace(data.INN)), FNNumber: strPtr(strings.TrimSpace(data.FNSerial)), FNExpireDate: parseDate(data.DateTimeEnd), OrganizationName: strPtr(strings.TrimSpace(data.OrganizationName)), Address: strPtr(strings.TrimSpace(data.Address))}).Error; err != nil {
			return nil, err
		}
	}
	_ = s.createOrRefreshTask(tx, "candidate_connection", fmt.Sprintf("candidate:%d", c.ID), "Кандидат на подключение ТП", map[string]interface{}{"candidate_id": c.ID, "server_key": c.ServerKey, "server_crm_id": c.ServerCRMID})
	s.logger.Info("Создан/обновлен staging по кандидату",
		"candidate_id", c.ID,
		"observation_id", obs.ID,
		"server_key", serverKey,
		"server_crm_id", strings.TrimSpace(data.CRMID),
	)
	return c, nil
}

func (s *agentObservationServiceImpl) findOrCreateCandidate(tx *gorm.DB, crmID, serverKey, rms string) (*models.Candidate, error) {
	var c models.Candidate
	crmID = strings.TrimSpace(crmID)
	if crmID != "" {
		if err := tx.Where("server_crm_id = ? AND status <> ?", crmID, models.CandidateStatusApproved).Order("id desc").First(&c).Error; err == nil {
			s.logger.Info("Найден существующий кандидат по CRM ID",
				"candidate_id", c.ID,
				"server_crm_id", crmID,
			)
			return &c, nil
		}
	}
	if strings.TrimSpace(serverKey) != "" {
		if err := tx.Where("server_key = ? AND status <> ?", serverKey, models.CandidateStatusApproved).Order("id desc").First(&c).Error; err == nil {
			s.logger.Info("Найден существующий кандидат по server_key",
				"candidate_id", c.ID,
				"server_key", serverKey,
			)
			return &c, nil
		}
	}
	meta, _ := json.Marshal(map[string]interface{}{"server_url": rms})
	c = models.Candidate{ServerKey: strPtr(serverKey), ServerCRMID: strPtr(crmID), ServerURL: strPtr(rms), Status: models.CandidateStatusNew, Meta: datatypes.JSON(meta)}
	if err := tx.Create(&c).Error; err != nil {
		return nil, err
	}
	reason := "создан автоматически по данным агента"
	_ = tx.Create(&models.CandidateStatusHistory{CandidateID: c.ID, ToStatus: models.CandidateStatusNew, Reason: &reason}).Error
	s.logger.Info("Создан новый кандидат",
		"candidate_id", c.ID,
		"server_key", serverKey,
		"server_crm_id", crmID,
	)
	return &c, nil
}

func (s *agentObservationServiceImpl) upsertAgent(tx *gorm.DB, source string, data *api.AgentDataDTO, wsID string, observedAt time.Time) error {
	agentUUID := strings.TrimSpace(data.AgentUUID)
	if agentUUID == "" && isUUID(source) {
		agentUUID = source
	}
	if agentUUID == "" {
		s.logger.Info("Обновление agent_instance пропущено: не задан agent_uuid", "source", source, "workstation_id", wsID)
		return nil
	}
	var agent models.Agent
	err := tx.Where("uuid = ?", agentUUID).First(&agent).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		agent = models.Agent{UUID: agentUUID, Type: defaultStr(strings.TrimSpace(data.AgentType), "workstation"), Status: models.StatusActive, Hostname: strings.TrimSpace(data.Hostname), Version: strings.TrimSpace(data.AgentVersion), WorkstationID: &wsID, LastHeartbeat: time.Now(), LastObservedAt: &observedAt}
		if err := tx.Create(&agent).Error; err != nil {
			return err
		}
		s.logger.Info("Создан agent_instance", "agent_uuid", agentUUID, "workstation_id", wsID)
		return nil
	}
	updates := map[string]interface{}{"workstation_id": wsID, "last_heartbeat": time.Now()}
	if data.AgentVersion != "" {
		updates["version"] = strings.TrimSpace(data.AgentVersion)
	}
	if data.Hostname != "" {
		updates["hostname"] = strings.TrimSpace(data.Hostname)
	}
	if data.AgentType != "" {
		updates["type"] = strings.TrimSpace(data.AgentType)
	}
	if agent.LastObservedAt == nil || observedAt.After(*agent.LastObservedAt) {
		updates["last_observed_at"] = observedAt
	}
	if err := tx.Model(&models.Agent{}).Where("uuid = ?", agentUUID).Updates(updates).Error; err != nil {
		return err
	}
	s.logger.Info("Обновлен agent_instance", "agent_uuid", agentUUID, "workstation_id", wsID)
	return nil
}

func (s *agentObservationServiceImpl) createOrRefreshTask(tx *gorm.DB, taskType, entityUUID, comment string, details map[string]interface{}) error {
	var existing models.ReconciliationTask
	err := tx.Where("task_type = ? AND entity_uuid = ? AND status IN ?", taskType, entityUUID, []string{"new", "pending_sd_action", "sd_error"}).Order("id desc").First(&existing).Error
	payload, _ := json.Marshal(details)
	if err == nil {
		return tx.Model(&models.ReconciliationTask{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{"details": datatypes.JSON(payload), "comment": comment}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Create(&models.ReconciliationTask{TaskType: taskType, EntityType: "AgentObservation", EntityUUID: entityUUID, Details: datatypes.JSON(payload), Status: "new", Comment: comment}).Error
}

func (s *agentObservationServiceImpl) resolveConflicts(tx *gorm.DB, obs *models.AgentObservation) error {
	_ = tx
	_ = obs
	return nil
}

func parseObservedAt(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Now().UTC()
	}
	layouts := []string{"2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

func parseDate(v string) *time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	layouts := []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339, "02.01.2006", "02.01.2006 15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func normalizeRMS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	withSchema := raw
	if !strings.Contains(withSchema, "://") {
		withSchema = "http://" + withSchema
	}
	parsed, err := url.Parse(withSchema)
	if err != nil {
		return strings.ToLower(raw)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return strings.ToLower(raw)
	}
	port := strings.TrimSpace(parsed.Port())
	if port == "" {
		port = "8080"
	}
	return host + ":" + port
}

func isLocalRMS(rms string) bool {
	host := strings.TrimSpace(strings.ToLower(rms))
	if strings.Contains(host, ":") {
		if u, err := url.Parse("http://" + host); err == nil {
			host = u.Hostname()
		}
	}
	if host == "" || host == "localhost" || host == "127.0.0.1" || strings.HasPrefix(host, "127.") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	cidrs := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8"}
	for _, c := range cidrs {
		_, block, _ := net.ParseCIDR(c)
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func buildServerKey(rms string) string {
	rms = strings.TrimSpace(strings.ToLower(rms))
	if rms == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(rms)).String()
}

func normalizeSerial(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	return strings.ReplaceAll(v, " ", "")
}

func normRID(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return ""
	}
	return strings.ReplaceAll(v, " ", "")
}

func normRIDPtr(v string) *string {
	n := normRID(v)
	if n == "" {
		return nil
	}
	return &n
}

func hasRemoteID(data *api.AgentDataDTO) bool {
	return normRID(data.TeamviewerID) != "" || normRID(data.LitemanagerID) != "" || normRID(data.AnydeskID) != ""
}

func workstationUUIDByRemote(tv, lm string) string {
	tv = normRID(tv)
	lm = normRID(lm)
	if tv == "" || lm == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(tv+":"+lm)).String()
}

func identityHash(tv, lm string) string {
	tv = normRID(tv)
	lm = normRID(lm)
	if tv == "" || lm == "" {
		return ""
	}
	s := sha256.Sum256([]byte(tv + ":" + lm))
	return hex.EncodeToString(s[:])
}

func payloadDigest(data *api.AgentDataDTO) (string, datatypes.JSON, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", nil, err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), datatypes.JSON(b), nil
}

func isUUID(v string) bool {
	_, err := uuid.Parse(strings.TrimSpace(v))
	return err == nil
}

func defaultStr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func strPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func valOrNil(v *string) interface{} {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return strings.TrimSpace(*v)
}

// ptrValue безопасно возвращает значение строкового указателя.
func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
