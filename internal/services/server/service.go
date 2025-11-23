package server

import (
	"context"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
)

type serviceImpl struct {
	logger logger.LoggerInterface
	tm     interfaces.Transactor
	repo   server.Repository
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, repo server.Repository) server.Service {
	return &serviceImpl{logger, tm, repo}
}

func (s *serviceImpl) Create(ctx context.Context, dto *api.ServerCreateDTO) (*server.Server, error) {
	entity := &server.Server{
		UniqueID: dto.UniqueID, CRMid: dto.CRMid, Teamviewer: dto.Teamviewer,
		RDP: dto.RDP, Anydesk: dto.Anydesk, IP: dto.IP, DeviceName: dto.DeviceName,
		ServerName: dto.ServerName, ServerVersion: dto.ServerVersion, Description: dto.Description,
		OwnerID: dto.OwnerID, Status: "unknown",
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.Create(txCtx, nil, entity) // nil tx, так как WithinTransaction положит его в контекст, а репо извлечет
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

func (s *serviceImpl) Get(ctx context.Context, id string) (*server.Server, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, limit, offset int) ([]server.Server, int64, error) {
	// В репозитории пока нет метода Count и List без фильтров, используем FindForPolling как аналог или добавим позже.
	// Для простоты пока используем Search с пустой строкой, если нужно.
	// Но лучше реализовать полноценный List в репо.
	// Т.к. мы рефакторим CrudHandler, там использовался db.Find.
	// Давай пока вернем Search с пустой строкой, это сработает.
	list, err := s.repo.Search(ctx, "", limit, offset)
	// Count пока заглушка 0, так как в Search нет count.
	// Если критично, нужно расширить репозиторий.
	return list, 0, err
}

func (s *serviceImpl) Search(ctx context.Context, term string, limit, offset int) ([]server.Server, error) {
	return s.repo.Search(ctx, term, limit, offset)
}

func cleanData(data map[string]interface{}) {
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")
}
