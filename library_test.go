package main

import (
	"path/filepath"
	"testing"
)

func TestCollections(t *testing.T) {
	st := newTestStore(t)
	c, err := st.AddCollection("Favourites", `{"q":"x","tag":"y"}`)
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.Collections()
	if err != nil || len(list) != 1 || list[0].Name != "Favourites" {
		t.Fatalf("collections: %v %v", list, err)
	}
	if err := st.DeleteCollection(c.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.Collections()
	if len(list) != 0 {
		t.Fatalf("delete failed: %v", list)
	}
}

func TestTagRenameMergeAlias(t *testing.T) {
	st := newTestStore(t)
	for _, id := range []string{"m1", "m2", "m3"} {
		if err := st.Upsert(Media{ID: id, Path: "/tmp/" + id + ".mp4", Title: id, AddedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.AddTags("m1", []string{"cos"})
	_ = st.AddTags("m2", []string{"cosplay"})
	if err := st.AddTagAlias("cos", "cosplay"); err != nil {
		t.Fatal(err)
	}
	// Adding via an alias must land on the canonical tag.
	if err := st.AddTags("m3", []string{"cos"}); err != nil {
		t.Fatal(err)
	}
	m3, _ := st.Get("m3")
	if len(m3.Tags) != 1 || m3.Tags[0] != "cosplay" {
		t.Fatalf("alias not resolved: %v", m3.Tags)
	}
	// Rename into an existing tag merges members and drops the source.
	if err := st.RenameTag("cos", "cosplay"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.tagID("cos"); err == nil {
		t.Fatal("source tag still exists")
	}
	m2, _ := st.Get("m2")
	if len(m2.Tags) != 1 || m2.Tags[0] != "cosplay" {
		t.Fatalf("merge lost membership: %v", m2.Tags)
	}
}

func TestRemapAndHealth(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "a", Path: "/old/root/a.mp4", Title: "a", Size: 10, DurationMs: 5, AddedAt: 1})
	_ = st.Upsert(Media{ID: "b", Path: "/old/root/b.mp4", Title: "b", Size: 10, DurationMs: 5, AddedAt: 1})
	_ = st.Upsert(Media{ID: "c", Path: "/old/root/c.mp4", Title: "c", Size: 0, AddedAt: 1})
	n, err := st.RemapPaths("/old/root", "/new/root")
	if err != nil || n != 3 {
		t.Fatalf("remap: %d %v", n, err)
	}
	m, _ := st.Get("a")
	if m.Path != "/new/root/a.mp4" {
		t.Fatalf("path: %s", m.Path)
	}
	h := st.LibraryHealth()
	if h["files"].(int64) != 3 || h["zero_byte"].(int64) != 1 {
		t.Fatalf("health: %+v", h)
	}
	groups, err := st.Duplicates(10)
	if err != nil || len(groups) != 1 || groups[0].Count != 2 {
		t.Fatalf("duplicates: %+v %v", groups, err)
	}
}

func TestOpenStoreCreatesLibraryTables(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Collections(); err != nil {
		t.Fatalf("collections table missing: %v", err)
	}
}

func TestHomeShelves(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "a", Path: "/tmp/a.mp4", Title: "a", Size: 10, DurationMs: 10000, AddedAt: 2})
	_ = st.Upsert(Media{ID: "b", Path: "/tmp/b.mp4", Title: "b", Size: 11, DurationMs: 10000, AddedAt: 1})
	_ = st.SetProgress("a", "default", 6000)
	_, _ = st.ToggleFavorite("b", "default")
	h, err := st.Home("default")
	if err != nil {
		t.Fatal(err)
	}
	if cont := h["continue"].([]Media); len(cont) != 1 || cont[0].ID != "a" || cont[0].PositionMs != 6000 {
		t.Fatalf("continue: %+v", cont)
	}
	if favs := h["favorites"].([]Media); len(favs) != 1 || favs[0].ID != "b" {
		t.Fatalf("favorites: %+v", favs)
	}
	if recent := h["recent"].([]Media); len(recent) != 2 {
		t.Fatalf("recent: %+v", recent)
	}
}

func TestRemoveMediaCleansChildren(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "x", Path: "/tmp/x.mp4", Title: "x", AddedAt: 1})
	_ = st.AddTags("x", []string{"keep"})
	p, _ := st.AddPlaylist("L")
	_, _ = st.AddToPlaylist(p.ID, []string{"x"})
	if err := st.RemoveMedia("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("x"); err == nil {
		t.Fatal("media still present")
	}
	pls, _ := st.Playlists()
	if pls[0].Count != 0 {
		t.Fatalf("playlist item left: %+v", pls)
	}
}
