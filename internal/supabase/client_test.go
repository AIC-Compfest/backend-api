package supabase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckSendsPublishableKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/v1/" { t.Fatalf("path = %s", r.URL.Path) }
		if r.Header.Get("apikey") != "test-key" { t.Fatal("missing apikey") }
		w.WriteHeader(http.StatusOK)
	})); defer server.Close()
	if err := NewClient(server.URL, "test-key").Check(context.Background()); err != nil { t.Fatal(err) }
}
func TestCheckRejectsErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })); defer server.Close()
	if err := NewClient(server.URL, "test-key").Check(context.Background()); err == nil { t.Fatal("expected error") }
}
