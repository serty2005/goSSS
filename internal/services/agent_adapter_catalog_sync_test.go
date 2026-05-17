package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/db"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type fileAgentAdapterObjectStore struct {
	root string
}

func (s fileAgentAdapterObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	body, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(key)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrAgentAdapterObjectNotFound, key)
		}
		return nil, err
	}
	return body, nil
}

func (s fileAgentAdapterObjectStore) PutObject(context.Context, string, []byte, string) error {
	return errors.New("операция записи не поддерживается read-only фикстурой")
}

func (s fileAgentAdapterObjectStore) PutFile(context.Context, string, string, string) error {
	return errors.New("операция записи не поддерживается read-only фикстурой")
}

func (s fileAgentAdapterObjectStore) StatObject(_ context.Context, key string) (AgentAdapterObjectInfo, error) {
	info, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(key)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return AgentAdapterObjectInfo{}, fmt.Errorf("%w: %s", ErrAgentAdapterObjectNotFound, key)
		}
		return AgentAdapterObjectInfo{}, err
	}
	return AgentAdapterObjectInfo{
		Size:         info.Size(),
		LastModified: info.ModTime().UTC(),
	}, nil
}

type memoryAgentAdapterObjectStore struct {
	objects map[string][]byte
}

func newMemoryAgentAdapterObjectStore() *memoryAgentAdapterObjectStore {
	return &memoryAgentAdapterObjectStore{
		objects: make(map[string][]byte),
	}
}

func (s *memoryAgentAdapterObjectStore) GetObject(_ context.Context, key string) ([]byte, error) {
	body, ok := s.objects[normalizeObjectKey(key)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAgentAdapterObjectNotFound, key)
	}
	return append([]byte(nil), body...), nil
}

func (s *memoryAgentAdapterObjectStore) PutObject(_ context.Context, key string, body []byte, _ string) error {
	s.objects[normalizeObjectKey(key)] = append([]byte(nil), body...)
	return nil
}

func (s *memoryAgentAdapterObjectStore) PutFile(_ context.Context, key string, filePath string, _ string) error {
	body, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	s.objects[normalizeObjectKey(key)] = body
	return nil
}

func (s *memoryAgentAdapterObjectStore) StatObject(_ context.Context, key string) (AgentAdapterObjectInfo, error) {
	body, ok := s.objects[normalizeObjectKey(key)]
	if !ok {
		return AgentAdapterObjectInfo{}, fmt.Errorf("%w: %s", ErrAgentAdapterObjectNotFound, key)
	}
	return AgentAdapterObjectInfo{
		Size:         int64(len(body)),
		LastModified: time.Now().UTC(),
	}, nil
}

