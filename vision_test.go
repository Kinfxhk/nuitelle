package main

import "testing"

func TestSuggestionFlow(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "m", Path: "/tmp/m.mp4", Title: "m", AddedAt: 1})
	if err := st.AddSuggestions("m", []Suggestion{{Name: "indoor", Confidence: 0.9}, {Name: "solo", Confidence: 0.8}}, "test"); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListSuggestions("m")
	if err != nil || len(items) != 2 || items[0].Name != "indoor" {
		t.Fatalf("list: %+v %v", items, err)
	}
	if err := st.AcceptSuggestions("m", []string{"indoor"}); err != nil {
		t.Fatal(err)
	}
	m, _ := st.Get("m")
	if len(m.Tags) != 1 || m.Tags[0] != "indoor" {
		t.Fatalf("accept: %+v", m.Tags)
	}
	items, _ = st.ListSuggestions("m")
	if len(items) != 1 || items[0].Name != "solo" {
		t.Fatalf("after accept: %+v", items)
	}
	if err := st.RejectSuggestions("m", []string{"solo"}); err != nil {
		t.Fatal(err)
	}
	items, _ = st.ListSuggestions("m")
	if len(items) != 0 {
		t.Fatalf("reject: %+v", items)
	}
}

func TestNormalizeTag(t *testing.T) {
	for in, want := range map[string]string{"Close-Up": "close-up", "two people": "two-people", "  red!": "red", "A_B": "a-b"} {
		if got := normalizeTag(in); got != want {
			t.Fatalf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestPendingAndFolder(t *testing.T) {
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "a", Path: "/media/x/a.mp4", Title: "a", AddedAt: 1})
	_ = st.Upsert(Media{ID: "b", Path: "/media/x/b.mp4", Title: "b", AddedAt: 1})
	_ = st.Upsert(Media{ID: "c", Path: "/other/c.mp4", Title: "c", AddedAt: 1})
	_ = st.AddSuggestions("a", []Suggestion{{Name: "indoor", Confidence: 0.9}}, "m")
	_ = st.AddSuggestions("b", []Suggestion{{Name: "solo", Confidence: 0.8}}, "m")
	p, err := st.PendingSuggestions(10)
	if err != nil || len(p) != 2 || p[0].Title == "" {
		t.Fatalf("pending: %+v %v", p, err)
	}
	ids, err := st.MediaIDsUnder("/media/x")
	if err != nil || len(ids) != 2 {
		t.Fatalf("folder: %+v %v", ids, err)
	}
}
