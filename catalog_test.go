package main

import "testing"

func TestCatalogPluginAndWishlist(t *testing.T) {
	p := Plugin{ID: "demo", Hooks: []string{"catalog.search"}, Script: `function catalogSearch(i){ return {items:[{id:"x1",title:"Metropolis",year:1927}]}; }`}
	items, err := p.Catalog("metro")
	if err != nil || len(items) != 1 || items[0].ID != "x1" {
		t.Fatalf("catalog: %+v %v", items, err)
	}
	st := newTestStore(t)
	_ = st.Upsert(Media{ID: "m", Path: "/tmp/m.mp4", Title: "Metropolis", AddedAt: 1})
	items[0].Provider = "demo"
	items[0].InLibrary = st.InLibrary(items[0].Title)
	if !items[0].InLibrary {
		t.Fatal("in-library match failed")
	}
	if err := st.AddWishlist(items[0]); err != nil {
		t.Fatal(err)
	}
	wl, _ := st.Wishlist()
	if len(wl) != 1 || wl[0].Title != "Metropolis" {
		t.Fatalf("wishlist: %+v", wl)
	}
	if err := st.RemoveWishlist("demo", "x1"); err != nil {
		t.Fatal(err)
	}
	wl, _ = st.Wishlist()
	if len(wl) != 0 {
		t.Fatalf("remove: %+v", wl)
	}
}
