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
