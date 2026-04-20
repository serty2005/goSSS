//go:build windows

package frontinstall

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"etalon-agent/internal/iikosyrverms/domain"

	"golang.org/x/sys/windows/registry"
)

type windowsResolver struct {
	getenv func(string) string
}

type installCandidate struct {
	softwareType domain.SoftwareType
	rootPath     string
	executable   string
	source       string
}

type registryLocation struct {
	path   string
	access uint32
}

func New() Resolver {
	return windowsResolver{
		getenv: os.Getenv,
	}
}

func (r windowsResolver) Resolve(ctx context.Context, preferred []domain.SoftwareType) (domain.FrontInstallation, error) {
	if err := ctx.Err(); err != nil {
		return domain.FrontInstallation{}, err
	}

	candidates := r.collectCandidates()
	if len(candidates) == 0 {
		return domain.FrontInstallation{}, ErrNotFound
	}

	priority := softwarePriority(preferred)
	slices.SortFunc(candidates, func(a, b installCandidate) int {
		return cmp.Or(
			cmp.Compare(priority[a.softwareType], priority[b.softwareType]),
			cmp.Compare(strings.ToLower(a.source), strings.ToLower(b.source)),
			cmp.Compare(strings.ToLower(a.executable), strings.ToLower(b.executable)),
		)
	})

	selected := candidates[0]
	installation := domain.FrontInstallation{
		SoftwareType:   selected.softwareType,
		RootPath:       selected.rootPath,
		ExecutablePath: selected.executable,
		WorkingDir:     filepath.Dir(selected.executable),
		Source:         selected.source,
	}
	installation.PluginsRoot = resolvePluginsRoot(selected.rootPath, selected.executable)
	return installation, nil
}

func (r windowsResolver) collectCandidates() []installCandidate {
	seen := make(map[string]struct{})
	result := make([]installCandidate, 0, 8)

	addExecutable := func(softwareType domain.SoftwareType, rootPath, executable, source string) {
		executable = filepath.Clean(strings.TrimSpace(executable))
		if executable == "" {
			return
		}
		info, err := os.Stat(executable)
		if err != nil || info.IsDir() {
			return
		}

		key := strings.ToLower(executable)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}

		rootPath = strings.TrimSpace(rootPath)
		if rootPath == "" {
			rootPath = filepath.Dir(executable)
		}
		result = append(result, installCandidate{
			softwareType: softwareType,
			rootPath:     filepath.Clean(rootPath),
			executable:   executable,
			source:       source,
		})
	}

	for _, location := range registryInstallLocations() {
		softwareType := detectSoftwareType(location)
		if softwareType == domain.SoftwareTypeUnknown {
			continue
		}
		rootPath := location
		if filepath.Ext(rootPath) != "" {
			rootPath = filepath.Dir(rootPath)
		}
		for _, executable := range executableCandidates(softwareType, rootPath) {
			addExecutable(softwareType, rootPath, executable, "registry")
		}
	}

	for _, softwareType := range []domain.SoftwareType{domain.SoftwareTypeIiko, domain.SoftwareTypeSyrve} {
		for _, rootPath := range r.knownRoots(softwareType) {
			for _, executable := range executableCandidates(softwareType, rootPath) {
				addExecutable(softwareType, rootPath, executable, "known-path")
			}
		}
	}

	return result
}

func (r windowsResolver) knownRoots(softwareType domain.SoftwareType) []string {
	result := make([]string, 0, 4)
	seen := make(map[string]struct{})
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		cleanPath := filepath.Clean(path)
		key := strings.ToLower(cleanPath)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, cleanPath)
	}

	for _, envName := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		base := strings.TrimSpace(r.getenv(envName))
		if base == "" {
			continue
		}
		switch softwareType {
		case domain.SoftwareTypeIiko:
			add(filepath.Join(base, "iiko"))
		case domain.SoftwareTypeSyrve:
			add(filepath.Join(base, "Syrve"))
			add(filepath.Join(base, "syrve"))
		}
	}

	return result
}

func executableCandidates(softwareType domain.SoftwareType, rootPath string) []string {
	rootPath = filepath.Clean(strings.TrimSpace(rootPath))
	if rootPath == "" {
		return nil
	}

	switch softwareType {
	case domain.SoftwareTypeIiko:
		return []string{
			filepath.Join(rootPath, "iikoFront.Net.exe"),
			filepath.Join(rootPath, "iikoRMS", "Front.Net", "iikoFront.Net.exe"),
			filepath.Join(rootPath, "Front.Net", "iikoFront.Net.exe"),
		}
	case domain.SoftwareTypeSyrve:
		return []string{
			filepath.Join(rootPath, "Front.Net", "Front.Net.exe"),
			filepath.Join(rootPath, "Front.Net.exe"),
			filepath.Join(rootPath, "SyrveFront.Net.exe"),
		}
	default:
		return nil
	}
}

func resolvePluginsRoot(rootPath, executable string) string {
	candidates := []string{
		filepath.Join(filepath.Dir(executable), "Plugins"),
		filepath.Join(rootPath, "Plugins"),
		filepath.Join(filepath.Dir(filepath.Dir(executable)), "Plugins"),
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return filepath.Clean(candidate)
		}
	}
	return ""
}

func registryInstallLocations() []string {
	locations := []registryLocation{
		{path: `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, access: registry.READ | registry.WOW64_64KEY},
		{path: `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, access: registry.READ | registry.WOW64_32KEY},
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, 8)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		cleanPath := filepath.Clean(path)
		key := strings.ToLower(cleanPath)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, cleanPath)
	}

	for _, location := range locations {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, location.path, location.access)
		if err != nil {
			continue
		}

		names, err := key.ReadSubKeyNames(-1)
		if err != nil {
			key.Close()
			continue
		}

		for _, name := range names {
			subKey, err := registry.OpenKey(key, name, registry.READ)
			if err != nil {
				continue
			}

			displayName, _, _ := subKey.GetStringValue("DisplayName")
			installLocation, _, _ := subKey.GetStringValue("InstallLocation")
			displayIcon, _, _ := subKey.GetStringValue("DisplayIcon")
			if detectSoftwareType(displayName+" "+installLocation+" "+displayIcon) != domain.SoftwareTypeUnknown {
				add(installLocation)
				if displayIcon != "" {
					iconPath, _, _ := strings.Cut(displayIcon, ",")
					add(iconPath)
				}
			}
			subKey.Close()
		}
		key.Close()
	}

	return result
}

func detectSoftwareType(value string) domain.SoftwareType {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(normalized, "syrve"):
		return domain.SoftwareTypeSyrve
	case strings.Contains(normalized, "iiko"):
		return domain.SoftwareTypeIiko
	default:
		return domain.SoftwareTypeUnknown
	}
}

func softwarePriority(preferred []domain.SoftwareType) map[domain.SoftwareType]int {
	priority := map[domain.SoftwareType]int{
		domain.SoftwareTypeIiko:  50,
		domain.SoftwareTypeSyrve: 51,
	}
	for index, item := range preferred {
		priority[item] = index
	}
	return priority
}
