package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestIngestURL(t *testing.T) {
	payload := []byte("fake-video-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Write(payload)
	}))
	defer srv.Close()
	cfg := &Config{Roots: []string{t.TempDir()}}
	path, err := IngestURL(context.Background(), cfg, srv.URL+"/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != string(payload) {
		t.Fatalf("content: %q %v", b, err)
	}
	if _, err := IngestURL(context.Background(), cfg, "file:///etc/passwd"); err == nil {
		t.Fatal("file scheme accepted")
	}
}
