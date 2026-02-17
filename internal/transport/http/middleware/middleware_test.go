package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestTimeoutUnlessSkipsConfiguredRequests(t *testing.T) {
	deadlineSeen := make(chan bool, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline := r.Context().Deadline()
		deadlineSeen <- hasDeadline
		w.WriteHeader(http.StatusOK)
	})

	withSkip := TimeoutUnless(10*time.Millisecond, func(r *http.Request) bool {
		return r.URL.Path == "/api/events"
	})(handler)

	reqSkip := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	recSkip := httptest.NewRecorder()
	withSkip.ServeHTTP(recSkip, reqSkip)
	if recSkip.Code != http.StatusOK {
		t.Fatalf("ожидался статус %d для исключённого пути, получен %d", http.StatusOK, recSkip.Code)
	}
	if hasDeadline := <-deadlineSeen; hasDeadline {
		t.Fatal("для исключённого пути не ожидался deadline в контексте")
	}

	reqRegular := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	recRegular := httptest.NewRecorder()
	withSkip.ServeHTTP(recRegular, reqRegular)
	if recRegular.Code != http.StatusOK {
		t.Fatalf("ожидался статус %d для обычного пути, получен %d", http.StatusOK, recRegular.Code)
	}
	if hasDeadline := <-deadlineSeen; !hasDeadline {
		t.Fatal("для обычного пути ожидался deadline в контексте")
	}
}
