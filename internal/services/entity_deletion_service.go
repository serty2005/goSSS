package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	infraDB "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/logger"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	ErrDeletionCandidateNotFound         = errors.New("кандидат на удаление не найден")
	ErrDeletionCandidateInvalidState     = errors.New("кандидат на удаление не в ожидаемом статусе")
	ErrDeletionSelfConfirmationForbidden = errors.New("нельзя подтверждать удаление своей же заявкой")
	ErrDeletionEntityNotFound            = errors.New("сущность не найдена")
	ErrDeletionEntityTypeUnsupported     = errors.New("неподдерживаемый тип сущности")
)

type EntityDeletionRequest struct {
	EntityType string
	EntityID   string
	Reason     string
	Comment    string
	Source     string

	RequestedByUserID   *string
	DuplicateOfEntityID *string
	DuplicateField      *string
	DuplicateValue      *string
	Meta                map[string]interface{}
}

type EntityDeletionCandidateListFilter struct {
	Status string
	Limit  int
	Offset int
}

type EntityDeletionService interface {
	RequestDeletion(ctx context.Context, req EntityDeletionRequest) (*models.EntityDeletionCandidate, error)
	ConfirmDeletion(ctx context.Context, candidateID uint) (*models.EntityDeletionCandidate, error)
	ListCandidates(ctx context.Context, filter EntityDeletionCandidateListFilter) ([]models.EntityDeletionCandidate, int64, error)
	GetActiveCandidateByEntity(ctx context.Context, entityType, entityID string) (*models.EntityDeletionCandidate, error)
	GetCandidateDetails(ctx context.Context, candidateID uint) (*EntityDeletionCandidateDetails, error)
	ReplayDuplicateChoice(ctx context.Context, candidateID uint, keepEntityID, deleteEntityID string) (*models.EntityDeletionCandidate, error)
	TryAutoMergeDuplicateGroup(ctx context.Context, entityType, field, value string, internalIDs []string) (bool, error)
	CleanupStalePendingCandidates(ctx context.Context) (int, error)
}

type EntityDeletionCandidateAgentData struct {
	ObservationID uint
	ObservedAt    *time.Time
	PayloadJSON   map[string]interface{}
}

type EntityDeletionCandidateEntityDetails struct {
	Snapshot        *entitySnapshot
	Raw             map[string]interface{}
	IsMoreActual    bool
	LatestAgentData *EntityDeletionCandidateAgentData
}

type EntityDeletionCandidateDetails struct {
	Candidate          *models.EntityDeletionCandidate
	ReasonText         string
	KeepEntity         *EntityDeletionCandidateEntityDetails
	DeleteEntity       *EntityDeletionCandidateEntityDetails
	Entities           []*EntityDeletionCandidateEntityDetails
	CascadeEntities    []*EntityDeletionCandidateEntityDetails
	MoreActualEntityID string
}

type entityDeletionServiceImpl struct {
	logger           logger.LoggerInterface
	db               *gorm.DB
	tm               interfaces.Transactor
	serverRepo       server.Repository
	workstationRepo  workstation.Repository
	frRepo           fiscal.Repository
	companyRepo      company.Repository
	contractRepo     contract.Repository
	ownerHistoryRepo domainrepos.OwnerHistoryRepo
}

func NewEntityDeletionService(
	logger logger.LoggerInterface,
	db *gorm.DB,
	tm interfaces.Transactor,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
	companyRepo company.Repository,
	contractRepo contract.Repository,
	ownerHistoryRepo domainrepos.OwnerHistoryRepo,
) EntityDeletionService {
	return &entityDeletionServiceImpl{
		logger:           logger,
		db:               db,
		tm:               tm,
		serverRepo:       serverRepo,
		workstationRepo:  workstationRepo,
		frRepo:           frRepo,
		companyRepo:      companyRepo,
		contractRepo:     contractRepo,
		ownerHistoryRepo: ownerHistoryRepo,
	}
}

func (s *entityDeletionServiceImpl) RequestDeletion(ctx context.Context, req EntityDeletionRequest) (*models.EntityDeletionCandidate, error) {
	req.EntityType = normalizeEntityType(req.EntityType)
	req.EntityID = strings.TrimSpace(req.EntityID)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = models.EntityDeletionSourceManual
	}
	if req.RequestedByUserID == nil {
		if userID := contextUserIDString(ctx); userID != "" {
			req.RequestedByUserID = &userID
		}
	}

	var out *models.EntityDeletionCandidate
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		handled, err := s.tryResolvePendingDuplicateCandidateTx(txCtx, req, &out)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		return s.requestDeletionTx(txCtx, req, &out)
	})
	return out, err
}

func (s *entityDeletionServiceImpl) ConfirmDeletion(ctx context.Context, candidateID uint) (*models.EntityDeletionCandidate, error) {
	currentUserID := strings.TrimSpace(contextUserIDString(ctx))
	var out *models.EntityDeletionCandidate

	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		candidate, err := s.getPendingCandidateByIDTx(txCtx, candidateID)
		if err != nil {
			return err
		}
		if err := s.confirmDeletionCandidateTx(txCtx, &candidate, currentUserID, true); err != nil {
			return err
		}
		out = &candidate
		return nil
	})
	return out, err
}

func (s *entityDeletionServiceImpl) ListCandidates(ctx context.Context, filter EntityDeletionCandidateListFilter) ([]models.EntityDeletionCandidate, int64, error) {
	limit := filter.Limit
	offset := filter.Offset
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	db := s.dbOrTx(ctx).Model(&models.EntityDeletionCandidate{})
	if status := strings.TrimSpace(filter.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []models.EntityDeletionCandidate
	if err := db.Order("CASE WHEN status = 'PENDING' THEN 0 ELSE 1 END").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *entityDeletionServiceImpl) GetActiveCandidateByEntity(ctx context.Context, entityType, entityID string) (*models.EntityDeletionCandidate, error) {
	entityType = normalizeEntityType(entityType)
	entityID = strings.TrimSpace(entityID)
	if entityType == "" || entityID == "" {
		return nil, nil
	}

	var item models.EntityDeletionCandidate
	err := s.dbOrTx(ctx).
		Where("entity_type = ? AND entity_id = ? AND status = ?", entityType, entityID, models.EntityDeletionCandidateStatusPending).
		Order("created_at DESC").
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *entityDeletionServiceImpl) GetCandidateDetails(ctx context.Context, candidateID uint) (*EntityDeletionCandidateDetails, error) {
	var out *EntityDeletionCandidateDetails
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		db := s.dbOrTx(txCtx)
		var candidate models.EntityDeletionCandidate
		if err := db.Where("id = ?", candidateID).First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeletionCandidateNotFound
			}
			return err
		}

		ids := s.getCandidateAllEntityIDs(candidate)

		entityRows := make([]*EntityDeletionCandidateEntityDetails, 0, len(ids))
		for _, id := range uniqueTrimmedStrings(ids) {
			snap, err := s.getEntitySnapshot(txCtx, candidate.EntityType, id, true)
			if err != nil || snap == nil {
				continue
			}
			raw, err := s.getEntityRawMap(txCtx, candidate.EntityType, id)
			if err != nil {
				raw = map[string]interface{}{"id": id}
			}
			entityRows = append(entityRows, &EntityDeletionCandidateEntityDetails{
				Snapshot: snap,
				Raw:      raw,
			})
		}

		if len(entityRows) == 0 {
			return ErrDeletionEntityNotFound
		}

		sort.SliceStable(entityRows, func(i, j int) bool {
			return entitySortTime(entityRows[i].Snapshot).After(entitySortTime(entityRows[j].Snapshot))
		})
		moreActualID := entityRows[0].Snapshot.EntityID
		entityRows[0].IsMoreActual = true
		agentData, _ := s.getLatestAgentDataForEntity(txCtx, candidate.EntityType, moreActualID)
		entityRows[0].LatestAgentData = agentData

		var keepRow *EntityDeletionCandidateEntityDetails
		var deleteRow *EntityDeletionCandidateEntityDetails
		for _, row := range entityRows {
			if row.Snapshot == nil {
				continue
			}
			if row.Snapshot.EntityID == candidate.EntityID {
				deleteRow = row
			}
			if candidate.DuplicateOfEntityID != nil && row.Snapshot.EntityID == strings.TrimSpace(*candidate.DuplicateOfEntityID) {
				keepRow = row
			}
		}

		reasonText := strings.TrimSpace(derefString(candidate.Reason))
		if reasonText == "" {
			reasonText = strings.TrimSpace(derefString(candidate.Comment))
		}
		if reasonText == "" && candidate.DuplicateField != nil && candidate.DuplicateValue != nil {
			reasonText = fmt.Sprintf("Обнаружен дубль по полю %s=%s", derefString(candidate.DuplicateField), derefString(candidate.DuplicateValue))
		}
		if reasonText == "" {
			reasonText = "Кандидат на удаление"
		}

		var cascadeRows []*EntityDeletionCandidateEntityDetails
		if normalizeEntityType(candidate.EntityType) == "Company" {
			cascadeRows, _ = s.getCompanyCascadePreviewRows(txCtx, strings.TrimSpace(candidate.EntityID))
		}

		out = &EntityDeletionCandidateDetails{
			Candidate:          &candidate,
			ReasonText:         reasonText,
			KeepEntity:         keepRow,
			DeleteEntity:       deleteRow,
			Entities:           entityRows,
			CascadeEntities:    cascadeRows,
			MoreActualEntityID: moreActualID,
		}
		return nil
	})
	return out, err
}

