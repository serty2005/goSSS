package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"etalon-server/internal/infra/repositories"
	"etalon-server/pkg/eventbus"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type importedCommentRecorder struct {
	mu    sync.Mutex
	items []events.TicketCommentImportedPayload
}

func (r *importedCommentRecorder) subscribe(bus eventbus.EventBus) {
	bus.Subscribe(events.TicketCommentImported, func(_ context.Context, event eventbus.Event) {
		payload, ok := event.Payload.(events.TicketCommentImportedPayload)
		if !ok {
			return
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.items = append(r.items, payload)
	})
}

func (r *importedCommentRecorder) snapshot() []events.TicketCommentImportedPayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.TicketCommentImportedPayload(nil), r.items...)
}

func TestPyrusIncomingService_PublishesImportedCommentsOnce(t *testing.T) {
	env := newPyrusTestEnv(t, true)
	ctx := context.Background()
	recorder := &importedCommentRecorder{}
	recorder.subscribe(env.bus)

	const taskID = int64(8101)
	ownerID := createCompanyRecord(t, env.db, "company-bridge-in", "Компания")
	createServerRecord(t, env.db, "CRM-BRIDGE", ownerID)
	actual := pyrusTestTask(env, taskID, "CRM-BRIDGE", pyrusTestComment(501, "Первый"), pyrusTestComment(502, "Второй"))
	env.api.setTask(&actual)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000501", time.Now(), actual)
	insertPyrusTestEvent(t, env, "11111111-0000-4000-8000-000000000502", time.Now().Add(time.Second), actual)

	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000501")
	waitForCondition(t, 2*time.Second, func() bool { return len(recorder.snapshot()) == 2 })
	for _, item := range recorder.snapshot() {
		if item.Source != events.CommentImportSourcePyrus || item.Comment == nil || item.TicketID == "" {
			t.Fatalf("неверное событие импорта: %+v", item)
		}
	}

	// Повторное событие той же задачи не публикует уже импортированные комментарии.
	env.incoming.processIncomingEvent(ctx, "11111111-0000-4000-8000-000000000502")
	time.Sleep(150 * time.Millisecond)
	if got := len(recorder.snapshot()); got != 2 {
		t.Fatalf("повторная обработка не должна публиковать комментарии заново, событий: %d", got)
	}
}

func TestPyrusIncomingService_ToManagerTicketKeepsStatusOnPyrusUpdate(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	finished := pyrusplugin.Task{
		ID:     8201,
		FormID: env.cfg.PyrusFormID,
		Fields: []pyrusplugin.Field{{Code: "status", Name: "status", Type: "status", Value: "finished"}},
	}

	toManager := &tickets.Ticket{Subject: "К менеджеру", Status: tickets.StatusToManager, CompanyID: createCompanyRecord(t, env.db, "company-keep", "Компания")}
	if err := env.ticketRepo.Create(ctx, toManager); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if changed, err := env.incoming.applyPyrusStatusToTicket(ctx, toManager, &finished); err != nil || changed {
		t.Fatalf("статус «к менеджеру» не должен меняться статусом Pyrus: changed=%v err=%v", changed, err)
	}
	if toManager.Status != tickets.StatusToManager {
		t.Fatalf("статус тикета изменился на %q", toManager.Status)
	}

	regular := &tickets.Ticket{Subject: "В работе", Status: tickets.StatusInProgress, CompanyID: toManager.CompanyID}
	if err := env.ticketRepo.Create(ctx, regular); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if changed, err := env.incoming.applyPyrusStatusToTicket(ctx, regular, &finished); err != nil || !changed || regular.Status != tickets.StatusResolved {
		t.Fatalf("для обычного тикета статус Pyrus должен применяться: changed=%v status=%q err=%v", changed, regular.Status, err)
	}
}

func TestPyrusSyncService_ToManagerStatusIsNotSentToPyrus(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	ticketID := createPyrusSyncTicket(t, env, "to-manager-status")
	client := &fakePyrusAPIClient{
		configured: true,
		getTaskFunc: func(context.Context, int64) (*pyrusplugin.Task, error) {
			return nil, errors.New("задача Pyrus не должна запрашиваться для статуса «к менеджеру»")
		},
	}
	service, ok := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo).(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}
	raw, err := json.Marshal(events.PyrusSyncEntityPayload{TicketID: ticketID, TaskID: 7000, Status: tickets.StatusToManager})
	if err != nil {
		t.Fatalf("не удалось сериализовать payload: %v", err)
	}
	status, reason, err := service.handleOutgoingEvent(ctx, &pyrus.OutgoingEvent{
		ID: "outgoing-to-manager-1", EventName: events.PyrusTicketStatusSyncRequested, PayloadJSON: string(raw),
	})
	if err != nil || status != pyrus.OutgoingEventStatusIgnored || reason == "" {
		t.Fatalf("ожидали ignored с причиной: status=%q reason=%q err=%v", status, reason, err)
	}
}

