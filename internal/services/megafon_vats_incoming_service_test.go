package services

import (
	"errors"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	infraRepos "etalon-server/internal/infra/repositories"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type megafonIncomingTestEnv struct {
	db      *gorm.DB
	repo    telephony.Repository
	service *megafonVATSIncomingService
}

func TestMegafonVATSIncomingService_HandleWebhookDeduplicatesCanonicalPayloadHash(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)

	eventID1 := enqueueMegafonWebhook(
		t,
		env,
		"cmd=event&type=INCOMING&callid=call-001&crm_token=test-token&phone=%2B79990001122",
	)
	eventID2 := enqueueMegafonWebhook(
		t,
		env,
		"phone=%2B79990001122&crm_token=test-token&callid=call-001&type=INCOMING&cmd=event",
	)

	if eventID1 == "" {
		t.Fatal("ожидали id первого сохранённого события")
	}
	if eventID2 != "" {
		t.Fatalf("ожидали, что reordered payload будет отброшен как дубликат, получили event_id=%q", eventID2)
	}

	items, total, err := env.repo.ListIncomingEvents(t.Context(), telephony.IncomingEventListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("не удалось получить входящие события телефонии: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("ожидали одно входящее событие после дедупликации, total=%d len=%d", total, len(items))
	}
	if items[0].Status != telephony.IncomingEventStatusNew {
		t.Fatalf("ожидали статус %q до запуска worker, получили %q", telephony.IncomingEventStatusNew, items[0].Status)
	}
}

func TestMegafonVATSIncomingService_ProcessIncomingEventCreatesCallSnapshot(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)
	eventID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=event&type=INCOMING&callid=call-002&crm_token=test-token&phone=%2B79990001122&user=admin&direction=in&diversion=74950000001",
	)

	processMegafonEvent(t, env, eventID)

	call, err := env.repo.GetCallByExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-002")
	if err != nil {
		t.Fatalf("не удалось получить звонок: %v", err)
	}
	if call == nil {
		t.Fatal("ожидали созданный telephony_call")
	}
	if call.Status != "incoming" {
		t.Fatalf("ожидали status=incoming, получили %q", call.Status)
	}
	if call.Direction != "in" {
		t.Fatalf("ожидали direction=in, получили %q", call.Direction)
	}
	if call.EmployeeLogin == nil || *call.EmployeeLogin != "admin" {
		t.Fatalf("ожидали employee_login=admin, получили %+v", call.EmployeeLogin)
	}

	item, err := env.repo.GetIncomingEventByID(t.Context(), eventID)
	if err != nil {
		t.Fatalf("не удалось получить обработанное событие: %v", err)
	}
	if item == nil || item.Status != telephony.IncomingEventStatusDone {
		t.Fatalf("ожидали обработанное событие со статусом done, получили %+v", item)
	}
}

func TestMegafonVATSIncomingService_HistoryFinalizesCallAndStoresRecording(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)
	eventID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=history&type=in&status=Success&callid=call-003&crm_token=test-token&phone=%2B79990001122&user=admin&diversion=74950000001&start=20260405T120000Z&duration=120&wait=15&link=https%3A%2F%2Fexample.com%2Frecord.mp3&missedStatus=2",
	)

	processMegafonEvent(t, env, eventID)

	call, err := env.repo.GetCallByExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-003")
	if err != nil {
		t.Fatalf("не удалось получить звонок: %v", err)
	}
	if call == nil {
		t.Fatal("ожидали созданный telephony_call")
	}
	if call.Status != "success" {
		t.Fatalf("ожидали финальный status=success, получили %q", call.Status)
	}
	if call.DurationSeconds == nil || *call.DurationSeconds != 120 {
		t.Fatalf("ожидали duration_seconds=120, получили %+v", call.DurationSeconds)
	}
	if call.WaitSeconds == nil || *call.WaitSeconds != 15 {
		t.Fatalf("ожидали wait_seconds=15, получили %+v", call.WaitSeconds)
	}
	if !call.HasRecording {
		t.Fatal("ожидали has_recording=true")
	}
	if call.RecordingURL == nil || *call.RecordingURL != "https://example.com/record.mp3" {
		t.Fatalf("ожидали ссылку на запись, получили %+v", call.RecordingURL)
	}
}

