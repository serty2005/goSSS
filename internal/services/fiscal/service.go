package fiscal

import (
	"context"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
)

type serviceImpl struct {
	logger logger.LoggerInterface
	tm     interfaces.Transactor
	repo   fiscal.Repository
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, repo fiscal.Repository) fiscal.Service {
	return &serviceImpl{logger, tm, repo}
}

func (s *serviceImpl) Create(ctx context.Context, dto *api.FiscalRegisterCreateDTO) (*fiscal.FiscalRegister, error) {
	entity := &fiscal.FiscalRegister{
		ModelKKT: dto.ModelKKT, RNKKT: dto.RNKKT, INN: dto.INN, FRSerialNumber: dto.FRSerialNumber,
		FNNumber: dto.FNNumber, FRDownloader: dto.FRDownloader, FRFirmware: dto.FRFirmware,
		DriverVersion: dto.DriverVersion, OwnerID: dto.OwnerID, HealthStatus: "ok",
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.Create(txCtx, nil, entity)
	})
	return entity, err
}

func (s *serviceImpl) Update(ctx context.Context, id string, data map[string]interface{}) error {
	cleanData(data)
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.repo.Update(txCtx, nil, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *serviceImpl) Delete(ctx context.Context, id string) error {
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		deleted, err := s.repo.Delete(txCtx, nil, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return nil
	})
}

func (s *serviceImpl) Get(ctx context.Context, id string) (*fiscal.FiscalRegister, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, limit, offset int) ([]fiscal.FiscalRegister, int64, error) {
	list, err := s.repo.Search(ctx, "", limit, offset)
	return list, 0, err
}

func (s *serviceImpl) Search(ctx context.Context, term string, limit, offset int) ([]fiscal.FiscalRegister, error) {
	return s.repo.Search(ctx, term, limit, offset)
}

func cleanData(data map[string]interface{}) {
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")
}
