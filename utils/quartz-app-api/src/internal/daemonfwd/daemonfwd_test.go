package daemonfwd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusProxiesSnapshot(t *testing.T) {
	payload := `{"v":1,"ts":1760000000,"host":{"hostname":"r"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/status" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := New(srv.URL + "/api/v1/status")
	raw, err := c.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != payload {
		t.Fatalf("raw=%s", raw)
	}
}

func TestStatusErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"http500", 500, `{"err":1}`},
		{"notjson", 200, `oops`},
		{"array", 200, `[1,2]`},
		{"empty", 200, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := New(srv.URL)
			if _, err := c.Status(context.Background()); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestStatusTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * Timeout)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	start := time.Now()
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > 2*Timeout+time.Second {
		t.Fatalf("timeout not enforced: %v", time.Since(start))
	}
}

func TestStatusTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"x":"` + strings.Repeat("a", MaxBody) + `"}`))
	}))
	defer srv.Close()
	c := New(srv.URL)
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("want too-large error")
	}
}
