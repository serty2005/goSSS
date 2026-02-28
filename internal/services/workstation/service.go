package workstation

import (
	"context"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"strings"
)

type serviceImpl struct {
	logger           logger.LoggerInterface
	tm               interfaces.Transactor
	repo             workstation.Repository
	ownerHistoryRepo domainrepos.OwnerHistoryRepo
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, repo workstation.Repository, ownerHistoryRepo domainrepos.OwnerHistoryRepo) workstation.Service {
	return &serviceImpl{logger: logger, tm: tm, repo: repo, ownerHistoryRepo: ownerHistoryRepo}
}

func (s *serviceImpl) Create(ctx context.Context, dto *api.WorkstationCreateDTO) (*workstation.Workstation, error) {
	if err := normalizeWorkstationCreate(dto); err != nil {
		return nil, err
	}
	entity := &workstation.Workstation{
		Teamviewer: dto.Teamviewer, Anydesk: dto.Anydesk, Litemanager: dto.Litemanager,
		DeviceName: dto.DeviceName, Description: dto.Description, OwnerID: dto.OwnerID,
		HealthStatus: "ok",
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Create(txCtx, nil, entity); err != nil {
			return err
		}
		ownerID := strings.TrimSpace(stringPtrValue(entity.OwnerID))
		if s.ownerHistoryRepo != nil && ownerID != "" {
			changedBy := contextUserID(txCtx)
			history := &models.OwnerChangeHistory{
				EntityType:      "Workstation",
				EntityID:        entity.ID,
				ToOwnerID:       ownerID,
				ChangeSource:    models.OwnerChangeSourceCreated,
				ChangedByUserID: stringPtrOrNil(changedBy),
				Comment:         stringPtrOrNil("Создание рабочей станции"),
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
	if err := normalizeWorkstationUpdate(data); err != nil {
		return err
	}
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
		var before *workstation.Workstation
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
			return fmt.Errorf("workstation not found")
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
					EntityType:      "Workstation",
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
			return fmt.Errorf("workstation not found")
		}
		return nil
	})
}

func (s *serviceImpl) Get(ctx context.Context, id string) (*workstation.Workstation, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, limit, offset int) ([]workstation.Workstation, int64, error) {
	return s.repo.List(ctx, limit, offset)
}

func (s *serviceImpl) Search(ctx context.Context, term string, limit, offset int) ([]workstation.Workstation, int64, error) {
	return s.repo.SearchWithTotal(ctx, term, limit, offset)
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

func normalizeWorkstationCreate(dto *api.WorkstationCreateDTO) error {
	if dto == nil {
		return fmt.Errorf("пустые данные рабочей станции")
	}
	var err error
	dto.Teamviewer, err = normalizeNumericRemoteID(dto.Teamviewer, "teamviewer")
	if err != nil {
		return err
	}
	dto.Anydesk, err = normalizeNumericRemoteID(dto.Anydesk, "anydesk")
	if err != nil {
		return err
	}
	dto.Litemanager, err = normalizeAlphaNumericRemoteID(dto.Litemanager, "litemanager")
	if err != nil {
		return err
	}
	dto.Rustdesk, err = normalizeAlphaNumericRemoteID(dto.Rustdesk, "rustdesk")
	if err != nil {
		return err
	}
	return nil
}

func normalizeWorkstationUpdate(data map[string]interface{}) error {
	if err := normalizeMapRemoteID(data, "teamviewer", normalizeNumericRemoteID); err != nil {
		return err
	}
	if err := normalizeMapRemoteID(data, "anydesk", normalizeNumericRemoteID); err != nil {
		return err
	}
	if err := normalizeMapRemoteID(data, "litemanager", normalizeAlphaNumericRemoteID); err != nil {
		return err
	}
	if err := normalizeMapRemoteID(data, "rustdesk", normalizeAlphaNumericRemoteID); err != nil {
		return err
	}
	return nil
}

func normalizeMapRemoteID(data map[string]interface{}, field string, normalizer func(*string, string) (*string, error)) error {
	raw, exists := data[field]
	if !exists {
		return nil
	}
	if raw == nil {
		data[field] = nil
		return nil
	}
	value, ok := raw.(string)
	if !ok {
		return fmt.Errorf("поле %s должно быть строкой", field)
	}
	normalized, err := normalizer(&value, field)
	if err != nil {
		return err
	}
	if normalized == nil {
		data[field] = nil
	} else {
		data[field] = *normalized
	}
	return nil
}

func normalizeNumericRemoteID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	normalized := validators.ValidateRemoteAccessID(trimmed)
	if normalized == nil {
		return nil, fmt.Errorf("некорректный формат %s", field)
	}
	return normalized, nil
}

func normalizeAlphaNumericRemoteID(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	normalized := validators.ValidateWorkstationRemoteID(trimmed)
	if normalized == nil {
		return nil, fmt.Errorf("некорректный формат %s", field)
	}
	return normalized, nil
}
