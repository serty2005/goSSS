package company

import (
	"context"
	"testing"

	domainCompany "etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestGetNetwork_ReturnsParentRootDescendantsAndServers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:network_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&domainCompany.Company{}, &contract.Contract{}, &models.CompanyContract{}, &server.Server{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	companyRepo := repositories.NewCompanyRepo(db)

	create := func(title string, parentID *string) *domainCompany.Company {
		t.Helper()
		item := &domainCompany.Company{Title: &title, ParentID: parentID}
		if err := companyRepo.Create(ctx, item); err != nil {
			t.Fatalf("не удалось создать компанию %q: %v", title, err)
		}
		return item
	}

	root := create("ЦО", nil)
	pointB := create("Точка Б", &root.ID)
	pointA := create("Точка А", &root.ID)
	subPoint := create("Подточка", &pointA.ID)
	create("Чужая компания", nil)

	serverIP := "srv.example.local"
	srv := &server.Server{OwnerID: &pointA.ID, IP: &serverIP}
	if err := db.Create(srv).Error; err != nil {
		t.Fatalf("не удалось создать сервер: %v", err)
	}

	svc := &serviceImpl{companyRepo: companyRepo, serverRepo: repositories.NewServerRepo(db)}

	network, err := svc.GetNetwork(ctx, pointA.ID)
	if err != nil {
		t.Fatalf("GetNetwork вернул ошибку: %v", err)
	}
	if network.RootID != root.ID {
		t.Fatalf("корнем сети должен быть прямой родитель %s, получили %s", root.ID, network.RootID)
	}

	gotOrder := make([]string, 0, len(network.Nodes))
	byID := map[string]domainCompany.NetworkNode{}
	for _, node := range network.Nodes {
		gotOrder = append(gotOrder, node.Company.ID)
		byID[node.Company.ID] = node
	}
	wantOrder := []string{root.ID, pointA.ID, pointB.ID, subPoint.ID}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("ожидали %d узлов, получили %d: %v", len(wantOrder), len(gotOrder), gotOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("неверный порядок обхода: ожидали %v, получили %v", wantOrder, gotOrder)
		}
	}

	if node := byID[subPoint.ID]; node.Depth != 2 || node.ParentID != pointA.ID {
		t.Fatalf("подточка должна быть на глубине 2 у родителя %s, получили depth=%d parent=%s", pointA.ID, node.Depth, node.ParentID)
	}
	if servers := byID[pointA.ID].Servers; len(servers) != 1 || servers[0].UUID != srv.ID {
		t.Fatalf("у точки А ожидали сервер %s, получили %+v", srv.ID, servers)
	}
	if servers := byID[pointB.ID].Servers; len(servers) != 0 {
		t.Fatalf("у точки Б не должно быть серверов, получили %+v", servers)
	}
}