func TestMegafonVATSIncomingService_ReplayEventDoesNotDuplicateCallEvent(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)
	eventID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=event&type=ACCEPTED&callid=call-004&crm_token=test-token&phone=%2B79990001122&user=admin&direction=in",
	)

	processMegafonEvent(t, env, eventID)
	if got := countMegafonCallEvents(t, env); got != 1 {
		t.Fatalf("ожидали один call_event после первичной обработки, получили %d", got)
	}

	if err := env.service.ReplayEvent(t.Context(), eventID); err != nil {
		t.Fatalf("ReplayEvent вернул ошибку: %v", err)
	}
	processMegafonEvent(t, env, eventID)

	if got := countMegafonCallEvents(t, env); got != 1 {
		t.Fatalf("ожидали, что replay не создаст дубликат call_event, получили %d записей", got)
	}
}

func TestMegafonVATSIncomingService_TransferredMergesSecondLegIntoPrimaryCall(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)

	secondAcceptedID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=event&type=ACCEPTED&callid=call-b&crm_token=test-token&phone=%2B79990001122&user=operator-b&direction=in",
	)
	processMegafonEvent(t, env, secondAcceptedID)

	transferID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=event&type=TRANSFERRED&callid=call-a&second_callid=call-b&crm_token=test-token&phone=%2B79990001122&user=operator-a&direction=in",
	)
	processMegafonEvent(t, env, transferID)

	primaryCall, err := env.repo.GetCallByExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-a")
	if err != nil {
		t.Fatalf("не удалось получить первичный звонок: %v", err)
	}
	if primaryCall == nil {
		t.Fatal("ожидали созданный первичный звонок")
	}

	secondDirectCall, err := env.repo.GetCallByExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-b")
	if err != nil {
		t.Fatalf("не удалось получить вторичный звонок: %v", err)
	}
	if secondDirectCall != nil {
		t.Fatalf("ожидали, что вторичный leg будет слит в первичный, получили %+v", secondDirectCall)
	}

	secondHistoryID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=history&type=in&status=Success&callid=call-b&crm_token=test-token&phone=%2B79990001122&user=operator-b&diversion=74950000001&start=20260405T120000Z&duration=120&wait=15&link=https%3A%2F%2Fexample.com%2Ftransfer.mp3",
	)
	processMegafonEvent(t, env, secondHistoryID)

	updatedPrimaryCall, err := env.repo.GetCallByExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-a")
	if err != nil {
		t.Fatalf("не удалось получить обновлённый первичный звонок: %v", err)
	}
	if updatedPrimaryCall == nil {
		t.Fatal("ожидали обновлённый первичный звонок")
	}
	if updatedPrimaryCall.Status != "success" {
		t.Fatalf("ожидали финальный status=success, получили %q", updatedPrimaryCall.Status)
	}
	if updatedPrimaryCall.RecordingURL == nil || *updatedPrimaryCall.RecordingURL != "https://example.com/transfer.mp3" {
		t.Fatalf("ожидали запись из history второго leg, получили %+v", updatedPrimaryCall.RecordingURL)
	}
	if updatedPrimaryCall.EmployeeLogin == nil || *updatedPrimaryCall.EmployeeLogin != "operator-b" {
		t.Fatalf("ожидали финального сотрудника operator-b, получили %+v", updatedPrimaryCall.EmployeeLogin)
	}

	aliasCall, err := env.repo.GetCallByAnyExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-b")
	if err != nil {
		t.Fatalf("не удалось разрешить звонок по second_call_id: %v", err)
	}
	if aliasCall == nil || aliasCall.ID != updatedPrimaryCall.ID {
		t.Fatalf("ожидали, что call-b будет разрешаться в первичный звонок, получили %+v", aliasCall)
	}
}

func TestMegafonVATSIncomingService_IgnoresInternalClientPhone(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)
	eventID := enqueueMegafonWebhook(
		t,
		env,
		"cmd=event&type=INCOMING&callid=call-005&crm_token=test-token&phone=101&direction=internal",
	)

	processMegafonEvent(t, env, eventID)

	call, err := env.repo.GetCallByExternalID(t.Context(), telephony.ProviderMegafonVATS, "call-005")
	if err != nil {
		t.Fatalf("не удалось проверить отсутствие звонка: %v", err)
	}
	if call != nil {
		t.Fatalf("не ожидали сохранения внутреннего звонка, получили %+v", call)
	}

	item, err := env.repo.GetIncomingEventByID(t.Context(), eventID)
	if err != nil {
		t.Fatalf("не удалось получить событие: %v", err)
	}
	if item == nil || item.Status != telephony.IncomingEventStatusIgnored {
		t.Fatalf("ожидали ignored-статус для внутреннего звонка, получили %+v", item)
	}
}

