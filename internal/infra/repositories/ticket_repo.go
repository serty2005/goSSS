package repositories

import (
	"context"
	"errors"
	"strings"
	"time"

	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ticketRepo struct {
	db *gorm.DB
}

const (
	ticketTableName               = "tickets"
	ticketCompanyAlias            = "c"
	ticketParentCompanyAlias      = "parent"
	ticketHistoryAlias            = "th"
	ticketCommentAlias            = "tc"
	ticketUserAlias               = "u"
	ticketUserTableName           = "users"
	ticketCompanyJoinClause       = "LEFT JOIN companies c ON c.id = tickets.company_id"
	ticketParentCompanyJoinClause = "LEFT JOIN companies parent ON parent.id = c.parent_id"
	ticketQualifiedIDColumn       = "tickets.id"
	ticketQualifiedNumberColumn   = "tickets.number"
	ticketQualifiedSubjectColumn  = "tickets.subject"
	ticketQualifiedDescColumn     = "tickets.description"
	ticketQualifiedStatusColumn   = "tickets.status"
	ticketQualifiedCompanyColumn  = "tickets.company_id"
	ticketQualifiedCreatedAt      = "tickets.created_at"
	ticketQualifiedUpdatedAt      = "tickets.updated_at"
	ticketCompanyNameExpr         = "COALESCE(c.title, c.additional_name, tickets.company_id)"
	ticketParentNameExpr          = "COALESCE(parent.title, '')"
	ticketCompanySelectExpr       = "tickets.*, COALESCE(c.title, c.additional_name, tickets.company_id) as company_name"
	ticketNumberTextExpr          = "CAST(tickets.number AS TEXT)"
	serverStatusExpr              = "COALESCE(NULLIF(TRIM(servers.status), ''), 'unknown')"
	resolvedUserNameExpr          = "COALESCE(NULLIF(TRIM(u.full_name), ''), NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''), u.username)"
	ticketSortFieldCreatedAt      = "created_at"
	ticketSortFieldUpdatedAt      = "updated_at"
	ticketSortFieldNumber         = "number"
	ticketSortFieldStatus         = "status"
	ticketSortFieldLastActivity   = "last_activity"
)

var ticketSortableColumns = map[string]clause.Column{
	ticketSortFieldCreatedAt:    {Table: ticketTableName, Name: "created_at"},
	ticketSortFieldUpdatedAt:    {Table: ticketTableName, Name: "updated_at"},
	ticketSortFieldLastActivity: {Table: ticketTableName, Name: "updated_at"},
	ticketSortFieldNumber:       {Table: ticketTableName, Name: "number"},
	ticketSortFieldStatus:       {Table: ticketTableName, Name: "status"},
}

func NewTicketRepo(db *gorm.DB) tickets.TicketRepository {
	return &ticketRepo{db: db}
}

func (r *ticketRepo) Create(ctx context.Context, ticket *tickets.Ticket) error {
	if ticket.Number == 0 {
		var nextNumber int
		if err := r.db.WithContext(ctx).
			Raw("SELECT COALESCE(MAX(number), 0) + 1 FROM tickets").
			Scan(&nextNumber).Error; err != nil {
			return err
		}
		ticket.Number = nextNumber
	}
	desiredSyncWithBitrix := ticket.SyncWithBitrix
	if err := r.db.WithContext(ctx).Create(ticket).Error; err != nil {
		return err
	}
	if !desiredSyncWithBitrix {
		if err := r.db.WithContext(ctx).
			Model(&tickets.Ticket{}).
			Where("id = ?", ticket.ID).
			UpdateColumn("sync_with_bitrix", false).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *ticketRepo) Update(ctx context.Context, ticket *tickets.Ticket) error {
	return r.db.WithContext(ctx).Omit(clause.Associations).Save(ticket).Error
}

func (r *ticketRepo) RebindBitrixServicePoint(ctx context.Context, fromID, toID int64) (int64, error) {
	if fromID <= 0 || toID <= 0 || fromID == toID {
		return 0, nil
	}

	tx := r.db.WithContext(ctx).
		Model(&tickets.Ticket{}).
		Where("bitrix_service_point_id = ?", fromID).
		Update("bitrix_service_point_id", toID)
	if tx.Error != nil {
		return 0, tx.Error
	}
	return tx.RowsAffected, nil
}

func (r *ticketRepo) GetByID(ctx context.Context, id string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).
		Scopes(withTicketCompanyJoin).
		Select(ticketCompanySelectExpr).
		Preload("Assignee").
		Preload("Reporter").
		Where(ticketQualifiedIDColumn+" = ?", id).
		First(&ticket).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (r *ticketRepo) GetByNumber(ctx context.Context, number int) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).
		Preload("Assignee").
		Where("number = ?", number).
		First(&ticket).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &ticket, err
}

