package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakePyrusAPI отдаёт заранее заданные задачи вместо обращения к Pyrus API.
type fakePyrusAPI struct {
	mu       sync.Mutex
	tasks    map[int64]pyrusplugin.Task
	getErr   error
	getCalls int
}

func newFakePyrusAPI() *fakePyrusAPI {
	return &fakePyrusAPI{tasks: make(map[int64]pyrusplugin.Task)}
}

func (f *fakePyrusAPI) setTask(task *pyrusplugin.Task) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = *task
}

func (f *fakePyrusAPI) IsConfigured() bool { return true }

func (f *fakePyrusAPI) GetTask(_ context.Context, taskID int64) (*pyrusplugin.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	task, ok := f.tasks[taskID]
	if !ok {
		return nil, &pyrusplugin.HTTPError{StatusCode: http.StatusNotFound, Message: "задача не найдена"}
	}
	return &task, nil
}

func (f *fakePyrusAPI) AddComment(context.Context, int64, pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
	return nil, errors.New("не поддерживается заглушкой")
}

func (f *fakePyrusAPI) ListMembers(context.Context) ([]pyrusplugin.Member, error) { return nil, nil }

func (f *fakePyrusAPI) UpdateTaskExtID(context.Context, int64, string) (*pyrusplugin.Task, error) {
	return nil, errors.New("не поддерживается заглушкой")
}

func (f *fakePyrusAPI) DownloadFile(context.Context, int64) (*pyrusplugin.DownloadedFile, error) {
	return nil, errors.New("не поддерживается заглушкой")
}

func (f *fakePyrusAPI) UploadFile(context.Context, string, string, []byte) (string, error) {
	return "", errors.New("не поддерживается заглушкой")
}

func pyrusTestComment(id int64, text string) pyrusplugin.Comment {
	return pyrusplugin.Comment{
		ID:         id,
		Text:       text,
		CreateDate: time.Now().Add(-time.Hour),
		Author:     &pyrusplugin.Person{FirstName: "Юрий", Email: "client@example.com"},
		Channel: &pyrusplugin.Channel{
			Type: "mobile_app",
			From: &pyrusplugin.ChannelParty{Name: "Юрий", Email: "client@example.com"},
		},
	}
}

func pyrusTestTask(env *pyrusTestEnv, taskID int64, crmID string, comments ...pyrusplugin.Comment) pyrusplugin.Task {
	return pyrusplugin.Task{
		ID:     taskID,
		FormID: env.cfg.PyrusFormID,
		Text:   "Сообщение клиента",
		Fields: []pyrusplugin.Field{
			{Code: "CrmId", Name: "CrmId", Value: crmID},
			{Code: "Subject", Name: "Subject", Value: "Не открывается смена"},
			{Code: "SenderName", Name: "SenderName", Value: "Юрий"},
		},
		Comments: comments,
	}
}

// insertPyrusTestEvent сохраняет входящее событие со снимком задачи, как это делает webhook.
func insertPyrusTestEvent(t *testing.T, env *pyrusTestEnv, id string, receivedAt time.Time, snapshot pyrusplugin.Task) {
	t.Helper()
	raw := mustPyrusJSON(t, pyrusplugin.WebhookPayload{Event: "comment", TaskID: snapshot.ID, Task: snapshot})
	hash := sha256.Sum256([]byte(id))
	taskID := snapshot.ID
	created, err := env.pyrusRepo.InsertIncomingEventIfNotExists(context.Background(), &pyrus.IncomingEvent{
		ID:          id,
		EventName:   "comment",
		PyrusTaskID: &taskID,
		PayloadHash: hex.EncodeToString(hash[:]),
		PayloadRaw:  string(raw),
		Status:      pyrus.IncomingEventStatusNew,
		ReceivedAt:  receivedAt,
	})
	if err != nil || !created {
		t.Fatalf("не удалось сохранить событие %s: created=%v err=%v", id, created, err)
	}
}

