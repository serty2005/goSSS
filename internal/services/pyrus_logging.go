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
	context := buildPyrusTaskContext(task)
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
		"comments":          pyrusCommentSummaries(task, task.Comments),
		"task_attachments":  pyrusAttachmentSummaries(task.Attachments),
		"author":            safePyrusPersonName(task.Author),
		"responsible":       safePyrusPersonName(task.Responsible),
		"context":           pyrusTicketContextSummary(context),
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

func pyrusCommentSummaries(task *pyrusplugin.Task, comments []pyrusplugin.Comment) []map[string]any {
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
			"channel_from":      safePyrusChannelParty(comment.Channel, true),
			"channel_to":        safePyrusChannelParty(comment.Channel, false),
			"classification":    classifyPyrusComment(task, &comment),
			"roles":             safePyrusCommentRoles(comment.CommentAsRoles),
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
		"reply_to_client":   comment.ReplyToClient,
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

func safePyrusChannelParty(channel *pyrusplugin.Channel, from bool) map[string]string {
	if channel == nil {
		return nil
	}
	party := channel.To
	if from {
		party = channel.From
	}
	if party == nil {
		return nil
	}
	return map[string]string{
		"name":  truncateForPyrusLog(party.Name, pyrusLogTextPreviewLimit),
		"email": truncateForPyrusLog(party.Email, pyrusLogTextPreviewLimit),
	}
}

func safePyrusCommentRoles(roles []pyrusplugin.CommentRole) []string {
	if len(roles) == 0 {
		return nil
	}
	result := make([]string, 0, len(roles))
	for i := range roles {
		label := strings.TrimSpace(roles[i].Name)
		if label == "" {
			label = fmt.Sprintf("%d", roles[i].ID)
		}
		result = append(result, label)
	}
	return result
}

func pyrusTicketContextSummary(context *pyrusTaskContext) map[string]any {
	if context == nil {
		return nil
	}
	return map[string]any{
		"crm_id":                    context.CRMID,
		"uid":                       context.UID,
		"sender_name":               context.SenderName,
		"sender_email":              context.SenderEmail,
		"sender_position":           context.SenderPosition,
		"sender_messenger_nickname": context.SenderMessengerNickname,
		"restaurant_task_id":        safeInt64Pointer(context.RestaurantTaskID),
		"restaurant_subject":        truncateForPyrusLog(context.RestaurantSubject, pyrusLogTextPreviewLimit),
		"call_type":                 truncateForPyrusLog(context.CallType, pyrusLogTextPreviewLimit),
		"module":                    truncateForPyrusLog(context.Module, pyrusLogTextPreviewLimit),
		"partner_name":              truncateForPyrusLog(context.PartnerName, pyrusLogTextPreviewLimit),
		"partner_crm_id":            truncateForPyrusLog(context.PartnerCRMID, pyrusLogTextPreviewLimit),
		"iiko_web_link":             truncateForPyrusLog(context.IikoWebLink, pyrusLogTextPreviewLimit),
		"iiko_biz_link":             truncateForPyrusLog(context.IikoBizLink, pyrusLogTextPreviewLimit),
		"domain":                    truncateForPyrusLog(context.Domain, pyrusLogTextPreviewLimit),
		"version":                   truncateForPyrusLog(context.Version, pyrusLogTextPreviewLimit),
		"open_period":               context.OpenPeriod,
	}
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
