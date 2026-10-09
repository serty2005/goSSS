package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"sort"
	"strings"
	"sync"
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

type fakeMaterialFileStore struct {
	mu          sync.Mutex
	objects     map[string][]byte
	disposition map[string]string
	failOnPut   int
	puts        int
}

func newFakeMaterialFileStore() *fakeMaterialFileStore {
	return &fakeMaterialFileStore{objects: map[string][]byte{}, disposition: map[string]string{}}
}

func (s *fakeMaterialFileStore) PutFile(_ context.Context, key string, body io.ReadSeeker, _ int64, _ string, contentDisposition string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if s.failOnPut > 0 && s.puts == s.failOnPut {
		return fmt.Errorf("сбой записи")
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.objects[key] = data
	s.disposition[key] = contentDisposition
	return nil
}

func (s *fakeMaterialFileStore) DeleteFile(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *fakeMaterialFileStore) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func newMaterialFilesTestHandler(t *testing.T) (*MaterialHandler, *fakeMaterialFileStore, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}
	if err := db.AutoMigrate(&company.Company{}, &server.Server{}, &workstation.Workstation{}, &fiscal.FiscalRegister{},
		&models.Material{}, &models.MaterialLink{}, &models.MaterialFile{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	title := "Ресторан"
	if err := db.Create(&company.Company{Base: common.Base{ID: "c1"}, Title: &title}).Error; err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}
	store := newFakeMaterialFileStore()
	handler := NewMaterialHandler(db, nil).WithFileStorage(store, "https://sd.example.com/material-files/", 1<<20)
	return handler, store, db
}

type uploadFile struct {
	name        string
	contentType string
	body        string
}

func buildMaterialMultipart(t *testing.T, fields map[string]string, files []uploadFile) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("не удалось записать поле: %v", err)
		}
	}
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, file.name))
		if file.contentType != "" {
			header.Set("Content-Type", file.contentType)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("не удалось создать часть: %v", err)
		}
		_, _ = part.Write([]byte(file.body))
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("не удалось закрыть multipart: %v", err)
	}
	return &buf, writer.FormDataContentType()
}

func decodeMaterial(t *testing.T, rec *httptest.ResponseRecorder) materialDTO {
	t.Helper()
	var payload struct {
		Data materialDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("некорректный JSON: %v: %s", err, rec.Body.String())
	}
	return payload.Data
}

