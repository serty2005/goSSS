package megafonvats

import (
	"net/http"
	"net/url"
	"strings"
)

func RedactURLForLog(rawURL string) string {
	trimmedURL := strings.TrimSpace(rawURL)
	if trimmedURL == "" {
		return ""
	}

	parsed, err := url.Parse(trimmedURL)
	if err != nil {
		return trimmedURL
	}

	if parsed.User != nil {
		username := redactSecretForLog(parsed.User.Username())
		if password, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(username, redactSecretForLog(password))
		} else {
			parsed.User = url.User(username)
		}
	}

	query := parsed.Query()
	if len(query) == 0 {
		return parsed.String()
	}

	for key, values := range query {
		if !isSensitiveLogField(key) {
			continue
		}
		maskedValues := make([]string, 0, len(values))
		for _, value := range values {
			maskedValues = append(maskedValues, redactSecretForLog(value))
		}
		query[key] = maskedValues
	}

	parsed.RawQuery = query.Encode()
	return strings.ReplaceAll(parsed.String(), "%2A", "*")
}

func RedactHeadersForLog(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	result := make(map[string]string, len(headers))
	for key, values := range headers {
		joined := strings.Join(values, ", ")
		if isSensitiveLogField(key) {
			joined = redactSecretForLog(joined)
		}
		result[key] = joined
	}
	return result
}

func RedactFormPayloadForLog(rawPayload string) string {
	trimmedPayload := strings.TrimSpace(rawPayload)
	if trimmedPayload == "" {
		return ""
	}

	form, err := url.ParseQuery(trimmedPayload)
	if err == nil {
		for key, values := range form {
			if !isSensitiveLogField(key) {
				continue
			}
			for i := range values {
				values[i] = redactSecretForLog(values[i])
			}
			form[key] = values
		}
		return strings.ReplaceAll(form.Encode(), "%2A", "*")
	}

	parts := strings.Split(trimmedPayload, "&")
	for i := range parts {
		key, value, found := strings.Cut(parts[i], "=")
		if !found {
			continue
		}

		decodedKey, decodeErr := url.QueryUnescape(key)
		if decodeErr != nil {
			decodedKey = key
		}
		if !isSensitiveLogField(decodedKey) {
			continue
		}

		decodedValue, decodeErr := url.QueryUnescape(value)
		if decodeErr != nil {
			decodedValue = value
		}
		parts[i] = key + "=" + url.QueryEscape(redactSecretForLog(decodedValue))
	}
	return strings.ReplaceAll(strings.Join(parts, "&"), "%2A", "*")
}

func isSensitiveLogField(field string) bool {
	normalized := strings.ToLower(strings.TrimSpace(field))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "x_api_key", "api_key", "apikey", "crm_token", "token", "access_token", "refresh_token", "authorization", "secret":
		return true
	default:
		return false
	}
}

func redactSecretForLog(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	runes := []rune(trimmed)
	switch len(runes) {
	case 1:
		return "*"
	case 2:
		return string(runes[0]) + "*"
	case 3, 4:
		return string(runes[0]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1])
	default:
		return string(runes[:2]) + strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-2:])
	}
}
