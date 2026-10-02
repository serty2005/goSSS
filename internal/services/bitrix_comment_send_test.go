package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/repositories"
	"etalon-server/internal/infra/testdb"

	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	testIntegrationUserID = int64(457)
	testDealID            = int64(6299)
)

// fakeBitrixTimeline имитирует таймлайн сделки Bitrix24 и управляемо ломает ответы на crm.timeline.comment.add.
type fakeBitrixTimeline struct {
	mu       sync.Mutex
	nextID   int64
	comments []map[string]any
	addCalls int
	// addMode возвращает поведение для n-го вызова add: "ok", "create_then_hang" (создал, но ответил после таймаута), "hang" (не создал, ответил после таймаута).
	addMode func(call int) string
	hang    time.Duration
}

func newFakeBitrixTimeline(addMode func(call int) string) *fakeBitrixTimeline {
	return &fakeBitrixTimeline{nextID: 310600, addMode: addMode, hang: 600 * time.Millisecond}
}

func (f *fakeBitrixTimeline) addCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addCalls
}

func (f *fakeBitrixTimeline) commentsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.comments)
}

func (f *fakeBitrixTimeline) handler(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch {
	case strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.add.json"):
		fields, _ := body["fields"].(map[string]any)
		f.mu.Lock()
		f.addCalls++
		call := f.addCalls
		mode := "ok"
		if f.addMode != nil {
			mode = f.addMode(call)
		}
		var id int64
		if mode != "hang" {
			f.nextID++
			id = f.nextID
			f.comments = append(f.comments, map[string]any{
				"ID":          id,
				"COMMENT":     fields["COMMENT"],
				"AUTHOR_ID":   testIntegrationUserID,
				"ENTITY_TYPE": "deal",
				"ENTITY_ID":   testDealID,
				"CREATED":     time.Now().Format(time.RFC3339),
			})
		}
		hang := f.hang
		f.mu.Unlock()
		if mode != "ok" {
			time.Sleep(hang)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": id})
	case strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.list.json"):
		f.mu.Lock()
		items := append([]map[string]any(nil), f.comments...)
		f.mu.Unlock()
		if order, _ := body["order"].(map[string]any); order["ID"] == "DESC" {
			sort.Slice(items, func(i, j int) bool { return items[i]["ID"].(int64) > items[j]["ID"].(int64) })
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": items})
	case strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.update.json"):
		id := int64(body["id"].(float64))
		fields, _ := body["fields"].(map[string]any)
		f.mu.Lock()
		for _, item := range f.comments {
			if item["ID"].(int64) == id && fields["COMMENT"] != nil {
				item["COMMENT"] = fields["COMMENT"]
			}
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	case strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.get.json"):
		id := int64(body["id"].(float64))
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, item := range f.comments {
			if item["ID"].(int64) == id {
				_ = json.NewEncoder(w).Encode(map[string]any{"result": item})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": nil})
	default:
		http.Error(w, "unexpected method", http.StatusNotFound)
	}
}

// openCommentSendTestDB открывает отдельную файловую БД на каждый прогон; одно соединение исключает блокировки sqlite между параллельными записями.
func commentSendTestModels() []any {
	return []any{
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
		&bitrix.CommentSendState{},
		&bitrix.UserMap{},
		&bitrix.UserCache{},
		&bitrix.IgnoredDeal{},
		&bitrix.CompanyServicePointMapping{},
	}
}

// openCommentSendTestDB открывает реальный PostgreSQL при заданном TEST_POSTGRES_DSN, иначе отдельную файловую sqlite на каждый прогон.
func openCommentSendTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testdb.PostgresDSN() != "" {
		return testdb.OpenPostgres(t, commentSendTestModels()...)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "comments.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("не удалось получить пул соединений: %v", err)
	}
	// Одно соединение исключает блокировки sqlite между параллельными записями.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(commentSendTestModels()...); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	return db
}

type commentSendTestEnv struct {
	redis    *redis.Client
	svc      *bitrixSyncService
	incoming *bitrixIncomingService
	repo     bitrix.Repository
	fake     *fakeBitrixTimeline
	ticket   *tickets.Ticket
	userID   uint
}

func newCommentSendTestEnv(t *testing.T, addMode func(call int) string) *commentSendTestEnv {
	t.Helper()
	return newCommentSendTestEnvWithRedis(t, addMode, nil)
}

func newCommentSendTestEnvWithRedis(t *testing.T, addMode func(call int) string, redisClient *redis.Client) *commentSendTestEnv {
	t.Helper()
	ctx := context.Background()

	db := openCommentSendTestDB(t)
	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	admin := createAdminUserForTicketDeleteTests(t, ctx, userRepo)
	companyID := createCompanyForTicketDeleteTests(t, ctx, companyRepo)
	if err := bitrixRepo.UpsertUserMap(ctx, &bitrix.UserMap{EtalonUserID: admin.ID, B24UserID: 465}); err != nil {
		t.Fatalf("не удалось создать user_map: %v", err)
	}

	pointID := int64(16961)
	ticket := &tickets.Ticket{
		Subject:              "Тикет для комментариев",
		Description:          "Описание",
		Status:               tickets.StatusNew,
		Priority:             tickets.PriorityMedium,
		Type:                 tickets.TypeIncident,
		CompanyID:            companyID,
		ServiceDeskUUID:      "b24:deal:6299",
		SyncWithBitrix:       true,
		BitrixServicePointID: &pointID,
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if err := bitrixRepo.UpsertDealLink(ctx, &bitrix.DealLink{TicketID: ticket.ID, B24DealID: testDealID, LastSyncAt: time.Now()}); err != nil {
		t.Fatalf("не удалось создать deal_link: %v", err)
	}

	fake := newFakeBitrixTimeline(addMode)
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)

	cfg := &config.Config{
		EnableBitrixGateway:       true,
		RequestTimeout:            200 * time.Millisecond,
		BitrixBaseURL:             server.URL + "/rest/457/secret",
		BitrixIntegrationUserID:   testIntegrationUserID,
		BitrixCommentConfirmGrace: time.Millisecond,
		BitrixSuppressTTL:         20 * time.Second,
	}
	log := logger.New("", "test", "error", true)
	client := b24.NewClient(cfg, log)

	return &commentSendTestEnv{
		redis: redisClient,
		svc: &bitrixSyncService{
			cfg:        cfg,
			log:        log,
			client:     client,
			redis:      redisClient,
			repo:       bitrixRepo,
			ticketRepo: ticketRepo,
			userRepo:   userRepo,
		},
		incoming: &bitrixIncomingService{
			cfg:        cfg,
			log:        log,
			client:     client,
			redis:      redisClient,
			repo:       bitrixRepo,
			ticketRepo: ticketRepo,
			userRepo:   userRepo,
		},
		repo:   bitrixRepo,
		fake:   fake,
		ticket: ticket,
		userID: admin.ID,
	}
}

func (e *commentSendTestEnv) addTicketComment(t *testing.T, id string, text string) *tickets.TicketComment {
	t.Helper()
	userID := e.userID
	comment := tickets.TicketComment{
		ID:              id,
		TicketID:        e.ticket.ID,
		ServiceDeskUUID: id,
		Text:            text,
		AuthorName:      "Оператор",
		AuthorUserID:    &userID,
		Source:          tickets.CommentSourceUI,
		CreationDate:    time.Now(),
	}
	if err := e.svc.ticketRepo.AddComments(context.Background(), []tickets.TicketComment{comment}); err != nil {
		t.Fatalf("не удалось создать комментарий: %v", err)
	}
	return &comment
}

func (e *commentSendTestEnv) send(comment *tickets.TicketComment) error {
	return e.svc.SyncComment(context.Background(), e.ticket.ID, comment, e.userID)
}

func (e *commentSendTestEnv) sendState(t *testing.T, commentID string) *bitrix.CommentSendState {
	t.Helper()
	state, err := e.repo.GetCommentSendState(context.Background(), commentID)
	if err != nil || state == nil {
		t.Fatalf("состояние отправки %s не найдено: %v", commentID, err)
	}
	return state
}

// Воспроизводит инцидент 02.10: Bitrix24 создал комментарий, но ответил после таймаута.
// Повторной отправки быть не должно, а сверка подтверждает уже созданный комментарий.
func TestSyncComment_TimeoutDoesNotDuplicateAndReconcileAdopts(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "create_then_hang" })
	comment := env.addTicketComment(t, "c-1", "<p>Почему это заняло так много времени?</p>")

	err := env.send(comment)
	if !errors.Is(err, b24.ErrResultUnknown) {
		t.Fatalf("ожидалась ErrResultUnknown, получено %v", err)
	}
	if got := env.sendState(t, comment.ID).Status; got != bitrix.CommentSendStatusAmbiguous {
		t.Fatalf("ожидался статус ambiguous, получен %q", got)
	}

	// Синхронизация тикета (например, после смены исполнителя) не должна отправлять комментарий повторно.
	if err := env.svc.syncPendingComments(context.Background(), env.ticket, testDealID); err != nil {
		t.Fatalf("syncPendingComments завершился ошибкой: %v", err)
	}
	if err := env.send(comment); err != nil {
		t.Fatalf("повторный SyncComment не должен возвращать ошибку: %v", err)
	}
	if got := env.fake.addCount(); got != 1 {
		t.Fatalf("ожидался один вызов crm.timeline.comment.add, получено %d", got)
	}

	time.Sleep(10 * time.Millisecond)
	handled, err := env.svc.ReconcileCommentSends(context.Background())
	if err != nil || handled != 1 {
		t.Fatalf("сверка должна обработать одну отправку: handled=%d err=%v", handled, err)
	}

	state := env.sendState(t, comment.ID)
	if state.Status != bitrix.CommentSendStatusConfirmed || state.B24CommentID == nil || *state.B24CommentID != 310601 {
		t.Fatalf("ожидалось подтверждение комментарием 310601, получено %+v", state)
	}
	link, err := env.repo.GetCommentLinkByEtalonID(context.Background(), comment.ID)
	if err != nil || link == nil || link.B24CommentID != 310601 || link.Direction != "etalon_to_b24" {
		t.Fatalf("связь после сверки записана неверно: %+v err=%v", link, err)
	}
	if got := env.fake.addCount(); got != 1 {
		t.Fatalf("сверка не должна отправлять комментарий заново, вызовов add: %d", got)
	}
	if got := env.fake.commentsCount(); got != 1 {
		t.Fatalf("в Bitrix24 должен остаться один комментарий, получено %d", got)
	}
}

// Если комментария в сделке достоверно нет, сверка повторяет отправку и подтверждает её.
func TestReconcile_AbsentCommentIsResentOnce(t *testing.T) {
	env := newCommentSendTestEnv(t, func(call int) string {
		if call == 1 {
			return "hang"
		}
		return "ok"
	})
	comment := env.addTicketComment(t, "c-1", "<p>Комментарий, который не дошёл</p>")

	if err := env.send(comment); !errors.Is(err, b24.ErrResultUnknown) {
		t.Fatalf("ожидалась ErrResultUnknown, получено %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if handled, err := env.svc.ReconcileCommentSends(context.Background()); err != nil || handled != 1 {
		t.Fatalf("сверка должна обработать одну отправку: handled=%d err=%v", handled, err)
	}

	state := env.sendState(t, comment.ID)
	if state.Status != bitrix.CommentSendStatusConfirmed || state.Attempts != 2 {
		t.Fatalf("ожидалось подтверждение со второй попытки, получено %+v", state)
	}
	if got := env.fake.addCount(); got != 2 {
		t.Fatalf("ожидалось 2 вызова add, получено %d", got)
	}
	if got := env.fake.commentsCount(); got != 1 {
		t.Fatalf("в Bitrix24 должен быть один комментарий, получено %d", got)
	}
}

// Параллельные источники отправки (событие комментария, синхронизация тикета, повтор события) создают один комментарий.
func TestSendNewComment_ConcurrentSourcesSendOnce(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "ok" })
	comment := env.addTicketComment(t, "c-1", "<p>Один комментарий</p>")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if i%2 == 0 {
				err = env.send(comment)
			} else {
				err = env.svc.syncPendingComments(context.Background(), env.ticket, testDealID)
			}
			if err != nil {
				t.Errorf("отправка %d завершилась ошибкой: %v", i, err)
			}
		}()
	}
	wg.Wait()

	if got := env.fake.addCount(); got != 1 {
		t.Fatalf("ожидался один вызов add, получено %d", got)
	}
	if got := env.sendState(t, comment.ID).Status; got != bitrix.CommentSendStatusConfirmed {
		t.Fatalf("ожидался статус confirmed, получен %q", got)
	}
}

