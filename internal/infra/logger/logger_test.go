package logger

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
)

func TestNewJSONHandlerWritesNormalizedJSON(t *testing.T) {
	var buffer bytes.Buffer
	handler := newJSONHandler(&buffer, slog.LevelDebug)
	log := slog.New(handler).With("component", "test").With("component", "override")

	log.Error("ошибка теста", "event", "unit_test", "error", errors.New("boom"))

	var payload map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatalf("не удалось разобрать JSON-лог: %v", err)
	}

	if payload["level"] != "error" {
		t.Fatalf("ожидался уровень error, получено %#v", payload["level"])
	}
	if payload["msg"] != "ошибка теста" {
		t.Fatalf("ожидалось сообщение \"ошибка теста\", получено %#v", payload["msg"])
	}
	if payload["component"] != "override" {
		t.Fatalf("ожидался component=override, получено %#v", payload["component"])
	}
	if payload["event"] != "unit_test" {
		t.Fatalf("ожидался event=unit_test, получено %#v", payload["event"])
	}
	if payload["error"] != "boom" {
		t.Fatalf("ожидался error=boom, получено %#v", payload["error"])
	}
}
