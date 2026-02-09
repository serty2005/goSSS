package repositories

import (
	"context"
	"errors"
	"strings"

	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/tickets"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ticketRepo struct {
	db *gorm.DB
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
	return r.db.WithContext(ctx).Create(ticket).Error
}

func (r *ticketRepo) Update(ctx context.Context, ticket *tickets.Ticket) error {
	return r.db.WithContext(ctx).Save(ticket).Error
}

func (r *ticketRepo) GetByID(ctx context.Context, id string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).
		Joins("LEFT JOIN companies c ON c.id = tickets.company_id").
		Select("tickets.*, COALESCE(c.title, c.additional_name, tickets.company_id) as company_name").
		Preload("Assignee").
		Preload("Reporter").
		Where("tickets.id = ?", id).
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

func (r *ticketRepo) Find(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, error) {
	var items []tickets.Ticket
	query := r.buildQuery(ctx, filter).
		Joins("LEFT JOIN companies c ON c.id = tickets.company_id").
		Select("tickets.*, COALESCE(c.title, c.additional_name, tickets.company_id) as company_name")

	if filter.SortBy != "" {
		query = query.Order(filter.SortBy)
	} else {
		query = query.Order("created_at desc")
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	err := query.Preload("Assignee").Find(&items).Error
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
	if filter.CompanyID != "" {
		query = query.Where("company_id = ?", filter.CompanyID)
	}
	if filter.AssetID != nil {
		query = query.Where("asset_id = ?", *filter.AssetID)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	if filter.AssigneeID != nil {
		query = query.Where("assignee_id = ?", *filter.AssigneeID)
	}
	if filter.ReporterID != nil {
		query = query.Where("reporter_id = ?", *filter.ReporterID)
	}
	if filter.SearchQuery != "" {
		q := "%" + filter.SearchQuery + "%"
		clauses := []string{
			"CAST(tickets.number AS TEXT) ILIKE ?",
			"tickets.subject ILIKE ?",
			"tickets.description ILIKE ?",
			"EXISTS (SELECT 1 FROM ticket_comments tc WHERE tc.ticket_id = tickets.id AND tc.text ILIKE ?)",
		}
		args := []interface{}{q, q, q, q}
		query = query.Where("("+strings.Join(clauses, " OR ")+")", args...)
	}

	return query
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
		Order("creation_date asc").
		Find(&comments).Error
	return comments, err
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
	}

	var rows []row
	err := r.db.WithContext(ctx).Raw(
		`SELECT tc.ticket_id, tc.text, tc.author_name
		 FROM ticket_comments tc
		 WHERE tc.ticket_id IN ?
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
			}
		}
	}

	return result, nil
}

func (r *ticketRepo) GetCompanyFilters(ctx context.Context, filter tickets.TicketFilter) ([]tickets.CompanyFilterItem, error) {
	var rows []tickets.CompanyFilterItem

	query := r.db.WithContext(ctx).Table("tickets").
		Select(`
			tickets.company_id as id,
			COALESCE(c.title, c.additional_name, tickets.company_id) as name,
			COALESCE(parent.title, '') as parent_name,
			COUNT(*) as count
		`).
		Joins("LEFT JOIN companies c ON c.id = tickets.company_id").
		Joins("LEFT JOIN companies parent ON parent.id = c.parent_id")

	query = r.applyFilters(query, filter)

	err := query.
		Group("tickets.company_id, name, parent_name").
		Order("parent_name, name").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	return rows, nil
}
