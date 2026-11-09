package httpsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerReadOnlyContract(t *testing.T) {
	s := New("127.0.0.1:0")
	h := s.Handler()

	// 尚无快照 → 503
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("before Set: code = %d, want 503", rr.Code)
	}

	payload := []byte(`{"v":1,"interval":2}`)
	s.Set(payload)

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != string(payload) {
		t.Fatalf("after Set: code=%d body=%q", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("cache-control = %q", cc)
	}

	// 写方法与未知路径均拒绝
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/status", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: code = %d, want 405", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown path: code = %d, want 404", rr.Code)
	}
}

func TestOnRequestCallback(t *testing.T) {
	s := New("127.0.0.1:0")
	called := 0
	s.SetOnRequest(func() { called++ })
	s.Set([]byte(`{}`))

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if called != 1 {
		t.Fatalf("onRequest called %d times, want 1", called)
	}
}
