package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/pkg/eventbus"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type pyrusTestEnv struct {
	cfg           *config.Config
	db            *gorm.DB
	log           logger.LoggerInterface
	bus           *eventbus.InMemoryEventBus
	cancel        context.CancelFunc
	ticketRepo    tickets.TicketRepository
	pyrusRepo     pyrus.Repository
	companyRepo   company.Repository
	contractRepo  contract.Repository
	serverRepo    server.Repository
	userRepo      user.Repository
	ticketService TicketService
	incoming      *pyrusIncomingService
}

func newPyrusTestEnv(t *testing.T, startBus bool) *pyrusTestEnv {
	t.Helper()

	dbName := fmt.Sprintf("file:pyrus-test-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&contract.Contract{},
		&server.Server{},
		&tickets.Ticket{},
		&tickets.TicketHistory{},
		&tickets.TicketComment{},
		&tickets.FileAsset{},
		&tickets.TicketFileLink{},
		&pyrus.TicketLink{},
		&pyrus.CommentLink{},
		&pyrus.FileLink{},
		&pyrus.UserMap{},
		&pyrus.IncomingEvent{},
		&pyrus.OutgoingEvent{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему БД: %v", err)
	}

	cfg := &config.Config{
		CommonContractID:    "common-contract",
		TicketStoragePath:   t.TempDir(),
		EnablePyrusGateway:  true,
		PyrusWebhookEnabled: true,
		PyrusWebhookSecret:  "webhook-secret",
		PyrusSecurityKey:    "security-key",
		PyrusLogin:          "pyrus-login",
		PyrusFormID:         101,
	}
	log := logger.New("", "test", "error", true)
	bus := eventbus.NewInMemoryEventBus(100)

	companyRepo := infraRepos.NewCompanyRepo(db)
	contractRepo := infraRepos.NewContractRepo(db)
	ticketRepo := infraRepos.NewTicketRepo(db)
	serverRepo := infraRepos.NewServerRepo(db)
	userRepo := infraRepos.NewUserRepo(db)
	pyrusRepo := infraRepos.NewPyrusRepo(db)

	ticketService := NewTicketService(
		log,
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		cfg,
		serverRepo,
		nil,
		nil,
		nil,
		pyrusRepo,
		nil,
	)
	pyrusClient := pyrusplugin.NewClient(cfg, log)
	pyrusSync := NewPyrusSyncService(cfg, log, pyrusClient, nil, pyrusRepo)
	bus.Subscribe(events.PyrusTicketExtIDSyncRequested, func(ctx context.Context, event eventbus.Event) {
		payload, ok := event.Payload.(events.PyrusSyncEntityPayload)
		if !ok {
			return
		}
		_ = pyrusSync.EnqueueEvent(ctx, events.PyrusTicketExtIDSyncRequested, payload)
	})

	var cancel context.CancelFunc = func() {}
	if startBus {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go bus.Start(ctx, log)
	}

	incoming := NewPyrusIncomingService(
		cfg,
		log,
		pyrusClient,
		nil,
		ticketRepo,
		ticketService,
		userRepo,
		serverRepo,
		pyrusRepo,
		bus,
	)
	concreteIncoming, ok := incoming.(*pyrusIncomingService)
	if !ok {
		t.Fatalf("не удалось привести входящий сервис Pyrus к concrete type")
	}

	t.Cleanup(func() {
		cancel()
	})

	return &pyrusTestEnv{
		cfg:           cfg,
		db:            db,
		log:           log,
		bus:           bus,
		cancel:        cancel,
		ticketRepo:    ticketRepo,
		pyrusRepo:     pyrusRepo,
		companyRepo:   companyRepo,
		contractRepo:  contractRepo,
		serverRepo:    serverRepo,
		userRepo:      userRepo,
		ticketService: ticketService,
		incoming:      concreteIncoming,
	}
}

func TestPyrusIncomingService_HandleWebhookDeduplicatesByPayloadHash(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	rawBody := mustPyrusJSON(t, pyrusplugin.WebhookPayload{
		Event:  "form_task_changed",
		TaskID: 5001,
		Task: pyrusplugin.Task{
			ID:     5001,
			FormID: env.cfg.PyrusFormID,
		},
	})
	signature := signPyrusPayload(env.cfg, rawBody)

	if err := env.incoming.HandleWebhook(context.Background(), rawBody, signature); err != nil {
		t.Fatalf("первый HandleWebhook вернул ошибку: %v", err)
	}
	if err := env.incoming.HandleWebhook(context.Background(), rawBody, signature); err != nil {
		t.Fatalf("второй HandleWebhook вернул ошибку: %v", err)
	}

	items, total, err := env.pyrusRepo.ListIncomingEvents(context.Background(), pyrus.IncomingEventListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("не удалось получить входящие события: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("ожидали одно входящее событие после дедупликации, total=%d len=%d", total, len(items))
	}
}

func TestPyrusIncomingService_CreateTicketFromPyrusAndQueueExtIDSync(t *testing.T) {
	env := newPyrusTestEnv(t, true)
	ownerID := createCompanyRecord(t, env.db, "company-owner-1", "Компания 1")
	createServerRecord(t, env.db, "CRM-100", ownerID)

	task := &pyrusplugin.Task{
		ID:     5002,
		FormID: env.cfg.PyrusFormID,
		Text:   "У клиента не печатается чек.",
		Fields: []pyrusplugin.Field{
			{Code: "CRMID", Name: "CRMID", Value: "CRM-100"},
			{Code: "Restaurant", Name: "Restaurant", Value: "Ресторан 1"},
			{Code: "Module", Name: "Module", Value: "Касса"},
			{Code: "CallType", Name: "CallType", Value: "Инцидент"},
			{Code: "Subject", Name: "Subject", Value: "Не печатает чек"},
			{Code: "SenderName", Name: "SenderName", Value: "Иван Клиент"},
		},
	}

	status, reason, err := env.incoming.handleIncomingEvent(context.Background(), &pyrus.IncomingEvent{
		ID:         "incoming-create-1",
		EventName:  "form_task_changed",
		PayloadRaw: string(mustPyrusJSON(t, pyrusplugin.WebhookPayload{Event: "form_task_changed", TaskID: task.ID, Task: *task})),
	})
	if err != nil {
		t.Fatalf("handleIncomingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.IncomingEventStatusDone || reason != "" {
		t.Fatalf("ожидали успешную обработку, получили status=%q reason=%q", status, reason)
	}

	ticket, err := env.ticketRepo.GetByServiceDeskUUID(context.Background(), "pyrus:task:5002")
	if err != nil {
		t.Fatalf("не удалось получить созданный тикет: %v", err)
	}
	if ticket == nil {
		t.Fatalf("ожидали созданный тикет из Pyrus")
	}
	if ticket.AssigneeID != nil {
		t.Fatalf("ожидали, что тикет из Pyrus создаётся без assignee_id")
	}
	if ticket.ReporterName != "Иван Клиент" {
		t.Fatalf("ожидали reporter_name=Иван Клиент, получили %q", ticket.ReporterName)
	}
	if !strings.Contains(ticket.Description, "CRMID: CRM-100") {
		t.Fatalf("ожидали CRMID в описании, получили %q", ticket.Description)
	}

	link, err := env.pyrusRepo.GetTicketLinkByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("не удалось получить ticket link: %v", err)
	}
	if link == nil || link.TicketID != ticket.ID {
		t.Fatalf("ожидали ticket link на созданный тикет, получили %+v", link)
	}

	waitForCondition(t, 2*time.Second, func() bool {
		items, total, err := env.pyrusRepo.ListOutgoingEvents(context.Background(), pyrus.OutgoingEventListFilter{Limit: 10})
		if err != nil || total == 0 || len(items) == 0 {
			return false
		}
		return items[0].EventName == events.PyrusTicketExtIDSyncRequested && items[0].TicketID != nil && *items[0].TicketID == ticket.ID
	})
}

func TestPyrusIncomingService_FailsOnAmbiguousCRMID(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ownerID1 := createCompanyRecord(t, env.db, "company-owner-a", "Компания А")
	ownerID2 := createCompanyRecord(t, env.db, "company-owner-b", "Компания Б")
	createServerRecord(t, env.db, "CRM-AMB", ownerID1)
	createServerRecord(t, env.db, "CRM-AMB", ownerID2)

	task := &pyrusplugin.Task{
		ID:     5003,
		FormID: env.cfg.PyrusFormID,
		Fields: []pyrusplugin.Field{
			{Code: "CRMID", Name: "CRMID", Value: "CRM-AMB"},
		},
	}

	_, _, err := env.incoming.handleIncomingEvent(context.Background(), &pyrus.IncomingEvent{
		ID:         "incoming-ambiguous-1",
		EventName:  "form_task_changed",
		PayloadRaw: string(mustPyrusJSON(t, pyrusplugin.WebhookPayload{Event: "form_task_changed", TaskID: task.ID, Task: *task})),
	})
	if err == nil {
		t.Fatalf("ожидали ошибку при неоднозначном CRMID")
	}
	if !strings.Contains(err.Error(), "несколько owner_id") {
		t.Fatalf("ожидали ошибку про неоднозначный owner_id, получили: %v", err)
	}
}

func TestPyrusIncomingService_AddsCommentForExistingExtIDAndWritesPyrusHistory(t *testing.T) {
	env := newPyrusTestEnv(t, false)
	ownerID := createCompanyRecord(t, env.db, "company-owner-2", "Компания 2")

	ticket := &tickets.Ticket{
		Subject:         "Существующий тикет",
		Description:     "Описание",
		Status:          tickets.StatusNew,
		Priority:        tickets.PriorityMedium,
		Type:            tickets.TypeIncident,
		CompanyID:       ownerID,
		ServiceDeskUUID: "pyrus:task:5004",
		ReporterName:    "Pyrus",
		SyncWithBitrix:  false,
	}
	if err := env.ticketRepo.Create(context.Background(), ticket); err != nil {
		t.Fatalf("не удалось создать локальный тикет: %v", err)
	}

	commentTime := time.Now().Add(-time.Minute)
	task := &pyrusplugin.Task{
		ID:     5004,
		FormID: env.cfg.PyrusFormID,
		Fields: []pyrusplugin.Field{
			{Code: "ext_id", Name: "ext_id", Value: ticket.ID},
		},
		Comments: []pyrusplugin.Comment{
			{
				ID:         7001,
				Text:       "Комментарий из Pyrus",
				CreateDate: commentTime,
				Author: &pyrusplugin.Person{
					ID:        42,
					FirstName: "Пётр",
					LastName:  "Оператор",
				},
			},
		},
	}

	status, reason, err := env.incoming.handleIncomingEvent(context.Background(), &pyrus.IncomingEvent{
		ID:         "incoming-existing-1",
		EventName:  "form_task_changed",
		PayloadRaw: string(mustPyrusJSON(t, pyrusplugin.WebhookPayload{Event: "form_task_changed", TaskID: task.ID, Task: *task})),
	})
	if err != nil {
		t.Fatalf("handleIncomingEvent вернул ошибку: %v", err)
	}
	if status != pyrus.IncomingEventStatusDone || reason != "" {
		t.Fatalf("ожидали успешную обработку, получили status=%q reason=%q", status, reason)
	}

	comments, err := env.ticketRepo.GetComments(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("не удалось получить комментарии тикета: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("ожидали один импортированный комментарий, получили %d", len(comments))
	}
	if comments[0].Text != "Комментарий из Pyrus" {
		t.Fatalf("ожидали текст комментария из Pyrus, получили %q", comments[0].Text)
	}
	if comments[0].Source != tickets.CommentSourcePyrus {
		t.Fatalf("ожидали source комментария pyrus, получили %q", comments[0].Source)
	}

	history, err := env.ticketRepo.GetHistory(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("не удалось получить историю тикета: %v", err)
	}
	foundPyrusHistory := false
	for i := range history {
		if history[i].Source == tickets.HistorySourcePyrus && history[i].Action == tickets.HistoryActionCommentAdded {
			foundPyrusHistory = true
			break
		}
	}
	if !foundPyrusHistory {
		t.Fatalf("ожидали запись history с source=pyrus")
	}
}

func TestPyrusIncomingService_ShouldProcessIncomingNowHonorsBackoff(t *testing.T) {
	service := &pyrusIncomingService{
		cfg: &config.Config{
			PyrusIncomingRetryBase:   time.Second,
			PyrusIncomingRetryMax:    10 * time.Second,
			PyrusIncomingMaxAttempts: 5,
		},
	}

	item := &pyrus.IncomingEvent{
		Status:    pyrus.IncomingEventStatusFailed,
		Attempts:  2,
		UpdatedAt: time.Now(),
	}
	if service.shouldProcessIncomingNow(item) {
		t.Fatalf("ожидали, что событие ещё рано переобрабатывать")
	}

	item.UpdatedAt = time.Now().Add(-3 * time.Second)
	if !service.shouldProcessIncomingNow(item) {
		t.Fatalf("ожидали, что после backoff событие можно переобработать")
	}
}

func createCompanyRecord(t *testing.T, db *gorm.DB, id string, title string) string {
	t.Helper()
	companyItem := &company.Company{
		Base: common.Base{ID: id},
	}
	companyItem.Title = strPtr(title)
	companyItem.ActiveContract = boolPtr(false)
	if err := db.Create(companyItem).Error; err != nil {
		t.Fatalf("не удалось создать компанию %s: %v", id, err)
	}
	return companyItem.ID
}

func createServerRecord(t *testing.T, db *gorm.DB, crmID string, ownerID string) {
	t.Helper()
	serverItem := &server.Server{
		CRMid:   strPtr(crmID),
		OwnerID: strPtr(ownerID),
		Status:  "active",
	}
	if err := db.Create(serverItem).Error; err != nil {
		t.Fatalf("не удалось создать сервер для CRMID=%s: %v", crmID, err)
	}
}

func signPyrusPayload(cfg *config.Config, rawBody []byte) string {
	mac := hmac.New(sha1.New, []byte(pyrusWebhookSecret(cfg)))
	_, _ = mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil))
}

func mustPyrusJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("не удалось сериализовать JSON: %v", err)
	}
	return data
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("условие не выполнилось за %s", timeout)
}

func strPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}
