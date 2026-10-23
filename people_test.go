package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestPeopleServer(t *testing.T) (*Store, *Config, *httptest.Server) {
	t.Helper()
	st := newTestStore(t)
	cfg := defaultConfig()
	cfg.PeopleDir = filepath.Join(t.TempDir(), "people")
	ts := httptest.NewServer(buildMux(cfg, st))
	t.Cleanup(ts.Close)
	return st, cfg, ts
}

func doJSON(t *testing.T, method, url string, body any, out any) int {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	} else {
		io.Copy(io.Discard, res.Body)
	}
	return res.StatusCode
}

func seedMedia(t *testing.T, st *Store, names ...string) []Media {
	t.Helper()
	var out []Media
	for i, n := range names {
		m := Media{ID: mediaID("/media/" + n), Path: "/media/" + n + ".mp4", Title: n, AddedAt: int64(100 + i)}
		if err := st.Upsert(m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestPeopleCrudAndDedup(t *testing.T) {
	st := newTestStore(t)
	p, err := st.CreatePerson(PersonInput{Name: "  Hana   Kim ", Aliases: "hana, Hana", HeightCM: 168, Note: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Hana Kim" {
		t.Fatalf("name not normalised: %q", p.Name)
	}
	if p.Aliases != "hana" {
		t.Fatalf("aliases not normalised: %q", p.Aliases)
	}
	if _, err := st.CreatePerson(PersonInput{Name: "hana kim"}); err != ErrPersonExists {
		t.Fatalf("want ErrPersonExists, got %v", err)
	}
	// The unique index itself rejects a case variant, not just the pre-check.
	if _, err := st.db.Exec(`INSERT INTO people(id,name,created_at) VALUES('dup','HANA KIM',1)`); err == nil {
		t.Fatal("unique index on lower(name) did not reject a duplicate")
	}
	// Alias resolves to the same person.
	if got, ok := st.PersonByName("HANA"); !ok || got.ID != p.ID {
		t.Fatalf("alias lookup failed: %+v %v", got, ok)
	}
	// Empty name is rejected.
	if _, err := st.CreatePerson(PersonInput{Name: "   "}); err == nil {
		t.Fatal("empty name accepted")
	}

	newNote := "updated"
	got, err := st.UpdatePerson(p.ID, PersonPatch{Note: &newNote})
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != newNote || got.HeightCM != 168 {
		t.Fatalf("patch not applied: %+v", got)
	}
	if _, err := st.UpdatePerson(p.ID, PersonPatch{Name: ptrStr("")}); err == nil {
		t.Fatal("empty name accepted on patch")
	}

	other, err := st.CreatePerson(PersonInput{Name: "Mina"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdatePerson(other.ID, PersonPatch{Name: ptrStr("hana kim")}); err != ErrPersonExists {
		t.Fatalf("duplicate rename: %v", err)
	}
	if err := st.DeletePerson(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetPerson(p.ID); err == nil {
		t.Fatal("person still present after delete")
	}
}

func ptrStr(s string) *string { return &s }
func ptrInt(i int) *int       { return &i }

func TestPeopleListSorts(t *testing.T) {
	st := newTestStore(t)
	media := seedMedia(t, st, "a", "b", "c")
	bea, _ := st.CreatePerson(PersonInput{Name: "Bea"})
	abe, _ := st.CreatePerson(PersonInput{Name: "abe"})
	cy, _ := st.CreatePerson(PersonInput{Name: "Cy", Aliases: "cyanne"})
	// Bea has 2 items, abe 1, Cy 0.
	st.LinkPerson(media[0].ID, bea.ID, "")
	st.LinkPerson(media[1].ID, bea.ID, "")
	st.LinkPerson(media[0].ID, abe.ID, "")

	items, total, err := st.ListPeople("", "name", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("want 3 people, got %d/%d", len(items), total)
	}
	if items[0].Name != "abe" || items[1].Name != "Bea" || items[2].Name != "Cy" {
		t.Fatalf("name sort wrong: %v %v %v", items[0].Name, items[1].Name, items[2].Name)
	}
	items, _, _ = st.ListPeople("", "count", 10, 0)
	if items[0].ID != bea.ID || items[0].Count != 2 {
		t.Fatalf("count sort wrong: %+v", items[0])
	}
	items, _, _ = st.ListPeople("", "recent", 10, 0)
	if items[0].ID != cy.ID {
		t.Fatalf("recent sort wrong: %+v", items[0])
	}
	// Search hits name and alias.
	if items, _, _ = st.ListPeople("cya", "", 10, 0); len(items) != 1 || items[0].ID != cy.ID {
		t.Fatalf("alias search failed: %+v", items)
	}
	// Wildcards are escaped.
	if _, total, _ = st.ListPeople("%", "", 10, 0); total != 0 {
		t.Fatalf("wildcard not escaped: %d", total)
	}
}

func TestPeopleLinksAndMediaFilter(t *testing.T) {
	st := newTestStore(t)
	media := seedMedia(t, st, "one", "two", "three")
	a, _ := st.CreatePerson(PersonInput{Name: "A", Aliases: "ay"})
	b, _ := st.CreatePerson(PersonInput{Name: "B"})
	if err := st.LinkPerson(media[0].ID, a.ID, "actor"); err != nil {
		t.Fatal(err)
	}
	st.LinkPerson(media[1].ID, a.ID, "actor")
	st.LinkPerson(media[2].ID, b.ID, "actor")

	got, total, err := st.ListFiltered(MediaFilter{Person: a.ID, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("single person filter: %d/%d", len(got), total)
	}
	got, total, _ = st.ListFiltered(MediaFilter{Person: a.ID + "," + b.ID, Limit: 50})
	if total != 3 {
		t.Fatalf("OR filter: want 3, got %d", total)
	}
	if got, total, _ = st.ListFiltered(MediaFilter{Person: "nobody", Limit: 50}); total != 0 || len(got) != 0 {
		t.Fatalf("unknown person should match nothing: %d", total)
	}
	// Combined with a tag filter.
	if err := st.AddTags(media[0].ID, []string{"solo"}); err != nil {
		t.Fatal(err)
	}
	if _, total, _ = st.ListFiltered(MediaFilter{Person: a.ID, Tag: "solo", Limit: 50}); total != 1 {
		t.Fatalf("person+tag filter: want 1, got %d", total)
	}

	people, err := st.PeopleForMedia(media[0].ID)
	if err != nil || len(people) != 1 || people[0].ID != a.ID {
		t.Fatalf("people for media: %+v %v", people, err)
	}
	if err := st.UnlinkPerson(media[0].ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, total, _ = st.ListFiltered(MediaFilter{Person: a.ID, Limit: 50}); total != 1 {
		t.Fatalf("unlink: want 1, got %d", total)
	}

	// AssignPeople resolves aliases, creates unknown names and removes by name.
	if ppl, err := st.AssignPeople(media[1].ID, []string{"AY"}, nil); err != nil || len(ppl) != 1 || ppl[0].ID != a.ID {
		t.Fatalf("alias assign: %+v %v", ppl, err)
	}
	if err := st.UnlinkPerson(media[1].ID, a.ID); err != nil {
		t.Fatal(err)
	}
	st.LinkPerson(media[1].ID, a.ID, "")
	ppl, err := st.AssignPeople(media[0].ID, []string{"New Face", "A"}, []string{"New Face"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ppl) != 1 || ppl[0].ID != a.ID {
		t.Fatalf("assign by name: %+v", ppl)
	}
	// Create-on-the-fly keeps the person row; only the link is removed.
	if _, ok := st.PersonByName("new face"); !ok {
		t.Fatal("create-on-the-fly lost the person row")
	}
	if ppl, _ := st.PeopleForMedia(media[0].ID); len(ppl) != 1 {
		t.Fatalf("remove did not unlink: %+v", ppl)
	}
}

func TestDeletePersonKeepsMediaAndTags(t *testing.T) {
	st := newTestStore(t)
	media := seedMedia(t, st, "keep")
	if err := st.AddTags(media[0].ID, []string{"favourite-genre"}); err != nil {
		t.Fatal(err)
	}
	p, _ := st.CreatePerson(PersonInput{Name: "Temp"})
	st.LinkPerson(media[0].ID, p.ID, "")
	if err := st.DeletePerson(p.ID); err != nil {
		t.Fatal(err)
	}
	m, err := st.Get(media[0].ID)
	if err != nil {
		t.Fatalf("media disappeared: %v", err)
	}
	if len(m.Tags) != 1 || m.Tags[0] != "favourite-genre" {
		t.Fatalf("tags changed: %v", m.Tags)
	}
	people, _ := st.PeopleForMedia(m.ID)
	if len(people) != 0 {
		t.Fatalf("links survived: %+v", people)
	}
}

// testPNG builds a solid PNG of the given size.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / maxInt(1, w)), G: uint8(y * 255 / maxInt(1, h)), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPersonImagePipeline(t *testing.T) {
	dir := t.TempDir()
	pngBytes := testPNG(t, 2000, 1000)
	full, thumb, err := savePersonImageFiles(dir, "p1", pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(full, ".jpg") || !strings.HasSuffix(thumb, "_t.jpg") {
		t.Fatalf("unexpected names: %q %q", full, thumb)
	}
	fi, err := os.Stat(filepath.Join(dir, full))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 || fi.Size() > maxPersonUploadBytes {
		t.Fatalf("bad full size: %d", fi.Size())
	}
	f, _ := os.Open(filepath.Join(dir, full))
	fimg, err := jpeg.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if b := fimg.Bounds(); b.Dx() != 1600 || b.Dy() != 800 {
		t.Fatalf("full should be 1600x800, got %dx%d", b.Dx(), b.Dy())
	}
	tf, _ := os.Open(filepath.Join(dir, thumb))
	timg, err := jpeg.Decode(tf)
	tf.Close()
	if err != nil {
		t.Fatal(err)
	}
	if b := timg.Bounds(); b.Dx() != 320 || b.Dy() != 160 {
		t.Fatalf("thumb should be 320x160, got %dx%d", b.Dx(), b.Dy())
	}

	// A small image is never upscaled.
	small := testPNG(t, 200, 100)
	full2, thumb2, err := savePersonImageFiles(dir, "p2", small)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][2]int{full2: {200, 100}, thumb2: {200, 100}} {
		fh, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		im, err := jpeg.Decode(fh)
		fh.Close()
		if err != nil {
			t.Fatal(err)
		}
		if b := im.Bounds(); b.Dx() != want[0] || b.Dy() != want[1] {
			t.Fatalf("%s: got %dx%d want %dx%d", name, b.Dx(), b.Dy(), want[0], want[1])
		}
	}
}

func TestPersonImageValidation(t *testing.T) {
	if _, _, err := processPersonImage([]byte("not an image at all")); err == nil {
		t.Fatal("garbage accepted")
	}
	if _, _, err := processPersonImage(nil); err == nil {
		t.Fatal("empty accepted")
	}
	dir := t.TempDir()
	if _, err := readPersonImageFile("relative/path.jpg"); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, err := readPersonImageFile(filepath.Join(dir, "missing.jpg")); err == nil {
		t.Fatal("missing file accepted")
	}
	big := filepath.Join(dir, "big.jpg")
	if err := os.WriteFile(big, []byte{0xFF, 0xD8, 0xFF}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, maxPersonUploadBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readPersonImageFile(big); err == nil {
		t.Fatal("oversized file accepted")
	}
	// A stored name with a path component must never resolve.
	if _, ok := personImageFile(dir, "../../etc/passwd"); ok {
		t.Fatal("traversal accepted")
	}
	if _, ok := personImageFile(dir, "sub/x.jpg"); ok {
		t.Fatal("nested name accepted")
	}

	// EXIF orientation is read from a minimal TIFF block.
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	if o := tiffOrientation(tiff); o != 6 {
		t.Fatalf("orientation: %d", o)
	}
	// Orientation 6 swaps the dimensions.
	rot := orientImage(image.NewRGBA(image.Rect(0, 0, 30, 10)), 6)
	if b := rot.Bounds(); b.Dx() != 10 || b.Dy() != 30 {
		t.Fatalf("orient6 bounds: %dx%d", b.Dx(), b.Dy())
	}
}

func TestPeopleAPI(t *testing.T) {
	st, _, ts := newTestPeopleServer(t)
	media := seedMedia(t, st, "first", "second")

	var created struct {
		Item Person `json:"item"`
	}
	code := doJSON(t, "POST", ts.URL+"/api/people", PersonInput{Name: "Yuna", Aliases: "yuna, yu", HeightCM: 165}, &created)
	if code != 201 || created.Item.ID == "" {
		t.Fatalf("create: %d %+v", code, created)
	}
	id := created.Item.ID

	// Duplicate create is a conflict that still reports the existing row.
	var dup map[string]any
	if code = doJSON(t, "POST", ts.URL+"/api/people", PersonInput{Name: "yuna"}, &dup); code != 409 {
		t.Fatalf("duplicate: want 409, got %d", code)
	}

	var list struct {
		Items []Person `json:"items"`
		Total int64    `json:"total"`
	}
	if code = doJSON(t, "GET", ts.URL+"/api/people?q=yu&sort=name", nil, &list); code != 200 || len(list.Items) != 1 {
		t.Fatalf("list: %d %+v", code, list)
	}

	var patched struct {
		Item Person `json:"item"`
	}
	if code = doJSON(t, "PATCH", ts.URL+"/api/people/"+id, map[string]any{"note": "hello"}, &patched); code != 200 || patched.Item.Note != "hello" {
		t.Fatalf("patch: %d %+v", code, patched)
	}

	// Link by name through the media endpoint, then filter the library.
	var linked struct {
		People []Person `json:"people"`
	}
	if code = doJSON(t, "POST", ts.URL+"/api/media/"+media[0].ID+"/people", map[string]any{"add": []string{"Yuna"}}, &linked); code != 200 || len(linked.People) != 1 {
		t.Fatalf("link: %d %+v", code, linked)
	}
	var filtered struct {
		Items []Media `json:"items"`
		Total int64   `json:"total"`
	}
	if code = doJSON(t, "GET", ts.URL+"/api/media?person="+id, nil, &filtered); code != 200 || filtered.Total != 1 || filtered.Items[0].ID != media[0].ID {
		t.Fatalf("person filter: %d %+v", code, filtered)
	}

	// Upload a portrait as multipart.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "portrait.png")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(testPNG(t, 900, 1200))
	mw.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/people/"+id+"/image", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		Item Person `json:"item"`
	}
	err = json.NewDecoder(res.Body).Decode(&uploaded)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("upload: %d %v", res.StatusCode, err)
	}
	if uploaded.Item.Image == "" || uploaded.Item.Thumb == "" {
		t.Fatalf("image not recorded: %+v", uploaded.Item)
	}
	if filepath.Base(uploaded.Item.Image) != uploaded.Item.Image {
		t.Fatalf("image field is a path: %q", uploaded.Item.Image)
	}

	imgRes, err := http.Get(ts.URL + "/api/people/" + id + "/image?size=thumb")
	if err != nil {
		t.Fatal(err)
	}
	ct := imgRes.Header.Get("Content-Type")
	thumbBytes, _ := io.ReadAll(imgRes.Body)
	imgRes.Body.Close()
	if imgRes.StatusCode != 200 || ct != "image/jpeg" {
		t.Fatalf("thumb: %d %q", imgRes.StatusCode, ct)
	}
	im, err := jpeg.Decode(bytes.NewReader(thumbBytes))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != 320 {
		t.Fatalf("thumb width: %d", im.Bounds().Dx())
	}

	// Local path import works too.
	src := filepath.Join(t.TempDir(), "local.png")
	if err := os.WriteFile(src, testPNG(t, 400, 600), 0o600); err != nil {
		t.Fatal(err)
	}
	var imported struct {
		Item Person `json:"item"`
	}
	if code = doJSON(t, "POST", ts.URL+"/api/people/"+id+"/image", map[string]any{"path": src}, &imported); code != 200 {
		t.Fatalf("path import: %d", code)
	}
	if imported.Item.Image == uploaded.Item.Image {
		t.Fatal("re-upload did not replace the image")
	}
	if code = doJSON(t, "POST", ts.URL+"/api/people/"+id+"/image", map[string]any{"path": "relative.jpg"}, nil); code != 400 {
		t.Fatalf("bad path: want 400, got %d", code)
	}

	// Deleting the person keeps the media.
	if code = doJSON(t, "DELETE", ts.URL+"/api/people/"+id, nil, nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	var all struct {
		Total int64 `json:"total"`
	}
	doJSON(t, "GET", ts.URL+"/api/media", nil, &all)
	if all.Total != 2 {
		t.Fatalf("media lost on delete: %d", all.Total)
	}
	if code = doJSON(t, "GET", ts.URL+"/api/people/"+id, nil, nil); code != 404 {
		t.Fatalf("deleted person still served: %d", code)
	}
}
