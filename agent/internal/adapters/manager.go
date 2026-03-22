package adapters

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type Downloader interface {
	DownloadFile(context.Context, string) ([]byte, error)
}

type Manager struct {
	rootDir        string
	binariesDir    string
	descriptorsDir string
	tmpDir         string
	downloader     Downloader
	targetOS       string
	targetArch     string
}

func NewManager(rootDir string, downloader Downloader) *Manager {
	rootDir = filepath.Clean(strings.TrimSpace(rootDir))
	return &Manager{
		rootDir:        rootDir,
		binariesDir:    filepath.Join(rootDir, "bin"),
		descriptorsDir: filepath.Join(rootDir, "descriptors"),
		tmpDir:         filepath.Join(rootDir, "tmp"),
		downloader:     downloader,
		targetOS:       runtime.GOOS,
		targetArch:     runtime.GOARCH,
	}
}

func (m *Manager) EnsureLayout() error {
	for _, path := range []string{m.rootDir, m.binariesDir, m.descriptorsDir, m.tmpDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("не удалось подготовить каталог адаптеров %s: %w", path, err)
		}
	}
	return nil
}

func (m *Manager) Sync(ctx context.Context, manifests []ManifestItem) ([]Status, error) {
	if err := m.EnsureLayout(); err != nil {
		return nil, err
	}

	var errs []error
	for _, item := range manifests {
		if shouldSkipManifest(item) {
			continue
		}
		if err := m.syncOne(ctx, item); err != nil {
			errs = append(errs, fmt.Errorf("adapter_id=%s: %w", strings.TrimSpace(item.AdapterID), err))
		}
	}

	statuses, err := m.ListStatuses()
	if err != nil {
		errs = append(errs, err)
	}
	return statuses, errors.Join(errs...)
}

func (m *Manager) ListStatuses() ([]Status, error) {
	if err := m.EnsureLayout(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(m.descriptorsDir)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать descriptors адаптеров: %w", err)
	}

	latest := make(map[string]Status)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}

		descriptor, err := m.readDescriptor(filepath.Join(m.descriptorsDir, entry.Name()))
		if err != nil {
			return nil, err
		}

		status := descriptorToStatus(descriptor)
		if status.LocalPath != "" {
			if info, err := os.Stat(status.LocalPath); err == nil && !info.IsDir() {
				status.FileSize = info.Size()
				if status.Status == "" {
					status.Status = "ready"
				}
			} else {
				status.Status = "missing_binary"
				status.LastError = "локальный бинарник не найден"
			}
		}

		current, ok := latest[status.AdapterID]
		if !ok || current.UpdatedAt.Before(status.UpdatedAt) {
			latest[status.AdapterID] = status
		}
	}

	statuses := make([]Status, 0, len(latest))
	for _, status := range latest {
		statuses = append(statuses, status)
	}
	slices.SortFunc(statuses, func(a, b Status) int {
		return cmp.Or(cmp.Compare(a.AdapterID, b.AdapterID), cmp.Compare(a.Version, b.Version))
	})
	return statuses, nil
}

func (m *Manager) Compatible(item ManifestItem) bool {
	targetOS := normalizePlatform(item.TargetOS)
	targetArch := normalizePlatform(item.TargetArch)
	return osCompatible(m.targetOS, targetOS) && archCompatible(m.targetOS, m.targetArch, targetArch)
}

