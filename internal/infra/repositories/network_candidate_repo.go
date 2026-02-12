package repositories

import (
	"context"
	"crypto/sha256"
	"errors"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type networkCandidateRepo struct {
	db *gorm.DB
}

func NewNetworkCandidateRepo(db *gorm.DB) domainrepos.NetworkCandidateRepo {
	return &networkCandidateRepo{db: db}
}

func (r *networkCandidateRepo) List(ctx context.Context, status string, limit, offset int) ([]models.NetworkCandidate, error) {
	query := r.db.WithContext(ctx).Model(&models.NetworkCandidate{})
	switch status {
	case "ACTIVE":
		query = query.Where("status IN ?", []string{models.NetworkCandidateStatusNew, models.NetworkCandidateStatusInReview})
	case "ALL":
	default:
		if strings.TrimSpace(status) != "" {
			query = query.Where("status = ?", strings.TrimSpace(status))
		}
	}
	var items []models.NetworkCandidate
	err := query.Order("updated_at desc").Limit(limit).Offset(offset).Find(&items).Error
	return items, err
}

func (r *networkCandidateRepo) GetByID(ctx context.Context, id uint) (*domainrepos.NetworkCandidateDetails, error) {
	var candidate models.NetworkCandidate
	if err := r.db.WithContext(ctx).First(&candidate, id).Error; err != nil {
		return nil, err
	}

	var groups []models.NetworkCandidateGroup
	if err := r.db.WithContext(ctx).
		Where("candidate_id = ? AND status = ?", id, models.NetworkCandidateGroupStatusActive).
		Order("id asc").
		Find(&groups).Error; err != nil {
		return nil, err
	}

	out := &domainrepos.NetworkCandidateDetails{
		Candidate: &candidate,
		Groups:    make([]domainrepos.NetworkCandidateGroupDetails, 0, len(groups)),
	}
	for _, group := range groups {
		var ws models.NetworkCandidateWSStaging
		var wsPtr *models.NetworkCandidateWSStaging
		if err := r.db.WithContext(ctx).Where("group_id = ?", group.ID).First(&ws).Error; err == nil {
			wsCopy := ws
			wsPtr = &wsCopy
		}
		var frs []models.NetworkCandidateFRStaging
		if err := r.db.WithContext(ctx).Where("group_id = ?", group.ID).Order("id asc").Find(&frs).Error; err != nil {
			return nil, err
		}
		out.Groups = append(out.Groups, domainrepos.NetworkCandidateGroupDetails{
			Group: group,
			WS:    wsPtr,
			FRs:   frs,
		})
	}
	return out, nil
}

func (r *networkCandidateRepo) Approve(ctx context.Context, in domainrepos.NetworkCandidateApproveInput) (*models.NetworkCandidate, error) {
	if in.CandidateID == 0 {
		return nil, errors.New("candidate_id РѕР±СЏР·Р°С‚РµР»РµРЅ")
	}
	var out models.NetworkCandidate
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", in.CandidateID).First(&out).Error; err != nil {
			return err
		}
		childID, err := r.ensureChildCompany(tx, out.HubCompanyID, in)
		if err != nil {
			return err
		}
		var srv server.Server
		if err := tx.Where("id = ?", out.ServerID).First(&srv).Error; err != nil {
			return err
		}

		var groups []models.NetworkCandidateGroup
		if err := tx.Where("candidate_id = ? AND status = ?", out.ID, models.NetworkCandidateGroupStatusActive).Find(&groups).Error; err != nil {
			return err
		}

		for _, group := range groups {
			wsEntity, err := r.applyGroupWorkstation(tx, group.ID, &srv, childID, ncPtrValue(in.Comment))
			if err != nil {
				return err
			}
			if err := r.applyGroupFiscals(tx, group.ID, &srv, wsEntity, childID, ncPtrValue(in.Comment)); err != nil {
				return err
			}
		}

		return tx.Model(&models.NetworkCandidate{}).Where("id = ?", out.ID).Updates(map[string]interface{}{
			"status":     models.NetworkCandidateStatusApproved,
			"updated_at": time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Where("id = ?", in.CandidateID).First(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *networkCandidateRepo) RemoveGroup(ctx context.Context, candidateID, groupID uint) (*models.NetworkCandidate, error) {
	if candidateID == 0 || groupID == 0 {
		return nil, errors.New("candidate_id Рё group_id РѕР±СЏР·Р°С‚РµР»СЊРЅС‹")
	}
	var newCandidate models.NetworkCandidate
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var sourceCandidate models.NetworkCandidate
		if err := tx.Where("id = ?", candidateID).First(&sourceCandidate).Error; err != nil {
			return err
		}
		var sourceGroup models.NetworkCandidateGroup
		if err := tx.Where("id = ? AND candidate_id = ?", groupID, candidateID).First(&sourceGroup).Error; err != nil {
			return err
		}

		newCandidate = models.NetworkCandidate{
			Status:       models.NetworkCandidateStatusNew,
			HubCompanyID: sourceCandidate.HubCompanyID,
			ServerID:     sourceCandidate.ServerID,
			ServerKey:    sourceCandidate.ServerKey,
			ServerCRMID:  sourceCandidate.ServerCRMID,
			ServerURL:    sourceCandidate.ServerURL,
		}
		if err := tx.Create(&newCandidate).Error; err != nil {
			return err
		}

		newGroup := models.NetworkCandidateGroup{
			CandidateID:   newCandidate.ID,
			ObservationID: sourceGroup.ObservationID,
			Status:        models.NetworkCandidateGroupStatusActive,
		}
		if err := tx.Create(&newGroup).Error; err != nil {
			return err
		}

		var sourceWS models.NetworkCandidateWSStaging
		if err := tx.Where("group_id = ?", sourceGroup.ID).First(&sourceWS).Error; err == nil {
			copyWS := sourceWS
			copyWS.ID = 0
			copyWS.GroupID = newGroup.ID
			if err := tx.Create(&copyWS).Error; err != nil {
				return err
			}
		}

		var sourceFRs []models.NetworkCandidateFRStaging
		if err := tx.Where("group_id = ?", sourceGroup.ID).Find(&sourceFRs).Error; err != nil {
			return err
		}
		for _, item := range sourceFRs {
			copyFR := item
			copyFR.ID = 0
			copyFR.GroupID = newGroup.ID
			if err := tx.Create(&copyFR).Error; err != nil {
				return err
			}
		}

		if err := tx.Model(&models.NetworkCandidateGroup{}).Where("id = ?", sourceGroup.ID).Update("status", models.NetworkCandidateGroupStatusTransferred).Error; err != nil {
			return err
		}

		return tx.Model(&models.NetworkCandidate{}).Where("id = ?", sourceCandidate.ID).Update("updated_at", time.Now().UTC()).Error
	})
	if err != nil {
		return nil, err
	}
	return &newCandidate, nil
}

func (r *networkCandidateRepo) ensureChildCompany(tx *gorm.DB, hubCompanyID string, in domainrepos.NetworkCandidateApproveInput) (string, error) {
	companyID := strings.TrimSpace(in.ChildCompanyID)
	if companyID != "" {
		var existing company.Company
		if err := tx.Where("id = ?", companyID).First(&existing).Error; err != nil {
			return "", err
		}
		if existing.ParentID == nil || strings.TrimSpace(*existing.ParentID) != strings.TrimSpace(hubCompanyID) {
			return "", fmt.Errorf("РІС‹Р±СЂР°РЅРЅР°СЏ РєРѕРјРїР°РЅРёСЏ РЅРµ СЏРІР»СЏРµС‚СЃСЏ РґРѕС‡РµСЂРЅРµР№ РґР»СЏ hub %s", hubCompanyID)
		}
		return existing.ID, nil
	}
	title := strings.TrimSpace(ncPtrValue(in.ChildCompanyTitle))
	if title == "" {
		return "", errors.New("СѓРєР°Р¶РёС‚Рµ child_company_id РёР»Рё child_company.title")
	}
	entity := company.Company{
		Title:     &title,
		Address:   ncStrPtr(ncPtrValue(in.ChildCompanyAddr)),
		ParentID:  ncStrPtr(hubCompanyID),
		OwnerMode: models.CompanyOwnerModeNormal,
	}
	if err := tx.Create(&entity).Error; err != nil {
		return "", err
	}
	return entity.ID, nil
}

func (r *networkCandidateRepo) applyGroupWorkstation(tx *gorm.DB, groupID uint, srv *server.Server, ownerID string, comment string) (*workstation.Workstation, error) {
	var wsStage models.NetworkCandidateWSStaging
	if err := tx.Where("group_id = ?", groupID).First(&wsStage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	identity := ncIdentityHash(ncPtrValue(wsStage.TeamviewerID), ncPtrValue(wsStage.LitemanagerID))
	var ws workstation.Workstation
	found := false
	if identity != "" {
		if err := tx.Where("identity_hash = ?", identity).First(&ws).Error; err == nil {
			found = true
		}
	}
	if !found {
		for _, cond := range []struct {
			Field string
			Value string
		}{
			{Field: "teamviewer", Value: ncNormRID(ncPtrValue(wsStage.TeamviewerID))},
			{Field: "litemanager", Value: ncNormRID(ncPtrValue(wsStage.LitemanagerID))},
			{Field: "anydesk", Value: ncNormRID(ncPtrValue(wsStage.AnydeskID))},
		} {
			if cond.Value == "" {
				continue
			}
			if err := tx.Where(cond.Field+" = ?", cond.Value).First(&ws).Error; err == nil {
				found = true
				break
			}
		}
	}

	if !found {
		ws = workstation.Workstation{
			OwnerID:          ncStrPtr(ownerID),
			OwnerBindingMode: models.OwnerBindingModeManual,
			ServerID:         &srv.ID,
			DeviceName:       wsStage.Hostname,
			Teamviewer:       ncStrPtr(ncNormRID(ncPtrValue(wsStage.TeamviewerID))),
			Litemanager:      ncStrPtr(ncNormRID(ncPtrValue(wsStage.LitemanagerID))),
			Anydesk:          ncStrPtr(ncNormRID(ncPtrValue(wsStage.AnydeskID))),
			IdentityHash:     ncStrPtr(identity),
			LastModifiedDate: timeRef(wsStage.ObservedAt),
			IsNew:            false,
		}
		if err := tx.Create(&ws).Error; err != nil {
			return nil, err
		}
		return &ws, nil
	}

	updates := map[string]interface{}{
		"owner_id":           ownerID,
		"owner_binding_mode": models.OwnerBindingModeManual,
		"server_id":          srv.ID,
		"last_modified_date": wsStage.ObservedAt,
	}
	if v := ncNormRID(ncPtrValue(wsStage.TeamviewerID)); v != "" {
		updates["teamviewer"] = v
	}
	if v := ncNormRID(ncPtrValue(wsStage.LitemanagerID)); v != "" {
		updates["litemanager"] = v
	}
	if v := ncNormRID(ncPtrValue(wsStage.AnydeskID)); v != "" {
		updates["anydesk"] = v
	}
	if wsStage.Hostname != nil && strings.TrimSpace(*wsStage.Hostname) != "" {
		updates["device_name"] = strings.TrimSpace(*wsStage.Hostname)
	}
	if identity != "" {
		updates["identity_hash"] = identity
	}
	if err := tx.Model(&workstation.Workstation{}).Where("id = ?", ws.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	if ws.OwnerID != nil && strings.TrimSpace(*ws.OwnerID) != "" && *ws.OwnerID != ownerID {
		fromOwner := strings.TrimSpace(*ws.OwnerID)
		changeComment := fmt.Sprintf("РЎРјРµРЅР° РІР»Р°РґРµР»СЊС†Р° СЃ %s РЅР° %s", fromOwner, ownerID)
		if strings.TrimSpace(comment) != "" {
			changeComment = changeComment + ". " + strings.TrimSpace(comment)
		}
		history := models.OwnerChangeHistory{
			EntityType:   "Workstation",
			EntityID:     ws.ID,
			FromOwnerID:  &fromOwner,
			ToOwnerID:    ownerID,
			ChangeSource: models.OwnerChangeSourceCandidateApprove,
			Comment:      ncStrPtr(changeComment),
		}
		if err := tx.Create(&history).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.Where("id = ?", ws.ID).First(&ws).Error; err != nil {
		return nil, err
	}
	return &ws, nil
}

func (r *networkCandidateRepo) applyGroupFiscals(tx *gorm.DB, groupID uint, srv *server.Server, ws *workstation.Workstation, ownerID string, comment string) error {
	var frStages []models.NetworkCandidateFRStaging
	if err := tx.Where("group_id = ?", groupID).Find(&frStages).Error; err != nil {
		return err
	}
	for _, stage := range frStages {
		sn := ncNormalizeSerial(ncPtrValue(stage.SerialNumber))
		if sn == "" {
			sn = ncNormalizeSerial(ncPtrValue(stage.SerialNormalized))
		}
		if sn == "" {
			continue
		}
		var fr fiscal.FiscalRegister
		err := tx.Where("fr_serial_normalized = ?", sn).First(&fr).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		workstationID := ""
		if ws != nil {
			workstationID = ws.ID
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			entity := fiscal.FiscalRegister{
				OwnerID:            ncStrPtr(ownerID),
				OwnerBindingMode:   models.OwnerBindingModeManual,
				WorkstationID:      ncStrPtr(workstationID),
				FRSerialNumber:     stage.SerialNumber,
				FRSerialNormalized: &sn,
				ModelKKT:           stage.ModelName,
				RNKKT:              stage.RNKKT,
				INN:                stage.INN,
				FNNumber:           stage.FNNumber,
				LegalName:          stage.OrganizationName,
				Address:            stage.Address,
				LastModifiedDate:   timeRef(stage.ObservedAt),
			}
			if stage.FNExpireDate != nil {
				entity.FNExpireDate = stage.FNExpireDate
			}
			if err := tx.Create(&entity).Error; err != nil {
				return err
			}
			continue
		}

		updates := map[string]interface{}{
			"owner_id":             ownerID,
			"owner_binding_mode":   models.OwnerBindingModeManual,
			"fr_serial_number":     ncPtrValue(stage.SerialNumber),
			"fr_serial_normalized": sn,
			"model_kkt":            ncValOrNil(stage.ModelName),
			"rn_kkt":               ncValOrNil(stage.RNKKT),
			"inn":                  ncValOrNil(stage.INN),
			"fn_number":            ncValOrNil(stage.FNNumber),
			"legal_name":           ncValOrNil(stage.OrganizationName),
			"address":              ncValOrNil(stage.Address),
			"last_modified_date":   stage.ObservedAt,
		}
		if workstationID != "" {
			updates["workstation_id"] = workstationID
		}
		if stage.FNExpireDate != nil {
			updates["fn_expire_date"] = *stage.FNExpireDate
		}
		if err := tx.Model(&fiscal.FiscalRegister{}).Where("id = ?", fr.ID).Updates(updates).Error; err != nil {
			return err
		}
		if fr.OwnerID != nil && strings.TrimSpace(*fr.OwnerID) != "" && *fr.OwnerID != ownerID {
			fromOwner := strings.TrimSpace(*fr.OwnerID)
			changeComment := fmt.Sprintf("РЎРјРµРЅР° РІР»Р°РґРµР»СЊС†Р° СЃ %s РЅР° %s", fromOwner, ownerID)
			if strings.TrimSpace(comment) != "" {
				changeComment = changeComment + ". " + strings.TrimSpace(comment)
			}
			history := models.OwnerChangeHistory{
				EntityType:   "FiscalRegister",
				EntityID:     fr.ID,
				FromOwnerID:  &fromOwner,
				ToOwnerID:    ownerID,
				ChangeSource: models.OwnerChangeSourceCandidateApprove,
				Comment:      ncStrPtr(changeComment),
			}
			if err := tx.Create(&history).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func timeRef(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	c := t
	return &c
}

func ncValOrNil(v *string) interface{} {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return strings.TrimSpace(*v)
}

func ncPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func ncStrPtr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func ncNormalizeSerial(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	return strings.ReplaceAll(v, " ", "")
}

func ncNormRID(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return ""
	}
	return strings.ReplaceAll(v, " ", "")
}

func ncIdentityHash(tv, lm string) string {
	tv = ncNormRID(tv)
	lm = ncNormRID(lm)
	if tv == "" || lm == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tv + ":" + lm))
	return fmt.Sprintf("%x", sum[:])
}
