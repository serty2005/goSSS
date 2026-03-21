package hostinfo

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

func extractServerURLFromXML(raw []byte) string {
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

func normalizeCommandOutput(raw []byte) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if value := strings.TrimSpace(line); value != "" {
			return value
		}
	}
	return ""
}
