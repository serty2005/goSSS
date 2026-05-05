package services

import (
	"context"
	"testing"
	"time"

	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	infraRepos "etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestExtractFirstPhoneFromTicketTextStrictMegafonMobile(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "плюс семь", text: "Клиент: +79254443311", want: "79254443311"},
		{name: "восемь", text: "Звонить 89254443311", want: "79254443311"},
		{name: "без кода", text: "номер 9254443311", want: "79254443311"},
		{name: "разделители не поддерживаются", text: "+7-925-444-3311", want: ""},
		{name: "городской не подходит", text: "74951234567", want: ""},
		{name: "слишком длинный не подходит", text: "1792544433119", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstPhoneFromTicketText(tt.text)
			if got != tt.want {
				t.Fatalf("ожидали %q, получили %q", tt.want, got)
			}
		})
	}
}

func TestBindTicketTelephonyByTextHonorsProfileFlag(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket-phone-profile?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err = db.AutoMigrate(&user.User{}, &user.Role{}, &user.Integration{}, &tickets.Ticket{}, &telephony.Contact{}, &telephony.ContactCompanyLink{}, &telephony.Call{}, &telephony.CallTicketLink{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	userRepo := infraRepos.NewUserRepo(db)
	ticketRepo := infraRepos.NewTicketRepo(db)
	telephonyRepo := infraRepos.NewTelephonyRepo(db)

	actor := &user.User{
		Username:      "operator",
		PasswordHash:  "test",
		FullName:      "Оператор",
		ProfileConfig: datatypes.JSON([]byte(`{"tickets":{"parse_phone_from_description":false}}`)),
		IsActive:      true,
	}
	if err = userRepo.Create(context.Background(), actor); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	ticket := &tickets.Ticket{
		Subject:     "Проверка телефона",
		Description: "Телефон +79254443311",
		Status:      tickets.StatusNew,
		Type:        tickets.TypeIncident,
		CompanyID:   "company-phones",
	}
	if err = ticketRepo.Create(context.Background(), ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	svc := &ticketServiceImpl{
		ticketRepo:    ticketRepo,
		userRepo:      userRepo,
		telephonyRepo: telephonyRepo,
	}
	if err = svc.bindTicketTelephonyByText(context.Background(), ticket, ticket.Description, actor.ID); err != nil {
		t.Fatalf("bindTicketTelephonyByText вернул ошибку: %v", err)
	}

	contact, err := telephonyRepo.GetContactByPhone(context.Background(), "79254443311")
	if err != nil {
		t.Fatalf("не удалось проверить контакт: %v", err)
	}
	if contact != nil {
		t.Fatalf("не ожидали создания контакта при отключённом парсинге, получили %+v", contact)
	}

	actor.ProfileConfig = datatypes.JSON([]byte(`{"tickets":{"parse_phone_from_description":true}}`))
	if err = userRepo.Update(context.Background(), actor); err != nil {
		t.Fatalf("не удалось включить парсинг: %v", err)
	}
	if err = svc.bindTicketTelephonyByText(context.Background(), ticket, ticket.Description, actor.ID); err != nil {
		t.Fatalf("bindTicketTelephonyByText после включения вернул ошибку: %v", err)
	}

	contact, err = telephonyRepo.GetContactByPhone(context.Background(), "79254443311")
	if err != nil {
		t.Fatalf("не удалось перечитать контакт: %v", err)
	}
	if contact == nil {
		t.Fatal("ожидали создание контакта после включения парсинга")
	}

	links, err := telephonyRepo.ListContactCompanyLinks(context.Background(), contact.ID)
	if err != nil {
		t.Fatalf("не удалось получить связь контакта с компанией: %v", err)
	}
	if len(links) != 1 || links[0].CompanyID != "company-phones" || time.Since(links[0].LastSeenAt) > time.Minute {
		t.Fatalf("ожидали актуальную связь контакта с компанией, получили %+v", links)
	}
}