func (r *ticketRepo) GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).
		Where("service_desk_uuid = ?", sdUUID).
		First(&ticket).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &ticket, err
}

func (r *ticketRepo) Delete(ctx context.Context, ticketID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fileIDs []string
		if err := tx.Model(&tickets.TicketFileLink{}).
			Where("ticket_id = ?", ticketID).
			Pluck("file_id", &fileIDs).Error; err != nil {
			return err
		}

		if err := tx.Where("ticket_id = ?", ticketID).Delete(&tickets.TicketFileLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("entity_id = ?", ticketID).Delete(&tickets.Attachment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("ticket_id = ?", ticketID).Delete(&tickets.TicketComment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("ticket_id = ?", ticketID).Delete(&tickets.TicketHistory{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", ticketID).Delete(&tickets.Ticket{}).Error; err != nil {
			return err
		}

		uniqueFileIDs := make([]string, 0, len(fileIDs))
		seenFileIDs := make(map[string]struct{}, len(fileIDs))
		for _, fileID := range fileIDs {
			normalized := strings.TrimSpace(fileID)
			if normalized == "" {
				continue
			}
			if _, exists := seenFileIDs[normalized]; exists {
				continue
			}
			seenFileIDs[normalized] = struct{}{}
			uniqueFileIDs = append(uniqueFileIDs, normalized)
		}
		if len(uniqueFileIDs) == 0 {
			return nil
		}

		return tx.Where("id IN ?", uniqueFileIDs).
			Where("NOT EXISTS (SELECT 1 FROM ticket_file_links tfl WHERE tfl.file_id = file_assets.id)").
			Delete(&tickets.FileAsset{}).Error
	})
}

func (r *ticketRepo) Find(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, error) {
	var items []tickets.Ticket
	query := r.buildQuery(ctx, filter).
		Scopes(withTicketCompanyJoin).
		Select(ticketCompanySelectExpr)
	query = applyTicketSort(query, filter.SortBy)

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	err := query.Preload("Assignee").Preload("Reporter").Find(&items).Error
	return items, err
}

func (r *ticketRepo) AssociateAsset(ctx context.Context, ticketID, assetID, assetType string) error {
	return r.db.WithContext(ctx).Model(&tickets.Ticket{}).Where("id = ?", ticketID).Updates(map[string]interface{}{
		"asset_id":   assetID,
		"asset_type": assetType,
	}).Error
}

func (r *ticketRepo) Count(ctx context.Context, filter tickets.TicketFilter) (int64, error) {
	var count int64
	err := r.buildQuery(ctx, filter).Count(&count).Error
	return count, err
}

func (r *ticketRepo) buildQuery(ctx context.Context, filter tickets.TicketFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&tickets.Ticket{})
	return r.applyFilters(query, filter)
}

func (r *ticketRepo) applyFilters(query *gorm.DB, filter tickets.TicketFilter) *gorm.DB {
	switch strings.TrimSpace(filter.ArchiveMode) {
	case "archive":
		query = query.Where("is_archived = ?", true)
	case "all":
		// Без ограничений.
	default:
		query = query.Where("is_archived = ?", false)
	}
	if filter.CompanyID != "" {
		query = query.Where("company_id = ?", filter.CompanyID)
	}
	if len(filter.CompanyIDs) > 0 {
		query = query.Where("company_id IN ?", filter.CompanyIDs)
	}
	if filter.AssetID != nil {
		query = query.Where("asset_id = ?", *filter.AssetID)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	if len(filter.ExcludeStatuses) > 0 {
		query = query.Where("status NOT IN ?", filter.ExcludeStatuses)
	}
	if filter.UpdatedFrom != nil {
		query = query.Where("updated_at >= ?", *filter.UpdatedFrom)
	}
	if filter.UpdatedTo != nil {
		query = query.Where("updated_at <= ?", *filter.UpdatedTo)
	}
	if filter.CreatedFrom != nil {
		query = query.Where(ticketQualifiedCreatedAt+" >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		query = query.Where(ticketQualifiedCreatedAt+" <= ?", *filter.CreatedTo)
	}
	if filter.ResolvedFrom != nil || filter.ResolvedTo != nil {
		resolvedFilter := r.db.Table("ticket_histories AS "+ticketHistoryAlias).
			Select("1").
			Where(ticketHistoryAlias+".ticket_id = "+ticketQualifiedIDColumn).
			Where(ticketHistoryAlias+".field = ?", tickets.HistoryFieldStatus).
			Where(ticketHistoryAlias+".new_value = ?", tickets.StatusResolved)
		if filter.ResolvedFrom != nil {
			resolvedFilter = resolvedFilter.Where(ticketHistoryAlias+".created_at >= ?", *filter.ResolvedFrom)
		}
		if filter.ResolvedTo != nil {
			resolvedFilter = resolvedFilter.Where(ticketHistoryAlias+".created_at <= ?", *filter.ResolvedTo)
		}
		query = query.Where("EXISTS (?)", resolvedFilter)
	}
	if len(filter.AssigneeIDs) > 0 {
		query = query.Where("assignee_id IN ?", filter.AssigneeIDs)
	}
	if filter.AssigneeID != nil {
		query = query.Where("assignee_id = ?", *filter.AssigneeID)
	}
	if filter.ReporterID != nil {
		query = query.Where("reporter_id = ?", *filter.ReporterID)
	}
	if filter.SearchQuery != "" {
		q := "%" + filter.SearchQuery + "%"
		commentSearch := r.db.Table("ticket_comments AS "+ticketCommentAlias).
			Select("1").
			Where(ticketCommentAlias+".ticket_id = "+ticketQualifiedIDColumn).
			Where(ticketCommentAlias+".deleted_in_bitrix = ?", false).
			Where(ticketCommentAlias+".text ILIKE ?", q)
		query = query.Where(
			r.db.
				Where(ticketNumberTextExpr+" ILIKE ?", q).
				Or(ticketQualifiedSubjectColumn+" ILIKE ?", q).
				Or(ticketQualifiedDescColumn+" ILIKE ?", q).
				Or("EXISTS (?)", commentSearch),
		)
	}

	return query
}

func (r *ticketRepo) ArchiveStale(ctx context.Context, threshold time.Duration) (int64, error) {
	if threshold <= 0 {
		threshold = 14 * 24 * time.Hour
	}
	before := time.Now().Add(-threshold)
	updates := map[string]interface{}{
		"is_archived":      true,
		"archived_at":      time.Now(),
		"sync_with_bitrix": false,
	}
	result := r.db.WithContext(ctx).
		Model(&tickets.Ticket{}).
		Where("is_archived = ?", false).
		Where("updated_at <= ?", before).
		Updates(updates)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *ticketRepo) AddHistory(ctx context.Context, history *tickets.TicketHistory) error {
	return r.db.WithContext(ctx).Create(history).Error
}

func (r *ticketRepo) GetHistory(ctx context.Context, ticketID string) ([]tickets.TicketHistory, error) {
	var history []tickets.TicketHistory
	err := r.db.WithContext(ctx).Where("ticket_id = ?", ticketID).Order("created_at desc").Find(&history).Error
	return history, err
}

func (r *ticketRepo) AddAttachment(ctx context.Context, attachment *tickets.Attachment) error {
	return r.db.WithContext(ctx).Create(attachment).Error
}

func (r *ticketRepo) GetAttachments(ctx context.Context, ticketID string) ([]tickets.Attachment, error) {
	var attachments []tickets.Attachment
	err := r.db.WithContext(ctx).Raw(
		`SELECT
			fa.id as id,
			tfl.ticket_id as entity_id,
			'Ticket' as entity_type,
			fa.original_name as file_name,
			('/api/static/tickets/' || fa.storage_key) as file_path,
			fa.mime_type,
			fa.size,
			tfl.created_at
		FROM ticket_file_links tfl
		JOIN file_assets fa ON fa.id = tfl.file_id
		WHERE tfl.ticket_id = ? AND tfl.relation_type = ?
		ORDER BY tfl.created_at DESC`,
		ticketID,
		tickets.RelationTypeDirectTicketAttachment,
	).Scan(&attachments).Error
	return attachments, err
}

func (r *ticketRepo) UpsertFileAsset(ctx context.Context, file *tickets.FileAsset) (*tickets.FileAsset, error) {
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "storage_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"original_name", "mime_type", "size", "checksum", "updated_at"}),
	}).Create(file).Error; err != nil {
		return nil, err
	}

	var persisted tickets.FileAsset
	if err := r.db.WithContext(ctx).Where("storage_key = ?", file.StorageKey).First(&persisted).Error; err != nil {
		return nil, err
	}
	return &persisted, nil
}

func (r *ticketRepo) GetFileAssetByID(ctx context.Context, id string) (*tickets.FileAsset, error) {
	var file tickets.FileAsset
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *ticketRepo) GetFileAssetByStorageKey(ctx context.Context, storageKey string) (*tickets.FileAsset, error) {
	var file tickets.FileAsset
	err := r.db.WithContext(ctx).Where("storage_key = ?", storageKey).First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &file, nil
}

func (r *ticketRepo) UpsertTicketFileLink(ctx context.Context, link *tickets.TicketFileLink) error {
	if strings.TrimSpace(link.ID) == "" {
		link.ID = uuid.New().String()
	}
	return r.db.WithContext(ctx).Exec(
		`INSERT INTO ticket_file_links (id, ticket_id, file_id, relation_type, comment_uuid, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, NOW(), NOW())
		 ON CONFLICT (ticket_id, file_id, relation_type, COALESCE(comment_uuid, ''))
		 DO UPDATE SET updated_at = NOW()`,
		link.ID, link.TicketID, link.FileID, link.RelationType, link.CommentUUID,
	).Error
}

func (r *ticketRepo) GetTicketFileLinksByRelation(ctx context.Context, ticketID string, relationTypes []string) ([]tickets.TicketFileLink, error) {
	var links []tickets.TicketFileLink
	query := r.db.WithContext(ctx).Where("ticket_id = ?", ticketID)
	if len(relationTypes) > 0 {
		query = query.Where("relation_type IN ?", relationTypes)
	}
	err := query.Find(&links).Error
	return links, err
}

func (r *ticketRepo) AddComments(ctx context.Context, comments []tickets.TicketComment) error {
	if len(comments) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(comments, 200).Error
}

func (r *ticketRepo) GetComments(ctx context.Context, ticketID string) ([]tickets.TicketComment, error) {
	var comments []tickets.TicketComment
	err := r.db.WithContext(ctx).
		Where("ticket_id = ?", ticketID).
		Where("deleted_in_bitrix = false").
		Order("creation_date asc").
		Find(&comments).Error
	return comments, err
}

func (r *ticketRepo) GetCommentByUUID(ctx context.Context, ticketID string, commentUUID string) (*tickets.TicketComment, error) {
	id := strings.TrimSpace(commentUUID)
	if id == "" {
		return nil, nil
	}

	var comment tickets.TicketComment
	err := r.db.WithContext(ctx).
		Where("ticket_id = ?", ticketID).
		Where("deleted_in_bitrix = false").
		Where("(id = ? OR service_desk_uuid = ?)", id, id).
		First(&comment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *ticketRepo) UpdateCommentText(ctx context.Context, ticketID string, commentUUID string, text string) (*tickets.TicketComment, error) {
	comment, err := r.GetCommentByUUID(ctx, ticketID, commentUUID)
	if err != nil || comment == nil {
		return comment, err
	}

	if err := r.db.WithContext(ctx).
		Model(&tickets.TicketComment{}).
		Where("id = ?", comment.ID).
		Update("text", text).Error; err != nil {
		return nil, err
	}
	comment.Text = text
	return comment, nil
}

func (r *ticketRepo) SoftDeleteComment(ctx context.Context, ticketID string, commentUUID string, deletedAt time.Time) (*tickets.TicketComment, error) {
	comment, err := r.GetCommentByUUID(ctx, ticketID, commentUUID)
	if err != nil || comment == nil {
		return comment, err
	}

	if err := r.db.WithContext(ctx).
		Model(&tickets.TicketComment{}).
		Where("id = ?", comment.ID).
		Updates(map[string]interface{}{
			"deleted_in_bitrix":    true,
			"deleted_in_bitrix_at": deletedAt,
		}).Error; err != nil {
		return nil, err
	}
	comment.DeletedInBitrix = true
	comment.DeletedInBitrixAt = &deletedAt
	return comment, nil
}

func (r *ticketRepo) HardDeleteComment(ctx context.Context, ticketID string, commentUUID string) (*tickets.TicketComment, error) {
	comment, err := r.GetCommentByUUID(ctx, ticketID, commentUUID)
	if err != nil || comment == nil {
		return comment, err
	}

	if err := r.db.WithContext(ctx).
		Where("id = ?", comment.ID).
		Delete(&tickets.TicketComment{}).Error; err != nil {
		return nil, err
	}
	return comment, nil
}

func (r *ticketRepo) UpdateCommentFromBitrix(ctx context.Context, commentID string, text string, authorName string) error {
	updates := map[string]interface{}{
		"text":                 text,
		"author_name":          authorName,
		"deleted_in_bitrix":    false,
		"deleted_in_bitrix_at": nil,
	}
	return r.db.WithContext(ctx).
		Model(&tickets.TicketComment{}).
		Where("id = ?", commentID).
		Updates(updates).Error
}

func (r *ticketRepo) MarkCommentDeletedInBitrix(ctx context.Context, commentID string, deletedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&tickets.TicketComment{}).
		Where("id = ?", commentID).
		Updates(map[string]interface{}{
			"deleted_in_bitrix":    true,
			"deleted_in_bitrix_at": deletedAt,
		}).Error
}

func (r *ticketRepo) GetLastComments(ctx context.Context, ticketIDs []string) (map[string]tickets.LastCommentInfo, error) {
	result := make(map[string]tickets.LastCommentInfo)
	if len(ticketIDs) == 0 {
		return result, nil
	}

	type row struct {
		TicketID   string `gorm:"column:ticket_id"`
		Text       string `gorm:"column:text"`
		AuthorName string `gorm:"column:author_name"`
		IsPrivate  bool   `gorm:"column:is_private"`
	}

	var rows []row
	err := r.db.WithContext(ctx).Raw(
		`SELECT tc.ticket_id, tc.text, tc.author_name, tc.is_private
		 FROM ticket_comments tc
		 WHERE tc.ticket_id IN ?
		   AND tc.deleted_in_bitrix = false
		 ORDER BY tc.ticket_id, tc.creation_date DESC`,
		ticketIDs,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, r := range rows {
		if _, exists := result[r.TicketID]; !exists {
			result[r.TicketID] = tickets.LastCommentInfo{
				Text:       r.Text,
				AuthorName: r.AuthorName,
				IsPrivate:  r.IsPrivate,
			}
		}
	}

	return result, nil
}

func (r *ticketRepo) GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error) {
	var rows []tickets.CompanyFilterItem

	query := r.buildQuery(ctx, filter).
		Scopes(withTicketCompanyJoin, withTicketParentCompanyJoin).
		Select(strings.Join([]string{
			ticketQualifiedCompanyColumn + " as id",
			ticketCompanyNameExpr + " as name",
			ticketParentNameExpr + " as parent_name",
			"COUNT(*) as count",
		}, ", "))

	err := query.
		Group(strings.Join([]string{
			ticketQualifiedCompanyColumn,
			ticketCompanyNameExpr,
			ticketParentNameExpr,
		}, ", ")).
		Order(ticketParentNameExpr).
		Order(ticketCompanyNameExpr).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *ticketRepo) GetDashboardStats(ctx context.Context) (*tickets.DashboardStats, error) {
	stats := &tickets.DashboardStats{
		ResolvedByAssignee: make([]tickets.ResolvedByAssigneeStat, 0),
		ServerStatuses:     make([]tickets.ServerStatusStat, 0),
	}

	if err := r.db.WithContext(ctx).Model(&tickets.Ticket{}).Count(&stats.TotalTickets).Error; err != nil {
		return nil, err
	}

	if err := r.db.WithContext(ctx).
		Model(&server.Server{}).
		Where("last_polled_at IS NOT NULL").
		Where("last_polled_at >= ?", time.Now().Add(-24*time.Hour)).
		Count(&stats.PolledServers24h).Error; err != nil {
		return nil, err
	}

	rows := make([]tickets.ResolvedByAssigneeStat, 0)
	if err := r.db.WithContext(ctx).
		Model(&tickets.Ticket{}).
		Table(ticketTableName+" AS t").
		Select(strings.Join([]string{
			ticketUserAlias + ".id AS user_id",
			resolvedUserNameExpr + " AS user_name",
			"COUNT(*) AS count",
		}, ", ")).
		Joins("JOIN "+ticketUserTableName+" AS "+ticketUserAlias+" ON "+ticketUserAlias+".id = t.assignee_id").
		Where("t.status IN ?", []string{tickets.StatusResolved, tickets.StatusClosed}).
		Group(strings.Join([]string{
			ticketUserAlias + ".id",
			ticketUserAlias + ".full_name",
			ticketUserAlias + ".first_name",
			ticketUserAlias + ".last_name",
			ticketUserAlias + ".username",
		}, ", ")).
		Order("count DESC").
		Order("user_name ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	stats.ResolvedByAssignee = rows

	serverStatusRows := make([]tickets.ServerStatusStat, 0)
	if err := r.db.WithContext(ctx).
		Model(&server.Server{}).
		Select(serverStatusExpr + " AS status, COUNT(*) AS count").
		Group(serverStatusExpr).
		Order("count DESC").
		Order("status ASC").
		Scan(&serverStatusRows).Error; err != nil {
		return nil, err
	}
	stats.ServerStatuses = serverStatusRows

	return stats, nil
}

func withTicketCompanyJoin(query *gorm.DB) *gorm.DB {
	return query.Joins(ticketCompanyJoinClause)
}

func withTicketParentCompanyJoin(query *gorm.DB) *gorm.DB {
	return query.Joins(ticketParentCompanyJoinClause)
}

func applyTicketSort(query *gorm.DB, rawSort string) *gorm.DB {
	sortField, desc := parseTicketSort(rawSort)
	column, ok := ticketSortableColumns[sortField]
	if !ok {
		column = ticketSortableColumns[ticketSortFieldCreatedAt]
		desc = true
	}
	return query.Order(clause.OrderByColumn{Column: column, Desc: desc})
}

func parseTicketSort(rawSort string) (string, bool) {
	sortValue := strings.TrimSpace(strings.ReplaceAll(rawSort, ":", " "))
	if sortValue == "" {
		return ticketSortFieldCreatedAt, true
	}

	parts := strings.Fields(sortValue)
	field := parts[0]
	desc := true
	if len(parts) > 1 && strings.EqualFold(parts[1], "asc") {
		desc = false
	}

	if _, ok := ticketSortableColumns[field]; !ok {
		return ticketSortFieldCreatedAt, true
	}
	return field, desc
}

func (r *ticketRepo) ListResolvedForAutoClose(ctx context.Context, threshold time.Duration) ([]tickets.Ticket, error) {
	if threshold <= 0 {
		threshold = 14 * 24 * time.Hour
	}

	var items []tickets.Ticket
	err := r.db.WithContext(ctx).
		Where("status = ?", tickets.StatusResolved).
		Where(`COALESCE((
			SELECT MAX(th.created_at)
			FROM ticket_histories th
			WHERE th.ticket_id = tickets.id
				AND th.field = ?
				AND th.new_value = ?
		), tickets.updated_at) <= ?`, tickets.HistoryFieldStatus, tickets.StatusResolved, time.Now().Add(-threshold)).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ticketRepo) ListExpiredDeferred(ctx context.Context, now time.Time, limit int) ([]tickets.Ticket, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if limit <= 0 {
		limit = 200
	}

	var items []tickets.Ticket
	err := r.db.WithContext(ctx).
		Where("status = ?", tickets.StatusDeferred).
		Where("deferred_until IS NOT NULL").
		Where("deferred_until <= ?", now).
		Order("deferred_until ASC").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}
