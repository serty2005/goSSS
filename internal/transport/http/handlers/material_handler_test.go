package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

func TestMaterialHandlerCompanyScopeCollectsCompanyParentAndEquipment(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}
	if err := db.AutoMigrate(&company.Company{}, &server.Server{}, &workstation.Workstation{}, &fiscal.FiscalRegister{}, &models.Material{}, &models.MaterialLink{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	strPtr := func(v string) *string { return &v }
	parent := company.Company{Base: common.Base{ID: "parent"}, Title: strPtr("Сеть")}
	child := company.Company{Base: common.Base{ID: "child"}, Title: strPtr("Ресторан"), ParentID: strPtr("parent")}
	other := company.Company{Base: common.Base{ID: "other"}, Title: strPtr("Чужая")}
	for _, item := range []company.Company{parent, child, other} {
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("не удалось создать компанию: %v", err)
		}
	}
	srv := server.Server{Base: common.Base{ID: "srv"}, DeviceName: strPtr("RMS"), OwnerID: strPtr("child")}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatalf("не удалось создать сервер: %v", err)
	}

	createMaterial := func(id string, refs ...models.MaterialLink) {
		material := models.Material{Base: common.Base{ID: id}, Subject: "Материал " + id, Content: "Текст"}
		if err := db.Create(&material).Error; err != nil {
			t.Fatalf("не удалось создать материал: %v", err)
		}
		for _, ref := range refs {
			ref.MaterialID = id
			if err := db.Create(&ref).Error; err != nil {
				t.Fatalf("не удалось создать связь материала: %v", err)
			}
		}
	}
	createMaterial("m-company", models.MaterialLink{EntityType: "Company", EntityID: "child"})
	createMaterial("m-parent", models.MaterialLink{EntityType: "Company", EntityID: "parent"})
	createMaterial("m-server", models.MaterialLink{EntityType: "Server", EntityID: "srv"}, models.MaterialLink{EntityType: "Company", EntityID: "other"})
	createMaterial("m-other", models.MaterialLink{EntityType: "Company", EntityID: "other"})

	h := NewMaterialHandler(db, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/materials/company-scope/child", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("companyID", "child")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()

	h.CompanyScope(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался код 200, получен %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []companyScopedMaterialDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("некорректный JSON: %v", err)
	}
	scopes := map[string]string{}
	for _, item := range payload.Data {
		if len(item.Sources) != 1 {
			t.Fatalf("у материала %s должен быть ровно один релевантный источник: %+v", item.ID, item.Sources)
		}
		scopes[item.ID] = item.Sources[0].Scope
	}
	expected := map[string]string{"m-company": "company", "m-parent": "parent", "m-server": "equipment"}
	if len(scopes) != len(expected) {
		t.Fatalf("ожидали материалы %v, получили %v", expected, scopes)
	}
	for id, scope := range expected {
		if scopes[id] != scope {
			t.Fatalf("для %s ожидали источник %s, получили %s", id, scope, scopes[id])
		}
	}
}
