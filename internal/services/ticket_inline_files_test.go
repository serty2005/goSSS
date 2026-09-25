package services

import (
	"bytes"
	"context"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"
)

func TestExtractTicketStorageKeys(t *testing.T) {
	text := `<p><img src="/api/static/tickets/t1/a.png"> и <a href="/static/tickets/t1/b.pdf">файл</a>` +
		` <img src="/api/static/tickets/t2/c.png"> <img src="/api/static/tickets/t1/a.png"></p>`
	keys := extractTicketStorageKeys("t1", text)
	if len(keys) != 2 || keys[0] != "t1/a.png" || keys[1] != "t1/b.pdf" {
		t.Fatalf("неожиданные ключи: %v", keys)
	}
	if got := extractTicketStorageKeys("t1", ""); len(got) != 0 {
		t.Fatalf("для пустого текста ожидали пустой результат, получили %v", got)
	}
}

func TestNormalizeTicketUploadRelation(t *testing.T) {
	cases := map[string]string{
		"":                   tickets.RelationTypeDirectTicketAttachment,
		"direct":             tickets.RelationTypeDirectTicketAttachment,
		"inline_comment":     tickets.RelationTypeInlineComment,
		"inline_description": tickets.RelationTypeInlineDescription,
	}
	for input, expected := range cases {
		got, ok := NormalizeTicketUploadRelation(input)
		if !ok || got != expected {
			t.Fatalf("для %q ожидали %q, получили %q (ok=%v)", input, expected, got, ok)
		}
	}
	if _, ok := NormalizeTicketUploadRelation("inline_result"); ok {
		t.Fatalf("inline_result не должен приниматься при загрузке из UI")
	}
}

func TestCommentInlineFilesLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTicketServiceDeleteTestDB(t)
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ticket_file_links_unique
		ON ticket_file_links (ticket_id, file_id, relation_type, COALESCE(comment_uuid, ''))`).Error; err != nil {
		t.Fatalf("не удалось создать индекс связей файлов: %v", err)
	}

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	admin := createAdminUserForTicketDeleteTests(t, ctx, userRepo)
	companyID := createCompanyForTicketDeleteTests(t, ctx, companyRepo)

	ticket := &tickets.Ticket{
		Subject:     "Тикет с картинками",
		Description: "Описание",
		Status:      tickets.StatusNew,
		Priority:    tickets.PriorityMedium,
		Type:        tickets.TypeIncident,
		CompanyID:   companyID,
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	storagePath := t.TempDir()
	svc := NewTicketService(
		logger.New("", "test", "error", true),
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		&config.Config{CommonContractID: "common-contract", TicketStoragePath: storagePath},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)

	upload := func(name string, relation string) tickets.Attachment {
		t.Helper()
		items, err := svc.UploadAttachments(ctx, ticket.ID, buildTestFileHeaders(t, name, []byte("content-"+name)), relation)
		if err != nil || len(items) != 1 {
			t.Fatalf("не удалось загрузить %s: %v", name, err)
		}
		return items[0]
	}
	fileExists := func(item tickets.Attachment) bool {
		key := strings.TrimPrefix(item.FilePath, "/api/static/tickets/")
		_, err := os.Stat(filepath.Join(storagePath, filepath.FromSlash(key)))
		return err == nil
	}

	first := upload("first.png", tickets.RelationTypeInlineComment)
	second := upload("second.png", tickets.RelationTypeInlineComment)
	direct := upload("direct.pdf", tickets.RelationTypeDirectTicketAttachment)

	attachments, err := ticketRepo.GetAttachments(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("GetAttachments: %v", err)
	}
	if len(attachments) != 1 || attachments[0].ID != direct.ID {
		t.Fatalf("во вложениях должен быть только прямой файл, получили %+v", attachments)
	}

	comment, err := svc.AddComment(ctx, ticket.ID,
		`<p><img src="`+first.FilePath+`"><img src="`+second.FilePath+`"><a href="`+direct.FilePath+`">pdf</a></p>`,
		false, false, admin.ID)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	links, err := ticketRepo.GetTicketFileLinksByRelation(ctx, ticket.ID, []string{tickets.RelationTypeInlineComment})
	if err != nil {
		t.Fatalf("GetTicketFileLinksByRelation: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("ожидали 2 inline-связи, получили %d", len(links))
	}
	for _, link := range links {
		if link.CommentUUID == nil || *link.CommentUUID != comment.ServiceDeskUUID {
			t.Fatalf("inline-файл не привязан к комментарию: %+v", link)
		}
	}

	if _, err := svc.UpdateComment(ctx, ticket.ID, comment.ServiceDeskUUID, `<p><img src="`+second.FilePath+`"></p>`, false, admin.ID, nil); err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if fileExists(first) {
		t.Fatalf("убранный из комментария файл должен быть удален из хранилища")
	}
	if asset, _ := ticketRepo.GetFileAssetByID(ctx, first.ID); asset != nil {
		t.Fatalf("запись удаленного файла должна быть удалена")
	}
	if !fileExists(second) {
		t.Fatalf("оставшийся в комментарии файл не должен удаляться")
	}
	if !fileExists(direct) {
		t.Fatalf("прямое вложение тикета не должно удаляться при правке комментария")
	}

	if err := svc.DeleteComment(ctx, ticket.ID, comment.ServiceDeskUUID, admin.ID, nil); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if fileExists(second) {
		t.Fatalf("файл удаленного комментария должен быть удален из хранилища")
	}
}

func TestCleanupKeepsFileUsedInAnotherComment(t *testing.T) {
	ctx := context.Background()
	db := openTicketServiceDeleteTestDB(t)
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ticket_file_links_unique
		ON ticket_file_links (ticket_id, file_id, relation_type, COALESCE(comment_uuid, ''))`).Error; err != nil {
		t.Fatalf("не удалось создать индекс связей файлов: %v", err)
	}
	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	admin := createAdminUserForTicketDeleteTests(t, ctx, userRepo)
	companyID := createCompanyForTicketDeleteTests(t, ctx, companyRepo)
	ticket := &tickets.Ticket{Subject: "Тикет", Status: tickets.StatusNew, Priority: tickets.PriorityMedium, Type: tickets.TypeIncident, CompanyID: companyID}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	storagePath := t.TempDir()
	svc := NewTicketService(
		logger.New("", "test", "error", true),
		ticketRepo, userRepo, companyRepo, repositories.NewContractRepo(db), nil,
		&config.Config{CommonContractID: "common-contract", TicketStoragePath: storagePath},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	items, err := svc.UploadAttachments(ctx, ticket.ID, buildTestFileHeaders(t, "shared.png", []byte("shared")), tickets.RelationTypeInlineComment)
	if err != nil {
		t.Fatalf("UploadAttachments: %v", err)
	}
	image := `<img src="` + items[0].FilePath + `">`
	firstComment, err := svc.AddComment(ctx, ticket.ID, image, false, false, admin.ID)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := svc.AddComment(ctx, ticket.ID, "Повтор "+image, false, false, admin.ID); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := svc.DeleteComment(ctx, ticket.ID, firstComment.ServiceDeskUUID, admin.ID, nil); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	key := strings.TrimPrefix(items[0].FilePath, "/api/static/tickets/")
	if _, err := os.Stat(filepath.Join(storagePath, filepath.FromSlash(key))); err != nil {
		t.Fatalf("файл, используемый в другом комментарии, не должен удаляться: %v", err)
	}
}

func buildTestFileHeaders(t *testing.T, name string, content []byte) []*multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", name)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("запись multipart: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("закрытие multipart: %v", err)
	}
	form, err := multipart.NewReader(&body, writer.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("ReadForm: %v", err)
	}
	return form.File["files"]
}