// Лимит попыток исключает бесконечные повторы и не блокирует остальные комментарии тикета.
func TestSyncPendingComments_ExhaustedCommentDoesNotBlockOthers(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "ok" })
	ctx := context.Background()
	stuck := env.addTicketComment(t, "c-1", "<p>Исчерпал попытки</p>")
	env.addTicketComment(t, "c-2", "<p>Следующий комментарий</p>")

	for range bitrixCommentMaxSendAttempts {
		_, claimed, err := env.repo.ClaimCommentSend(ctx, &bitrix.CommentSendState{EtalonCommentID: stuck.ID, TicketID: env.ticket.ID, B24DealID: testDealID}, bitrixCommentMaxSendAttempts)
		if err != nil || !claimed {
			t.Fatalf("не удалось зарезервировать отправку: claimed=%v err=%v", claimed, err)
		}
		if err := env.repo.MarkCommentSendRejected(ctx, stuck.ID, "ошибка"); err != nil {
			t.Fatalf("MarkCommentSendRejected: %v", err)
		}
	}

	if err := env.svc.syncPendingComments(ctx, env.ticket, testDealID); err != nil {
		t.Fatalf("syncPendingComments завершился ошибкой: %v", err)
	}
	if got := env.fake.addCount(); got != 1 {
		t.Fatalf("должен быть отправлен только второй комментарий, вызовов add: %d", got)
	}
	if got := env.sendState(t, "c-2").Status; got != bitrix.CommentSendStatusConfirmed {
		t.Fatalf("второй комментарий должен быть подтверждён, получен %q", got)
	}
}

