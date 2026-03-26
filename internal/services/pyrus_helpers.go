package services

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/datatypes"
)

const pyrusExtIDSystemCommentText = "Служебное сообщение goSSS: записан локальный идентификатор заявки."

type pyrusTaskContext struct {
	TaskID                  int64
	FormID                  int64
	CRMID                   string
	UID                     string
	Subject                 string
	CallType                string
	Module                  string
	SenderName              string
	SenderEmail             string
	SenderPosition          string
	SenderMessengerNickname string
	RestaurantTaskID        *int64
	RestaurantSubject       string
	PartnerItemID           *int64
	PartnerName             string
	PartnerCRMID            string
	IikoWebLink             string
	IikoBizLink             string
	Domain                  string
	Version                 string
	OpenPeriod              *int
	RawFields               map[string]any
}

type pyrusFormLinkValue struct {
	TaskID  int64   `json:"task_id"`
	TaskIDs []int64 `json:"task_ids"`
	Subject string  `json:"subject"`
}

type pyrusCatalogValue struct {
	ItemID  int64      `json:"item_id"`
	ItemIDs []int64    `json:"item_ids"`
	Headers []string   `json:"headers"`
	Values  []string   `json:"values"`
	Rows    [][]string `json:"rows"`
}

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

func buildPyrusTaskContext(task *pyrusplugin.Task) *pyrusTaskContext {
	if task == nil {
		return nil
	}
	context := &pyrusTaskContext{
		TaskID:      task.ID,
		FormID:      task.FormID,
		CRMID:       strings.TrimSpace(extractPyrusFieldString(task, "CRMID", "CrmId", "crm_id")),
		UID:         strings.TrimSpace(extractPyrusFieldString(task, "UID")),
		Subject:     strings.TrimSpace(extractPyrusFieldString(task, "Subject")),
		CallType:    strings.TrimSpace(extractPyrusFieldString(task, "CallType", "Тип обращения")),
		Module:      strings.TrimSpace(extractPyrusFieldString(task, "Module", "Модуль")),
		SenderName:  strings.TrimSpace(extractPyrusFieldString(task, "SenderName", "Имя отправителя")),
		IikoWebLink: strings.TrimSpace(extractPyrusFieldString(task, "iikoWEB")),
		IikoBizLink: strings.TrimSpace(extractPyrusFieldString(task, "iikoBIZ")),
		Domain:      strings.TrimSpace(extractPyrusFieldString(task, "Домен")),
		Version:     strings.TrimSpace(extractPyrusFieldString(task, "Версия")),
		OpenPeriod:  parsePyrusIntPointer(extractPyrusFieldString(task, "Открытый период")),
		RawFields:   buildPyrusRawFieldSnapshot(task),
	}
	if context.SenderName == "" {
		context.SenderName = resolvePyrusTaskClientName(task)
	}
	context.IikoWebLink = strings.TrimSpace(trimmedPtrString(validators.ValidateIikoWebLink(context.IikoWebLink)))
	context.SenderEmail = resolvePyrusTaskClientEmail(task)
	context.SenderPosition = resolvePyrusTaskClientPosition(task)
	context.SenderMessengerNickname = resolvePyrusTaskClientMessenger(task)

	if restaurant := decodePyrusFormLinkField(findPyrusTopLevelField(task, "Restaurant", "Ресторан")); restaurant != nil {
		if restaurant.TaskID > 0 {
			context.RestaurantTaskID = &restaurant.TaskID
		}
		context.RestaurantSubject = strings.TrimSpace(restaurant.Subject)
	}
	if partner := decodePyrusCatalogField(findPyrusTopLevelField(task, "Партнер", "Partner")); partner != nil {
		if partner.ItemID > 0 {
			context.PartnerItemID = &partner.ItemID
		}
		context.PartnerName = pyrusCatalogLookup(*partner, "Partner_name", "Partner", "Партнер")
		context.PartnerCRMID = pyrusCatalogLookup(*partner, "CRMID", "CrmId")
	}
	return context
}

func buildPyrusRawFieldSnapshot(task *pyrusplugin.Task) map[string]any {
	if task == nil || len(task.Fields) == 0 {
		return nil
	}
	result := make(map[string]any, len(task.Fields))
	for i := range task.Fields {
		field := task.Fields[i]
		key := strings.TrimSpace(field.Code)
		if key == "" {
			key = strings.TrimSpace(field.Name)
		}
		if key == "" {
			key = fmt.Sprintf("field_%d", i+1)
		}
		result[key] = field.Value
	}
	return result
}

