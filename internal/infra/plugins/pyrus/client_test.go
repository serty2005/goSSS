package pyrus

import (
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientGetTaskParsesWrappedTaskResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/tasks/11613" {
			t.Fatalf("неожиданный путь запроса: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":{"id":11613,"form_id":2315062,"text":"Тестовая задача"}}`))
	}))
	defer server.Close()

	client := &Client{
		configured:  true,
		accessToken: "token",
		apiURL:      server.URL + "/v4/",
		httpClient:  server.Client(),
	}

	task, err := client.GetTask(t.Context(), 11613)
	if err != nil {
		t.Fatalf("GetTask вернул ошибку: %v", err)
	}
	if task == nil || task.ID != 11613 || task.FormID != 2315062 || task.Text != "Тестовая задача" {
		t.Fatalf("ожидали корректно распарсенную задачу, получили %+v", task)
	}
}

func TestClientAddCommentParsesWrappedTaskResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/tasks/11613/comments" {
			t.Fatalf("неожиданный путь запроса: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":{"id":11613,"comments":[{"id":7001,"text":"Ответ оператора","action":""}]}}`))
	}))
	defer server.Close()

	client := &Client{
		configured:  true,
		accessToken: "token",
		apiURL:      server.URL + "/v4/",
		httpClient:  server.Client(),
	}

	task, err := client.AddComment(t.Context(), 11613, CommentRequest{Text: "Ответ оператора"})
	if err != nil {
		t.Fatalf("AddComment вернул ошибку: %v", err)
	}
	if task == nil || task.ID != 11613 || len(task.Comments) != 1 || task.Comments[0].ID != 7001 {
		t.Fatalf("ожидали корректно распарсенный ответ AddComment, получили %+v", task)
	}
}

func TestClientUploadFileParsesGUID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/files/upload" {
			t.Fatalf("неожиданный путь запроса: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("неожиданный метод запроса: %s", r.Method)
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("не удалось разобрать Content-Type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Fatalf("ожидали multipart/form-data, получили %s", mediaType)
		}
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("не удалось получить multipart reader: %v", err)
		}
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("не удалось получить часть multipart: %v", err)
		}
		if part.FormName() != "file" {
			t.Fatalf("ожидали form-data name=file, получили %s", part.FormName())
		}
		if part.FileName() != "report.txt" {
			t.Fatalf("ожидали имя файла report.txt, получили %s", part.FileName())
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("не удалось прочитать часть multipart: %v", err)
		}
		if string(body) != "hello pyrus" {
			t.Fatalf("ожидали содержимое hello pyrus, получили %q", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"guid":"guid-123"}`))
	}))
	defer server.Close()

	client := &Client{
		configured:  true,
		accessToken: "token",
		apiURL:      server.URL + "/v4/",
		httpClient:  server.Client(),
	}

	guid, err := client.UploadFile(t.Context(), "report.txt", "text/plain", []byte("hello pyrus"))
	if err != nil {
		t.Fatalf("UploadFile вернул ошибку: %v", err)
	}
	if guid != "guid-123" {
		t.Fatalf("ожидали guid=guid-123, получили %q", guid)
	}
}
