package services

import (
	"strings"
	"testing"

	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"

	"github.com/stretchr/testify/require"
)

func TestBuildDealDescription_AddsServicePrefixOnlyOnce(t *testing.T) {
	svc := &bitrixSyncService{
		cfg: &config.Config{EtalonTicketBaseURL: "http://etalon.serty.top"},
	}

	ticketID := "0910fb2f-37a5-4d06-a1eb-c4b3b8d14203"
	ticket := &tickets.Ticket{
		Number:      1,
		Subject:     "тест сделка",
		Description: "Тикет Etalon #1\nтест сделка\nhttp://etalon.serty.top/tickets/" + ticketID + "\n\nТикет Etalon #1\nЗадача №5081\nhttp://etalon.serty.top/tickets/" + ticketID + "\n\nЕще одно редактирование описания",
	}
	ticket.ID = ticketID

	out := svc.buildDealDescription(ticket)

	require.Equal(t, 1, strings.Count(out, "Тикет Etalon #1"))
	require.Equal(t, 1, strings.Count(out, "http://etalon.serty.top/tickets/"+ticketID))
	require.Contains(t, out, "Еще одно редактирование описания")
	require.NotContains(t, out, "Задача №5081")
}