func (s *entityDeletionServiceImpl) ReplayDuplicateChoice(ctx context.Context, candidateID uint, keepEntityID, deleteEntityID string) (*models.EntityDeletionCandidate, error) {
	keepEntityID = strings.TrimSpace(keepEntityID)
	deleteEntityID = strings.TrimSpace(deleteEntityID)
	if keepEntityID == "" || deleteEntityID == "" || keepEntityID == deleteEntityID {
		return nil, ErrDeletionEntityNotFound
	}

	var out *models.EntityDeletionCandidate
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		candidate, err := s.getPendingCandidateByIDTx(txCtx, candidateID)
		if err != nil {
			return err
		}
		if err := s.replayDuplicateChoiceTx(txCtx, &candidate, keepEntityID, deleteEntityID); err != nil {
			return err
		}
		out = &candidate
		return nil
	})
	return out, err
}

func (s *entityDeletionServiceImpl) TryAutoMergeDuplicateGroup(ctx context.Context, entityType, field, value string, internalIDs []string) (bool, error) {
	entityType = normalizeEntityType(entityType)
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if entityType == "" || field == "" || len(internalIDs) < 2 {
		return false, nil
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		switch entityType {
		case "Server":
			return s.autoMergeServerDuplicatesTx(txCtx, field, value, internalIDs)
		case "Workstation":
			return s.autoMergeWorkstationDuplicatesTx(txCtx, field, value, internalIDs)
		case "FiscalRegister":
			return s.autoMergeFiscalDuplicatesTx(txCtx, field, value, internalIDs)
		default:
			return ErrDeletionEntityTypeUnsupported
		}
	})
	return true, err
}

func (s *entityDeletionServiceImpl) CleanupStalePendingCandidates(ctx context.Context) (int, error) {
	var cleaned int
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		cleaned, err = s.cleanupStalePendingCandidatesTx(txCtx, "")
		return err
	})
	return cleaned, err
}

func (s *entityDeletionServiceImpl) requestDeletionTx(ctx context.Context, req EntityDeletionRequest, out **models.EntityDeletionCandidate) error {
	if req.EntityType == "" || req.EntityID == "" {
		return ErrDeletionEntityNotFound
	}
	snapshot, err := s.getEntitySnapshot(ctx, req.EntityType, req.EntityID, false)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return ErrDeletionEntityNotFound
	}

	db := s.dbOrTx(ctx)
	var existing models.EntityDeletionCandidate
	err = db.Where("entity_type = ? AND entity_id = ? AND status = ?", req.EntityType, req.EntityID, models.EntityDeletionCandidateStatusPending).
		Order("created_at DESC").
		First(&existing).Error
	if err == nil {
		if out != nil {
			*out = &existing
		}
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	metaJSON := datatypes.JSON([]byte("{}"))
	if len(req.Meta) > 0 {
		if raw, mErr := json.Marshal(req.Meta); mErr == nil {
			metaJSON = datatypes.JSON(raw)
		}
	}

	now := time.Now()
	item := &models.EntityDeletionCandidate{
		EntityType:          req.EntityType,
		EntityID:            req.EntityID,
		EntityDisplayName:   edsStringPtrOrNil(snapshot.DisplayName),
		Status:              models.EntityDeletionCandidateStatusPending,
		Reason:              edsStringPtrOrNil(req.Reason),
		Source:              req.Source,
		Comment:             edsStringPtrOrNil(req.Comment),
		RequestedByUserID:   trimStringPtr(req.RequestedByUserID),
		RequestedAt:         now,
		DuplicateOfEntityID: trimStringPtr(req.DuplicateOfEntityID),
		DuplicateField:      trimStringPtr(req.DuplicateField),
		DuplicateValue:      trimStringPtr(req.DuplicateValue),
		Meta:                metaJSON,
	}
	if err := db.Create(item).Error; err != nil {
		return err
	}

	comment := "Сущность добавлена в кандидаты на удаление"
	if req.Source == models.EntityDeletionSourceDuplicateWorker {
		comment = "Автоматически добавлена в кандидаты на удаление как менее актуальный дубль"
	}
	s.writeEntityHistory(ctx, req.EntityType, req.EntityID, models.OwnerChangeSourceDeleteMarked, comment, derefString(item.RequestedByUserID))

	if out != nil {
		*out = item
	}
	return nil
}

func (s *entityDeletionServiceImpl) tryResolvePendingDuplicateCandidateTx(ctx context.Context, req EntityDeletionRequest, out **models.EntityDeletionCandidate) (bool, error) {
	if req.Source != models.EntityDeletionSourceManual {
		return false, nil
	}
	candidate, err := s.findPendingDuplicateCandidateByMemberTx(ctx, req.EntityType, req.EntityID)
	if err != nil {
		return false, err
	}
	if candidate == nil {
		return false, nil
	}
	if !isPairCandidate(*candidate) {
		return false, nil
	}

	deleteEntityID := strings.TrimSpace(req.EntityID)
	keepEntityID := ""
	for _, entityID := range pairCandidateEntityIDs(*candidate) {
		if entityID != deleteEntityID {
			keepEntityID = entityID
			break
		}
	}
	if keepEntityID == "" {
		return false, nil
	}

	if strings.TrimSpace(candidate.EntityID) != deleteEntityID {
		if err := s.replayDuplicateChoiceTx(ctx, candidate, keepEntityID, deleteEntityID); err != nil {
			return true, err
		}
	}
	if err := s.confirmDeletionCandidateTx(ctx, candidate, contextUserIDString(ctx), true); err != nil {
		return true, err
	}
	if out != nil {
		*out = candidate
	}
	return true, nil
}

func (s *entityDeletionServiceImpl) getPendingCandidateByIDTx(ctx context.Context, candidateID uint) (models.EntityDeletionCandidate, error) {
	var candidate models.EntityDeletionCandidate
	if err := s.dbOrTx(ctx).Where("id = ?", candidateID).First(&candidate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.EntityDeletionCandidate{}, ErrDeletionCandidateNotFound
		}
		return models.EntityDeletionCandidate{}, err
	}
	if candidate.Status != models.EntityDeletionCandidateStatusPending {
		return models.EntityDeletionCandidate{}, ErrDeletionCandidateInvalidState
	}
	return candidate, nil
}

func (s *entityDeletionServiceImpl) confirmDeletionCandidateTx(ctx context.Context, candidate *models.EntityDeletionCandidate, currentUserID string, enforceSelfConfirmation bool) error {
	if candidate == nil {
		return ErrDeletionCandidateNotFound
	}
	if candidate.Status != models.EntityDeletionCandidateStatusPending {
		return ErrDeletionCandidateInvalidState
	}
	if enforceSelfConfirmation && candidate.RequestedByUserID != nil && currentUserID != "" && strings.TrimSpace(*candidate.RequestedByUserID) == currentUserID {
		return ErrDeletionSelfConfirmationForbidden
	}

	deleteIDs := s.getCandidateDeleteEntityIDs(*candidate)
	if len(deleteIDs) == 0 {
		deleteIDs = []string{candidate.EntityID}
	}
	deleteIDs = uniqueTrimmedStrings(append(deleteIDs, strings.TrimSpace(candidate.EntityID)))
	affectedEntityIDs := make([]string, 0, len(deleteIDs))
	deletedAny := false

	for _, deleteID := range deleteIDs {
		if candidate.DuplicateOfEntityID != nil && strings.TrimSpace(*candidate.DuplicateOfEntityID) == deleteID {
			continue
		}
		deleted, err := s.deleteEntityByID(ctx, candidate.EntityType, deleteID)
		if err != nil {
			return err
		}
		if deleted {
			deletedAny = true
			affectedEntityIDs = append(affectedEntityIDs, deleteID)
			s.writeEntityHistory(ctx, candidate.EntityType, deleteID, models.OwnerChangeSourceDeleteConfirmed, fmt.Sprintf("Подтверждено удаление сущности (кандидат #%d)", candidate.ID), currentUserID)
			continue
		}

		alreadyDeleted, err := s.isEntityDeletedOrMissing(ctx, candidate.EntityType, deleteID)
		if err != nil {
			return err
		}
		if alreadyDeleted {
			deletedAny = true
			affectedEntityIDs = append(affectedEntityIDs, deleteID)
		}
	}

	if !deletedAny {
		return ErrDeletionEntityNotFound
	}
	if err := s.markCandidateConfirmedTx(ctx, candidate, currentUserID); err != nil {
		return err
	}
	s.writeEntityHistory(ctx, candidate.EntityType, candidate.EntityID, models.OwnerChangeSourceDeleteConfirmed, fmt.Sprintf("Подтверждено удаление сущности (кандидат #%d)", candidate.ID), currentUserID)
	return s.cleanupPendingCandidatesForDeletedEntitiesTx(ctx, candidate.EntityType, affectedEntityIDs, candidate.ID, currentUserID)
}

