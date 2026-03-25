package services

import (
	"etalon-server/internal/domain/tickets"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"fmt"
	"strings"
)

const (
	pyrusLogTextPreviewLimit    = 240
	pyrusLogPayloadPreviewLimit = 2048
)

func truncateForPyrusLog(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "...(truncated)"
}

func pyrusWebhookPayloadSummary(payload *pyrusplugin.WebhookPayload) map[string]any {
	if payload == nil {
		return map[string]any{"is_nil": true}
	}
	return map[string]any{
		"event":                strings.TrimSpace(payload.Event),
		"task_id":              resolvePyrusTaskID(payload),
		"task_form_id":         payload.Task.FormID,
		"user_id":              safePyrusUserID(payload.UserID),
		"access_token_present": strings.TrimSpace(payload.AccessToken) != "",
		"task":                 pyrusTaskSummary(&payload.Task),
	}
}

func pyrusTaskSummary(task *pyrusplugin.Task) map[string]any {
	if task == nil {
		return map[string]any{"is_nil": true}
	}
	return map[string]any{
		"id":                task.ID,
		"form_id":           task.FormID,
		"subject":           truncateForPyrusLog(extractPyrusFieldString(task, "Subject"), pyrusLogTextPreviewLimit),
		"crm_id":            strings.TrimSpace(extractPyrusFieldString(task, "CRMID", "CrmId", "crm_id")),
		"restaurant":        truncateForPyrusLog(extractPyrusFieldString(task, "Restaurant"), pyrusLogTextPreviewLimit),
		"module":            truncateForPyrusLog(extractPyrusFieldString(task, "Module"), pyrusLogTextPreviewLimit),
		"call_type":         truncateForPyrusLog(extractPyrusFieldString(task, "CallType", "Call Type"), pyrusLogTextPreviewLimit),
		"sender_name":       truncateForPyrusLog(extractPyrusFieldString(task, "SenderName", "Sender Name"), pyrusLogTextPreviewLimit),
		"ext_id":            strings.TrimSpace(extractPyrusFieldString(task, "ext_id")),
		"text_preview":      truncateForPyrusLog(task.Text, pyrusLogTextPreviewLimit),
		"status":            resolvePyrusTaskStatus(task),
		"field_count":       len(task.Fields),
		"comment_count":     len(task.Comments),
		"attachment_count":  len(task.Attachments),
		"field_values":      pyrusFieldMap(task.Fields),
		"comments":          pyrusCommentSummaries(task.Comments),
		"task_attachments":  pyrusAttachmentSummaries(task.Attachments),
		"author":            safePyrusPersonName(task.Author),
		"responsible":       safePyrusPersonName(task.Responsible),
		"close_date_exists": task.CloseDate != nil,
	}
}

func pyrusFieldMap(fields []pyrusplugin.Field) map[string]string {
	if len(fields) == 0 {
		return nil
	}
	result := make(map[string]string, len(fields))
	for i := range fields {
		field := fields[i]
		key := strings.TrimSpace(field.Code)
		if key == "" {
			key = strings.TrimSpace(field.Name)
		}
		if key == "" {
			key = fmt.Sprintf("field_%d", i+1)
		}
		result[key] = truncateForPyrusLog(fieldToString(field), pyrusLogTextPreviewLimit)
	}
	return result
}

func pyrusCommentSummaries(comments []pyrusplugin.Comment) []map[string]any {
	if len(comments) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(comments))
	for i := range comments {
		comment := comments[i]
		result = append(result, map[string]any{
			"id":                comment.ID,
			"author":            safePyrusPersonName(comment.Author),
			"action":            strings.TrimSpace(comment.Action),
			"channel_type":      safePyrusChannelType(comment.Channel),
			"text_preview":      truncateForPyrusLog(comment.Text, pyrusLogTextPreviewLimit),
			"attachment_count":  len(comment.Attachments),
			"attachments":       pyrusAttachmentSummaries(comment.Attachments),
			"field_updates":     pyrusFieldMap(comment.FieldUpdates),
			"created_at_is_set": !comment.CreateDate.IsZero(),
		})
	}
	return result
}

func pyrusAttachmentSummaries(attachments []pyrusplugin.Attachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(attachments))
	for i := range attachments {
		attachment := attachments[i]
		result = append(result, map[string]any{
			"id":      attachment.ID,
			"name":    strings.TrimSpace(attachment.Name),
			"size":    attachment.Size,
			"root_id": attachment.RootID,
		})
	}
	return result
}

func localTicketCommentSummary(comment *tickets.TicketComment) map[string]any {
	if comment == nil {
		return map[string]any{"is_nil": true}
	}
	return map[string]any{
		"id":                strings.TrimSpace(comment.ID),
		"ticket_id":         strings.TrimSpace(comment.TicketID),
		"service_desk_uuid": strings.TrimSpace(comment.ServiceDeskUUID),
		"source":            strings.TrimSpace(comment.Source),
		"author_name":       truncateForPyrusLog(comment.AuthorName, pyrusLogTextPreviewLimit),
		"text_preview":      truncateForPyrusLog(comment.Text, pyrusLogTextPreviewLimit),
		"is_private":        comment.IsPrivate,
		"is_internal":       comment.IsInternal,
		"has_author_user":   comment.AuthorUserID != nil,
	}
}

func safePyrusPersonName(person *pyrusplugin.Person) string {
	if person == nil {
		return ""
	}
	return truncateForPyrusLog(person.DisplayName(), pyrusLogTextPreviewLimit)
}

func safePyrusChannelType(channel *pyrusplugin.Channel) string {
	if channel == nil {
		return ""
	}
	return strings.TrimSpace(channel.Type)
}

func safePyrusUserID(userID *int64) any {
	if userID == nil {
		return nil
	}
	return *userID
}

func safeStringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func safeInt64Pointer(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
