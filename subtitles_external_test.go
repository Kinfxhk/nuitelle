package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSidecars(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "clip.mp4")
	os.WriteFile(media, []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "clip.en.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nHi\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "clip.zh-Hant.vtt"), []byte("WEBVTT\n"), 0o644)
	st := newTestStore(t)
	m := Media{ID: "m", Path: media, Title: "clip", AddedAt: 1}
	_ = st.Upsert(m)
	DiscoverSidecars(st, m)
	subs, err := st.ListSubtitles("m")
	if err != nil || len(subs) != 2 {
		t.Fatalf("subs: %+v %v", subs, err)
	}
}