func TestMegafonVATSIncomingService_RejectsPayloadWithoutCallID(t *testing.T) {
	env := newMegafonVATSIncomingTestEnv(t)
	rawBody := []byte("cmd=history&crm_token=test-token")
	form, err := url.ParseQuery(string(rawBody))
	if err != nil {
		t.Fatalf("не удалось распарсить form-urlencoded payload: %v", err)
	}

	err = env.service.HandleWebhook(t.Context(), rawBody, form)
	if err == nil {
		t.Fatal("ожидали ошибку валидации при отсутствии callid")
	}
	if !errors.Is(err, ErrMegafonVATSWebhookBadRequest) {
		t.Fatalf("ожидали ErrMegafonVATSWebhookBadRequest, получили %v", err)
	}
}

func TestMegafonVATSIncomingService_ShouldProcessIncomingNowHonorsBackoff(t *testing.T) {
	service := &megafonVATSIncomingService{
		cfg: &config.Config{
			MegafonVATSRetryBase:           time.Second,
			MegafonVATSRetryMax:            10 * time.Second,
			MegafonVATSIncomingMaxAttempts: 5,
		},
	}

	item := &telephony.IncomingEvent{
		Status:    telephony.IncomingEventStatusFailed,
		Attempts:  2,
		UpdatedAt: time.Now(),
	}
	if service.shouldProcessIncomingNow(item) {
		t.Fatal("ожидали, что событие ещё рано переобрабатывать")
	}

	item.UpdatedAt = time.Now().Add(-3 * time.Second)
	if !service.shouldProcessIncomingNow(item) {
		t.Fatal("ожидали, что после backoff событие можно переобработать")
	}
}

func newMegafonVATSIncomingTestEnv(t *testing.T) *megafonIncomingTestEnv {
	t.Helper()

	dbName := "file:megafon-vats-test-" + time.Now().Format("20060102150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err = db.AutoMigrate(&telephony.IncomingEvent{}, &telephony.Call{}, &telephony.CallEvent{}, &telephony.CallArtifact{}, &telephony.PendingContext{}, &telephony.Contact{}); err != nil {
		t.Fatalf("не удалось подготовить схему БД: %v", err)
	}

	repo := infraRepos.NewTelephonyRepo(db)
	service, ok := NewMegafonVATSIncomingService(
		&config.Config{
			EnableMegafonVATS:              true,
			MegafonVATSCRMToken:            "test-token",
			MegafonVATSIncomingMaxAttempts: 5,
			MegafonVATSRetryBase:           time.Second,
			MegafonVATSRetryMax:            10 * time.Second,
		},
		logger.New("", "test", "error", true),
		nil,
		repo,
		nil,
		nil,
		nil,
		nil,
	).(*megafonVATSIncomingService)
	if !ok {
		t.Fatal("не удалось привести сервис Мегафон ВАТС к concrete type")
	}

	return &megafonIncomingTestEnv{
		db:      db,
		repo:    repo,
		service: service,
	}
}

func enqueueMegafonWebhook(t *testing.T, env *megafonIncomingTestEnv, rawQuery string) string {
	t.Helper()

	rawBody := []byte(rawQuery)
	form, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("не удалось распарсить payload: %v", err)
	}
	if err = env.service.HandleWebhook(t.Context(), rawBody, form); err != nil {
		t.Fatalf("HandleWebhook вернул ошибку: %v", err)
	}

	items, _, err := env.repo.ListIncomingEvents(t.Context(), telephony.IncomingEventListFilter{Limit: 20})
	if err != nil {
		t.Fatalf("не удалось получить сохранённые входящие события: %v", err)
	}
	for i := range items {
		if items[i].PayloadRaw == rawQuery {
			return items[i].ID
		}
	}
	return ""
}

func processMegafonEvent(t *testing.T, env *megafonIncomingTestEnv, eventID string) {
	t.Helper()
	if strings.TrimSpace(eventID) == "" {
		t.Fatal("не передан event_id для обработки")
	}
	env.service.processIncomingEvent(t.Context(), eventID)
}

func countMegafonCallEvents(t *testing.T, env *megafonIncomingTestEnv) int64 {
	t.Helper()
	var count int64
	if err := env.db.Model(&telephony.CallEvent{}).Count(&count).Error; err != nil {
		t.Fatalf("не удалось посчитать telephony_call_events: %v", err)
	}
	return count
}
