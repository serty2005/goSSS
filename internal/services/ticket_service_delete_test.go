package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUnlinkFromBitrix_RemovesBindingsAndMarksDealIgnored(t *testing.T) {
	ctx := context.Background()
	db := openTicketServiceDeleteTestDB(t)

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	admin := createAdminUserForTicketDeleteTests(t, ctx, userRepo)
	companyID := createCompanyForTicketDeleteTests(t, ctx, companyRepo)

	pointID := int64(16961)
	ticket := &tickets.Ticket{
		Subject:              "Тикет для unlink",
		Description:          "Описание",
		Status:               tickets.StatusNew,
		Priority:             tickets.PriorityMedium,
		Type:                 tickets.TypeIncident,
		CompanyID:            companyID,
		ServiceDeskUUID:      "b24:deal:6299",
		SyncWithBitrix:       true,
		BitrixServicePointID: &pointID,
		BitrixDealTitle:      "Сделка 6299",
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if err := bitrixRepo.UpsertDealLink(ctx, &bitrix.DealLink{
		TicketID:   ticket.ID,
		B24DealID:  6299,
		LastSyncAt: time.Now(),
	}); err != nil {
		t.Fatalf("не удалось создать deal_link: %v", err)
	}
	if err := bitrixRepo.UpsertCommentLink(ctx, &bitrix.CommentLink{
		EtalonCommentID: "comment-1",
		B24CommentID:    551,
		TicketID:        ticket.ID,
		Direction:       "b24_to_etalon",
	}); err != nil {
		t.Fatalf("не удалось создать comment_link: %v", err)
	}

	svc := NewTicketService(
		logger.New("", "test", "error", true),
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		&config.Config{CommonContractID: "common-contract", TicketStoragePath: t.TempDir()},
		nil,
		nil,
		nil,
		bitrixRepo,
		nil,
		nil,
		nil,
		nil,
	)

	updated, err := svc.UnlinkFromBitrix(ctx, ticket.ID, admin.ID, []string{user.RoleAdmin})
	if err != nil {
		t.Fatalf("UnlinkFromBitrix вернул ошибку: %v", err)
	}
	if updated.SyncWithBitrix {
		t.Fatalf("ожидали отключенную синхронизацию с Bitrix24")
	}
	if updated.BitrixServicePointID != nil {
		t.Fatalf("ожидали очистку bitrix_service_point_id, получили %v", *updated.BitrixServicePointID)
	}
	if updated.BitrixDealTitle != "" {
		t.Fatalf("ожидали очистку bitrix_deal_title, получили %q", updated.BitrixDealTitle)
	}
	if updated.ServiceDeskUUID != "" {
		t.Fatalf("ожидали очистку service_desk_uuid для тикета Bitrix24, получили %q", updated.ServiceDeskUUID)
	}

	link, err := bitrixRepo.GetDealLinkByTicketID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось проверить deal_link: %v", err)
	}
	if link != nil {
		t.Fatalf("ожидали удаление deal_link, получили %+v", link)
	}
	commentLink, err := bitrixRepo.GetCommentLinkByB24ID(ctx, 551)
	if err != nil {
		t.Fatalf("не удалось проверить comment_link: %v", err)
	}
	if commentLink != nil {
		t.Fatalf("ожидали удаление comment_link, получили %+v", commentLink)
	}
	ignored, err := bitrixRepo.HasIgnoredDeal(ctx, 6299)
	if err != nil {
		t.Fatalf("не удалось проверить ignored deal: %v", err)
	}
	if !ignored {
		t.Fatalf("ожидали ignored deal для сделки 6299")
	}

	history, err := ticketRepo.GetHistory(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось получить историю: %v", err)
	}
	if len(history) == 0 {
		t.Fatalf("ожидали запись в истории о разрыве связи")
	}
	if history[0].Field != tickets.HistoryFieldBitrixLink {
		t.Fatalf("ожидали поле истории %q, получили %q", tickets.HistoryFieldBitrixLink, history[0].Field)
	}
}

