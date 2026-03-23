package services

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"fmt"
	"strings"
)

const pyrusExtIDSystemCommentText = "Служебное сообщение goSSS: записан локальный идентификатор заявки."

func pyrusWebhookSecret(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if value := strings.TrimSpace(cfg.PyrusWebhookSecret); value != "" {
		return value
	}
	return strings.TrimSpace(cfg.PyrusSecurityKey)
}

func isPyrusSignatureValid(secret string, rawBody []byte, signature string) bool {
	normalizedSecret := strings.TrimSpace(secret)
	normalizedSignature := strings.ToLower(strings.TrimSpace(signature))
	if normalizedSecret == "" || normalizedSignature == "" || len(rawBody) == 0 {
		return false
	}
	mac := hmac.New(sha1.New, []byte(normalizedSecret))
	_, _ = mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(normalizedSignature))
}

func resolvePyrusTaskID(payload *pyrusplugin.WebhookPayload) int64 {
	if payload == nil {
		return 0
	}
	if payload.TaskID > 0 {
		return payload.TaskID
	}
	if payload.Task.ID > 0 {
		return payload.Task.ID
	}
	return 0
}

func normalizePyrusFieldKey(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

func extractPyrusFieldString(task *pyrusplugin.Task, aliases ...string) string {
	if task == nil || len(aliases) == 0 {
		return ""
	}
	targets := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		key := normalizePyrusFieldKey(alias)
		if key != "" {
			targets[key] = struct{}{}
		}
	}
	for i := range task.Fields {
		field := task.Fields[i]
		if _, ok := targets[normalizePyrusFieldKey(field.Code)]; ok {
			if value := fieldToString(field); value != "" {
				return value
			}
		}
		if _, ok := targets[normalizePyrusFieldKey(field.Name)]; ok {
			if value := fieldToString(field); value != "" {
				return value
			}
		}
	}
	return ""
}

func fieldToString(field pyrusplugin.Field) string {
	if value := strings.TrimSpace(field.Text); value != "" {
		return value
	}
	if value := anyToString(field.Value); value != "" {
		return value
	}
	return anyToString(field.Number)
}

func anyToString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case *string:
		if typed == nil {
			return ""
		}
		return strings.TrimSpace(*typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	case float32:
		if typed == float32(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case uint8:
		return fmt.Sprintf("%d", typed)
	case uint16:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case []string:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item != "" {
				parts = append(parts, item)
			}
		}
		return strings.Join(parts, ", ")
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			text := anyToString(item)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, key := range []string{"text", "value", "name", "title"} {
			if text := anyToString(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func pyrusCommentAuthorName(comment *pyrusplugin.Comment) string {
	if comment == nil {
		return "Pyrus"
	}
	if comment.Author != nil {
		if value := strings.TrimSpace(comment.Author.DisplayName()); value != "" {
			return value
		}
	}
	if comment.Channel != nil && comment.Channel.From != nil {
		if value := strings.TrimSpace(comment.Channel.From.Name); value != "" {
			return value
		}
		if value := strings.TrimSpace(comment.Channel.From.Email); value != "" {
			return value
		}
	}
	return "Pyrus"
}

func isPyrusExtIDSystemComment(comment *pyrusplugin.Comment, extID string) bool {
	if comment == nil {
		return false
	}
	targetExtID := strings.TrimSpace(extID)
	hasExtIDUpdate := false
	for i := range comment.FieldUpdates {
		update := comment.FieldUpdates[i]
		keyCode := normalizePyrusFieldKey(update.Code)
		keyName := normalizePyrusFieldKey(update.Name)
		if keyCode != normalizePyrusFieldKey("ext_id") && keyName != normalizePyrusFieldKey("ext_id") {
			continue
		}
		value := fieldToString(update)
		if targetExtID == "" || strings.TrimSpace(value) == targetExtID {
			hasExtIDUpdate = true
			break
		}
	}
	if !hasExtIDUpdate {
		return false
	}
	text := strings.TrimSpace(comment.Text)
	return text == "" || text == pyrusExtIDSystemCommentText
}

func resolvePyrusTaskStatus(task *pyrusplugin.Task) string {
	if task == nil {
		return tickets.StatusNew
	}
	for i := range task.Fields {
		field := task.Fields[i]
		fieldType := normalizePyrusFieldKey(field.Type)
		fieldCode := normalizePyrusFieldKey(field.Code)
		fieldName := normalizePyrusFieldKey(field.Name)
		if fieldType != "status" && fieldCode != "status" && fieldName != "status" {
			continue
		}
		if mapped := mapPyrusStatusToken(fieldToString(field)); mapped != "" {
			return mapped
		}
	}
	for i := len(task.Comments) - 1; i >= 0; i-- {
		if mapped := mapPyrusStatusToken(task.Comments[i].Action); mapped != "" {
			return mapped
		}
	}
	if task.CloseDate != nil {
		return tickets.StatusResolved
	}
	return tickets.StatusNew
}

func mapPyrusStatusToken(raw string) string {
	switch normalizePyrusFieldKey(raw) {
	case "closed", "finished", "resolved", "done", "completed":
		return tickets.StatusResolved
	case "reopened", "reopen", "inprogress", "processing", "active", "assigned":
		return tickets.StatusInProgress
	case "pending", "waiting", "wait":
		return tickets.StatusPending
	case "deferred", "postponed":
		return tickets.StatusDeferred
	case "new", "open", "opened":
		return tickets.StatusNew
	default:
		return ""
	}
}

func buildPyrusTicketDescription(task *pyrusplugin.Task) string {
	if task == nil {
		return ""
	}
	lines := make([]string, 0, 6)
	if value := extractPyrusFieldString(task, "CRMID", "CrmId", "crm_id"); value != "" {
		lines = append(lines, "CRMID: "+value)
	}
	if value := extractPyrusFieldString(task, "Restaurant"); value != "" {
		lines = append(lines, "Restaurant: "+value)
	}
	if value := extractPyrusFieldString(task, "Module"); value != "" {
		lines = append(lines, "Module: "+value)
	}
	if value := extractPyrusFieldString(task, "CallType", "Call Type"); value != "" {
		lines = append(lines, "CallType: "+value)
	}
	if message := firstPyrusClientMessage(task); message != "" {
		lines = append(lines, "")
		lines = append(lines, "Первое сообщение:")
		lines = append(lines, message)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func firstPyrusClientMessage(task *pyrusplugin.Task) string {
	if task == nil {
		return ""
	}
	if value := strings.TrimSpace(task.Text); value != "" {
		return value
	}
	for i := range task.Comments {
		comment := task.Comments[i]
		if isPyrusExtIDSystemComment(&comment, "") {
			continue
		}
		if value := strings.TrimSpace(comment.Text); value != "" {
			return value
		}
	}
	return ""
}