func (s *entityDeletionServiceImpl) replayDuplicateChoiceTx(ctx context.Context, candidate *models.EntityDeletionCandidate, keepEntityID, deleteEntityID string) error {
	if candidate == nil {
		return ErrDeletionCandidateNotFound
	}
	if candidate.Status != models.EntityDeletionCandidateStatusPending {
		return ErrDeletionCandidateInvalidState
	}
	if candidate.DuplicateOfEntityID == nil || strings.TrimSpace(*candidate.DuplicateOfEntityID) == "" {
		return errors.New("переигрывание доступно только для кандидатов, созданных по дублям")
	}

	idA := strings.TrimSpace(candidate.EntityID)
	idB := strings.TrimSpace(*candidate.DuplicateOfEntityID)
	valid := (keepEntityID == idA && deleteEntityID == idB) || (keepEntityID == idB && deleteEntityID == idA)
	if !valid {
		return errors.New("выбранные сущности не соответствуют паре дублей кандидата")
	}

	if err := s.mergePairWithSelectedKeepTx(ctx, candidate.EntityType, keepEntityID, deleteEntityID, derefString(candidate.DuplicateField), derefString(candidate.DuplicateValue)); err != nil {
		return err
	}

	updateData := map[string]interface{}{
		"entity_id":              deleteEntityID,
		"duplicate_of_entity_id": keepEntityID,
	}
	meta := s.parseCandidateMeta(*candidate)
	meta["duplicate_entity_ids"] = []string{deleteEntityID}
	meta["survivor_id"] = keepEntityID
	meta["loser_id"] = deleteEntityID
	if metaJSON, mErr := json.Marshal(meta); mErr == nil {
		updateData["meta"] = datatypes.JSON(metaJSON)
		candidate.Meta = datatypes.JSON(metaJSON)
	}
	if snap, _ := s.getEntitySnapshot(ctx, candidate.EntityType, deleteEntityID, true); snap != nil {
		updateData["entity_display_name"] = edsStringPtrOrNil(snap.DisplayName)
	}
	if err := s.dbOrTx(ctx).Model(&models.EntityDeletionCandidate{}).Where("id = ?", candidate.ID).Updates(updateData).Error; err != nil {
		return err
	}
	candidate.EntityID = deleteEntityID
	candidate.DuplicateOfEntityID = &keepEntityID
	if value, ok := updateData["entity_display_name"].(*string); ok {
		candidate.EntityDisplayName = value
	}
	return nil
}

func (s *entityDeletionServiceImpl) findPendingDuplicateCandidateByMemberTx(ctx context.Context, entityType, entityID string) (*models.EntityDeletionCandidate, error) {
	entityType = normalizeEntityType(entityType)
	entityID = strings.TrimSpace(entityID)
	if entityType == "" || entityID == "" {
		return nil, nil
	}

	var candidates []models.EntityDeletionCandidate
	if err := s.dbOrTx(ctx).
		Where("entity_type = ? AND status = ? AND source = ?", entityType, models.EntityDeletionCandidateStatusPending, models.EntityDeletionSourceDuplicateWorker).
		Order("created_at ASC").
		Find(&candidates).Error; err != nil {
		return nil, err
	}
	for i := range candidates {
		if containsString(s.getCandidateAllEntityIDs(candidates[i]), entityID) {
			candidate := candidates[i]
			return &candidate, nil
		}
	}
	return nil, nil
}

func (s *entityDeletionServiceImpl) markCandidateConfirmedTx(ctx context.Context, candidate *models.EntityDeletionCandidate, currentUserID string) error {
	if candidate == nil {
		return nil
	}
	now := time.Now()
	if err := s.dbOrTx(ctx).Model(&models.EntityDeletionCandidate{}).Where("id = ?", candidate.ID).Updates(map[string]interface{}{
		"status":               models.EntityDeletionCandidateStatusConfirmed,
		"confirmed_at":         now,
		"confirmed_by_user_id": edsStringPtrOrNil(currentUserID),
	}).Error; err != nil {
		return err
	}
	candidate.Status = models.EntityDeletionCandidateStatusConfirmed
	candidate.ConfirmedAt = &now
	candidate.ConfirmedByUserID = edsStringPtrOrNil(currentUserID)
	return nil
}

func (s *entityDeletionServiceImpl) isEntityDeletedOrMissing(ctx context.Context, entityType, entityID string) (bool, error) {
	snap, err := s.getEntitySnapshot(ctx, entityType, entityID, true)
	if err != nil {
		return false, err
	}
	if snap == nil {
		return true, nil
	}
	return snap.Deleted, nil
}

func (s *entityDeletionServiceImpl) cleanupPendingCandidatesForDeletedEntitiesTx(ctx context.Context, entityType string, deletedEntityIDs []string, excludedCandidateID uint, currentUserID string) error {
	deletedEntityIDs = uniqueTrimmedStrings(deletedEntityIDs)
	if len(deletedEntityIDs) == 0 {
		return nil
	}

	var candidates []models.EntityDeletionCandidate
	query := s.dbOrTx(ctx).Where("entity_type = ? AND status = ?", normalizeEntityType(entityType), models.EntityDeletionCandidateStatusPending)
	if excludedCandidateID > 0 {
		query = query.Where("id <> ?", excludedCandidateID)
	}
	if err := query.Order("created_at ASC").Find(&candidates).Error; err != nil {
		return err
	}
	for i := range candidates {
		if !hasAnyString(s.getCandidateAllEntityIDs(candidates[i]), deletedEntityIDs) {
			continue
		}
		if _, err := s.reconcilePendingCandidateTx(ctx, &candidates[i], currentUserID); err != nil {
			return err
		}
	}
	return nil
}

func (s *entityDeletionServiceImpl) cleanupStalePendingCandidatesTx(ctx context.Context, currentUserID string) (int, error) {
	var candidates []models.EntityDeletionCandidate
	if err := s.dbOrTx(ctx).
		Where("status = ?", models.EntityDeletionCandidateStatusPending).
		Order("created_at ASC").
		Find(&candidates).Error; err != nil {
		return 0, err
	}

	cleaned := 0
	for i := range candidates {
		changed, err := s.reconcilePendingCandidateTx(ctx, &candidates[i], currentUserID)
		if err != nil {
			return cleaned, err
		}
		if changed {
			cleaned++
		}
	}
	return cleaned, nil
}

func (s *entityDeletionServiceImpl) reconcilePendingCandidateTx(ctx context.Context, candidate *models.EntityDeletionCandidate, currentUserID string) (bool, error) {
	if candidate == nil || candidate.Status != models.EntityDeletionCandidateStatusPending {
		return false, nil
	}

	currentDeleteIDs := s.getCandidateDeleteEntityIDs(*candidate)
	remainingDeleteIDs := make([]string, 0, len(currentDeleteIDs))
	for _, deleteID := range currentDeleteIDs {
		deleted, err := s.isEntityDeletedOrMissing(ctx, candidate.EntityType, deleteID)
		if err != nil {
			return false, err
		}
		if !deleted {
			remainingDeleteIDs = append(remainingDeleteIDs, deleteID)
		}
	}

	survivorDeleted := false
	if candidate.DuplicateOfEntityID != nil && strings.TrimSpace(*candidate.DuplicateOfEntityID) != "" {
		var err error
		survivorDeleted, err = s.isEntityDeletedOrMissing(ctx, candidate.EntityType, strings.TrimSpace(*candidate.DuplicateOfEntityID))
		if err != nil {
			return false, err
		}
	}
	if len(remainingDeleteIDs) == 0 || (survivorDeleted && len(remainingDeleteIDs) == 1) {
		if err := s.markCandidateConfirmedTx(ctx, candidate, currentUserID); err != nil {
			return false, err
		}
		return true, nil
	}

	needsUpdate := !sameTrimmedStringSlices(currentDeleteIDs, remainingDeleteIDs) || !containsString(remainingDeleteIDs, candidate.EntityID)
	if !needsUpdate {
		return false, nil
	}

	meta := s.parseCandidateMeta(*candidate)
	meta["duplicate_entity_ids"] = remainingDeleteIDs
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return false, err
	}

	updateData := map[string]interface{}{
		"meta": datatypes.JSON(metaJSON),
	}
	candidate.Meta = datatypes.JSON(metaJSON)

	if !containsString(remainingDeleteIDs, candidate.EntityID) {
		nextDeleteID := remainingDeleteIDs[0]
		updateData["entity_id"] = nextDeleteID
		candidate.EntityID = nextDeleteID
		if snap, err := s.getEntitySnapshot(ctx, candidate.EntityType, nextDeleteID, true); err == nil && snap != nil {
			updateData["entity_display_name"] = edsStringPtrOrNil(snap.DisplayName)
			candidate.EntityDisplayName = edsStringPtrOrNil(snap.DisplayName)
		}
	}
	if err := s.dbOrTx(ctx).Model(&models.EntityDeletionCandidate{}).Where("id = ?", candidate.ID).Updates(updateData).Error; err != nil {
		return false, err
	}
	return true, nil
}

