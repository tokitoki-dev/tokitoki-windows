package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckReturnsUpdateWithAbsoluteDownloadURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"available": true,
			"version":   "1.2.0",
			"url":       "/api/updates/download/windows/1.2.0/windows/amd64",
		})
	}))
	defer server.Close()

	update, err := Check(context.Background(), server.URL, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if update == nil {
		t.Fatal("Check() = nil, want update")
	}
	if update.Version != "1.2.0" {
		t.Fatalf("version = %q, want 1.2.0", update.Version)
	}
	want := server.URL + "/api/updates/download/windows/1.2.0/windows/amd64"
	if update.DownloadURL != want {
		t.Fatalf("download URL = %q, want %q", update.DownloadURL, want)
	}
	if gotPath == "" || !containsAll(gotPath, "channel=windows", "platform=windows", "version=1.0.0") {
		t.Fatalf("request = %q, want channel/platform/version params", gotPath)
	}
}

func TestCheckReturnsNilWhenCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"available": false})
	}))
	defer server.Close()

	update, err := Check(context.Background(), server.URL, "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if update != nil {
		t.Fatalf("Check() = %+v, want nil", update)
	}
}

func TestCheckRejectsDevBuild(t *testing.T) {
	_, err := Check(context.Background(), "http://localhost:0", "dev")
	if !errors.Is(err, ErrDevBuild) {
		t.Fatalf("Check() error = %v, want ErrDevBuild", err)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
