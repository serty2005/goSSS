//go:build windows

package hostinfo

import (
	"cmp"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const remoteIDCommandTimeout = 5 * time.Second

type registryLocation struct {
	Path   string
	Access uint32
}

type appDataCandidate struct {
	Path     string
	Priority int
}

type cashServerCandidate struct {
	Product   string
	Path      string
	AppData   string
	URL       string
	Priority  int
	UpdatedAt time.Time
}

func Collect(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	snapshot := Snapshot{
		TeamviewerID:  readRegistryRemoteID([]registryLocation{{Path: `SOFTWARE\TeamViewer`, Access: registry.READ | registry.WOW64_64KEY}, {Path: `SOFTWARE\TeamViewer`, Access: registry.READ | registry.WOW64_32KEY}}, "ClientID"),
		LitemanagerID: readRegistryRemoteID([]registryLocation{{Path: `SOFTWARE\LiteManager`, Access: registry.READ | registry.WOW64_64KEY}, {Path: `SOFTWARE\LiteManager`, Access: registry.READ | registry.WOW64_32KEY}}, "ID (read only)"),
		RustdeskID:    readExecutableRemoteID(ctx, []string{`C:\Program Files\RustDesk\rustdesk.exe`}, "rustdesk.exe"),
		AnydeskID:     readExecutableRemoteID(ctx, []string{`C:\Program Files\AnyDesk\AnyDesk.exe`, `C:\Program Files (x86)\AnyDesk\AnyDesk.exe`}, "anydesk.exe"),
	}

	if config, ok := findCashServerConfig(); ok {
		snapshot.RoamingAppDataPath = config.AppData
		snapshot.CashServerProduct = config.Product
		snapshot.CashServerConfig = config.Path
		snapshot.CashServerURL = config.URL
	}

	return snapshot, nil
}

func readRegistryRemoteID(locations []registryLocation, valueName string) string {
	for _, location := range locations {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, location.Path, location.Access)
		if err != nil {
			continue
		}

		value := readRegistryValue(key, valueName)
		key.Close()
		if value != "" {
			return value
		}
	}
	return ""
}

func readRegistryValue(key registry.Key, valueName string) string {
	if value, _, err := key.GetStringValue(valueName); err == nil {
		return strings.TrimSpace(value)
	}
	if value, _, err := key.GetIntegerValue(valueName); err == nil {
		return strconv.FormatUint(value, 10)
	}
	return ""
}

func readExecutableRemoteID(ctx context.Context, knownPaths []string, executableName string) string {
	executablePath := findExecutablePath(knownPaths, executableName)
	if executablePath == "" {
		return ""
	}

	cmdCtx, cancel := context.WithTimeout(ctx, remoteIDCommandTimeout)
	defer cancel()

	output, err := exec.CommandContext(cmdCtx, executablePath, "--get-id").Output()
	if err != nil {
		return ""
	}
	return normalizeCommandOutput(output)
}

func findExecutablePath(knownPaths []string, executableName string) string {
	for _, path := range knownPaths {
		if pathExists(path) {
			return filepath.Clean(path)
		}
	}

	path, err := exec.LookPath(executableName)
	if err != nil {
		return ""
	}
	return filepath.Clean(path)
}

func findCashServerConfig() (cashServerCandidate, bool) {
	candidates := collectCashServerCandidates()
	slices.SortFunc(candidates, func(a, b cashServerCandidate) int {
		return cmp.Or(
			cmp.Compare(a.Priority, b.Priority),
			compareTimesDesc(a.UpdatedAt, b.UpdatedAt),
			cmp.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path)),
		)
	})

	for _, candidate := range candidates {
		return candidate, true
	}
	return cashServerCandidate{}, false
}

func collectCashServerCandidates() []cashServerCandidate {
	type configPath struct {
		Product  string
		Relative string
	}

	relativePaths := []configPath{
		{Product: "iiko", Relative: filepath.Join("iiko", "cashserver", "config.xml")},
		{Product: "iiko", Relative: filepath.Join("iiko", "CashServer", "config.xml")},
		{Product: "syrve", Relative: filepath.Join("syrve", "cashserver", "config.xml")},
		{Product: "syrve", Relative: filepath.Join("syrve", "CashServer", "config.xml")},
	}

	seen := make(map[string]struct{})
	result := make([]cashServerCandidate, 0, 8)
	for _, appDataPath := range roamingAppDataCandidates() {
		for _, item := range relativePaths {
			path := filepath.Join(appDataPath.Path, item.Relative)
			info, err := os.Stat(path)
			if err != nil {
				continue
			}

			normalizedPath := strings.ToLower(filepath.Clean(path))
			if _, exists := seen[normalizedPath]; exists {
				continue
			}
			url := configURL(path)
			if url == "" {
				continue
			}

			seen[normalizedPath] = struct{}{}
			result = append(result, cashServerCandidate{
				Product:   item.Product,
				Path:      filepath.Clean(path),
				AppData:   filepath.Clean(appDataPath.Path),
				URL:       url,
				Priority:  appDataPath.Priority,
				UpdatedAt: info.ModTime(),
			})
		}
	}
	return result
}

func configURL(path string) string {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	return extractServerURLFromXML(raw)
}

func roamingAppDataCandidates() []appDataCandidate {
	seen := make(map[string]struct{})
	result := make([]appDataCandidate, 0, 8)
	add := func(path string, priority int) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}

		cleanPath := filepath.Clean(path)
		if !pathExists(cleanPath) {
			return
		}

		normalized := strings.ToLower(cleanPath)
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		result = append(result, appDataCandidate{Path: cleanPath, Priority: priority})
	}

	if appData := strings.TrimSpace(os.Getenv("APPDATA")); isUserAppDataPath(appData) {
		add(appData, 0)
	}
	if userProfile := strings.TrimSpace(os.Getenv("USERPROFILE")); userProfile != "" {
		appDataPath := filepath.Join(userProfile, "AppData", "Roaming")
		if isUserAppDataPath(appDataPath) {
			add(appDataPath, 0)
		}
	}

	usersRoot := filepath.Clean(`C:\Users`)
	entries, err := os.ReadDir(usersRoot)
	if err != nil {
		return result
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

	return result
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

func compareTimesDesc(a, b time.Time) int {
	switch {
	case a.After(b):
		return -1
	case a.Before(b):
		return 1
	default:
		return 0
	}
}
