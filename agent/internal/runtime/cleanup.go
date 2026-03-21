package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"etalon-agent/internal/config"
	"etalon-agent/internal/state"
)

var runtimeExecutablePath = os.Executable

type registryDataCleaner interface {
	DeleteAll() error
}

type dataCleaner struct {
	cfg            config.Config
	registry       registryDataCleaner
	executablePath func() (string, error)
	removeAll      func(string) error
	remove         func(string) error
}

func CleanupLocalData(cfg config.Config) error {
	return newDataCleaner(cfg).Cleanup()
}

func newDataCleaner(cfg config.Config) *dataCleaner {
	return &dataCleaner{
		cfg:            cfg,
		registry:       state.NewRegistryStore(cfg.RegistryPath),
		executablePath: runtimeExecutablePath,
		removeAll:      os.RemoveAll,
		remove:         os.Remove,
	}
}

func (c *dataCleaner) Cleanup() error {
	registryPath, err := validateRegistryPath(c.cfg.RegistryPath, c.cfg.AgentProcessName)
	if err != nil {
		return err
	}
	dataDir, err := validateDataDir(c.cfg.DataDir, c.cfg.AgentProcessName)
	if err != nil {
		return err
	}

	var errs []error

	if err := c.cleanupRegistry(registryPath); err != nil {
		errs = append(errs, err)
	}
	if err := c.cleanupDataDir(dataDir); err != nil {
		errs = append(errs, err)
	}
	if err := c.cleanupSelfUpdateArtifacts(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func (c *dataCleaner) cleanupRegistry(registryPath string) error {
	if err := c.registry.DeleteAll(); err != nil {
		return fmt.Errorf("не удалось очистить данные агента в HKLM\\%s: %w", registryPath, err)
	}
	return nil
}

func (c *dataCleaner) cleanupDataDir(dataDir string) error {
	if err := c.removeAll(dataDir); err != nil {
		return fmt.Errorf("не удалось удалить каталог данных агента %s: %w", dataDir, err)
	}
	return nil
}

func (c *dataCleaner) cleanupSelfUpdateArtifacts() error {
	exePath, err := c.executablePath()
	if err != nil {
		return fmt.Errorf("не удалось определить путь к текущему exe для очистки self-update артефактов: %w", err)
	}

	exePath = filepath.Clean(strings.TrimSpace(exePath))
	if exePath == "." || exePath == "" {
		return fmt.Errorf("получен пустой путь к текущему exe для очистки self-update артефактов")
	}

	var errs []error
	for _, path := range []string{
		filepath.Join(filepath.Dir(exePath), "agent-update.cmd"),
		exePath + ".bak",
	} {
		if err := c.remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("не удалось удалить файл %s: %w", path, err))
		}
	}

	return errors.Join(errs...)
}

func validateDataDir(dataDir, expectedLeaf string) (string, error) {
	path := filepath.Clean(strings.TrimSpace(dataDir))
	switch {
	case path == "" || path == ".":
		return "", fmt.Errorf("не задан каталог данных агента для очистки")
	case strings.TrimSpace(expectedLeaf) == "":
		return "", fmt.Errorf("не задано имя процесса агента для проверки каталога данных")
	case isRootPath(path):
		return "", fmt.Errorf("небезопасный каталог данных для очистки: %s", path)
	case !strings.EqualFold(filepath.Base(path), strings.TrimSpace(expectedLeaf)):
		return "", fmt.Errorf("небезопасный каталог данных для очистки: %s", path)
	default:
		return path, nil
	}
}

func validateRegistryPath(registryPath, expectedLeaf string) (string, error) {
	path := strings.Trim(strings.TrimSpace(registryPath), `\`)
	switch {
	case path == "":
		return "", fmt.Errorf("не задан registry path агента для очистки")
	case strings.TrimSpace(expectedLeaf) == "":
		return "", fmt.Errorf("не задано имя процесса агента для проверки registry path")
	case !strings.EqualFold(registryPathLeaf(path), strings.TrimSpace(expectedLeaf)):
		return "", fmt.Errorf("небезопасный registry path для очистки: HKLM\\%s", path)
	default:
		return path, nil
	}
}

func registryPathLeaf(path string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(path), func(r rune) bool {
		return r == '\\' || r == '/'
	})
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func isRootPath(path string) bool {
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) {
		return true
	}
	volume := filepath.VolumeName(cleaned)
	if volume == "" {
		return false
	}
	return strings.EqualFold(cleaned, volume+string(filepath.Separator))
}
