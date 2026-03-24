package pyrus

import (
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