func mustGetPyrusEvent(t *testing.T, env *pyrusTestEnv, id string) *pyrus.IncomingEvent {
	t.Helper()
	item, err := env.pyrusRepo.GetIncomingEventByID(context.Background(), id)
	if err != nil || item == nil {
		t.Fatalf("не удалось получить событие %s: %v", id, err)
	}
	return item
}

func countPyrusTaskTickets(t *testing.T, env *pyrusTestEnv, taskID int64) int64 {
	t.Helper()
	var count int64
	if err := env.db.Model(&tickets.Ticket{}).Where("service_desk_uuid = ?", pyrusTicketServiceDeskUUID(taskID)).Count(&count).Error; err != nil {
		t.Fatalf("не удалось посчитать тикеты задачи: %v", err)
	}
	return count
}

func TestPyrusIncomingService_UnknownCRMIDWaitsInsteadOfFailing(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	task := pyrusTestTask(env, 7001, "CRM-WAIT", pyrusTestComment(11, "Первый"))
	env.api.setTask(&task)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000001", time.Now(), task)

	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000001")

	item := mustGetPyrusEvent(t, env, "11111111-0000-4000-8000-000000000001")
	if item.Status != pyrus.IncomingEventStatusWaiting {
		t.Fatalf("ожидали статус waiting, получили %q", item.Status)
	}
	if item.Attempts != 0 {
		t.Fatalf("ожидание данных не должно расходовать попытки, получили attempts=%d", item.Attempts)
	}
	if item.LastError == nil || !strings.Contains(*item.LastError, "CRMID=CRM-WAIT") {
		t.Fatalf("ожидали причину с CRMID, получили %v", item.LastError)
	}
	if item.WaitStartedAt == nil || item.NextRetryAt == nil || !item.NextRetryAt.After(time.Now()) {
		t.Fatalf("ожидали назначенный будущий повтор, получили wait_started_at=%v next_retry_at=%v", item.WaitStartedAt, item.NextRetryAt)
	}
	if countPyrusTaskTickets(t, env, task.ID) != 0 {
		t.Fatalf("тикет не должен создаваться без компании")
	}

	due, err := env.pyrusRepo.ListIncomingDueForProcessing(ctx, 10, 10)
	if err != nil {
		t.Fatalf("не удалось получить события к обработке: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("событие с будущим сроком повтора не должно попадать в выборку, получили %d", len(due))
	}

	// Срок повтора наступил: событие снова берётся в работу.
	if err := env.pyrusRepo.MarkIncomingWaiting(ctx, item.ID, "причина", *item.WaitStartedAt, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("не удалось сдвинуть срок повтора: %v", err)
	}
	due, err = env.pyrusRepo.ListIncomingDueForProcessing(ctx, 10, 10)
	if err != nil {
		t.Fatalf("не удалось получить события к обработке: %v", err)
	}
	if len(due) != 1 || !env.incoming.shouldProcessIncomingNow(&due[0]) {
		t.Fatalf("ожидали одно событие к повтору, получили %d", len(due))
	}
}

func TestPyrusIncomingService_AutoRetryCreatesTicketOnceCRMIDAppears(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	task := pyrusTestTask(env, 7002, "CRM-APPEARS", pyrusTestComment(21, "Первый"))
	env.api.setTask(&task)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000002", time.Now(), task)

	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000002")
	if got := mustGetPyrusEvent(t, env, "11111111-0000-4000-8000-000000000002").Status; got != pyrus.IncomingEventStatusWaiting {
		t.Fatalf("ожидали waiting, получили %q", got)
	}

	ownerID := createCompanyRecord(t, env.db, "company-appears", "Компания")
	createServerRecord(t, env.db, "CRM-APPEARS", ownerID)

	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000002")
	item := mustGetPyrusEvent(t, env, "11111111-0000-4000-8000-000000000002")
	if item.Status != pyrus.IncomingEventStatusDone || item.WaitStartedAt != nil || item.NextRetryAt != nil {
		t.Fatalf("ожидали done без следов ожидания, получили %+v", item)
	}
	if countPyrusTaskTickets(t, env, task.ID) != 1 {
		t.Fatalf("ожидали ровно один тикет")
	}
}

func TestPyrusIncomingService_ReplayTaskAssemblesTicketFromAllEvents(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	const taskID = int64(7003)

	// В Pyrus уже три комментария, а каждый webhook-снимок содержит только свой.
	c1 := pyrusTestComment(31, "Первый комментарий")
	c2 := pyrusTestComment(32, "Второй комментарий")
	c3 := pyrusTestComment(33, "Третий комментарий")
	actual := pyrusTestTask(env, taskID, "CRM-ASSEMBLE", c1, c2, c3)
	env.api.setTask(&actual)

	base := time.Now().Add(-time.Hour)
	ids := []string{
		"11111111-0000-4000-8000-000000000031",
		"11111111-0000-4000-8000-000000000032",
		"11111111-0000-4000-8000-000000000033",
	}
	for i, comment := range []pyrusplugin.Comment{c1, c2, c3} {
		insertPyrusTestEvent(t, env, ids[i], base.Add(time.Duration(i)*time.Minute), pyrusTestTask(env, taskID, "CRM-ASSEMBLE", comment))
	}
	for _, id := range ids {
		env.incoming.processIncomingEvent(ctx, id)
		if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusWaiting {
			t.Fatalf("событие %s: ожидали waiting, получили %q", id, got)
		}
	}

	// Оператор проставил CRMID у сервера и повторил задачу целиком.
	ownerID := createCompanyRecord(t, env.db, "company-assemble", "Компания")
	createServerRecord(t, env.db, "CRM-ASSEMBLE", ownerID)

	replayed, err := env.incoming.ReplayTask(ctx, taskID)
	if err != nil {
		t.Fatalf("ReplayTask вернул ошибку: %v", err)
	}
	if replayed != 3 {
		t.Fatalf("ожидали повтор трёх событий, получили %d", replayed)
	}
	for _, id := range ids {
		item := mustGetPyrusEvent(t, env, id)
		if item.ReplayCount != 1 || item.Attempts != 0 || item.WaitStartedAt != nil {
			t.Fatalf("событие %s после replay: %+v", id, item)
		}
	}

	// Порядок обработки не важен: первым берём среднее событие.
	for _, id := range []string{ids[1], ids[0], ids[2]} {
		env.incoming.processIncomingEvent(ctx, id)
		if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusDone {
			t.Fatalf("событие %s: ожидали done, получили %q", id, got)
		}
	}

	if got := countPyrusTaskTickets(t, env, taskID); got != 1 {
		t.Fatalf("ожидали один тикет, получили %d", got)
	}
	ticket, err := env.ticketRepo.GetByServiceDeskUUID(ctx, pyrusTicketServiceDeskUUID(taskID))
	if err != nil || ticket == nil {
		t.Fatalf("не удалось получить тикет: %v", err)
	}
	var comments int64
	if err := env.db.Model(&tickets.TicketComment{}).Where("ticket_id = ?", ticket.ID).Count(&comments).Error; err != nil {
		t.Fatalf("не удалось посчитать комментарии: %v", err)
	}
	if comments != 3 {
		t.Fatalf("ожидали три комментария из Pyrus, получили %d", comments)
	}
	link, err := env.pyrusRepo.GetTicketLinkByTaskID(ctx, taskID)
	if err != nil || link == nil || link.TicketID != ticket.ID {
		t.Fatalf("ожидали связку задачи с тикетом, получили %+v err=%v", link, err)
	}

	again, err := env.incoming.ReplayTask(ctx, taskID)
	if err != nil || again != 0 {
		t.Fatalf("после успешной обработки повторять нечего: replayed=%d err=%v", again, err)
	}
}

func TestPyrusIncomingService_SecondEventWithoutExtIDDoesNotDuplicateTicket(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	const taskID = int64(7004)
	ownerID := createCompanyRecord(t, env.db, "company-dup", "Компания")
	createServerRecord(t, env.db, "CRM-DUP", ownerID)

	c1 := pyrusTestComment(41, "Первый")
	c2 := pyrusTestComment(42, "Второй")
	actual := pyrusTestTask(env, taskID, "CRM-DUP", c1, c2)
	env.api.setTask(&actual)

	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000041", time.Now().Add(-time.Minute), pyrusTestTask(env, taskID, "CRM-DUP", c1))
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000042", time.Now(), pyrusTestTask(env, taskID, "CRM-DUP", c2))

	// В снимках поле ext_id пустое, потому что Pyrus ещё не получил идентификатор тикета.
	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000041")
	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000042")

	if got := countPyrusTaskTickets(t, env, taskID); got != 1 {
		t.Fatalf("повторное событие создало дубль тикета, тикетов: %d", got)
	}
	for _, id := range []string{"11111111-0000-4000-8000-000000000041", "11111111-0000-4000-8000-000000000042"} {
		if got := mustGetPyrusEvent(t, env, id).Status; got != pyrus.IncomingEventStatusDone {
			t.Fatalf("событие %s: ожидали done, получили %q", id, got)
		}
	}
}

func TestPyrusIncomingService_ResumesInterruptedTicketCreation(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	const taskID = int64(7005)
	ownerID := createCompanyRecord(t, env.db, "company-resume", "Компания")
	createServerRecord(t, env.db, "CRM-RESUME", ownerID)

	// Первая попытка успела создать тикет, но упала до сохранения связки.
	partial, err := env.ticketService.CreateFromPyrus(ctx, TicketCreateFromPyrusInput{
		TaskID:    taskID,
		CompanyID: ownerID,
		Subject:   "Начатый тикет",
		Status:    tickets.StatusNew,
	})
	if err != nil {
		t.Fatalf("не удалось подготовить начатый тикет: %v", err)
	}

	actual := pyrusTestTask(env, taskID, "CRM-RESUME", pyrusTestComment(51, "Комментарий"))
	env.api.setTask(&actual)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000051", time.Now(), actual)

	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000051")

	if got := countPyrusTaskTickets(t, env, taskID); got != 1 {
		t.Fatalf("ожидали достройку существующего тикета, тикетов: %d", got)
	}
	link, err := env.pyrusRepo.GetTicketLinkByTaskID(ctx, taskID)
	if err != nil || link == nil || link.TicketID != partial.ID {
		t.Fatalf("ожидали связку на начатый тикет, получили %+v err=%v", link, err)
	}
}

func TestPyrusIncomingService_PyrusAPIFailureWaitsAndMissingTaskIsIgnored(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	task := pyrusTestTask(env, 7006, "CRM-API", pyrusTestComment(61, "Комментарий"))
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000061", time.Now(), task)

	env.api.getErr = errors.New("connection refused")
	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000061")
	item := mustGetPyrusEvent(t, env, "11111111-0000-4000-8000-000000000061")
	if item.Status != pyrus.IncomingEventStatusWaiting || item.LastError == nil || !strings.Contains(*item.LastError, "Pyrus API") {
		t.Fatalf("при недоступном Pyrus API ожидали waiting с причиной, получили %+v", item)
	}

	env.api.getErr = nil // заглушка вернёт 404: задачи нет в Pyrus
	if err := env.pyrusRepo.MarkIncomingWaiting(ctx, item.ID, "причина", *item.WaitStartedAt, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("не удалось сдвинуть срок повтора: %v", err)
	}
	env.incoming.processIncomingEvent(ctx, item.ID)
	if got := mustGetPyrusEvent(t, env, item.ID).Status; got != pyrus.IncomingEventStatusIgnored {
		t.Fatalf("ожидали ignored для отсутствующей задачи, получили %q", got)
	}
}

func TestPyrusIncomingService_WaitingExpiresAfterMaxAge(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	env.cfg.PyrusIncomingMaxAttempts = 4
	env.cfg.PyrusIncomingWaitMaxAge = 24 * time.Hour
	task := pyrusTestTask(env, 7007, "CRM-EXPIRE", pyrusTestComment(71, "Комментарий"))
	env.api.setTask(&task)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000071", time.Now().Add(-48*time.Hour), task)

	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000071")
	item := mustGetPyrusEvent(t, env, "11111111-0000-4000-8000-000000000071")
	if item.Status != pyrus.IncomingEventStatusWaiting {
		t.Fatalf("ожидали waiting, получили %q", item.Status)
	}

	started := time.Now().Add(-25 * time.Hour)
	if err := env.pyrusRepo.MarkIncomingWaiting(ctx, item.ID, "причина", started, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("не удалось состарить ожидание: %v", err)
	}
	env.incoming.processIncomingEvent(ctx, item.ID)
	item = mustGetPyrusEvent(t, env, item.ID)
	if item.Status != pyrus.IncomingEventStatusFailed || item.Attempts != 4 {
		t.Fatalf("ожидали failed с исчерпанными попытками, получили status=%q attempts=%d", item.Status, item.Attempts)
	}
	if item.LastError == nil || !strings.Contains(*item.LastError, "ожидание данных прекращено") {
		t.Fatalf("ожидали причину прекращения ожидания, получили %v", item.LastError)
	}
	due, err := env.pyrusRepo.ListIncomingDueForProcessing(ctx, 10, 4)
	if err != nil || len(due) != 0 {
		t.Fatalf("истёкшее событие не должно повторяться автоматически: due=%d err=%v", len(due), err)
	}
}

func TestPyrusIncomingService_WaitRetryDelayGrowsWithinBounds(t *testing.T) {
	service := &pyrusIncomingService{cfg: &config.Config{
		PyrusIncomingWaitRetryBase: time.Minute,
		PyrusIncomingWaitRetryMax:  30 * time.Minute,
	}}
	cases := []struct {
		waited time.Duration
		want   time.Duration
	}{
		{0, time.Minute},
		{3 * time.Minute, time.Minute},
		{time.Hour, 12 * time.Minute},
		{24 * time.Hour, 30 * time.Minute},
	}
	for _, tc := range cases {
		if got := service.waitRetryDelay(tc.waited); got != tc.want {
			t.Fatalf("waited=%s: ожидали %s, получили %s", tc.waited, tc.want, got)
		}
	}
}

func TestPyrusIncomingService_ReplayProblemTasksSkipsHealthyTasks(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	problem := pyrusTestTask(env, 7101, "CRM-NONE", pyrusTestComment(81, "Комментарий"))
	healthy := pyrusTestTask(env, 7102, "CRM-OK", pyrusTestComment(82, "Комментарий"))
	env.api.setTask(&problem)
	env.api.setTask(&healthy)
	ownerID := createCompanyRecord(t, env.db, "company-problem", "Компания")
	createServerRecord(t, env.db, "CRM-OK", ownerID)

	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000081", time.Now(), problem)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000082", time.Now(), healthy)
	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000081")
	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000082")

	tasks, events, err := env.incoming.ReplayProblemTasks(ctx)
	if err != nil {
		t.Fatalf("ReplayProblemTasks вернул ошибку: %v", err)
	}
	if tasks != 1 || events != 1 {
		t.Fatalf("ожидали повтор одной задачи и одного события, получили tasks=%d events=%d", tasks, events)
	}
	if got := mustGetPyrusEvent(t, env, "11111111-0000-4000-8000-000000000082"); got.Status != pyrus.IncomingEventStatusDone || got.ReplayCount != 0 {
		t.Fatalf("успешное событие не должно повторяться: %+v", got)
	}
}

