//go:build windows

package state

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	valueAgentUUID             = "AgentUUID"
	valueMachineFingerprint    = "MachineFingerprintHash"
	valueAccessToken           = "AccessTokenEnc"
	valueRefreshToken          = "RefreshTokenEnc"
	valueAccessTokenExpiresAt  = "AccessTokenExpiresAt"
	valueRefreshTokenExpiresAt = "RefreshTokenExpiresAt"
	valueLastTokenRefreshAt    = "LastTokenRefreshAt"
)

type Identity struct {
	UUID            string
	FingerprintHash string
	ResetPerformed  bool
}

type Tokens struct {
	AccessToken           string
	RefreshToken          string
	AccessTokenExpiresAt  time.Time
	RefreshTokenExpiresAt time.Time
	LastTokenRefreshAt    time.Time
}

type RegistryStore struct {
	registryPath string
}

func NewRegistryStore(registryPath string) *RegistryStore {
	return &RegistryStore{registryPath: strings.TrimSpace(registryPath)}
}

func (s *RegistryStore) EnsureIdentity(currentFingerprintHash string, uuidFactory func() (string, error)) (*Identity, error) {
	key, err := s.openOrCreate()
	if err != nil {
		return nil, err
	}
	defer key.Close()

	storedUUID, _, _ := key.GetStringValue(valueAgentUUID)
	storedFP, _, _ := key.GetStringValue(valueMachineFingerprint)
	storedUUID = strings.TrimSpace(storedUUID)
	storedFP = strings.TrimSpace(storedFP)

	identity := &Identity{UUID: storedUUID, FingerprintHash: currentFingerprintHash}
	if storedUUID == "" || storedFP == "" || !strings.EqualFold(storedFP, currentFingerprintHash) {
		if storedUUID != "" || storedFP != "" {
			identity.ResetPerformed = true
		}
		if err := s.clearIdentityAndTokens(key); err != nil {
			return nil, err
		}
		newUUID, err := uuidFactory()
		if err != nil {
			return nil, err
		}
		if err := key.SetStringValue(valueAgentUUID, newUUID); err != nil {
			return nil, fmt.Errorf("не удалось сохранить AgentUUID в реестр: %w", err)
		}
		if err := key.SetStringValue(valueMachineFingerprint, currentFingerprintHash); err != nil {
			return nil, fmt.Errorf("не удалось сохранить fingerprint в реестр: %w", err)
		}
		identity.UUID = newUUID
	}

	return identity, nil
}

func (s *RegistryStore) LoadTokens() (*Tokens, error) {
	key, err := s.openReadonly()
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer key.Close()

	accessEnc, _, err1 := key.GetStringValue(valueAccessToken)
	refreshEnc, _, err2 := key.GetStringValue(valueRefreshToken)
	accessExpRaw, _, err3 := key.GetStringValue(valueAccessTokenExpiresAt)
	refreshExpRaw, _, err4 := key.GetStringValue(valueRefreshTokenExpiresAt)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return nil, nil
	}

	accessToken, err := s.decryptString(strings.TrimSpace(accessEnc))
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать access token: %w", err)
	}
	refreshToken, err := s.decryptString(strings.TrimSpace(refreshEnc))
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать refresh token: %w", err)
	}
	accessExp, err := time.Parse(time.RFC3339, accessExpRaw)
	if err != nil {
		return nil, nil
	}
	refreshExp, err := time.Parse(time.RFC3339, refreshExpRaw)
	if err != nil {
		return nil, nil
	}

	var lastRefresh time.Time
	if raw, _, err := key.GetStringValue(valueLastTokenRefreshAt); err == nil {
		if parsed, parseErr := time.Parse(time.RFC3339, raw); parseErr == nil {
			lastRefresh = parsed
		}
	}

	return &Tokens{
		AccessToken:           accessToken,
		RefreshToken:          refreshToken,
		AccessTokenExpiresAt:  accessExp,
		RefreshTokenExpiresAt: refreshExp,
		LastTokenRefreshAt:    lastRefresh,
	}, nil
}

func (s *RegistryStore) SaveTokens(tokens Tokens) error {
	key, err := s.openOrCreate()
	if err != nil {
		return err
	}
	defer key.Close()

	accessEnc, err := s.encryptString(tokens.AccessToken)
	if err != nil {
		return err
	}
	refreshEnc, err := s.encryptString(tokens.RefreshToken)
	if err != nil {
		return err
	}

	if err := key.SetStringValue(valueAccessToken, accessEnc); err != nil {
		return fmt.Errorf("не удалось сохранить access token: %w", err)
	}
	if err := key.SetStringValue(valueRefreshToken, refreshEnc); err != nil {
		return fmt.Errorf("не удалось сохранить refresh token: %w", err)
	}
	if err := key.SetStringValue(valueAccessTokenExpiresAt, tokens.AccessTokenExpiresAt.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if err := key.SetStringValue(valueRefreshTokenExpiresAt, tokens.RefreshTokenExpiresAt.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if !tokens.LastTokenRefreshAt.IsZero() {
		if err := key.SetStringValue(valueLastTokenRefreshAt, tokens.LastTokenRefreshAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func (s *RegistryStore) ClearTokens() error {
	key, err := s.openOrCreate()
	if err != nil {
		return err
	}
	defer key.Close()
	for _, name := range []string{
		valueAccessToken, valueRefreshToken, valueAccessTokenExpiresAt,
		valueRefreshTokenExpiresAt, valueLastTokenRefreshAt,
	} {
		_ = key.DeleteValue(name)
	}
	return nil
}

func (s *RegistryStore) CollectRegistrationSystemInfo(agentProcessName string) map[string]interface{} {
	host, _ := os.Hostname()
	return map[string]interface{}{
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"hostname":      strings.TrimSpace(host),
		"agent_process": strings.TrimSpace(agentProcessName),
		"registry_path": s.registryPath,
	}
}

func (s *RegistryStore) openOrCreate() (registry.Key, error) {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, s.registryPath, registry.ALL_ACCESS)
	if err != nil {
		return 0, fmt.Errorf("не удалось открыть HKLM\\%s (нужны права администратора): %w", s.registryPath, err)
	}
	return key, nil
}

func (s *RegistryStore) openReadonly() (registry.Key, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, s.registryPath, registry.READ)
	if err != nil {
		return 0, err
	}
	return key, nil
}

func (s *RegistryStore) clearIdentityAndTokens(key registry.Key) error {
	for _, name := range []string{
		valueAgentUUID, valueMachineFingerprint, valueAccessToken, valueRefreshToken,
		valueAccessTokenExpiresAt, valueRefreshTokenExpiresAt, valueLastTokenRefreshAt,
	} {
		_ = key.DeleteValue(name)
	}
	return nil
}

func (s *RegistryStore) encryptString(plain string) (string, error) {
	cipher, err := ProtectMachineScope([]byte(plain))
	if err != nil {
		return "", fmt.Errorf("ошибка DPAPI encrypt: %w", err)
	}
	return base64.StdEncoding.EncodeToString(cipher), nil
}

func (s *RegistryStore) decryptString(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	plain, err := Unprotect(raw)
	if err != nil {
		return "", fmt.Errorf("ошибка DPAPI decrypt: %w", err)
	}
	return string(plain), nil
}
