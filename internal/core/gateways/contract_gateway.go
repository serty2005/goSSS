package gateways

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"etalon-server/internal/domain/bitrix"
	contractdom "etalon-server/internal/domain/contract"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	contractsvc "etalon-server/internal/services/contract"

	"gorm.io/datatypes"
)

type ContractGateway interface {
	Start(ctx context.Context)
}

type contractGatewayImpl struct {
	cfg          *config.Config
	logger       logger.LoggerInterface
	mailbox      contractsvc.ContractMailboxClient
	bitrixSync   services.BitrixSyncService
	bitrixRepo   bitrix.Repository
	contractSvc  contractdom.Service
	contractRepo contractdom.Repository
}

// NewContractGateway создает воркер актуализации контрактов из почтовой рассылки 1С.
func NewContractGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	mailbox contractsvc.ContractMailboxClient,
	bitrixSync services.BitrixSyncService,
	bitrixRepo bitrix.Repository,
	contractSvc contractdom.Service,
	contractRepo contractdom.Repository,
) ContractGateway {
	return &contractGatewayImpl{
		cfg:          cfg,
		logger:       logger,
		mailbox:      mailbox,
		bitrixSync:   bitrixSync,
		bitrixRepo:   bitrixRepo,
		contractSvc:  contractSvc,
		contractRepo: contractRepo,
	}
}

