package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew_ЧитаетОбщийS3КонфигИзНовыхПеременныхОкружения(t *testing.T) {
	unsetEnv(t,
		"S3_ENDPOINT",
		"S3_REGION",
		"S3_ACCESS_KEY",
		"S3_SECRET_KEY",
		"AGENT_ADAPTER_CATALOG_ENABLED",
		"AGENT_ADAPTER_CATALOG_BUCKET",
		"AGENT_ADAPTER_CATALOG_PUBLIC_BASE_URL",
		"AGENT_ADAPTER_CATALOG_KEY",
		"AGENT_ADAPTER_CATALOG_SYNC_INTERVAL_MIN",
		"AGENT_ADAPTER_CATALOG_DEFAULT_CHANNEL",
		"MEGAFON_VATS_RECORDINGS_ENABLED",
		"MEGAFON_VATS_RECORDINGS_BUCKET",
		"MEGAFON_VATS_RECORDINGS_PUBLIC_BASE_URL",
		"MEGAFON_VATS_RECORDINGS_RETENTION_DAYS",
	)
	t.Setenv("S3_ENDPOINT", "http://s3.local:9000")
	t.Setenv("S3_REGION", "ru-central-1")
	t.Setenv("S3_ACCESS_KEY", "key-new")
	t.Setenv("S3_SECRET_KEY", "secret-new")
	t.Setenv("AGENT_ADAPTER_CATALOG_ENABLED", "true")
	t.Setenv("AGENT_ADAPTER_CATALOG_BUCKET", "agents")
	t.Setenv("AGENT_ADAPTER_CATALOG_PUBLIC_BASE_URL", "https://sd.example.test/agents")
	t.Setenv("AGENT_ADAPTER_CATALOG_KEY", "catalog/index.json")
	t.Setenv("AGENT_ADAPTER_CATALOG_SYNC_INTERVAL_MIN", "7")
	t.Setenv("AGENT_ADAPTER_CATALOG_DEFAULT_CHANNEL", "stable")
	t.Setenv("MEGAFON_VATS_RECORDINGS_ENABLED", "true")
	t.Setenv("MEGAFON_VATS_RECORDINGS_BUCKET", "telephony-recordings")
	t.Setenv("MEGAFON_VATS_RECORDINGS_PUBLIC_BASE_URL", "https://sd.example.test/records")
	t.Setenv("MEGAFON_VATS_RECORDINGS_RETENTION_DAYS", "14")

	cfg := New()

	require.Equal(t, "http://s3.local:9000", cfg.S3.Endpoint)
	require.Equal(t, "ru-central-1", cfg.S3.Region)
	require.Equal(t, "key-new", cfg.S3.AccessKey)
	require.Equal(t, "secret-new", cfg.S3.SecretKey)
	require.True(t, cfg.AgentAdapterCatalog.Enabled)
	require.Equal(t, "agents", cfg.AgentAdapterCatalog.Bucket)
	require.Equal(t, "https://sd.example.test/agents", cfg.AgentAdapterCatalog.PublicBaseURL)
	require.Equal(t, "catalog/index.json", cfg.AgentAdapterCatalog.CatalogKey)
	require.Equal(t, 7*time.Minute, cfg.AgentAdapterCatalog.SyncInterval)
	require.Equal(t, "stable", cfg.AgentAdapterCatalog.DefaultChannel)
	require.True(t, cfg.MegafonVATSRecordings.Enabled)
	require.Equal(t, "telephony-recordings", cfg.MegafonVATSRecordings.Bucket)
	require.Equal(t, "https://sd.example.test/records", cfg.MegafonVATSRecordings.PublicBaseURL)
	require.Equal(t, 14, cfg.MegafonVATSRecordings.RetentionDays)
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		value, exists := os.LookupEnv(key)
		if exists {
			require.NoError(t, os.Unsetenv(key))
		}
		t.Cleanup(func() {
			if exists {
				require.NoError(t, os.Setenv(key, value))
				return
			}
			require.NoError(t, os.Unsetenv(key))
		})
	}
}
