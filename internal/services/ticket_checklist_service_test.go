package services

import (
	"context"
	"errors"
	"testing"

	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"
)

func newChecklistTestService(t *testing.T) (TicketChecklistService, string, uint) {
	t.Helper()
	ctx := context.Background()
	db := openTicketServiceDeleteTestDB(t)
	if err := db.AutoMigrate(&tickets.TicketChecklistItem{}, &tickets.TicketChecklistItemAssignee{}, &tickets.ChecklistTemplate{}); err != nil {
		t.Fatalf("не удалось подготовить схему чеклистов: %v", err)
	}
	userRepo := repositories.NewUserRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	admin := createAdminUserForTicketDeleteTests(t, ctx, userRepo)
	companyID := createCompanyForTicketDeleteTests(t, ctx, repositories.NewCompanyRepo(db))
	ticket := &tickets.Ticket{Subject: "Тикет с чеклистом", Status: tickets.StatusNew, Priority: tickets.PriorityMedium, Type: tickets.TypeIncident, CompanyID: companyID}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	svc := NewTicketChecklistService(repositories.NewTicketChecklistRepo(db), ticketRepo, userRepo, logger.New("", "test", "error", true))
	return svc, ticket.ID, admin.ID
}

func TestChecklistCreateToggleAndDeleteSubtree(t *testing.T) {
	ctx := context.Background()
	svc, ticketID, adminID := newChecklistTestService(t)

	roots, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "Дизайн\n\nПечать\n", AssigneeIDs: []uint{adminID}}, adminID)
	if err != nil {
		t.Fatalf("CreateItems: %v", err)
	}
	if len(roots) != 2 || roots[0].Title != "Дизайн" || roots[1].Title != "Печать" || roots[1].Position != 1 {
		t.Fatalf("многострочный ввод должен создать два пункта по порядку: %+v", roots)
	}
	if len(roots[0].Assignees) != 1 || roots[0].Assignees[0].Name != "Администратор" {
		t.Fatalf("ожидали исполнителя с именем: %+v", roots[0].Assignees)
	}

	parentID := roots[0].ID
	children, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "Макет", ParentID: &parentID}, adminID)
	if err != nil {
		t.Fatalf("CreateItems child: %v", err)
	}
	childID := children[0].ID
	grandChildren, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "Цвета", ParentID: &childID}, adminID)
	if err != nil {
		t.Fatalf("CreateItems grandchild: %v", err)
	}
	tooDeepParent := grandChildren[0].ID
	if _, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "Слишком глубоко", ParentID: &tooDeepParent}, adminID); !errors.Is(err, ErrChecklistValidation) {
		t.Fatalf("ожидали ошибку глубины вложенности, получили %v", err)
	}

	done := true
	updated, err := svc.UpdateItem(ctx, ticketID, childID, ChecklistItemUpdateInput{IsDone: &done}, adminID)
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if !updated.IsDone || updated.DoneAt == nil || updated.DoneBy == nil || updated.DoneBy.ID != adminID {
		t.Fatalf("пункт должен быть отмечен выполненным с автором: %+v", updated)
	}
	notDone := false
	updated, err = svc.UpdateItem(ctx, ticketID, childID, ChecklistItemUpdateInput{IsDone: &notDone, AssigneeIDs: &[]uint{}}, adminID)
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if updated.IsDone || updated.DoneAt != nil || updated.DoneBy != nil {
		t.Fatalf("отметка выполнения должна сниматься полностью: %+v", updated)
	}

	if err := svc.DeleteItem(ctx, ticketID, parentID, adminID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	items, err := svc.List(ctx, ticketID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Title != "Печать" {
		t.Fatalf("удаление пункта должно удалять подпункты: %+v", items)
	}
}

func TestChecklistReorderRejectsCycle(t *testing.T) {
	ctx := context.Background()
	svc, ticketID, adminID := newChecklistTestService(t)
	roots, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "A\nB"}, adminID)
	if err != nil {
		t.Fatalf("CreateItems: %v", err)
	}
	parentID := roots[0].ID
	children, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "A1", ParentID: &parentID}, adminID)
	if err != nil {
		t.Fatalf("CreateItems child: %v", err)
	}
	childID := children[0].ID
	if err := svc.Reorder(ctx, ticketID, &childID, []string{parentID}); !errors.Is(err, ErrChecklistValidation) {
		t.Fatalf("ожидали запрет перемещения в собственный подпункт, получили %v", err)
	}
	if err := svc.Reorder(ctx, ticketID, nil, []string{roots[1].ID, roots[0].ID, childID}); err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	items, err := svc.List(ctx, ticketID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, item := range items {
		if item.ParentID != nil {
			t.Fatalf("после переноса на корневой уровень родителя быть не должно: %+v", item)
		}
	}
	if items[0].ID != roots[1].ID {
		t.Fatalf("ожидали новый порядок пунктов, получили %+v", items)
	}
}

func TestChecklistTemplateApply(t *testing.T) {
	ctx := context.Background()
	svc, ticketID, adminID := newChecklistTestService(t)
	if _, err := svc.CreateTemplate(ctx, ChecklistTemplateInput{Title: "  ", Items: []tickets.ChecklistTemplateNode{{Title: "X"}}}, adminID); !errors.Is(err, ErrChecklistValidation) {
		t.Fatalf("шаблон без названия должен отклоняться, получили %v", err)
	}
	template, err := svc.CreateTemplate(ctx, ChecklistTemplateInput{
		Title:    "Установка кассы",
		IsActive: true,
		Items: []tickets.ChecklistTemplateNode{
			{Title: "Подготовка", Children: []tickets.ChecklistTemplateNode{{Title: "Проверить ФН"}, {Title: " "}}},
			{Title: "Запуск"},
		},
	}, adminID)
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if nodes := template.Items.Data(); len(nodes) != 2 || len(nodes[0].Children) != 1 {
		t.Fatalf("пустые пункты шаблона должны отбрасываться: %+v", nodes)
	}
	if _, err := svc.CreateItems(ctx, ticketID, ChecklistItemCreateInput{Title: "Существующий"}, adminID); err != nil {
		t.Fatalf("CreateItems: %v", err)
	}
	created, err := svc.ApplyTemplate(ctx, ticketID, template.ID, adminID)
	if err != nil {
		t.Fatalf("ApplyTemplate: %v", err)
	}
	if len(created) != 3 {
		t.Fatalf("ожидали 3 пункта из шаблона, получили %d", len(created))
	}
	if created[0].Position != 1 || created[1].ParentID == nil || *created[1].ParentID != created[0].ID {
		t.Fatalf("пункты шаблона должны добавляться в конец с сохранением вложенности: %+v", created)
	}
	templates, err := svc.ListTemplates(ctx, true)
	if err != nil || len(templates) != 1 {
		t.Fatalf("ListTemplates: %v, %d", err, len(templates))
	}
}
