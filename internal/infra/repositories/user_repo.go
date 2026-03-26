package repositories

import (
	"context"
	"errors"
	"etalon-server/internal/domain/user"
	"strings"
	"time"

	"gorm.io/gorm"
)

type userRepo struct{ db *gorm.DB }

func NewUserRepo(db *gorm.DB) user.Repository {
	return &userRepo{db: db}
}

func (r *userRepo) GetAll(ctx context.Context) ([]user.User, error) {
	var users []user.User
	err := r.db.WithContext(ctx).Preload("Roles").Preload("Integrations").Find(&users).Error
	return users, err
}

func (r *userRepo) GetByID(ctx context.Context, id uint) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).Preload("Roles").Preload("Integrations").First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).Preload("Roles").Preload("Integrations").Where("username = ?", strings.TrimSpace(username)).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) GetDeletedByUsername(ctx context.Context, username string) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).
		Unscoped().
		Preload("Roles").
		Preload("Integrations").
		Where("username = ? AND deleted_at IS NOT NULL", strings.TrimSpace(username)).
		First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) Create(ctx context.Context, u *user.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepo) Update(ctx context.Context, u *user.User) error {
	return r.db.WithContext(ctx).Save(u).Error
}

func (r *userRepo) ReplaceIntegrations(ctx context.Context, userID uint, items []user.Integration) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&user.Integration{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		for i := range items {
			items[i].UserID = userID
		}
		return tx.Create(&items).Error
	})
}

func (r *userRepo) UpdateExternalFields(ctx context.Context, userID uint, externalType, externalID *string) error {
	return r.db.WithContext(ctx).
		Model(&user.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"external_type": externalType,
			"external_id":   externalID,
		}).Error
}

func (r *userRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&user.User{}, id).Error
}

func (r *userRepo) Restore(ctx context.Context, u *user.User) error {
	if u == nil || u.ID == 0 {
		return gorm.ErrInvalidData
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().
			Model(&user.User{}).
			Where("id = ?", u.ID).
			Updates(map[string]any{
				"deleted_at": nil,
				"updated_at": time.Now(),
			}).Error; err != nil {
			return err
		}

		return tx.Session(&gorm.Session{FullSaveAssociations: true}).Save(u).Error
	})
}

func (r *userRepo) GetRoleByName(ctx context.Context, name string) (*user.Role, error) {
	var role user.Role
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &role, err
}

func (r *userRepo) EnsureRoleExists(ctx context.Context, name, description string) (*user.Role, error) {
	var role user.Role
	err := r.db.WithContext(ctx).Where("name = ?", name).First(&role).Error
	if err == nil {
		return &role, nil
	}
	role = user.Role{Name: name, Description: description}
	if err := r.db.WithContext(ctx).Create(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}