// Start запускает периодический цикл чтения почты и применения ежедневного отчета.
func (g *contractGatewayImpl) Start(ctx context.Context) {
	interval := g.cfg.ContractSyncInterval
	if interval < time.Hour {
		interval = time.Hour
	}

	g.logger.Info(
		"Запуск воркера актуализации контрактов из почты",
		"interval", interval,
		"bitrix_dry_run", g.cfg != nil && !g.cfg.BitrixWebhookEnabled,
	)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.sync(ctx)

	for {
		select {
		case <-ticker.C:
			g.sync(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка воркера актуализации контрактов.")
			return
		}
	}
}

// sync выполняет один цикл получения отчетов из почты и их последовательной обработки.
func (g *contractGatewayImpl) sync(ctx context.Context) {
	if g.mailbox == nil || g.bitrixSync == nil || g.contractSvc == nil || g.contractRepo == nil || g.bitrixRepo == nil {
		g.logger.Error("Воркер актуализации контрактов инициализирован не полностью")
		return
	}
	g.logger.Info("Запущен цикл актуализации контрактов из почты")

	reports, err := g.mailbox.FetchReports(ctx)
	if err != nil {
		g.logger.Error("Не удалось получить отчёты по контрактам из почты", "error", err)
		return
	}
	if len(reports) == 0 {
		g.logger.Info("Новых отчётов по контрактам в почте не найдено")
		return
	}
	g.logger.Info("Получены отчёты по контрактам из почты", "reports_count", len(reports))

	slices.SortFunc(reports, func(a, b contractsvc.ContractMailReport) int {
		aTime := time.Time{}
		if a.ReceivedAt != nil {
			aTime = *a.ReceivedAt
		}
		bTime := time.Time{}
		if b.ReceivedAt != nil {
			bTime = *b.ReceivedAt
		}
		if aTime.Before(bTime) {
			return -1
		}
		if aTime.After(bTime) {
			return 1
		}
		return strings.Compare(a.AttachmentHash, b.AttachmentHash)
	})

	for _, report := range reports {
		if strings.TrimSpace(report.AttachmentHash) == "" {
			continue
		}

		existing, err := g.contractRepo.GetMailImportByAttachmentHash(ctx, report.AttachmentHash)
		if err != nil {
			g.logger.Error("Не удалось проверить историю обработки почтового отчёта", "attachment_hash", report.AttachmentHash, "error", err)
			continue
		}
		if existing != nil && existing.Status == contractdom.MailImportStatusProcessed {
			g.logger.Info(
				"Отчёт по контрактам уже обработан ранее",
				"attachment_name", report.AttachmentName,
				"attachment_hash", report.AttachmentHash,
				"message_id", report.MessageID,
			)
			continue
		}

		g.logger.Info(
			"Начата обработка почтового отчёта по контрактам",
			"attachment_name", report.AttachmentName,
			"attachment_hash", report.AttachmentHash,
			"message_id", report.MessageID,
			"extracted_service_points", len(report.Rows),
		)
		if err := g.applyReport(ctx, report); err != nil {
			g.logger.Error("Не удалось применить почтовый отчёт по контрактам", "attachment_name", report.AttachmentName, "error", err)
			g.storeMailImportStatus(ctx, report, contractdom.MailImportStatusFailed, err)
			continue
		}

		g.storeMailImportStatus(ctx, report, contractdom.MailImportStatusProcessed, nil)
	}
	g.logger.Info("Цикл актуализации контрактов из почты завершён", "reports_count", len(reports))
}

// applyReport синхронизирует точки Bitrix24, сохраняет конфликты и пересчитывает контракты компаний.
func (g *contractGatewayImpl) applyReport(ctx context.Context, report contractsvc.ContractMailReport) error {
	syncResult, err := g.bitrixSync.SyncServicePointsFromDailyReport(ctx, report.Rows)
	if err != nil {
		return err
	}

	if err := g.replaceConflicts(ctx, report, syncResult.Conflicts); err != nil {
		return err
	}
	g.logger.Info(
		"Конфликты точек обслуживания обновлены",
		"attachment_name", report.AttachmentName,
		"conflicts_count", len(syncResult.Conflicts),
	)

	snapshots, stats, err := g.buildDailySnapshots(ctx, report.AttachmentHash, syncResult.Resolved)
	if err != nil {
		return err
	}

	if err := g.contractSvc.SyncDailySnapshots(ctx, snapshots); err != nil {
		return err
	}

	g.logger.Info(
		"Почтовый отчёт по контрактам применён",
		"attachment_name", report.AttachmentName,
		"processed_rows", syncResult.Processed,
		"extracted_service_points", len(report.Rows),
		"extracted_unique_contractors", countUniqueContractors(report.Rows),
		"planned_created_points", syncResult.Created,
		"planned_updated_points", syncResult.Updated,
		"applied_created_points", syncResult.AppliedCreated,
		"applied_updated_points", syncResult.AppliedUpdated,
		"uploaded_to_bitrix", syncResult.AppliedCreated+syncResult.AppliedUpdated,
		"bitrix_dry_run", syncResult.DryRun,
		"conflicts", len(syncResult.Conflicts),
		"deletion_candidates", countConflictDeletionCandidates(syncResult.Conflicts),
		"mapped_companies", stats.MappedCompanies,
		"unmapped_companies", stats.UnmappedCompanies,
		"available_mappings", stats.TotalMappings,
		"company_snapshots", len(snapshots),
	)

	return nil
}

// replaceConflicts полностью заменяет список актуальных конфликтов точек обслуживания.
func (g *contractGatewayImpl) replaceConflicts(ctx context.Context, report contractsvc.ContractMailReport, conflicts []services.ServicePointContractConflict) error {
	items := make([]contractdom.ServicePointSyncConflict, 0, len(conflicts))
	for _, conflict := range conflicts {
		details, err := json.Marshal(map[string]any{
			"matched_point_ids":      conflict.MatchedPointIDs,
			"mapped_point_ids":       conflict.MappedPointIDs,
			"deletion_candidate_ids": conflict.DeletionCandidateIDs,
		})
		if err != nil {
			return err
		}
		messageID := strings.TrimSpace(report.MessageID)
		attachmentHash := strings.TrimSpace(report.AttachmentHash)
		contractorID := strings.TrimSpace(conflict.ContractorID)
		items = append(items, contractdom.ServicePointSyncConflict{
			ConflictType:     conflict.ConflictType,
			ServicePointName: strings.TrimSpace(conflict.ServicePointName),
			ContractorID:     nullableString(contractorID),
			MessageID:        nullableString(messageID),
			AttachmentHash:   nullableString(attachmentHash),
			Details:          datatypes.JSON(details),
		})
	}
	return g.contractRepo.ReplaceServicePointSyncConflicts(ctx, items)
}

type contractSnapshotBuildStats struct {
	TotalMappings     int
	MappedCompanies   int
	UnmappedCompanies int
}

// buildDailySnapshots превращает текущие mapping компании к точке в снимок контрактов для доменного сервиса.
func (g *contractGatewayImpl) buildDailySnapshots(
	ctx context.Context,
	sourceHash string,
	resolved []services.ServicePointContractResolution,
) ([]contractdom.DailyCompanyContractSnapshot, contractSnapshotBuildStats, error) {
	mappings, err := g.bitrixRepo.ListCompanyServicePointMappings(ctx)
	if err != nil {
		return nil, contractSnapshotBuildStats{}, err
	}
	if len(mappings) == 0 {
		return []contractdom.DailyCompanyContractSnapshot{}, contractSnapshotBuildStats{}, nil
	}

	resolvedByPointID := make(map[int64]services.ServicePointContractResolution, len(resolved))
	pointIDs := make([]int64, 0, len(mappings))
	for _, item := range resolved {
		resolvedByPointID[item.B24ElementID] = item
	}
	for _, mapping := range mappings {
		pointIDs = append(pointIDs, mapping.BitrixServicePointID)
	}

	points, err := g.bitrixRepo.ListServicePointsByIDs(ctx, pointIDs)
	if err != nil {
		return nil, contractSnapshotBuildStats{}, err
	}
	pointsByID := make(map[int64]bitrix.ServicePoint, len(points))
	for _, point := range points {
		pointsByID[point.B24ElementID] = point
	}

	snapshots := make([]contractdom.DailyCompanyContractSnapshot, 0, len(mappings))
	stats := contractSnapshotBuildStats{TotalMappings: len(mappings)}
	for _, mapping := range mappings {
		snapshot := contractdom.DailyCompanyContractSnapshot{
			CompanyID:      mapping.CompanyID,
			ServicePointID: mapping.BitrixServicePointID,
			SourceHash:     sourceHash,
		}

		if resolvedPoint, ok := resolvedByPointID[mapping.BitrixServicePointID]; ok {
			snapshot.ServicePointName = resolvedPoint.ServicePointName
			snapshot.ContractorID = resolvedPoint.ContractorID
			snapshot.ContractType = resolvedPoint.ContractType
			snapshot.Active = resolvedPoint.ContractOn
			snapshot.StartDate = resolvedPoint.StartDate
			snapshot.EndDate = resolvedPoint.EndDate
			snapshot.ClientOrder = resolvedPoint.ClientOrder
			snapshots = append(snapshots, snapshot)
			stats.MappedCompanies++
			continue
		}

		if point, ok := pointsByID[mapping.BitrixServicePointID]; ok {
			snapshot.ServicePointName = point.Name
			snapshot.ContractorID = dereferenceString(point.OneCCode)
			snapshot.ContractType = dereferenceString(point.ContractType)
			snapshot.Active = false
			snapshot.StartDate = point.ContractStart
			snapshot.EndDate = point.ContractEnd
			snapshot.ClientOrder = dereferenceString(point.ClientOrder)
		}

		if strings.TrimSpace(snapshot.ServicePointName) != "" {
			stats.MappedCompanies++
		} else {
			stats.UnmappedCompanies++
		}
		snapshots = append(snapshots, snapshot)
	}

	return snapshots, stats, nil
}

// storeMailImportStatus фиксирует итог обработки архива в локальной истории.
func (g *contractGatewayImpl) storeMailImportStatus(ctx context.Context, report contractsvc.ContractMailReport, status string, syncErr error) {
	var errText *string
	if syncErr != nil {
		text := syncErr.Error()
		errText = &text
	}
	now := time.Now().UTC()
	item := &contractdom.MailImport{
		MessageID:      strings.TrimSpace(report.MessageID),
		AttachmentName: strings.TrimSpace(report.AttachmentName),
		AttachmentHash: strings.TrimSpace(report.AttachmentHash),
		ReceivedAt:     report.ReceivedAt,
		Status:         status,
		ErrorText:      errText,
		ProcessedAt:    &now,
	}
	item.LastUpdatedBy = contractMailSyncUpdatedBy

	if err := g.contractRepo.UpsertMailImport(ctx, item); err != nil {
		g.logger.Error("Не удалось сохранить статус обработки почтового отчёта", "attachment_hash", report.AttachmentHash, "error", err)
	}
}

// nullableString возвращает nil для пустых строк перед сохранением в БД.
func nullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// dereferenceString безопасно извлекает строку из указателя.
func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// countUniqueContractors считает количество уникальных идентификаторов контрагентов в отчете.
func countUniqueContractors(rows []contractsvc.ContractReportRow) int {
	unique := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.ContractorID)
		if key == "" {
			continue
		}
		unique[key] = struct{}{}
	}
	return len(unique)
}

// countConflictDeletionCandidates считает суммарное количество точек, попавших в список удаления.
func countConflictDeletionCandidates(conflicts []services.ServicePointContractConflict) int {
	total := 0
	for _, conflict := range conflicts {
		total += len(conflict.DeletionCandidateIDs)
	}
	return total
}

const contractMailSyncUpdatedBy = "contract_mail_sync"