type entitySnapshot struct {
	EntityType     string
	EntityID       string
	DisplayName    string
	OwnerID        string
	LastUpdatedBy  string
	UpdatedAt      time.Time
	LastModifiedAt *time.Time
	Deleted        bool
}

func (s *entityDeletionServiceImpl) getEntitySnapshot(ctx context.Context, entityType, entityID string, unscoped bool) (*entitySnapshot, error) {
	switch normalizeEntityType(entityType) {
	case "Contract":
		if s.contractRepo == nil {
			return nil, ErrDeletionEntityTypeUnsupported
		}
		item, err := s.contractRepo.GetByID(ctx, entityID)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		return snapshotFromContract(item), nil
	case "Company":
		var item *company.Company
		var err error
		if unscoped {
			item, err = s.companyRepo.GetByIDUnscoped(ctx, entityID)
		} else {
			item, err = s.companyRepo.GetByID(ctx, entityID)
		}
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		return snapshotFromCompany(item), nil
	case "Server":
		var item *server.Server
		var err error
		if unscoped {
			item, err = s.serverRepo.GetByIDUnscoped(ctx, entityID)
		} else {
			item, err = s.serverRepo.GetByID(ctx, entityID)
		}
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		return snapshotFromServer(item), nil
	case "Workstation":
		var item *workstation.Workstation
		var err error
		if unscoped {
			item, err = s.workstationRepo.GetByIDUnscoped(ctx, entityID)
		} else {
			item, err = s.workstationRepo.GetByID(ctx, entityID)
		}
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		return snapshotFromWorkstation(item), nil
	case "FiscalRegister":
		var item *fiscal.FiscalRegister
		var err error
		if unscoped {
			item, err = s.frRepo.GetByIDUnscoped(ctx, entityID)
		} else {
			item, err = s.frRepo.GetByID(ctx, entityID)
		}
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, nil
		}
		return snapshotFromFiscal(item), nil
	default:
		return nil, ErrDeletionEntityTypeUnsupported
	}
}

func (s *entityDeletionServiceImpl) deleteEntityByID(ctx context.Context, entityType, entityID string) (bool, error) {
	switch normalizeEntityType(entityType) {
	case "Company":
		if err := s.cascadeDeleteCompanyDependencies(ctx, entityID); err != nil {
			return false, err
		}
		return s.companyRepo.Delete(ctx, entityID)
	case "Server":
		return s.serverRepo.Delete(ctx, nil, entityID)
	case "Workstation":
		return s.workstationRepo.Delete(ctx, nil, entityID)
	case "FiscalRegister":
		return s.frRepo.Delete(ctx, nil, entityID)
	default:
		return false, ErrDeletionEntityTypeUnsupported
	}
}

func (s *entityDeletionServiceImpl) writeEntityHistory(ctx context.Context, entityType, entityID, source, comment, changedBy string) {
	if s.ownerHistoryRepo == nil {
		return
	}
	snapshot, err := s.getEntitySnapshot(ctx, entityType, entityID, true)
	if err != nil || snapshot == nil {
		return
	}
	event := &models.OwnerChangeHistory{
		EntityType:      normalizeEntityType(entityType),
		EntityID:        entityID,
		ToOwnerID:       strings.TrimSpace(snapshot.OwnerID),
		ChangeSource:    source,
		ChangedByUserID: edsStringPtrOrNil(changedBy),
		Comment:         edsStringPtrOrNil(comment),
	}
	_ = s.ownerHistoryRepo.Create(ctx, event)
}

func (s *entityDeletionServiceImpl) dbOrTx(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, s.db).WithContext(ctx)
}

func snapshotFromServer(item *server.Server) *entitySnapshot {
	if item == nil {
		return nil
	}
	return &entitySnapshot{
		EntityType:     "Server",
		EntityID:       item.ID,
		DisplayName:    firstNonEmpty(ptrStringValue(item.DeviceName), ptrStringValue(item.ServerName), ptrStringValue(item.IP), item.ID),
		OwnerID:        ptrStringValue(item.OwnerID),
		LastUpdatedBy:  strings.TrimSpace(item.LastUpdatedBy),
		UpdatedAt:      item.UpdatedAt,
		LastModifiedAt: item.LastModifiedDate,
		Deleted:        item.DeletedAt.Valid,
	}
}

func snapshotFromCompany(item *company.Company) *entitySnapshot {
	if item == nil {
		return nil
	}
	title := ""
	if item.Title != nil {
		title = strings.TrimSpace(*item.Title)
	}
	return &entitySnapshot{
		EntityType:     "Company",
		EntityID:       item.ID,
		DisplayName:    firstNonEmpty(title, item.ID),
		OwnerID:        "",
		LastUpdatedBy:  "",
		UpdatedAt:      item.UpdatedAt,
		LastModifiedAt: item.LastModifiedDate,
		Deleted:        item.DeletedAt.Valid,
	}
}

func snapshotFromContract(item *contract.Contract) *entitySnapshot {
	if item == nil {
		return nil
	}
	state := ""
	if item.State != nil {
		state = strings.TrimSpace(*item.State)
	}
	return &entitySnapshot{
		EntityType:     "Contract",
		EntityID:       item.ID,
		DisplayName:    firstNonEmpty(state, item.ID),
		OwnerID:        "",
		LastUpdatedBy:  strings.TrimSpace(item.LastUpdatedBy),
		UpdatedAt:      item.UpdatedAt,
		LastModifiedAt: item.LastModifiedDate,
		Deleted:        item.DeletedAt.Valid,
	}
}

func (s *entityDeletionServiceImpl) cascadeDeleteCompanyDependencies(ctx context.Context, companyID string) error {
	db := s.dbOrTx(ctx)

	var serverIDs []string
	if err := db.Model(&server.Server{}).Where("owner_id = ?", companyID).Pluck("id", &serverIDs).Error; err != nil {
		return err
	}
	for _, id := range uniqueTrimmedStrings(serverIDs) {
		if _, err := s.serverRepo.Delete(ctx, nil, id); err != nil {
			return err
		}
	}

	var workstationIDs []string
	if err := db.Model(&workstation.Workstation{}).Where("owner_id = ?", companyID).Pluck("id", &workstationIDs).Error; err != nil {
		return err
	}
	for _, id := range uniqueTrimmedStrings(workstationIDs) {
		if _, err := s.workstationRepo.Delete(ctx, nil, id); err != nil {
			return err
		}
	}

	var fiscalIDs []string
	if err := db.Model(&fiscal.FiscalRegister{}).Where("owner_id = ?", companyID).Pluck("id", &fiscalIDs).Error; err != nil {
		return err
	}
	for _, id := range uniqueTrimmedStrings(fiscalIDs) {
		if _, err := s.frRepo.Delete(ctx, nil, id); err != nil {
			return err
		}
	}

	var contractIDs []string
	if err := db.Table("company_contracts").Where("company_id = ?", companyID).Pluck("contract_id", &contractIDs).Error; err != nil {
		return err
	}
	contractIDs = uniqueTrimmedStrings(contractIDs)
	if err := db.Table("company_contracts").Where("company_id = ?", companyID).Delete(&models.CompanyContract{}).Error; err != nil {
		return err
	}

	for _, contractID := range contractIDs {
		var linksCount int64
		if err := db.Table("company_contracts").Where("contract_id = ?", contractID).Count(&linksCount).Error; err != nil {
			return err
		}
		if linksCount > 0 {
			continue
		}
		if s.contractRepo == nil {
			continue
		}
		if _, err := s.contractRepo.Delete(ctx, contractID); err != nil {
			return err
		}
	}

	return nil
}

func snapshotFromWorkstation(item *workstation.Workstation) *entitySnapshot {
	if item == nil {
		return nil
	}
	return &entitySnapshot{
		EntityType:     "Workstation",
		EntityID:       item.ID,
		DisplayName:    firstNonEmpty(ptrStringValue(item.DeviceName), ptrStringValue(item.Anydesk), ptrStringValue(item.Teamviewer), item.ID),
		OwnerID:        ptrStringValue(item.OwnerID),
		LastUpdatedBy:  strings.TrimSpace(item.LastUpdatedBy),
		UpdatedAt:      item.UpdatedAt,
		LastModifiedAt: item.LastModifiedDate,
		Deleted:        item.DeletedAt.Valid,
	}
}