func setupAgentAdapterCatalogFixture(t *testing.T, name string) string {
	t.Helper()

	root := t.TempDir()
	writeObject := func(key string, body []byte) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(key))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, body, 0o644))
	}

	switch name {
	case "valid":
		index := AgentAdapterCatalogIndex{
			SchemaVersion: 1,
			Adapters: []AgentAdapterCatalogIndexAdapter{
				{
					AdapterID: "fiscal-atol",
					Releases: []AgentAdapterCatalogIndexRelease{
						{
							Version:    "1.2.3",
							TargetOS:   "windows",
							TargetArch: "amd64",
							ReleaseKey: buildAgentAdapterReleaseKey("fiscal-atol", "1.2.3", "windows", "amd64"),
						},
						{
							Version:    "1.3.0",
							TargetOS:   "windows",
							TargetArch: "amd64",
							ReleaseKey: buildAgentAdapterReleaseKey("fiscal-atol", "1.3.0", "windows", "amd64"),
						},
					},
				},
				{
					AdapterID: "fiscal-mitsu",
					Releases: []AgentAdapterCatalogIndexRelease{
						{
							Version:    "2.0.0",
							TargetOS:   "windows",
							TargetArch: "amd64",
							ReleaseKey: buildAgentAdapterReleaseKey("fiscal-mitsu", "2.0.0", "windows", "amd64"),
						},
					},
				},
			},
		}
		writeJSONFixture(t, writeObject, "catalog/index.json", index)
		writeReleaseFixture(t, writeObject, "fiscal-atol", "1.2.3", "Фискальный адаптер АТОЛ", "windows", "amd64")
		writeReleaseFixture(t, writeObject, "fiscal-atol", "1.3.0", "Фискальный адаптер АТОЛ", "windows", "amd64")
		writeReleaseFixture(t, writeObject, "fiscal-mitsu", "2.0.0", "Фискальный адаптер Mitsu", "windows", "amd64")
		writeChannelFixture(t, writeObject, "fiscal-atol", "stable", "1.2.3", "windows", "amd64")
		writeChannelFixture(t, writeObject, "fiscal-atol", "latest", "1.3.0", "windows", "amd64")
		writeChannelFixture(t, writeObject, "fiscal-mitsu", "stable", "2.0.0", "windows", "amd64")
		writeChannelFixture(t, writeObject, "fiscal-mitsu", "latest", "2.0.0", "windows", "amd64")
	case "incomplete":
		releaseKey := buildAgentAdapterReleaseKey("broken-adapter", "0.1.0", "windows", "amd64")
		index := AgentAdapterCatalogIndex{
			SchemaVersion: 1,
			Adapters: []AgentAdapterCatalogIndexAdapter{
				{
					AdapterID: "broken-adapter",
					Releases: []AgentAdapterCatalogIndexRelease{
						{
							Version:    "0.1.0",
							TargetOS:   "windows",
							TargetArch: "amd64",
							ReleaseKey: releaseKey,
						},
					},
				},
			},
		}
		writeJSONFixture(t, writeObject, "catalog/index.json", index)
		writeJSONFixture(t, writeObject, releaseKey, AgentAdapterReleaseManifest{
			AdapterID:       "broken-adapter",
			Version:         "0.1.0",
			Title:           "Неполный адаптер",
			AdapterType:     "broken-adapter",
			TargetOS:        "windows",
			TargetArch:      "amd64",
			ProtocolVersion: "1",
			FileName:        "broken-adapter-0.1.0.exe",
			SourceKey:       buildAgentAdapterBinaryKey("broken-adapter", "0.1.0", "windows", "amd64", "broken-adapter-0.1.0.exe"),
			Published:       true,
		})
		writeChannelFixture(t, writeObject, "broken-adapter", "stable", "0.1.0", "windows", "amd64")
	case "broken-index":
		writeObject("catalog/index.json", []byte(`{"schema_version":1,"adapters":[`))
	default:
		t.Fatalf("неизвестная фикстура каталога адаптеров: %s", name)
	}

	return root
}

func writeReleaseFixture(
	t *testing.T,
	writeObject func(string, []byte),
	adapterID string,
	version string,
	title string,
	targetOS string,
	targetArch string,
) {
	t.Helper()

	fileName := fmt.Sprintf("%s-%s.exe", adapterID, version)
	sourceKey := buildAgentAdapterBinaryKey(adapterID, version, targetOS, targetArch, fileName)
	binary := []byte(fmt.Sprintf("%s/%s/%s/%s", adapterID, version, targetOS, targetArch))
	sum := sha256.Sum256(binary)
	digest := hex.EncodeToString(sum[:])
	writeObject(sourceKey, binary)
	writeObject(buildAgentAdapterSHA256Key(adapterID, version, targetOS, targetArch), []byte(digest))
	writeJSONFixture(t, writeObject, buildAgentAdapterReleaseKey(adapterID, version, targetOS, targetArch), AgentAdapterReleaseManifest{
		AdapterID:       adapterID,
		Version:         version,
		Title:           title,
		Description:     "Тестовый релиз каталога адаптеров",
		AdapterType:     adapterID,
		TargetOS:        targetOS,
		TargetArch:      targetArch,
		ProtocolVersion: "1",
		FileName:        fileName,
		SHA256:          digest,
		SourceKey:       sourceKey,
		Published:       true,
	})
}

func writeChannelFixture(
	t *testing.T,
	writeObject func(string, []byte),
	adapterID string,
	channel string,
	version string,
	targetOS string,
	targetArch string,
) {
	t.Helper()

	writeJSONFixture(t, writeObject, buildAgentAdapterChannelKey(adapterID, channel), AgentAdapterChannelPointer{
		AdapterID:  adapterID,
		Channel:    channel,
		Version:    version,
		TargetOS:   targetOS,
		TargetArch: targetArch,
		ReleaseKey: buildAgentAdapterReleaseKey(adapterID, version, targetOS, targetArch),
	})
}

func writeJSONFixture(t *testing.T, writeObject func(string, []byte), key string, payload any) {
	t.Helper()

	raw, err := json.MarshalIndent(payload, "", "  ")
	require.NoError(t, err)
	writeObject(key, raw)
}

