package services

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

var (
	pyrusOutgoingMentionLinkRe   = regexp.MustCompile(`(?is)<a\b[^>]*class=["'][^"']*etalon-user-link[^"']*["'][^>]*>.*?</a>`)
	pyrusOutgoingLinkRe          = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	pyrusOutgoingImageRe         = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	pyrusOutgoingPreCodeRe       = regexp.MustCompile(`(?is)<pre\b[^>]*>\s*<code\b[^>]*>(.*?)</code>\s*</pre>`)
	pyrusOutgoingPreRe           = regexp.MustCompile(`(?is)<pre\b[^>]*>(.*?)</pre>`)
	pyrusOutgoingHeadingBlockRe  = regexp.MustCompile(`(?is)<(?:h[1-6]|div\b[^>]*data-type=["']heading["'][^>]*)>(.*?)</(?:h[1-6]|div)>`)
	pyrusOutgoingParagraphOpenRe = regexp.MustCompile(`(?is)<p\b[^>]*>`)
	pyrusOutgoingParagraphEndRe  = regexp.MustCompile(`(?is)</p>`)
	pyrusOutgoingDivOpenRe       = regexp.MustCompile(`(?is)<div\b[^>]*>`)
	pyrusOutgoingDivEndRe        = regexp.MustCompile(`(?is)</div>`)
	pyrusOutgoingListOpenRe      = regexp.MustCompile(`(?is)<(ul|ol)\b[^>]*>`)
	pyrusOutgoingListEndRe       = regexp.MustCompile(`(?is)</(ul|ol)>`)
	pyrusOutgoingListItemOpenRe  = regexp.MustCompile(`(?is)<li\b[^>]*>`)
	pyrusOutgoingListItemEndRe   = regexp.MustCompile(`(?is)</li>`)
	pyrusOutgoingStrongOpenRe    = regexp.MustCompile(`(?is)<(?:strong|b)\b[^>]*>`)
	pyrusOutgoingStrongEndRe     = regexp.MustCompile(`(?is)</(?:strong|b)>`)
	pyrusOutgoingEmOpenRe        = regexp.MustCompile(`(?is)<(?:em|i)\b[^>]*>`)
	pyrusOutgoingEmEndRe         = regexp.MustCompile(`(?is)</(?:em|i)>`)
	pyrusOutgoingStrikeOpenRe    = regexp.MustCompile(`(?is)<(?:s|strike|del)\b[^>]*>`)
	pyrusOutgoingStrikeEndRe     = regexp.MustCompile(`(?is)</(?:s|strike|del)>`)
	pyrusOutgoingQuoteOpenRe     = regexp.MustCompile(`(?is)<(?:blockquote|q)\b[^>]*>`)
	pyrusOutgoingQuoteEndRe      = regexp.MustCompile(`(?is)</(?:blockquote|q)>`)
	pyrusOutgoingCodeOpenRe      = regexp.MustCompile(`(?is)<code\b[^>]*>`)
	pyrusOutgoingCodeEndRe       = regexp.MustCompile(`(?is)</code>`)
	pyrusOutgoingButtonOpenRe    = regexp.MustCompile(`(?is)<button\b[^>]*>`)
	pyrusOutgoingButtonEndRe     = regexp.MustCompile(`(?is)</button>`)
	pyrusOutgoingMarkOpenRe      = regexp.MustCompile(`(?is)<mark\b[^>]*data-color=["'](red|yellow|green|blue)["'][^>]*>`)
	pyrusOutgoingMarkEndRe       = regexp.MustCompile(`(?is)</mark>`)
	pyrusOutgoingBrRe            = regexp.MustCompile(`(?is)<br\s*/?>`)
	pyrusOutgoingAnyTagRe        = regexp.MustCompile(`(?is)<[^>]+>`)
	pyrusOutgoingMultiBreakRe    = regexp.MustCompile(`(?:\s*<br\/>\s*){3,}`)
	pyrusOutgoingHrefAttrRe      = regexp.MustCompile(`(?is)href=["']([^"']+)["']`)
)

func buildPyrusCommentText(raw string) (string, string) {
	plainText := convertEtalonHTMLToPyrusPlainText(raw)
	formattedText := convertEtalonHTMLToPyrusFormatted(raw)
	if !pyrusFormattedTextNeedsRichMode(formattedText) {
		return plainText, ""
	}
	return plainText, formattedText
}

