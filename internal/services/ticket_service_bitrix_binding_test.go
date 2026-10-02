package services

import (
	"context"
	"errors"
	"testing"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type bitrixBindingEnv struct {
	svc        TicketService
	ticketRepo tickets.TicketRepository
	bitrixRepo bitrix.Repository
	actorID    uint
	companyID  string
}

func newBitrixBindingEnv(t *testing.T, name string, bitrixEnabled bool) *bitrixBindingEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
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
		&tickets.TicketContact{},
		&tickets.TicketHistory{},
		&tickets.TicketComment{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	actor := &user.User{Username: "binding_actor_" + name, PasswordHash: "hash", FullName: "Оператор"}
	if err := userRepo.Create(ctx, actor); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}
	title := "Клиент " + name
	activeContract := false
	comp := &company.Company{Title: &title, ActiveContract: &activeContract}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	cfg := &config.Config{EnableBitrixGateway: bitrixEnabled}
	svc := NewTicketService(nil, ticketRepo, userRepo, companyRepo, contractRepo, nil, cfg, nil, nil, nil, bitrixRepo, nil, nil, nil, nil)
	return &bitrixBindingEnv{svc: svc, ticketRepo: ticketRepo, bitrixRepo: bitrixRepo, actorID: actor.ID, companyID: comp.ID}
}

// newPyrusLikeTicket создаёт тикет, как из Pyrus: без синхронизации с Bitrix24 и без точки обслуживания.
func (e *bitrixBindingEnv) newPyrusLikeTicket(t *testing.T) *tickets.Ticket {
	t.Helper()
	ticket := &tickets.Ticket{
		Subject:        "Не открывается смена",
		Status:         tickets.StatusInProgress,
		CompanyID:      e.companyID,
		ReporterID:     &e.actorID,
		SyncWithBitrix: false,
	}
	if err := e.ticketRepo.Create(context.Background(), ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	return ticket
}

func (e *bitrixBindingEnv) mapCompanyToPoint(t *testing.T, pointID int64) {
	t.Helper()
	if err := e.bitrixRepo.UpsertCompanyServicePointMapping(context.Background(), &bitrix.CompanyServicePointMapping{
		CompanyID: e.companyID, BitrixServicePointID: pointID,
	}); err != nil {
		t.Fatalf("не удалось сохранить сопоставление компании: %v", err)
	}
}

func managerTransferOptions() TicketStatusChangeOptions {
	return TicketStatusChangeOptions{
		ManagerTransferTarget: tickets.ManagerTransferTargetSales,
		ClientContactType:     tickets.ManagerTransferContactTelegram,
		ClientContactValue:    "@client_login",
	}
}

func TestChangeStatus_ToManagerBindsTicketByCompanyMapping(t *testing.T) {
	env := newBitrixBindingEnv(t, "binding_mapped", true)
	ctx := context.Background()
	env.mapCompanyToPoint(t, 777)
	ticket := env.newPyrusLikeTicket(t)

	updated, err := env.svc.ChangeStatus(ctx, ticket.ID, tickets.StatusToManager, "", managerTransferOptions(), env.actorID)
	if err != nil {
		t.Fatalf("ChangeStatus вернул ошибку: %v", err)
	}
	if updated.Status != tickets.StatusToManager || !updated.SyncWithBitrix {
		t.Fatalf("ожидали передачу менеджеру с включённой синхронизацией: %+v", updated)
	}
	if updated.BitrixServicePointID == nil || *updated.BitrixServicePointID != 777 {
		t.Fatalf("ожидали точку 777 из сопоставления компании, получили %v", updated.BitrixServicePointID)
	}
	if updated.BitrixDealTitle != "Не открывается смена" {
		t.Fatalf("ожидали заголовок сделки из темы тикета, получили %q", updated.BitrixDealTitle)
	}
	stored, err := env.ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil || stored == nil || !stored.SyncWithBitrix || stored.BitrixServicePointID == nil || *stored.BitrixServicePointID != 777 {
		t.Fatalf("привязка должна быть сохранена в БД: %+v err=%v", stored, err)
	}
}

func TestChangeStatus_ToManagerWithoutMappingRequiresPointAndHasNoSideEffects(t *testing.T) {
	env := newBitrixBindingEnv(t, "binding_unmapped", true)
	ctx := context.Background()
	ticket := env.newPyrusLikeTicket(t)

	_, err := env.svc.ChangeStatus(ctx, ticket.ID, tickets.StatusToManager, "", managerTransferOptions(), env.actorID)
	if !errors.Is(err, ErrBitrixServicePointRequired) {
		t.Fatalf("ожидали ErrBitrixServicePointRequired, получили %v", err)
	}
	stored, err := env.ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil || stored == nil || stored.Status != tickets.StatusInProgress || stored.SyncWithBitrix {
		t.Fatalf("тикет не должен измениться при отказе: %+v err=%v", stored, err)
	}
	if contacts, _ := env.ticketRepo.ListTicketContacts(ctx, ticket.ID); len(contacts) != 0 {
		t.Fatalf("контакт клиента не должен сохраняться при отказе, получили %d", len(contacts))
	}
	if comments, _ := env.ticketRepo.GetComments(ctx, ticket.ID); len(comments) != 0 {
		t.Fatalf("комментарий с контактом не должен создаваться при отказе, получили %d", len(comments))
	}

	// Оператор выбрал точку: передача проходит и сопоставление компании запоминается.
	point := int64(901)
	options := managerTransferOptions()
	options.BitrixServicePointID = &point
	updated, err := env.svc.ChangeStatus(ctx, ticket.ID, tickets.StatusToManager, "", options, env.actorID)
	if err != nil {
		t.Fatalf("ChangeStatus с выбранной точкой вернул ошибку: %v", err)
	}
	if updated.BitrixServicePointID == nil || *updated.BitrixServicePointID != 901 || !updated.SyncWithBitrix {
		t.Fatalf("ожидали выбранную точку 901: %+v", updated)
	}
	mapping, err := env.bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, env.companyID)
	if err != nil || mapping == nil || mapping.BitrixServicePointID != 901 {
		t.Fatalf("выбранная точка должна стать сопоставлением компании: %+v err=%v", mapping, err)
	}
}

