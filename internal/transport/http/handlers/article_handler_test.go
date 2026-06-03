package handlers

import (
	"encoding/json"
	"etalon-server/internal/domain/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestArticleWebhookHandler_RejectsEmptyConfiguredKey(t *testing.T) {
	db := newArticleHandlerTestDB(t)
	h := NewArticleHandler(db, nil, "")
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/articles/webhook",
		strings.NewReader(`{"title":"Webhook публикация","content":"Текст"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	h.CreateFromWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался код %d, получен %d", http.StatusUnauthorized, rec.Code)
	}
	assertArticleCount(t, db, 0)
}

func TestArticleWebhookHandler_RejectsInvalidKey(t *testing.T) {
	db := newArticleHandlerTestDB(t)
	h := NewArticleHandler(db, nil, "secret")
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/articles/webhook",
		strings.NewReader(`{"title":"Webhook публикация","content":"Текст"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()

	h.CreateFromWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался код %d, получен %d", http.StatusUnauthorized, rec.Code)
	}
	assertArticleCount(t, db, 0)
}

func TestArticleWebhookHandler_CreatesPublication(t *testing.T) {
	db := newArticleHandlerTestDB(t)
	h := NewArticleHandler(db, nil, "secret")
	body := `{
		"title":"Webhook публикация",
		"summary":"Анонс",
		"content":"Текст публикации",
		"type":"company_news",
		"status":"published",
		"show_on_home":true,
		"tags":["webhook","новости"],
		"author_name":"CI"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/articles/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret")
	rec := httptest.NewRecorder()

	h.CreateFromWebhook(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("ожидался код %d, получен %d: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
		Data   struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			ShowOnHome bool   `json:"show_on_home"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("не удалось разобрать ответ: %v", err)
	}
	if resp.Status != "success" {
		t.Fatalf("ожидали успешный ответ, получен %q", resp.Status)
	}
	if resp.Data.ID == "" {
		t.Fatal("ожидали идентификатор созданной публикации")
	}
	if resp.Data.Title != "Webhook публикация" || !resp.Data.ShowOnHome {
		t.Fatalf("ответ содержит некорректные данные публикации: %+v", resp.Data)
	}

	var item models.Article
	if err := db.First(&item, "id = ?", resp.Data.ID).Error; err != nil {
		t.Fatalf("не удалось найти созданную публикацию: %v", err)
	}
	if item.Status != models.ArticleStatusPublished {
		t.Fatalf("ожидался статус %q, получен %q", models.ArticleStatusPublished, item.Status)
	}
	if !item.ShowOnHome {
		t.Fatal("ожидали публикацию с флагом show_on_home")
	}
	if item.AuthorID != nil {
		t.Fatalf("у webhook-публикации не должно быть author_id, получен %v", *item.AuthorID)
	}
	if item.AuthorName != "CI" {
		t.Fatalf("ожидался автор CI, получен %q", item.AuthorName)
	}
	if item.PublishedAt == nil {
		t.Fatal("ожидали дату публикации для published-статьи")
	}
	if item.Tags != "webhook,новости" {
		t.Fatalf("ожидались теги webhook,новости, получено %q", item.Tags)
	}
}

func newArticleHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть тестовую БД: %v", err)
	}
	if err := db.AutoMigrate(&models.Article{}, &models.ArticleLink{}); err != nil {
		t.Fatalf("не удалось выполнить миграцию тестовой БД: %v", err)
	}
	return db
}

func assertArticleCount(t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.Model(&models.Article{}).Count(&count).Error; err != nil {
		t.Fatalf("не удалось посчитать публикации: %v", err)
	}
	if count != expected {
		t.Fatalf("ожидалось публикаций: %d, получено: %d", expected, count)
	}
}