func convertEtalonHTMLToPyrusPlainText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = pyrusOutgoingMentionLinkRe.ReplaceAllStringFunc(text, func(link string) string {
		return html.EscapeString(resolvePyrusMentionLabel(link))
	})
	text = pyrusOutgoingLinkRe.ReplaceAllStringFunc(text, func(item string) string {
		href, label := parsePyrusLink(item)
		switch {
		case label != "":
			return html.EscapeString(label)
		case href != "":
			return html.EscapeString(href)
		default:
			return ""
		}
	})
	text = pyrusOutgoingImageRe.ReplaceAllString(text, "")
	text = pyrusOutgoingPreCodeRe.ReplaceAllString(text, "$1")
	text = pyrusOutgoingPreRe.ReplaceAllString(text, "$1")
	text = pyrusOutgoingHeadingBlockRe.ReplaceAllString(text, "$1\n")
	text = pyrusOutgoingParagraphOpenRe.ReplaceAllString(text, "")
	text = pyrusOutgoingParagraphEndRe.ReplaceAllString(text, "\n\n")
	text = pyrusOutgoingDivOpenRe.ReplaceAllStringFunc(text, stripPyrusGenericDivOpenTag)
	text = pyrusOutgoingDivEndRe.ReplaceAllString(text, "\n")
	text = pyrusOutgoingListOpenRe.ReplaceAllString(text, "")
	text = pyrusOutgoingListEndRe.ReplaceAllString(text, "\n")
	text = pyrusOutgoingListItemOpenRe.ReplaceAllString(text, "• ")
	text = pyrusOutgoingListItemEndRe.ReplaceAllString(text, "\n")
	text = pyrusOutgoingBrRe.ReplaceAllString(text, "\n")
	text = pyrusOutgoingAnyTagRe.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	return normalizePyrusPlainText(text)
}

func convertEtalonHTMLToPyrusFormatted(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = pyrusOutgoingMentionLinkRe.ReplaceAllStringFunc(text, func(link string) string {
		return html.EscapeString(resolvePyrusMentionLabel(link))
	})
	text = pyrusOutgoingLinkRe.ReplaceAllStringFunc(text, func(item string) string {
		href, label := parsePyrusLink(item)
		if label == "" {
			label = href
		}
		label = html.EscapeString(label)
		if href == "" {
			return label
		}
		return fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(href), label)
	})
	text = pyrusOutgoingImageRe.ReplaceAllString(text, "")
	text = pyrusOutgoingPreCodeRe.ReplaceAllStringFunc(text, func(item string) string {
		match := pyrusOutgoingPreCodeRe.FindStringSubmatch(item)
		if len(match) < 2 {
			return ""
		}
		return "<code>" + html.EscapeString(strings.TrimSpace(stripHTMLTags(match[1]))) + "</code>"
	})
	text = pyrusOutgoingPreRe.ReplaceAllStringFunc(text, func(item string) string {
		match := pyrusOutgoingPreRe.FindStringSubmatch(item)
		if len(match) < 2 {
			return ""
		}
		return "<code>" + html.EscapeString(strings.TrimSpace(stripHTMLTags(match[1]))) + "</code>"
	})
	text = pyrusOutgoingHeadingBlockRe.ReplaceAllString(text, `<div data-type="heading">$1</div>`)
	text = pyrusOutgoingStrongOpenRe.ReplaceAllString(text, "<b>")
	text = pyrusOutgoingStrongEndRe.ReplaceAllString(text, "</b>")
	text = pyrusOutgoingEmOpenRe.ReplaceAllString(text, "<i>")
	text = pyrusOutgoingEmEndRe.ReplaceAllString(text, "</i>")
	text = pyrusOutgoingStrikeOpenRe.ReplaceAllString(text, "<s>")
	text = pyrusOutgoingStrikeEndRe.ReplaceAllString(text, "</s>")
	text = pyrusOutgoingQuoteOpenRe.ReplaceAllString(text, "<q>")
	text = pyrusOutgoingQuoteEndRe.ReplaceAllString(text, "</q>")
	text = pyrusOutgoingCodeOpenRe.ReplaceAllString(text, "<code>")
	text = pyrusOutgoingCodeEndRe.ReplaceAllString(text, "</code>")
	text = pyrusOutgoingButtonOpenRe.ReplaceAllString(text, "<button>")
	text = pyrusOutgoingButtonEndRe.ReplaceAllString(text, "</button>")
	text = pyrusOutgoingMarkOpenRe.ReplaceAllString(text, `<mark data-color="$1">`)
	text = pyrusOutgoingMarkEndRe.ReplaceAllString(text, "</mark>")
	text = pyrusOutgoingParagraphOpenRe.ReplaceAllString(text, "")
	text = pyrusOutgoingParagraphEndRe.ReplaceAllString(text, "<br/><br/>")
	text = pyrusOutgoingDivOpenRe.ReplaceAllStringFunc(text, stripPyrusGenericDivOpenTag)
	text = pyrusOutgoingDivEndRe.ReplaceAllString(text, "<br/>")
	text = pyrusOutgoingListOpenRe.ReplaceAllString(text, "<$1>")
	text = pyrusOutgoingListEndRe.ReplaceAllString(text, "</$1>")
	text = pyrusOutgoingListItemOpenRe.ReplaceAllString(text, "<li>")
	text = pyrusOutgoingListItemEndRe.ReplaceAllString(text, "</li>")
	text = pyrusOutgoingBrRe.ReplaceAllString(text, "<br/>")
	text = sanitizePyrusSupportedTags(text)
	text = strings.ReplaceAll(text, "\n", "<br/>")
	text = pyrusOutgoingMultiBreakRe.ReplaceAllString(text, "<br/><br/>")
	text = strings.TrimSpace(text)
	for strings.HasPrefix(text, "<br/>") {
		text = strings.TrimPrefix(text, "<br/>")
		text = strings.TrimSpace(text)
	}
	for strings.HasSuffix(text, "<br/>") {
		text = strings.TrimSuffix(text, "<br/>")
		text = strings.TrimSpace(text)
	}
	text = strings.TrimSpace(text)
	return text
}

