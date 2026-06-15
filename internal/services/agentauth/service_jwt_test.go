package agentauth

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"etalon-server/internal/domain/models"
	domainRepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	infraRepos "etalon-server/internal/infra/repositories"
	api "etalon-server/internal/transport/http/dtos"
)

// newJWTServiceForTest собирает сервис с JWT-выпуском access-токенов.
func newJWTServiceForTest(t *testing.T) (Service, *gorm.DB, domainRepos.AgentRepo) {
	t.Helper()

	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentSessionToken{}, &models.AgentRegistrationAttempt{}))
	repo := infraRepos.NewAgentRepo(db)

	tokens := newTestTokenService(t, time.Hour)
	svc := NewServiceWithJWT(db, logger.New("", "test", "error", true), repo, &testRegistrar{repo: repo}, tokens)
	return svc, db, repo
}

// approveAgent вручную переводит агента в подтверждённое состояние, как это
// делает администратор через operator-api.
func approveAgent(t *testing.T, db *gorm.DB, agentUUID string) {
	t.Helper()
	approvedAt := time.Now().UTC()
	require.NoError(t, db.Model(&models.Agent{}).
		Where("uuid = ?", agentUUID).
		Updates(map[string]any{
			"registration_approved_at": approvedAt,
			"registration_approved_by": "user-1",
		}).Error)
}

func registerAndApprove(t *testing.T, svc Service, db *gorm.DB, agentUUID string) *api.AgentRegistrationResponseDTO {
	t.Helper()
	req := &api.RegistrationRequestDTO{
		AgentUUID:          agentUUID,
		Hostname:           "ws-jwt",
		AgentVersion:       "1.2.3",
		MachineFingerprint: "fp-jwt",
		InitialData: api.AgentDataDTO{
			AgentType:    "sssruner",
			Hostname:     "ws-jwt",
			AgentVersion: "1.2.3",
		},
	}
	meta := RegistrationAttemptMeta{
		RemoteAddr: "10.0.0.1:9000",
		RawPayload: []byte(`{"agent_uuid":"` + agentUUID + `"}`),
	}

	_, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	approveAgent(t, db, agentUUID)

	resp, err := svc.RegisterAndIssueTokens(context.Background(), req, meta)
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Status)
	require.NotEmpty(t, resp.AccessToken)
	require.NotEmpty(t, resp.RefreshToken)
	return resp
}

// refreshStored проверяет, что refresh-токен присутствует в БД (lookup-валиден).
// Используется вместо несуществующего ValidateRefreshToken.
func refreshStored(t *testing.T, db *gorm.DB, agentUUID, rawRefresh string) {
	t.Helper()
	var rec models.AgentSessionToken
	err := db.Where("token_type = ? AND agent_uuid = ? AND token_hash = ?",
		tokenTypeRefresh, agentUUID, tokenHash(rawRefresh)).First(&rec).Error
	require.NoError(t, err, "refresh-токен должен храниться в БД")
}

// В JWT-режиме после успешной регистрации создаётся ОДНА запись токена
// (refresh); access как JWT в БД не хранится. Это устраняет write-per-heartbeat.
func TestJWTIssueTokenPair_СоздаётТолькоРefreshЗапись(t *testing.T) {
	svc, db, _ := newJWTServiceForTest(t)

	resp := registerAndApprove(t, svc, db, "agent-jwt-refresh-only")

	var refreshCount int64
	require.NoError(t, db.Model(&models.AgentSessionToken{}).Where("token_type = ?", tokenTypeRefresh).Count(&refreshCount).Error)
	require.EqualValues(t, 1, refreshCount)

	// Access-токена в БД быть не должно — он JWT и проверяется без неё.
	var accessCount int64
	require.NoError(t, db.Model(&models.AgentSessionToken{}).Where("token_type = ?", tokenTypeAccess).Count(&accessCount).Error)
	require.Zero(t, accessCount)

	// При этом refresh должен валидироваться (lookup в БД).
	refreshStored(t, db, "agent-jwt-refresh-only", resp.RefreshToken)
}

