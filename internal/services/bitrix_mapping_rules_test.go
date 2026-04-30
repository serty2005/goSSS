package services

import (
	"context"
	"testing"

	"etalon-server/internal/infra/config"
)

func TestIsBitrixTemporaryServicePoint_UsesConfiguredTestPointID(t *testing.T) {
	cfg := &config.Config{BitrixTestServicePointID: 123022}

	ok, err := isBitrixTemporaryServicePoint(context.Background(), cfg, nil, 123022)
	if err != nil {
		t.Fatalf("isBitrixTemporaryServicePoint вернул ошибку: %v", err)
	}
	if !ok {
		t.Fatal("ожидали, что точка из конфига считается тестовой")
	}

	ok, err = isBitrixTemporaryServicePoint(context.Background(), cfg, nil, 123023)
	if err != nil {
		t.Fatalf("isBitrixTemporaryServicePoint вернул ошибку для другой точки: %v", err)
	}
	if ok {
		t.Fatal("не ожидали, что другая точка считается тестовой без данных из репозитория")
	}
}