func (m *Manager) syncOne(ctx context.Context, item ManifestItem) error {
	if m.downloader == nil {
		return errors.New("не задан downloader адаптеров")
	}
	if strings.TrimSpace(item.AdapterID) == "" {
		return errors.New("manifest не содержит adapter_id")
	}
	if strings.TrimSpace(item.Version) == "" {
		return errors.New("manifest не содержит version")
	}
	if strings.TrimSpace(item.DownloadURL) == "" {
		return errors.New("manifest не содержит download_url")
	}
	if !m.Compatible(item) {
		return nil
	}

	current, err := m.findCurrentDescriptor(item.AdapterID)
	if err != nil {
		return err
	}
	if current != nil && sameRevision(*current, item) && fileExists(current.LocalPath) {
		return nil
	}

	content, err := m.downloader.DownloadFile(ctx, item.DownloadURL)
	if err != nil {
		return fmt.Errorf("не удалось скачать адаптер: %w", err)
	}

	actualSHA := checksum(content)
	if expected := strings.TrimSpace(item.SHA256); expected != "" && !strings.EqualFold(actualSHA, expected) {
		if writeErr := m.writeErrorDescriptor(current, item, fmt.Sprintf("sha256 не совпадает: expected=%s actual=%s", expected, actualSHA)); writeErr != nil {
			return errors.Join(fmt.Errorf("sha256 не совпадает: expected=%s actual=%s", expected, actualSHA), fmt.Errorf("не удалось сохранить descriptor ошибки: %w", writeErr))
		}
		return fmt.Errorf("sha256 не совпадает: expected=%s actual=%s", expected, actualSHA)
	}

	now := time.Now().UTC()
	fileName := m.resolveBinaryName(item, actualSHA)
	targetPath := filepath.Join(m.binariesDir, fileName)
	if err := writeFileAtomically(targetPath, content, 0o755); err != nil {
		return fmt.Errorf("не удалось сохранить бинарник адаптера: %w", err)
	}

	descriptor := Descriptor{
		ManifestItem: ManifestItem{
			AdapterID:       strings.TrimSpace(item.AdapterID),
			AdapterType:     strings.TrimSpace(item.AdapterType),
			Version:         strings.TrimSpace(item.Version),
			TargetOS:        strings.TrimSpace(item.TargetOS),
			TargetArch:      strings.TrimSpace(item.TargetArch),
			ProtocolVersion: strings.TrimSpace(item.ProtocolVersion),
			DownloadURL:     strings.TrimSpace(item.DownloadURL),
			SHA256:          actualSHA,
			FileName:        fileName,
		},
		LocalPath:   targetPath,
		FileSize:    int64(len(content)),
		Status:      "ready",
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if current != nil {
		descriptor.InstalledAt = current.InstalledAt
		if descriptor.InstalledAt.IsZero() {
			descriptor.InstalledAt = now
		}
	}

	descriptorPath := filepath.Join(m.descriptorsDir, descriptorFileName(descriptor.AdapterID, descriptor.Version))
	if err := writeJSONAtomically(descriptorPath, descriptor); err != nil {
		return fmt.Errorf("не удалось сохранить descriptor адаптера: %w", err)
	}

	if current != nil {
		m.cleanupOldDescriptor(*current, descriptor)
	}
	return nil
}

func (m *Manager) cleanupOldDescriptor(current Descriptor, next Descriptor) {
	if current.LocalPath != "" && !strings.EqualFold(filepath.Clean(current.LocalPath), filepath.Clean(next.LocalPath)) {
		_ = os.Remove(current.LocalPath)
	}

	oldDescriptorPath := filepath.Join(m.descriptorsDir, descriptorFileName(current.AdapterID, current.Version))
	newDescriptorPath := filepath.Join(m.descriptorsDir, descriptorFileName(next.AdapterID, next.Version))
	if !strings.EqualFold(oldDescriptorPath, newDescriptorPath) {
		_ = os.Remove(oldDescriptorPath)
	}
}

func (m *Manager) findCurrentDescriptor(adapterID string) (*Descriptor, error) {
	entries, err := os.ReadDir(m.descriptorsDir)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать descriptors адаптеров: %w", err)
	}

	var current *Descriptor
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}

		descriptor, err := m.readDescriptor(filepath.Join(m.descriptorsDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if descriptor.AdapterID != adapterID {
			continue
		}
		if current == nil || current.UpdatedAt.Before(descriptor.UpdatedAt) {
			copy := descriptor
			current = &copy
		}
	}
	return current, nil
}

func (m *Manager) readDescriptor(path string) (Descriptor, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Descriptor{}, fmt.Errorf("не удалось прочитать descriptor %s: %w", path, err)
	}

	var descriptor Descriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return Descriptor{}, fmt.Errorf("не удалось распарсить descriptor %s: %w", path, err)
	}
	return descriptor, nil
}

