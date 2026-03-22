package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
)

type AgentAdapterPublisher interface {
	Publish(ctx context.Context, req AgentAdapterPublishRequest) (AgentAdapterPublishResult, error)
	Promote(ctx context.Context, req AgentAdapterPromoteRequest) (AgentAdapterPromoteResult, error)
}

type agentAdapterPublisher struct {
	logger        logger.LoggerInterface
	store         AgentAdapterObjectStore
	catalogKey    string
	publicBaseURL string
	now           func() time.Time
}

func NewAgentAdapterPublisher(
	log logger.LoggerInterface,
	store AgentAdapterObjectStore,
	cfg *config.Config,
) AgentAdapterPublisher {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &agentAdapterPublisher{
		logger:        log,
		store:         store,
		catalogKey:    normalizeObjectKey(cfg.AgentAdapterCatalogKey),
		publicBaseURL: normalizePublicBaseURL(cfg.AgentAdapterPublicBaseURL),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (p *agentAdapterPublisher) Publish(ctx context.Context, req AgentAdapterPublishRequest) (AgentAdapterPublishResult, error) {
	if p.store == nil {
		return AgentAdapterPublishResult{}, ErrAgentAdapterCatalogDisabled
	}

	req = normalizeAgentAdapterPublishRequest(req)
	if err := validateAgentAdapterPublishRequest(req); err != nil {
		return AgentAdapterPublishResult{}, err
	}
	if p.publicBaseURL == "" {
		return AgentAdapterPublishResult{}, errors.New("AGENT_ADAPTER_PUBLIC_BASE_URL обязателен для публикации релиза")
	}

	fileInfo, err := os.Stat(req.FilePath)
	if err != nil {
		return AgentAdapterPublishResult{}, fmt.Errorf("не удалось получить доступ к бинарнику %s: %w", req.FilePath, err)
	}
	if fileInfo.IsDir() {
		return AgentAdapterPublishResult{}, fmt.Errorf("ожидался путь к файлу бинарника, получена директория: %s", req.FilePath)
	}

	fileName := strings.TrimSpace(fileInfo.Name())
	binaryKey := buildAgentAdapterBinaryKey(req.AdapterID, req.Version, req.TargetOS, req.TargetArch, fileName)
	releaseKey := buildAgentAdapterReleaseKey(req.AdapterID, req.Version, req.TargetOS, req.TargetArch)
	shaKey := buildAgentAdapterSHA256Key(req.AdapterID, req.Version, req.TargetOS, req.TargetArch)

	if err := p.ensureImmutableRelease(ctx, binaryKey, releaseKey, shaKey); err != nil {
		return AgentAdapterPublishResult{}, err
	}

	digest, err := computeFileSHA256(req.FilePath)
	if err != nil {
		return AgentAdapterPublishResult{}, err
	}

	now := p.now()
	manifest := normalizeAgentAdapterReleaseManifest(AgentAdapterReleaseManifest{
		AdapterID:       req.AdapterID,
		Version:         req.Version,
		Title:           req.Title,
		Description:     req.Description,
		AdapterType:     req.AdapterType,
		TargetOS:        req.TargetOS,
		TargetArch:      req.TargetArch,
		ProtocolVersion: req.ProtocolVersion,
		FileName:        fileName,
		DownloadURL:     buildAgentAdapterDownloadURL(p.publicBaseURL, binaryKey),
		SHA256:          digest,
		SourceKey:       binaryKey,
		Published:       true,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	releaseJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return AgentAdapterPublishResult{}, fmt.Errorf("не удалось сериализовать release manifest: %w", err)
	}

	if err := p.store.PutFile(ctx, binaryKey, req.FilePath, "application/octet-stream"); err != nil {
		return AgentAdapterPublishResult{}, err
	}
	if err := p.store.PutObject(ctx, shaKey, []byte(digest), "text/plain; charset=utf-8"); err != nil {
		return AgentAdapterPublishResult{}, err
	}
	if err := p.store.PutObject(ctx, releaseKey, releaseJSON, "application/json; charset=utf-8"); err != nil {
		return AgentAdapterPublishResult{}, err
	}

	index, err := p.loadCatalogIndex(ctx)
	if err != nil {
		return AgentAdapterPublishResult{}, err
	}
	index = upsertReleaseIntoCatalogIndex(index, AgentAdapterCatalogIndexRelease{
		Version:    req.Version,
		TargetOS:   req.TargetOS,
		TargetArch: req.TargetArch,
		ReleaseKey: releaseKey,
	}, req.AdapterID)
	if err := p.saveCatalogIndex(ctx, index); err != nil {
		return AgentAdapterPublishResult{}, err
	}
	if err := p.promoteChannels(ctx, manifest, releaseKey, req.PromoteChannels); err != nil {
		return AgentAdapterPublishResult{}, err
	}

	p.logger.Info("Релиз адаптера опубликован",
		"adapter_id", req.AdapterID,
		"version", req.Version,
		"target_os", req.TargetOS,
		"target_arch", req.TargetArch,
		"channels", req.PromoteChannels,
	)

	return AgentAdapterPublishResult{
		ReleaseKey:  releaseKey,
		BinaryKey:   binaryKey,
		SHA256:      digest,
		DownloadURL: manifest.DownloadURL,
		Channels:    slices.Clone(req.PromoteChannels),
	}, nil
}

func (p *agentAdapterPublisher) Promote(ctx context.Context, req AgentAdapterPromoteRequest) (AgentAdapterPromoteResult, error) {
	if p.store == nil {
		return AgentAdapterPromoteResult{}, ErrAgentAdapterCatalogDisabled
	}

	req = normalizeAgentAdapterPromoteRequest(req)
	if err := validateAgentAdapterPromoteRequest(req); err != nil {
		return AgentAdapterPromoteResult{}, err
	}

	releaseKey := buildAgentAdapterReleaseKey(req.AdapterID, req.Version, req.TargetOS, req.TargetArch)
	rawManifest, err := p.store.GetObject(ctx, releaseKey)
	if err != nil {
		return AgentAdapterPromoteResult{}, fmt.Errorf("не удалось загрузить существующий релиз %s: %w", releaseKey, err)
	}

	var manifest AgentAdapterReleaseManifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return AgentAdapterPromoteResult{}, fmt.Errorf("release manifest %s содержит невалидный JSON: %w", releaseKey, err)
	}
	manifest = normalizeAgentAdapterReleaseManifest(manifest)
	manifest.AdapterID = defaultStr(manifest.AdapterID, req.AdapterID)
	manifest.Version = defaultStr(manifest.Version, req.Version)
	manifest.TargetOS = defaultStr(manifest.TargetOS, req.TargetOS)
	manifest.TargetArch = defaultStr(manifest.TargetArch, req.TargetArch)
	if manifest.SourceKey == "" && manifest.FileName != "" {
		manifest.SourceKey = buildAgentAdapterBinaryKey(manifest.AdapterID, manifest.Version, manifest.TargetOS, manifest.TargetArch, manifest.FileName)
	}
	if manifest.DownloadURL == "" {
		manifest.DownloadURL = buildAgentAdapterDownloadURL(p.publicBaseURL, manifest.SourceKey)
	}

	index, err := p.loadCatalogIndex(ctx)
	if err != nil {
		return AgentAdapterPromoteResult{}, err
	}
	index = upsertReleaseIntoCatalogIndex(index, AgentAdapterCatalogIndexRelease{
		Version:    manifest.Version,
		TargetOS:   manifest.TargetOS,
		TargetArch: manifest.TargetArch,
		ReleaseKey: releaseKey,
	}, manifest.AdapterID)
	if err := p.saveCatalogIndex(ctx, index); err != nil {
		return AgentAdapterPromoteResult{}, err
	}
	if err := p.promoteChannels(ctx, manifest, releaseKey, req.Channels); err != nil {
		return AgentAdapterPromoteResult{}, err
	}

	p.logger.Info("Каналы адаптера переключены на существующий релиз",
		"adapter_id", manifest.AdapterID,
		"version", manifest.Version,
		"target_os", manifest.TargetOS,
		"target_arch", manifest.TargetArch,
		"channels", req.Channels,
	)

	return AgentAdapterPromoteResult{
		ReleaseKey: releaseKey,
		Channels:   slices.Clone(req.Channels),
	}, nil
}

func validateAgentAdapterPublishRequest(req AgentAdapterPublishRequest) error {
	switch {
	case req.FilePath == "":
		return errors.New("параметр --file обязателен")
	case req.AdapterID == "":
		return errors.New("параметр --adapter-id обязателен")
	case req.Version == "":
		return errors.New("параметр --version обязателен")
	case req.Title == "":
		return errors.New("параметр --title обязателен")
	case req.TargetOS == "":
		return errors.New("параметр --target-os обязателен")
	case req.TargetArch == "":
		return errors.New("параметр --target-arch обязателен")
	}
	return nil
}

func validateAgentAdapterPromoteRequest(req AgentAdapterPromoteRequest) error {
	switch {
	case req.AdapterID == "":
		return errors.New("параметр --adapter-id обязателен")
	case req.Version == "":
		return errors.New("параметр --version обязателен")
	case req.TargetOS == "":
		return errors.New("параметр --target-os обязателен")
	case req.TargetArch == "":
		return errors.New("параметр --target-arch обязателен")
	case len(req.Channels) == 0:
		return errors.New("нужно указать хотя бы один канал через --channel")
	}
	return nil
}

func computeFileSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("не удалось открыть бинарник %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("не удалось вычислить sha256 файла %s: %w", filePath, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (p *agentAdapterPublisher) ensureImmutableRelease(ctx context.Context, binaryKey, releaseKey, shaKey string) error {
	if _, err := p.store.StatObject(ctx, releaseKey); err == nil {
		return fmt.Errorf("релиз %s уже существует и не может быть переписан", releaseKey)
	} else if err != nil && !errors.Is(err, ErrAgentAdapterObjectNotFound) {
		return err
	}

	partialKeys := make([]string, 0, 2)
	for _, objectKey := range []string{binaryKey, shaKey} {
		if _, err := p.store.StatObject(ctx, objectKey); err == nil {
			partialKeys = append(partialKeys, objectKey)
			continue
		} else if err != nil && !errors.Is(err, ErrAgentAdapterObjectNotFound) {
			return err
		}
	}

	if len(partialKeys) > 0 {
		return fmt.Errorf(
			"обнаружены следы частичной публикации (%s); versioned release URLs должны оставаться immutable",
			strings.Join(partialKeys, ", "),
		)
	}

	return nil
}

func (p *agentAdapterPublisher) loadCatalogIndex(ctx context.Context) (AgentAdapterCatalogIndex, error) {
	raw, err := p.store.GetObject(ctx, p.catalogKey)
	if err != nil {
		if errors.Is(err, ErrAgentAdapterObjectNotFound) {
			return AgentAdapterCatalogIndex{SchemaVersion: 1}, nil
		}
		return AgentAdapterCatalogIndex{}, fmt.Errorf("не удалось загрузить %s: %w", p.catalogKey, err)
	}

	var index AgentAdapterCatalogIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return AgentAdapterCatalogIndex{}, fmt.Errorf("catalog/index.json содержит невалидный JSON: %w", err)
	}
	return normalizeAgentAdapterCatalogIndex(index), nil
}

func (p *agentAdapterPublisher) saveCatalogIndex(ctx context.Context, index AgentAdapterCatalogIndex) error {
	index = normalizeAgentAdapterCatalogIndex(index)
	index.GeneratedAt = p.now()

	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать catalog/index.json: %w", err)
	}
	return p.store.PutObject(ctx, p.catalogKey, raw, "application/json; charset=utf-8")
}

