package registry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"etalon-agent/internal/iikosyrverms/domain"
)

type KnownPath struct {
	RelativePath   string
	Kind           domain.PathKind
	UseForActivity bool
	UseForConfig   bool
}

type ProductDefinition struct {
	SoftwareType     domain.SoftwareType
	RootRelativePath string
	KnownPaths       []KnownPath
}

type AppDataRoot struct {
	Path     string
	Priority int
}

type Discovery struct {
	EnvPath      string
	EnvAvailable bool
	Roots        []AppDataRoot
}

func DefaultProducts() []ProductDefinition {
	return []ProductDefinition{
		{
			SoftwareType:     domain.SoftwareTypeIiko,
			RootRelativePath: "iiko",
			KnownPaths: []KnownPath{
				{RelativePath: "iiko", Kind: domain.PathKindDirectory, UseForActivity: true},
				{RelativePath: filepath.Join("iiko", "cashserver"), Kind: domain.PathKindDirectory, UseForActivity: true},
				{RelativePath: filepath.Join("iiko", "CashServer"), Kind: domain.PathKindDirectory, UseForActivity: true},
				{RelativePath: filepath.Join("iiko", "cashserver", "config.xml"), Kind: domain.PathKindFile, UseForActivity: true, UseForConfig: true},
				{RelativePath: filepath.Join("iiko", "CashServer", "config.xml"), Kind: domain.PathKindFile, UseForActivity: true, UseForConfig: true},
			},
		},
		{
			SoftwareType:     domain.SoftwareTypeSyrve,
			RootRelativePath: "syrve",
			KnownPaths: []KnownPath{
				{RelativePath: "syrve", Kind: domain.PathKindDirectory, UseForActivity: true},
				{RelativePath: filepath.Join("syrve", "cashserver"), Kind: domain.PathKindDirectory, UseForActivity: true},
				{RelativePath: filepath.Join("syrve", "CashServer"), Kind: domain.PathKindDirectory, UseForActivity: true},
				{RelativePath: filepath.Join("syrve", "cashserver", "config.xml"), Kind: domain.PathKindFile, UseForActivity: true, UseForConfig: true},
				{RelativePath: filepath.Join("syrve", "CashServer", "config.xml"), Kind: domain.PathKindFile, UseForActivity: true, UseForConfig: true},
			},
		},
	}
}

func Discover() Discovery {
	envPath := strings.TrimSpace(os.Getenv("APPDATA"))
	discovery := Discovery{
		EnvPath:      envPath,
		EnvAvailable: isExistingUserAppData(envPath),
	}

	seen := make(map[string]struct{})
	add := func(path string, priority int) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}

		cleanPath := filepath.Clean(path)
		if !pathExists(cleanPath) {
			return
		}

		key := strings.ToLower(cleanPath)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		discovery.Roots = append(discovery.Roots, AppDataRoot{
			Path:     cleanPath,
			Priority: priority,
		})
	}

	if isUserAppDataPath(envPath) {
		add(envPath, 0)
	}
	if userProfile := strings.TrimSpace(os.Getenv("USERPROFILE")); userProfile != "" {
		userAppData := filepath.Join(userProfile, "AppData", "Roaming")
		if isUserAppDataPath(userAppData) {
			add(userAppData, 0)
		}
	}

	usersRoot := filepath.Clean(`C:\Users`)
	entries, err := os.ReadDir(usersRoot)
	if err != nil {
		return discovery
	}

	userNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || isSystemUserDir(entry.Name()) {
			continue
		}
		userNames = append(userNames, entry.Name())
	}
	slices.Sort(userNames)

	for _, userName := range userNames {
		add(filepath.Join(usersRoot, userName, "AppData", "Roaming"), 1)
	}

	return discovery
}

func isExistingUserAppData(path string) bool {
	return isUserAppDataPath(path) && pathExists(filepath.Clean(path))
}

func isUserAppDataPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}

	path = strings.ToLower(filepath.Clean(path))
	switch {
	case strings.Contains(path, `\systemprofile\`):
		return false
	case strings.Contains(path, `\serviceprofiles\localservice\`):
		return false
	case strings.Contains(path, `\serviceprofiles\networkservice\`):
		return false
	default:
		return true
	}
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
