package gateways

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	RefreshLatestReport(ctx context.Context) error
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
	if interval <= 0 {
		interval = 12 * time.Hour
	}

	g.logger.Info(
		"Запуск воркера актуализации контрактов из почты",
		"interval", interval,
		"bitrix_dry_run", g.cfg != nil && !g.cfg.BitrixWebhookEnabled,
	)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

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

func (g *contractGatewayImpl) RefreshLatestReport(ctx context.Context) error {
	if err := g.ensureReady(); err != nil {
		return err
	}

	g.logger.Info("Запущен принудительный пересчёт контрактов по текущему состоянию почтового ящика")
	reports, err := g.fetchSortedReports(ctx)
	if err != nil {
		return err
	}
	if len(reports) == 0 {
		g.logger.Info("Принудительный пересчёт контрактов завершён: подходящие отчёты в почте не найдены")
		return nil
	}

	selectedReports := selectLatestReportsBySource(reports)
	report := buildCombinedContractMailReport(selectedReports)
	g.logger.Info(
		"Запущена принудительная обработка актуальных почтовых отчётов по контрактам",
		"attachment_name", report.AttachmentName,
		"attachment_hash", report.AttachmentHash,
		"message_id", report.MessageID,
		"received_at", report.ReceivedAt,
		"extracted_service_points", len(report.Rows),
		"reports_merged", len(selectedReports),
	)
	if err := g.applyReport(ctx, report); err != nil {
		g.logger.Error("Не удалось выполнить принудительный пересчёт по актуальным почтовым отчётам", "attachment_name", report.AttachmentName, "error", err)
		for _, selectedReport := range selectedReports {
			g.storeMailImportStatus(ctx, selectedReport, contractdom.MailImportStatusFailed, err)
		}
		return err
	}

	for _, selectedReport := range selectedReports {
		g.storeMailImportStatus(ctx, selectedReport, contractdom.MailImportStatusProcessed, nil)
	}
	g.logger.Info(
		"Принудительный пересчёт контрактов по актуальным почтовым отчётам завершён",
		"attachment_name", report.AttachmentName,
		"attachment_hash", report.AttachmentHash,
		"reports_merged", len(selectedReports),
	)
	return nil
}

// sync выполняет один цикл получения отчетов из почты и их последовательной обработки.
func (g *contractGatewayImpl) sync(ctx context.Context) {
	if err := g.ensureReady(); err != nil {
		g.logger.Error("Воркер актуализации контрактов инициализирован не полностью", "error", err)
		return
	}
	g.logger.Info("Запущен цикл актуализации контрактов из почты")

	reports, err := g.fetchSortedReports(ctx)
	if err != nil {
		g.logger.Error("Не удалось получить отчёты по контрактам из почты", "error", err)
		return
	}
	if len(reports) == 0 {
		g.logger.Info("Новых отчётов по контрактам в почте не найдено")
		return
	}
	g.logger.Info("Получены отчёты по контрактам из почты", "reports_count", len(reports))

	selectedReports := selectLatestReportsBySource(reports)
	reportsToMark := make([]contractsvc.ContractMailReport, 0, len(selectedReports))
	for _, report := range selectedReports {
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
				"Актуальный отчёт по контрактам для источника уже обработан ранее",
				"attachment_name", report.AttachmentName,
				"attachment_hash", report.AttachmentHash,
				"message_id", report.MessageID,
			)
			continue
		}
		reportsToMark = append(reportsToMark, report)
	}
	if len(reportsToMark) == 0 {
		g.logger.Info("Актуальные отчёты по контрактам уже обработаны ранее", "sources_count", len(selectedReports))
		return
	}

	combinedReport := buildCombinedContractMailReport(selectedReports)
	g.logger.Info(
		"Начата обработка актуального набора почтовых отчётов по контрактам",
		"attachment_name", combinedReport.AttachmentName,
		"attachment_hash", combinedReport.AttachmentHash,
		"message_id", combinedReport.MessageID,
		"extracted_service_points", len(combinedReport.Rows),
		"reports_merged", len(selectedReports),
	)
	if err := g.applyReport(ctx, combinedReport); err != nil {
		g.logger.Error("Не удалось применить актуальный набор почтовых отчётов по контрактам", "attachment_name", combinedReport.AttachmentName, "error", err)
		for _, report := range reportsToMark {
			g.storeMailImportStatus(ctx, report, contractdom.MailImportStatusFailed, err)
		}
		return
	}

	for _, report := range reportsToMark {
		g.storeMailImportStatus(ctx, report, contractdom.MailImportStatusProcessed, nil)
	}
	g.maybeRunAutomaticBitrixSync(ctx, combinedReport, selectedReports)
	g.logger.Info("Цикл актуализации контрактов из почты завершён", "reports_count", len(reports))
}

