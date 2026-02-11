package services

import (
	"context"
	"errors"
	"etalon-server/internal/domain/user"
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
	userRepo user.Repository
	logger   logger.LoggerInterface
}

func NewAuthService(cfg *config.Config, userRepo user.Repository, logger logger.LoggerInterface) AuthService {
	return &authServiceImpl{cfg, userRepo, logger}
}

// Login проверяет учетные данные и генерирует JWT.
func (s *authServiceImpl) Login(ctx context.Context, username, password string) (*api.LoginResponseDTO, error) {
	s.logger.Debug("Начало проверки учетных данных", "username", username)

	u, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		s.logger.Error("Ошибка получения пользователя из БД", "username", username, "error", err)
		return nil, fmt.Errorf("ошибка получения пользователя: %w", err)
	}

	if u == nil {
		s.logger.Info("Пользователь не найден", "username", username)
		return nil, ErrInvalidCredentials
	}

	if !u.CheckPassword(password) {
		s.logger.Info("Неверный пароль", "username", username)
		return nil, ErrInvalidCredentials
	}

	if !u.IsActive {
		s.logger.Warn("Попытка входа заблокированного пользователя", "username", username)
		return nil, errors.New("пользователь заблокирован")
	}

	if !u.HasLoggedIn {
		u.HasLoggedIn = true
		if err := s.userRepo.Update(ctx, u); err != nil {
			s.logger.Error("Не удалось сохранить факт первого входа пользователя", "user_id", u.ID, "error", err)
			return nil, fmt.Errorf("ошибка фиксации входа пользователя: %w", err)
		}
	}

	s.logger.Info("Успешная аутентификация", "username", username, "user_id", u.ID)

	expirationTime := time.Now().Add(time.Duration(s.cfg.JWTExpirationMin) * time.Minute)

	var rolesStr []string
	for _, r := range u.Roles {
		rolesStr = append(rolesStr, r.Name)
	}

	claims := &jwt.RegisteredClaims{
		Subject:   fmt.Sprint(u.ID),
		ExpiresAt: jwt.NewNumericDate(expirationTime),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}

	tokenClaims := jwt.MapClaims{
		"sub":   claims.Subject,
		"exp":   claims.ExpiresAt.Unix(),
		"iat":   claims.IssuedAt.Unix(),
		"roles": rolesStr,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		s.logger.Error("Ошибка генерации JWT токена", "error", err)
		return nil, fmt.Errorf("ошибка генерации токена: %w", err)
	}

	response := &api.LoginResponseDTO{
		AccessToken: tokenString,
		User: api.UserDTO{
			ID:               u.ID,
			Username:         u.Username,
			FullName:         u.FullName,
			FirstName:        u.FirstName,
			LastName:         u.LastName,
			Position:         u.Position,
			Roles:            rolesStr,
			ExternalSystemID: u.ExternalID,
			ExternalType:     u.ExternalType,
			ScheduleType:     u.ScheduleType,
			IsActive:         u.IsActive,
			HasLoggedIn:      u.HasLoggedIn,
		},
	}

	return response, nil
}