func TestPyrusSyncService_EnqueueImportedCommentFromBitrix(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	service, ok := NewPyrusSyncService(env.cfg, env.log, &fakePyrusAPIClient{configured: true}, nil, env.ticketRepo, env.userRepo, env.pyrusRepo).(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}
	pyrusTicketID := createPyrusSyncTicket(t, env, "bridge-out") // pyrus:task:7000
	comment := &tickets.TicketComment{ID: "b24-1", TicketID: pyrusTicketID, Text: "Ответ менеджера"}

	if err := service.EnqueueImportedComment(ctx, events.TicketCommentImportedPayload{TicketID: pyrusTicketID, Comment: comment, Source: events.CommentImportSourceBitrix}); err != nil {
		t.Fatalf("EnqueueImportedComment вернул ошибку: %v", err)
	}
	items, total, err := env.pyrusRepo.ListOutgoingEvents(ctx, pyrus.OutgoingEventListFilter{Limit: 10})
	if err != nil || total != 1 || items[0].EventName != events.PyrusCommentSyncRequested || items[0].PyrusTaskID == nil || *items[0].PyrusTaskID != 7000 {
		t.Fatalf("ожидали одно событие отправки комментария в задачу 7000: total=%d items=%+v err=%v", total, items, err)
	}

	// Комментарий из Pyrus, приватный комментарий и тикет без задачи Pyrus не отправляются.
	privateComment := &tickets.TicketComment{ID: "b24-2", TicketID: pyrusTicketID, Text: "Заметка", IsPrivate: true}
	plainTicket := &tickets.Ticket{Subject: "Обычный", Status: tickets.StatusNew, CompanyID: createCompanyRecord(t, env.db, "company-plain", "Компания")}
	if err := env.ticketRepo.Create(ctx, plainTicket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	for _, payload := range []events.TicketCommentImportedPayload{
		{TicketID: pyrusTicketID, Comment: comment, Source: events.CommentImportSourcePyrus},
		{TicketID: pyrusTicketID, Comment: privateComment, Source: events.CommentImportSourceBitrix},
		{TicketID: plainTicket.ID, Comment: &tickets.TicketComment{ID: "b24-3", TicketID: plainTicket.ID, Text: "Не Pyrus"}, Source: events.CommentImportSourceBitrix},
	} {
		if err := service.EnqueueImportedComment(ctx, payload); err != nil {
			t.Fatalf("EnqueueImportedComment вернул ошибку: %v", err)
		}
	}
	if _, total, _ := env.pyrusRepo.ListOutgoingEvents(ctx, pyrus.OutgoingEventListFilter{Limit: 10}); total != 1 {
		t.Fatalf("лишние события отправки в Pyrus: %d", total)
	}

	// Тикет без префикса в service_desk_uuid, но со связкой с задачей Pyrus (создан не из Pyrus).
	linked := &tickets.Ticket{Subject: "Со связкой", Status: tickets.StatusNew, CompanyID: plainTicket.CompanyID}
	if err := env.ticketRepo.Create(ctx, linked); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if err := env.pyrusRepo.UpsertTicketLink(ctx, &pyrus.TicketLink{TicketID: linked.ID, PyrusTaskID: 7777}); err != nil {
		t.Fatalf("не удалось создать связку: %v", err)
	}
	if err := service.EnqueueImportedComment(ctx, events.TicketCommentImportedPayload{TicketID: linked.ID, Comment: &tickets.TicketComment{ID: "b24-4", TicketID: linked.ID, Text: "Через связку"}, Source: events.CommentImportSourceBitrix}); err != nil {
		t.Fatalf("EnqueueImportedComment вернул ошибку: %v", err)
	}
	if _, total, _ := env.pyrusRepo.ListOutgoingEvents(ctx, pyrus.OutgoingEventListFilter{Limit: 10}); total != 2 {
		t.Fatalf("ожидали событие для тикета со связкой, всего %d", total)
	}
}

func TestBitrixIncomingService_ManagerCommentPublishesImportEventOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:bitrix_import_event?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(commentSendTestModels()...); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	ctx := context.Background()
	ticketRepo := repositories.NewTicketRepo(db)
	userRepo := repositories.NewUserRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)
	companyID := createCompanyForTicketDeleteTests(t, ctx, repositories.NewCompanyRepo(db))
	ticket := &tickets.Ticket{Subject: "Из Pyrus", Status: tickets.StatusToManager, CompanyID: companyID, ServiceDeskUUID: "pyrus:task:8301"}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	bus := eventbus.NewInMemoryEventBus(100)
	log := logger.New("", "test", "error", true)
	busCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go bus.Start(busCtx, log)
	recorder := &importedCommentRecorder{}
	recorder.subscribe(bus)

	incoming := &bitrixIncomingService{
		cfg:        &config.Config{EnableBitrixGateway: true},
		log:        log,
		repo:       bitrixRepo,
		ticketRepo: ticketRepo,
		userRepo:   userRepo,
		eventBus:   bus,
	}
	comment := &b24.TimelineComment{ID: 9001, Comment: "Менеджер: уточните реквизиты", EntityType: "deal", EntityID: 1}
	if err := incoming.addOrUpdateCommentFromBitrix(ctx, ticket, comment, false); err != nil {
		t.Fatalf("addOrUpdateCommentFromBitrix вернул ошибку: %v", err)
	}
	waitForCondition(t, 2*time.Second, func() bool { return len(recorder.snapshot()) == 1 })
	item := recorder.snapshot()[0]
	if item.Source != events.CommentImportSourceBitrix || item.Comment == nil || item.Comment.ID != "b24-9001" || item.TicketID != ticket.ID {
		t.Fatalf("неверное событие импорта: %+v", item)
	}

	// Правка того же комментария в Bitrix24 не отправляется в Pyrus повторно.
	edited := &b24.TimelineComment{ID: 9001, Comment: "Менеджер: уточните реквизиты ООО", EntityType: "deal", EntityID: 1}
	if err := incoming.addOrUpdateCommentFromBitrix(ctx, ticket, edited, false); err != nil {
		t.Fatalf("повторный addOrUpdateCommentFromBitrix вернул ошибку: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := len(recorder.snapshot()); got != 1 {
		t.Fatalf("правка не должна публиковать импорт заново, событий: %d", got)
	}
	var link bitrix.CommentLink
	if err := db.Where("b24_comment_id = ?", 9001).First(&link).Error; err != nil || link.EtalonCommentID != "b24-9001" {
		t.Fatalf("ожидали связь комментария Bitrix24: %+v err=%v", link, err)
	}
}