func (m *Manager) resolveBinaryName(item ManifestItem, actualSHA string) string {
	if candidate := strings.TrimSpace(item.FileName); candidate != "" {
		return sanitizeFileName(candidate)
	}

	baseName := sanitizeName(item.AdapterID) + "-" + sanitizeName(item.Version)
	ext := extensionFromURL(item.DownloadURL)
	if ext != "" {
		return baseName + ext
	}
	if runtime.GOOS == "windows" {
		return baseName + ".exe"
	}
	return baseName + "-" + actualSHA[:12]
}

func descriptorToStatus(descriptor Descriptor) Status {
	return Status{
		AdapterID:       descriptor.AdapterID,
		AdapterType:     descriptor.AdapterType,
		Version:         descriptor.Version,
		TargetOS:        descriptor.TargetOS,
		TargetArch:      descriptor.TargetArch,
		ProtocolVersion: descriptor.ProtocolVersion,
		Status:          descriptor.Status,
		LocalPath:       descriptor.LocalPath,
		FileSize:        descriptor.FileSize,
		SHA256:          descriptor.SHA256,
		LastError:       descriptor.LastError,
		InstalledAt:     descriptor.InstalledAt,
		UpdatedAt:       descriptor.UpdatedAt,
	}
}

func sameRevision(current Descriptor, next ManifestItem) bool {
	return current.Version == strings.TrimSpace(next.Version) &&
		(strings.TrimSpace(next.SHA256) == "" || strings.EqualFold(current.SHA256, strings.TrimSpace(next.SHA256)))
}

func shouldSkipManifest(item ManifestItem) bool {
	return strings.TrimSpace(item.AdapterID) == "" ||
		strings.TrimSpace(item.Version) == "" ||
		strings.TrimSpace(item.DownloadURL) == ""
}

func (m *Manager) writeErrorDescriptor(current *Descriptor, item ManifestItem, lastError string) error {
	now := time.Now().UTC()
	descriptor := Descriptor{
		ManifestItem: ManifestItem{
			AdapterID:       strings.TrimSpace(item.AdapterID),
			AdapterType:     strings.TrimSpace(item.AdapterType),
			Version:         strings.TrimSpace(item.Version),
			TargetOS:        strings.TrimSpace(item.TargetOS),
			TargetArch:      strings.TrimSpace(item.TargetArch),
			ProtocolVersion: strings.TrimSpace(item.ProtocolVersion),
			DownloadURL:     strings.TrimSpace(item.DownloadURL),
			SHA256:          strings.TrimSpace(item.SHA256),
			FileName:        strings.TrimSpace(item.FileName),
		},
		Status:    "error",
		LastError: strings.TrimSpace(lastError),
		UpdatedAt: now,
	}
	if current != nil {
		descriptor.InstalledAt = current.InstalledAt
	}

	descriptorPath := filepath.Join(m.descriptorsDir, descriptorFileName(descriptor.AdapterID, descriptor.Version))
	return writeJSONAtomically(descriptorPath, descriptor)
}

func writeFileAtomically(path string, content []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}

	_ = os.Remove(path)
	return os.Rename(tmpPath, path)
}

func writeJSONAtomically(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomically(path, raw, 0o644)
}

func checksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func extensionFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil {
		if ext := strings.ToLower(filepath.Ext(parsed.Path)); ext != "" {
			return ext
		}
	}
	return strings.ToLower(filepath.Ext(raw))
}

func normalizePlatform(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func osCompatible(runtimeOS, manifestOS string) bool {
	return manifestOS == "" || manifestOS == runtimeOS
}

func archCompatible(runtimeOS, runtimeArch, manifestArch string) bool {
	if manifestArch == "" || manifestArch == runtimeArch {
		return true
	}
	if runtimeOS == "windows" && runtimeArch == "amd64" && manifestArch == "386" {
		return true
	}
	return false
}

func sanitizeFileName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	replacer := strings.NewReplacer("..", "", "/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	name = replacer.Replace(name)
	if name == "" || name == "." {
		return "adapter.bin"
	}
	return name
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", "_", "-", ".", "-")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "adapter"
	}
	return value
}

func descriptorFileName(adapterID, version string) string {
	return sanitizeName(adapterID) + "-" + sanitizeName(version) + ".json"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
