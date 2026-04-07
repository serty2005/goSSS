package bitrix

import (
	"context"
	"strings"
	"time"
)

type ContactField struct {
	ID        string
	TypeID    string
	ValueType string
	Value     string
}

type Contact struct {
	ID         int64
	Name       string
	SecondName string
	LastName   string
	CompanyID  *int64
	DateModify *time.Time
	Phones     []ContactField
	Emails     []ContactField
	IMs        []ContactField
	Raw        map[string]interface{}
}

type DealContactBinding struct {
	ContactID int64
	IsPrimary bool
	Sort      int
}

func (c *Client) DuplicateFindContactsByPhone(ctx context.Context, phone string) ([]int64, error) {
	raw, _, _, err := c.call(ctx, "crm.duplicate.findbycomm", map[string]interface{}{
		"entity_type": "CONTACT",
		"type":        "PHONE",
		"values":      []string{strings.TrimSpace(phone)},
	})
	if err != nil {
		return nil, err
	}

	resultMap, ok := raw.(map[string]interface{})
	if !ok {
		return []int64{}, nil
	}
	return normalizeBitrixIDList(resultMap["CONTACT"]), nil
}

func (c *Client) ContactGet(ctx context.Context, contactID int64) (*Contact, error) {
	raw, _, _, err := c.call(ctx, "crm.contact.get", map[string]interface{}{"id": contactID})
	if err != nil {
		return nil, err
	}
	return parseBitrixContact(raw), nil
}

func (c *Client) ContactAdd(ctx context.Context, fields map[string]interface{}) (int64, error) {
	raw, _, _, err := c.call(ctx, "crm.contact.add", map[string]interface{}{"fields": fields})
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) ContactUpdate(ctx context.Context, contactID int64, fields map[string]interface{}) error {
	_, _, _, err := c.call(ctx, "crm.contact.update", map[string]interface{}{
		"id":     contactID,
		"fields": fields,
	})
	return err
}

func (c *Client) ContactListByPhone(ctx context.Context, phone string) ([]Contact, error) {
	raw, _, _, err := c.call(ctx, "crm.contact.list", map[string]interface{}{
		"filter": map[string]interface{}{
			"PHONE": strings.TrimSpace(phone),
		},
		"order": map[string]interface{}{
			"DATE_MODIFY": "DESC",
			"ID":          "ASC",
		},
		"select": []string{
			"ID",
			"NAME",
			"SECOND_NAME",
			"LAST_NAME",
			"PHONE",
			"EMAIL",
			"IM",
			"COMPANY_ID",
			"DATE_MODIFY",
		},
	})
	if err != nil {
		return nil, err
	}
	return parseBitrixContacts(raw), nil
}

func (c *Client) DealContactItemsGet(ctx context.Context, dealID int64) ([]DealContactBinding, error) {
	raw, _, _, err := c.call(ctx, "crm.deal.contact.items.get", map[string]interface{}{"id": dealID})
	if err != nil {
		return nil, err
	}
	return parseDealContactBindings(raw), nil
}

func (c *Client) DealContactAdd(ctx context.Context, dealID int64, binding DealContactBinding) (bool, error) {
	raw, _, _, err := c.call(ctx, "crm.deal.contact.add", map[string]interface{}{
		"id": dealID,
		"fields": map[string]interface{}{
			"CONTACT_ID": binding.ContactID,
			"IS_PRIMARY": boolToBitrixFlag(binding.IsPrimary),
			"SORT":       binding.Sort,
		},
	})
	if err != nil {
		return false, err
	}
	return parseBitrixBool(raw), nil
}

func (c *Client) DealContactItemsSet(ctx context.Context, dealID int64, items []DealContactBinding) error {
	payload := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if item.ContactID <= 0 {
			continue
		}
		payload = append(payload, map[string]interface{}{
			"CONTACT_ID": item.ContactID,
			"IS_PRIMARY": boolToBitrixFlag(item.IsPrimary),
			"SORT":       item.Sort,
		})
	}
	_, _, _, err := c.call(ctx, "crm.deal.contact.items.set", map[string]interface{}{
		"id":    dealID,
		"items": payload,
	})
	return err
}

func (c *Client) DealContactItemsDelete(ctx context.Context, dealID int64) error {
	_, _, _, err := c.call(ctx, "crm.deal.contact.items.delete", map[string]interface{}{"id": dealID})
	return err
}

func parseBitrixContact(raw interface{}) *Contact {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}

	id := toInt64(m["ID"])
	if id <= 0 {
		return nil
	}

	companyID := toInt64(m["COMPANY_ID"])
	var companyIDPtr *int64
	if companyID > 0 {
		companyIDPtr = &companyID
	}

	return &Contact{
		ID:         id,
		Name:       strings.TrimSpace(toString(m["NAME"])),
		SecondName: strings.TrimSpace(toString(m["SECOND_NAME"])),
		LastName:   strings.TrimSpace(toString(m["LAST_NAME"])),
		CompanyID:  companyIDPtr,
		DateModify: parseBitrixDateTime(m["DATE_MODIFY"]),
		Phones:     parseBitrixContactFields(m["PHONE"]),
		Emails:     parseBitrixContactFields(m["EMAIL"]),
		IMs:        parseBitrixContactFields(m["IM"]),
		Raw:        m,
	}
}

func parseBitrixContacts(raw interface{}) []Contact {
	items, ok := raw.([]interface{})
	if !ok {
		return []Contact{}
	}

	result := make([]Contact, 0, len(items))
	for _, item := range items {
		parsed := parseBitrixContact(item)
		if parsed == nil {
			continue
		}
		result = append(result, *parsed)
	}
	return result
}

func parseBitrixContactFields(raw interface{}) []ContactField {
	items, ok := raw.([]interface{})
	if !ok {
		return []ContactField{}
	}

	result := make([]ContactField, 0, len(items))
	for _, item := range items {
		fieldMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, ContactField{
			ID:        strings.TrimSpace(toString(fieldMap["ID"])),
			TypeID:    strings.TrimSpace(toString(fieldMap["TYPE_ID"])),
			ValueType: strings.TrimSpace(toString(fieldMap["VALUE_TYPE"])),
			Value:     strings.TrimSpace(toString(fieldMap["VALUE"])),
		})
	}
	return result
}

func parseBitrixDateTime(raw interface{}) *time.Time {
	value := strings.TrimSpace(toString(raw))
	if value == "" {
		return nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		return &parsed
	}
	return nil
}

func parseDealContactBindings(raw interface{}) []DealContactBinding {
	items, ok := raw.([]interface{})
	if !ok {
		return []DealContactBinding{}
	}

	result := make([]DealContactBinding, 0, len(items))
	for _, item := range items {
		bindingMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		contactID := toInt64(bindingMap["CONTACT_ID"])
		if contactID <= 0 {
			continue
		}

		result = append(result, DealContactBinding{
			ContactID: contactID,
			IsPrimary: parseBitrixBool(bindingMap["IS_PRIMARY"]),
			Sort:      toInt(bindingMap["SORT"]),
		})
	}
	return result
}

func normalizeBitrixIDList(raw interface{}) []int64 {
	items, ok := raw.([]interface{})
	if !ok {
		return []int64{}
	}

	result := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		id := toInt64(item)
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func parseBitrixBool(raw interface{}) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		return normalized == "y" || normalized == "yes" || normalized == "true" || normalized == "1"
	case int:
		return value != 0
	case int64:
		return value != 0
	case float64:
		return int64(value) != 0
	default:
		return false
	}
}

func boolToBitrixFlag(value bool) string {
	if value {
		return "Y"
	}
	return "N"
}
