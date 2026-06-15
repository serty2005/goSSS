package agentauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"etalon-server/internal/infra/config"
)

// newTestTokenService создаёт JWT-сервис с фиксированным временем для тестов.
func newTestTokenService(t *testing.T, accessTTL time.Duration) *EdDSATokenService {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	return &EdDSATokenService{
		priv:      priv,
		pub:       pub,
		accessTTL: accessTTL,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func TestEdDSAService_ВыпускИВалидацияТокенаБезБД(t *testing.T) {
	svc := newTestTokenService(t, time.Hour)
	const agentUUID = "agent-jwt-1"

	raw, expiresAt, err := svc.Issue(agentUUID)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.True(t, looksLikeJWT(raw), "выпущенный токен должен определяться как JWT")
	require.True(t, expiresAt.After(time.Now()))

	// Допустимый токен для своего агента валидируется без обращения к БД.
	require.NoError(t, svc.Verify(raw, agentUUID))
}

func TestEdDSAService_ЧужойSubjectОтклоняется(t *testing.T) {
	svc := newTestTokenService(t, time.Hour)
	raw, _, err := svc.Issue("agent-a")
	require.NoError(t, err)

	// Подпись верна, но subject не совпадает — доступ запрещён.
	err = svc.Verify(raw, "agent-b")
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestEdDSAService_ИстёкшийТокенВозвращаетErrTokenExpired(t *testing.T) {
	svc := newTestTokenService(t, time.Hour)
	// Выпускаем токен «в прошлом», а проверяем в настоящем.
	past := time.Now().UTC().Add(-2 * time.Hour)
	svc.now = func() time.Time { return past }
	raw, _, err := svc.Issue("agent-exp")
	require.NoError(t, err)

	// Возвращаем настоящее время и проверяем — токен уже истёк.
	svc.now = func() time.Time { return time.Now().UTC() }
	err = svc.Verify(raw, "agent-exp")
	require.ErrorIs(t, err, ErrTokenExpired)
}

func TestEdDSAService_ПодделаннаяПодписьОтклоняется(t *testing.T) {
	legit := newTestTokenService(t, time.Hour)
	attacker := newTestTokenService(t, time.Hour)

	// Агент выпускает токен легитимным ключом.
	raw, _, err := legit.Issue("agent-legit")
	require.NoError(t, err)

	// Атакующий проверяет токен своим публичным ключом — подпись не сойдётся.
	err = attacker.Verify(raw, "agent-legit")
	require.ErrorIs(t, err, ErrInvalidToken)
}

func TestLoadOrGenerateKeys_ЗаданныйПриватныйКлючПарсится(t *testing.T) {
	// Генерируем ключевую пару и сериализуем приватный ключ в PKCS#8 PEM,
	// как это делает x509.MarshalPKCS8PrivateKey — именно этот формат ожидает
	// jwt.ParseEdPrivateKeyFromPEM.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	privPKCS8, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privPKCS8,
	})

	parsedPriv, parsedPub, err := loadOrGenerateKeys(string(privPEM), "")
	require.NoError(t, err)
	require.Equal(t, priv, ed25519.PrivateKey(parsedPriv))
	require.Equal(t, pub, ed25519.PublicKey(parsedPub))
}

func TestNewAccessTokenService_БезКлючаВозвращаетNil(t *testing.T) {
	// JWTEnabled=false → opaque-режим, сервис токенов не нужен.
	svc, err := NewAccessTokenService(config.AgentAuthConfig{JWTEnabled: false})
	require.NoError(t, err)
	require.Nil(t, svc)
}

func TestNewAccessTokenService_АвтогенерацияКлючаПриПустомENV(t *testing.T) {
	// Приватный ключ не задан, но JWTEnabled=true → ключ генерируется в памяти.
	svc, err := NewAccessTokenService(config.AgentAuthConfig{
		JWTEnabled: true,
		AccessTTL:  30 * time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, svc)
	require.Equal(t, 30*time.Minute, svc.accessTTL)

	// Выпущенный токен должен валидироваться тем же сервисом.
	raw, _, err := svc.Issue("agent-auto")
	require.NoError(t, err)
	require.NoError(t, svc.Verify(raw, "agent-auto"))
}

func TestLoadOrGenerateKeys_ПубличныйКлючБезПриватногоОшибка(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})

	_, _, err = loadOrGenerateKeys("", string(pubPEM))
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "публичный")
}

// Проверяем, что в JWT-сервисе header.typ действительно равен "access" — это
// позволяет middleware отличать агентские access-токены от пользовательских JWT.
func TestEdDSAService_HeaderTypAccess(t *testing.T) {
	svc := newTestTokenService(t, time.Hour)
	raw, _, err := svc.Issue("agent-header")
	require.NoError(t, err)

	parts := strings.SplitN(raw, ".", 2)
	require.Len(t, parts, 2)

	// Декодируем header и проверяем поле typ.
	decoded, _, err := jwt.NewParser().ParseUnverified(raw, &jwt.RegisteredClaims{})
	require.NoError(t, err)
	require.Equal(t, agentAccessTokenType, decoded.Header["typ"])
}