func normalizePyrusFieldKey(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

func findPyrusTopLevelField(task *pyrusplugin.Task, aliases ...string) *pyrusplugin.Field {
	if task == nil || len(aliases) == 0 {
		return nil
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
			return &task.Fields[i]
		}
		if _, ok := targets[normalizePyrusFieldKey(field.Name)]; ok {
			return &task.Fields[i]
		}
	}
	return nil
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
	for _, field := range flattenPyrusFields(task.Fields) {
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
	if nested := flattenPyrusFields([]pyrusplugin.Field{field}); len(nested) > 1 {
		parts := make([]string, 0, len(nested)-1)
		for _, child := range nested[1:] {
			childValue := fieldToString(child)
			if childValue == "" {
				continue
			}
			label := strings.TrimSpace(child.Name)
			if label == "" {
				label = strings.TrimSpace(child.Code)
			}
			if label == "" {
				parts = append(parts, childValue)
				continue
			}
			parts = append(parts, label+": "+childValue)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
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
		for _, key := range []string{"text", "value", "name", "title", "subject", "nickname", "email"} {
			if text := anyToString(typed[key]); text != "" {
				return text
			}
		}
		headers, headersOK := typed["headers"].([]any)
		values, valuesOK := typed["values"].([]any)
		if headersOK && valuesOK && len(headers) == len(values) {
			parts := make([]string, 0, len(headers))
			for i := range headers {
				key := anyToString(headers[i])
				value := anyToString(values[i])
				if key == "" && value == "" {
					continue
				}
				if key == "" {
					parts = append(parts, value)
					continue
				}
				parts = append(parts, key+"="+value)
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; ")
			}
		}
		if fields, ok := typed["fields"].([]any); ok {
			parts := make([]string, 0, len(fields))
			for _, item := range fields {
				text := anyToString(item)
				if text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; ")
			}
		}
	}
	return ""
}

func pyrusCommentAuthorName(task *pyrusplugin.Task, comment *pyrusplugin.Comment) string {
	if comment == nil {
		return "Pyrus"
	}
	switch classifyPyrusComment(task, comment) {
	case pyrusCommentKindClientIncoming:
		if value := resolvePyrusClientDisplayName(task, comment); value != "" {
			return value
		}
	case pyrusCommentKindClientOutgoing, pyrusCommentKindInternal:
		if value := resolvePyrusOperatorDisplayName(comment); value != "" {
			return value
		}
	}
	if comment.Author != nil {
		if value := strings.TrimSpace(comment.Author.DisplayName()); value != "" {
			return value
		}
	}
	return "Pyrus"
}

type pyrusCommentKind string

const (
	pyrusCommentKindUnknown        pyrusCommentKind = "unknown"
	pyrusCommentKindClientIncoming pyrusCommentKind = "client_incoming"
	pyrusCommentKindClientOutgoing pyrusCommentKind = "client_outgoing"
	pyrusCommentKindInternal       pyrusCommentKind = "internal"
	pyrusCommentKindSystem         pyrusCommentKind = "system"
)

func classifyPyrusComment(task *pyrusplugin.Task, comment *pyrusplugin.Comment) pyrusCommentKind {
	if isPyrusExtIDSystemComment(comment, strings.TrimSpace(extractPyrusFieldString(task, "ext_id"))) {
		return pyrusCommentKindSystem
	}
	if comment == nil {
		return pyrusCommentKindUnknown
	}
	if comment.Channel != nil && normalizePyrusFieldKey(comment.Channel.Type) == normalizePyrusFieldKey("mobile_app") {
		if comment.Channel.From != nil && (strings.TrimSpace(comment.Channel.From.Name) != "" || strings.TrimSpace(comment.Channel.From.Email) != "") {
			return pyrusCommentKindClientIncoming
		}
		if comment.Channel.To != nil && (strings.TrimSpace(comment.Channel.To.Name) != "" || strings.TrimSpace(comment.Channel.To.Email) != "") {
			return pyrusCommentKindClientOutgoing
		}
	}
	if len(comment.CommentAsRoles) > 0 || comment.Channel == nil {
		return pyrusCommentKindInternal
	}
	return pyrusCommentKindUnknown
}

func flattenPyrusFields(fields []pyrusplugin.Field) []pyrusplugin.Field {
	if len(fields) == 0 {
		return nil
	}
	result := make([]pyrusplugin.Field, 0, len(fields))
	for i := range fields {
		field := fields[i]
		result = append(result, field)
		children := decodePyrusTitleFields(field)
		if len(children) == 0 {
			continue
		}
		result = append(result, flattenPyrusFields(children)...)
	}
	return result
}

func decodePyrusTitleFields(field pyrusplugin.Field) []pyrusplugin.Field {
	payload, ok := field.Value.(map[string]any)
	if !ok {
		return nil
	}
	rawFields, ok := payload["fields"]
	if !ok {
		return nil
	}
	items, ok := rawFields.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return nil
	}
	var result []pyrusplugin.Field
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil
	}
	return result
}

