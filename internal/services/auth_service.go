package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
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
	logger   logger.LoggerInterface
}

func NewAuthService(cfg *config.Config, userRepo repositories.UserRepo, logger logger.LoggerInterface) AuthService {
	return &authServiceImpl{cfg, userRepo, logger}
}

// Login проверяет учетные данные и генерирует JWT.
func (s *authServiceImpl) Login(ctx context.Context, username, password string) (*api.LoginResponseDTO, error) {
	s.logger.Debug("Начало проверки учетных данных", "username", username)

	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		s.logger.Error("Ошибка получения пользователя из БД", "username", username, "error", err)
		return nil, fmt.Errorf("ошибка получения пользователя: %w", err)
	}

	if user == nil {
		s.logger.Info("Пользователь не найден", "username", username)
		return nil, ErrInvalidCredentials
	}

	s.logger.Debug("Пользователь найден, проверка пароля", "username", username, "user_id", user.ID)

	if !user.CheckPassword(password) {
		s.logger.Info("Неверный пароль для пользователя", "username", username, "user_id", user.ID)
		return nil, ErrInvalidCredentials
	}

	s.logger.Info("Успешная аутентификация пользователя", "username", username, "user_id", user.ID)

	// Генерация токена
	s.logger.Debug("Генерация JWT токена", "user_id", user.ID)

	expirationTime := time.Now().Add(time.Duration(s.cfg.JWTExpirationMin) * time.Minute)
	var roles []string
	_ = json.Unmarshal(user.Roles, &roles)

	s.logger.Debug("Извлечены роли пользователя", "user_id", user.ID, "roles_count", len(roles))

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
		s.logger.Error("Ошибка генерации JWT токена", "user_id", user.ID, "error", err)
		return nil, fmt.Errorf("ошибка генерации токена: %w", err)
	}

	s.logger.Info("JWT токен успешно сгенерирован", "user_id", user.ID, "expires_at", expirationTime)

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
