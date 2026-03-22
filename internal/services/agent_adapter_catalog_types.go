package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

const (
	agentAdapterChannelStable = "stable"
	agentAdapterChannelLatest = "latest"
)

var ErrAgentAdapterCatalogDisabled = errors.New("s3-синхронизация каталога адаптеров отключена")
var ErrAgentAdapterObjectNotFound = errors.New("объект каталога адаптеров не найден")

type AgentAdapterObjectInfo struct {
	Size         int64
	LastModified time.Time
}

type AgentAdapterObjectStore interface {
	GetObject(ctx context.Context, key string) ([]byte, error)
	PutObject(ctx context.Context, key string, body []byte, contentType string) error
	PutFile(ctx context.Context, key string, filePath string, contentType string) error
	StatObject(ctx context.Context, key string) (AgentAdapterObjectInfo, error)
}

type AgentAdapterCatalogSyncService interface {
	Start(ctx context.Context)
	Refresh(ctx context.Context) (AgentAdapterCatalogRefreshResult, error)
}

type AgentAdapterCatalogRefreshResult struct {
	AdaptersCount   int       `json:"adapters_count"`
	ReleasesUpserted int      `json:"releases_upserted"`
	ChannelsUpserted int      `json:"channels_upserted"`
	ReleasesDeleted  int      `json:"releases_deleted"`
	ChannelsDeleted  int      `json:"channels_deleted"`
	RefreshedAt      time.Time `json:"refreshed_at"`
}

type AgentAdapterCatalogIndex struct {
	SchemaVersion int                               `json:"schema_version"`
	GeneratedAt   time.Time                         `json:"generated_at,omitzero"`
	Adapters      []AgentAdapterCatalogIndexAdapter `json:"adapters,omitzero"`
}

type AgentAdapterCatalogIndexAdapter struct {
	AdapterID string                           `json:"adapter_id"`
	Releases  []AgentAdapterCatalogIndexRelease `json:"releases,omitzero"`
}

type AgentAdapterCatalogIndexRelease struct {
	Version    string `json:"version"`
	TargetOS   string `json:"target_os"`
	TargetArch string `json:"target_arch"`
	ReleaseKey string `json:"release_key"`
}

type AgentAdapterReleaseManifest struct {
	AdapterID       string    `json:"adapter_id"`
	Version         string    `json:"version"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	AdapterType     string    `json:"adapter_type"`
	TargetOS        string    `json:"target_os"`
	TargetArch      string    `json:"target_arch"`
	ProtocolVersion string    `json:"protocol_version"`
	FileName        string    `json:"file_name"`
	DownloadURL     string    `json:"download_url"`
	SHA256          string    `json:"sha256"`
	SourceKey       string    `json:"source_key"`
	Published       bool      `json:"published"`
	CreatedAt       time.Time `json:"created_at,omitzero"`
	UpdatedAt       time.Time `json:"updated_at,omitzero"`
}

type AgentAdapterChannelPointer struct {
	AdapterID  string    `json:"adapter_id"`
	Channel    string    `json:"channel"`
	Version    string    `json:"version"`
	TargetOS   string    `json:"target_os"`
	TargetArch string    `json:"target_arch"`
	ReleaseKey string    `json:"release_key"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
}

type AgentAdapterPublishRequest struct {
	FilePath        string
	AdapterID       string
	Version         string
	Title           string
	Description     string
	AdapterType     string
	TargetOS        string
	TargetArch      string
	ProtocolVersion string
	PromoteChannels []string
}

type AgentAdapterPublishResult struct {
	ReleaseKey  string
	BinaryKey   string
	SHA256      string
	DownloadURL string
	Channels    []string
}

type AgentAdapterPromoteRequest struct {
	AdapterID string
	Version   string
	TargetOS  string
	TargetArch string
	Channels  []string
}

type AgentAdapterPromoteResult struct {
	ReleaseKey string
	Channels   []string
}