// Вебхук ADD по собственному комментарию, пришедший до подтверждения, связывается с отправкой и не импортируется как чужой.
func TestIncomingCommentAdd_AdoptsOpenOutgoingComment(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "create_then_hang" })
	ctx := context.Background()
	comment := env.addTicketComment(t, "c-1", "<p>Почему это заняло так много времени?</p>")

	if err := env.send(comment); !errors.Is(err, b24.ErrResultUnknown) {
		t.Fatalf("ожидалась ErrResultUnknown, получено %v", err)
	}

	status, reason, err := env.incoming.handleTimelineCommentAdd(ctx, 310601)
	if err != nil {
		t.Fatalf("handleTimelineCommentAdd завершился ошибкой: %v", err)
	}
	if status != bitrix.IncomingEventStatusIgnored {
		t.Fatalf("ожидался статус ignored, получено %q (%s)", status, reason)
	}

	link, err := env.repo.GetCommentLinkByB24ID(ctx, 310601)
	if err != nil || link == nil || link.EtalonCommentID != comment.ID || link.Direction != "etalon_to_b24" {
		t.Fatalf("комментарий должен быть связан с отправкой ServiceDesk: %+v err=%v", link, err)
	}
	all, err := env.svc.ticketRepo.GetComments(ctx, env.ticket.ID)
	if err != nil {
		t.Fatalf("GetComments: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("в тикете не должно появиться чужих копий, получено %d комментариев", len(all))
	}

	// Повторная доставка того же события остаётся безвредной.
	status, _, err = env.incoming.handleTimelineCommentAdd(ctx, 310601)
	if err != nil || status != bitrix.IncomingEventStatusIgnored {
		t.Fatalf("повторное событие должно игнорироваться: status=%q err=%v", status, err)
	}
}

