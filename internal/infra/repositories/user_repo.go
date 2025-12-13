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
	// Загружаем связи Roles
	err := r.db.WithContext(ctx).Preload("Roles").Find(&users).Error
	return users, err
}

func (r *userRepo) GetByID(ctx context.Context, id uint) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).Preload("Roles").First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).Preload("Roles").Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) Create(ctx context.Context, u *user.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepo) Update(ctx context.Context, u *user.User) error {
	// Используем Save для обновления всех полей, включая связи, если они были изменены через Association
	// Но для GORM many2many лучше обновлять через Association отдельно, если меняются роли.
	// Здесь простой Save обновит скалярные поля.
	return r.db.WithContext(ctx).Save(u).Error
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
	// Создаем
	role = user.Role{Name: name, Description: description}
	if err := r.db.WithContext(ctx).Create(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}