func decodePyrusFormLinkField(field *pyrusplugin.Field) *pyrusFormLinkValue {
	if field == nil {
		return nil
	}
	payload, ok := decodePyrusFieldValue[pyrusFormLinkValue](field.Value)
	if !ok {
		return nil
	}
	return payload
}

func decodePyrusCatalogField(field *pyrusplugin.Field) *pyrusCatalogValue {
	if field == nil {
		return nil
	}
	payload, ok := decodePyrusFieldValue[pyrusCatalogValue](field.Value)
	if !ok {
		return nil
	}
	return payload
}

func decodePyrusFieldValue[T any](value any) (*T, bool) {
	if value == nil {
		return nil, false
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var result T
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, false
	}
	return &result, true
}

func pyrusCatalogLookup(value pyrusCatalogValue, aliases ...string) string {
	if len(value.Headers) == 0 || len(value.Values) == 0 || len(value.Headers) != len(value.Values) {
		return ""
	}
	targets := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		key := normalizePyrusFieldKey(alias)
		if key != "" {
			targets[key] = struct{}{}
		}
	}
	for i := range value.Headers {
		if _, ok := targets[normalizePyrusFieldKey(value.Headers[i])]; !ok {
			continue
		}
		if text := strings.TrimSpace(value.Values[i]); text != "" {
			return text
		}
	}
	return ""
}

func resolvePyrusTaskClientName(task *pyrusplugin.Task) string {
	if task == nil {
		return ""
	}
	for i := range task.Comments {
		if classifyPyrusComment(task, &task.Comments[i]) != pyrusCommentKindClientIncoming {
			continue
		}
		if value := resolvePyrusClientDisplayName(task, &task.Comments[i]); value != "" {
			return value
		}
	}
	if task.Author != nil {
		if value := strings.TrimSpace(task.Author.DisplayName()); value != "" {
			return value
		}
	}
	return ""
}

func resolvePyrusTaskClientEmail(task *pyrusplugin.Task) string {
	if task == nil {
		return ""
	}
	for i := range task.Comments {
		comment := task.Comments[i]
		if classifyPyrusComment(task, &comment) != pyrusCommentKindClientIncoming {
			continue
		}
		for _, candidate := range []string{
			safePyrusPersonEmail(&comment),
			pyrusChannelPartyEmail(comment.Channel, true),
		} {
			if value := strings.TrimSpace(candidate); value != "" {
				return value
			}
		}
	}
	if task.Author != nil {
		return strings.TrimSpace(task.Author.Email)
	}
	return ""
}

func resolvePyrusTaskClientPosition(task *pyrusplugin.Task) string {
	if task == nil {
		return ""
	}
	for i := range task.Comments {
		comment := task.Comments[i]
		if classifyPyrusComment(task, &comment) != pyrusCommentKindClientIncoming || comment.Author == nil {
			continue
		}
		if value := strings.TrimSpace(comment.Author.Position); value != "" {
			return value
		}
	}
	if task.Author != nil {
		return strings.TrimSpace(task.Author.Position)
	}
	return ""
}

func resolvePyrusTaskClientMessenger(task *pyrusplugin.Task) string {
	if task == nil {
		return ""
	}
	for i := range task.Comments {
		comment := task.Comments[i]
		if classifyPyrusComment(task, &comment) != pyrusCommentKindClientIncoming {
			continue
		}
		if value := safePyrusPersonMessenger(&comment); value != "" {
			return value
		}
	}
	if task.Author != nil && task.Author.Messenger != nil {
		return strings.TrimSpace(task.Author.Messenger.Nickname)
	}
	return ""
}

func resolvePyrusClientDisplayName(task *pyrusplugin.Task, comment *pyrusplugin.Comment) string {
	for _, candidate := range []string{
		extractPyrusFieldString(task, "SenderName", "Имя отправителя"),
		pyrusChannelPartyName(comment, true),
		safePyrusPersonDisplayName(comment),
		safePyrusPersonEmail(comment),
		safePyrusPersonMessenger(comment),
	} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return "Клиент Pyrus"
}