func TestAgentAdapterCatalogSync_СинхронизируетФикстуруИзS3ВБД(t *testing.T) {
	ctx := t.Context()
	database := setupAgentAdapterCatalogDB(t)
	service := NewAgentAdapterCatalogSyncService(
		database,
		logger.New("", "test", "error", true),
		fileAgentAdapterObjectStore{root: setupAgentAdapterCatalogFixture(t, "valid")},
		&config.Config{
			AgentAdapterCatalog: config.AgentAdapterCatalogConfig{
				Enabled:        true,
				CatalogKey:     "catalog/index.json",
				DefaultChannel: "stable",
				PublicBaseURL:  "https://etalon.serty.top/agents",
				SyncInterval:   time.Minute,
			},
		},
	)

	result, err := service.Refresh(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, result.AdaptersCount)
	require.Equal(t, 3, result.ReleasesUpserted)
	require.Equal(t, 4, result.ChannelsUpserted)

	operatorFlow := NewAgentOperatorFlowService(database, "stable").(*agentOperatorFlowService)
	options, err := operatorFlow.listPublishedAdapterOptions(ctx)
	require.NoError(t, err)
	require.Len(t, options, 2)

	atolIndex := slices.IndexFunc(options, func(item api.PublishedAgentAdapterOptionDTO) bool {
		return item.AdapterID == "fiscal-atol"
	})
	require.GreaterOrEqual(t, atolIndex, 0)
	require.True(t, options[atolIndex].Selectable)
	require.Equal(t, "1.2.3", options[atolIndex].StableVersion)
	require.Equal(t, "1.3.0", options[atolIndex].LatestVersion)
	require.Equal(t, "1.2.3", options[atolIndex].Version)
}

func TestAgentAdapterCatalogSync_ResolveSelectedAdapterManifestsИспользуетStableКанал(t *testing.T) {
	ctx := t.Context()
	database := setupAgentAdapterCatalogDB(t)
	service := NewAgentAdapterCatalogSyncService(
		database,
		logger.New("", "test", "error", true),
		fileAgentAdapterObjectStore{root: setupAgentAdapterCatalogFixture(t, "valid")},
		&config.Config{
			AgentAdapterCatalog: config.AgentAdapterCatalogConfig{
				Enabled:        true,
				CatalogKey:     "catalog/index.json",
				DefaultChannel: "stable",
				PublicBaseURL:  "https://etalon.serty.top/agents",
				SyncInterval:   time.Minute,
			},
		},
	)
	_, err := service.Refresh(ctx)
	require.NoError(t, err)

	configJSON, err := json.Marshal(api.AgentConfigDTO{
		SelectedAdapterIDs: []string{"fiscal-atol"},
	})
	require.NoError(t, err)
	agent := &models.Agent{
		UUID:   "agent-stable",
		Type:   "sssruner",
		Status: models.StatusActive,
		Config: datatypes.JSON(configJSON),
	}

	operatorFlow := NewAgentOperatorFlowService(database, "stable")
	manifests, err := operatorFlow.ResolveAgentAdapterManifests(ctx, agent)
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	require.Equal(t, "1.2.3", manifests[0].Version)
	require.Equal(t, "https://etalon.serty.top/agents/adapters/fiscal-atol/releases/1.2.3/windows/amd64/fiscal-atol-1.2.3.exe", manifests[0].DownloadURL)
}

func TestAgentAdapterCatalogSync_ПропускаетНеполныйRelease(t *testing.T) {
	ctx := t.Context()
	database := setupAgentAdapterCatalogDB(t)
	service := NewAgentAdapterCatalogSyncService(
		database,
		logger.New("", "test", "error", true),
		fileAgentAdapterObjectStore{root: setupAgentAdapterCatalogFixture(t, "incomplete")},
		&config.Config{
			AgentAdapterCatalog: config.AgentAdapterCatalogConfig{
				Enabled:        true,
				CatalogKey:     "catalog/index.json",
				DefaultChannel: "stable",
				PublicBaseURL:  "https://etalon.serty.top/agents",
				SyncInterval:   time.Minute,
			},
		},
	)

	_, err := service.Refresh(ctx)
	require.NoError(t, err)

	operatorFlow := NewAgentOperatorFlowService(database, "stable").(*agentOperatorFlowService)
	_, warnings, err := operatorFlow.resolveSelectedAdapterManifests(ctx, []string{"broken-adapter"})
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "manifest неполон")

	options, err := operatorFlow.listPublishedAdapterOptions(ctx)
	require.NoError(t, err)
	require.Len(t, options, 1)
	require.False(t, options[0].Selectable)
	require.Contains(t, options[0].DisabledReason, "Manifest неполон")
}

