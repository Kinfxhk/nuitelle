package main

import "testing"

func TestPlaylists(t *testing.T) {
	st := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		_ = st.Upsert(Media{ID: id, Path: "/tmp/" + id + ".mp4", Title: id, AddedAt: 1})
	}
	p, err := st.AddPlaylist("List")
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.AddToPlaylist(p.ID, []string{"a", "b", "c"})
	if err != nil || n != 3 {
		t.Fatalf("add: %d %v", n, err)
	}
	items, err := st.PlaylistItems(p.ID)
	if err != nil || len(items) != 3 || items[0].ID != "a" {
		t.Fatalf("items: %+v %v", items, err)
	}
	if err := st.ReorderPlaylist(p.ID, []string{"c", "b", "a"}); err != nil {
		t.Fatal(err)
	}
	items, _ = st.PlaylistItems(p.ID)
	if items[0].ID != "c" {
		t.Fatalf("reorder failed: %+v", items)
	}
	if err := st.RemoveFromPlaylist(p.ID, "b"); err != nil {
		t.Fatal(err)
	}
	items, _ = st.PlaylistItems(p.ID)
	if len(items) != 2 {
		t.Fatalf("remove failed: %+v", items)
	}
	if err := st.DeletePlaylist(p.ID); err != nil {
		t.Fatal(err)
	}
	ps, _ := st.Playlists()
	if len(ps) != 0 {
		t.Fatalf("delete failed: %+v", ps)
	}
}
