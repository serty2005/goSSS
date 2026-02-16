package middleware

import (
	"net/http/httptest"
	"testing"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
}

func TestStatusRecorderFlushDelegatesToUnderlyingWriter(t *testing.T) {
	base := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: base}

	rec.Flush()

	if !base.flushed {
		t.Fatal("ожидалось, что Flush будет делегирован в базовый writer")
	}
}