func normalizeAgentAdapterCatalogIndex(index AgentAdapterCatalogIndex) AgentAdapterCatalogIndex {
	index.SchemaVersion = max(1, index.SchemaVersion)
	index.Adapters = slicesClipMap(index.Adapters, normalizeAgentAdapterCatalogIndexAdapter)
	return index
}

func normalizeAgentAdapterCatalogIndexAdapter(item AgentAdapterCatalogIndexAdapter) AgentAdapterCatalogIndexAdapter {
	item.AdapterID = normalizeLower(item.AdapterID)
	item.Releases = slicesClipMap(item.Releases, normalizeAgentAdapterCatalogIndexRelease)
	return item
}

func normalizeAgentAdapterCatalogIndexRelease(item AgentAdapterCatalogIndexRelease) AgentAdapterCatalogIndexRelease {
	item.Version = strings.TrimSpace(item.Version)
	item.TargetOS = normalizeLower(item.TargetOS)
	item.TargetArch = normalizeLower(item.TargetArch)
	item.ReleaseKey = normalizeObjectKey(item.ReleaseKey)
	return item
}

func normalizeAgentAdapterReleaseManifest(item AgentAdapterReleaseManifest) AgentAdapterReleaseManifest {
	item.AdapterID = normalizeLower(item.AdapterID)
	item.Version = strings.TrimSpace(item.Version)
	item.Title = strings.TrimSpace(item.Title)
	item.Description = strings.TrimSpace(item.Description)
	item.AdapterType = normalizeLower(item.AdapterType)
	item.TargetOS = normalizeLower(item.TargetOS)
	item.TargetArch = normalizeLower(item.TargetArch)
	item.ProtocolVersion = strings.TrimSpace(item.ProtocolVersion)
	item.FileName = strings.TrimSpace(item.FileName)
	item.DownloadURL = strings.TrimSpace(item.DownloadURL)
	item.SHA256 = normalizeLower(item.SHA256)
	item.SourceKey = normalizeObjectKey(item.SourceKey)
	return item
}

func normalizeAgentAdapterChannelPointer(item AgentAdapterChannelPointer) AgentAdapterChannelPointer {
	item.AdapterID = normalizeLower(item.AdapterID)
	item.Channel = normalizeAgentAdapterChannel(item.Channel)
	item.Version = strings.TrimSpace(item.Version)
	item.TargetOS = normalizeLower(item.TargetOS)
	item.TargetArch = normalizeLower(item.TargetArch)
	item.ReleaseKey = normalizeObjectKey(item.ReleaseKey)
	return item
}

func normalizeAgentAdapterPublishRequest(req AgentAdapterPublishRequest) AgentAdapterPublishRequest {
	req.FilePath = strings.TrimSpace(req.FilePath)
	req.AdapterID = normalizeLower(req.AdapterID)
	req.Version = strings.TrimSpace(req.Version)
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.AdapterType = defaultStr(normalizeLower(req.AdapterType), req.AdapterID)
	req.TargetOS = normalizeLower(req.TargetOS)
	req.TargetArch = normalizeLower(req.TargetArch)
	req.ProtocolVersion = defaultStr(strings.TrimSpace(req.ProtocolVersion), "1")
	req.PromoteChannels = normalizeAgentAdapterChannels(req.PromoteChannels)
	return req
}

func normalizeAgentAdapterPromoteRequest(req AgentAdapterPromoteRequest) AgentAdapterPromoteRequest {
	req.AdapterID = normalizeLower(req.AdapterID)
	req.Version = strings.TrimSpace(req.Version)
	req.TargetOS = normalizeLower(req.TargetOS)
	req.TargetArch = normalizeLower(req.TargetArch)
	req.Channels = normalizeAgentAdapterChannels(req.Channels)
	return req
}

func normalizeAgentAdapterChannel(value string) string {
	return defaultStr(normalizeLower(value), agentAdapterChannelStable)
}

