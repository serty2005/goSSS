package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
)

type CandidateRepo interface {
	List(ctx context.Context, status string, limit, offset int) ([]models.Candidate, error)
	GetByID(ctx context.Context, id uint) (*models.Candidate, error)
	ListWorkstationStaging(ctx context.Context, candidateID uint) ([]models.CandidateWorkstationStaging, error)
	ListFiscalStaging(ctx context.Context, candidateID uint) ([]models.CandidateFiscalStaging, error)
}
