package task

import (
	"context"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/task"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"fmt"
	"sort"
	"time"

	"gorm.io/gorm"
)

type serviceImpl struct {
	logger logger.LoggerInterface
	tm     interfaces.Transactor
	db     *gorm.DB
}

func NewService(logger logger.LoggerInterface, tm interfaces.Transactor, db *gorm.DB) task.Service {
	return &serviceImpl{logger, tm, db}
}

func (s *serviceImpl) GetTasks(ctx context.Context, status string, limit, offset int) ([]models.ReconciliationTask, error) {
	var tasks []models.ReconciliationTask
	query := s.db.Model(&models.ReconciliationTask{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err := query.Limit(limit).Offset(offset).Order("created_at desc").Find(&tasks).Error
	return tasks, err
}

func (s *serviceImpl) GetDuplicates(ctx context.Context) ([]task.DuplicateGroup, error) {
	var allGroups []task.DuplicateGroup

	wsFields := []string{"anydesk", "teamviewer", "litemanager"}
	for _, field := range wsFields {
		groups, err := s.findDuplicateGroups(field, "Workstation")
		if err != nil {
			s.logger.Error("Ошибка поиска дубликатов Workstation", "field", field, "error", err)
			return nil, err
		}
		allGroups = append(allGroups, groups...)
	}

	serverGroups, err := s.findDuplicateGroups("ip", "Server")
	if err != nil {
		s.logger.Error("Ошибка поиска дубликатов Server", "field", "ip", "error", err)
		return nil, err
	}
	allGroups = append(allGroups, serverGroups...)

	return allGroups, nil
}

func (s *serviceImpl) findDuplicateGroups(field string, entityType string) ([]task.DuplicateGroup, error) {
	var results []struct {
		Value string
		Count int
	}
	model := s.getModel(entityType)
	if model == nil {
		return nil, fmt.Errorf("неизвестный тип сущности: %s", entityType)
	}

	err := s.db.Model(model).
		Select(fmt.Sprintf("%s as value, count(*) as count", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&results).Error

	if err != nil {
		return nil, err
	}

	var groups []task.DuplicateGroup
	for _, res := range results {
		var records []interface{}
		switch entityType {
		case "Workstation":
			var wsRecords []workstation.Workstation
			s.db.Where(fmt.Sprintf("%s = ?", field), res.Value).Find(&wsRecords)
			for i := range wsRecords {
				records = append(records, wsRecords[i])
			}
		case "Server":
			var srvRecords []server.Server
			s.db.Where(fmt.Sprintf("%s = ?", field), res.Value).Find(&srvRecords)
			for i := range srvRecords {
				records = append(records, srvRecords[i])
			}
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

func (s *serviceImpl) getModel(entityType string) interface{} {
	switch entityType {
	case "Workstation":
		return &workstation.Workstation{}
	case "Server":
		return &server.Server{}
	default:
		return nil
	}
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