func (g *contractGatewayImpl) ensureReady() error {
	switch {
	case g.mailbox == nil:
		return fmt.Errorf("не настроен клиент чтения почтовых отчётов")
	case g.bitrixSync == nil:
		return fmt.Errorf("не настроен сервис синхронизации Bitrix24")
	case g.contractSvc == nil:
		return fmt.Errorf("не настроен сервис контрактов")
	case g.contractRepo == nil:
		return fmt.Errorf("не настроен репозиторий контрактов")
	case g.bitrixRepo == nil:
		return fmt.Errorf("не настроен репозиторий Bitrix24")
	default:
		return nil
	}
}

func (g *contractGatewayImpl) fetchSortedReports(ctx context.Context) ([]contractsvc.ContractMailReport, error) {
	reports, err := g.mailbox.FetchReports(ctx)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить отчёты по контрактам из почты: %w", err)
	}
	slices.SortFunc(reports, compareContractMailReports)
	return reports, nil
}

func compareContractMailReports(a, b contractsvc.ContractMailReport) int {
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
}

// applyReport пересчитывает контракты компаний по последнему отчету, не меняя Bitrix24 автоматически.
func (g *contractGatewayImpl) applyReport(ctx context.Context, report contractsvc.ContractMailReport) error {
	snapshots, stats, err := g.buildDailySnapshots(ctx, report.AttachmentHash, report.Rows)
	if err != nil {
		return err
	}

	if err := g.contractSvc.SyncDailySnapshots(ctx, snapshots); err != nil {
		return err
	}

	g.logger.Info(
		"Почтовый отчёт по контрактам применён",
		"attachment_name", report.AttachmentName,
		"extracted_service_points", len(report.Rows),
		"extracted_unique_contractors", countUniqueContractors(report.Rows),
		"matched_report_rows", stats.MatchedRows,
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
	MatchedRows       int
}

// buildDailySnapshots превращает текущие mapping компании к точке в снимок контрактов по последнему отчету.
func (g *contractGatewayImpl) buildDailySnapshots(
	ctx context.Context,
	sourceHash string,
	rows []contractsvc.ContractReportRow,
) ([]contractdom.DailyCompanyContractSnapshot, contractSnapshotBuildStats, error) {
	mappings, err := g.bitrixRepo.ListCompanyServicePointMappings(ctx)
	if err != nil {
		return nil, contractSnapshotBuildStats{}, err
	}
	if len(mappings) == 0 {
		return []contractdom.DailyCompanyContractSnapshot{}, contractSnapshotBuildStats{}, nil
	}

	pointIDs := make([]int64, 0, len(mappings))
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

	rowsByCode, rowsByName := buildReportRowIndexes(rows)
	snapshots := make([]contractdom.DailyCompanyContractSnapshot, 0, len(mappings))
	stats := contractSnapshotBuildStats{TotalMappings: len(mappings)}
	for _, mapping := range mappings {
		snapshot := contractdom.DailyCompanyContractSnapshot{
			CompanyID:      mapping.CompanyID,
			ServicePointID: mapping.BitrixServicePointID,
			SourceHash:     sourceHash,
		}

		if point, ok := pointsByID[mapping.BitrixServicePointID]; ok {
			pointContractType := contractsvc.NormalizeServicePointContractType(dereferenceString(point.ContractType))
			pointContractKnown := point.ContractOn != nil || pointContractType != ""
			snapshot.ServicePointName = point.Name
			snapshot.ServicePointCode = dereferenceString(point.OneCCode)
			snapshot.ContractorID = dereferenceString(point.OneCCode)
			snapshot.ContractType = pointContractType
			snapshot.Active = contractsvc.IsServicePointContractActive(point.ContractOn, pointContractType)
			snapshot.StartDate = point.ContractStart
			snapshot.EndDate = point.ContractEnd
			snapshot.ClientOrder = dereferenceString(point.ClientOrder)

			if row, matched := matchReportRowToPoint(point, rowsByCode, rowsByName); matched {
				snapshot.ServicePointName = row.ServicePointName
				snapshot.ServicePointCode = row.ServicePointCode
				snapshot.ContractorID = row.ContractorID
				if !pointContractKnown {
					snapshot.ContractType = contractsvc.NormalizeServicePointContractType(row.ContractType)
					snapshot.Active = row.ContractOn
				}
				snapshot.StartDate = row.StartDate
				snapshot.EndDate = row.EndDate
				snapshot.ClientOrder = row.ClientOrder
				stats.MatchedRows++
			}
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

	var reportRows json.RawMessage
	if status == contractdom.MailImportStatusProcessed && len(report.Rows) > 0 {
		if encoded, err := json.Marshal(report.Rows); err == nil {
			reportRows = encoded
		}
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
		ReportRows:     datatypes.JSON(reportRows),
	}
	item.LastUpdatedBy = contractMailSyncUpdatedBy

	if err := g.contractRepo.UpsertMailImport(ctx, item); err != nil {
		g.logger.Error("Не удалось сохранить статус обработки почтового отчёта", "attachment_hash", report.AttachmentHash, "error", err)
	}
}

func buildReportRowIndexes(rows []contractsvc.ContractReportRow) (map[string]contractsvc.ContractReportRow, map[string][]contractsvc.ContractReportRow) {
	aggregated := contractsvc.AggregateContractReportRows(rows)
	rowsByCode := make(map[string]contractsvc.ContractReportRow, len(aggregated))
	rowsByName := make(map[string][]contractsvc.ContractReportRow, len(aggregated))
	for _, row := range aggregated {
		for _, code := range contractReportCodeLookupKeys(row.ServicePointCode) {
			if code == "" {
				continue
			}
			rowsByCode[code] = row
		}
		name := normalizePointName(row.ServicePointName)
		if name != "" {
			rowsByName[name] = append(rowsByName[name], row)
		}
	}
	return rowsByCode, rowsByName
}

func matchReportRowToPoint(
	point bitrix.ServicePoint,
	rowsByCode map[string]contractsvc.ContractReportRow,
	rowsByName map[string][]contractsvc.ContractReportRow,
) (contractsvc.ContractReportRow, bool) {
	if code := strings.TrimSpace(dereferenceString(point.OneCCode)); code != "" {
		if row, ok := rowsByCode[code]; ok {
			return row, true
		}
	}

	matches := rowsByName[normalizePointName(point.Name)]
	if len(matches) == 1 {
		return matches[0], true
	}

	return contractsvc.ContractReportRow{}, false
}

func normalizePointName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	return strings.Join(strings.Fields(normalized), " ")
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

func contractReportCodeLookupKeys(code string) []string {
	normalizedCode := strings.TrimSpace(code)
	if normalizedCode == "" {
		return nil
	}

	keys := []string{normalizedCode}
	lowerCode := strings.ToLower(normalizedCode)
	if stripped, ok := strings.CutPrefix(lowerCode, "ru"); ok && stripped != "" {
		keys = append(keys, normalizedCode[len(normalizedCode)-len(stripped):])
	}
	return keys
}

func selectLatestReportsBySource(reports []contractsvc.ContractMailReport) []contractsvc.ContractMailReport {
	if len(reports) == 0 {
		return nil
	}

	latestBySource := make(map[string]contractsvc.ContractMailReport, len(reports))
	for _, report := range reports {
		source := contractReportSourceKey(report.Rows)
		latestBySource[source] = report
	}

	selected := make([]contractsvc.ContractMailReport, 0, len(latestBySource))
	for _, report := range latestBySource {
		selected = append(selected, report)
	}
	slices.SortFunc(selected, compareContractMailReports)
	return selected
}

func contractReportSourceKey(rows []contractsvc.ContractReportRow) string {
	for _, row := range rows {
		if source := contractReportSourceKeyByCode(row.ServicePointCode); source != "" {
			return source
		}
		if source := contractReportSourceKeyByCode(row.ContractorID); source != "" {
			return source
		}
	}
	return "ru"
}

func contractReportSourceKeyByCode(code string) string {
	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	switch {
	case strings.HasPrefix(normalizedCode, "id"):
		return "id"
	case strings.HasPrefix(normalizedCode, "ru"):
		return "ru"
	case normalizedCode == "":
		return ""
	default:
		return "ru"
	}
}

func buildCombinedContractMailReport(reports []contractsvc.ContractMailReport) contractsvc.ContractMailReport {
	if len(reports) == 0 {
		return contractsvc.ContractMailReport{}
	}

	selected := slices.Clone(reports)
	slices.SortFunc(selected, compareContractMailReports)

	mergedRows := make([]contractsvc.ContractReportRow, 0, 1024)
	hashBuilder := strings.Builder{}
	names := make([]string, 0, len(selected))
	for _, report := range selected {
		mergedRows = append(mergedRows, report.Rows...)
		if strings.TrimSpace(report.AttachmentHash) != "" {
			hashBuilder.WriteString(report.AttachmentHash)
			hashBuilder.WriteString("|")
		}
		names = append(names, strings.TrimSpace(report.AttachmentName))
	}
	mergedRows = contractsvc.AggregateContractReportRows(mergedRows)

	latestReport := selected[len(selected)-1]
	combinedHash := sha256.Sum256([]byte(hashBuilder.String()))
	return contractsvc.ContractMailReport{
		MessageID:      latestReport.MessageID,
		Subject:        latestReport.Subject,
		ReceivedAt:     latestReport.ReceivedAt,
		AttachmentName: strings.Join(names, " + "),
		AttachmentHash: hex.EncodeToString(combinedHash[:]),
		Rows:           mergedRows,
	}
}

func (g *contractGatewayImpl) maybeRunAutomaticBitrixSync(
	ctx context.Context,
	report contractsvc.ContractMailReport,
	activeReports []contractsvc.ContractMailReport,
) {
	if g.cfg == nil || !g.cfg.EnableContractBitrixAutoSync || g.bitrixSync == nil {
		return
	}

	startedAt := time.Now().UTC()
	preview, err := g.bitrixSync.PreviewContractReportSync(ctx, report.Rows)
	if err != nil {
		completedAt := time.Now().UTC()
		g.logger.Error("Автоматическая синхронизация точек по отчёту не смогла построить preview", "error", err)
		g.storeAutomaticBitrixSyncRun(ctx, automaticBitrixSyncRunInput{
			Status:        contractdom.ServicePointSyncRunStatusFailed,
			ActiveReports: activeReports,
			Preview:       nil,
			QueueItems:    nil,
			Result:        nil,
			StartedAt:     startedAt,
			CompletedAt:   &completedAt,
			Note:          err.Error(),
		})
		return
	}

	selectedQueueItems, noteParts := selectAutomaticContractSyncQueueItems(preview, g.cfg.ContractBitrixAutoSyncApplyDeletes)
	if len(selectedQueueItems) == 0 {
		completedAt := time.Now().UTC()
		if len(noteParts) == 0 {
			noteParts = append(noteParts, "Автоматически применять нечего")
		}
		g.storeAutomaticBitrixSyncRun(ctx, automaticBitrixSyncRunInput{
			Status:        contractdom.ServicePointSyncRunStatusSkipped,
			ActiveReports: activeReports,
			Preview:       preview,
			QueueItems:    nil,
			Result:        nil,
			StartedAt:     startedAt,
			CompletedAt:   &completedAt,
			Note:          strings.Join(noteParts, ". "),
		})
		return
	}

	result, execErr := g.bitrixSync.ExecuteContractReportSync(ctx, report.Rows, services.ContractReportSyncExecuteOptions{
		SelectedKeys: contractSyncQueueItemKeys(selectedQueueItems),
		QueueItems:   selectedQueueItems,
	})
	completedAt := time.Now().UTC()
	status := contractdom.ServicePointSyncRunStatusSuccess
	note := strings.Join(noteParts, ". ")
	if execErr != nil {
		status = contractdom.ServicePointSyncRunStatusFailed
		if note == "" {
			note = execErr.Error()
		} else {
			note = note + ". " + execErr.Error()
		}
		g.logger.Error("Автоматическая синхронизация точек по отчёту завершилась ошибкой", "error", execErr)
	} else if len(result.Errors) > 0 || len(result.ErrorDetails) > 0 {
		status = contractdom.ServicePointSyncRunStatusPartial
		g.logger.Warn("Автоматическая синхронизация точек по отчёту завершилась с частичными ошибками", "errors", len(result.Errors), "error_details", len(result.ErrorDetails))
	}

	g.storeAutomaticBitrixSyncRun(ctx, automaticBitrixSyncRunInput{
		Status:        status,
		ActiveReports: activeReports,
		Preview:       preview,
		QueueItems:    selectedQueueItems,
		Result:        result,
		StartedAt:     startedAt,
		CompletedAt:   &completedAt,
		Note:          note,
	})
}

type automaticBitrixSyncRunInput struct {
	Status        string
	ActiveReports []contractsvc.ContractMailReport
	Preview       *services.ContractReportSyncPreview
	QueueItems    []services.ContractReportSyncPlanItem
	Result        *services.ContractReportSyncExecuteResult
	StartedAt     time.Time
	CompletedAt   *time.Time
	Note          string
}

func (g *contractGatewayImpl) storeAutomaticBitrixSyncRun(ctx context.Context, input automaticBitrixSyncRunInput) {
	if g.contractRepo == nil {
		return
	}

	activeImportsJSON, err := marshalAutomaticBitrixSyncActiveReports(input.ActiveReports)
	if err != nil {
		g.logger.Error("Не удалось сериализовать активные отчёты для журнала автоматической синхронизации", "error", err)
		return
	}
	queueItemsJSON, err := marshalAutomaticBitrixSyncQueueItems(input.QueueItems)
	if err != nil {
		g.logger.Error("Не удалось сериализовать очередь для журнала автоматической синхронизации", "error", err)
		return
	}
	errorsJSON, err := marshalAutomaticBitrixSyncErrors(input.Result)
	if err != nil {
		g.logger.Error("Не удалось сериализовать ошибки для журнала автоматической синхронизации", "error", err)
		return
	}
	errorDetailsJSON, err := marshalAutomaticBitrixSyncErrorDetails(input.Result)
	if err != nil {
		g.logger.Error("Не удалось сериализовать детали ошибок для журнала автоматической синхронизации", "error", err)
		return
	}

	run := &contractdom.ServicePointSyncRun{
		Mode:          contractdom.ServicePointSyncRunModeAutomatic,
		Status:        strings.TrimSpace(input.Status),
		ActorType:     contractdom.ServicePointSyncRunActorSystem,
		ActorName:     nullableString("Система"),
		Note:          nullableString(strings.TrimSpace(input.Note)),
		StartedAt:     input.StartedAt,
		CompletedAt:   input.CompletedAt,
		ActiveImports: datatypes.JSON(activeImportsJSON),
		QueueItems:    datatypes.JSON(queueItemsJSON),
		Errors:        datatypes.JSON(errorsJSON),
		ErrorDetails:  datatypes.JSON(errorDetailsJSON),
	}
	if input.Preview != nil {
		run.ReportRows = input.Preview.ReportRows
		run.ToCreate = input.Preview.ToCreate
		run.ToUpdate = input.Preview.ToUpdate
		run.ToDelete = input.Preview.ToDelete
		run.BlockedRows = input.Preview.BlockedRows
	}
	if input.Result != nil {
		run.Processed = input.Result.Processed
		run.Created = input.Result.Created
		run.Updated = input.Result.Updated
		run.Deleted = input.Result.Deleted
	}
	run.LastUpdatedBy = "contract_sync_auto"

	if err := g.contractRepo.CreateServicePointSyncRun(ctx, run); err != nil {
		g.logger.Error("Не удалось сохранить журнал автоматической синхронизации точек обслуживания", "error", err)
	}
}

func selectAutomaticContractSyncQueueItems(
	preview *services.ContractReportSyncPreview,
	allowDeletes bool,
) ([]services.ContractReportSyncPlanItem, []string) {
	if preview == nil {
		return nil, nil
	}

	noteParts := make([]string, 0, 3)
	if preview.BlockedRows > 0 {
		noteParts = append(noteParts, fmt.Sprintf("Заблокированных строк осталось %d", preview.BlockedRows))
	}
	if preview.ToDelete > 0 && !allowDeletes {
		noteParts = append(noteParts, fmt.Sprintf("Удаления (%d) оставлены на ручное подтверждение", preview.ToDelete))
	}

	queueItems := slices.Clone(preview.UpsertItems)
	if allowDeletes && len(preview.DeleteItems) > 0 {
		queueItems = append(queueItems, preview.DeleteItems...)
	}
	return queueItems, noteParts
}

func contractSyncQueueItemKeys(items []services.ContractReportSyncPlanItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func marshalAutomaticBitrixSyncActiveReports(items []contractsvc.ContractMailReport) ([]byte, error) {
	snapshots := make([]contractdom.ServicePointSyncRunImportSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = append(snapshots, contractdom.ServicePointSyncRunImportSnapshot{
			Source:         contractReportSourceKey(item.Rows),
			MessageID:      strings.TrimSpace(item.MessageID),
			AttachmentName: strings.TrimSpace(item.AttachmentName),
			AttachmentHash: strings.TrimSpace(item.AttachmentHash),
			ReceivedAt:     item.ReceivedAt,
			Status:         contractdom.MailImportStatusProcessed,
			RowsCount:      len(contractsvc.AggregateContractReportRows(item.Rows)),
		})
	}
	return json.Marshal(snapshots)
}

func marshalAutomaticBitrixSyncQueueItems(items []services.ContractReportSyncPlanItem) ([]byte, error) {
	snapshots := make([]contractdom.ServicePointSyncRunQueueItemSnapshot, 0, len(items))
	for _, item := range items {
		snapshots = append(snapshots, contractdom.ServicePointSyncRunQueueItemSnapshot{
			Key:                 item.Key,
			Action:              string(item.Action),
			ServicePointName:    item.ServicePointName,
			ServicePointCode:    item.ServicePointCode,
			ContractorID:        item.ContractorID,
			ContractorName:      item.ContractorName,
			ContractType:        item.ContractType,
			B24ElementID:        item.B24ElementID,
			CurrentName:         item.CurrentName,
			CurrentCode:         item.CurrentCode,
			CurrentContractType: item.CurrentContractType,
			ChangeSet:           mapAutomaticBitrixSyncFieldDiffs(item.ChangeSet),
			MatchedPointIDs:     item.MatchedPointIDs,
			FilledFields:        item.FilledFields,
			IsMapped:            item.IsMapped,
			Reason:              item.Reason,
		})
	}
	return json.Marshal(snapshots)
}

func mapAutomaticBitrixSyncFieldDiffs(items []services.ContractReportSyncFieldDiff) []contractdom.ServicePointSyncRunFieldDiffSnapshot {
	result := make([]contractdom.ServicePointSyncRunFieldDiffSnapshot, 0, len(items))
	for _, item := range items {
		result = append(result, contractdom.ServicePointSyncRunFieldDiffSnapshot{
			Field:        item.Field,
			Label:        item.Label,
			CurrentValue: item.CurrentValue,
			NextValue:    item.NextValue,
		})
	}
	return result
}

func marshalAutomaticBitrixSyncErrors(result *services.ContractReportSyncExecuteResult) ([]byte, error) {
	if result == nil || len(result.Errors) == 0 {
		return json.Marshal([]string{})
	}
	return json.Marshal(result.Errors)
}

func marshalAutomaticBitrixSyncErrorDetails(result *services.ContractReportSyncExecuteResult) ([]byte, error) {
	if result == nil || len(result.ErrorDetails) == 0 {
		return json.Marshal([]contractdom.ServicePointSyncRunErrorDetailSnapshot{})
	}

	items := make([]contractdom.ServicePointSyncRunErrorDetailSnapshot, 0, len(result.ErrorDetails))
	for _, item := range result.ErrorDetails {
		items = append(items, contractdom.ServicePointSyncRunErrorDetailSnapshot{
			Key:              item.Key,
			Action:           string(item.Action),
			ServicePointName: item.ServicePointName,
			ServicePointCode: item.ServicePointCode,
			B24ElementID:     item.B24ElementID,
			Message:          item.Message,
		})
	}
	return json.Marshal(items)
}
