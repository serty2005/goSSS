package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
)

func TestPyrusLockedServerCreatesTicketAndResumesWaiting(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		t.Run(fmt.Sprintf("waiting=%v", waiting), func(t *testing.T) {
			env := newPyrusTestEnv(t, false)
			ctx := t.Context()
			task := pyrusTestTask(env, 9001, "CRM-LOCKED", pyrusTestComment(901, "Обращение"))
			env.api.setTask(&task)
			const id = "11111111-0000-4000-8000-000000000901"
			insertPyrusTestEvent(t, env, id, time.Now(), task)
			if waiting {
				env.incoming.processIncomingEvent(ctx, id)
				if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusWaiting {
					t.Fatalf("ожидали waiting: %s", got)
				}
			}
			owner := createCompanyRecord(t, env.db, "company-locked", "Без активного договора")
			createServerRecord(t, env.db, "CRM-LOCKED", owner)
			if err := env.db.Model(&server.Server{}).Where("crm_id = ?", "CRM-LOCKED").Update("status", "locked").Error; err != nil {
				t.Fatal(err)
			}
			env.incoming.processIncomingEvent(ctx, id)
			if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusDone {
				t.Fatalf("ожидали done: %s", got)
			}
			ticket, err := env.ticketRepo.GetByServiceDeskUUID(ctx, pyrusTicketServiceDeskUUID(task.ID))
			if err != nil || ticket == nil || ticket.CompanyID != owner {
				t.Fatalf("неверная компания тикета: %+v, %v", ticket, err)
			}
			servers, err := env.serverRepo.ListByCRMid(ctx, "CRM-LOCKED")
			if err != nil || len(servers) != 1 || servers[0].Status != "locked" {
				t.Fatalf("сервер должен оставаться locked: %+v, %v", servers, err)
			}
		})
	}
}

