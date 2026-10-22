package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestUpsertListSearch(t *testing.T) {
	st := newTestStore(t)
	for i, name := range []string{"alpha", "beta", "gamma"} {
		m := Media{ID: mediaID("/tmp/" + name), Path: "/tmp/" + name + ".mp4", Title: name, Size: int64(i + 1), AddedAt: int64(100 + i)}
		if err := st.Upsert(m); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := st.List("", "", "new", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("want 3, got %d/%d", len(items), total)
	}
	items, total, _ = st.List("bet", "", "", 10, 0)
	if total != 1 || len(items) != 1 || items[0].Title != "beta" {
		t.Fatalf("search failed: %+v", items)
	}
	// % must be escaped, not act as a wildcard.
	_, total, _ = st.List("%", "", "", 10, 0)
	if total != 0 {
		t.Fatalf("wildcard was not escaped: %d", total)
	}
}

func TestTagsAndProgress(t *testing.T) {
	st := newTestStore(t)
	m := Media{ID: "abc", Path: "/tmp/a.mp4", Title: "a", AddedAt: 1}
	if err := st.Upsert(m); err != nil {
		t.Fatal(err)
	}
	if err := st.AddTags("abc", []string{"one", "two", "one"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("want 2 tags, got %v", got.Tags)
	}
	if err := st.RemoveTag("abc", "one"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get("abc")
	if len(got.Tags) != 1 || got.Tags[0] != "two" {
		t.Fatalf("remove failed: %v", got.Tags)
	}
	if err := st.SetProgress("abc", "default", 1234); err != nil {
		t.Fatal(err)
	}
	if p := st.Progress("abc", "default"); p != 1234 {
		t.Fatalf("progress: %d", p)
	}
}

func TestTrashRecordAndLayout(t *testing.T) {
	st := newTestStore(t)
	if err := st.RecordTrash("abc", "/a.mp4", "/trash/a.mp4"); err != nil {
		t.Fatal(err)
	}
	orig, moved, ok := st.TrashRecord("abc")
	if !ok || orig != "/a.mp4" || moved != "/trash/a.mp4" {
		t.Fatalf("trash record: %v %v %v", orig, moved, ok)
	}
	if err := st.SetLayout(`{"version":1}`); err != nil {
		t.Fatal(err)
	}
	if got := st.Layout(); got != `{"version":1}` {
		t.Fatalf("layout: %s", got)
	}
	if err := st.SetLayout(`not json`); err == nil {
		t.Fatal("expected invalid layout to fail")
	}
}

func TestMediaIDStable(t *testing.T) {
	a := mediaID("/tmp/x.mp4")
	b := mediaID("/tmp/x.mp4")
	c := mediaID("/tmp/y.mp4")
	if a != b || a == c || len(a) != 16 {
		t.Fatalf("ids: %s %s %s", a, b, c)
	}
	if titleOf("/tmp/My Clip.final.mp4") != "My Clip.final" {
		t.Fatalf("title: %s", titleOf("/tmp/My Clip.final.mp4"))
	}
}

func TestFTSSearch(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "a", Path: "/media/portrait.mp4", Title: "Portrait", AddedAt: 1})
	_ = st.Upsert(Media{ID: "b", Path: "/media/other.mp4", Title: "Other", AddedAt: 1})
	_ = st.AddTags("a", []string{"solo"})
	items, total, err := st.List("port", "", "", 10, 0)
	if err != nil || total != 1 || items[0].ID != "a" {
		t.Fatalf("title fts: %d %v", total, err)
	}
	items, total, err = st.List("solo", "", "", 10, 0)
	if err != nil || total != 1 || items[0].ID != "a" {
		t.Fatalf("tag fts: %d %v", total, err)
	}
	if err := st.RebuildFTS(); err != nil {
		t.Fatal(err)
	}
	_, total, _ = st.List("other", "", "", 10, 0)
	if total != 1 {
		t.Fatalf("rebuild: %d", total)
	}
}

func TestJobsLifecycle(t *testing.T) {
	st := newTestStore(t)
	if err := st.StartJob("batch", "batch", "", 4); err != nil {
		t.Fatal(err)
	}
	st.UpdateJob("batch", 0.5)
	jobs, err := st.Jobs()
	if err != nil || len(jobs) != 1 || jobs[0]["state"] != "running" {
		t.Fatalf("jobs: %+v %v", jobs, err)
	}
	st.FinishJob("batch", "done", "")
	jobs, _ = st.Jobs()
	if jobs[0]["state"] != "done" {
		t.Fatalf("finish: %+v", jobs[0])
	}
}

func TestNormalizeLayout(t *testing.T) {
	out := normalizeLayout(`{"sidebar":[{"id":"home","label":"Home"}]}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	if m["version"] != float64(1) {
		t.Fatalf("version: %v", m["version"])
	}
	sb := m["sidebar"].([]any)
	if len(sb) != 2 {
		t.Fatalf("sidebar: %v", sb)
	}
	found := false
	for _, it := range sb {
		o := it.(map[string]any)
		if o["id"] == "favorites" {
			found = true
		}
		if o["id"] == "home" && o["order"] != float64(10) {
			t.Fatalf("order: %v", o["order"])
		}
	}
	if !found {
		t.Fatal("favorites missing")
	}
	if normalizeLayout("{bad") == "" {
		t.Fatal("invalid layout not replaced")
	}
}

func TestProfilesIsolateProgress(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "m", Path: "/tmp/m.mp4", Title: "m", AddedAt: 1})
	if err := st.SetProgress("m", "alice", 1000); err != nil {
		t.Fatal(err)
	}
	if err := st.SetProgress("m", "bob", 2000); err != nil {
		t.Fatal(err)
	}
	if got := st.Progress("m", "alice"); got != 1000 {
		t.Fatalf("alice: %d", got)
	}
	if got := st.Progress("m", "bob"); got != 2000 {
		t.Fatalf("bob: %d", got)
	}
	ps, err := st.Profiles()
	if err != nil || len(ps) == 0 {
		t.Fatalf("profiles: %v %v", ps, err)
	}
}

func TestFavoritesPerProfile(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "m", Path: "/tmp/m.mp4", Title: "m", AddedAt: 1})
	on, err := st.ToggleFavorite("m", "alice")
	if err != nil || !on {
		t.Fatalf("toggle: %v %v", on, err)
	}
	if !st.IsFavorite("m", "alice") || st.IsFavorite("m", "bob") {
		t.Fatal("favorite leaked across profiles")
	}
	favs, _ := st.Favorites("alice", 10)
	if len(favs) != 1 {
		t.Fatalf("alice favs: %+v", favs)
	}
	if favs, _ := st.Favorites("bob", 10); len(favs) != 0 {
		t.Fatalf("bob favs: %+v", favs)
	}
	on, _ = st.ToggleFavorite("m", "alice")
	if on || st.IsFavorite("m", "alice") {
		t.Fatal("untoggle failed")
	}
}
