package repositories

import (
	"context"
	"errors"

	"etalon-server/internal/domain/tickets"

	"gorm.io/gorm"
)

type ticketChecklistRepo struct {
	db *gorm.DB
}

// NewTicketChecklistRepo создает репозиторий чеклистов тикетов и шаблонов.
func NewTicketChecklistRepo(db *gorm.DB) tickets.ChecklistRepository {
	return &ticketChecklistRepo{db: db}
}

func (r *ticketChecklistRepo) ListItems(ctx context.Context, ticketID string) ([]tickets.TicketChecklistItem, error) {
	var items []tickets.TicketChecklistItem
	err := r.db.WithContext(ctx).
		Preload("Assignees").
		Where("ticket_id = ?", ticketID).
		Order("position ASC, created_at ASC").
		Find(&items).Error
	return items, err
}

func (r *ticketChecklistRepo) GetItem(ctx context.Context, ticketID string, itemID string) (*tickets.TicketChecklistItem, error) {
	var item tickets.TicketChecklistItem
	err := r.db.WithContext(ctx).
		Preload("Assignees").
		Where("ticket_id = ? AND id = ?", ticketID, itemID).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ticketChecklistRepo) NextPosition(ctx context.Context, ticketID string, parentID *string) (int, error) {
	var maxPosition *int
	query := r.db.WithContext(ctx).
		Model(&tickets.TicketChecklistItem{}).
		Select("MAX(position)").
		Where("ticket_id = ?", ticketID)
	if parentID == nil {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	if err := query.Scan(&maxPosition).Error; err != nil {
		return 0, err
	}
	if maxPosition == nil {
		return 0, nil
	}
	return *maxPosition + 1, nil
}

func (r *ticketChecklistRepo) CreateItems(ctx context.Context, items []tickets.TicketChecklistItem) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&items).Error
}

func (r *ticketChecklistRepo) UpdateItem(ctx context.Context, ticketID string, itemID string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Model(&tickets.TicketChecklistItem{}).
		Where("ticket_id = ? AND id = ?", ticketID, itemID).
		Updates(updates).Error
}

func (r *ticketChecklistRepo) ReplaceAssignees(ctx context.Context, itemID string, userIDs []uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("item_id = ?", itemID).Delete(&tickets.TicketChecklistItemAssignee{}).Error; err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return nil
		}
		rows := make([]tickets.TicketChecklistItemAssignee, 0, len(userIDs))
		for _, userID := range userIDs {
			rows = append(rows, tickets.TicketChecklistItemAssignee{ItemID: itemID, UserID: userID})
		}
		return tx.Create(&rows).Error
	})
}

func (r *ticketChecklistRepo) DeleteItems(ctx context.Context, ticketID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("item_id IN ?", itemIDs).Delete(&tickets.TicketChecklistItemAssignee{}).Error; err != nil {
			return err
		}
		return tx.Where("ticket_id = ? AND id IN ?", ticketID, itemIDs).Delete(&tickets.TicketChecklistItem{}).Error
	})
}

func (r *ticketChecklistRepo) UpdatePositions(ctx context.Context, ticketID string, parentID *string, orderedIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index, itemID := range orderedIDs {
			if err := tx.Model(&tickets.TicketChecklistItem{}).
				Where("ticket_id = ? AND id = ?", ticketID, itemID).
				Updates(map[string]interface{}{"position": index, "parent_id": parentID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *ticketChecklistRepo) ListTemplates(ctx context.Context, activeOnly bool) ([]tickets.ChecklistTemplate, error) {
	var items []tickets.ChecklistTemplate
	query := r.db.WithContext(ctx).Order("title ASC")
	if activeOnly {
		query = query.Where("is_active = ?", true)
	}
	err := query.Find(&items).Error
	return items, err
}

func (r *ticketChecklistRepo) GetTemplate(ctx context.Context, id string) (*tickets.ChecklistTemplate, error) {
	var item tickets.ChecklistTemplate
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ticketChecklistRepo) CreateTemplate(ctx context.Context, template *tickets.ChecklistTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

func (r *ticketChecklistRepo) UpdateTemplate(ctx context.Context, template *tickets.ChecklistTemplate) error {
	return r.db.WithContext(ctx).
		Model(&tickets.ChecklistTemplate{}).
		Where("id = ?", template.ID).
		Updates(map[string]interface{}{
			"title":       template.Title,
			"description": template.Description,
			"items":       template.Items,
			"is_active":   template.IsActive,
		}).Error
}

func (r *ticketChecklistRepo) DeleteTemplate(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&tickets.ChecklistTemplate{}).Error
}