func resolvePyrusOperatorDisplayName(comment *pyrusplugin.Comment) string {
	if comment == nil {
		return ""
	}
	for i := range comment.CommentAsRoles {
		if value := strings.TrimSpace(comment.CommentAsRoles[i].Name); value != "" {
			return value
		}
	}
	for _, candidate := range []string{
		safePyrusPersonDisplayName(comment),
		safePyrusPersonEmail(comment),
		pyrusChannelPartyName(comment, false),
		safePyrusPersonMessenger(comment),
	} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func pyrusChannelPartyName(comment *pyrusplugin.Comment, from bool) string {
	if comment == nil || comment.Channel == nil {
		return ""
	}
	party := comment.Channel.To
	if from {
		party = comment.Channel.From
	}
	if party == nil {
		return ""
	}
	for _, candidate := range []string{party.Name, party.Email} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func pyrusChannelPartyEmail(channel *pyrusplugin.Channel, from bool) string {
	if channel == nil {
		return ""
	}
	party := channel.To
	if from {
		party = channel.From
	}
	if party == nil {
		return ""
	}
	return strings.TrimSpace(party.Email)
}

func safePyrusPersonDisplayName(comment *pyrusplugin.Comment) string {
	if comment == nil || comment.Author == nil {
		return ""
	}
	name := comment.Author.DisplayName()
	if strings.TrimSpace(name) != "" {
		return name
	}
	fullName := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(comment.Author.FirstName),
		strings.TrimSpace(comment.Author.LastName),
	}, " "))
	return strings.TrimSpace(fullName)
}

func safePyrusPersonEmail(comment *pyrusplugin.Comment) string {
	if comment == nil || comment.Author == nil {
		return ""
	}
	return strings.TrimSpace(comment.Author.Email)
}

func safePyrusPersonMessenger(comment *pyrusplugin.Comment) string {
	if comment == nil || comment.Author == nil || comment.Author.Messenger == nil {
		return ""
	}
	return strings.TrimSpace(comment.Author.Messenger.Nickname)
}

func parsePyrusIntPointer(value string) *int {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	result := parsed
	return &result
}

func buildPyrusTicketContextEntity(ticketID string, context *pyrusTaskContext) *pyrus.TicketContext {
	if strings.TrimSpace(ticketID) == "" || context == nil {
		return nil
	}
	item := &pyrus.TicketContext{
		TicketID:                strings.TrimSpace(ticketID),
		PyrusTaskID:             context.TaskID,
		PyrusFormID:             context.FormID,
		CRMID:                   strings.TrimSpace(context.CRMID),
		UID:                     strings.TrimSpace(context.UID),
		Subject:                 strings.TrimSpace(context.Subject),
		CallType:                strings.TrimSpace(context.CallType),
		Module:                  strings.TrimSpace(context.Module),
		SenderName:              strings.TrimSpace(context.SenderName),
		SenderEmail:             strings.TrimSpace(context.SenderEmail),
		SenderPosition:          strings.TrimSpace(context.SenderPosition),
		SenderMessengerNickname: strings.TrimSpace(context.SenderMessengerNickname),
		RestaurantSubject:       strings.TrimSpace(context.RestaurantSubject),
		PartnerName:             strings.TrimSpace(context.PartnerName),
		PartnerCRMID:            strings.TrimSpace(context.PartnerCRMID),
		IikoWebLink:             strings.TrimSpace(context.IikoWebLink),
		IikoBizLink:             strings.TrimSpace(context.IikoBizLink),
		Domain:                  strings.TrimSpace(context.Domain),
		Version:                 strings.TrimSpace(context.Version),
		OpenPeriod:              context.OpenPeriod,
	}
	if context.RestaurantTaskID != nil && *context.RestaurantTaskID > 0 {
		taskID := *context.RestaurantTaskID
		item.RestaurantTaskID = &taskID
	}
	if context.PartnerItemID != nil && *context.PartnerItemID > 0 {
		itemID := *context.PartnerItemID
		item.PartnerItemID = &itemID
	}
	if len(context.RawFields) > 0 {
		payload, err := json.Marshal(context.RawFields)
		if err == nil {
			item.RawFields = datatypes.JSON(payload)
		}
	}
	return item
}

func applyPyrusClientContextToTicket(ticket *tickets.Ticket, context *pyrusTaskContext) bool {
	if ticket == nil || context == nil {
		return false
	}
	changed := false
	if value := strings.TrimSpace(resolvePyrusTaskClientNameFromContext(context)); value != "" && ticket.ReporterName != value {
		ticket.ReporterName = value
		changed = true
	}
	if value := strings.TrimSpace(context.SenderEmail); value != "" && ticket.ReporterEmail != value {
		ticket.ReporterEmail = value
		changed = true
	}
	return changed
}

func resolvePyrusTaskClientNameFromContext(context *pyrusTaskContext) string {
	if context == nil {
		return ""
	}
	for _, candidate := range []string{
		context.SenderName,
		context.SenderEmail,
		context.SenderMessengerNickname,
	} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func trimmedPtrString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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
		if classifyPyrusComment(task, &comment) != pyrusCommentKindClientIncoming {
			continue
		}
		if value := strings.TrimSpace(comment.Text); value != "" {
			return value
		}
	}
	return ""
}
