package main

import "testing"

func TestBackupRoundTrip(t *testing.T) {
	src := newTestStore(t)
	_ = src.Upsert(Media{ID: "a", Path: "/media/a.mp4", Title: "A", Size: 10, DurationMs: 5, AddedAt: 1})
	_ = src.AddTags("a", []string{"solo", "indoor"})
	_, _ = src.AddCollection("Favs", `{"q":"a"}`)
	p, _ := src.AddPlaylist("List")
	_, _ = src.AddToPlaylist(p.ID, []string{"a"})

	b, err := src.ExportAll()
	if err != nil || len(b.Media) != 1 || len(b.Playlists) != 1 {
		t.Fatalf("export: %+v %v", b, err)
	}
	dst := newTestStore(t)
	ms, ps, err := dst.ImportAll(b)
	if err != nil || ms != 1 || ps != 1 {
		t.Fatalf("import: %d %d %v", ms, ps, err)
	}
	// Re-import must not duplicate collections.
	_, _, _ = dst.ImportAll(b)
	cs, _ := dst.Collections()
	if len(cs) != 1 {
		t.Fatalf("collections duplicated: %d", len(cs))
	}
	m, _ := dst.Get("a")
	if len(m.Tags) != 2 {
		t.Fatalf("tags: %+v", m.Tags)
	}
	pls, _ := dst.Playlists()
	if len(pls) != 1 || pls[0].Count != 1 {
		t.Fatalf("playlists: %+v", pls)
	}
}
