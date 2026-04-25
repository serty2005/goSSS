package parser

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"etalon-agent/internal/iikosyrverms/domain"
)

type configFile struct {
	Path      string
	UpdatedAt time.Time
}

type xmlNode struct {
	Name       string
	Attributes map[string]string
	TextParts  []string
	Children   []*xmlNode
}

func ReadConfigFile(path string) (domain.ConfigSnapshot, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return domain.ConfigSnapshot{}, fmt.Errorf("не удалось прочитать config.xml %q: %w", path, err)
	}

	root, err := parseConfigTree(raw)
	if err != nil {
		return domain.ConfigSnapshot{}, fmt.Errorf("не удалось разобрать config.xml %q: %w", path, err)
	}

	snapshot := domain.ConfigSnapshot{
		SourceFile:  filepath.Clean(path),
		RootElement: root.Name,
	}
	tree, settings, serverURL, hasRepeatedNodes := buildConfigSnapshot(root)
	snapshot.Tree = &tree
	snapshot.Settings = settings
	snapshot.ServerURL = serverURL
	snapshot.HasRepeatedNodes = hasRepeatedNodes
	return snapshot, nil
}

func ReadConfigFiles(paths []string) (domain.ConfigSnapshot, string) {
	files := collectConfigFiles(paths)
	if len(files) == 0 {
		return domain.ConfigSnapshot{}, "Известные каталоги найдены, но config.xml отсутствует"
	}

	for _, file := range files {
		snapshot, err := ReadConfigFile(file.Path)
		if err != nil {
			continue
		}
		if snapshot.ServerURL != "" {
			return snapshot, "RMS URL успешно извлечён из config.xml"
		}
		return snapshot, "config.xml найден, но поле serverUrl отсутствует или пустое"
	}

	return domain.ConfigSnapshot{}, "Известные config.xml найдены, но их не удалось прочитать"
}

func collectConfigFiles(paths []string) []configFile {
	files := make([]configFile, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(filepath.Clean(path))
		if err != nil || info.IsDir() {
			continue
		}
		files = append(files, configFile{
			Path:      filepath.Clean(path),
			UpdatedAt: info.ModTime(),
		})
	}

	slices.SortFunc(files, func(a, b configFile) int {
		switch {
		case a.UpdatedAt.After(b.UpdatedAt):
			return -1
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		default:
			return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
		}
	})
	return files
}

func parseConfigTree(raw []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})))

	var root *xmlNode
	stack := make([]*xmlNode, 0, 8)

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		switch value := token.(type) {
		case xml.StartElement:
			node := &xmlNode{
				Name:       strings.TrimSpace(value.Name.Local),
				Attributes: make(map[string]string, len(value.Attr)),
			}
			for _, attr := range value.Attr {
				node.Attributes[strings.TrimSpace(attr.Name.Local)] = strings.TrimSpace(attr.Value)
			}
			if len(node.Attributes) == 0 {
				node.Attributes = nil
			}

			if len(stack) == 0 {
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, node)
			}
			stack = append(stack, node)
		case xml.CharData:
			if len(stack) == 0 {
				continue
			}
			stack[len(stack)-1].TextParts = append(stack[len(stack)-1].TextParts, string(value))
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			stack = stack[:len(stack)-1]
		}
	}

	if root == nil {
		return nil, fmt.Errorf("корневой элемент XML не найден")
	}
	return root, nil
}

func buildConfigSnapshot(root *xmlNode) (domain.ConfigNode, []domain.ConfigSetting, string, bool) {
	settings := make([]domain.ConfigSetting, 0, 16)
	hasRepeatedNodes := false
	serverURL := ""

	var walk func(node *xmlNode, parentPath string, index int, repeated bool) domain.ConfigNode
	walk = func(node *xmlNode, parentPath string, index int, repeated bool) domain.ConfigNode {
		path := nodePath(parentPath, node.Name, index, repeated)
		value := strings.TrimSpace(strings.Join(node.TextParts, ""))
		current := domain.ConfigNode{
			Name:       node.Name,
			Path:       path,
			Value:      value,
			Attributes: cloneAttributes(node.Attributes),
			ParentPath: parentPath,
			Index:      index,
			Repeated:   repeated,
		}
		settings = append(settings, domain.ConfigSetting{
			Path:       current.Path,
			Name:       current.Name,
			Value:      current.Value,
			Attributes: cloneAttributes(current.Attributes),
			ParentPath: current.ParentPath,
			Index:      current.Index,
			Repeated:   current.Repeated,
		})

		if repeated {
			hasRepeatedNodes = true
		}
		if serverURL == "" {
			serverURL = serverURLFromNode(current)
		}

		childTotals := make(map[string]int, len(node.Children))
		for _, child := range node.Children {
			childTotals[child.Name]++
		}

		childIndexes := make(map[string]int, len(childTotals))
		current.Children = make([]domain.ConfigNode, 0, len(node.Children))
		for _, child := range node.Children {
			childIndex := childIndexes[child.Name]
			childIndexes[child.Name]++
			childRepeated := childTotals[child.Name] > 1
			current.Children = append(current.Children, walk(child, current.Path, childIndex, childRepeated))
		}

		return current
	}

	tree := walk(root, "", 0, false)
	return tree, settings, serverURL, hasRepeatedNodes
}

func nodePath(parentPath, name string, index int, repeated bool) string {
	name = strings.TrimSpace(name)
	if parentPath == "" {
		return "/" + name
	}
	if repeated {
		return parentPath + "/" + name + "[" + strconv.Itoa(index) + "]"
	}
	return parentPath + "/" + name
}

func serverURLFromNode(node domain.ConfigNode) string {
	for name, value := range node.Attributes {
		if strings.EqualFold(strings.TrimSpace(name), "serverUrl") && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if strings.EqualFold(strings.TrimSpace(node.Name), "serverUrl") && strings.TrimSpace(node.Value) != "" {
		return strings.TrimSpace(node.Value)
	}
	return ""
}

func cloneAttributes(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}

	result := make(map[string]string, len(value))
	for name, item := range value {
		result[name] = item
	}
	return result
}
