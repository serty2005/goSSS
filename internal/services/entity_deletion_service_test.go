package services

import (
	"context"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/common"
	domainCompany "etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	dbpkg "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestRequestDeletion_AutoResolvesDuplicateWorkerCandidate(t *testing.T) {
	svc, db, serverRepo := newEntityDeletionServiceTestFixture(t)
	ctx := context.Background()

	survivor := createTestServer(t, db, "server-survivor", "10.10.10.10", time.Now().Add(2*time.Hour))
	loser := createTestServer(t, db, "server-loser", "10.10.10.10", time.Now().Add(time.Hour))

	duplicateCandidate := createTestCandidate(t, db, models.EntityDeletionCandidate{
		EntityType:          "Server",
		EntityID:            loser.ID,
		Status:              models.EntityDeletionCandidateStatusPending,
		Reason:              edsStringPtrOrNil("Дубль сервера"),
		Source:              models.EntityDeletionSourceDuplicateWorker,
		RequestedAt:         time.Now(),
		DuplicateOfEntityID: edsStringPtrOrNil(survivor.ID),
		DuplicateField:      edsStringPtrOrNil("ip"),
		DuplicateValue:      edsStringPtrOrNil("10.10.10.10"),
		Meta:                datatypes.JSON([]byte(`{"duplicate_entity_ids":["server-loser"],"survivor_id":"server-survivor","loser_id":"server-loser"}`)),
	})

	result, err := svc.RequestDeletion(ctx, EntityDeletionRequest{
		EntityType: "Server",
		EntityID:   loser.ID,
		Reason:     "Ручное удаление из карточки сущности",
		Source:     models.EntityDeletionSourceManual,
	})
	if err != nil {
		t.Fatalf("ожидалось авторазрешение duplicate-worker кандидата, ошибка: %v", err)
	}
	if result == nil || result.ID != duplicateCandidate.ID {
		t.Fatalf("ожидали возврат существующего duplicate-worker кандидата, получили %#v", result)
	}
	if result.Status != models.EntityDeletionCandidateStatusConfirmed {
		t.Fatalf("ожидали подтверждённый статус кандидата, получили %q", result.Status)
	}

	item, err := serverRepo.GetByID(ctx, loser.ID)
	if err == nil || item != nil {
		t.Fatalf("ожидали, что сервер %s будет удалён, получили item=%#v err=%v", loser.ID, item, err)
	}
}

func TestTryAutoMergeDuplicateGroup_ConfirmsManualCandidate(t *testing.T) {
	svc, db, serverRepo := newEntityDeletionServiceTestFixture(t)
	ctx := context.Background()

	survivor := createTestServer(t, db, "server-newer", "10.20.30.40", time.Now().Add(2*time.Hour))
	loser := createTestServer(t, db, "server-older", "10.20.30.40", time.Now().Add(time.Hour))

	manualCandidate, err := svc.RequestDeletion(ctx, EntityDeletionRequest{
		EntityType: "Server",
		EntityID:   loser.ID,
		Reason:     "Ручное удаление из карточки сущности",
		Source:     models.EntityDeletionSourceManual,
	})
	if err != nil {
		t.Fatalf("не удалось создать ручного кандидата: %v", err)
	}

	handled, err := svc.TryAutoMergeDuplicateGroup(ctx, "Server", "ip", "10.20.30.40", []string{loser.ID, survivor.ID})
	if err != nil {
		t.Fatalf("автосклейка дублей завершилась ошибкой: %v", err)
	}
	if !handled {
		t.Fatalf("ожидали, что группа дублей будет обработана")
	}

	var refreshed models.EntityDeletionCandidate
	if err := db.Where("id = ?", manualCandidate.ID).First(&refreshed).Error; err != nil {
		t.Fatalf("не удалось перечитать кандидата: %v", err)
	}
	if refreshed.Status != models.EntityDeletionCandidateStatusConfirmed {
		t.Fatalf("ожидали, что ручной кандидат будет автоматически подтверждён, получили %q", refreshed.Status)
	}
	if derefString(refreshed.DuplicateOfEntityID) != survivor.ID {
		t.Fatalf("ожидали, что survivor будет сохранён в кандидате, получили %q", derefString(refreshed.DuplicateOfEntityID))
	}

	item, err := serverRepo.GetByID(ctx, loser.ID)
	if err == nil || item != nil {
		t.Fatalf("ожидали, что loser %s будет удалён, получили item=%#v err=%v", loser.ID, item, err)
	}
}

func TestConfirmDeletion_CleansRelatedPendingCandidates(t *testing.T) {
	svc, db, serverRepo := newEntityDeletionServiceTestFixture(t)
	ctx := context.Background()

	survivor := createTestServer(t, db, "server-survivor-clean", "10.50.60.70", time.Now().Add(2*time.Hour))
	loser := createTestServer(t, db, "server-loser-clean", "10.50.60.70", time.Now().Add(time.Hour))

	duplicateCandidate := createTestCandidate(t, db, models.EntityDeletionCandidate{
		EntityType:          "Server",
		EntityID:            loser.ID,
		Status:              models.EntityDeletionCandidateStatusPending,
		Reason:              edsStringPtrOrNil("Дубль"),
		Source:              models.EntityDeletionSourceDuplicateWorker,
		RequestedAt:         time.Now(),
		DuplicateOfEntityID: edsStringPtrOrNil(survivor.ID),
		DuplicateField:      edsStringPtrOrNil("ip"),
		DuplicateValue:      edsStringPtrOrNil("10.50.60.70"),
		Meta:                datatypes.JSON([]byte(`{"duplicate_entity_ids":["server-loser-clean"],"survivor_id":"server-survivor-clean","loser_id":"server-loser-clean"}`)),
	})
	manualCandidate := createTestCandidate(t, db, models.EntityDeletionCandidate{
		EntityType:        "Server",
		EntityID:          loser.ID,
		Status:            models.EntityDeletionCandidateStatusPending,
		Reason:            edsStringPtrOrNil("Ручное удаление"),
		Source:            models.EntityDeletionSourceManual,
		RequestedByUserID: edsStringPtrOrNil("admin-1"),
		RequestedAt:       time.Now(),
		Meta:              datatypes.JSON([]byte(`{}`)),
	})

	if _, err := svc.ConfirmDeletion(ctx, duplicateCandidate.ID); err != nil {
		t.Fatalf("не удалось подтвердить duplicate-worker кандидата: %v", err)
	}

	var refreshedManual models.EntityDeletionCandidate
	if err := db.Where("id = ?", manualCandidate.ID).First(&refreshedManual).Error; err != nil {
		t.Fatalf("не удалось перечитать ручного кандидата: %v", err)
	}
	if refreshedManual.Status != models.EntityDeletionCandidateStatusConfirmed {
		t.Fatalf("ожидали, что ручной кандидат будет автоматически закрыт после физического удаления, получили %q", refreshedManual.Status)
	}

	item, err := serverRepo.GetByID(ctx, loser.ID)
	if err == nil || item != nil {
		t.Fatalf("ожидали, что loser %s будет удалён, получили item=%#v err=%v", loser.ID, item, err)
	}
}

func TestCleanupStalePendingCandidates_ConfirmsAlreadyDeletedEntries(t *testing.T) {
	svc, db, serverRepo := newEntityDeletionServiceTestFixture(t)
	ctx := context.Background()

	target := createTestServer(t, db, "server-stale", "10.70.80.90", time.Now())
	candidate := createTestCandidate(t, db, models.EntityDeletionCandidate{
		EntityType:        "Server",
		EntityID:          target.ID,
		Status:            models.EntityDeletionCandidateStatusPending,
		Reason:            edsStringPtrOrNil("Ручное удаление"),
		Source:            models.EntityDeletionSourceManual,
		RequestedByUserID: edsStringPtrOrNil("admin-1"),
		RequestedAt:       time.Now(),
		Meta:              datatypes.JSON([]byte(`{}`)),
	})

	if _, err := serverRepo.Delete(ctx, nil, target.ID); err != nil {
		t.Fatalf("не удалось физически удалить тестовую сущность: %v", err)
	}

	cleaned, err := svc.CleanupStalePendingCandidates(ctx)
	if err != nil {
		t.Fatalf("очистка устаревших кандидатов завершилась ошибкой: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("ожидали очистку одного кандидата, получили %d", cleaned)
	}

	var refreshed models.EntityDeletionCandidate
	if err := db.Where("id = ?", candidate.ID).First(&refreshed).Error; err != nil {
		t.Fatalf("не удалось перечитать кандидата: %v", err)
	}
	if refreshed.Status != models.EntityDeletionCandidateStatusConfirmed {
		t.Fatalf("ожидали подтверждение устаревшего кандидата, получили %q", refreshed.Status)
	}
}

func TestConfirmDeletion_RemovesCompanyBitrixMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:TestConfirmDeletionRemovesCompanyBitrixMapping?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(
		&domainCompany.Company{},
		&server.Server{},
		&workstation.Workstation{},
		&fiscal.FiscalRegister{},
		&contract.Contract{},
		&models.CompanyContract{},
		&bitrix.CompanyServicePointMapping{},
		&models.EntityDeletionCandidate{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := t.Context()
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	title := "Компания с mapping Bitrix24"
	comp := &domainCompany.Company{Title: &title}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            comp.ID,
		BitrixServicePointID: 11059,
	}); err != nil {
		t.Fatalf("не удалось создать mapping компании: %v", err)
	}

	svc := NewEntityDeletionService(
		nil,
		db,
		dbpkg.NewGormTransactor(db),
		nil,
		nil,
		nil,
		companyRepo,
		contractRepo,
		nil,
	)
	candidate := createTestCandidate(t, db, models.EntityDeletionCandidate{
		EntityType:  "Company",
		EntityID:    comp.ID,
		Status:      models.EntityDeletionCandidateStatusPending,
		Reason:      edsStringPtrOrNil("Ручное удаление компании"),
		Source:      models.EntityDeletionSourceManual,
		RequestedAt: time.Now(),
	})

	if _, err := svc.ConfirmDeletion(ctx, candidate.ID); err != nil {
		t.Fatalf("подтверждение удаления компании завершилось ошибкой: %v", err)
	}

	mapping, err := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, comp.ID)
	if err != nil {
		t.Fatalf("не удалось проверить mapping компании: %v", err)
	}
	if mapping != nil {
		t.Fatalf("ожидали удаление mapping компании, получили %+v", mapping)
	}
}

