package services

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	b24 "etalon-server/internal/infra/plugins/bitrix"

	"etalon-server/internal/domain/telephony"
)

const (
	bitrixAutoCreatedContactName              = "Неизвестный клиент"
	bitrixAutoCreatedContactSourceDescription = "Создан автоматически из ServiceDesk по входящему звонку"
	bitrixAutoCreatedContactComment           = "Автосоздание контакта по событию телефонии"
)

type BitrixContactService interface {
	EnsureContactByPhone(ctx context.Context, input BitrixEnsureContactInput) (*BitrixEnsureContactResult, error)
}

type BitrixEnsureContactInput struct {
	NormalizedPhone string
	DisplayPhone    string
	Name            string
}

type BitrixEnsureContactResult struct {
	ContactID int64
	Created   bool
	Contact   *b24.Contact
}

func (s *bitrixSyncService) EnsureContactByPhone(ctx context.Context, input BitrixEnsureContactInput) (*BitrixEnsureContactResult, error) {
	if s == nil || s.client == nil || !s.canReadBitrix() {
		return nil, nil
	}

	normalizedPhone := strings.TrimSpace(input.NormalizedPhone)
	if normalizedPhone == "" {
		return nil, nil
	}

	contact, created, err := s.findOrCreateBitrixContact(ctx, normalizedPhone, strings.TrimSpace(input.Name))
	if err != nil {
		return nil, err
	}
	if contact == nil || contact.ID <= 0 {
		return nil, nil
	}

	contact, err = s.fillBitrixContactName(ctx, contact, strings.TrimSpace(input.Name))
	if err != nil {
		return nil, err
	}

	return &BitrixEnsureContactResult{
		ContactID: contact.ID,
		Created:   created,
		Contact:   contact,
	}, nil
}

func (s *bitrixSyncService) ensureBitrixContactForLocalContact(ctx context.Context, contact *telephony.Contact) (*BitrixEnsureContactResult, error) {
	if contact == nil {
		return nil, nil
	}

	preferredName := safeTelephonyContactName(contact.Name)
	displayPhone := strings.TrimSpace(contact.PhoneDisplay)
	if displayPhone == "" {
		displayPhone = strings.TrimSpace(contact.PhoneNormalized)
	}

	if bitrixContactID := parseLocalBitrixContactID(contact.BitrixContactID); bitrixContactID > 0 && s.client != nil {
		existing, err := s.client.ContactGet(ctx, bitrixContactID)
		if err == nil && existing != nil {
			existing, err = s.fillBitrixContactName(ctx, existing, preferredName)
			if err != nil {
				return nil, err
			}
			result := &BitrixEnsureContactResult{
				ContactID: existing.ID,
				Created:   false,
				Contact:   existing,
			}
			if _, syncErr := mergeTelephonyContactWithBitrix(ctx, s.telephonyRepo, contact.PhoneNormalized, displayPhone, preferredName, result); syncErr != nil {
				return nil, syncErr
			}
			return result, nil
		}
	}

	result, err := s.EnsureContactByPhone(ctx, BitrixEnsureContactInput{
		NormalizedPhone: contact.PhoneNormalized,
		DisplayPhone:    displayPhone,
		Name:            preferredName,
	})
	if err != nil {
		return nil, err
	}
	if _, syncErr := mergeTelephonyContactWithBitrix(ctx, s.telephonyRepo, contact.PhoneNormalized, displayPhone, preferredName, result); syncErr != nil {
		return nil, syncErr
	}
	return result, nil
}

func (s *bitrixSyncService) findOrCreateBitrixContact(ctx context.Context, normalizedPhone string, preferredName string) (*b24.Contact, bool, error) {
	contactIDs, err := s.client.DuplicateFindContactsByPhone(ctx, normalizedPhone)
	if err != nil {
		if s.log != nil {
			s.log.Warn("Bitrix24: поиск контакта по crm.duplicate.findbycomm завершился ошибкой, запускается fallback crm.contact.list", "phone", normalizedPhone, "error", err)
		}
		contacts, fallbackErr := s.client.ContactListByPhone(ctx, normalizedPhone)
		if fallbackErr != nil {
			return nil, false, fmt.Errorf("не удалось найти контакт Bitrix24 по телефону %s: %w", normalizedPhone, fallbackErr)
		}
		return s.resolveBitrixContactCandidates(ctx, contacts, normalizedPhone, preferredName)
	}

	if len(contactIDs) == 0 {
		return s.createBitrixContact(ctx, normalizedPhone, preferredName)
	}

	contacts := make([]b24.Contact, 0, len(contactIDs))
	for _, contactID := range contactIDs {
		item, getErr := s.client.ContactGet(ctx, contactID)
		if getErr != nil {
			return nil, false, getErr
		}
		if item == nil {
			continue
		}
		contacts = append(contacts, *item)
	}
	return s.resolveBitrixContactCandidates(ctx, contacts, normalizedPhone, preferredName)
}

