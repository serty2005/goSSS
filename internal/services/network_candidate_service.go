package services

import (
	"context"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
)

type NetworkCandidateService interface {
	List(ctx context.Context, status string, limit, offset int) ([]models.NetworkCandidate, error)
	GetByID(ctx context.Context, id uint) (*domainrepos.NetworkCandidateDetails, error)
	Approve(ctx context.Context, in domainrepos.NetworkCandidateApproveInput) (*models.NetworkCandidate, error)
	RemoveGroup(ctx context.Context, candidateID, groupID uint) (*models.NetworkCandidate, error)
}

type networkCandidateServiceImpl struct {
	repo domainrepos.NetworkCandidateRepo
}

func NewNetworkCandidateService(repo domainrepos.NetworkCandidateRepo) NetworkCandidateService {
	return &networkCandidateServiceImpl{repo: repo}
}

func (s *networkCandidateServiceImpl) List(ctx context.Context, status string, limit, offset int) ([]models.NetworkCandidate, error) {
	return s.repo.List(ctx, status, limit, offset)
}

func (s *networkCandidateServiceImpl) GetByID(ctx context.Context, id uint) (*domainrepos.NetworkCandidateDetails, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *networkCandidateServiceImpl) Approve(ctx context.Context, in domainrepos.NetworkCandidateApproveInput) (*models.NetworkCandidate, error) {
	return s.repo.Approve(ctx, in)
}

func (s *networkCandidateServiceImpl) RemoveGroup(ctx context.Context, candidateID, groupID uint) (*models.NetworkCandidate, error) {
	return s.repo.RemoveGroup(ctx, candidateID, groupID)
}
