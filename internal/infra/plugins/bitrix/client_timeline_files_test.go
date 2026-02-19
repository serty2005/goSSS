package bitrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
)

func TestExtractTimelineCommentDiskFileIDs_RegexAndDedupe(t *testing.T) {
	raw := map[string]interface{}{
		"COMMENT": "text [DISK FILE ID=n36447] [DISK FILE ID=36448] [DISK FILE ID=n36447]",
		"FILES": map[string]interface{}{
			"36448": map[string]interface{}{"id": 36448},
			"50001": map[string]interface{}{"id": 50001},
		},
	}

	got := ExtractTimelineCommentDiskFileIDs(raw)
	want := []int64{36448, 50001, 36447}
	if len(got) != len(want) {
		t.Fatalf("ожидалось %d id, получено %d: %#v", len(want), len(got), got)
	}
	seen := make(map[int64]struct{}, len(got))
	for _, id := range got {
		seen[id] = struct{}{}
	}
	for _, id := range want {
		if _, ok := seen[id]; !ok {
			t.Fatalf("ожидался id=%d, но его нет в результате: %#v", id, got)
		}
	}
}

func TestBuildTimelineCommentAddFields_WithFiles(t *testing.T) {
	fields := buildTimelineCommentAddFields("deal", 101, "comment", []FileToUpload{
		{Name: "img.png", Base64Content: "data:image/png;base64,dGVzdA=="},
		{Name: "log.txt", Base64Content: "dGVzdDI="},
	})

	if fields["ENTITY_TYPE"] != "deal" {
		t.Fatalf("ожидался ENTITY_TYPE=deal, получено %#v", fields["ENTITY_TYPE"])
	}
	if fields["ENTITY_ID"] != int64(101) {
		t.Fatalf("ожидался ENTITY_ID=101, получено %#v", fields["ENTITY_ID"])
	}
	files, ok := fields["FILES"].([][]string)
	if !ok {
		t.Fatalf("FILES должен быть [][]string, получено %#v", fields["FILES"])
	}
	if len(files) != 2 {
		t.Fatalf("ожидалось 2 файла в FILES, получено %d", len(files))
	}
	if len(files[0]) != 2 || files[0][0] != "img.png" || files[0][1] != "dGVzdA==" {
		t.Fatalf("неверный первый элемент FILES: %#v", files[0])
	}
}

func TestBuildTimelineCommentUpdateFields_WithFiles(t *testing.T) {
	fields := buildTimelineCommentUpdateFields("updated", []FileToUpload{
		{Name: "a.txt", Base64Content: "YQ=="},
		{Name: "b.txt", Base64Content: "Yg=="},
	}, true)

	if fields["COMMENT"] != "updated" {
		t.Fatalf("ожидался COMMENT=updated, получено %#v", fields["COMMENT"])
	}
	files, ok := fields["FILES"].([][]string)
	if !ok || len(files) != 2 {
		t.Fatalf("ожидался полный список FILES из 2 файлов, получено %#v", fields["FILES"])
	}
}

func TestTimelineCommentAddWithFiles_SendsFilesInExpectedFormat(t *testing.T) {
	var (
		mu       sync.Mutex
		payloads []map[string]interface{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.add.json") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		defer r.Body.Close()
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		payloads = append(payloads, body)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": 777})
	}))
	defer server.Close()

	cfg := &config.Config{
		BitrixBaseURL:  server.URL + "/rest/1/key",
		RequestTimeout: 2 * time.Second,
	}
	client := NewClient(cfg, logger.New("", "test", "error", true))

	id, err := client.TimelineCommentAddWithFiles(context.Background(), "deal", 45, "text", []FileToUpload{
		{Name: "one.txt", Base64Content: "b25l"},
	})
	if err != nil {
		t.Fatalf("ошибка addWithFiles: %v", err)
	}
	if id != 777 {
		t.Fatalf("ожидался id=777, получено %d", id)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) != 1 {
		t.Fatalf("ожидался 1 запрос, получено %d", len(payloads))
	}
	fields, ok := payloads[0]["fields"].(map[string]interface{})
	if !ok {
		t.Fatalf("поле fields отсутствует или неверного типа: %#v", payloads[0]["fields"])
	}
	files, ok := fields["FILES"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("ожидался FILES из 1 элемента, получено %#v", fields["FILES"])
	}
	fileData, ok := files[0].([]interface{})
	if !ok || len(fileData) != 2 {
		t.Fatalf("FILES[0] имеет неверный формат: %#v", files[0])
	}
	if fileData[0] != "one.txt" || fileData[1] != "b25l" {
		t.Fatalf("неверный FILES[0]: %#v", fileData)
	}
}

func TestTimelineCommentUpdateWithFiles_SendsFullFilesList(t *testing.T) {
	var (
		mu      sync.Mutex
		payload map[string]interface{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.update.json") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": true})
	}))
	defer server.Close()

	cfg := &config.Config{
		BitrixBaseURL:  server.URL + "/rest/1/key",
		RequestTimeout: 2 * time.Second,
	}
	client := NewClient(cfg, logger.New("", "test", "error", true))

	err := client.TimelineCommentUpdateWithFiles(context.Background(), 501, "updated", []FileToUpload{
		{Name: "a.txt", Base64Content: "YQ=="},
		{Name: "b.txt", Base64Content: "Yg=="},
	})
	if err != nil {
		t.Fatalf("ошибка updateWithFiles: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	fields, ok := payload["fields"].(map[string]interface{})
	if !ok {
		t.Fatalf("поле fields отсутствует или неверного типа: %#v", payload["fields"])
	}
	files, ok := fields["FILES"].([]interface{})
	if !ok {
		t.Fatalf("поле FILES отсутствует: %#v", fields["FILES"])
	}
	if len(files) != 2 {
		t.Fatalf("ожидался полный список FILES из 2 элементов, получено %d", len(files))
	}
}
