package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
)

type NetworkCandidateApproveInput struct {
	CandidateID       uint
	ChildCompanyID    string
	ChildCompanyTitle *string
	ChildCompanyAddr  *string
	Comment           *string
}

type NetworkCandidateDetails struct {
	Candidate *models.NetworkCandidate
	Groups    []NetworkCandidateGroupDetails
}

type NetworkCandidateGroupDetails struct {
	Group models.NetworkCandidateGroup
	WS    *models.NetworkCandidateWSStaging
	FRs   []models.NetworkCandidateFRStaging
}

type NetworkCandidateRepo interface {
	List(ctx context.Context, status string, limit, offset int) ([]models.NetworkCandidate, error)
	GetByID(ctx context.Context, id uint) (*NetworkCandidateDetails, error)
	Approve(ctx context.Context, in NetworkCandidateApproveInput) (*models.NetworkCandidate, error)
	RemoveGroup(ctx context.Context, candidateID, groupID uint) (*models.NetworkCandidate, error)
}