func withURLParams(req *http.Request, params map[string]string) *http.Request {
	routeCtx := chi.NewRouteContext()
	for key, value := range params {
		routeCtx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestMaterialCreateWithFilesStoresObjectsAndReturnsDirectLinks(t *testing.T) {
	h, store, _ := newMaterialFilesTestHandler(t)
	body, contentType := buildMaterialMultipart(t, map[string]string{
		"subject":     "Инструкция",
		"entity_refs": `[{"entity_type":"Company","entity_id":"c1"}]`,
	}, []uploadFile{{name: "../схема сети.pdf", contentType: "application/pdf", body: "PDF-DATA"}})
	req := httptest.NewRequest(http.MethodPost, "/api/materials", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("ожидался 201, получен %d: %s", rec.Code, rec.Body.String())
	}
	material := decodeMaterial(t, rec)
	if material.Content != "" || len(material.Files) != 1 {
		t.Fatalf("ожидался материал без текста с одним файлом: %+v", material)
	}
	file := material.Files[0]
	if file.Name != "схема сети.pdf" {
		t.Fatalf("имя файла не очищено от пути: %q", file.Name)
	}
	wantPrefix := "https://sd.example.com/material-files/" + material.ID + "/" + file.ID + "/"
	if !strings.HasPrefix(file.URL, wantPrefix) || !strings.Contains(file.URL, "%D1%81%D1%85%D0%B5%D0%BC%D0%B0") {
		t.Fatalf("некорректная прямая ссылка: %s", file.URL)
	}
	keys := store.keys()
	if len(keys) != 1 || string(store.objects[keys[0]]) != "PDF-DATA" {
		t.Fatalf("объект не сохранен в хранилище: %v", keys)
	}
	if !strings.HasPrefix(store.disposition[keys[0]], "attachment") {
		t.Fatalf("файл должен отдаваться как вложение, получено %q", store.disposition[keys[0]])
	}
}

func TestMaterialCreateWithoutContentAndFilesIsRejected(t *testing.T) {
	h, store, _ := newMaterialFilesTestHandler(t)
	payload := `{"subject":"Пусто","content":"","entity_refs":[{"entity_type":"Company","entity_id":"c1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/materials", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400, получен %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.keys()) != 0 {
		t.Fatalf("в хранилище не должно быть объектов")
	}
}

func TestMaterialCreateRollsBackStoredObjectsOnPartialFailure(t *testing.T) {
	h, store, db := newMaterialFilesTestHandler(t)
	store.failOnPut = 2
	body, contentType := buildMaterialMultipart(t, map[string]string{
		"subject":     "Два файла",
		"entity_refs": `[{"entity_type":"Company","entity_id":"c1"}]`,
	}, []uploadFile{{name: "a.txt", body: "a"}, {name: "b.txt", body: "b"}})
	req := httptest.NewRequest(http.MethodPost, "/api/materials", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("ожидался 502, получен %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.keys()) != 0 {
		t.Fatalf("загруженный объект должен быть удален, остались: %v", store.keys())
	}
	var count int64
	db.Model(&models.Material{}).Count(&count)
	if count != 0 {
		t.Fatalf("материал не должен создаваться при ошибке загрузки")
	}
}

func TestMaterialUploadRejectsOversizedFile(t *testing.T) {
	h, store, _ := newMaterialFilesTestHandler(t)
	body, contentType := buildMaterialMultipart(t, map[string]string{
		"subject":     "Большой",
		"entity_refs": `[{"entity_type":"Company","entity_id":"c1"}]`,
	}, []uploadFile{{name: "big.bin", body: strings.Repeat("x", (1<<20)+1)}})
	req := httptest.NewRequest(http.MethodPost, "/api/materials", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("ожидался 413, получен %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.keys()) != 0 {
		t.Fatalf("в хранилище не должно быть объектов")
	}
}

func TestMaterialFilesLifecycleUploadDeleteAndMaterialRemoval(t *testing.T) {
	h, store, db := newMaterialFilesTestHandler(t)
	material := models.Material{Base: common.Base{ID: "m1"}, Subject: "Заметка", Content: ""}
	if err := db.Create(&material).Error; err != nil {
		t.Fatalf("не удалось создать материал: %v", err)
	}
	if err := db.Create(&models.MaterialLink{MaterialID: "m1", EntityType: "Company", EntityID: "c1"}).Error; err != nil {
		t.Fatalf("не удалось создать связь: %v", err)
	}

	body, contentType := buildMaterialMultipart(t, nil, []uploadFile{{name: "one.txt", body: "1"}, {name: "two.txt", body: "2"}})
	req := withURLParams(httptest.NewRequest(http.MethodPost, "/api/materials/m1/files", body), map[string]string{"id": "m1"})
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.UploadFiles(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ожидался 201, получен %d: %s", rec.Code, rec.Body.String())
	}
	dto := decodeMaterial(t, rec)
	if len(dto.Files) != 2 || len(store.keys()) != 2 {
		t.Fatalf("ожидалось два файла: %+v, объектов %d", dto.Files, len(store.keys()))
	}

	deleteFile := func(fileID string) *httptest.ResponseRecorder {
		req := withURLParams(httptest.NewRequest(http.MethodDelete, "/api/materials/m1/files/"+fileID, nil),
			map[string]string{"id": "m1", "fileID": fileID})
		rec := httptest.NewRecorder()
		h.DeleteFile(rec, req)
		return rec
	}
	if rec := deleteFile(dto.Files[0].ID); rec.Code != http.StatusNoContent {
		t.Fatalf("ожидался 204, получен %d: %s", rec.Code, rec.Body.String())
	}
	if len(store.keys()) != 1 {
		t.Fatalf("объект первого файла должен быть удален: %v", store.keys())
	}
	if rec := deleteFile(dto.Files[1].ID); rec.Code != http.StatusConflict {
		t.Fatalf("последний файл материала без текста удалять нельзя, получен %d", rec.Code)
	}

	delReq := withURLParams(httptest.NewRequest(http.MethodDelete, "/api/materials/m1", nil), map[string]string{"id": "m1"})
	delRec := httptest.NewRecorder()
	h.Delete(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("ожидался 204, получен %d: %s", delRec.Code, delRec.Body.String())
	}
	if len(store.keys()) != 0 {
		t.Fatalf("при удалении материала объекты должны удаляться: %v", store.keys())
	}
}

func TestMaterialUploadDisabledWithoutStorage(t *testing.T) {
	h, _, _ := newMaterialFilesTestHandler(t)
	h.fileStore = nil
	body, contentType := buildMaterialMultipart(t, map[string]string{"subject": "x"}, []uploadFile{{name: "a.txt", body: "a"}})
	req := httptest.NewRequest(http.MethodPost, "/api/materials", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ожидался 503, получен %d", rec.Code)
	}
}

func TestSanitizeMaterialFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":           "report.pdf",
		`C:\Users\me\doc.docx`: "doc.docx",
		"../../etc/passwd":     "passwd",
		".hidden":              "hidden",
		"   ":                  "file",
		"a\"b\x00c.txt":        "abc.txt",
	}
	for input, want := range cases {
		if got := sanitizeMaterialFileName(input); got != want {
			t.Errorf("sanitizeMaterialFileName(%q) = %q, ожидалось %q", input, got, want)
		}
	}
	long := strings.Repeat("я", 300) + ".pdf"
	if got := sanitizeMaterialFileName(long); len([]rune(got)) != materialMaxFileNameRunes || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("длинное имя не обрезано корректно: %d рун", len([]rune(got)))
	}
}

func TestMaterialCreateWithoutEntityRefsAndListOnlyWithFiles(t *testing.T) {
	h, store, _ := newMaterialFilesTestHandler(t)

	body, contentType := buildMaterialMultipart(t, map[string]string{"subject": "Общий файл"}, []uploadFile{{name: "общий.pdf", body: "x"}})
	req := httptest.NewRequest(http.MethodPost, "/api/materials", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("материал без привязок должен создаваться, получен %d: %s", rec.Code, rec.Body.String())
	}
	withFile := decodeMaterial(t, rec)
	if len(withFile.EntityRefs) != 0 || len(withFile.Files) != 1 {
		t.Fatalf("ожидался материал без привязок с файлом: %+v", withFile)
	}

	textReq := httptest.NewRequest(http.MethodPost, "/api/materials",
		strings.NewReader(`{"subject":"Заметка","content":"текст","entity_refs":[]}`))
	textReq.Header.Set("Content-Type", "application/json")
	textRec := httptest.NewRecorder()
	h.Create(textRec, textReq)
	if textRec.Code != http.StatusCreated {
		t.Fatalf("текстовый материал без привязок должен создаваться, получен %d: %s", textRec.Code, textRec.Body.String())
	}

	// Обновление без привязок не падает на пустом списке связей.
	updateReq := withURLParams(httptest.NewRequest(http.MethodPut, "/api/materials/"+withFile.ID,
		strings.NewReader(`{"subject":"Общий файл 2","content":"","entity_refs":[]}`)), map[string]string{"id": withFile.ID})
	updateRec := httptest.NewRecorder()
	h.Update(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("обновление без привязок: %d: %s", updateRec.Code, updateRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/materials?has_files=true", nil)
	listRec := httptest.NewRecorder()
	h.List(listRec, listReq)
	var payload struct {
		Data []materialDTO `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("некорректный JSON: %v: %s", err, listRec.Body.String())
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != withFile.ID || len(store.keys()) != 1 {
		t.Fatalf("has_files должен вернуть только материал с файлом: %+v", payload.Data)
	}
}
