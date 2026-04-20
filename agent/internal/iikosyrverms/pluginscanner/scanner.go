package pluginscanner

import (
	"cmp"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"etalon-agent/internal/iikosyrverms/domain"
)

type Scanner struct {
	readVersion func(string) (string, error)
}

type pluginManifest struct {
	FileName   string
	APIVersion string
}

func New() *Scanner {
	return &Scanner{
		readVersion: readDLLVersion,
	}
}

func (s *Scanner) Scan(root string) ([]domain.PluginInfo, []string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" {
		return nil, nil, fmt.Errorf("не задан каталог Plugins")
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось получить доступ к каталогу Plugins %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("путь Plugins %q не является каталогом", root)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось прочитать каталог Plugins %q: %w", root, err)
	}

	plugins := make([]domain.PluginInfo, 0, len(entries))
	warnings := make([]string, 0)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		plugin, itemWarnings, ok := s.scanPluginDir(filepath.Join(root, entry.Name()))
		warnings = append(warnings, itemWarnings...)
		if ok {
			plugins = append(plugins, plugin)
		}
	}

	slices.SortFunc(plugins, func(a, b domain.PluginInfo) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(strings.ToLower(a.Version), strings.ToLower(b.Version)),
			cmp.Compare(strings.ToLower(a.Directory), strings.ToLower(b.Directory)),
		)
	})
	return plugins, warnings, nil
}

func (s *Scanner) scanPluginDir(dir string) (domain.PluginInfo, []string, bool) {
	dir = filepath.Clean(dir)
	manifest, hasManifest, manifestErr := readManifest(dir)
	warnings := make([]string, 0, 2)
	if manifestErr != nil {
		warnings = append(warnings, fmt.Sprintf("Не удалось прочитать manifest.xml для %q: %v", filepath.Base(dir), manifestErr))
	}

	dllPath, err := detectPrimaryDLL(dir, manifest)
	if err != nil && !hasManifest {
		return domain.PluginInfo{}, warnings, false
	}
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Не удалось определить основную DLL для %q: %v", filepath.Base(dir), err))
	}

	nameSource := filepath.Base(dir)
	if manifest.FileName != "" {
		nameSource = manifest.FileName
	}
	if dllPath != "" {
		nameSource = filepath.Base(dllPath)
	}
	name := normalizePluginName(nameSource, manifest.APIVersion)
	if name == "" {
		name = filepath.Base(dir)
	}

	version := ""
	if dllPath != "" {
		version, err = s.readVersion(dllPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Не удалось определить версию DLL %q: %v", dllPath, err))
		}
	}
	if version == "" {
		version = versionFromDirectory(filepath.Base(dir))
	}

	return domain.PluginInfo{
		Name:         name,
		APIVersion:   manifest.APIVersion,
		Version:      version,
		Directory:    filepath.Base(dir),
		ManifestFile: manifestPath(dir, hasManifest),
		DLLFile:      dllPath,
	}, warnings, true
}

func readManifest(dir string) (pluginManifest, bool, error) {
	path := filepath.Join(filepath.Clean(dir), "manifest.xml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pluginManifest{}, false, nil
		}
		return pluginManifest{}, false, err
	}

	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	manifest := pluginManifest{}
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return pluginManifest{}, true, err
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(start.Name.Local)) {
		case "filename":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return pluginManifest{}, true, err
			}
			manifest.FileName = strings.TrimSpace(value)
		case "apiversion":
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return pluginManifest{}, true, err
			}
			manifest.APIVersion = strings.TrimSpace(value)
		}
	}

	return manifest, true, nil
}

func detectPrimaryDLL(dir string, manifest pluginManifest) (string, error) {
	if manifest.FileName != "" {
		path := filepath.Join(filepath.Clean(dir), manifest.FileName)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return filepath.Clean(path), nil
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}

	dlls := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".dll") {
			continue
		}
		dlls = append(dlls, filepath.Join(dir, entry.Name()))
	}
	if len(dlls) == 0 {
		return "", fmt.Errorf("DLL-файлы не найдены")
	}
	if len(dlls) == 1 {
		return filepath.Clean(dlls[0]), nil
	}

	dirName := filepath.Base(dir)
	manifestName := strings.TrimSuffix(filepath.Base(manifest.FileName), filepath.Ext(manifest.FileName))
	slices.SortFunc(dlls, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(primaryDLLScore(filepath.Base(a), dirName, manifestName), primaryDLLScore(filepath.Base(b), dirName, manifestName))*-1,
			cmp.Compare(strings.ToLower(a), strings.ToLower(b)),
		)
	})
	return filepath.Clean(dlls[0]), nil
}

func primaryDLLScore(fileName, dirName, manifestName string) int {
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	score := 0
	if strings.EqualFold(baseName, manifestName) {
		score += 100
	}
	if strings.EqualFold(baseName, dirName) {
		score += 80
	}

	baseTokens := normalizePluginTokens(baseName)
	dirTokens := normalizePluginTokens(dirName)
	manifestTokens := normalizePluginTokens(manifestName)
	score += tokenIntersectionScore(baseTokens, dirTokens) * 10
	score += tokenIntersectionScore(baseTokens, manifestTokens) * 10
	return score
}

func tokenIntersectionScore(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}

	rightSet := make(map[string]struct{}, len(right))
	for _, item := range right {
		rightSet[item] = struct{}{}
	}

	score := 0
	for _, item := range left {
		if _, exists := rightSet[item]; exists {
			score++
		}
	}
	return score
}

func normalizePluginName(raw, apiVersion string) string {
	name := strings.TrimSpace(strings.TrimSuffix(raw, filepath.Ext(raw)))
	for _, prefix := range []string{"Resto.Front.Api.", "Plugin.Front.Api.", "Plugin.Front.", "Resto.Front."} {
		name = strings.TrimPrefix(name, prefix)
	}
	if apiVersion != "" {
		name = strings.TrimSuffix(name, "."+apiVersion)
		name = strings.TrimSuffix(name, "-"+apiVersion)
	}
	name = strings.Trim(name, ".-_ ")
	replacer := strings.NewReplacer(".", " ", "_", " ", "-", " ")
	return strings.Join(strings.Fields(replacer.Replace(name)), " ")
}

func normalizePluginTokens(raw string) []string {
	replacer := strings.NewReplacer(".", " ", "_", " ", "-", " ")
	normalized := replacer.Replace(strings.TrimSpace(raw))
	tokens := make([]string, 0)
	for _, token := range strings.Fields(normalized) {
		value := normalizePluginToken(token)
		if value == "" {
			continue
		}
		tokens = append(tokens, value)
	}
	return tokens
}

func normalizePluginToken(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "", "resto", "front", "api", "plugin", "plugins", "dll":
		return ""
	default:
		return value
	}
}

func manifestPath(dir string, exists bool) string {
	if !exists {
		return ""
	}
	return filepath.Join(filepath.Clean(dir), "manifest.xml")
}

func versionFromDirectory(dir string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(dir), func(r rune) bool {
		switch {
		case r >= '0' && r <= '9':
			return false
		case r == '.':
			return false
		default:
			return true
		}
	})
	for _, part := range parts {
		if strings.Count(part, ".") >= 1 {
			return part
		}
	}
	return ""
}
