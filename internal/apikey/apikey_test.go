package apikey

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySendsKeyOnlyInHeader(t *testing.T) {
	var got *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		w.Write([]byte(`{"valid":true}`))
	}))
	defer server.Close()

	valid, err := NewVerifier(server.URL+"/").Verify(context.Background(), "secret-key")
	if err != nil || !valid {
		t.Fatalf("Verify() = %v, %v, want true, nil", valid, err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/api/auth/api-key/verify" {
		t.Fatalf("request = %s %s, want POST /api/auth/api-key/verify", got.Method, got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want bearer key", got.Header.Get("Authorization"))
	}
	if got.URL.RawQuery != "" {
		t.Fatalf("query = %q, want key kept out of the URL", got.URL.RawQuery)
	}
}

func TestVerifyRejectedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	valid, err := NewVerifier(server.URL).Verify(context.Background(), "revoked")
	if err != nil {
		t.Fatalf("Verify() error = %v, want nil for a definite rejection", err)
	}
	if valid {
		t.Fatal("Verify() = true, want false on 401")
	}
}

func TestVerifyUnavailable(t *testing.T) {
	responses := []func(w http.ResponseWriter){
		func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) },
		func(w http.ResponseWriter) { w.Write([]byte(`not json`)) },
		func(w http.ResponseWriter) { w.Write([]byte(`{"valid":false}`)) },
	}
	for _, respond := range responses {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			respond(w)
		}))
		_, err := NewVerifier(server.URL).Verify(context.Background(), "key")
		server.Close()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Verify() error = %v, want ErrUnavailable", err)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(nil))
	server.Close() // unreachable server
	if _, err := NewVerifier(server.URL).Verify(context.Background(), "key"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Verify() error = %v, want ErrUnavailable for network failure", err)
	}
}
