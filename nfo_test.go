package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNFOAndPoster(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "clip.mp4")
	os.WriteFile(media, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "clip.nfo"), []byte(`<movie><title>NFO Title</title><tag>Tag One</tag></movie>`), 0o644)
	poster := filepath.Join(dir, "clip-poster.jpg")
	os.WriteFile(poster, []byte("jpeg"), 0o644)
	st := newTestStore(t)
	m := Media{ID: "m", Path: media, Title: "clip", AddedAt: 1}
	_ = st.Upsert(m)
	ApplyNFO(st, m)
	got, _ := st.Get("m")
	if got.Title != "NFO Title" || len(got.Tags) != 1 || got.Tags[0] != "tag-one" {
		t.Fatalf("nfo: %+v", got)
	}
	if LocalPoster(m) != poster {
		t.Fatalf("poster: %s", LocalPoster(m))
	}
}
