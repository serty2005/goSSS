package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakePyrusAPIClient struct {
	configured       bool
	getTaskFunc      func(ctx context.Context, taskID int64) (*pyrusplugin.Task, error)
	addCommentFunc   func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error)
	listMembersFunc  func(ctx context.Context) ([]pyrusplugin.Member, error)
	updateExtIDFunc  func(ctx context.Context, taskID int64, extID string) (*pyrusplugin.Task, error)
	downloadFileFunc func(ctx context.Context, fileID int64) (*pyrusplugin.DownloadedFile, error)
	uploadFileFunc   func(ctx context.Context, fileName string, mimeType string, content []byte) (string, error)
}

func (f *fakePyrusAPIClient) IsConfigured() bool {
	return f != nil && f.configured
}

func (f *fakePyrusAPIClient) GetTask(ctx context.Context, taskID int64) (*pyrusplugin.Task, error) {
	if f == nil || f.getTaskFunc == nil {
		return nil, nil
	}
	return f.getTaskFunc(ctx, taskID)
}

func (f *fakePyrusAPIClient) AddComment(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
	if f == nil || f.addCommentFunc == nil {
		return nil, nil
	}
	return f.addCommentFunc(ctx, taskID, req)
}

func (f *fakePyrusAPIClient) ListMembers(ctx context.Context) ([]pyrusplugin.Member, error) {
	if f == nil || f.listMembersFunc == nil {
		return []pyrusplugin.Member{}, nil
	}
	return f.listMembersFunc(ctx)
}

func (f *fakePyrusAPIClient) UpdateTaskExtID(ctx context.Context, taskID int64, extID string) (*pyrusplugin.Task, error) {
	if f == nil || f.updateExtIDFunc == nil {
		return nil, nil
	}
	return f.updateExtIDFunc(ctx, taskID, extID)
}

func (f *fakePyrusAPIClient) DownloadFile(ctx context.Context, fileID int64) (*pyrusplugin.DownloadedFile, error) {
	if f == nil || f.downloadFileFunc == nil {
		return nil, nil
	}
	return f.downloadFileFunc(ctx, fileID)
}

func (f *fakePyrusAPIClient) UploadFile(ctx context.Context, fileName string, mimeType string, content []byte) (string, error) {
	if f == nil || f.uploadFileFunc == nil {
		return "", nil
	}
	return f.uploadFileFunc(ctx, fileName, mimeType, content)
}