func upsertReleaseIntoCatalogIndex(
	index AgentAdapterCatalogIndex,
	release AgentAdapterCatalogIndexRelease,
	adapterID string,
) AgentAdapterCatalogIndex {
	index = normalizeAgentAdapterCatalogIndex(index)
	release = normalizeAgentAdapterCatalogIndexRelease(release)
	adapterID = normalizeLower(adapterID)

	adapterIndex := slices.IndexFunc(index.Adapters, func(item AgentAdapterCatalogIndexAdapter) bool {
		return item.AdapterID == adapterID
	})
	if adapterIndex < 0 {
		index.Adapters = append(index.Adapters, AgentAdapterCatalogIndexAdapter{
			AdapterID: adapterID,
			Releases:  []AgentAdapterCatalogIndexRelease{release},
		})
	} else {
		releaseIndex := slices.IndexFunc(index.Adapters[adapterIndex].Releases, func(item AgentAdapterCatalogIndexRelease) bool {
			return item.ReleaseKey == release.ReleaseKey
		})
		if releaseIndex < 0 {
			index.Adapters[adapterIndex].Releases = append(index.Adapters[adapterIndex].Releases, release)
		} else {
			index.Adapters[adapterIndex].Releases[releaseIndex] = release
		}
		slices.SortFunc(index.Adapters[adapterIndex].Releases, func(left, right AgentAdapterCatalogIndexRelease) int {
			switch {
			case left.Version != right.Version:
				return strings.Compare(left.Version, right.Version)
			case left.TargetOS != right.TargetOS:
				return strings.Compare(left.TargetOS, right.TargetOS)
			default:
				return strings.Compare(left.TargetArch, right.TargetArch)
			}
		})
		index.Adapters[adapterIndex].Releases = slices.Clip(index.Adapters[adapterIndex].Releases)
	}

	slices.SortFunc(index.Adapters, func(left, right AgentAdapterCatalogIndexAdapter) int {
		return strings.Compare(left.AdapterID, right.AdapterID)
	})
	index.Adapters = slices.Clip(index.Adapters)
	return index
}

func (p *agentAdapterPublisher) promoteChannels(
	ctx context.Context,
	manifest AgentAdapterReleaseManifest,
	releaseKey string,
	channels []string,
) error {
	for _, channel := range normalizeAgentAdapterChannels(channels) {
		pointer := normalizeAgentAdapterChannelPointer(AgentAdapterChannelPointer{
			AdapterID:  manifest.AdapterID,
			Channel:    channel,
			Version:    manifest.Version,
			TargetOS:   manifest.TargetOS,
			TargetArch: manifest.TargetArch,
			ReleaseKey: releaseKey,
			UpdatedAt:  p.now(),
		})
		raw, err := json.MarshalIndent(pointer, "", "  ")
		if err != nil {
			return fmt.Errorf("не удалось сериализовать pointer канала %s: %w", channel, err)
		}
		if err := p.store.PutObject(ctx, buildAgentAdapterChannelKey(manifest.AdapterID, channel), raw, "application/json; charset=utf-8"); err != nil {
			return err
		}
	}
	return nil
}
