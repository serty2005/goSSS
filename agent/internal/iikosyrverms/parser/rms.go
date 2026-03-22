package parser

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func ExtractRMSURL(raw []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				return ""
			}
			return ""
		}

		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}

		for _, attr := range start.Attr {
			if strings.EqualFold(strings.TrimSpace(attr.Name.Local), "serverUrl") {
				if value := strings.TrimSpace(attr.Value); value != "" {
					return value
				}
			}
		}

		if !strings.EqualFold(strings.TrimSpace(start.Name.Local), "serverUrl") {
			continue
		}

		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
}

func ParseConfigFiles(paths []string) (string, string, string) {
	type configFile struct {
		Path      string
		UpdatedAt time.Time
	}

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

	if len(files) == 0 {
		return "", "", "Известные каталоги найдены, но config.xml отсутствует"
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

	for _, file := range files {
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			continue
		}
		if url := ExtractRMSURL(raw); url != "" {
			return url, file.Path, "RMS URL успешно извлечён из config.xml"
		}
	}

	return "", "", "config.xml найден, но поле serverUrl отсутствует или пустое"
}
