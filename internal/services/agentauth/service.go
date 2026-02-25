package agentauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	tokenTypeAccess  = "access"
	tokenTypeRefresh = "refresh"
)

var (
	ErrInvalidToken = errors.New("невалидный токен агента")
	ErrTokenExpired = errors.New("токен агента просрочен")
)

type AgentRegistrar interface {
	RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error)
}

type Service interface {
	RegisterAndIssueTokens(ctx context.Context, req *api.RegistrationRequestDTO) (*api.AgentRegistrationResponseDTO, error)
	RefreshTokens(ctx context.Context, req *api.AgentTokenRefreshRequestDTO) (*api.AgentTokenRefreshResponseDTO, error)
	ValidateAccessToken(ctx context.Context, agentUUID, rawToken string) error
}

type service struct {
	db        *gorm.DB
	log       logger.LoggerInterface
	agentRepo repositories.AgentRepo
	registrar AgentRegistrar
}

func NewService(db *gorm.DB, log logger.LoggerInterface, agentRepo repositories.AgentRepo, registrar AgentRegistrar) Service {
	return &service{
		db:        db,
		log:       log,
		agentRepo: agentRepo,
		registrar: registrar,
	}
}

func (s *service) RegisterAndIssueTokens(ctx context.Context, req *api.RegistrationRequestDTO) (*api.AgentRegistrationResponseDTO, error) {
	if req == nil || strings.TrimSpace(req.AgentUUID) == "" {
		return nil, fmt.Errorf("agent_uuid обязателен")
	}

	if req.InitialData.AgentType == "" {
		req.InitialData.AgentType = "sssruner"
	}

	_, err := s.registrar.RegisterAgent(ctx, req)
	if err != nil && !errors.Is(err, domain.ErrAlreadyExists) {
		return nil, err
	}

	agent, err := s.agentRepo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить агента после регистрации: %w", err)
	}
	if agent == nil {
		return nil, fmt.Errorf("агент не найден после регистрации")
	}

	agent.Type = "sssruner"
	if strings.TrimSpace(req.Hostname) != "" {
		agent.Hostname = strings.TrimSpace(req.Hostname)
	}
	if strings.TrimSpace(req.AgentVersion) != "" {
		agent.Version = strings.TrimSpace(req.AgentVersion)
	}
	agent.LastHeartbeat = time.Now()
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		s.log.Warn("Не удалось обновить тип агента при регистрации", "uuid", agent.UUID, "error", err)
	}

	pair, err := s.issueTokenPair(ctx, req.AgentUUID)
	if err != nil {
		return nil, err
	}

	return &api.AgentRegistrationResponseDTO{
		Status:                "ok",
		AgentUUID:             req.AgentUUID,
		AccessToken:           pair.AccessToken,
		AccessTokenExpiresAt:  pair.AccessExpiresAt,
		RefreshToken:          pair.RefreshToken,
		RefreshTokenExpiresAt: pair.RefreshExpiresAt,
	}, nil
}

func (s *service) RefreshTokens(ctx context.Context, req *api.AgentTokenRefreshRequestDTO) (*api.AgentTokenRefreshResponseDTO, error) {
	if req == nil || strings.TrimSpace(req.AgentUUID) == "" || strings.TrimSpace(req.RefreshToken) == "" {
		return nil, ErrInvalidToken
	}

	rec, err := s.findToken(ctx, req.RefreshToken, tokenTypeRefresh)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.AgentUUID != strings.TrimSpace(req.AgentUUID) {
		return nil, ErrInvalidToken
	}
	if rec.RevokedAt != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().After(rec.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	now := time.Now()
	rec.LastUsedAt = &now
	rec.RevokedAt = &now // ротация refresh-токена
	if err := s.db.WithContext(ctx).Save(rec).Error; err != nil {
		return nil, fmt.Errorf("не удалось обновить refresh-токен: %w", err)
	}

	pair, err := s.issueTokenPair(ctx, rec.AgentUUID)
	if err != nil {
		return nil, err
	}

	return &api.AgentTokenRefreshResponseDTO{
		Status:                "ok",
		AgentUUID:             rec.AgentUUID,
		AccessToken:           pair.AccessToken,
		AccessTokenExpiresAt:  pair.AccessExpiresAt,
		RefreshToken:          pair.RefreshToken,
		RefreshTokenExpiresAt: pair.RefreshExpiresAt,
	}, nil
}

func (s *service) ValidateAccessToken(ctx context.Context, agentUUID, rawToken string) error {
	if strings.TrimSpace(agentUUID) == "" || strings.TrimSpace(rawToken) == "" {
		return ErrInvalidToken
	}

	rec, err := s.findToken(ctx, rawToken, tokenTypeAccess)
	if err != nil {
		return err
	}
	if rec == nil || rec.AgentUUID != strings.TrimSpace(agentUUID) {
		return ErrInvalidToken
	}
	if rec.RevokedAt != nil {
		return ErrInvalidToken
	}
	if time.Now().After(rec.ExpiresAt) {
		return ErrTokenExpired
	}

	now := time.Now()
	rec.LastUsedAt = &now
	_ = s.db.WithContext(ctx).Model(&models.AgentSessionToken{}).Where("id = ?", rec.ID).Update("last_used_at", now).Error
	return nil
}

type tokenPair struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

func (s *service) issueTokenPair(ctx context.Context, agentUUID string) (*tokenPair, error) {
	now := time.Now()
	pair := &tokenPair{
		AccessExpiresAt:  now.Add(24 * time.Hour),
		RefreshExpiresAt: now.Add(30 * 24 * time.Hour),
	}

	var err error
	pair.AccessToken, err = generateToken("ags")
	if err != nil {
		return nil, err
	}
	pair.RefreshToken, err = generateToken("agr")
	if err != nil {
		return nil, err
	}

	if err := s.revokeActiveTokens(ctx, agentUUID, tokenTypeAccess); err != nil {
		return nil, err
	}
	if err := s.revokeActiveTokens(ctx, agentUUID, tokenTypeRefresh); err != nil {
		return nil, err
	}

	records := []models.AgentSessionToken{
		{
			AgentUUID: agentUUID,
			TokenType: tokenTypeAccess,
			TokenHash: tokenHash(pair.AccessToken),
			ExpiresAt: pair.AccessExpiresAt,
		},
		{
			AgentUUID: agentUUID,
			TokenType: tokenTypeRefresh,
			TokenHash: tokenHash(pair.RefreshToken),
			ExpiresAt: pair.RefreshExpiresAt,
		},
	}
	if err := s.db.WithContext(ctx).Create(&records).Error; err != nil {
		return nil, fmt.Errorf("не удалось сохранить токены агента: %w", err)
	}

	return pair, nil
}

func (s *service) revokeActiveTokens(ctx context.Context, agentUUID, tokenType string) error {
	now := time.Now()
	return s.db.WithContext(ctx).
		Model(&models.AgentSessionToken{}).
		Where("agent_uuid = ? AND token_type = ? AND revoked_at IS NULL", agentUUID, tokenType).
		Update("revoked_at", now).Error
}

func (s *service) findToken(ctx context.Context, rawToken, tokenType string) (*models.AgentSessionToken, error) {
	var rec models.AgentSessionToken
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND token_type = ?", tokenHash(rawToken), tokenType).
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("не удалось найти токен агента: %w", err)
	}
	return &rec, nil
}

func generateToken(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("не удалось сгенерировать токен: %w", err)
	}
	return prefix + "." + base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenHash(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}