// Настоящий чужой комментарий из Bitrix24 по-прежнему импортируется, а повторная доставка события не создаёт дубль.
func TestIncomingCommentAdd_ImportsForeignCommentOnce(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "ok" })
	ctx := context.Background()

	env.fake.mu.Lock()
	env.fake.comments = append(env.fake.comments, map[string]any{
		"ID":          int64(500),
		"COMMENT":     "Комментарий менеджера из Bitrix24",
		"AUTHOR_ID":   int64(900),
		"ENTITY_TYPE": "deal",
		"ENTITY_ID":   testDealID,
	})
	env.fake.mu.Unlock()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := env.incoming.handleTimelineCommentAdd(ctx, 500); err != nil {
				t.Errorf("handleTimelineCommentAdd завершился ошибкой: %v", err)
			}
		}()
	}
	wg.Wait()

	all, err := env.svc.ticketRepo.GetComments(ctx, env.ticket.ID)
	if err != nil {
		t.Fatalf("GetComments: %v", err)
	}
	if len(all) != 1 || all[0].ID != "b24-500" {
		t.Fatalf("ожидался один импортированный комментарий b24-500, получено %+v", all)
	}
}

// Запоздавший вебхук изменения с текстом, совпадающим с отправленной версией, не перезаписывает комментарий ServiceDesk.
func TestIncomingCommentUpdate_IgnoresOutgoingEchoAfterSuppressExpiry(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "ok" })
	ctx := context.Background()
	comment := env.addTicketComment(t, "c-1", "<p>Исходный текст</p>")

	if err := env.send(comment); err != nil {
		t.Fatalf("SyncComment завершился ошибкой: %v", err)
	}
	status, reason, err := env.incoming.handleTimelineCommentUpdate(ctx, 310601)
	if err != nil {
		t.Fatalf("handleTimelineCommentUpdate завершился ошибкой: %v", err)
	}
	if status != bitrix.IncomingEventStatusIgnored {
		t.Fatalf("эхо собственного комментария должно игнорироваться: status=%q reason=%q", status, reason)
	}

	// Правка в Bitrix24 с другим текстом применяется.
	env.fake.mu.Lock()
	env.fake.comments[0]["COMMENT"] = "Текст, исправленный менеджером"
	env.fake.mu.Unlock()
	status, _, err = env.incoming.handleTimelineCommentUpdate(ctx, 310601)
	if err != nil || status != bitrix.IncomingEventStatusDone {
		t.Fatalf("реальная правка из Bitrix24 должна применяться: status=%q err=%v", status, err)
	}
}

