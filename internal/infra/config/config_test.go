package config

import (
	"testing"
	"time"
)

func TestNew_UsesContractSyncIntervalMinutesWithoutHourConversion(t *testing.T) {
	t.Setenv("CONTRACT_SYNC_INTERVAL_MIN", "90")

	cfg := New()
	if cfg.ContractSyncInterval != 90*time.Minute {
		t.Fatalf("ожидали интервал 90 минут, получили %s", cfg.ContractSyncInterval)
	}
}

func TestNew_UsesTwelveHoursAsDefaultContractSyncInterval(t *testing.T) {
	t.Setenv("CONTRACT_SYNC_INTERVAL_MIN", "")

	cfg := New()
	if cfg.ContractSyncInterval != 12*time.Hour {
		t.Fatalf("ожидали интервал 12 часов по умолчанию, получили %s", cfg.ContractSyncInterval)
	}
}