// Внутренний комментарий (контакт клиента при передаче менеджеру) не попадает в Pyrus ни при смене статуса, ни как импортированный.
func TestPyrusSyncService_InternalCommentIsNotSentToPyrus(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ctx := context.Background()
	ticketID := createPyrusSyncTicket(t, env, "internal-comment")
	internal := tickets.TicketComment{ID: "contact-1", TicketID: ticketID, ServiceDeskUUID: "contact-1", Text: "Контакт в телеграмм: @client", AuthorName: "Оператор", IsInternal: true, CreationDate: time.Now()}
	public := tickets.TicketComment{ID: "public-1", TicketID: ticketID, ServiceDeskUUID: "public-1", Text: "Ответ клиенту", AuthorName: "Оператор", CreationDate: time.Now()}
	if err := env.ticketRepo.AddComments(ctx, []tickets.TicketComment{internal, public}); err != nil {
		t.Fatalf("не удалось создать комментарии: %v", err)
	}

	var sent []string
	client := &fakePyrusAPIClient{
		configured: true,
		addCommentFunc: func(_ context.Context, taskID int64, req pyrusplugin.CommentRequest) (*pyrusplugin.Task, error) {
			sent = append(sent, req.Text+req.FormattedText)
			return &pyrusplugin.Task{ID: taskID}, nil
		},
	}
	service, ok := NewPyrusSyncService(env.cfg, env.log, client, nil, env.ticketRepo, env.userRepo, env.pyrusRepo).(*pyrusSyncService)
	if !ok {
		t.Fatalf("не удалось привести PyrusSyncService к concrete type")
	}
	if err := service.syncPendingPublicComments(ctx, 7000, ticketID); err != nil {
		t.Fatalf("syncPendingPublicComments вернул ошибку: %v", err)
	}
	if len(sent) != 1 || sent[0] != "Ответ клиенту" {
		t.Fatalf("в Pyrus должен уйти только публичный комментарий, отправлено: %q", sent)
	}
	if err := service.EnqueueImportedComment(ctx, events.TicketCommentImportedPayload{TicketID: ticketID, Comment: &internal, Source: events.CommentImportSourceBitrix}); err != nil {
		t.Fatalf("EnqueueImportedComment вернул ошибку: %v", err)
	}
	if _, total, _ := env.pyrusRepo.ListOutgoingEvents(ctx, pyrus.OutgoingEventListFilter{Limit: 10}); total != 0 {
		t.Fatalf("внутренний комментарий не должен ставиться в очередь Pyrus, событий: %d", total)
	}
}