// Реальный Redis: ключ подавления живет ограниченное время, а вебхук может прийти позже. Собственный комментарий
// должен распознаваться по связи в БД, а не только по TTL (причина дубля 02.10).
func TestIncomingCommentAdd_OwnCommentIgnoredAfterSuppressKeyExpired(t *testing.T) {
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

	env := newCommentSendTestEnvWithRedis(t, func(int) string { return "ok" }, redisClient)
	comment := env.addTicketComment(t, "c-1", "<p>Собственный комментарий</p>")
	if err := env.send(comment); err != nil {
		t.Fatalf("SyncComment завершился ошибкой: %v", err)
	}

	key := "b24:suppress:comment:310601"
	t.Cleanup(func() { redisClient.Del(ctx, key) })
	if exists, _ := redisClient.Exists(ctx, key).Result(); exists != 1 {
		t.Fatalf("после отправки должен стоять ключ подавления %s", key)
	}
	status, reason, err := env.incoming.handleTimelineCommentAdd(ctx, 310601)
	if err != nil || status != bitrix.IncomingEventStatusIgnored || reason != "подавлено anti-loop ключом" {
		t.Fatalf("при живом ключе ожидалось подавление: status=%q reason=%q err=%v", status, reason, err)
	}

	// Ключ истек до обработки вебхука.
	redisClient.Del(ctx, key)
	status, reason, err = env.incoming.handleTimelineCommentAdd(ctx, 310601)
	if err != nil || status != bitrix.IncomingEventStatusIgnored || reason != "комментарий создан самим ServiceDesk" {
		t.Fatalf("после истечения ключа ожидалось распознавание по связи: status=%q reason=%q err=%v", status, reason, err)
	}
	all, err := env.svc.ticketRepo.GetComments(ctx, env.ticket.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("дублей в тикете быть не должно: comments=%d err=%v", len(all), err)
	}
}