func sanitizePyrusSupportedTags(value string) string {
	return pyrusOutgoingAnyTagRe.ReplaceAllStringFunc(value, func(tag string) string {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		switch normalized {
		case "<b>", "</b>", "<i>", "</i>", "<s>", "</s>", "<code>", "</code>", "<q>", "</q>",
			"<ul>", "</ul>", "<ol>", "</ol>", "<li>", "</li>", "<button>", "</button>",
			"<br>", "<br/>", "<br />", "</mark>", "</div>":
			if strings.HasPrefix(normalized, "<br") {
				return "<br/>"
			}
			return normalized
		}
		if strings.HasPrefix(normalized, "<a ") {
			href := sanitizePyrusOutgoingURL(extractAttribute(tag, pyrusOutgoingHrefAttrRe))
			if href == "" {
				return ""
			}
			return fmt.Sprintf(`<a href="%s">`, html.EscapeString(href))
		}
		if strings.HasPrefix(normalized, "<div") && strings.Contains(normalized, `data-type="heading"`) {
			return `<div data-type="heading">`
		}
		if strings.HasPrefix(normalized, "<mark") {
			color := strings.ToLower(strings.TrimSpace(extractAttribute(tag, pyrusOutgoingMarkOpenRe)))
			switch color {
			case "red", "yellow", "green", "blue":
				return fmt.Sprintf(`<mark data-color="%s">`, color)
			default:
				return ""
			}
		}
		if normalized == "</a>" {
			return "</a>"
		}
		return ""
	})
}

func pyrusFormattedTextNeedsRichMode(formatted string) bool {
	value := strings.TrimSpace(formatted)
	if value == "" {
		return false
	}
	value = pyrusOutgoingBrRe.ReplaceAllString(value, "")
	return pyrusOutgoingAnyTagRe.MatchString(value)
}

func resolvePyrusMentionLabel(link string) string {
	name := strings.TrimSpace(strings.TrimPrefix(extractAttribute(link, htmlDataUserNameRe), "@"))
	if name != "" {
		return name
	}
	return strings.TrimSpace(strings.TrimPrefix(stripHTMLTags(extractInnerHTML(link)), "@"))
}

func parsePyrusLink(value string) (string, string) {
	matches := pyrusOutgoingLinkRe.FindStringSubmatch(value)
	if len(matches) < 3 {
		return "", strings.TrimSpace(stripHTMLTags(value))
	}
	href := sanitizePyrusOutgoingURL(html.UnescapeString(matches[1]))
	label := strings.TrimSpace(html.UnescapeString(stripHTMLTags(matches[2])))
	return href, label
}

func sanitizePyrusOutgoingURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https", "mailto", "tel":
		return value
	default:
		return ""
	}
}

func normalizePyrusPlainText(value string) string {
	text := strings.ReplaceAll(value, "\u00a0", " ")
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	emptyCount := 0
	for _, line := range lines {
		normalizedLine := strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if normalizedLine == "" {
			emptyCount++
			if emptyCount > 1 {
				continue
			}
			cleaned = append(cleaned, "")
			continue
		}
		emptyCount = 0
		cleaned = append(cleaned, normalizedLine)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func stripPyrusGenericDivOpenTag(tag string) string {
	normalized := strings.ToLower(strings.TrimSpace(tag))
	if strings.Contains(normalized, `data-type="heading"`) {
		return tag
	}
	return ""
}