func TestPyrusImportsAllAttachmentsAndRepairsExistingInlineLinks(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()
	if err := env.db.Exec(`CREATE UNIQUE INDEX idx_test_pyrus_files ON ticket_file_links (ticket_id, file_id, relation_type, COALESCE(comment_uuid, ''))`).Error; err != nil {
		t.Fatal(err)
	}
	owner := createCompanyRecord(t, env.db, "company-files", "Компания")
	createServerRecord(t, env.db, "CRM-FILES", owner)
	comment := pyrusTestComment(902, "Файлы")
	comment.Attachments = []pyrusplugin.Attachment{{ID: 2, Name: "comment.txt"}}
	task := pyrusTestTask(env, 9002, "CRM-FILES", comment)
	task.Attachments = []pyrusplugin.Attachment{{ID: 1, Name: "task.txt"}, {ID: 2, Name: "comment.txt"}}
	task.Fields = append(task.Fields,
		pyrusplugin.Field{ID: 10, Type: "file", Value: []pyrusplugin.Attachment{{ID: 3, Name: "field.txt"}}},
		pyrusplugin.Field{ID: 11, Type: "title", Value: map[string]any{"fields": []pyrusplugin.Field{{ID: 12, Type: "table", Value: []map[string]any{{"cells": []pyrusplugin.Field{{ID: 13, Type: "file", Value: []pyrusplugin.Attachment{{ID: 4, Name: "cell.txt"}}}}}}}}}},
	)
	for id := int64(1); id <= 4; id++ {
		env.api.files[id] = pyrusplugin.DownloadedFile{Content: []byte(fmt.Sprintf("файл %d", id)), MimeType: "text/plain"}
	}
	if err := json.Unmarshal(mustPyrusJSON(t, task), &task); err != nil {
		t.Fatal(err)
	}
	env.api.setTask(&task)
	ticket, err := env.incoming.createTicketFromPyrusTask(ctx, &task)
	if err != nil {
		t.Fatal(err)
	}
	links, err := env.ticketRepo.GetTicketFileLinksByRelation(ctx, ticket.ID, []string{tickets.RelationTypeDirectTicketAttachment})
	if err != nil || len(links) != 4 {
		t.Fatalf("ожидали четыре прямых вложения: %d, %v", len(links), err)
	}
	attachments, err := env.ticketRepo.GetAttachments(ctx, ticket.ID)
	if err != nil || len(attachments) != 4 {
		t.Fatalf("вложения не доступны в тикете: %d, %v", len(attachments), err)
	}
	for _, link := range links {
		asset, err := env.ticketRepo.GetFileAssetByID(ctx, link.FileID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.ReadFile(filepath.Join(env.cfg.TicketStoragePath, asset.StorageKey)); err != nil {
			t.Fatal(err)
		}
	}
	// Ранее импортированный файл комментария мог иметь только inline-связь.
	if err := env.db.Where("ticket_id = ? AND relation_type = ?", ticket.ID, tickets.RelationTypeDirectTicketAttachment).Delete(&tickets.TicketFileLink{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.incoming.syncExistingTicketFromPyrusTask(ctx, &task, ticket.ID); err != nil {
		t.Fatal(err)
	}
	links, err = env.ticketRepo.GetTicketFileLinksByRelation(ctx, ticket.ID, []string{tickets.RelationTypeDirectTicketAttachment})
	if err != nil || len(links) != 4 {
		t.Fatalf("повтор должен восстановить все вложения: %d, %v", len(links), err)
	}
	for id, count := range env.api.downloads {
		if count != 1 {
			t.Fatalf("файл %d скачан %d раз", id, count)
		}
	}
}

func TestPyrusAttachmentFailureRetriesActualTask(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()
	if err := env.db.Exec(`CREATE UNIQUE INDEX idx_test_pyrus_files ON ticket_file_links (ticket_id, file_id, relation_type, COALESCE(comment_uuid, ''))`).Error; err != nil {
		t.Fatal(err)
	}
	owner := createCompanyRecord(t, env.db, "company-retry-files", "Компания")
	createServerRecord(t, env.db, "CRM-RETRY-FILES", owner)
	snapshot := pyrusTestTask(env, 9003, "CRM-RETRY-FILES", pyrusTestComment(903, "Файл в актуальной задаче"))
	actual := snapshot
	actual.Attachments = []pyrusplugin.Attachment{{ID: 9, Name: "retry.txt"}}
	env.api.setTask(&actual)
	env.api.files[9] = pyrusplugin.DownloadedFile{Content: []byte("данные")}
	env.api.downloadErr = errors.New("ошибка скачивания")
	const id = "11111111-0000-4000-8000-000000000903"
	insertPyrusTestEvent(t, env, id, time.Now(), snapshot)
	env.incoming.processIncomingEvent(ctx, id)
	if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusFailed {
		t.Fatalf("ожидали failed: %s", got)
	}
	env.api.downloadErr = nil
	env.incoming.processIncomingEvent(ctx, id)
	if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusDone {
		t.Fatalf("ожидали done: %s", got)
	}
	if countPyrusTaskTickets(t, env, actual.ID) != 1 {
		t.Fatal("повтор создал дубль тикета")
	}
	link, err := env.pyrusRepo.GetFileLinkByPyrusAttachmentID(ctx, 9)
	if err != nil || link == nil {
		t.Fatalf("повтор потерял вложение актуальной задачи: %v", err)
	}
}

func TestPyrusCancelledEventsCannotCreateTicketOrReplay(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := t.Context()
	task := pyrusTestTask(env, 9004, "CRM-CANCEL", pyrusTestComment(904, "Ненужное обращение"))
	env.api.setTask(&task)
	control := NewIntegrationSyncControlService(env.pyrusRepo, env.incoming, nil, nil)
	statuses := []string{pyrus.IncomingEventStatusNew, pyrus.IncomingEventStatusQueued, pyrus.IncomingEventStatusWaiting, pyrus.IncomingEventStatusFailed}
	for i, status := range statuses {
		id := fmt.Sprintf("11111111-0000-4000-8000-%012d", 904+i)
		insertPyrusTestEvent(t, env, id, time.Now(), task)
		if err := env.db.Model(&pyrus.IncomingEvent{}).Where("id = ?", id).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
	}
	const processingID = "11111111-0000-4000-8000-000000000999"
	insertPyrusTestEvent(t, env, processingID, time.Now(), task)
	if claimed, err := env.pyrusRepo.TryMarkIncomingProcessing(ctx, processingID); err != nil || !claimed {
		t.Fatalf("не удалось взять событие в работу: %v", err)
	}
	count, err := control.CancelIncomingEvents(ctx, "pyrus", "", task.ID)
	if err != nil || count != 4 {
		t.Fatalf("ожидали отмену всех событий: %d, %v", count, err)
	}
	if item := mustGetPyrusEvent(t, env, processingID); item.Status != pyrus.IncomingEventStatusProcessing {
		t.Fatalf("обрабатываемое событие отменено: %+v", item)
	}
	owner := createCompanyRecord(t, env.db, "company-cancel", "Компания")
	createServerRecord(t, env.db, "CRM-CANCEL", owner)
	for i := range statuses {
		id := fmt.Sprintf("11111111-0000-4000-8000-%012d", 904+i)
		// Имитируем старое сообщение Redis и запоздавшую отметку queued.
		if err := env.pyrusRepo.MarkIncomingQueued(ctx, id); err != nil {
			t.Fatal(err)
		}
		env.incoming.processIncomingEvent(ctx, id)
		item := mustGetPyrusEvent(t, env, id)
		if item.Status != pyrus.IncomingEventStatusCancelled || item.NextRetryAt != nil || env.incoming.shouldProcessIncomingNow(item) {
			t.Fatalf("отмена потеряна: %+v", item)
		}
		if err := env.incoming.ReplayEvent(ctx, id); !errors.Is(err, pyrus.ErrIncomingEventNotReplayable) {
			t.Fatalf("отменённое событие повторилось: %v", err)
		}
	}
	if countPyrusTaskTickets(t, env, task.ID) != 0 || env.api.getCalls != 0 {
		t.Fatal("отменённые события дошли до создания тикета")
	}
	groups, _, err := env.pyrusRepo.ListIncomingTaskGroups(ctx, pyrus.IncomingTaskGroupFilter{Limit: 10})
	if err != nil || len(groups) != 1 || groups[0].CancelledCount != 4 {
		t.Fatalf("отмена не видна в сводке: %+v, %v", groups, err)
	}
	if count, err := env.incoming.ReplayTask(ctx, task.ID); err != nil || count != 0 {
		t.Fatalf("отменённая задача повторилась: %d, %v", count, err)
	}
}

func TestBitrixInitialSyncIncludesPrivatePyrusCommentsOnly(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "ok" })
	ctx := t.Context()
	comments := []tickets.TicketComment{
		{ID: "pyrus-private", TicketID: env.ticket.ID, Text: "Из Pyrus", Source: tickets.CommentSourcePyrus, IsPrivate: true, IsInternal: true},
		{ID: "xenion-private", TicketID: env.ticket.ID, Text: "Из Xenion", Source: tickets.CommentSourceUI, IsPrivate: true},
		{ID: "xenion-public", TicketID: env.ticket.ID, Text: "Публичный", Source: tickets.CommentSourceUI},
	}
	if err := env.svc.ticketRepo.AddComments(ctx, comments); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := env.svc.SyncPendingComments(ctx, env.ticket.ID); err != nil {
			t.Fatal(err)
		}
	}
	env.fake.mu.Lock()
	count := env.fake.addCalls
	env.fake.mu.Unlock()
	if count != 2 {
		t.Fatalf("ожидали две отправки без дублей, получили %d", count)
	}
	for _, id := range []string{"pyrus-private", "xenion-public"} {
		link, err := env.repo.GetCommentLinkByEtalonID(ctx, id)
		if err != nil || link == nil {
			t.Fatalf("комментарий %s не отправлен: %v", id, err)
		}
	}
	if err := env.svc.SyncComment(ctx, env.ticket.ID, &comments[0], env.userID); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.SyncComment(ctx, env.ticket.ID, &comments[1], env.userID); err != nil {
		t.Fatal(err)
	}
	link, err := env.repo.GetCommentLinkByEtalonID(ctx, "xenion-private")
	if err != nil || link != nil {
		t.Fatalf("приватный комментарий Xenion отправлен: %+v, %v", link, err)
	}
}
