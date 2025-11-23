package company

import (
	"context"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
)

type serviceImpl struct {
	logger      logger.LoggerInterface
	tm          interfaces.Transactor
	companyRepo company.Repository
}

func NewService(
	logger logger.LoggerInterface,
	tm interfaces.Transactor,
	companyRepo company.Repository,
) company.Service {
	return &serviceImpl{
		logger:      logger,
		tm:          tm,
		companyRepo: companyRepo,
	}
}

func (s *serviceImpl) CreateCompany(ctx context.Context, dto *api.CompanyCreateDTO) (*company.Company, error) {
	entity := &company.Company{
		Title:          dto.Title,
		Address:        dto.Address,
		AdditionalName: dto.AdditionalName,
	}
	entity.MetaClass = "ou$company"

	// Используем транзакцию
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.companyRepo.Create(txCtx, entity); err != nil {
			s.logger.Error("failed to create company", "error", err)
			return err
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *serviceImpl) UpdateCompany(ctx context.Context, id string, data map[string]interface{}) error {
	// Очистка системных полей
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")

	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.companyRepo.Update(txCtx, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *serviceImpl) DeleteCompany(ctx context.Context, id string) error {
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		deleted, err := s.companyRepo.Delete(txCtx, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *serviceImpl) GetCompany(ctx context.Context, id string) (*company.Company, error) {
	return s.companyRepo.GetByID(ctx, id)
}

func (s *serviceImpl) SearchCompanies(ctx context.Context, term string, limit, offset int) ([]company.Company, error) {
	// Здесь можно добавить бизнес-логику, например, фильтрацию по правам пользователя
	// Пока просто проксируем в репозиторий
	// showInactive = true (показываем всех)
	return s.companyRepo.Search(ctx, term, true, limit, offset)
}
