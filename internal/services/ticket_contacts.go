package services

import (
	"context"
	"strings"

	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
)

func saveTicketPhoneContact(
	ctx context.Context,
	ticketRepo tickets.TicketRepository,
	ticket *tickets.Ticket,
	contact *telephony.Contact,
	contactName string,
	source string,
	isPrimary bool,
) error {
	if ticketRepo == nil || ticket == nil || contact == nil {
		return nil
	}
	value := strings.TrimSpace(contact.PhoneNormalized)
	if value == "" {
		return nil
	}
	display := strings.TrimSpace(contact.PhoneDisplay)
	if display == "" {
		display = value
	}
	name := strings.TrimSpace(contactName)
	if name == "" && contact.Name != nil {
		name = strings.TrimSpace(*contact.Name)
	}
	contactID := contact.ID
	if _, err := ticketRepo.UpsertTicketContact(ctx, tickets.TicketContactUpsertInput{
		TicketID:           ticket.ID,
		ContactType:        tickets.ManagerTransferContactPhone,
		TelephonyContactID: &contactID,
		Value:              value,
		DisplayValue:       display,
		Name:               name,
		IsPrimary:          isPrimary,
		Source:             source,
	}); err != nil {
		return err
	}
	return syncTicketPrimaryContactID(ctx, ticketRepo, ticket)
}

func saveTicketTelegramContact(
	ctx context.Context,
	ticketRepo tickets.TicketRepository,
	ticket *tickets.Ticket,
	login string,
	contactName string,
	source string,
	isPrimary bool,
) error {
	if ticketRepo == nil || ticket == nil {
		return nil
	}
	login = strings.TrimSpace(login)
	if login == "" {
		return nil
	}
	if _, err := ticketRepo.UpsertTicketContact(ctx, tickets.TicketContactUpsertInput{
		TicketID:     ticket.ID,
		ContactType:  tickets.ManagerTransferContactTelegram,
		Value:        login,
		DisplayValue: login,
		Name:         strings.TrimSpace(contactName),
		IsPrimary:    isPrimary,
		Source:       source,
	}); err != nil {
		return err
	}
	return syncTicketPrimaryContactID(ctx, ticketRepo, ticket)
}

func syncTicketPrimaryContactID(ctx context.Context, ticketRepo tickets.TicketRepository, ticket *tickets.Ticket) error {
	if ticketRepo == nil || ticket == nil {
		return nil
	}
	contacts, err := ticketRepo.ListTicketContacts(ctx, ticket.ID)
	if err != nil {
		return err
	}

	var nextContactID *uint
	for i := range contacts {
		item := contacts[i]
		if !item.IsPrimary {
			continue
		}
		if item.ContactType == tickets.ManagerTransferContactPhone && item.TelephonyContactID != nil {
			id := *item.TelephonyContactID
			nextContactID = &id
		}
		break
	}

	if (ticket.ContactID == nil && nextContactID == nil) ||
		(ticket.ContactID != nil && nextContactID != nil && *ticket.ContactID == *nextContactID) {
		return nil
	}
	ticket.ContactID = nextContactID
	return ticketRepo.Update(ctx, ticket)
}
