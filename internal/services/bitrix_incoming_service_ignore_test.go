package services

import (
	"context"
	"fmt"
	"testing"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBitrixIncomingService_HandleDealAddOrUpdate_IgnoresManuallyUnlinkedDeal(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&bitrix.IgnoredDeal{}); err != nil {
		t.Fatalf("не удалось подготовить схему bitrix: %v", err)
	}

	repo := repositories.NewBitrixRepo(db)
	ctx := context.Background()
	if err := repo.UpsertIgnoredDeal(ctx, &bitrix.IgnoredDeal{
		B24DealID: 6299,
		TicketID:  "ticket-ignored-1",
	}); err != nil {
		t.Fatalf("не удалось создать ignored deal: %v", err)
	}

	svc := &bitrixIncomingService{
		cfg:  &config.Config{},
		log:  logger.New("", "test", "error", true),
		repo: repo,
	}

	status, reason, err := svc.handleDealAddOrUpdate(ctx, 6299)
	if err != nil {
		t.Fatalf("handleDealAddOrUpdate вернул ошибку: %v", err)
	}
	if status != bitrix.IncomingEventStatusIgnored {
		t.Fatalf("ожидали статус ignored, получили %q", status)
	}
	if reason == "" {
		t.Fatalf("ожидали непустую причину игнорирования")
	}
}