func (s *bitrixSyncService) resolveBitrixContactCandidates(ctx context.Context, contacts []b24.Contact, normalizedPhone string, preferredName string) (*b24.Contact, bool, error) {
	if len(contacts) == 0 {
		return s.createBitrixContact(ctx, normalizedPhone, preferredName)
	}
	if len(contacts) == 1 {
		contact := contacts[0]
		return &contact, false, nil
	}

	type candidate struct {
		contact    b24.Contact
		hasCompany bool
		modifiedAt time.Time
	}

	candidates := make([]candidate, 0, len(contacts))
	for _, contact := range contacts {
		current := candidate{
			contact:    contact,
			hasCompany: contact.CompanyID != nil && *contact.CompanyID > 0,
		}
		if contact.DateModify != nil {
			current.modifiedAt = *contact.DateModify
		}
		candidates = append(candidates, current)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].hasCompany != candidates[j].hasCompany {
			return candidates[i].hasCompany
		}
		if !candidates[i].modifiedAt.Equal(candidates[j].modifiedAt) {
			return candidates[i].modifiedAt.After(candidates[j].modifiedAt)
		}
		return candidates[i].contact.ID < candidates[j].contact.ID
	})

	if len(candidates) > 1 &&
		candidates[0].hasCompany == candidates[1].hasCompany &&
		candidates[0].modifiedAt.Equal(candidates[1].modifiedAt) {
		return nil, false, fmt.Errorf("по телефону %s найдено несколько контактов Bitrix24 с неразрешимым конфликтом", normalizedPhone)
	}

	selected := candidates[0].contact
	return &selected, false, nil
}

func (s *bitrixSyncService) createBitrixContact(ctx context.Context, normalizedPhone string, preferredName string) (*b24.Contact, bool, error) {
	name := strings.TrimSpace(preferredName)
	if name == "" {
		name = bitrixAutoCreatedContactName
	}

	contactID, err := s.client.ContactAdd(ctx, map[string]interface{}{
		"NAME": name,
		"PHONE": []map[string]string{
			{
				"VALUE":      normalizedPhone,
				"VALUE_TYPE": "WORK",
			},
		},
		"SOURCE_DESCRIPTION": bitrixAutoCreatedContactSourceDescription,
		"COMMENTS":           bitrixAutoCreatedContactComment,
	})
	if err != nil {
		return nil, false, err
	}

	contact, err := s.client.ContactGet(ctx, contactID)
	if err != nil {
		return nil, false, err
	}
	return contact, true, nil
}

func (s *bitrixSyncService) fillBitrixContactName(ctx context.Context, contact *b24.Contact, preferredName string) (*b24.Contact, error) {
	if s == nil || s.client == nil || contact == nil || contact.ID <= 0 {
		return contact, nil
	}

	preferredName = strings.TrimSpace(preferredName)
	if preferredName == "" || !shouldBackfillBitrixContactName(contact) {
		return contact, nil
	}

	if err := s.client.ContactUpdate(ctx, contact.ID, map[string]interface{}{"NAME": preferredName}); err != nil {
		return nil, err
	}
	return s.client.ContactGet(ctx, contact.ID)
}

func mergeTelephonyContactWithBitrix(
	ctx context.Context,
	repo telephony.Repository,
	normalizedPhone string,
	displayPhone string,
	preferredName string,
	result *BitrixEnsureContactResult,
) (*telephony.Contact, error) {
	if repo == nil {
		return nil, nil
	}

	normalizedPhone = strings.TrimSpace(normalizedPhone)
	if normalizedPhone == "" {
		return nil, nil
	}

	displayPhone = strings.TrimSpace(displayPhone)
	if displayPhone == "" {
		displayPhone = normalizedPhone
	}

	upsert := telephony.ContactUpsert{
		PhoneNormalized: normalizedPhone,
		PhoneDisplay:    displayPhone,
	}

	name := strings.TrimSpace(preferredName)
	if result != nil {
		if result.ContactID > 0 {
			bitrixContactID := strconv.FormatInt(result.ContactID, 10)
			upsert.BitrixContactID = &bitrixContactID
		}
		if result.Contact != nil {
			if contactPhone := pickBitrixContactPhone(result.Contact); contactPhone != "" {
				upsert.PhoneDisplay = contactPhone
			}
			if name == "" {
				if contactName := buildBitrixContactDisplayName(result.Contact); contactName != "" {
					name = contactName
				}
			}
		}
	}
	if name != "" {
		upsert.Name = &name
	}

	return repo.UpsertContact(ctx, upsert)
}

func buildBitrixContactDisplayName(contact *b24.Contact) string {
	if contact == nil {
		return ""
	}

	parts := make([]string, 0, 3)
	for _, value := range []string{contact.Name, contact.SecondName, contact.LastName} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func pickBitrixContactPhone(contact *b24.Contact) string {
	if contact == nil || len(contact.Phones) == 0 {
		return ""
	}
	for _, phone := range contact.Phones {
		value := strings.TrimSpace(phone.Value)
		if value != "" {
			return value
		}
	}
	return ""
}

func shouldBackfillBitrixContactName(contact *b24.Contact) bool {
	if contact == nil {
		return false
	}
	if strings.TrimSpace(contact.LastName) != "" || strings.TrimSpace(contact.SecondName) != "" {
		return false
	}

	name := strings.TrimSpace(contact.Name)
	return name == "" || strings.EqualFold(name, bitrixAutoCreatedContactName)
}

func parseLocalBitrixContactID(value *string) int64 {
	if value == nil {
		return 0
	}
	parsed, _ := strconv.ParseInt(strings.TrimSpace(*value), 10, 64)
	return parsed
}

func safeTelephonyContactName(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
