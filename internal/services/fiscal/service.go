package fiscal

import (
	"context"
	"etalon-server/internal/contextkeys"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"strings"
)

type serviceImpl struct {
	logger           logger.LoggerInterface
	tm               interfaces.Transactor
	repo             fiscal.Repository
	ownerHistoryRepo domainrepos.OwnerHistoryRepo
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, repo fiscal.Repository, ownerHistoryRepo domainrepos.OwnerHistoryRepo) fiscal.Service {
	return &serviceImpl{logger: logger, tm: tm, repo: repo, ownerHistoryRepo: ownerHistoryRepo}
}

func (s *serviceImpl) Create(ctx context.Context, dto *api.FiscalRegisterCreateDTO) (*fiscal.FiscalRegister, error) {
	entity := &fiscal.FiscalRegister{
		ModelKKT: dto.ModelKKT, RNKKT: dto.RNKKT, INN: dto.INN, FRSerialNumber: dto.FRSerialNumber,
		FNNumber: dto.FNNumber, FRDownloader: dto.FRDownloader, FRFirmware: dto.FRFirmware,
		DriverVersion: dto.DriverVersion, OwnerID: dto.OwnerID, HealthStatus: "ok",
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Create(txCtx, nil, entity); err != nil {
			return err
		}
		ownerID := strings.TrimSpace(stringPtrValue(entity.OwnerID))
		if s.ownerHistoryRepo != nil && ownerID != "" {
			changedBy := contextUserID(txCtx)
			history := &models.OwnerChangeHistory{
				EntityType:      "FiscalRegister",
				EntityID:        entity.ID,
				ToOwnerID:       ownerID,
				ChangeSource:    models.OwnerChangeSourceCreated,
				ChangedByUserID: stringPtrOrNil(changedBy),
				Comment:         stringPtrOrNil("Создание фискального регистратора"),
			}
			if err := s.ownerHistoryRepo.Create(txCtx, history); err != nil {
				return err
			}
		}
		return nil
	})
	return entity, err
}

func (s *serviceImpl) Update(ctx context.Context, id string, data map[string]interface{}) error {
	cleanData(data)
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		var before *fiscal.FiscalRegister
		ownerPatchRequested := false
		var requestedOwner string
		if rawOwner, ok := data["owner_id"]; ok {
			ownerPatchRequested = true
			if ownerStr, okOwner := rawOwner.(string); okOwner {
				requestedOwner = strings.TrimSpace(ownerStr)
			}
			entity, err := s.repo.GetByID(txCtx, id)
			if err != nil {
				return err
			}
			before = entity
		}

		updated, err := s.repo.Update(txCtx, nil, id, data)
		if err != nil {
			return err
		}
		if !updated {
			return domain.ErrNotFound
		}
		if ownerPatchRequested {
			updates := map[string]interface{}{
				"owner_binding_mode": models.OwnerBindingModeManual,
			}
			if requestedOwner == "" {
				updates["owner_id"] = nil
			} else {
				updates["owner_id"] = requestedOwner
			}
			if _, err := s.repo.Update(txCtx, nil, id, updates); err != nil {
				return err
			}

			prevOwner := ""
			if before != nil && before.OwnerID != nil {
				prevOwner = strings.TrimSpace(*before.OwnerID)
			}
			if prevOwner != requestedOwner && s.ownerHistoryRepo != nil && requestedOwner != "" {
				var fromOwner *string
				if prevOwner != "" {
					fromOwner = &prevOwner
				}
				changedBy := contextUserID(txCtx)
				history := &models.OwnerChangeHistory{
					EntityType:      "FiscalRegister",
					EntityID:        id,
					FromOwnerID:     fromOwner,
					ToOwnerID:       requestedOwner,
					ChangeSource:    models.OwnerChangeSourceManualUpdate,
					ChangedByUserID: stringPtrOrNil(changedBy),
					Comment:         stringPtrOrNil("Ручная смена владельца"),
				}
				if err := s.ownerHistoryRepo.Create(txCtx, history); err != nil {
					return err
				}
			}
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

func contextUserID(ctx context.Context) string {
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

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
