package agentauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"net"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	tokenTypeAccess                 = "access"
	tokenTypeRefresh                = "refresh"
	registrationPendingApprovalText = "Регистрация ожидает подтверждения оператором"
)

var (
	ErrInvalidToken = errors.New("невалидный токен агента")
	ErrTokenExpired = errors.New("токен агента просрочен")
)

type RegistrationAttemptStatus string

const (
	RegistrationAttemptStatusSuccess         RegistrationAttemptStatus = models.AgentRegistrationStatusSuccess
	RegistrationAttemptStatusPendingApproval RegistrationAttemptStatus = models.AgentRegistrationStatusPendingApproval
	RegistrationAttemptStatusUnauthorized    RegistrationAttemptStatus = models.AgentRegistrationStatusUnauthorized
	RegistrationAttemptStatusInvalidRequest  RegistrationAttemptStatus = models.AgentRegistrationStatusInvalidRequest
	RegistrationAttemptStatusFailed          RegistrationAttemptStatus = models.AgentRegistrationStatusFailed
)

type RegistrationAttemptMeta struct {
	RemoteAddr string
	RawPayload []byte
}

type AgentRegistrar interface {
	RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error)
}

type Service interface {
	RegisterAndIssueTokens(ctx context.Context, req *api.RegistrationRequestDTO, meta RegistrationAttemptMeta) (*api.AgentRegistrationResponseDTO, error)
	RecordRegistrationAttempt(ctx context.Context, req *api.RegistrationRequestDTO, meta RegistrationAttemptMeta, status RegistrationAttemptStatus, errorText string) error
	RefreshTokens(ctx context.Context, req *api.AgentTokenRefreshRequestDTO) (*api.AgentTokenRefreshResponseDTO, error)
	ValidateAccessToken(ctx context.Context, agentUUID, rawToken string) error
}

type service struct {
	db        *gorm.DB
	log       logger.LoggerInterface
	agentRepo repositories.AgentRepo
	registrar AgentRegistrar
	// tokens выпускает/проверяет JWT access-токены. nil = opaque-режим
	// (access-токен хранится в БД как AgentSessionToken, как исторически).
	tokens *EdDSATokenService
}

func NewService(db *gorm.DB, log logger.LoggerInterface, agentRepo repositories.AgentRepo, registrar AgentRegistrar) Service {
	return &service{
		db:        db,
		log:       log,
		agentRepo: agentRepo,
		registrar: registrar,
	}
}

// NewServiceWithJWT создаёт сервис с JWT-выпуском access-токенов.
// tokens=nil эквивалентно NewService (opaque-режим).
func NewServiceWithJWT(db *gorm.DB, log logger.LoggerInterface, agentRepo repositories.AgentRepo, registrar AgentRegistrar, tokens *EdDSATokenService) Service {
	return &service{
		db:        db,
		log:       log,
		agentRepo: agentRepo,
		registrar: registrar,
		tokens:    tokens,
	}
}

func (s *service) RegisterAndIssueTokens(ctx context.Context, req *api.RegistrationRequestDTO, meta RegistrationAttemptMeta) (*api.AgentRegistrationResponseDTO, error) {
	if req == nil || strings.TrimSpace(req.AgentUUID) == "" {
		err := fmt.Errorf("agent_uuid обязателен")
		s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusInvalidRequest, err.Error())
		return nil, err
	}

	if req.InitialData.AgentType == "" {
		req.InitialData.AgentType = "sssruner"
	}

	_, err := s.registrar.RegisterAgent(ctx, req)
	if err != nil && !errors.Is(err, domain.ErrAlreadyExists) {
		s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusFailed, err.Error())
		return nil, err
	}

	agent, err := s.agentRepo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		wrappedErr := fmt.Errorf("не удалось получить агента после регистрации: %w", err)
		s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusFailed, wrappedErr.Error())
		return nil, wrappedErr
	}
	if agent == nil {
		err := fmt.Errorf("агент не найден после регистрации")
		s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusFailed, err.Error())
		return nil, err
	}

	agent.Type = registrationAgentType(req)
	if host := hostnameFromRequest(req); host != "" {
		agent.Hostname = host
	}
	if version := versionFromRequest(req); version != "" {
		agent.Version = version
	}
	agent.LastHeartbeat = time.Now()
	if !registrationApproved(agent) {
		agent.Status = models.StatusPendingRegistration
		if err := s.agentRepo.Update(ctx, agent); err != nil {
			wrappedErr := fmt.Errorf("не удалось сохранить состояние ожидания подтверждения регистрации: %w", err)
			s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusFailed, wrappedErr.Error())
			return nil, wrappedErr
		}

		resp := &api.AgentRegistrationResponseDTO{
			Status:    models.AgentRegistrationStatusPendingApproval,
			Message:   registrationPendingApprovalText,
			AgentUUID: req.AgentUUID,
		}
		s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusPendingApproval, registrationPendingApprovalText)
		return resp, nil
	}
	if agent.Status == models.StatusPendingRegistration || agent.Status == models.StatusRegistrationFailed {
		agent.Status = models.StatusPendingOwner
	}
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		s.log.Warn("Не удалось обновить тип агента при регистрации", "uuid", agent.UUID, "error", err)
	}

	pair, err := s.issueTokenPair(ctx, req.AgentUUID)
	if err != nil {
		s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusFailed, err.Error())
		return nil, err
	}

	resp := &api.AgentRegistrationResponseDTO{
		Status:                "ok",
		AgentUUID:             req.AgentUUID,
		AccessToken:           pair.AccessToken,
		AccessTokenExpiresAt:  pair.AccessExpiresAt,
		RefreshToken:          pair.RefreshToken,
		RefreshTokenExpiresAt: pair.RefreshExpiresAt,
	}
	s.logRegistrationAttemptError(ctx, req, meta, RegistrationAttemptStatusSuccess, "")
	return resp, nil
}