func TestAgentAdapterCatalogSync_БитыйCatalogIndexНеЗатираетТекущуюБД(t *testing.T) {
	ctx := t.Context()
	database := setupAgentAdapterCatalogDB(t)
	require.NoError(t, db.EnsureDefaultPublishedAgentAdapters(database))

	var beforeCount int64
	require.NoError(t, database.Model(&models.AgentAdapterRelease{}).Count(&beforeCount).Error)
	require.Greater(t, beforeCount, int64(0))

	service := NewAgentAdapterCatalogSyncService(
		database,
		logger.New("", "test", "error", true),
		fileAgentAdapterObjectStore{root: setupAgentAdapterCatalogFixture(t, "broken-index")},
		&config.Config{
			AgentAdapterCatalog: config.AgentAdapterCatalogConfig{
				Enabled:        true,
				CatalogKey:     "catalog/index.json",
				DefaultChannel: "stable",
				PublicBaseURL:  "https://etalon.serty.top/agents",
				SyncInterval:   time.Minute,
			},
		},
	)

	_, err := service.Refresh(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog/index.json")

	var afterCount int64
	require.NoError(t, database.Model(&models.AgentAdapterRelease{}).Count(&afterCount).Error)
	require.Equal(t, beforeCount, afterCount)
}

func TestAgentAdapterPublisher_ПубликуетРелизИГенерируетManifest(t *testing.T) {
	ctx := t.Context()
	store := newMemoryAgentAdapterObjectStore()
	publisher := NewAgentAdapterPublisher(
		logger.New("", "test", "error", true),
		store,
		&config.Config{
			AgentAdapterCatalog: config.AgentAdapterCatalogConfig{
				CatalogKey:    "catalog/index.json",
				PublicBaseURL: "https://etalon.serty.top/agents",
			},
		},
	)

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "fiscal-atol-2.0.0.exe")
	require.NoError(t, os.WriteFile(filePath, []byte("adapter-binary-v2"), 0o644))

	result, err := publisher.Publish(ctx, AgentAdapterPublishRequest{
		FilePath:        filePath,
		AdapterID:       "fiscal-atol",
		Version:         "2.0.0",
		Title:           "Фискальный адаптер АТОЛ",
		Description:     "Боевой релиз для CI",
		TargetOS:        "windows",
		TargetArch:      "amd64",
		ProtocolVersion: "1",
		PromoteChannels: []string{"latest", "stable"},
	})
	require.NoError(t, err)
	require.Equal(t, "adapters/fiscal-atol/releases/2.0.0/windows/amd64/release.json", result.ReleaseKey)
	require.Equal(t, "https://etalon.serty.top/agents/adapters/fiscal-atol/releases/2.0.0/windows/amd64/fiscal-atol-2.0.0.exe", result.DownloadURL)

	rawRelease, err := store.GetObject(ctx, result.ReleaseKey)
	require.NoError(t, err)

	var manifest AgentAdapterReleaseManifest
	require.NoError(t, json.Unmarshal(rawRelease, &manifest))
	require.Equal(t, "fiscal-atol", manifest.AdapterID)
	require.Equal(t, "2.0.0", manifest.Version)
	require.Equal(t, result.SHA256, manifest.SHA256)
	require.Equal(t, "adapters/fiscal-atol/releases/2.0.0/windows/amd64/fiscal-atol-2.0.0.exe", manifest.SourceKey)

	rawIndex, err := store.GetObject(ctx, "catalog/index.json")
	require.NoError(t, err)
	indexText := string(rawIndex)
	require.Contains(t, indexText, result.ReleaseKey)

	rawStable, err := store.GetObject(ctx, "adapters/fiscal-atol/channels/stable.json")
	require.NoError(t, err)
	require.Contains(t, string(rawStable), `"version": "2.0.0"`)
}

func setupAgentAdapterCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(
		&models.Agent{},
		&models.AgentCOMSignatureRule{},
		&models.PublishedAgentAdapter{},
		&models.AgentAdapterRelease{},
		&models.AgentAdapterChannel{},
		&models.AgentCommand{},
	))
	return database
}