func snapshotFromFiscal(item *fiscal.FiscalRegister) *entitySnapshot {
	if item == nil {
		return nil
	}
	return &entitySnapshot{
		EntityType:     "FiscalRegister",
		EntityID:       item.ID,
		DisplayName:    firstNonEmpty(ptrStringValue(item.ModelKKT), ptrStringValue(item.FRSerialNumber), ptrStringValue(item.RNKKT), item.ID),
		OwnerID:        ptrStringValue(item.OwnerID),
		LastUpdatedBy:  strings.TrimSpace(item.LastUpdatedBy),
		UpdatedAt:      item.UpdatedAt,
		LastModifiedAt: item.LastModifiedDate,
		Deleted:        item.DeletedAt.Valid,
	}
}

func entitySortTime(snap *entitySnapshot) time.Time {
	if snap == nil {
		return time.Time{}
	}
	if snap.LastModifiedAt != nil && snap.LastModifiedAt.After(snap.UpdatedAt) {
		return *snap.LastModifiedAt
	}
	return snap.UpdatedAt
}

func (s *entityDeletionServiceImpl) getEntityRawMap(ctx context.Context, entityType, entityID string) (map[string]interface{}, error) {
	switch normalizeEntityType(entityType) {
	case "Contract":
		if s.contractRepo == nil {
			return nil, ErrDeletionEntityTypeUnsupported
		}
		item, err := s.contractRepo.GetByID(ctx, entityID)
		if err != nil || item == nil {
			return nil, err
		}
		return structToMap(item), nil
	case "Company":
		item, err := s.companyRepo.GetByIDUnscoped(ctx, entityID)
		if err != nil || item == nil {
			return nil, err
		}
		return structToMap(item), nil
	case "Server":
		item, err := s.serverRepo.GetByIDUnscoped(ctx, entityID)
		if err != nil || item == nil {
			return nil, err
		}
		return structToMap(item), nil
	case "Workstation":
		item, err := s.workstationRepo.GetByIDUnscoped(ctx, entityID)
		if err != nil || item == nil {
			return nil, err
		}
		return structToMap(item), nil
	case "FiscalRegister":
		item, err := s.frRepo.GetByIDUnscoped(ctx, entityID)
		if err != nil || item == nil {
			return nil, err
		}
		return structToMap(item), nil
	default:
		return nil, ErrDeletionEntityTypeUnsupported
	}
}

func (s *entityDeletionServiceImpl) getCompanyCascadePreviewRows(ctx context.Context, companyID string) ([]*EntityDeletionCandidateEntityDetails, error) {
	if strings.TrimSpace(companyID) == "" {
		return nil, nil
	}
	db := s.dbOrTx(ctx)
	rows := make([]*EntityDeletionCandidateEntityDetails, 0)

	appendByIDs := func(entityType string, ids []string) error {
		for _, id := range uniqueTrimmedStrings(ids) {
			snap, err := s.getEntitySnapshot(ctx, entityType, id, true)
			if err != nil {
				return err
			}
			if snap == nil {
				continue
			}
			raw, err := s.getEntityRawMap(ctx, entityType, id)
			if err != nil {
				raw = map[string]interface{}{"id": id}
			}
			rows = append(rows, &EntityDeletionCandidateEntityDetails{
				Snapshot: snap,
				Raw:      raw,
			})
		}
		return nil
	}

	var serverIDs []string
	if err := db.Model(&server.Server{}).Where("owner_id = ?", companyID).Pluck("id", &serverIDs).Error; err != nil {
		return nil, err
	}
	if err := appendByIDs("Server", serverIDs); err != nil {
		return nil, err
	}

	var workstationIDs []string
	if err := db.Model(&workstation.Workstation{}).Where("owner_id = ?", companyID).Pluck("id", &workstationIDs).Error; err != nil {
		return nil, err
	}
	if err := appendByIDs("Workstation", workstationIDs); err != nil {
		return nil, err
	}

	var fiscalIDs []string
	if err := db.Model(&fiscal.FiscalRegister{}).Where("owner_id = ?", companyID).Pluck("id", &fiscalIDs).Error; err != nil {
		return nil, err
	}
	if err := appendByIDs("FiscalRegister", fiscalIDs); err != nil {
		return nil, err
	}

	var contractIDs []string
	if err := db.Table("company_contracts").Where("company_id = ?", companyID).Pluck("contract_id", &contractIDs).Error; err != nil {
		return nil, err
	}
	for _, contractID := range uniqueTrimmedStrings(contractIDs) {
		var linksCount int64
		if err := db.Table("company_contracts").Where("contract_id = ?", contractID).Count(&linksCount).Error; err != nil {
			return nil, err
		}
		if linksCount != 1 {
			continue
		}
		if err := appendByIDs("Contract", []string{contractID}); err != nil {
			return nil, err
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i]
		right := rows[j]
		if left == nil || left.Snapshot == nil {
			return false
		}
		if right == nil || right.Snapshot == nil {
			return true
		}
		if left.Snapshot.EntityType != right.Snapshot.EntityType {
			return left.Snapshot.EntityType < right.Snapshot.EntityType
		}
		return entitySortTime(left.Snapshot).After(entitySortTime(right.Snapshot))
	})

	return rows, nil
}

func (s *entityDeletionServiceImpl) getLatestAgentDataForEntity(ctx context.Context, entityType, entityID string) (*EntityDeletionCandidateAgentData, error) {
	if s.ownerHistoryRepo == nil {
		return nil, nil
	}
	history, err := s.ownerHistoryRepo.ListByEntitiesAndSources(ctx, []string{normalizeEntityType(entityType)}, []string{entityID}, []string{models.OwnerChangeSourceAgentDataUpdate}, 20)
	if err != nil || len(history) == 0 {
		return nil, err
	}
	var observationID uint
	for _, item := range history {
		if item.ObservationID != nil && *item.ObservationID > 0 {
			observationID = *item.ObservationID
			break
		}
	}
	if observationID == 0 {
		return nil, nil
	}
	var obs models.AgentObservation
	if err := s.dbOrTx(ctx).Where("id = ?", observationID).First(&obs).Error; err != nil {
		return nil, err
	}
	payload := map[string]interface{}{}
	if len(obs.PayloadJSON) > 0 {
		_ = json.Unmarshal(obs.PayloadJSON, &payload)
	}
	return &EntityDeletionCandidateAgentData{
		ObservationID: obs.ID,
		ObservedAt:    &obs.ObservedAt,
		PayloadJSON:   payload,
	}, nil
}

