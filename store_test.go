package main

import (
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
	if err := st.SetProgress("abc", 1234); err != nil {
		t.Fatal(err)
	}
	if p := st.Progress("abc"); p != 1234 {
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
