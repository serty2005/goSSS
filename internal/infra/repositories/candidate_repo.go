package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"

	"gorm.io/gorm"
)

type candidateRepo struct {
	db *gorm.DB
}

func NewCandidateRepo(db *gorm.DB) domainrepos.CandidateRepo {
	return &candidateRepo{db: db}
}

func (r *candidateRepo) List(ctx context.Context, status string, limit, offset int) ([]models.Candidate, error) {
	query := r.db.WithContext(ctx).Model(&models.Candidate{})
	switch status {
	case "ACTIVE":
		query = query.Where("status IN ?", []string{models.CandidateStatusNew, models.CandidateStatusInReview})
	case "ALL":
	default:
		query = query.Where("status = ?", status)
	}
	var items []models.Candidate
	err := query.Order("updated_at desc").Limit(limit).Offset(offset).Find(&items).Error
	return items, err
}

func (r *candidateRepo) GetByID(ctx context.Context, id uint) (*models.Candidate, error) {
	var candidate models.Candidate
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&candidate).Error; err != nil {
		return nil, err
	}
	return &candidate, nil
}

func (r *candidateRepo) ListWorkstationStaging(ctx context.Context, candidateID uint) ([]models.CandidateWorkstationStaging, error) {
	var items []models.CandidateWorkstationStaging
	err := r.db.WithContext(ctx).
		Where("candidate_id = ?", candidateID).
		Order("observed_at desc, id desc").
		Find(&items).Error
	return items, err
}

func (r *candidateRepo) ListFiscalStaging(ctx context.Context, candidateID uint) ([]models.CandidateFiscalStaging, error) {
	var items []models.CandidateFiscalStaging
	err := r.db.WithContext(ctx).
		Where("candidate_id = ?", candidateID).
		Order("observed_at desc, id desc").
		Find(&items).Error
	return items, err
}