func newEntityDeletionServiceTestFixture(t *testing.T) (*entityDeletionServiceImpl, *gorm.DB, interface {
	GetByID(ctx context.Context, internalID string) (*server.Server, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)
}) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(
		&server.Server{},
		&workstation.Workstation{},
		&models.EntityDeletionCandidate{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	serverRepo := repositories.NewServerRepo(db)
	svc := NewEntityDeletionService(
		nil,
		db,
		dbpkg.NewGormTransactor(db),
		serverRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	impl, ok := svc.(*entityDeletionServiceImpl)
	if !ok {
		t.Fatalf("ожидали concrete implementation entityDeletionServiceImpl")
	}
	return impl, db, serverRepo
}

func createTestServer(t *testing.T, db *gorm.DB, id string, ip string, updatedAt time.Time) *server.Server {
	t.Helper()

	deviceName := id
	ipValue := ip
	item := &server.Server{
		Base: common.Base{
			ID: id,
		},
		DeviceName: &deviceName,
		IP:         &ipValue,
	}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("не удалось создать сервер %s: %v", id, err)
	}
	if err := db.Model(&server.Server{}).Where("id = ?", id).Update("updated_at", updatedAt).Error; err != nil {
		t.Fatalf("не удалось обновить updated_at для %s: %v", id, err)
	}
	item.UpdatedAt = updatedAt
	return item
}

func createTestCandidate(t *testing.T, db *gorm.DB, candidate models.EntityDeletionCandidate) *models.EntityDeletionCandidate {
	t.Helper()

	if len(candidate.Meta) == 0 {
		candidate.Meta = datatypes.JSON([]byte(`{}`))
	}
	if candidate.RequestedAt.IsZero() {
		candidate.RequestedAt = time.Now()
	}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatalf("не удалось создать кандидата на удаление: %v", err)
	}
	return &candidate
}
