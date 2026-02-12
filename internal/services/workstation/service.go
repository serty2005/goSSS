package workstation

import (
	"context"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"strings"
)

type serviceImpl struct {
	logger logger.LoggerInterface
	tm     interfaces.Transactor
	repo   workstation.Repository
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, repo workstation.Repository) workstation.Service {
	return &serviceImpl{logger, tm, repo}
}

func (s *serviceImpl) Create(ctx context.Context, dto *api.WorkstationCreateDTO) (*workstation.Workstation, error) {
	entity := &workstation.Workstation{
		Teamviewer: dto.Teamviewer, Anydesk: dto.Anydesk, Litemanager: dto.Litemanager,
		DeviceName: dto.DeviceName, Description: dto.Description, OwnerID: dto.OwnerID,
		HealthStatus: "ok",
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.Create(txCtx, nil, entity)
	})
	return entity, err
}

func (s *serviceImpl) Update(ctx context.Context, id string, data map[string]interface{}) error {
	cleanData(data)
	// После ручного именования станция перестаёт считаться новой.
	if rawName, ok := data["device_name"]; ok {
		if name, okCast := rawName.(string); okCast && name != "" {
			data["device_name"] = strings.TrimSpace(name)
		}
		if name, okCast := data["device_name"].(string); okCast && name != "" {
			data["is_new"] = false
		}
	}
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		updated, err := s.repo.Update(txCtx, nil, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return fmt.Errorf("workstation not found")
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
			return fmt.Errorf("workstation not found")
		}
		return nil
	})
}

func (s *serviceImpl) Get(ctx context.Context, id string) (*workstation.Workstation, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, limit, offset int) ([]workstation.Workstation, int64, error) {
	list, err := s.repo.Search(ctx, "", limit, offset)
	return list, 0, err
}

func (s *serviceImpl) Search(ctx context.Context, term string, limit, offset int) ([]workstation.Workstation, error) {
	return s.repo.Search(ctx, term, limit, offset)
}

func cleanData(data map[string]interface{}) {
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")
}
