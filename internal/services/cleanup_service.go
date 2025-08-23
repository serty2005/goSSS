package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"fmt"
	"sort"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// CleanupService отвечает за задачи по очистке данных в базе.
type CleanupService interface {
	// CleanupFRDuplicates находит и выполняет "мягкое удаление" дубликатов фискальных регистраторов.
	CleanupFRDuplicates(ctx context.Context)
	CleanupServerDuplicatesAndJunk(ctx context.Context)
}

type cleanupServiceImpl struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewCleanupService создает новый экземпляр сервиса очистки.
func NewCleanupService(db *gorm.DB, logger *zap.Logger) CleanupService {
	return &cleanupServiceImpl{
		db:     db,
		logger: logger,
	}
}

// CleanupFRDuplicates реализует основную логику поиска и удаления дублей.
func (s *cleanupServiceImpl) CleanupFRDuplicates(ctx context.Context) {
	log := s.logger.With(zap.String("service", "CleanupService"))
	log.Info("Запуск очистки дубликатов фискальных регистраторов...")

	// Поля, по которым ищем дубликаты
	fields := []string{"fr_serial_number", "rn_kkt"}
	totalDeleted := 0

	for _, field := range fields {
		log.Info("Поиск дубликатов по полю", zap.String("field", field))
		deletedCount, err := s.cleanupByField(ctx, field)
		if err != nil {
			log.Error("Ошибка при очистке дубликатов по полю", zap.String("field", field), zap.Error(err))
			continue
		}
		if deletedCount > 0 {
			log.Info("Удалено дубликатов по полю", zap.String("field", field), zap.Int("count", deletedCount))
			totalDeleted += deletedCount
		}
	}

	log.Info("Очистка дубликатов фискальных регистраторов завершена.", zap.Int("total_deleted", totalDeleted))
}

