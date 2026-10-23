package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPollFeedIngestsEnclosure(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "ep.mp4")
	os.WriteFile(media, []byte("fake"), 0o644)
	mux := http.NewServeMux()
	mux.HandleFunc("/ep.mp4", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("fake")) })
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>
<item><title>Ep</title><guid>ep1</guid><enclosure url="http://` + r.Host + `/ep.mp4" type="video/mp4"/></item>
</channel></rss>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	st := newTestStore(t)
	feed, err := st.AddFeed(srv.URL+"/feed.xml", "test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{Roots: []string{dir}, IngestAllowPrivate: true}
	n, err := PollFeed(context.Background(), cfg, st, feed)
	if err != nil || n != 1 {
		t.Fatalf("poll: %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_ingest", "ep.mp4")); err != nil {
		t.Fatalf("ingested file missing: %v", err)
	}
	// A second poll must not re-ingest the same GUID.
	n, _ = PollFeed(context.Background(), cfg, st, feed)
	if n != 0 {
		t.Fatalf("duplicate ingest: %d", n)
	}
}

func TestIsMediaURL(t *testing.T) {
	for _, u := range []string{"http://x/a.mp4", "https://x/b.m3u8?t=1", "http://x/c.MKV"} {
		if !isMediaURL(u) {
			t.Fatalf("rejected %s", u)
		}
	}
	if isMediaURL("http://x/page.html") {
		t.Fatal("accepted html")
	}
}
