package server

import (
	"context"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"sort"
	"strings"
)

type serviceImpl struct {
	logger           logger.LoggerInterface
	tm               interfaces.Transactor
	repo             server.Repository
	ownerHistoryRepo domainrepos.OwnerHistoryRepo
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, repo server.Repository, ownerHistoryRepo domainrepos.OwnerHistoryRepo) server.Service {
	return &serviceImpl{logger: logger, tm: tm, repo: repo, ownerHistoryRepo: ownerHistoryRepo}
}

func (s *serviceImpl) Create(ctx context.Context, dto *api.ServerCreateDTO) (*server.Server, error) {
	if err := normalizeServerCreate(dto); err != nil {
		return nil, err
	}
	entity := &server.Server{
		UniqueID: dto.UniqueID, CRMid: nil, Teamviewer: dto.Teamviewer,
		RDP: dto.RDP, Anydesk: dto.Anydesk, IP: dto.IP, IikoWebLink: dto.IikoWebLink, DeviceName: dto.DeviceName,
		ServerName: dto.ServerName, ServerVersion: dto.ServerVersion, Description: dto.Description,
		OwnerID: dto.OwnerID, Status: "unknown",
	}
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Create(txCtx, nil, entity); err != nil {
			return err
		}
		ownerID := strings.TrimSpace(ptrString(entity.OwnerID))
		if s.ownerHistoryRepo != nil && ownerID != "" {
			changedBy := contextUserID(txCtx)
			history := &models.OwnerChangeHistory{
				EntityType:      "Server",
				EntityID:        entity.ID,
				ToOwnerID:       ownerID,
				ChangeSource:    models.OwnerChangeSourceCreated,
				ChangedByUserID: stringPtrOrNil(changedBy),
				Comment:         stringPtrOrNil("Создание сервера"),
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
	if err := normalizeServerUpdate(data); err != nil {
		return err
	}
	return s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		before, err := s.repo.GetByID(txCtx, id)
		if err != nil {
			return err
		}

		ownerPatchRequested := false
		var requestedOwner string
		if rawOwner, ok := data["owner_id"]; ok {
			ownerPatchRequested = true
			if ownerStr, okOwner := rawOwner.(string); okOwner {
				requestedOwner = strings.TrimSpace(ownerStr)
			}
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
					EntityType:      "Server",
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

		after, err := s.repo.GetByID(txCtx, id)
		if err != nil {
			return err
		}
		if s.ownerHistoryRepo != nil {
			changes := collectServerFieldChanges(before, after, data)
			if len(changes) > 0 {
				changedBy := contextUserID(txCtx)
				fromOwner := stringPtrOrNil(strings.TrimSpace(ptrString(before.OwnerID)))
				history := &models.OwnerChangeHistory{
					EntityType:      "Server",
					EntityID:        id,
					FromOwnerID:     fromOwner,
					ToOwnerID:       strings.TrimSpace(ptrString(after.OwnerID)),
					ChangeSource:    models.OwnerChangeSourceManualUpdate,
					ChangedByUserID: stringPtrOrNil(changedBy),
					Comment:         stringPtrOrNil("Изменены поля сервера: " + strings.Join(changes, "; ")),
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

func (s *serviceImpl) Get(ctx context.Context, id string) (*server.Server, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) List(ctx context.Context, limit, offset int, companyIDs, statuses, types []string) ([]server.Server, int64, error) {
	return s.repo.List(ctx, limit, offset, companyIDs, statuses, types)
}

func (s *serviceImpl) Search(ctx context.Context, term string, limit, offset int, companyIDs, statuses, types []string) ([]server.Server, int64, error) {
	return s.repo.SearchWithTotal(ctx, term, limit, offset, companyIDs, statuses, types)
}

func cleanData(data map[string]interface{}) {
	delete(data, "id")
	delete(data, "meta_class")
	delete(data, "created_at")
	delete(data, "updated_at")
	delete(data, "deleted_at")
}

func normalizeServerCreate(dto *api.ServerCreateDTO) error {
	if dto == nil {
		return fmt.Errorf("пустые данные сервера")
	}
	dto.CRMid = nil
	if dto.IP != nil {
		raw := strings.TrimSpace(*dto.IP)
		if raw == "" {
			dto.IP = nil
		} else {
			normalized := validators.ValidateIPAddress(raw)
			if normalized == nil {
				return fmt.Errorf("некорректный формат URL/IP сервера")
			}
			dto.IP = normalized
		}
	}
	if dto.IikoWebLink != nil {
		dto.IikoWebLink = validators.ValidateIikoWebLink(*dto.IikoWebLink)
	}
	return nil
}

func normalizeServerUpdate(data map[string]interface{}) error {
	delete(data, "crm_id")

	rawIP, existsIP := data["ip"]
	if existsIP {
		if rawIP == nil {
			data["ip"] = nil
		} else {
			ipValue, ok := rawIP.(string)
			if !ok {
				return fmt.Errorf("поле ip должно быть строкой")
			}
			ipValue = strings.TrimSpace(ipValue)
			if ipValue == "" {
				data["ip"] = nil
			} else {
				normalized := validators.ValidateIPAddress(ipValue)
				if normalized == nil {
					return fmt.Errorf("некорректный формат URL/IP сервера")
				}
				data["ip"] = *normalized
			}
		}
	}

	rawIikoWebLink, existsIikoWebLink := data["iiko_web_link"]
	if existsIikoWebLink {
		if rawIikoWebLink == nil {
			data["iiko_web_link"] = nil
		} else {
			iikoWebLinkValue, ok := rawIikoWebLink.(string)
			if !ok {
				return fmt.Errorf("поле iiko_web_link должно быть строкой")
			}
			normalized := validators.ValidateIikoWebLink(iikoWebLinkValue)
			if normalized == nil {
				data["iiko_web_link"] = nil
			} else {
				data["iiko_web_link"] = *normalized
			}
		}
	}

	rawValue, exists := data["cabinet_link"]
	if !exists {
		return nil
	}

	strValue, ok := rawValue.(string)
	if !ok {
		return fmt.Errorf("поле cabinet_link должно быть строкой")
	}

	data["cabinet_link"] = validators.ValidateCabinetLink(strValue, "")
	return nil
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

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func printableValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "<пусто>"
	}
	return value
}

func collectServerFieldChanges(before, after *server.Server, patch map[string]interface{}) []string {
	if before == nil || after == nil {
		return nil
	}

	type fieldSnapshot struct {
		before string
		after  string
		label  string
	}

	fields := map[string]fieldSnapshot{
		"device_name": {
			before: ptrString(before.DeviceName),
			after:  ptrString(after.DeviceName),
			label:  "Название устройства",
		},
		"server_name": {
			before: ptrString(before.ServerName),
			after:  ptrString(after.ServerName),
			label:  "Имя сервера",
		},
		"ip": {
			before: ptrString(before.IP),
			after:  ptrString(after.IP),
			label:  "IP",
		},
		"unique_id": {
			before: ptrString(before.UniqueID),
			after:  ptrString(after.UniqueID),
			label:  "Unique ID",
		},
		"crm_id": {
			before: ptrString(before.CRMid),
			after:  ptrString(after.CRMid),
			label:  "CRM ID",
		},
		"server_version": {
			before: ptrString(before.ServerVersion),
			after:  ptrString(after.ServerVersion),
			label:  "Версия сервера",
		},
		"description": {
			before: ptrString(before.Description),
			after:  ptrString(after.Description),
			label:  "Описание",
		},
		"cabinet_link": {
			before: ptrString(before.CabinetLink),
			after:  ptrString(after.CabinetLink),
			label:  "Cabinet Link",
		},
		"iiko_web_link": {
			before: ptrString(before.IikoWebLink),
			after:  ptrString(after.IikoWebLink),
			label:  "SyrveApp / iikoWeb",
		},
	}

	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	changes := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, present := patch[key]; !present {
			continue
		}
		item := fields[key]
		oldValue := strings.TrimSpace(item.before)
		newValue := strings.TrimSpace(item.after)
		if oldValue == newValue {
			continue
		}
		changes = append(changes, fmt.Sprintf("%s: \"%s\" -> \"%s\"", item.label, printableValue(oldValue), printableValue(newValue)))
	}

	return changes
}
