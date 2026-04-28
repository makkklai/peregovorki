package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDummyLoginHTTP(t *testing.T) {
	s := NewServer(nil, "test-secret-test-secret-test-se")
	t.Run("ok", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/dummyLogin", bytes.NewBufferString(`{"role":"user"}`))
		req.Header.Set("Content-Type", "application/json")
		s.handleDummyLogin(rec, req)
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
		var out map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&out)
		if out["token"] == nil || out["token"] == "" {
			t.Fatal("no token")
		}
	})
	t.Run("bad role", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/dummyLogin", bytes.NewBufferString(`{"role":"god"}`))
		s.handleDummyLogin(rec, req)
		if rec.Code != 400 {
			t.Fatal(rec.Code)
		}
	})
}

func TestAuthMiddlewareOK(t *testing.T) {
	s := NewServer(nil, "test-secret-test-secret-test-se")
	uid, _ := dummyUserID("user")
	tok, _ := makeToken(s.jwtSecret, uid, "user", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	var ok bool
	s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok = true
		w.WriteHeader(200)
	})).ServeHTTP(rec, req)
	if rec.Code != 200 || !ok {
		t.Fatal(rec.Code, ok)
	}
}

func TestParsePagination(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x?page=2&pageSize=50", nil)
	p, ps, ok := parsePagination(req)
	if !ok || p != 2 || ps != 50 {
		t.Fatal(p, ps, ok)
	}
}