func normalizeAgentAdapterChannels(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		channel := normalizeLower(value)
		if channel == "" {
			continue
		}
		if _, exists := seen[channel]; exists {
			continue
		}
		seen[channel] = struct{}{}
		out = append(out, channel)
	}
	slices.Sort(out)
	return slices.Clip(out)
}

func normalizeObjectKey(value string) string {
	return strings.Trim(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")), "/")
}

func normalizePublicBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func releaseManifestMandatoryFields(item AgentAdapterReleaseManifest) []string {
	missingFields := make([]string, 0, 10)
	if item.AdapterID == "" {
		missingFields = append(missingFields, "adapter_id")
	}
	if item.Version == "" {
		missingFields = append(missingFields, "version")
	}
	if item.Title == "" {
		missingFields = append(missingFields, "title")
	}
	if item.AdapterType == "" {
		missingFields = append(missingFields, "adapter_type")
	}
	if item.TargetOS == "" {
		missingFields = append(missingFields, "target_os")
	}
	if item.TargetArch == "" {
		missingFields = append(missingFields, "target_arch")
	}
	if item.ProtocolVersion == "" {
		missingFields = append(missingFields, "protocol_version")
	}
	if item.FileName == "" {
		missingFields = append(missingFields, "file_name")
	}
	if item.DownloadURL == "" {
		missingFields = append(missingFields, "download_url")
	}
	if item.SHA256 == "" {
		missingFields = append(missingFields, "sha256")
	}
	if item.SourceKey == "" {
		missingFields = append(missingFields, "source_key")
	}
	return missingFields
}

func buildAgentAdapterReleaseDir(adapterID, version, targetOS, targetArch string) string {
	return path.Join("adapters", normalizeLower(adapterID), "releases", strings.TrimSpace(version), normalizeLower(targetOS), normalizeLower(targetArch))
}

func buildAgentAdapterReleaseKey(adapterID, version, targetOS, targetArch string) string {
	return path.Join(buildAgentAdapterReleaseDir(adapterID, version, targetOS, targetArch), "release.json")
}

func buildAgentAdapterSHA256Key(adapterID, version, targetOS, targetArch string) string {
	return path.Join(buildAgentAdapterReleaseDir(adapterID, version, targetOS, targetArch), "sha256.txt")
}

func buildAgentAdapterBinaryKey(adapterID, version, targetOS, targetArch, fileName string) string {
	return path.Join(buildAgentAdapterReleaseDir(adapterID, version, targetOS, targetArch), strings.TrimSpace(fileName))
}

func buildAgentAdapterChannelKey(adapterID, channel string) string {
	return path.Join("adapters", normalizeLower(adapterID), "channels", normalizeAgentAdapterChannel(channel)+".json")
}

func buildAgentAdapterDownloadURL(publicBaseURL, sourceKey string) string {
	base := normalizePublicBaseURL(publicBaseURL)
	key := normalizeObjectKey(sourceKey)
	if base == "" || key == "" {
		return ""
	}
	return base + "/" + key
}

func sourceKeyFromDownloadURL(downloadURL, fileName, adapterID string) string {
	parsed, err := url.Parse(strings.TrimSpace(downloadURL))
	if err == nil {
		if key := normalizeObjectKey(parsed.Path); key != "" {
			return key
		}
	}

	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return ""
	}
	return path.Join("legacy", normalizeLower(adapterID), fileName)
}

func adapterReleaseLookupKey(adapterID, version, targetOS, targetArch string) string {
	return fmt.Sprintf("%s|%s|%s|%s", normalizeLower(adapterID), strings.TrimSpace(version), normalizeLower(targetOS), normalizeLower(targetArch))
}

func slicesClipMap[T any](items []T, normalize func(T) T) []T {
	if len(items) == 0 {
		return nil
	}

	out := make([]T, 0, len(items))
	for _, item := range items {
		out = append(out, normalize(item))
	}
	return slices.Clip(out)
}
