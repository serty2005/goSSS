package services

import (
	"strings"
	"testing"

	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/workstation"
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

	require.Equal(t, 0, strings.Count(out, "Тикет Etalon #1"))
	require.Equal(t, 0, strings.Count(out, "http://etalon.serty.top/tickets/"+ticketID))
	require.Contains(t, out, "Еще одно редактирование описания")
	require.NotContains(t, out, "Задача №5081")
}

func TestBuildDealComment_UsesXDLinkTextAndBitrixBBCode(t *testing.T) {
	svc := &bitrixSyncService{
		cfg: &config.Config{EtalonTicketBaseURL: "http://etalon.serty.top"},
	}

	ticketID := "98ed6bab-593e-4fb6-a014-6c19914d69f0"
	ticket := &tickets.Ticket{
		Number: 67569,
	}
	ticket.ID = ticketID

	out := svc.buildDealComment(ticket)

	require.Equal(
		t,
		"[p]\n[url=http://etalon.serty.top/tickets/"+ticketID+"]Тикет в XD #67569[/url]\n[/p]",
		out,
	)
}

func TestBuildDealConnections_SortPriorityAndFormat(t *testing.T) {
	companyName := "Хачапури Марико, СПБ Невский"
	mainName := "ГК POS-01"
	vtkName := "ВтК POS-02"
	otherName := "Касса POS-03"

	srvDeviceName := "iikoRMS ХТМ, Невский пр-кт"
	srvUID := "419-126-780"
	srvCabinet := "8411363"
	srvIP := "https://hachapuri-mariko-nevskii-pr-kt.iiko.it/resto/"
	srvVersion := "9.2.7014"
	srvTV := "111222333"

	wsMainAD := "1013042693"
	wsVtkLM := "MH_12345"
	wsOtherTV := "987654321"

	srv := &server.Server{
		DeviceName:    &srvDeviceName,
		UniqueID:      &srvUID,
		CabinetLink:   &srvCabinet,
		IP:            &srvIP,
		ServerVersion: &srvVersion,
		Teamviewer:    &srvTV,
	}
	wsMain := workstation.Workstation{DeviceName: &mainName, Anydesk: &wsMainAD}
	wsVtk := workstation.Workstation{DeviceName: &vtkName, Litemanager: &wsVtkLM}
	wsOther := workstation.Workstation{DeviceName: &otherName, Teamviewer: &wsOtherTV}

	items := []string{
		formatServerConnectionBlock(companyName, srv),
		formatWorkstationConnectionBlock(wsOther, collectRemoteConnectionIDs(wsOther.Teamviewer, wsOther.Anydesk, wsOther.Litemanager, nil)),
		formatWorkstationConnectionBlock(wsMain, collectRemoteConnectionIDs(wsMain.Teamviewer, wsMain.Anydesk, wsMain.Litemanager, nil)),
		formatWorkstationConnectionBlock(wsVtk, collectRemoteConnectionIDs(wsVtk.Teamviewer, wsVtk.Anydesk, wsVtk.Litemanager, nil)),
	}

	require.Contains(t, items[0], "Link to partner account: https://pp.iiko.ru/ru/cabinet/client-area/index.html?clientId=8411363")
	require.Contains(t, items[0], "1. UID: 419-126-780")
	require.NotContains(t, items[0], "Сервер")
	require.NotContains(t, items[0], "Device name:")
	require.NotContains(t, items[0], "iikoCloud")

	workstations := []workstation.Workstation{wsOther, wsMain, wsVtk}
	sortWorkstationsByPriority(workstations)
	require.Equal(t, mainName, ptrString(workstations[0].DeviceName))
	require.Equal(t, vtkName, ptrString(workstations[1].DeviceName))
	require.Equal(t, otherName, ptrString(workstations[2].DeviceName))
}
