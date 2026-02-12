package task

import (
	"context"
	"etalon-server/internal/domain/models"
	domainRepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/task"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"fmt"
	"sort"
	"time"
)

type serviceImpl struct {
	logger   logger.LoggerInterface
	taskRepo domainRepos.TaskRepo
}

func NewService(logger logger.LoggerInterface, taskRepo domainRepos.TaskRepo) task.Service {
	return &serviceImpl{logger: logger, taskRepo: taskRepo}
}

func (s *serviceImpl) GetTasks(ctx context.Context, status string, limit, offset int) ([]models.ReconciliationTask, error) {
	return s.taskRepo.List(ctx, status, limit, offset)
}

func (s *serviceImpl) GetDuplicates(ctx context.Context) ([]task.DuplicateGroup, error) {
	var allGroups []task.DuplicateGroup

	wsFields := []string{"anydesk", "teamviewer", "litemanager"}
	for _, field := range wsFields {
		groups, err := s.findDuplicateGroups(ctx, field, "Workstation")
		if err != nil {
			s.logger.Error("Ошибка поиска дубликатов Workstation", "field", field, "error", err)
			return nil, err
		}
		allGroups = append(allGroups, groups...)
	}

	serverGroups, err := s.findDuplicateGroups(ctx, "ip", "Server")
	if err != nil {
		s.logger.Error("Ошибка поиска дубликатов Server", "field", "ip", "error", err)
		return nil, err
	}
	allGroups = append(allGroups, serverGroups...)

	return allGroups, nil
}

func (s *serviceImpl) findDuplicateGroups(ctx context.Context, field string, entityType string) ([]task.DuplicateGroup, error) {
	results, err := s.taskRepo.FindDuplicateValues(ctx, entityType, field)
	if err != nil {
		return nil, err
	}

	var groups []task.DuplicateGroup
	for _, res := range results {
		var records []interface{}
		switch entityType {
		case "Workstation":
			wsRecords, err := s.taskRepo.FindWorkstationsByFieldValue(ctx, field, res.Value)
			if err != nil {
				return nil, err
			}
			for i := range wsRecords {
				records = append(records, wsRecords[i])
			}
		case "Server":
			srvRecords, err := s.taskRepo.FindServersByFieldValue(ctx, field, res.Value)
			if err != nil {
				return nil, err
			}
			for i := range srvRecords {
				records = append(records, srvRecords[i])
			}
		default:
			return nil, fmt.Errorf("неизвестный тип сущности: %s", entityType)
		}

		if len(records) < 2 {
			continue
		}

		sort.Slice(records, func(i, j int) bool {
			dateI := getLMDFromInterface(records[i])
			dateJ := getLMDFromInterface(records[j])
			if dateI == nil {
				return false
			}
			if dateJ == nil {
				return true
			}
			return dateI.After(*dateJ)
		})

		groups = append(groups, task.DuplicateGroup{
			Field:      field,
			Value:      res.Value,
			MainRecord: records[0],
			Duplicates: records[1:],
			EntityType: entityType,
		})
	}
	return groups, nil
}

func getLMDFromInterface(record interface{}) *time.Time {
	switch v := record.(type) {
	case workstation.Workstation:
		return v.LastModifiedDate
	case server.Server:
		return v.LastModifiedDate
	default:
		return nil
	}
}
