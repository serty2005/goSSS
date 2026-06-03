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

func TestNew_UsesDefaultArchiveLimitWhenLegacyZipLimitIsSmaller(t *testing.T) {
	t.Setenv("CONTRACT_REPORT_ARCHIVE_MAX_BYTES", "")
	t.Setenv("CONTRACT_ZIP_MAX_BYTES", "102400")

	cfg := New()
	if cfg.ContractReportArchiveMaxBytes != DefaultContractReportArchiveMaxBytes {
		t.Fatalf("ожидали лимит архива %d байт, получили %d", DefaultContractReportArchiveMaxBytes, cfg.ContractReportArchiveMaxBytes)
	}
}

func TestNew_UsesExplicitContractReportLimits(t *testing.T) {
	t.Setenv("CONTRACT_REPORT_ARCHIVE_MAX_BYTES", "6291456")
	t.Setenv("CONTRACT_REPORT_TABLE_MAX_BYTES", "25165824")
	t.Setenv("CONTRACT_ZIP_MAX_BYTES", "102400")

	cfg := New()
	if cfg.ContractReportArchiveMaxBytes != 6291456 {
		t.Fatalf("ожидали явный лимит архива 6291456 байт, получили %d", cfg.ContractReportArchiveMaxBytes)
	}
	if cfg.ContractReportTableMaxBytes != 25165824 {
		t.Fatalf("ожидали явный лимит таблицы 25165824 байт, получили %d", cfg.ContractReportTableMaxBytes)
	}
}
