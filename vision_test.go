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
