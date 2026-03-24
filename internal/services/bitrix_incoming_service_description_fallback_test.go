package services

import (
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"testing"
)

func TestExtractIncomingDealDescription_FallbackToComments(t *testing.T) {
	deal := &b24.Deal{
		Raw: map[string]interface{}{
			"UF_CRM_1766060620": "",
			"COMMENTS":          "Описание из COMMENTS",
		},
	}

	got := extractIncomingDealDescription(deal)
	if got == "" {
		t.Fatalf("ожидали fallback описания из COMMENTS")
	}
	if got != "Описание из COMMENTS" {
		t.Fatalf("ожидали описание из COMMENTS, получили %q", got)
	}
}

func TestExtractIncomingDealDescription_StripsServiceLinkPrefixFromComments(t *testing.T) {
	deal := &b24.Deal{
		OriginID: "c9d7917a-3c16-4966-b8f1-63e2a73efe48",
		Raw: map[string]interface{}{
			"UF_CRM_1766060620": "",
			"COMMENTS": "[p]\n" +
				"[url=https://sd.myhoreca.io/tickets/c9d7917a-3c16-4966-b8f1-63e2a73efe48]Тикет в XD #68617[/url]\n" +
				"[/p]\n" +
				"Описание из COMMENTS",
		},
	}

	got := extractIncomingDealDescription(deal)
	if got != "Описание из COMMENTS" {
		t.Fatalf("ожидали описание без служебной ссылки, получили %q", got)
	}
}
