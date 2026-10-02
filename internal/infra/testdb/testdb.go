// Package testdb открывает изолированную схему реального PostgreSQL для интеграционных тестов.
package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// EnvDSN - переменная окружения с DSN тестового PostgreSQL, например postgres://postgres:test@localhost:54329/gosss_test?sslmode=disable.
const EnvDSN = "TEST_POSTGRES_DSN"

// PostgresDSN возвращает DSN тестового PostgreSQL или пустую строку, если он не задан.
func PostgresDSN() string {
	return os.Getenv(EnvDSN)
}

// OpenPostgres создает отдельную схему в PostgreSQL из TEST_POSTGRES_DSN, выполняет миграцию моделей и удаляет схему после теста.
func OpenPostgres(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	dsn := PostgresDSN()
	if dsn == "" {
		t.Skipf("не задана переменная %s", EnvDSN)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось подключиться к PostgreSQL: %v", err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("не удалось сгенерировать имя схемы: %v", err)
	}
	schema := "t_" + hex.EncodeToString(suffix)
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("не удалось создать схему: %v", err)
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("некорректный %s: %v", EnvDSN, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось подключиться к схеме %s: %v", schema, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec(fmt.Sprintf(`DROP SCHEMA "%s" CASCADE`, schema)).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("не удалось выполнить миграцию: %v", err)
	}
	return db
}