func TestPyrusSyncService_HandleOutgoingCommentSync(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-1")
	comment := &tickets.TicketComment{
		ID:            "local-comment-1",
		TicketID:      ticketID,
		Text:          "Комментарий сотрудника в общую ленту",
		CreationDate:  time.Now(),
		IsPrivate:     false,
		ReplyToClient: false,
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 1)
	client := &fakePyrusAPIClient{
		configured: true,
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			requests = append(requests, req)
			return &pyrusplugin.Task{
				ID: taskID,
				Comments: []pyrusplugin.Comment{
					{ID: 9001, Text: comment.Text, CreateDate: time.Now()},
				},
			}, nil
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete, ok := service.(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7001,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone || reason != "" {
		t.Fatalf("ожидали done без reason, получили status=%q reason=%q", status, reason)
	}
	if len(requests) != 1 {
		t.Fatalf("ожидали один запрос комментария в Pyrus, получили %d", len(requests))
	}
	if requests[0].Channel != nil {
		t.Fatalf("не ожидали channel для обычного комментария сотрудника, получили %+v", requests[0].Channel)
	}
	if requests[0].EditCommentID != nil {
		t.Fatalf("не ожидали edit_comment_id для нового комментария")
	}

	link, err := env.pyrusRepo.GetCommentLinkByEtalonID(ctx, comment.ID)
	if err != nil {
		t.Fatalf("не удалось получить link комментария: %v", err)
	}
	if link == nil || link.PyrusCommentID == nil || *link.PyrusCommentID != 9001 {
		t.Fatalf("ожидали сохранённый link комментария с pyrus_comment_id=9001, получили %+v", link)
	}
}

func TestPyrusSyncService_HandleOutgoingCommentSyncUsesFormattedText(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-html-1")
	comment := &tickets.TicketComment{
		ID:           "local-comment-html-1",
		TicketID:     ticketID,
		Text:         `<p>Тест <strong>жирный</strong> <a href="https://example.com">линк</a></p>`,
		CreationDate: time.Now(),
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 1)
	client := &fakePyrusAPIClient{
		configured: true,
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			requests = append(requests, req)
			return &pyrusplugin.Task{
				ID: taskID,
				Comments: []pyrusplugin.Comment{
					{ID: 9011, Text: "Тест жирный линк", CreateDate: time.Now()},
				},
			}, nil
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete := service.(*pyrusSyncService)

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7001,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-html-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone || reason != "" {
		t.Fatalf("ожидали done без reason, получили status=%q reason=%q", status, reason)
	}
	if len(requests) != 1 {
		t.Fatalf("ожидали один запрос комментария в Pyrus, получили %d", len(requests))
	}
	if requests[0].Text != "" {
		t.Fatalf("не ожидали text для rich-text комментария, получили %q", requests[0].Text)
	}
	expected := `Тест <b>жирный</b> <a href="https://example.com">линк</a>`
	if requests[0].FormattedText != expected {
		t.Fatalf("ожидали formatted_text %q, получили %q", expected, requests[0].FormattedText)
	}
	link, err := env.pyrusRepo.GetCommentLinkByEtalonID(ctx, comment.ID)
	if err != nil {
		t.Fatalf("не удалось получить link комментария: %v", err)
	}
	if link == nil || link.PyrusCommentID == nil || *link.PyrusCommentID != 9011 {
		t.Fatalf("ожидали сохранённый link комментария с pyrus_comment_id=9011, получили %+v", link)
	}
}

func TestPyrusSyncService_HandleOutgoingCommentSyncReplyToClientSendsRegularComment(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-reply-1")
	if err := env.pyrusRepo.UpsertTicketContext(ctx, &pyrus.TicketContext{
		TicketID:    ticketID,
		PyrusTaskID: 7001,
		SenderName:  "Юрий",
		SenderEmail: "client@example.com",
	}); err != nil {
		t.Fatalf("не удалось сохранить ticket context: %v", err)
	}
	comment := &tickets.TicketComment{
		ID:            "local-comment-reply-1",
		TicketID:      ticketID,
		Text:          "Ответ оператором клиенту",
		CreationDate:  time.Now(),
		ReplyToClient: true,
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 1)
	client := &fakePyrusAPIClient{
		configured: true,
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			requests = append(requests, req)
			return &pyrusplugin.Task{
				ID: taskID,
				Comments: []pyrusplugin.Comment{
					{
						ID:         9051,
						Text:       comment.Text,
						CreateDate: time.Now(),
						Channel:    &pyrusplugin.Channel{Type: "mobile_app", To: &pyrusplugin.ChannelParty{Name: "Юрий", Email: "client@example.com"}},
					},
				},
			}, nil
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete := service.(*pyrusSyncService)

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7001,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-reply-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone || reason != "" {
		t.Fatalf("ожидали done без reason, получили status=%q reason=%q", status, reason)
	}
	if len(requests) != 1 {
		t.Fatalf("ожидали один запрос комментария в Pyrus, получили %d", len(requests))
	}
	if requests[0].Channel != nil {
		t.Fatalf("не ожидали channel для обычного публичного комментария, получили %+v", requests[0].Channel)
	}
}

func TestPyrusSyncService_HandleOutgoingCommentSyncReplyToClientPropagatesRegularError(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-reply-ignored-1")
	if err := env.pyrusRepo.UpsertTicketContext(ctx, &pyrus.TicketContext{
		TicketID:    ticketID,
		PyrusTaskID: 7001,
		SenderName:  "Юрий",
		SenderEmail: "client@example.com",
	}); err != nil {
		t.Fatalf("не удалось сохранить ticket context: %v", err)
	}
	comment := &tickets.TicketComment{
		ID:            "local-comment-reply-ignored-1",
		TicketID:      ticketID,
		Text:          "Ответ клиенту без доступа к каналу",
		CreationDate:  time.Now(),
		ReplyToClient: true,
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 1)
	client := &fakePyrusAPIClient{
		configured: true,
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			requests = append(requests, req)
			return nil, &pyrusplugin.HTTPError{
				StatusCode: 403,
				Code:       "access_denied",
				Message:    "Доступ запрещен",
			}
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete := service.(*pyrusSyncService)

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7001,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-reply-ignored-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err == nil {
		t.Fatal("ожидали ошибку отправки комментария в Pyrus")
	}
	if len(requests) != 1 {
		t.Fatalf("ожидали один запрос в Pyrus перед ошибкой, получили %d", len(requests))
	}
	if status != "" || reason != "" {
		t.Fatalf("при ошибке не ожидали status/reason, получили status=%q reason=%q", status, reason)
	}
	if requests[0].Channel != nil {
		t.Fatalf("не ожидали channel для обычного публичного комментария, получили %+v", requests[0].Channel)
	}
}

func TestPyrusSyncService_HandleOutgoingCommentSyncUploadsAttachments(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-attachment-1")
	comment := &tickets.TicketComment{
		ID:           "local-comment-with-file",
		TicketID:     ticketID,
		Text:         "Ответ оператора с вложением",
		CreationDate: time.Now(),
		IsPrivate:    false,
	}

	storageKey := filepath.ToSlash(filepath.Join(ticketID, "outgoing", "attachment.txt"))
	absPath := filepath.Join(env.cfg.TicketStoragePath, filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("не удалось создать директорию для тестового вложения: %v", err)
	}
	if err := os.WriteFile(absPath, []byte("test attachment"), 0o644); err != nil {
		t.Fatalf("не удалось записать тестовое вложение: %v", err)
	}
	asset, err := env.ticketRepo.UpsertFileAsset(ctx, &tickets.FileAsset{
		StorageKey:   storageKey,
		OriginalName: "attachment.txt",
		MimeType:     "text/plain",
		Size:         int64(len("test attachment")),
		Checksum:     "checksum",
	})
	if err != nil {
		t.Fatalf("не удалось сохранить file_asset: %v", err)
	}
	if asset == nil {
		t.Fatal("ожидали сохранённый file_asset")
	}
	if err := env.db.WithContext(ctx).Create(&tickets.TicketFileLink{
		TicketID:     ticketID,
		FileID:       asset.ID,
		RelationType: tickets.RelationTypeInlineComment,
		CommentUUID:  &comment.ID,
	}).Error; err != nil {
		t.Fatalf("не удалось сохранить link комментария на файл: %v", err)
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 1)
	uploaded := make([]string, 0, 1)
	client := &fakePyrusAPIClient{
		configured: true,
		uploadFileFunc: func(ctx context.Context, fileName string, mimeType string, content []byte) (string, error) {
			uploaded = append(uploaded, fileName+"|"+mimeType+"|"+string(content))
			return "guid-attachment-1", nil
		},
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			requests = append(requests, req)
			return &pyrusplugin.Task{
				ID: taskID,
				Comments: []pyrusplugin.Comment{
					{ID: 9002, Text: comment.Text, CreateDate: time.Now()},
				},
			}, nil
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete, ok := service.(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7001,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-attachment-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone || reason != "" {
		t.Fatalf("ожидали done без reason, получили status=%q reason=%q", status, reason)
	}
	if len(uploaded) != 1 {
		t.Fatalf("ожидали одну загрузку файла в Pyrus, получили %d", len(uploaded))
	}
	if len(requests) != 1 {
		t.Fatalf("ожидали один запрос комментария в Pyrus, получили %d", len(requests))
	}
	if len(requests[0].Attachments) != 1 || requests[0].Attachments[0] != "guid-attachment-1" {
		t.Fatalf("ожидали guid загруженного файла в attachments, получили %+v", requests[0].Attachments)
	}
}

func TestPyrusSyncService_HandleOutgoingStatusSyncSendsPendingCommentBeforeFinish(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-status-1")
	if err := env.pyrusRepo.UpsertTicketLink(ctx, &pyrus.TicketLink{
		TicketID:    ticketID,
		PyrusTaskID: 7002,
	}); err != nil {
		t.Fatalf("не удалось сохранить ticket link: %v", err)
	}
	if err := env.pyrusRepo.UpsertTicketContext(ctx, &pyrus.TicketContext{
		TicketID:    ticketID,
		PyrusTaskID: 7002,
		SenderName:  "Юрий",
	}); err != nil {
		t.Fatalf("не удалось сохранить ticket context: %v", err)
	}

	finalReport := tickets.TicketComment{
		ID:              "local-comment-final",
		TicketID:        ticketID,
		ServiceDeskUUID: "local-comment-final",
		Text:            "Финальный отчёт оператора",
		AuthorName:      "Сотрудник",
		CreationDate:    time.Now(),
		IsPrivate:       false,
		IsInternal:      false,
	}
	if err := env.ticketRepo.AddComments(ctx, []tickets.TicketComment{finalReport}); err != nil {
		t.Fatalf("не удалось добавить финальный комментарий: %v", err)
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 2)
	client := &fakePyrusAPIClient{
		configured: true,
		getTaskFunc: func(ctx context.Context, taskID int64) (*pyrusplugin.Task, error) {
			return &pyrusplugin.Task{
				ID: taskID,
				Fields: []pyrusplugin.Field{
					{ID: 500, Code: "status", Name: "Status", Type: "text", Value: "open"},
				},
			}, nil
		},
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			requests = append(requests, req)
			switch len(requests) {
			case 1:
				return &pyrusplugin.Task{
					ID: taskID,
					Comments: []pyrusplugin.Comment{
						{
							ID:         9101,
							Text:       finalReport.Text,
							CreateDate: time.Now(),
							Channel:    &pyrusplugin.Channel{Type: "mobile_app", To: &pyrusplugin.ChannelParty{Name: "Юрий"}},
						},
					},
				}, nil
			case 2:
				return &pyrusplugin.Task{
					ID: taskID,
					Comments: []pyrusplugin.Comment{
						{ID: 9102, Action: "finished", CreateDate: time.Now()},
					},
				}, nil
			default:
				t.Fatalf("получили лишний вызов AddComment: %d", len(requests))
				return nil, nil
			}
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete, ok := service.(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7002,
		Status:   tickets.StatusResolved,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-status-1",
		EventName:   events.PyrusTicketStatusSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone || reason != "" {
		t.Fatalf("ожидали done без reason, получили status=%q reason=%q", status, reason)
	}
	if len(requests) != 2 {
		t.Fatalf("ожидали два вызова AddComment, получили %d", len(requests))
	}
	if requests[0].Text != finalReport.Text {
		t.Fatalf("первый вызов должен был отправить комментарий оператора, получили %+v", requests[0])
	}
	if requests[0].Channel != nil {
		t.Fatalf("не ожидали channel для обычного комментария сотрудника, получили %+v", requests[0].Channel)
	}
	if requests[1].Action != "finished" {
		t.Fatalf("второй вызов должен был закрыть задачу через action=finished, получили %+v", requests[1])
	}

	commentLink, err := env.pyrusRepo.GetCommentLinkByEtalonID(ctx, finalReport.ID)
	if err != nil {
		t.Fatalf("не удалось получить link финального комментария: %v", err)
	}
	if commentLink == nil || commentLink.PyrusCommentID == nil || *commentLink.PyrusCommentID != 9101 {
		t.Fatalf("ожидали link финального комментария с pyrus_comment_id=9101, получили %+v", commentLink)
	}
}

func TestBuildPyrusStatusCommentRequestReopensTaskAndUpdatesStatusField(t *testing.T) {
	task := &pyrusplugin.Task{
		ID: 7003,
		Fields: []pyrusplugin.Field{
			{ID: 501, Code: "status", Name: "Status", Type: "text", Value: "finished"},
		},
		Comments: []pyrusplugin.Comment{
			{ID: 9901, Action: "finished"},
		},
	}

	req, reason, err := buildPyrusStatusCommentRequest(task, tickets.StatusPending)
	if err != nil {
		t.Fatalf("buildPyrusStatusCommentRequest вернул ошибку: %v", err)
	}
	if reason != "" {
		t.Fatalf("не ожидали ignored reason, получили %q", reason)
	}
	if req.Action != "reopened" {
		t.Fatalf("ожидали action=reopened, получили %+v", req)
	}
	if len(req.FieldUpdates) != 1 || req.FieldUpdates[0].ID == nil || *req.FieldUpdates[0].ID != 501 || req.FieldUpdates[0].Value != "pending" {
		t.Fatalf("ожидали обновление поля status на pending, получили %+v", req.FieldUpdates)
	}
}

func TestPyrusSyncService_HandleOutgoingAssigneeSync(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	assignee := createPyrusSyncUser(t, env, "pyrus-sync-assignee", "Иван", "Петров")
	externalType := user.ExternalTypePyrus
	externalID := "91234"
	assignee.ExternalType = &externalType
	assignee.ExternalID = &externalID
	assignee.Integrations = []user.Integration{
		{
			IntegrationType: user.ExternalTypePyrus,
			ExternalID:      externalID,
			IsVerified:      true,
			IsLocked:        true,
			VerifiedName:    "Иван Петров",
		},
	}
	if err := env.userRepo.Update(ctx, assignee); err != nil {
		t.Fatalf("не удалось обновить пользователя исполнителя: %v", err)
	}
	if err := env.pyrusRepo.UpsertUserMap(ctx, &pyrus.UserMap{
		EtalonUserID: assignee.ID,
		PyrusUserID:  91234,
	}); err != nil {
		t.Fatalf("не удалось сохранить маппинг пользователя Pyrus: %v", err)
	}

	requests := make([]pyrusplugin.CommentRequest, 0, 1)
	client := &fakePyrusAPIClient{configured: true}
	client.getTaskFunc = func(ctx context.Context, taskID int64) (*pyrusplugin.Task, error) {
		return &pyrusplugin.Task{ID: taskID}, nil
	}
	client.addCommentFunc = func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
		requests = append(requests, req)
		return &pyrusplugin.Task{
			ID: taskID,
			Responsible: &pyrusplugin.Person{
				ID:        91234,
				FirstName: "Иван",
				LastName:  "Петров",
				Email:     "ivan.petrov@example.com",
				Type:      "user",
			},
		}, nil
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete, ok := service.(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}

	rawPayload, err := json.Marshal(events.PyrusSyncEntityPayload{
		TicketID:   "ticket-1",
		TaskID:     7004,
		AssigneeID: &assignee.ID,
	})
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-assignee-1",
		EventName:   events.PyrusTicketAssigneeSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone {
		t.Fatalf("ожидали done для sync исполнителя, получили %q", status)
	}
	if reason != "" {
		t.Fatalf("не ожидали ignored reason для sync исполнителя, получили %q", reason)
	}
	if len(requests) != 1 {
		t.Fatalf("ожидали один запрос в Pyrus, получили %d", len(requests))
	}
	if requests[0].ReassignTo == nil || requests[0].ReassignTo.ID == nil || *requests[0].ReassignTo.ID != 91234 {
		t.Fatalf("ожидали reassign_to.id=91234, получили %+v", requests[0].ReassignTo)
	}
}

func TestPyrusSyncService_HandleOutgoingCommentSyncUsesTicketLinkWhenPayloadTaskIDEmpty(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-link-1")
	if err := env.pyrusRepo.UpsertTicketLink(ctx, &pyrus.TicketLink{
		TicketID:    ticketID,
		PyrusTaskID: 7123,
	}); err != nil {
		t.Fatalf("не удалось сохранить ticket link: %v", err)
	}
	if err := env.pyrusRepo.UpsertTicketContext(ctx, &pyrus.TicketContext{
		TicketID:    ticketID,
		PyrusTaskID: 7123,
		SenderName:  "Юрий",
	}); err != nil {
		t.Fatalf("не удалось сохранить ticket context: %v", err)
	}

	comment := &tickets.TicketComment{
		ID:            "local-comment-link-1",
		TicketID:      ticketID,
		Text:          "Ответ через lookup link",
		CreationDate:  time.Now(),
		ReplyToClient: false,
	}

	var sentTaskID int64
	client := &fakePyrusAPIClient{
		configured: true,
		addCommentFunc: func(ctx context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			sentTaskID = taskID
			return &pyrusplugin.Task{
				ID: taskID,
				Comments: []pyrusplugin.Comment{
					{ID: 9201, Text: comment.Text, CreateDate: time.Now()},
				},
			}, nil
		},
	}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete := service.(*pyrusSyncService)

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-link-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusDone || reason != "" {
		t.Fatalf("ожидали done без reason, получили status=%q reason=%q", status, reason)
	}
	if sentTaskID != 7123 {
		t.Fatalf("ожидали task_id из pyrus_ticket_links, получили %d", sentTaskID)
	}
}

func TestPyrusSyncService_HandleOutgoingCommentSyncSkipsInternalComment(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()

	ticketID := createPyrusSyncTicket(t, env, "ticket-comment-internal-1")
	comment := &tickets.TicketComment{
		ID:           "local-comment-internal-1",
		TicketID:     ticketID,
		Text:         "Внутренний комментарий",
		CreationDate: time.Now(),
		IsInternal:   true,
	}

	client := &fakePyrusAPIClient{configured: true}
	service := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo)
	concrete := service.(*pyrusSyncService)

	payload := events.PyrusSyncEntityPayload{
		TicketID: ticketID,
		TaskID:   7001,
		Comment:  comment,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}

	status, reason, err := concrete.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID:          "outgoing-comment-internal-1",
		EventName:   events.PyrusCommentSyncRequested,
		PayloadJSON: string(rawPayload),
	})
	if err != nil {
		t.Fatalf("handleOutgoingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.OutgoingEventStatusIgnored {
		t.Fatalf("ожидали ignored для внутреннего комментария, получили %q", status)
	}
	if reason == "" {
		t.Fatal("ожидали явную причину пропуска внутреннего комментария")
	}
}

func createPyrusSyncTicket(t *testing.T, env *pyrusTestEnv, ownerSuffix string) string {
	t.Helper()
	ownerID := createCompanyRecord(t, env.db, "company-"+ownerSuffix, "Компания "+ownerSuffix)
	ticket := &tickets.Ticket{
		Subject:         "Тикет Pyrus " + ownerSuffix,
		Description:     "Описание",
		Status:          tickets.StatusInProgress,
		Priority:        tickets.PriorityMedium,
		Type:            tickets.TypeIncident,
		CompanyID:       ownerID,
		ServiceDeskUUID: "pyrus:task:7000",
		ReporterName:    "Pyrus",
		SyncWithBitrix:  false,
	}
	if err := env.ticketRepo.Create(t.Context(), ticket); err != nil {
		t.Fatalf("не удалось создать локальный тикет: %v", err)
	}
	return ticket.ID
}

func createPyrusSyncUser(t *testing.T, env *pyrusTestEnv, username, firstName, lastName string) *user.User {
	t.Helper()
	role, err := env.userRepo.EnsureRoleExists(t.Context(), user.RoleSupportSpecialist, "Специалист техподдержки")
	if err != nil {
		t.Fatalf("не удалось подготовить роль пользователя: %v", err)
	}
	u := &user.User{
		Username:     username,
		FirstName:    firstName,
		LastName:     lastName,
		FullName:     firstName + " " + lastName,
		Position:     user.RoleSupportSpecialist,
		ScheduleType: user.ScheduleFiveTwo,
		IsActive:     true,
		Roles:        []user.Role{*role},
	}
	if err := u.HashPassword("password-123"); err != nil {
		t.Fatalf("не удалось захэшировать пароль: %v", err)
	}
	if err := env.userRepo.Create(t.Context(), u); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}
	return u
}
