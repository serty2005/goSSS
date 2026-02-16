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
	etalonMentionLinkRe    = regexp.MustCompile(`(?is)<a[^>]*class=["'][^"']*etalon-user-link[^"']*["'][^>]*>(.*?)</a>`)
	htmlDataUserIDRe       = regexp.MustCompile(`(?is)data-etalon-user-id=["']([^"']+)["']`)
	htmlDataUserNameRe     = regexp.MustCompile(`(?is)data-etalon-user-name=["']([^"']+)["']`)
	htmlImageRe            = regexp.MustCompile(`(?is)<img[^>]*src=["']([^"']+)["'][^>]*>`)
	htmlLinkRe             = regexp.MustCompile(`(?is)<a[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	htmlStrongRe           = regexp.MustCompile(`(?is)<(strong|b)>(.*?)</(strong|b)>`)
	htmlEmRe               = regexp.MustCompile(`(?is)<(em|i)>(.*?)</(em|i)>`)
	htmlStrikeRe           = regexp.MustCompile(`(?is)<(s|strike|del)>(.*?)</(s|strike|del)>`)
	htmlQuoteRe            = regexp.MustCompile(`(?is)<blockquote[^>]*>(.*?)</blockquote>`)
	htmlBrRe               = regexp.MustCompile(`(?is)<br\s*/?>`)
	htmlParagraphOpenRe    = regexp.MustCompile(`(?is)<p[^>]*>`)
	htmlParagraphCloseRe   = regexp.MustCompile(`(?is)</p>`)
	htmlDivOpenRe          = regexp.MustCompile(`(?is)<div[^>]*>`)
	htmlDivCloseRe         = regexp.MustCompile(`(?is)</div>`)
	htmlAnyTagRe           = regexp.MustCompile(`(?is)<[^>]+>`)
	multiBreakRe           = regexp.MustCompile(`\n{3,}`)
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

type mentionResolver func(etalonUserID uint) (*int64, bool)

func convertEtalonHTMLToBitrix(raw string, resolve mentionResolver) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	text = etalonMentionLinkRe.ReplaceAllStringFunc(text, func(link string) string {
		name := strings.TrimSpace(strings.TrimPrefix(extractAttribute(link, htmlDataUserNameRe), "@"))
		inner := strings.TrimSpace(strings.TrimPrefix(stripHTMLTags(extractInnerHTML(link)), "@"))
		if name == "" {
			name = inner
		}
		if name == "" {
			name = "Пользователь"
		}

		if userIDRaw := extractAttribute(link, htmlDataUserIDRe); userIDRaw != "" && resolve != nil {
			if parsedID, err := strconv.ParseUint(strings.TrimSpace(userIDRaw), 10, 64); err == nil && parsedID > 0 {
				if bitrixUserID, ok := resolve(uint(parsedID)); ok && bitrixUserID != nil && *bitrixUserID > 0 {
					return fmt.Sprintf("[USER=%d]%s[/USER]", *bitrixUserID, sanitizeBitrixMentionName(name))
				}
			}
		}
		return "[" + sanitizeBitrixMentionName(name) + "]"
	})

	text = htmlImageRe.ReplaceAllString(text, "[IMG]$1[/IMG]")
	text = htmlLinkRe.ReplaceAllStringFunc(text, func(item string) string {
		matches := htmlLinkRe.FindStringSubmatch(item)
		if len(matches) < 3 {
			return item
		}
		href := strings.TrimSpace(html.UnescapeString(matches[1]))
		label := strings.TrimSpace(stripHTMLTags(matches[2]))
		if href == "" {
			return label
		}
		if label == "" {
			return href
		}
		return fmt.Sprintf("[URL=%s]%s[/URL]", href, label)
	})

	text = htmlStrongRe.ReplaceAllString(text, "[B]$2[/B]")
	text = htmlEmRe.ReplaceAllString(text, "[I]$2[/I]")
	text = htmlStrikeRe.ReplaceAllString(text, "[S]$2[/S]")
	text = htmlQuoteRe.ReplaceAllString(text, "[QUOTE]$1[/QUOTE]")

	text = htmlBrRe.ReplaceAllString(text, "\n")
	text = htmlParagraphOpenRe.ReplaceAllString(text, "\n")
	text = htmlParagraphCloseRe.ReplaceAllString(text, "\n")
	text = htmlDivOpenRe.ReplaceAllString(text, "\n")
	text = htmlDivCloseRe.ReplaceAllString(text, "\n")
	text = stripHTMLTags(text)
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = multiBreakRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func extractAttribute(value string, pattern *regexp.Regexp) string {
	match := pattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return html.UnescapeString(strings.TrimSpace(match[1]))
}

func stripHTMLTags(value string) string {
	return strings.TrimSpace(htmlAnyTagRe.ReplaceAllString(value, ""))
}

func extractInnerHTML(tag string) string {
	start := strings.Index(tag, ">")
	end := strings.LastIndex(tag, "<")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return html.UnescapeString(tag[start+1 : end])
}

// TODO: Для умного редактора комментариев расширить обработку BBCode:
// распознавать вложения изображений и файлов в унифицированный HTML-формат Etalon.