// cleanupByField находит и удаляет дубликаты для одного конкретного поля.
func (s *cleanupServiceImpl) cleanupByField(ctx context.Context, field string) (int, error) {
	var deletedCount int

	// 1. Находим все значения, которые встречаются более одного раза.
	var duplicateValues []struct {
		Value string
	}
	err := s.db.WithContext(ctx).Model(&models.FiscalRegister{}).
		Select(fmt.Sprintf("%s as value", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&duplicateValues).Error
	if err != nil {
		return 0, err
	}

	if len(duplicateValues) == 0 {
		return 0, nil
	}

	// 2. Для каждого дублирующегося значения...
	for _, item := range duplicateValues {
		var duplicates []models.FiscalRegister
		// ...получаем все записи с этим значением.
		err := s.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", field), item.Value).Find(&duplicates).Error
		if err != nil {
			s.logger.Error("Не удалось получить группу дубликатов", zap.String("field", field), zap.String("value", item.Value), zap.Error(err))
			continue
		}

		// 3. Сортируем их, чтобы самая свежая запись была первой.
		sort.Slice(duplicates, func(i, j int) bool {
			// Если дата nil, считаем ее "старой"
			if duplicates[i].LastModifiedDate == nil {
				return false
			}
			if duplicates[j].LastModifiedDate == nil {
				return true
			}
			return duplicates[i].LastModifiedDate.After(*duplicates[j].LastModifiedDate)
		})

		// 4. Удаляем все записи, кроме первой (самой свежей).
		idsToDelete := make([]string, 0, len(duplicates)-1)
		for _, fr := range duplicates[1:] {
			idsToDelete = append(idsToDelete, fr.ID)
		}

		if len(idsToDelete) > 0 {
			res := s.db.WithContext(ctx).Delete(&models.FiscalRegister{}, "id IN ?", idsToDelete)
			if res.Error != nil {
				s.logger.Error("Ошибка при 'мягком удалении' дубликатов", zap.Strings("ids", idsToDelete), zap.Error(res.Error))
			} else {
				deletedCount += int(res.RowsAffected)
			}
		}
	}

	return deletedCount, nil
}

// CleanupServerDuplicatesAndJunk ищет и удаляет дубликаты по IP и "мусорные" записи серверов.
func (s *cleanupServiceImpl) CleanupServerDuplicatesAndJunk(ctx context.Context) {
	log := s.logger.With(zap.String("service", "CleanupService"))
	log.Info("Запуск очистки дубликатов и мусорных записей серверов...")

	// Этап 1: Очистка дубликатов по полю IP
	duplicatesDeleted, err := s.cleanupServerDuplicates(ctx, log)
	if err != nil {
		log.Error("Ошибка при очистке дубликатов серверов", zap.Error(err))
	} else if duplicatesDeleted > 0 {
		log.Info("Удалено дубликатов серверов", zap.Int("count", duplicatesDeleted))
	}

	// Этап 2: Очистка "мусорных" записей
	junkDeleted, err := s.cleanupJunkServers(ctx, log)
	if err != nil {
		log.Error("Ошибка при очистке мусорных записей серверов", zap.Error(err))
	} else if junkDeleted > 0 {
		log.Info("Удалено мусорных записей серверов", zap.Int("count", junkDeleted))
	}

	log.Info("Очистка серверов завершена.", zap.Int("total_deleted", duplicatesDeleted+junkDeleted))
}

// cleanupServerDuplicates находит и удаляет дубликаты серверов по полю `ip`.
func (s *cleanupServiceImpl) cleanupServerDuplicates(ctx context.Context, log *zap.Logger) (int, error) {
	var deletedCount int

	// 1. Находим все IP, которые встречаются более одного раза.
	var duplicateValues []struct{ Value string }
	err := s.db.WithContext(ctx).Model(&models.Server{}).
		Select("ip as value").
		Where("ip IS NOT NULL AND ip != ''").
		Group("ip").
		Having("count(*) > 1").
		Find(&duplicateValues).Error
	if err != nil {
		return 0, err
	}

	if len(duplicateValues) == 0 {
		return 0, nil
	}

	// 2. Для каждого дублирующегося IP...
	for _, item := range duplicateValues {
		var duplicates []models.Server
		if err := s.db.WithContext(ctx).Where("ip = ?", item.Value).Find(&duplicates).Error; err != nil {
			log.Error("Не удалось получить группу дубликатов серверов", zap.String("ip", item.Value), zap.Error(err))
			continue
		}

		// 3. Сортируем, чтобы самая свежая запись была первой.
		sort.Slice(duplicates, func(i, j int) bool {
			if duplicates[i].LastModifiedDate == nil {
				return false
			}
			if duplicates[j].LastModifiedDate == nil {
				return true
			}
			return duplicates[i].LastModifiedDate.After(*duplicates[j].LastModifiedDate)
		})

		mainRecord := duplicates[0]
		recordsToDelete := duplicates[1:]
		idsToDelete := make([]string, 0, len(recordsToDelete))
		for _, rec := range recordsToDelete {
			idsToDelete = append(idsToDelete, rec.ID)
		}

		// 4. "Мягко" удаляем все, кроме основной записи.
		if len(idsToDelete) > 0 {
			res := s.db.WithContext(ctx).Delete(&models.Server{}, "id IN ?", idsToDelete)
			if res.Error == nil {
				deletedCount += int(res.RowsAffected)
				// 5. Создаем задачи на удаление в ServiceDesk.
				for _, rec := range recordsToDelete {
					s.createServerCleanupTask(ctx, rec, mainRecord, "duplicate")
				}
			}
		}
	}
	return deletedCount, nil
}

// cleanupJunkServers находит и удаляет "мусорные" серверы.
func (s *cleanupServiceImpl) cleanupJunkServers(ctx context.Context, _ *zap.Logger) (int, error) {
	var junkServers []models.Server
	// Ищем записи, где все ключевые поля пустые.
	err := s.db.WithContext(ctx).Where(
		"(ip IS NULL OR ip = '') AND " +
			"(teamviewer IS NULL OR teamviewer = '') AND " +
			"(anydesk IS NULL OR anydesk = '') AND " +
			"(litemanager IS NULL OR litemanager = '') AND " +
			"(rdp IS NULL OR rdp = '') AND " +
			"(description IS NULL OR description = '')",
	).Find(&junkServers).Error

	if err != nil {
		return 0, err
	}
	if len(junkServers) == 0 {
		return 0, nil
	}

	idsToDelete := make([]string, 0, len(junkServers))
	for _, server := range junkServers {
		idsToDelete = append(idsToDelete, server.ID)
	}

	res := s.db.WithContext(ctx).Delete(&models.Server{}, "id IN ?", idsToDelete)
	if res.Error == nil {
		for _, server := range junkServers {
			s.createServerCleanupTask(ctx, server, models.Server{}, "junk")
		}
		return int(res.RowsAffected), nil
	}

	return 0, res.Error
}

// createServerCleanupTask создает задачу на удаление сущности из ServiceDesk.
func (s *cleanupServiceImpl) createServerCleanupTask(ctx context.Context, serverToDelete, mainRecord models.Server, reason string) {
	var comment string
	detailsMap := map[string]string{
		"deletedServiceDeskUUID": utils.SafeStringDereference(serverToDelete.ServiceDeskUUID),
		"deletedInternalID":      serverToDelete.ID,
	}

	if reason == "duplicate" {
		comment = fmt.Sprintf("Задача на удаление дубликата из ServiceDesk. Эта запись является дубликатом записи с UUID %s по полю 'ip'.", utils.SafeStringDereference(mainRecord.ServiceDeskUUID))
		detailsMap["mainRecordServiceDeskUUID"] = utils.SafeStringDereference(mainRecord.ServiceDeskUUID)
	} else {
		comment = "Задача на удаление 'мусорной' записи из ServiceDesk. Запись не содержит IP, данных удаленного доступа или описания."
	}

	details, _ := json.Marshal(detailsMap)

	task := models.ReconciliationTask{
		TaskType:   "delete_from_servicedesk",
		EntityType: "Server",
		EntityUUID: utils.SafeStringDereference(serverToDelete.ServiceDeskUUID),
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    comment,
	}
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		s.logger.Error("Не удалось создать задачу на удаление из SD", zap.String("uuid", task.EntityUUID), zap.Error(err))
	}
}
