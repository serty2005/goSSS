package agentauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"etalon-server/internal/infra/config"
)

const (
	// agentAccessTokenType — значение claim typ для access-токенов агентов.
	agentAccessTokenType = "access"
	// jwtLeeway — допуск для проверки времени (nbf/exp/iat).
	jwtLeeway = 30 * time.Second
)

// accessTokenIssuer выпускает JWT access-токены агентов.
type accessTokenIssuer interface {
	// Issue подписывает новый access-токен для агента с заданным сроком жизни.
	Issue(agentUUID string, ttl time.Duration) (string, time.Time, error)
}

// accessTokenVerifier проверяет JWT access-токены локально, без обращения к БД.
type accessTokenVerifier interface {
	// Verify проверяет подпись, срок и принадлежность токена агенту.
	Verify(rawToken, agentUUID string) error
}

// EdDSATokenService выпускает и проверяет JWT access-токены агентов по Ed25519.
//
// Один сервис создаётся на процесс agent-gateway. Приватный ключ либо
// подгружается из ENV, либо генерируется в памяти при старте. Публичный ключ
// позволяет любому поду проверять токены без секрета и без обращения к БД.
type EdDSATokenService struct {
	priv      ed25519.PrivateKey
	pub       ed25519.PublicKey
	accessTTL time.Duration
	now       func() time.Time
}

// NewAccessTokenService создаёт JWT-сервис по конфигурации. Возвращает nil, если
// JWT-режим выключен (JWTEnabled=false) — caller должен использовать opaque.
func NewAccessTokenService(cfg config.AgentAuthConfig) (*EdDSATokenService, error) {
	if !cfg.JWTEnabled {
		return nil, nil
	}

	priv, pub, err := loadOrGenerateKeys(cfg.JWTPrivateKeyPEM, cfg.JWTPublicKeyPEM)
	if err != nil {
		return nil, err
	}

	ttl := cfg.AccessTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	return &EdDSATokenService{
		priv:      priv,
		pub:       pub,
		accessTTL: ttl,
		now:       func() time.Time { return time.Now().UTC() },
	}, nil
}

// loadOrGenerateKeys возвращает приватный и публичный Ed25519 ключи.
// Если приватный PEM задан, он парсится; иначе ключ генерируется в памяти.
// Публичный PEM опционален и выводится из приватного, если не задан.
func loadOrGenerateKeys(privatePEM, publicPEM string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if strings.TrimSpace(privatePEM) != "" {
		priv, err := parseEd25519PrivateKey([]byte(privatePEM))
		if err != nil {
			return nil, nil, fmt.Errorf("не удалось распарсить AGENT_JWT_PRIVATE_KEY: %w", err)
		}
		pubAny := ed25519.PrivateKey(priv).Public()
		pub, ok := pubAny.(ed25519.PublicKey)
		if !ok {
			return nil, nil, errors.New("не удалось извлечь публичный ключ Ed25519 из приватного")
		}
		return priv, pub, nil
	}

	// Приватный ключ не задан — генерируем в памяти.
	pubSeed, privSeed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось сгенерировать ключевую пару Ed25519: %w", err)
	}
	if strings.TrimSpace(publicPEM) != "" {
		// Публичный ключ задан, но без приватного — конфигурация бессмысленна:
		// подписать таким сервисом ничего нельзя, поэтому считаем это ошибкой.
		return nil, nil, errors.New("AGENT_JWT_PUBLIC_KEY задан без AGENT_JWT_PRIVATE_KEY — укажите приватный ключ или удалите публичный")
	}
	_ = pubSeed
	return privSeed, pubSeed, nil
}

func parseEd25519PrivateKey(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("ожидался PEM-блок приватного ключа")
	}
	key, err := jwt.ParseEdPrivateKeyFromPEM(pemBytes)
	if err != nil {
		return nil, err
	}
	edKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("ожидается Ed25519 приватный ключ, получен %T", key)
	}
	return edKey, nil
}

// Issue выпускает подписанный access-токен со сроком жизни из конфигурации.
func (s *EdDSATokenService) Issue(agentUUID string) (string, time.Time, error) {
	now := s.now()
	expiresAt := now.Add(s.accessTTL)
	claims := jwt.RegisteredClaims{
		Subject:   agentUUID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	// Тип токена отличает агентские access-токены от пользовательских JWT
	// и позволяет middleware корректно их разделять.
	token.Header["typ"] = agentAccessTokenType
	signed, err := token.SignedString(s.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("не удалось подписать access-токен агента: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify проверяет подпись, срок и принадлежность токена агенту.
// Не обращается к БД — только криптографическая проверка публичным ключом.
func (s *EdDSATokenService) Verify(rawToken, agentUUID string) error {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}), jwt.WithLeeway(jwtLeeway))

	token, err := parser.ParseWithClaims(rawToken, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("неожиданный метод подписи: %v", t.Header["alg"])
		}
		return s.pub, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return ErrTokenExpired
		}
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	claims, ok := token.Claims.(*jwt.RegisteredClaims)
	if !ok || !token.Valid {
		return ErrInvalidToken
	}
	if strings.TrimSpace(claims.Subject) != strings.TrimSpace(agentUUID) {
		return ErrInvalidToken
	}
	return nil
}

// looksLikeJWT возвращает true, если строка по структуре похожа на JWT.
// Используется в ValidateAccessToken для выбора пути проверки (JWT vs opaque).
func looksLikeJWT(raw string) bool {
	// JWT имеет форму header.payload.signature, три сегмента в base64url.
	first := strings.IndexByte(raw, '.')
	if first <= 0 {
		return false
	}
	second := strings.IndexByte(raw[first+1:], '.')
	return second > 0
}