func TestPyrusIncomingTaskGroups_AggregateByTaskWithTicket(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	ownerID := createCompanyRecord(t, env.db, "company-groups", "Компания")
	createServerRecord(t, env.db, "CRM-GROUP", ownerID)

	base := time.Now().Add(-time.Hour)
	waitingTask := pyrusTestTask(env, 7201, "CRM-MISSING", pyrusTestComment(91, "Комментарий"))
	doneTask := pyrusTestTask(env, 7202, "CRM-GROUP", pyrusTestComment(92, "Комментарий"))
	env.api.setTask(&waitingTask)
	env.api.setTask(&doneTask)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000091", base, waitingTask)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000092", base.Add(time.Minute), waitingTask)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000093", base.Add(2*time.Minute), doneTask)
	for _, id := range []string{"11111111-0000-4000-8000-000000000091", "11111111-0000-4000-8000-000000000092", "11111111-0000-4000-8000-000000000093"} {
		env.incoming.processIncomingEvent(ctx, id)
	}

	problem, total, err := env.pyrusRepo.ListIncomingTaskGroups(ctx, pyrus.IncomingTaskGroupFilter{OnlyProblem: true, Limit: 10})
	if err != nil {
		t.Fatalf("не удалось получить сводку проблемных задач: %v", err)
	}
	if total != 1 || len(problem) != 1 {
		t.Fatalf("ожидали одну проблемную задачу, получили total=%d len=%d", total, len(problem))
	}
	group := problem[0]
	if group.PyrusTaskID != 7201 || group.EventsTotal != 2 || group.WaitingCount != 2 || group.TicketID != nil {
		t.Fatalf("неверная сводка проблемной задачи: %+v", group)
	}
	if group.LastEventID != "11111111-0000-4000-8000-000000000092" || group.LastError == nil || group.NextRetryAt == nil {
		t.Fatalf("ожидали последнее событие, причину и срок повтора: %+v", group)
	}

	all, total, err := env.pyrusRepo.ListIncomingTaskGroups(ctx, pyrus.IncomingTaskGroupFilter{Limit: 10})
	if err != nil || total != 2 || len(all) != 2 {
		t.Fatalf("ожидали две задачи в полной сводке: total=%d len=%d err=%v", total, len(all), err)
	}
	if all[0].PyrusTaskID != 7202 || all[0].TicketID == nil || all[0].DoneCount != 1 {
		t.Fatalf("ожидали первой задачу с тикетом и done-событием: %+v", all[0])
	}

	items, _, err := NewIntegrationSyncControlService(env.pyrusRepo, env.incoming, nil, nil).ListIncomingTasks(ctx, "pyrus", PyrusIncomingTaskListFilter{OnlyProblem: true, Limit: 10})
	if err != nil || len(items) != 1 {
		t.Fatalf("не удалось получить сводку через сервис: len=%d err=%v", len(items), err)
	}
	if items[0].CRMID != "CRM-MISSING" || items[0].Subject != "Не открывается смена" || !items[0].NeedsAttention {
		t.Fatalf("ожидали CRMID и тему из последнего снимка: %+v", items[0])
	}
}