func (s *service) RecordRegistrationAttempt(ctx context.Context, req *api.RegistrationRequestDTO, meta RegistrationAttemptMeta, status RegistrationAttemptStatus, errorText string) error {
	return s.recordRegistrationAttempt(ctx, req, meta, status, errorText)
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

	// JWT-путь: проверяем подпись публичным ключом без обращения к БД.
	// Это устраняет write-per-heartbeat на master-БД при горизонтальном
	// масштабировании agent-gateway.
	if s.tokens != nil && looksLikeJWT(rawToken) {
		return s.tokens.Verify(rawToken, agentUUID)
	}

	// Opaque-путь (legacy): lookup по token_hash + UPDATE last_used_at.
	// Сохраняется для токенов, выданных до включения JWT, и для режима,
	// где приватный ключ не сконфигурирован.
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

	// Access-токен. В JWT-режиме подписывается EdDSA и НЕ сохраняется в БД —
	// проверка идёт по публичному ключу без write-per-heartbeat.
	// В opaque-режиме — случайная строка с записью в AgentSessionToken.
	if s.tokens != nil {
		access, expiresAt, err := s.tokens.Issue(agentUUID)
		if err != nil {
			return nil, err
		}
		pair.AccessToken = access
		pair.AccessExpiresAt = expiresAt
	} else {
		access, err := generateToken("ags")
		if err != nil {
			return nil, err
		}
		pair.AccessToken = access
	}

	// Refresh-токен всегда opaque и хранится в БД: ротация происходит редко
	// (раз в 30 дней), поэтому lookup при refresh не создаёт нагрузки на master.
	refresh, err := generateToken("agr")
	if err != nil {
		return nil, err
	}
	pair.RefreshToken = refresh

	// Отзываем активные refresh-токены перед выдачей новой пары.
	if err := s.revokeActiveTokens(ctx, agentUUID, tokenTypeRefresh); err != nil {
		return nil, err
	}
	// В opaque-режиме отзываем и активные access-токены (они тоже в БД).
	if s.tokens == nil {
		if err := s.revokeActiveTokens(ctx, agentUUID, tokenTypeAccess); err != nil {
			return nil, err
		}
	}

	records := []models.AgentSessionToken{
		{
			AgentUUID: agentUUID,
			TokenType: tokenTypeRefresh,
			TokenHash: tokenHash(pair.RefreshToken),
			ExpiresAt: pair.RefreshExpiresAt,
		},
	}
	if s.tokens == nil {
		records = append(records, models.AgentSessionToken{
			AgentUUID: agentUUID,
			TokenType: tokenTypeAccess,
			TokenHash: tokenHash(pair.AccessToken),
			ExpiresAt: pair.AccessExpiresAt,
		})
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

func (s *service) logRegistrationAttemptError(ctx context.Context, req *api.RegistrationRequestDTO, meta RegistrationAttemptMeta, status RegistrationAttemptStatus, errorText string) {
	if err := s.recordRegistrationAttempt(ctx, req, meta, status, errorText); err != nil {
		s.log.Warn(
			"Не удалось сохранить диагностику регистрации агента",
			"agent_uuid", agentUUIDFromRequest(req),
			"status", string(status),
			"error", err,
		)
	}
}

func (s *service) recordRegistrationAttempt(ctx context.Context, req *api.RegistrationRequestDTO, meta RegistrationAttemptMeta, status RegistrationAttemptStatus, errorText string) error {
	now := time.Now().UTC()
	payloadJSON := registrationPayloadJSON(req, meta.RawPayload)
	systemInfoJSON := jsonMapToJSON(systemInfoFromRequest(req))
	agentUUID := agentUUIDFromRequest(req)

	if agentUUID != "" {
		if err := s.upsertAgentRegistrationSnapshot(ctx, req, payloadJSON, systemInfoJSON, status, errorText, now); err != nil {
			return err
		}
	}

	attempt := &models.AgentRegistrationAttempt{
		Status:             string(status),
		MachineFingerprint: machineFingerprintFromRequest(req),
		SystemInfo:         systemInfoJSON,
		Payload:            payloadJSON,
		RemoteAddr:         normalizeRemoteAddr(meta.RemoteAddr),
		CreatedAt:          now,
	}
	if agentUUID != "" {
		attempt.AgentUUID = stringPtr(agentUUID)
	}
	if trimmedError := strings.TrimSpace(errorText); trimmedError != "" {
		attempt.ErrorText = stringPtr(trimmedError)
	}

	return s.db.WithContext(ctx).Create(attempt).Error
}

func (s *service) upsertAgentRegistrationSnapshot(
	ctx context.Context,
	req *api.RegistrationRequestDTO,
	payloadJSON datatypes.JSON,
	systemInfoJSON datatypes.JSON,
	status RegistrationAttemptStatus,
	errorText string,
	recordedAt time.Time,
) error {
	agentUUID := agentUUIDFromRequest(req)
	if agentUUID == "" {
		return nil
	}

	agent, err := s.agentRepo.GetByUUID(ctx, agentUUID)
	if err != nil {
		return fmt.Errorf("не удалось получить агента для обновления snapshot регистрации: %w", err)
	}

	if agent == nil {
		agent = &models.Agent{
			UUID:     agentUUID,
			Type:     registrationAgentType(req),
			Status:   models.StatusRegistrationFailed,
			Hostname: hostnameFromRequest(req),
			Version:  versionFromRequest(req),
		}
	}

	if host := hostnameFromRequest(req); host != "" {
		agent.Hostname = host
	}
	if version := versionFromRequest(req); version != "" {
		agent.Version = version
	}
	if agent.Type == "" {
		agent.Type = registrationAgentType(req)
	}

	agent.LastRegistrationAt = &recordedAt
	agent.LastRegistrationStatus = string(status)
	agent.LastRegistrationError = strings.TrimSpace(errorText)
	if fingerprint := machineFingerprintFromRequest(req); fingerprint != "" {
		agent.MachineFingerprint = fingerprint
	}
	if len(systemInfoJSON) > 0 {
		agent.RegistrationSystemInfo = systemInfoJSON
	}
	if len(payloadJSON) > 0 {
		agent.RegistrationPayload = payloadJSON
	}
	switch status {
	case RegistrationAttemptStatusPendingApproval:
		agent.Status = models.StatusPendingRegistration
	case RegistrationAttemptStatusSuccess:
		if agent.Status == models.StatusPendingRegistration || agent.Status == models.StatusRegistrationFailed {
			agent.Status = models.StatusPendingOwner
		}
	case RegistrationAttemptStatusUnauthorized, RegistrationAttemptStatusInvalidRequest, RegistrationAttemptStatusFailed:
		if !registrationApproved(agent) {
			agent.Status = models.StatusRegistrationFailed
		}
	}

	if existsInStorage(agent) {
		return s.agentRepo.Update(ctx, agent)
	}
	return s.agentRepo.Create(ctx, agent)
}

func existsInStorage(agent *models.Agent) bool {
	return agent != nil && agent.CreatedAt != (time.Time{})
}

func registrationPayloadJSON(req *api.RegistrationRequestDTO, rawPayload []byte) datatypes.JSON {
	if normalized, ok := normalizeRawJSON(rawPayload); ok {
		return normalized
	}
	if req == nil {
		return nil
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil
	}
	return datatypes.JSON(raw)
}

func normalizeRawJSON(rawPayload []byte) (datatypes.JSON, bool) {
	trimmed := strings.TrimSpace(string(rawPayload))
	if trimmed == "" {
		return nil, false
	}
	if json.Valid([]byte(trimmed)) {
		return datatypes.JSON([]byte(trimmed)), true
	}
	wrapped, err := json.Marshal(map[string]string{
		"raw_body": trimmed,
	})
	if err != nil {
		return nil, false
	}
	return datatypes.JSON(wrapped), true
}

func jsonMapToJSON(payload map[string]any) datatypes.JSON {
	if len(payload) == 0 {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return datatypes.JSON(raw)
}

func agentUUIDFromRequest(req *api.RegistrationRequestDTO) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.AgentUUID)
}

func hostnameFromRequest(req *api.RegistrationRequestDTO) string {
	if req == nil {
		return ""
	}
	if hostname := strings.TrimSpace(req.Hostname); hostname != "" {
		return hostname
	}
	return strings.TrimSpace(req.InitialData.Hostname)
}

func versionFromRequest(req *api.RegistrationRequestDTO) string {
	if req == nil {
		return ""
	}
	if version := strings.TrimSpace(req.AgentVersion); version != "" {
		return version
	}
	return strings.TrimSpace(req.InitialData.AgentVersion)
}

func registrationAgentType(req *api.RegistrationRequestDTO) string {
	if req == nil {
		return "sssruner"
	}
	if agentType := strings.TrimSpace(req.InitialData.AgentType); agentType != "" {
		return agentType
	}
	return "sssruner"
}

func machineFingerprintFromRequest(req *api.RegistrationRequestDTO) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(req.MachineFingerprint)
}

func systemInfoFromRequest(req *api.RegistrationRequestDTO) map[string]any {
	if req == nil || len(req.SystemInfo) == 0 {
		return nil
	}
	return req.SystemInfo
}

func normalizeRemoteAddr(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(value)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return value
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func registrationApproved(agent *models.Agent) bool {
	return agent != nil && agent.RegistrationApprovedAt != nil && !agent.RegistrationApprovedAt.IsZero()
}