func TestDelete_RemovesTicketAndLocalFiles(t *testing.T) {
	ctx := context.Background()
	db := openTicketServiceDeleteTestDB(t)

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	admin := createAdminUserForTicketDeleteTests(t, ctx, userRepo)
	companyID := createCompanyForTicketDeleteTests(t, ctx, companyRepo)
	storageRoot := t.TempDir()

	pointID := int64(17001)
	ticket := &tickets.Ticket{
		Subject:              "Тикет для удаления",
		Description:          "Описание",
		Status:               tickets.StatusNew,
		Priority:             tickets.PriorityMedium,
		Type:                 tickets.TypeIncident,
		CompanyID:            companyID,
		ServiceDeskUUID:      "b24:deal:7301",
		SyncWithBitrix:       true,
		BitrixServicePointID: &pointID,
		BitrixDealTitle:      "Сделка 7301",
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if err := bitrixRepo.UpsertDealLink(ctx, &bitrix.DealLink{
		TicketID:   ticket.ID,
		B24DealID:  7301,
		LastSyncAt: time.Now(),
	}); err != nil {
		t.Fatalf("не удалось создать deal_link: %v", err)
	}
	if err := bitrixRepo.UpsertCommentLink(ctx, &bitrix.CommentLink{
		EtalonCommentID: "comment-delete-1",
		B24CommentID:    771,
		TicketID:        ticket.ID,
		Direction:       "b24_to_etalon",
	}); err != nil {
		t.Fatalf("не удалось создать comment_link: %v", err)
	}
	if err := ticketRepo.AddComments(ctx, []tickets.TicketComment{{
		ID:              "comment-delete-1",
		TicketID:        ticket.ID,
		ServiceDeskUUID: "comment-delete-1",
		Text:            "Комментарий",
		AuthorName:      "Админ",
		CreationDate:    time.Now(),
	}}); err != nil {
		t.Fatalf("не удалось создать комментарий: %v", err)
	}
	if err := ticketRepo.AddHistory(ctx, &tickets.TicketHistory{
		TicketID:  ticket.ID,
		Action:    tickets.HistoryActionFieldChanged,
		Field:     tickets.HistoryFieldStatus,
		Source:    tickets.HistorySourceUI,
		OldValue:  tickets.StatusNew,
		NewValue:  tickets.StatusInProgress,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("не удалось создать историю: %v", err)
	}

	storageKey := filepath.ToSlash(filepath.Join(ticket.ID, "file-1.txt"))
	absPath := filepath.Join(storageRoot, filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		t.Fatalf("не удалось создать директорию файла: %v", err)
	}
	if err := os.WriteFile(absPath, []byte("test file"), 0644); err != nil {
		t.Fatalf("не удалось создать файл тикета: %v", err)
	}
	asset, err := ticketRepo.UpsertFileAsset(ctx, &tickets.FileAsset{
		ID:           "file-asset-1",
		StorageKey:   storageKey,
		OriginalName: "file-1.txt",
		MimeType:     "text/plain",
		Size:         9,
	})
	if err != nil {
		t.Fatalf("не удалось создать file_asset: %v", err)
	}
	if err := db.Create(&tickets.TicketFileLink{
		ID:           "ticket-file-link-1",
		TicketID:     ticket.ID,
		FileID:       asset.ID,
		RelationType: tickets.RelationTypeDirectTicketAttachment,
	}).Error; err != nil {
		t.Fatalf("не удалось создать ticket_file_link: %v", err)
	}

	svc := NewTicketService(
		logger.New("", "test", "error", true),
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		&config.Config{CommonContractID: "common-contract", TicketStoragePath: storageRoot},
		nil,
		nil,
		nil,
		bitrixRepo,
		nil,
		nil,
		nil,
		nil,
	)

	if err := svc.Delete(ctx, ticket.ID, admin.ID, []string{user.RoleAdmin}); err != nil {
		t.Fatalf("Delete вернул ошибку: %v", err)
	}

	_, err = ticketRepo.GetByID(ctx, ticket.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ожидали удаление тикета, получили err=%v", err)
	}

	var commentCount int64
	if err := db.Model(&tickets.TicketComment{}).Where("ticket_id = ?", ticket.ID).Count(&commentCount).Error; err != nil {
		t.Fatalf("не удалось проверить комментарии: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("ожидали удаление комментариев, осталось %d", commentCount)
	}

	var historyCount int64
	if err := db.Model(&tickets.TicketHistory{}).Where("ticket_id = ?", ticket.ID).Count(&historyCount).Error; err != nil {
		t.Fatalf("не удалось проверить историю: %v", err)
	}
	if historyCount != 0 {
		t.Fatalf("ожидали удаление истории, осталось %d", historyCount)
	}

	var fileLinkCount int64
	if err := db.Model(&tickets.TicketFileLink{}).Where("ticket_id = ?", ticket.ID).Count(&fileLinkCount).Error; err != nil {
		t.Fatalf("не удалось проверить ticket_file_links: %v", err)
	}
	if fileLinkCount != 0 {
		t.Fatalf("ожидали удаление ticket_file_links, осталось %d", fileLinkCount)
	}

	var fileAssetCount int64
	if err := db.Model(&tickets.FileAsset{}).Where("id = ?", asset.ID).Count(&fileAssetCount).Error; err != nil {
		t.Fatalf("не удалось проверить file_assets: %v", err)
	}
	if fileAssetCount != 0 {
		t.Fatalf("ожидали удаление file_asset, осталось %d", fileAssetCount)
	}

	if _, statErr := os.Stat(absPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("ожидали удаление файла %s, statErr=%v", absPath, statErr)
	}

	ignored, err := bitrixRepo.HasIgnoredDeal(ctx, 7301)
	if err != nil {
		t.Fatalf("не удалось проверить ignored deal: %v", err)
	}
	if !ignored {
		t.Fatalf("ожидали ignored deal для сделки 7301")
	}
}

func openTicketServiceDeleteTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&contract.Contract{},
		&tickets.Ticket{},
		&tickets.TicketHistory{},
		&tickets.TicketComment{},
		&tickets.Attachment{},
		&tickets.FileAsset{},
		&tickets.TicketFileLink{},
		&bitrix.DealLink{},
		&bitrix.CommentLink{},
		&bitrix.IgnoredDeal{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	return db
}

func createAdminUserForTicketDeleteTests(t *testing.T, ctx context.Context, userRepo user.Repository) *user.User {
	t.Helper()

	admin := &user.User{
		Username:     "admin_delete_test",
		PasswordHash: "hash",
		FullName:     "Администратор",
		Position:     user.RoleAdmin,
		Roles: []user.Role{
			{Name: user.RoleAdmin},
		},
	}
	if err := userRepo.Create(ctx, admin); err != nil {
		t.Fatalf("не удалось создать администратора: %v", err)
	}
	return admin
}

func createCompanyForTicketDeleteTests(t *testing.T, ctx context.Context, companyRepo company.Repository) string {
	t.Helper()

	title := "Компания для теста удаления"
	activeContract := false
	item := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(ctx, item); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}
	return item.ID
}