func TestChangeStatus_ToManagerKeepsExistingBindingAndSkipsWhenBitrixDisabled(t *testing.T) {
	env := newBitrixBindingEnv(t, "binding_existing", true)
	ctx := context.Background()
	env.mapCompanyToPoint(t, 777)
	ticket := env.newPyrusLikeTicket(t)
	existingPoint := int64(555)
	ticket.SyncWithBitrix = true
	ticket.BitrixServicePointID = &existingPoint
	ticket.BitrixDealTitle = "Ручной заголовок"
	if err := env.ticketRepo.Update(ctx, ticket); err != nil {
		t.Fatalf("не удалось подготовить тикет: %v", err)
	}
	updated, err := env.svc.ChangeStatus(ctx, ticket.ID, tickets.StatusToManager, "", managerTransferOptions(), env.actorID)
	if err != nil {
		t.Fatalf("ChangeStatus вернул ошибку: %v", err)
	}
	if updated.BitrixServicePointID == nil || *updated.BitrixServicePointID != 555 || updated.BitrixDealTitle != "Ручной заголовок" {
		t.Fatalf("существующая привязка не должна меняться: %+v", updated)
	}

	disabled := newBitrixBindingEnv(t, "binding_disabled", false)
	plain := disabled.newPyrusLikeTicket(t)
	moved, err := disabled.svc.ChangeStatus(ctx, plain.ID, tickets.StatusToManager, "", managerTransferOptions(), disabled.actorID)
	if err != nil || moved.Status != tickets.StatusToManager || moved.SyncWithBitrix {
		t.Fatalf("при отключённом Bitrix24 привязка не требуется: %+v err=%v", moved, err)
	}
}

func TestUpdateBitrixFields_ResolvesPointFromCompanyMappingAndDefaultsTitle(t *testing.T) {
	env := newBitrixBindingEnv(t, "binding_update", true)
	ctx := context.Background()
	ticket := env.newPyrusLikeTicket(t)

	if _, err := env.svc.UpdateBitrixFields(ctx, ticket.ID, nil, "", env.actorID); !errors.Is(err, ErrBitrixServicePointRequired) {
		t.Fatalf("без сопоставления ожидали ErrBitrixServicePointRequired, получили %v", err)
	}

	env.mapCompanyToPoint(t, 777)
	updated, err := env.svc.UpdateBitrixFields(ctx, ticket.ID, nil, "", env.actorID)
	if err != nil {
		t.Fatalf("UpdateBitrixFields вернул ошибку: %v", err)
	}
	if !updated.SyncWithBitrix || updated.BitrixServicePointID == nil || *updated.BitrixServicePointID != 777 || updated.BitrixDealTitle != "Не открывается смена" {
		t.Fatalf("ожидали точку из сопоставления и заголовок из темы: %+v", updated)
	}

	explicit := int64(888)
	changed, err := env.svc.UpdateBitrixFields(ctx, ticket.ID, &explicit, "Свой заголовок", env.actorID)
	if err != nil || changed.BitrixServicePointID == nil || *changed.BitrixServicePointID != 888 || changed.BitrixDealTitle != "Свой заголовок" {
		t.Fatalf("явный выбор оператора должен иметь приоритет: %+v err=%v", changed, err)
	}
}