func (s *entityDeletionServiceImpl) mergePairWithSelectedKeepTx(ctx context.Context, entityType, keepEntityID, deleteEntityID, field, value string) error {
	switch normalizeEntityType(entityType) {
	case "Server":
		keep, err := s.serverRepo.GetByID(ctx, keepEntityID)
		if err != nil || keep == nil {
			return ErrDeletionEntityNotFound
		}
		del, err := s.serverRepo.GetByID(ctx, deleteEntityID)
		if err != nil || del == nil {
			return ErrDeletionEntityNotFound
		}
		updates, mergeNote := buildServerMergePatch(keep, del)
		if len(updates) > 0 {
			updates["last_updated_by"] = firstNonEmpty(strings.TrimSpace(del.LastUpdatedBy), "duplicate_merge")
			if _, err := s.serverRepo.Update(ctx, nil, keep.ID, updates); err != nil {
				return err
			}
		}
		if err := s.dbOrTx(ctx).Model(&workstation.Workstation{}).Where("server_id = ?", del.ID).Update("server_id", keep.ID).Error; err != nil {
			return err
		}
		s.writeEntityHistory(ctx, "Server", keep.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Переигрывание выбора дубля: сохраняем %s, удаляем %s (%s=%s). %s", keep.ID, del.ID, field, value, mergeNote), contextUserIDString(ctx))
		s.writeEntityHistory(ctx, "Server", del.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Переигрывание выбора дубля: помечен на удаление, актуальная сущность %s", keep.ID), contextUserIDString(ctx))
		return nil
	case "Workstation":
		keep, err := s.workstationRepo.GetByID(ctx, keepEntityID)
		if err != nil || keep == nil {
			return ErrDeletionEntityNotFound
		}
		del, err := s.workstationRepo.GetByID(ctx, deleteEntityID)
		if err != nil || del == nil {
			return ErrDeletionEntityNotFound
		}
		updates, mergeNote := buildWorkstationMergePatch(keep, del)
		if len(updates) > 0 {
			updates["last_updated_by"] = firstNonEmpty(strings.TrimSpace(del.LastUpdatedBy), "duplicate_merge")
			if _, err := s.workstationRepo.Update(ctx, nil, keep.ID, updates); err != nil {
				return err
			}
		}
		db := s.dbOrTx(ctx)
		if err := db.Model(&fiscal.FiscalRegister{}).Where("workstation_id = ?", del.ID).Update("workstation_id", keep.ID).Error; err != nil {
			return err
		}
		if err := db.Model(&models.AgentObservation{}).Where("workstation_id = ?", del.ID).Update("workstation_id", keep.ID).Error; err != nil {
			return err
		}
		s.writeEntityHistory(ctx, "Workstation", keep.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Переигрывание выбора дубля: сохраняем %s, удаляем %s (%s=%s). %s", keep.ID, del.ID, field, value, mergeNote), contextUserIDString(ctx))
		s.writeEntityHistory(ctx, "Workstation", del.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Переигрывание выбора дубля: помечен на удаление, актуальная сущность %s", keep.ID), contextUserIDString(ctx))
		return nil
	case "FiscalRegister":
		keep, err := s.frRepo.GetByID(ctx, keepEntityID)
		if err != nil || keep == nil {
			return ErrDeletionEntityNotFound
		}
		del, err := s.frRepo.GetByID(ctx, deleteEntityID)
		if err != nil || del == nil {
			return ErrDeletionEntityNotFound
		}
		updates, mergeNote := buildFiscalMergePatch(keep, del)
		if len(updates) > 0 {
			updates["last_updated_by"] = firstNonEmpty(strings.TrimSpace(del.LastUpdatedBy), "duplicate_merge")
			if _, err := s.frRepo.Update(ctx, nil, keep.ID, updates); err != nil {
				return err
			}
		}
		if err := s.dbOrTx(ctx).Model(&models.AgentObservation{}).Where("fr_id = ?", del.ID).Update("fr_id", keep.ID).Error; err != nil {
			return err
		}
		s.writeEntityHistory(ctx, "FiscalRegister", keep.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Переигрывание выбора дубля: сохраняем %s, удаляем %s (%s=%s). %s", keep.ID, del.ID, field, value, mergeNote), contextUserIDString(ctx))
		s.writeEntityHistory(ctx, "FiscalRegister", del.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Переигрывание выбора дубля: помечен на удаление, актуальная сущность %s", keep.ID), contextUserIDString(ctx))
		return nil
	default:
		return ErrDeletionEntityTypeUnsupported
	}
}

type serverDuplicateRow struct {
	Item     *server.Server
	Snapshot *entitySnapshot
}

func (s *entityDeletionServiceImpl) autoMergeServerDuplicatesTx(ctx context.Context, field, value string, internalIDs []string) error {
	rows := make([]serverDuplicateRow, 0, len(internalIDs))
	for _, id := range internalIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		item, err := s.serverRepo.GetByID(ctx, id)
		if err != nil || item == nil {
			continue
		}
		rows = append(rows, serverDuplicateRow{Item: item, Snapshot: snapshotFromServer(item)})
	}
	if len(rows) < 2 {
		return nil
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return entitySortTime(rows[i].Snapshot).After(entitySortTime(rows[j].Snapshot))
	})

	survivor := rows[0].Item
	for i := 1; i < len(rows); i++ {
		loser := rows[i].Item
		if loser == nil || survivor == nil || loser.ID == survivor.ID {
			continue
		}
		pending, _ := s.GetActiveCandidateByEntity(ctx, "Server", loser.ID)
		if pending != nil && pending.Source != models.EntityDeletionSourceManual {
			continue
		}

		updates, mergeNote := buildServerMergePatch(survivor, loser)
		if len(updates) > 0 {
			updates["last_updated_by"] = firstNonEmpty(strings.TrimSpace(loser.LastUpdatedBy), "duplicate_merge")
			if _, err := s.serverRepo.Update(ctx, nil, survivor.ID, updates); err != nil {
				return err
			}
			if updated, err := s.serverRepo.GetByID(ctx, survivor.ID); err == nil && updated != nil {
				survivor = updated
			}
		}

		if err := s.dbOrTx(ctx).Model(&workstation.Workstation{}).Where("server_id = ?", loser.ID).Update("server_id", survivor.ID).Error; err != nil {
			return err
		}

		s.writeEntityHistory(ctx, "Server", survivor.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Склейка дубля %s по полю %s=%s. %s", loser.ID, field, value, mergeNote), "")
		s.writeEntityHistory(ctx, "Server", loser.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Запись признана дублем %s по полю %s=%s и подготовлена к удалению", survivor.ID, field, value), "")

		if pending != nil && pending.Source == models.EntityDeletionSourceManual {
			if err := s.attachDuplicateContextToCandidateTx(ctx, pending, survivor.ID, loser.ID, field, value); err != nil {
				return err
			}
			if err := s.confirmDeletionCandidateTx(ctx, pending, "", false); err != nil {
				return err
			}
			continue
		}
		if err := s.ensureDuplicateWorkerCandidateTx(ctx, "Server", survivor.ID, loser.ID, field, value); err != nil && !errors.Is(err, ErrDeletionEntityNotFound) {
			return err
		}
	}
	return nil
}

type workstationDuplicateRow struct {
	Item     *workstation.Workstation
	Snapshot *entitySnapshot
}

func (s *entityDeletionServiceImpl) autoMergeWorkstationDuplicatesTx(ctx context.Context, field, value string, internalIDs []string) error {
	rows := make([]workstationDuplicateRow, 0, len(internalIDs))
	for _, id := range internalIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		item, err := s.workstationRepo.GetByID(ctx, id)
		if err != nil || item == nil {
			continue
		}
		rows = append(rows, workstationDuplicateRow{Item: item, Snapshot: snapshotFromWorkstation(item)})
	}
	if len(rows) < 2 {
		return nil
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return entitySortTime(rows[i].Snapshot).After(entitySortTime(rows[j].Snapshot))
	})

	survivor := rows[0].Item
	for i := 1; i < len(rows); i++ {
		loser := rows[i].Item
		if loser == nil || survivor == nil || loser.ID == survivor.ID {
			continue
		}
		pending, _ := s.GetActiveCandidateByEntity(ctx, "Workstation", loser.ID)
		if pending != nil && pending.Source != models.EntityDeletionSourceManual {
			continue
		}

		updates, mergeNote := buildWorkstationMergePatch(survivor, loser)
		if len(updates) > 0 {
			updates["last_updated_by"] = firstNonEmpty(strings.TrimSpace(loser.LastUpdatedBy), "duplicate_merge")
			if _, err := s.workstationRepo.Update(ctx, nil, survivor.ID, updates); err != nil {
				return err
			}
			if updated, err := s.workstationRepo.GetByID(ctx, survivor.ID); err == nil && updated != nil {
				survivor = updated
			}
		}

		if err := s.dbOrTx(ctx).Model(&fiscal.FiscalRegister{}).Where("workstation_id = ?", loser.ID).Update("workstation_id", survivor.ID).Error; err != nil {
			return err
		}

		s.writeEntityHistory(ctx, "Workstation", survivor.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Склейка дубля %s по полю %s=%s. %s", loser.ID, field, value, mergeNote), "")
		s.writeEntityHistory(ctx, "Workstation", loser.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Запись признана дублем %s по полю %s=%s и подготовлена к удалению", survivor.ID, field, value), "")

		if pending != nil && pending.Source == models.EntityDeletionSourceManual {
			if err := s.attachDuplicateContextToCandidateTx(ctx, pending, survivor.ID, loser.ID, field, value); err != nil {
				return err
			}
			if err := s.confirmDeletionCandidateTx(ctx, pending, "", false); err != nil {
				return err
			}
			continue
		}
		if err := s.ensureDuplicateWorkerCandidateTx(ctx, "Workstation", survivor.ID, loser.ID, field, value); err != nil && !errors.Is(err, ErrDeletionEntityNotFound) {
			return err
		}
	}
	return nil
}

type fiscalDuplicateRow struct {
	Item     *fiscal.FiscalRegister
	Snapshot *entitySnapshot
}

func (s *entityDeletionServiceImpl) autoMergeFiscalDuplicatesTx(ctx context.Context, field, value string, internalIDs []string) error {
	rows := make([]fiscalDuplicateRow, 0, len(internalIDs))
	for _, id := range internalIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		item, err := s.frRepo.GetByID(ctx, id)
		if err != nil || item == nil {
			continue
		}
		rows = append(rows, fiscalDuplicateRow{Item: item, Snapshot: snapshotFromFiscal(item)})
	}
	if len(rows) < 2 {
		return nil
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return entitySortTime(rows[i].Snapshot).After(entitySortTime(rows[j].Snapshot))
	})

	survivor := rows[0].Item
	for i := 1; i < len(rows); i++ {
		loser := rows[i].Item
		if loser == nil || survivor == nil || loser.ID == survivor.ID {
			continue
		}
		pending, _ := s.GetActiveCandidateByEntity(ctx, "FiscalRegister", loser.ID)
		if pending != nil && pending.Source != models.EntityDeletionSourceManual {
			continue
		}

		updates, mergeNote := buildFiscalMergePatch(survivor, loser)
		if len(updates) > 0 {
			updates["last_updated_by"] = firstNonEmpty(strings.TrimSpace(loser.LastUpdatedBy), "duplicate_merge")
			if _, err := s.frRepo.Update(ctx, nil, survivor.ID, updates); err != nil {
				return err
			}
			if updated, err := s.frRepo.GetByID(ctx, survivor.ID); err == nil && updated != nil {
				survivor = updated
			}
		}

		s.writeEntityHistory(ctx, "FiscalRegister", survivor.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Склейка дубля %s по полю %s=%s. %s", loser.ID, field, value, mergeNote), "")
		s.writeEntityHistory(ctx, "FiscalRegister", loser.ID, models.OwnerChangeSourceDuplicateMerge, fmt.Sprintf("Запись признана дублем %s по полю %s=%s и подготовлена к удалению", survivor.ID, field, value), "")

		if pending != nil && pending.Source == models.EntityDeletionSourceManual {
			if err := s.attachDuplicateContextToCandidateTx(ctx, pending, survivor.ID, loser.ID, field, value); err != nil {
				return err
			}
			if err := s.confirmDeletionCandidateTx(ctx, pending, "", false); err != nil {
				return err
			}
			continue
		}
		if err := s.ensureDuplicateWorkerCandidateTx(ctx, "FiscalRegister", survivor.ID, loser.ID, field, value); err != nil && !errors.Is(err, ErrDeletionEntityNotFound) {
			return err
		}
	}
	return nil
}

func (s *entityDeletionServiceImpl) ensureDuplicateWorkerCandidateTx(ctx context.Context, entityType, survivorID, loserID, field, value string) error {
	entityType = normalizeEntityType(entityType)
	survivorID = strings.TrimSpace(survivorID)
	loserID = strings.TrimSpace(loserID)
	field = strings.TrimSpace(field)
	value = strings.TrimSpace(value)
	if entityType == "" || survivorID == "" || loserID == "" || survivorID == loserID {
		return nil
	}

	db := s.dbOrTx(ctx)
	var existing []models.EntityDeletionCandidate
	query := db.Where("entity_type = ? AND status = ? AND source = ?", entityType, models.EntityDeletionCandidateStatusPending, models.EntityDeletionSourceDuplicateWorker)
	if field != "" || value != "" {
		query = query.Where("duplicate_field = ? AND duplicate_value = ?", field, value)
	}
	if err := query.Order("created_at ASC").Find(&existing).Error; err != nil {
		return err
	}
	for i := range existing {
		candidate := existing[i]
		allIDs := uniqueTrimmedStrings(s.getCandidateAllEntityIDs(candidate))
		if !containsString(allIDs, survivorID) && !containsString(allIDs, loserID) {
			continue
		}
		return s.appendDuplicateEntityToCandidateTx(ctx, &candidate, survivorID, loserID, field, value)
	}

	var staged *models.EntityDeletionCandidate
	return s.requestDeletionTx(ctx, EntityDeletionRequest{
		EntityType:          entityType,
		EntityID:            loserID,
		Reason:              "Автоматическая постановка менее актуального дубля на удаление",
		Source:              models.EntityDeletionSourceDuplicateWorker,
		DuplicateOfEntityID: &survivorID,
		DuplicateField:      edsStringPtrOrNil(field),
		DuplicateValue:      edsStringPtrOrNil(value),
		Meta: map[string]interface{}{
			"survivor_id":          survivorID,
			"loser_id":             loserID,
			"duplicate_entity_ids": []string{loserID},
		},
	}, &staged)
}

func (s *entityDeletionServiceImpl) appendDuplicateEntityToCandidateTx(ctx context.Context, candidate *models.EntityDeletionCandidate, survivorID, loserID, field, value string) error {
	if candidate == nil {
		return nil
	}
	allIDs := uniqueTrimmedStrings(s.getCandidateAllEntityIDs(*candidate))
	if containsString(allIDs, loserID) {
		return nil
	}
	meta := s.parseCandidateMeta(*candidate)
	deleteIDs := uniqueTrimmedStrings(append(s.getCandidateDeleteEntityIDs(*candidate), loserID))
	meta["duplicate_entity_ids"] = deleteIDs
	if strings.TrimSpace(survivorID) != "" {
		meta["survivor_id"] = survivorID
	}
	meta["loser_id"] = loserID
	metaJSON, _ := json.Marshal(meta)

	updateData := map[string]interface{}{
		"duplicate_of_entity_id": edsStringPtrOrNil(survivorID),
		"duplicate_field":        edsStringPtrOrNil(field),
		"duplicate_value":        edsStringPtrOrNil(value),
		"meta":                   datatypes.JSON(metaJSON),
	}
	if err := s.dbOrTx(ctx).Model(&models.EntityDeletionCandidate{}).Where("id = ?", candidate.ID).Updates(updateData).Error; err != nil {
		return err
	}
	candidate.Meta = datatypes.JSON(metaJSON)
	candidate.DuplicateOfEntityID = edsStringPtrOrNil(survivorID)
	candidate.DuplicateField = edsStringPtrOrNil(field)
	candidate.DuplicateValue = edsStringPtrOrNil(value)
	return nil
}

func (s *entityDeletionServiceImpl) attachDuplicateContextToCandidateTx(ctx context.Context, candidate *models.EntityDeletionCandidate, survivorID, loserID, field, value string) error {
	if candidate == nil {
		return nil
	}
	meta := s.parseCandidateMeta(*candidate)
	meta["duplicate_entity_ids"] = uniqueTrimmedStrings(append(s.getCandidateDeleteEntityIDs(*candidate), loserID))
	if strings.TrimSpace(survivorID) != "" {
		meta["survivor_id"] = survivorID
	}
	if strings.TrimSpace(loserID) != "" {
		meta["loser_id"] = loserID
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	updateData := map[string]interface{}{
		"duplicate_of_entity_id": edsStringPtrOrNil(survivorID),
		"duplicate_field":        edsStringPtrOrNil(field),
		"duplicate_value":        edsStringPtrOrNil(value),
		"meta":                   datatypes.JSON(metaJSON),
	}
	if err := s.dbOrTx(ctx).Model(&models.EntityDeletionCandidate{}).Where("id = ?", candidate.ID).Updates(updateData).Error; err != nil {
		return err
	}
	candidate.Meta = datatypes.JSON(metaJSON)
	candidate.DuplicateOfEntityID = edsStringPtrOrNil(survivorID)
	candidate.DuplicateField = edsStringPtrOrNil(field)
	candidate.DuplicateValue = edsStringPtrOrNil(value)
	return nil
}

func (s *entityDeletionServiceImpl) parseCandidateMeta(candidate models.EntityDeletionCandidate) map[string]interface{} {
	meta := map[string]interface{}{}
	if len(candidate.Meta) > 0 {
		_ = json.Unmarshal(candidate.Meta, &meta)
	}
	return meta
}

func (s *entityDeletionServiceImpl) getCandidateDeleteEntityIDs(candidate models.EntityDeletionCandidate) []string {
	meta := s.parseCandidateMeta(candidate)
	ids := extractStringSlice(meta["duplicate_entity_ids"])
	if len(ids) == 0 && strings.TrimSpace(candidate.EntityID) != "" {
		ids = []string{strings.TrimSpace(candidate.EntityID)}
	}
	return uniqueTrimmedStrings(ids)
}

func (s *entityDeletionServiceImpl) getCandidateAllEntityIDs(candidate models.EntityDeletionCandidate) []string {
	ids := []string{strings.TrimSpace(candidate.EntityID)}
	if candidate.DuplicateOfEntityID != nil {
		ids = append(ids, strings.TrimSpace(*candidate.DuplicateOfEntityID))
	}
	ids = append(ids, s.getCandidateDeleteEntityIDs(candidate)...)
	meta := s.parseCandidateMeta(candidate)
	if survivorID, ok := meta["survivor_id"].(string); ok {
		ids = append(ids, survivorID)
	}
	if loserID, ok := meta["loser_id"].(string); ok {
		ids = append(ids, loserID)
	}
	return uniqueTrimmedStrings(ids)
}

func extractStringSlice(value interface{}) []string {
	if value == nil {
		return nil
	}
	res := make([]string, 0)
	switch v := value.(type) {
	case []string:
		res = append(res, v...)
	case []interface{}:
		for _, item := range v {
			res = append(res, fmt.Sprintf("%v", item))
		}
	case string:
		res = append(res, v)
	}
	return uniqueTrimmedStrings(res)
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func hasAnyString(values []string, candidates []string) bool {
	for _, candidate := range candidates {
		if containsString(values, candidate) {
			return true
		}
	}
	return false
}

func sameTrimmedStringSlices(left []string, right []string) bool {
	left = uniqueTrimmedStrings(left)
	right = uniqueTrimmedStrings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func pairCandidateEntityIDs(candidate models.EntityDeletionCandidate) []string {
	return uniqueTrimmedStrings([]string{candidate.EntityID, derefString(candidate.DuplicateOfEntityID)})
}

func isPairCandidate(candidate models.EntityDeletionCandidate) bool {
	return len(pairCandidateEntityIDs(candidate)) == 2
}

func normalizeEntityType(entityType string) string {
	switch strings.TrimSpace(entityType) {
	case "Company":
		return "Company"
	case "Fiscal", "FiscalRegister":
		return "FiscalRegister"
	case "Server":
		return "Server"
	case "Workstation":
		return "Workstation"
	default:
		return strings.TrimSpace(entityType)
	}
}

func contextUserIDString(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value := ctx.Value(contextkeys.UserIDContextKey)
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func edsStringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func trimStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return edsStringPtrOrNil(*value)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func ptrStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func copyStringField(updates map[string]interface{}, column string, current string, incoming *string, notes *[]string) {
	incomingValue := ptrStringValue(incoming)
	if incomingValue == "" || incomingValue == strings.TrimSpace(current) {
		return
	}
	if strings.TrimSpace(current) == "" {
		updates[column] = incomingValue
		*notes = append(*notes, column)
	}
}

func buildServerMergePatch(target, source *server.Server) (map[string]interface{}, string) {
	updates := make(map[string]interface{})
	notes := make([]string, 0, 12)

	copyStringField(updates, "unique_id", ptrStringValue(target.UniqueID), source.UniqueID, &notes)
	copyStringField(updates, "crm_id", ptrStringValue(target.CRMid), source.CRMid, &notes)
	copyStringField(updates, "server_key", ptrStringValue(target.ServerKey), source.ServerKey, &notes)
	copyStringField(updates, "teamviewer", ptrStringValue(target.Teamviewer), source.Teamviewer, &notes)
	copyStringField(updates, "rdp", ptrStringValue(target.RDP), source.RDP, &notes)
	copyStringField(updates, "anydesk", ptrStringValue(target.Anydesk), source.Anydesk, &notes)
	copyStringField(updates, "ip", ptrStringValue(target.IP), source.IP, &notes)
	copyStringField(updates, "cabinet_link", ptrStringValue(target.CabinetLink), source.CabinetLink, &notes)
	copyStringField(updates, "device_name", ptrStringValue(target.DeviceName), source.DeviceName, &notes)
	copyStringField(updates, "litemanager", ptrStringValue(target.Litemanager), source.Litemanager, &notes)
	copyStringField(updates, "server_version", ptrStringValue(target.ServerVersion), source.ServerVersion, &notes)
	copyStringField(updates, "description", ptrStringValue(target.Description), source.Description, &notes)
	copyStringField(updates, "server_name", ptrStringValue(target.ServerName), source.ServerName, &notes)
	copyStringField(updates, "server_edition", ptrStringValue(target.ServerEdition), source.ServerEdition, &notes)

	if ptrStringValue(target.OwnerID) == "" && ptrStringValue(source.OwnerID) != "" {
		updates["owner_id"] = ptrStringValue(source.OwnerID)
		notes = append(notes, "owner_id")
	}

	if (target.LastModifiedDate == nil && source.LastModifiedDate != nil) ||
		(target.LastModifiedDate != nil && source.LastModifiedDate != nil && source.LastModifiedDate.After(*target.LastModifiedDate)) {
		updates["last_modified_date"] = source.LastModifiedDate
	}

	if len(notes) == 0 {
		return updates, "Дополнять поля не потребовалось"
	}
	return updates, "Дополнены поля: " + strings.Join(notes, ", ")
}

func buildWorkstationMergePatch(target, source *workstation.Workstation) (map[string]interface{}, string) {
	updates := make(map[string]interface{})
	notes := make([]string, 0, 10)

	copyStringField(updates, "identity_hash", ptrStringValue(target.IdentityHash), source.IdentityHash, &notes)
	copyStringField(updates, "teamviewer", ptrStringValue(target.Teamviewer), source.Teamviewer, &notes)
	copyStringField(updates, "anydesk", ptrStringValue(target.Anydesk), source.Anydesk, &notes)
	copyStringField(updates, "litemanager", ptrStringValue(target.Litemanager), source.Litemanager, &notes)
	copyStringField(updates, "rustdesk", ptrStringValue(target.Rustdesk), source.Rustdesk, &notes)
	copyStringField(updates, "device_name", ptrStringValue(target.DeviceName), source.DeviceName, &notes)
	copyStringField(updates, "description", ptrStringValue(target.Description), source.Description, &notes)

	if ptrStringValue(target.ServerID) == "" && ptrStringValue(source.ServerID) != "" {
		updates["server_id"] = ptrStringValue(source.ServerID)
		notes = append(notes, "server_id")
	}
	if ptrStringValue(target.OwnerID) == "" && ptrStringValue(source.OwnerID) != "" {
		updates["owner_id"] = ptrStringValue(source.OwnerID)
		notes = append(notes, "owner_id")
	}
	if target.IsNew && !source.IsNew {
		updates["is_new"] = false
		notes = append(notes, "is_new")
	}

	if (target.LastModifiedDate == nil && source.LastModifiedDate != nil) ||
		(target.LastModifiedDate != nil && source.LastModifiedDate != nil && source.LastModifiedDate.After(*target.LastModifiedDate)) {
		updates["last_modified_date"] = source.LastModifiedDate
	}

	if len(notes) == 0 {
		return updates, "Дополнять поля не потребовалось"
	}
	return updates, "Дополнены поля: " + strings.Join(notes, ", ")
}

func buildFiscalMergePatch(target, source *fiscal.FiscalRegister) (map[string]interface{}, string) {
	updates := make(map[string]interface{})
	notes := make([]string, 0, 20)

	copyStringField(updates, "model_kkt", ptrStringValue(target.ModelKKT), source.ModelKKT, &notes)
	copyStringField(updates, "ffd", ptrStringValue(target.FFD), source.FFD, &notes)
	copyStringField(updates, "rn_kkt", ptrStringValue(target.RNKKT), source.RNKKT, &notes)
	copyStringField(updates, "legal_name", ptrStringValue(target.LegalName), source.LegalName, &notes)
	copyStringField(updates, "inn", ptrStringValue(target.INN), source.INN, &notes)
	copyStringField(updates, "fr_serial_number", ptrStringValue(target.FRSerialNumber), source.FRSerialNumber, &notes)
	copyStringField(updates, "fr_serial_normalized", ptrStringValue(target.FRSerialNormalized), source.FRSerialNormalized, &notes)
	copyStringField(updates, "fn_number", ptrStringValue(target.FNNumber), source.FNNumber, &notes)
	copyStringField(updates, "fn_execution", ptrStringValue(target.FNExecution), source.FNExecution, &notes)
	copyStringField(updates, "fr_downloader", ptrStringValue(target.FRDownloader), source.FRDownloader, &notes)
	copyStringField(updates, "fr_firmware", ptrStringValue(target.FRFirmware), source.FRFirmware, &notes)
	copyStringField(updates, "driver_version", ptrStringValue(target.DriverVersion), source.DriverVersion, &notes)
	copyStringField(updates, "address", ptrStringValue(target.Address), source.Address, &notes)
	copyStringField(updates, "ofd_name", ptrStringValue(target.OFDName), source.OFDName, &notes)

	if ptrStringValue(target.OwnerID) == "" && ptrStringValue(source.OwnerID) != "" {
		updates["owner_id"] = ptrStringValue(source.OwnerID)
		notes = append(notes, "owner_id")
	}
	if ptrStringValue(target.WorkstationID) == "" && ptrStringValue(source.WorkstationID) != "" {
		updates["workstation_id"] = ptrStringValue(source.WorkstationID)
		notes = append(notes, "workstation_id")
	}

	if (target.KKTRegDate == nil && source.KKTRegDate != nil) ||
		(target.KKTRegDate != nil && source.KKTRegDate != nil && source.KKTRegDate.After(*target.KKTRegDate)) {
		updates["kkt_reg_date"] = source.KKTRegDate
	}
	if (target.FNExpireDate == nil && source.FNExpireDate != nil) ||
		(target.FNExpireDate != nil && source.FNExpireDate != nil && source.FNExpireDate.After(*target.FNExpireDate)) {
		updates["fn_expire_date"] = source.FNExpireDate
	}
	if len(target.Licenses) == 0 && len(source.Licenses) > 0 {
		updates["licenses"] = source.Licenses
		notes = append(notes, "licenses")
	}
	if target.AttributeExcise == nil && source.AttributeExcise != nil {
		updates["attribute_excise"] = source.AttributeExcise
		notes = append(notes, "attribute_excise")
	}
	if target.AttributeMarked == nil && source.AttributeMarked != nil {
		updates["attribute_marked"] = source.AttributeMarked
		notes = append(notes, "attribute_marked")
	}

	if (target.LastModifiedDate == nil && source.LastModifiedDate != nil) ||
		(target.LastModifiedDate != nil && source.LastModifiedDate != nil && source.LastModifiedDate.After(*target.LastModifiedDate)) {
		updates["last_modified_date"] = source.LastModifiedDate
	}

	if len(notes) == 0 {
		return updates, "Дополнять поля не потребовалось"
	}
	return updates, "Дополнены поля: " + strings.Join(notes, ", ")
}

func structToMap(value interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	raw, err := json.Marshal(value)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func uniqueTrimmedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, item := range values {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