// Старый несвязанный комментарий с таким же текстом (например, «ок») не должен усыновляться вместо только что отправленного.
func TestReconcile_DoesNotAdoptOlderOrphanWithSameText(t *testing.T) {
	env := newCommentSendTestEnv(t, func(int) string { return "create_then_hang" })
	ctx := context.Background()
	comment := env.addTicketComment(t, "c-1", "<p>ок</p>")

	// Граница ID: до этой отметки комментарии Bitrix24 уже известны ServiceDesk.
	if err := env.repo.UpsertCommentLink(ctx, &bitrix.CommentLink{EtalonCommentID: "known", B24CommentID: 5000, TicketID: env.ticket.ID, Direction: "b24_to_etalon"}); err != nil {
		t.Fatalf("не удалось создать связь-границу: %v", err)
	}
	// Текст сироты строится так же, как при реальной отправке: с упоминанием автора.
	authorID, err := env.svc.resolveBitrixUserID(ctx, env.userID)
	if err != nil || authorID == nil {
		t.Fatalf("не определён пользователь Bitrix24: %v", err)
	}
	authorName, err := env.svc.resolveEtalonUserDisplayName(ctx, env.userID)
	if err != nil {
		t.Fatalf("resolveEtalonUserDisplayName: %v", err)
	}
	out, err := env.svc.buildOutgoingComment(ctx, env.ticket.ID, comment, authorID, authorName)
	if err != nil {
		t.Fatalf("buildOutgoingComment: %v", err)
	}
	orphanText := out.Message

	env.fake.mu.Lock()
	env.fake.comments = append(env.fake.comments, map[string]any{
		"ID": int64(100), "COMMENT": orphanText, "AUTHOR_ID": testIntegrationUserID,
		// CREATED свежий: старого сироту должна отсекать именно граница ID, а не окно времени.
		"ENTITY_TYPE": "deal", "ENTITY_ID": testDealID, "CREATED": time.Now().Format(time.RFC3339),
	})
	env.fake.mu.Unlock()

	if err := env.send(comment); !errors.Is(err, b24.ErrResultUnknown) {
		t.Fatalf("ожидалась ErrResultUnknown, получено %v", err)
	}
	if got := env.sendState(t, comment.ID).WatermarkB24ID; got != 5000 {
		t.Fatalf("ожидалась граница 5000, получено %d", got)
	}

	time.Sleep(10 * time.Millisecond)
	if _, err := env.svc.ReconcileCommentSends(ctx); err != nil {
		t.Fatalf("ReconcileCommentSends: %v", err)
	}
	link, err := env.repo.GetCommentLinkByEtalonID(ctx, comment.ID)
	if err != nil || link == nil || link.B24CommentID != 310601 {
		t.Fatalf("должен быть подтверждён только что созданный комментарий 310601, получено %+v err=%v", link, err)
	}
	if orphan, _ := env.repo.GetCommentLinkByB24ID(ctx, 100); orphan != nil {
		t.Fatalf("старый комментарий 100 не должен усыновляться: %+v", orphan)
	}
}

func TestBitrixCommentMatchesSend_WatermarkAndCreatedWindow(t *testing.T) {
	fp := bitrixCommentFingerprint("ок")
	base := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	state := bitrix.CommentSendState{B24DealID: testDealID, Fingerprint: fp, WatermarkB24ID: 5000, CreatedAt: base}
	candidate := func(id int64, created string) b24.TimelineComment {
		raw := map[string]interface{}{}
		if created != "" {
			raw["CREATED"] = created
		}
		authorID := testIntegrationUserID
		return b24.TimelineComment{ID: id, Comment: "ок", AuthorID: &authorID, EntityType: "deal", EntityID: testDealID, Raw: raw}
	}
	now := base.Add(time.Second).Format(time.RFC3339)

	cases := []struct {
		name string
		c    b24.TimelineComment
		want bool
	}{
		{"новее границы", candidate(5100, now), true},
		{"в пределах запаса границы", candidate(4960, now), true},
		{"старее границы с запасом", candidate(4900, now), false},
		{"создан задолго до отправки", candidate(5100, base.Add(-time.Hour).Format(time.RFC3339)), false},
		{"CREATED с допуском расхождения часов", candidate(5100, base.Add(-time.Minute).Format(time.RFC3339)), true},
		{"CREATED не разобрать - проверка пропускается", candidate(5100, "вчера"), true},
		{"чужая сделка", func() b24.TimelineComment { c := candidate(5100, now); c.EntityID = 1; return c }(), false},
	}
	for _, tc := range cases {
		if got := bitrixCommentMatchesSend(tc.c, state, testIntegrationUserID); got != tc.want {
			t.Errorf("%s: ожидалось %v, получено %v", tc.name, tc.want, got)
		}
	}
}