func TestRedactPyrusPayload_HidesAccessToken(t *testing.T) {
	redacted := redactPyrusPayload(`{"event":"comment","access_token":"secret-token-value","task_id":1}`)
	if strings.Contains(redacted, "secret-token-value") || !strings.Contains(redacted, `"task_id":1`) {
		t.Fatalf("access_token должен быть скрыт, остальное сохранено: %s", redacted)
	}
	broken := redactPyrusPayload(`{"access_token":"secret-token-value", oops`)
	if strings.Contains(broken, "secret-token-value") {
		t.Fatalf("access_token должен скрываться и в повреждённом JSON: %s", broken)
	}
	if untouched := `{"event":"comment"}`; redactPyrusPayload(untouched) != untouched {
		t.Fatalf("payload без токена не должен меняться")
	}
}

// Сквозная проверка на реальном Redis: webhook -> Redis Streams -> ожидание данных -> автоповтор после появления сервера -> ручной replay.
func TestPyrusIncomingService_RedisPipelineRetriesWaitingEventsAndReplaysFailed(t *testing.T) {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("не задана переменная TEST_REDIS_ADDR")
	}
	redisClient := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = redisClient.Close() })
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis недоступен: %v", err)
	}

	env := newPyrusTestEnv(t, false)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	env.cfg.PyrusEventsStreamName = "pyrus:events:test:" + suffix
	env.cfg.PyrusEventsConsumerGroup = "pyrus-workers-test-" + suffix
	env.cfg.PyrusIncomingWaitRetryBase = 300 * time.Millisecond
	env.cfg.PyrusIncomingWaitRetryMax = 300 * time.Millisecond
	t.Cleanup(func() { redisClient.Del(ctx, env.cfg.PyrusEventsStreamName) })

	service, ok := NewPyrusIncomingService(
		env.cfg, env.log, env.api, redisClient, env.ticketRepo, env.ticketService, env.userRepo, env.serverRepo, env.pyrusRepo, env.bus,
	).(*pyrusIncomingService)
	if !ok {
		t.Fatalf("не удалось привести входящий сервис Pyrus к concrete type")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Start(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	const taskID = int64(7301)
	c1 := pyrusTestComment(101, "Первый")
	c2 := pyrusTestComment(102, "Второй")
	actual := pyrusTestTask(env, taskID, "CRM-REDIS", c1, c2)
	env.api.setTask(&actual)
	waitForCondition(t, 5*time.Second, func() bool {
		return redisClient.XInfoGroups(ctx, env.cfg.PyrusEventsStreamName).Err() == nil
	})

	for _, comment := range []pyrusplugin.Comment{c1, c2} {
		raw := mustPyrusJSON(t, pyrusplugin.WebhookPayload{Event: "comment", TaskID: taskID, Task: pyrusTestTask(env, taskID, "CRM-REDIS", comment)})
		if err := service.HandleWebhook(ctx, raw, signPyrusPayload(env.cfg, raw)); err != nil {
			t.Fatalf("HandleWebhook вернул ошибку: %v", err)
		}
	}

	listStatuses := func() map[string]int {
		items, _, err := env.pyrusRepo.ListIncomingEvents(ctx, pyrus.IncomingEventListFilter{TaskID: taskID, Limit: 10})
		if err != nil {
			t.Fatalf("не удалось получить события: %v", err)
		}
		result := map[string]int{}
		for _, item := range items {
			result[item.Status]++
		}
		return result
	}
	waitForCondition(t, 10*time.Second, func() bool { return listStatuses()[pyrus.IncomingEventStatusWaiting] == 2 })

	// Сервер с CRMID появился: события обрабатываются сами, вручную ничего толкать не нужно.
	ownerID := createCompanyRecord(t, env.db, "company-redis", "Компания")
	createServerRecord(t, env.db, "CRM-REDIS", ownerID)
	waitForCondition(t, 20*time.Second, func() bool { return listStatuses()[pyrus.IncomingEventStatusDone] == 2 })

	if got := countPyrusTaskTickets(t, env, taskID); got != 1 {
		t.Fatalf("ожидали один тикет, получили %d", got)
	}
	ticket, err := env.ticketRepo.GetByServiceDeskUUID(ctx, pyrusTicketServiceDeskUUID(taskID))
	if err != nil || ticket == nil {
		t.Fatalf("не удалось получить тикет: %v", err)
	}
	var comments int64
	if err := env.db.Model(&tickets.TicketComment{}).Where("ticket_id = ?", ticket.ID).Count(&comments).Error; err != nil || comments != 2 {
		t.Fatalf("ожидали два комментария, получили %d err=%v", comments, err)
	}

	// Событие с исчерпанными попытками возвращается в работу ручным replay задачи.
	items, _, err := env.pyrusRepo.ListIncomingEvents(ctx, pyrus.IncomingEventListFilter{TaskID: taskID, Limit: 10})
	if err != nil || len(items) == 0 {
		t.Fatalf("не удалось получить события: %v", err)
	}
	if err := env.pyrusRepo.MarkIncomingExpired(ctx, items[0].ID, "ручная пометка", service.maxAttempts()); err != nil {
		t.Fatalf("не удалось пометить событие failed: %v", err)
	}
	replayed, err := service.ReplayTask(ctx, taskID)
	if err != nil || replayed != 1 {
		t.Fatalf("ожидали replay одного события: replayed=%d err=%v", replayed, err)
	}
	waitForCondition(t, 10*time.Second, func() bool { return listStatuses()[pyrus.IncomingEventStatusDone] == 2 })
	if got := countPyrusTaskTickets(t, env, taskID); got != 1 {
		t.Fatalf("replay не должен создавать дубль тикета, тикетов: %d", got)
	}
}