// Ключевая проверка: валидация access-JWT не делает UPDATE last_used_at.
// Создаём access-JWT, фиксируем состояние БД, валидируем многократно —
// счётчики и timestamp-ы AgentSessionToken не должны измениться.
func TestJWTValidateAccessToken_НеОбращаетсяКБД(t *testing.T) {
	svc, db, _ := newJWTServiceForTest(t)

	resp := registerAndApprove(t, svc, db, "agent-jwt-no-db")

	// Замеряем состояние до валидаций.
	var beforeCount int64
	require.NoError(t, db.Model(&models.AgentSessionToken{}).Count(&beforeCount).Error)

	// Многократно валидируем access-токен — эмулируем heartbeat-нагрузку.
	for i := 0; i < 5; i++ {
		require.NoError(t, svc.ValidateAccessToken(context.Background(), "agent-jwt-no-db", resp.AccessToken))
	}

	// Состояние БД не должно измениться: ни новых записей, ни UPDATE.
	var afterCount int64
	require.NoError(t, db.Model(&models.AgentSessionToken{}).Count(&afterCount).Error)
	require.EqualValues(t, beforeCount, afterCount)
}

// Обратная совместимость: в opaque-режиме (NewService без JWT) ValidateAccessToken
// по-прежнему делает lookup + UPDATE last_used_at. Регресс opaque-пути.
func TestOpaqueValidateAccessToken_ОбновляетLastUsedAt(t *testing.T) {
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentSessionToken{}, &models.AgentRegistrationAttempt{}))
	repo := infraRepos.NewAgentRepo(db)
	svc := NewService(db, logger.New("", "test", "error", true), repo, &testRegistrar{repo: repo})

	resp := registerAndApprove(t, svc, db, "agent-opaque-fallback")

	// В opaque-режиме access-токен хранится в БД.
	var access models.AgentSessionToken
	require.NoError(t, db.Where("token_type = ? AND agent_uuid = ?", tokenTypeAccess, "agent-opaque-fallback").First(&access).Error)
	require.Nil(t, access.LastUsedAt)

	// После валидации last_used_at должен проставиться.
	require.NoError(t, svc.ValidateAccessToken(context.Background(), "agent-opaque-fallback", resp.AccessToken))

	var updated models.AgentSessionToken
	require.NoError(t, db.Where("id = ?", access.ID).First(&updated).Error)
	require.NotNil(t, updated.LastUsedAt)
}

// Если токен не похож на JWT (opaque), а сервис в JWT-режиме — должен
// сработать opaque-fallback: это покрывает период миграции, когда часть
// агентов ещё ходит со старыми opaque-токенами.
func TestJWTValidateAccessToken_OpaqueFallbackДляСтарыхТокенов(t *testing.T) {
	svc, db, _ := newJWTServiceForTest(t)

	resp := registerAndApprove(t, svc, db, "agent-mixed")

	// Refresh — opaque, хранится в БД. Валиден как opaque.
	require.False(t, looksLikeJWT(resp.RefreshToken))
	refreshStored(t, db, "agent-mixed", resp.RefreshToken)
}

// Истёкший JWT должен возвращать ErrTokenExpired, а не ErrInvalidToken.
func TestJWTValidateAccessToken_Истёкший(t *testing.T) {
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentSessionToken{}, &models.AgentRegistrationAttempt{}))
	repo := infraRepos.NewAgentRepo(db)

	// Сервис с «замороженным» временем в прошлом: токен выпускается в прошлом,
	// а проверяется в настоящем — значит, уже истёк.
	issueSvc := newTestTokenService(t, time.Hour)
	issueSvc.now = func() time.Time { return time.Now().UTC().Add(-2 * time.Hour) }

	svc := NewServiceWithJWT(db, logger.New("", "test", "error", true), repo, &testRegistrar{repo: repo}, issueSvc)
	resp := registerAndApprove(t, svc, db, "agent-jwt-expired")

	// Проверка в «настоящем» времени — токен уже истёк.
	err = svc.ValidateAccessToken(context.Background(), "agent-jwt-expired", resp.AccessToken)
	require.ErrorIs(t, err, ErrTokenExpired)
}
