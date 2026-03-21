//go:build windows

package inventory

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func collectInstalledSoftware() ([]InstalledSoftware, error) {
	paths := []struct {
		root   registry.Key
		path   string
		access uint32
		source string
	}{
		{
			root:   registry.LOCAL_MACHINE,
			path:   `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
			access: registry.READ | registry.WOW64_64KEY,
			source: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall (64-bit)`,
		},
		{
			root:   registry.LOCAL_MACHINE,
			path:   `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
			access: registry.READ | registry.WOW64_32KEY,
			source: `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall (32-bit)`,
		},
	}

	type dedupeKey struct {
		name      string
		version   string
		publisher string
	}

	seen := make(map[dedupeKey]struct{})
	result := make([]InstalledSoftware, 0, 128)

	for _, entry := range paths {
		items, err := readInstalledSoftware(entry.root, entry.path, entry.access, entry.source)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			key := dedupeKey{
				name:      normalizeKey(item.Name),
				version:   normalizeKey(item.Version),
				publisher: normalizeKey(item.Publisher),
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, item)
		}
	}

	slices.SortFunc(result, func(a, b InstalledSoftware) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(strings.ToLower(a.Version), strings.ToLower(b.Version)),
			cmp.Compare(strings.ToLower(a.Publisher), strings.ToLower(b.Publisher)),
		)
	})
	return result, nil
}

func readInstalledSoftware(root registry.Key, uninstallPath string, access uint32, source string) ([]InstalledSoftware, error) {
	key, err := registry.OpenKey(root, uninstallPath, access)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil, nil
		}
		return nil, fmt.Errorf("не удалось открыть реестр установленного ПО %s: %w", source, err)
	}
	defer key.Close()

	subKeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать подразделы установленного ПО %s: %w", source, err)
	}

	result := make([]InstalledSoftware, 0, len(subKeys))
	for _, subKeyName := range subKeys {
		subKey, err := registry.OpenKey(key, subKeyName, access)
		if err != nil {
			continue
		}

		item := InstalledSoftware{
			Name:            readRegistryString(subKey, "DisplayName"),
			Version:         readRegistryString(subKey, "DisplayVersion"),
			Publisher:       readRegistryString(subKey, "Publisher"),
			InstallLocation: readRegistryString(subKey, "InstallLocation"),
			UninstallString: readRegistryString(subKey, "UninstallString"),
			Source:          source,
		}
		subKey.Close()

		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		result = append(result, item)
	}

	return result, nil
}

func readRegistryString(key registry.Key, valueName string) string {
	value, _, err := key.GetStringValue(valueName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func collectKnownComponents(installedSoftware []InstalledSoftware) ([]KnownComponent, error) {
	knownComponents := make([]KnownComponent, 0, 9)

	type signature struct {
		key            string
		name           string
		category       string
		programMatches []string
		fileMarkers    []string
		dirMarkers     []string
		registryChecks []registrySignature
	}

	signatures := []signature{
		{
			key:            "atol-drivers-10",
			name:           "Atol Drivers 10",
			category:       "fiscal-driver",
			programMatches: []string{"atol drivers 10", "драйверы атол 10", "драйверы ккт 10", "драйвер ккт 10"},
			fileMarkers: []string{
				`C:\Program Files\ATOL\Drivers10\KKT\bin\fptr10.dll`,
				`C:\Program Files (x86)\ATOL\Drivers10\KKT\bin\fptr10.dll`,
			},
			registryChecks: []registrySignature{
				{Path: `SOFTWARE\ATOL\Drivers\10.0\KKT`, ValueName: "INSTALL_DIR"},
				{Path: `SOFTWARE\WOW6432Node\ATOL\Drivers\10.0\KKT`, ValueName: "INSTALL_DIR"},
			},
		},
		{
			key:            "teamviewer",
			name:           "TeamViewer",
			category:       "remote-access",
			programMatches: []string{"teamviewer"},
			fileMarkers: []string{
				`C:\Program Files\TeamViewer\TeamViewer.exe`,
				`C:\Program Files (x86)\TeamViewer\TeamViewer.exe`,
			},
			registryChecks: []registrySignature{
				{Path: `SOFTWARE\TeamViewer`, ValueName: "ClientID"},
				{Path: `SOFTWARE\WOW6432Node\TeamViewer`, ValueName: "ClientID"},
			},
		},
		{
			key:            "anydesk",
			name:           "AnyDesk",
			category:       "remote-access",
			programMatches: []string{"anydesk"},
			fileMarkers: []string{
				`C:\Program Files\AnyDesk\AnyDesk.exe`,
				`C:\Program Files (x86)\AnyDesk\AnyDesk.exe`,
			},
		},
		{
			key:            "rustdesk",
			name:           "RustDesk",
			category:       "remote-access",
			programMatches: []string{"rustdesk"},
			fileMarkers: []string{
				`C:\Program Files\RustDesk\rustdesk.exe`,
			},
		},
		{
			key:            "litemanager",
			name:           "LiteManager",
			category:       "remote-access",
			programMatches: []string{"litemanager", "lite manager"},
			fileMarkers: []string{
				`C:\Program Files\LiteManager\ROMServer.exe`,
				`C:\Program Files (x86)\LiteManager\ROMServer.exe`,
				`C:\Program Files\LiteManager Pro - Server\ROMServer.exe`,
				`C:\Program Files (x86)\LiteManager Pro - Server\ROMServer.exe`,
			},
			registryChecks: []registrySignature{
				{Path: `SOFTWARE\LiteManager`, ValueName: "ID (read only)"},
				{Path: `SOFTWARE\WOW6432Node\LiteManager`, ValueName: "ID (read only)"},
			},
		},
		{
			key:            "iiko-front",
			name:           "iiko Front",
			category:       "pos",
			programMatches: []string{"iiko front", "iikofront"},
			fileMarkers: []string{
				`C:\Program Files\iiko\iikoFront.Net.exe`,
				`C:\Program Files (x86)\iiko\iikoFront.Net.exe`,
			},
		},
		{
			key:            "iiko-cashserver",
			name:           "iiko CashServer",
			category:       "pos",
			programMatches: []string{"iiko cashserver", "iiko cash server"},
			dirMarkers: []string{
				`AppData\Roaming\iiko\cashserver`,
				`AppData\Roaming\iiko\CashServer`,
			},
		},
		{
			key:            "syrve-front",
			name:           "Syrve Front",
			category:       "pos",
			programMatches: []string{"syrve front", "syrvefront"},
			fileMarkers: []string{
				`C:\Program Files\Syrve\Front.Net.exe`,
				`C:\Program Files (x86)\Syrve\Front.Net.exe`,
			},
		},
		{
			key:            "syrve-cashserver",
			name:           "Syrve CashServer",
			category:       "pos",
			programMatches: []string{"syrve cashserver", "syrve cash server"},
			dirMarkers: []string{
				`AppData\Roaming\syrve\cashserver`,
				`AppData\Roaming\syrve\CashServer`,
			},
		},
	}

	for _, signature := range signatures {
		component := KnownComponent{
			Key:      signature.key,
			Name:     signature.name,
			Category: signature.category,
		}

		if program, ok := matchInstalledSoftware(installedSoftware, signature.programMatches); ok {
			component.Detected = true
			component.Version = firstNonEmpty(program.Version, component.Version)
			component.Evidence = append(component.Evidence, ComponentEvidence{
				Type:   "installed_software",
				Source: program.Source,
				Value:  program.Name,
			})
		}

		for _, marker := range signature.fileMarkers {
			if pathExists(marker) {
				component.Detected = true
				component.Evidence = append(component.Evidence, ComponentEvidence{
					Type:   "file",
					Source: "filesystem",
					Value:  marker,
				})
			}
		}

		for _, check := range signature.registryChecks {
			if value, ok := readRegistryEvidence(check); ok {
				component.Detected = true
				component.Evidence = append(component.Evidence, ComponentEvidence{
					Type:   "registry",
					Source: `HKLM\` + check.Path,
					Value:  value,
				})
			}
		}

		for _, relativePath := range signature.dirMarkers {
			for _, path := range findUserRelativePaths(relativePath) {
				component.Detected = true
				component.Evidence = append(component.Evidence, ComponentEvidence{
					Type:   "directory",
					Source: "filesystem",
					Value:  path,
				})
			}
		}

		if component.Detected {
			slices.SortFunc(component.Evidence, func(a, b ComponentEvidence) int {
				return cmp.Or(
					cmp.Compare(a.Type, b.Type),
					cmp.Compare(a.Source, b.Source),
					cmp.Compare(a.Value, b.Value),
				)
			})
		}

		knownComponents = append(knownComponents, component)
	}

	return knownComponents, nil
}

type registrySignature struct {
	Path      string
	ValueName string
}

func matchInstalledSoftware(installedSoftware []InstalledSoftware, patterns []string) (InstalledSoftware, bool) {
	for _, item := range installedSoftware {
		name := normalizeKey(item.Name)
		for _, pattern := range patterns {
			if strings.Contains(name, normalizeKey(pattern)) {
				return item, true
			}
		}
	}
	return InstalledSoftware{}, false
}

func readRegistryEvidence(signature registrySignature) (string, bool) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, signature.Path, registry.READ)
	if err != nil {
		return "", false
	}
	defer key.Close()

	value := readRegistryString(key, signature.ValueName)
	if value == "" {
		return "", false
	}
	return value, true
}

func findUserRelativePaths(relativePath string) []string {
	usersRoot := filepath.Clean(`C:\Users`)
	entries, err := os.ReadDir(usersRoot)
	if err != nil {
		return nil
	}

	result := make([]string, 0, 4)
	for _, entry := range entries {
		if !entry.IsDir() || isSystemUserDir(entry.Name()) {
			continue
		}

		candidate := filepath.Join(usersRoot, entry.Name(), filepath.FromSlash(relativePath))
		if pathExists(candidate) {
			result = append(result, candidate)
		}
	}

	slices.Sort(result)
	return result
}

func isSystemUserDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "all users", "default", "default user", "public", "defaultapppool", "localservice", "networkservice":
		return true
	default:
		return false
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(filepath.Clean(path))
	return err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
