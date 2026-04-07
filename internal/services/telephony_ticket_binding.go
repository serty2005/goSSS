package services

import (
	"context"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"strings"
	"time"
)

var telephonyAutoBindExcludedTicketStatuses = []string{
	tickets.StatusResolved,
	tickets.StatusClosed,
	tickets.StatusSpam,
	tickets.StatusExecution,
}

func autoBindTelephonyCallToActiveTicket(
	ctx context.Context,
	telephonyRepo telephony.Repository,
	ticketRepo tickets.TicketRepository,
	call *telephony.Call,
	contact *telephony.Contact,
) (*tickets.Ticket, error) {
	if telephonyRepo == nil || ticketRepo == nil || call == nil || contact == nil || contact.ID == 0 {
		return nil, nil
	}

	existingLink, err := telephonyRepo.GetCallTicketLink(ctx, call.ID)
	if err != nil {
		return nil, err
	}
	if existingLink != nil {
		ticket, err := ticketRepo.GetByID(ctx, existingLink.TicketID)
		if err != nil || ticket == nil {
			return ticket, err
		}
		if strings.TrimSpace(ticket.CompanyID) != "" {
			if err = telephonyRepo.UpsertContactCompanyLink(ctx, contact.ID, ticket.CompanyID, time.Now()); err != nil {
				return nil, err
			}
		}
		return ticket, nil
	}

	contactID := contact.ID
	items, err := ticketRepo.Find(ctx, tickets.TicketFilter{
		ContactID:       &contactID,
		ExcludeStatuses: telephonyAutoBindExcludedTicketStatuses,
		Limit:           1,
	})
	if err != nil || len(items) == 0 {
		return nil, err
	}

	ticket := items[0]
	if err = telephonyRepo.UpsertCallTicketLink(ctx, &telephony.CallTicketLink{
		TelephonyCallID: call.ID,
		TicketID:        ticket.ID,
	}); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ticket.CompanyID) != "" {
		if err = telephonyRepo.UpsertContactCompanyLink(ctx, contact.ID, ticket.CompanyID, time.Now()); err != nil {
			return nil, err
		}
	}
	return &ticket, nil
}
