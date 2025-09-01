package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/config"
	"etalon-server/internal/repositories"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrInvalidCredentials = errors.New("неверное имя пользователя или пароль")

// AuthService определяет интерфейс для сервиса аутентификации.
type AuthService interface {
	Login(ctx context.Context, username, password string) (*api.LoginResponseDTO, error)
}

type authServiceImpl struct {
	cfg      *config.Config
	userRepo repositories.UserRepo
}

func NewAuthService(cfg *config.Config, userRepo repositories.UserRepo) AuthService {
	return &authServiceImpl{cfg, userRepo}
}

// Login проверяет учетные данные и генерирует JWT.
func (s *authServiceImpl) Login(ctx context.Context, username, password string) (*api.LoginResponseDTO, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения пользователя: %w", err)
	}
	if user == nil || !user.CheckPassword(password) {
		return nil, ErrInvalidCredentials
	}

	// Генерация токена
	expirationTime := time.Now().Add(time.Duration(s.cfg.JWTExpirationMin) * time.Minute)
	var roles []string
	_ = json.Unmarshal(user.Roles, &roles)

	claims := &jwt.RegisteredClaims{
		Subject:   fmt.Sprint(user.ID),
		ExpiresAt: jwt.NewNumericDate(expirationTime),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	// Добавляем кастомные поля
	tokenClaims := jwt.MapClaims{
		"sub":   claims.Subject,
		"exp":   claims.ExpiresAt.Unix(),
		"iat":   claims.IssuedAt.Unix(),
		"roles": roles,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("ошибка генерации токена: %w", err)
	}

	response := &api.LoginResponseDTO{
		AccessToken: tokenString,
		User: api.UserDTO{
			ID:       user.ID,
			Username: user.Username,
			FullName: user.FullName,
			Roles:    roles,
		},
	}

	return response, nil
}
