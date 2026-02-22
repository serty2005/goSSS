package services

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	bitrixUserTagRe        = regexp.MustCompile(`(?is)\[USER=(\d+)\](.*?)\[/USER\]`)
	bitrixLeadingUserTagRe = regexp.MustCompile(`(?is)^\s*\[USER=\d+\].*?\[/USER\]\s*`)
	bitrixLeadingUserIDRe  = regexp.MustCompile(`(?is)^\s*\[USER=(\d+)\].*?\[/USER\]\s*`)
	bitrixAnyTagRe         = regexp.MustCompile(`(?is)\[/?[A-Z][A-Z0-9_]*(?:=[^\]]+)?\]`)
	bitrixURLTagRe         = regexp.MustCompile(`(?is)\[URL=([^\]]+)\](.*?)\[/URL\]`)
	bitrixURLSimpleTagRe   = regexp.MustCompile(`(?is)\[URL\](.*?)\[/URL\]`)
	bitrixIMGTagRe         = regexp.MustCompile(`(?is)\[IMG\](.*?)\[/IMG\]`)
	bitrixDiskTagRe        = regexp.MustCompile(`(?is)\[DISK FILE ID=n?\d+\]`)
	bitrixDiskTagIDRe      = regexp.MustCompile(`(?is)\[DISK FILE ID=n?(\d+)\]`)
	bitrixParagraphTagRe   = regexp.MustCompile(`(?is)\[(?:/)?P(?:=[^\]]+)?\]`)
	bitrixBreakTagRe       = regexp.MustCompile(`(?is)\[BR\]`)
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

const bitrixDiskPlaceholderPrefix = "__B24_DISK_"

func normalizeBitrixCommentForEtalon(raw string, bitrixAuthorID *int64, integrationUserID int64) (string, *int64) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", nil
	}
	if integrationUserID <= 0 || bitrixAuthorID == nil || *bitrixAuthorID != integrationUserID {
		return convertBitrixMarkupForEtalon(text), nil
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
	return convertBitrixMarkupForEtalon(text), extractedAuthorID
}

func convertBitrixDescriptionForEtalon(raw string) string {
	return convertBitrixMarkupForEtalon(raw)
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
	text = htmlParagraphOpenRe.ReplaceAllString(text, "")
	text = htmlParagraphCloseRe.ReplaceAllString(text, "\n")
	text = htmlDivOpenRe.ReplaceAllString(text, "")
	text = htmlDivCloseRe.ReplaceAllString(text, "\n")
	text = stripHTMLTags(text)
	text = html.UnescapeString(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = multiBreakRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func convertBitrixMarkupForEtalon(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = html.UnescapeString(text)
	text = html.EscapeString(text)
	text = bitrixParagraphTagRe.ReplaceAllString(text, "\n")
	text = bitrixBreakTagRe.ReplaceAllString(text, "\n")

	text = bitrixUserTagRe.ReplaceAllStringFunc(text, func(token string) string {
		match := bitrixUserTagRe.FindStringSubmatch(token)
		if len(match) < 3 {
			return token
		}
		userID := strings.TrimSpace(html.UnescapeString(match[1]))
		userName := strings.TrimSpace(bitrixAnyTagRe.ReplaceAllString(html.UnescapeString(match[2]), ""))
		if userName == "" {
			userName = fmt.Sprintf("Пользователь #%s", userID)
		}
		return fmt.Sprintf(
			`<a href="#" class="etalon-user-link" data-etalon-user-id="%s" data-etalon-user-name="%s">%s</a>`,
			html.EscapeString(userID),
			html.EscapeString(userName),
			html.EscapeString(userName),
		)
	})

	text = bitrixURLTagRe.ReplaceAllStringFunc(text, func(token string) string {
		match := bitrixURLTagRe.FindStringSubmatch(token)
		if len(match) < 3 {
			return token
		}
		href := sanitizeBitrixURLForEtalon(html.UnescapeString(match[1]))
		label := strings.TrimSpace(bitrixAnyTagRe.ReplaceAllString(html.UnescapeString(match[2]), ""))
		if label == "" {
			label = href
		}
		if href == "" {
			return html.EscapeString(label)
		}
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noreferrer">%s</a>`, html.EscapeString(href), html.EscapeString(label))
	})

	text = bitrixURLSimpleTagRe.ReplaceAllStringFunc(text, func(token string) string {
		match := bitrixURLSimpleTagRe.FindStringSubmatch(token)
		if len(match) < 2 {
			return token
		}
		href := sanitizeBitrixURLForEtalon(html.UnescapeString(match[1]))
		if href == "" {
			return html.EscapeString(strings.TrimSpace(html.UnescapeString(match[1])))
		}
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noreferrer">%s</a>`, html.EscapeString(href), html.EscapeString(href))
	})

	text = bitrixIMGTagRe.ReplaceAllStringFunc(text, func(token string) string {
		match := bitrixIMGTagRe.FindStringSubmatch(token)
		if len(match) < 2 {
			return token
		}
		srcRaw := strings.TrimSpace(html.UnescapeString(match[1]))
		if srcRaw == "" {
			return ""
		}
		if diskMatch := bitrixDiskTagIDRe.FindStringSubmatch(srcRaw); len(diskMatch) >= 2 {
			return bitrixDiskPlaceholder(diskMatch[1])
		}
		if bitrixDiskTagRe.MatchString(srcRaw) {
			return ""
		}
		src := sanitizeBitrixURLForEtalon(srcRaw)
		if src == "" {
			return ""
		}
		return fmt.Sprintf(`<img src="%s" alt="Изображение" />`, html.EscapeString(src))
	})

	text = bitrixDiskTagIDRe.ReplaceAllStringFunc(text, func(token string) string {
		match := bitrixDiskTagIDRe.FindStringSubmatch(token)
		if len(match) < 2 {
			return ""
		}
		return bitrixDiskPlaceholder(match[1])
	})
	text = bitrixDiskTagRe.ReplaceAllString(text, "")
	text = bitrixAnyTagRe.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\n", "<br />")
	text = strings.ReplaceAll(text, "&lt;br /&gt;", "<br />")
	return strings.TrimSpace(text)
}

func bitrixDiskPlaceholder(id string) string {
	value := strings.TrimSpace(id)
	if value == "" {
		return ""
	}
	return bitrixDiskPlaceholderPrefix + value + "__"
}

func sanitizeBitrixURLForEtalon(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\\/", "/")
	if strings.HasPrefix(value, "/") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "http", "https", "mailto", "tel":
		return value
	default:
		return ""
	}
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
