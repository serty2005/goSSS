package services

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	bitrixUserTagRe        = regexp.MustCompile(`(?is)\[USER=(\d+)\](.*?)\[/USER\]`)
	bitrixLeadingUserTagRe = regexp.MustCompile(`(?is)^\s*\[USER=\d+\].*?\[/USER\]\s*`)
	bitrixLeadingUserIDRe  = regexp.MustCompile(`(?is)^\s*\[USER=(\d+)\].*?\[/USER\]\s*`)
	bitrixAnyTagRe         = regexp.MustCompile(`(?is)\[[^\]]+\]`)
)

func normalizeBitrixCommentForEtalon(raw string, bitrixAuthorID *int64, integrationUserID int64) (string, *int64) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", nil
	}
	if integrationUserID <= 0 || bitrixAuthorID == nil || *bitrixAuthorID != integrationUserID {
		return text, nil
	}

	var extractedAuthorID *int64
	for {
		match := bitrixLeadingUserIDRe.FindStringSubmatch(text)
		if len(match) >= 2 && extractedAuthorID == nil {
			if id, err := strconv.ParseInt(strings.TrimSpace(match[1]), 10, 64); err == nil && id > 0 {
				value := id
				extractedAuthorID = &value
			}
		}
		next := strings.TrimSpace(bitrixLeadingUserTagRe.ReplaceAllString(text, ""))
		if next == text {
			break
		}
		text = next
	}
	return text, extractedAuthorID
}

func convertBitrixDescriptionForEtalon(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	matches := bitrixUserTagRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return html.EscapeString(text)
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		if match[0] > last {
			builder.WriteString(html.EscapeString(text[last:match[0]]))
		}

		userID := strings.TrimSpace(text[match[2]:match[3]])
		userName := strings.TrimSpace(bitrixAnyTagRe.ReplaceAllString(text[match[4]:match[5]], ""))
		if userName == "" {
			userName = fmt.Sprintf("Пользователь #%s", userID)
		}

		builder.WriteString(
			fmt.Sprintf(
				`<a href="#" class="etalon-user-link" data-etalon-user-id="%s" data-etalon-user-name="%s">%s</a>`,
				html.EscapeString(userID),
				html.EscapeString(userName),
				html.EscapeString(userName),
			),
		)
		last = match[1]
	}
	if last < len(text) {
		builder.WriteString(html.EscapeString(text[last:]))
	}

	return builder.String()
}

// TODO: Для умного редактора комментариев расширить обработку BBCode:
// распознавать вложения изображений и файлов в унифицированный HTML-формат Etalon.
