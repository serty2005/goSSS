package repositories

import (
	"context"
	"errors"
	"etalon-server/internal/domain/user"

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
	err := r.db.WithContext(ctx).Preload("Roles").Preload("Integrations").Where("username = ?", username).First(&u).Error
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

func (r *userRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&user.User{}, id).Error
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
