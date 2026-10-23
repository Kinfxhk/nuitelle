package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportFlow(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "a.mp4")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	flow := filepath.Join(dir, "flow.sqlite3")
	db, err := sql.Open("sqlite", flow)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(strings.ReplaceAll(`
CREATE TABLE media(id TEXT,path TEXT,source TEXT,title TEXT,name TEXT,location TEXT,bytes INTEGER,added REAL,cover_ref TEXT,metadata TEXT,online INTEGER,seen REAL);
CREATE TABLE user_media(id TEXT,favorite INTEGER,position REAL,duration REAL,updated REAL);
CREATE TABLE playlists(id TEXT,name TEXT,created REAL,request_id TEXT,archived INTEGER);
CREATE TABLE playlist_items(playlist_id TEXT,media_id TEXT,ordinal INTEGER);
CREATE TABLE trash(media_id TEXT,tid TEXT,orig TEXT,online INTEGER,request_id TEXT,created REAL,memberships TEXT);
INSERT INTO media VALUES('f1','REPLACE_PATH','fc2','Flow A','a.mp4','ssd-hot',100,1000,'','{}',0,0);
INSERT INTO user_media VALUES('f1',1,12.5,60,0);
INSERT INTO playlists VALUES('p1','List',0,'',0);
INSERT INTO playlist_items VALUES('p1','f1',0);
INSERT INTO trash VALUES('f1','t','/media/a.mp4',0,'',0,'[]');
`, "REPLACE_PATH", mediaPath))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	st := newTestStore(t)
	_ = st.Upsert(Media{ID: mediaID(mediaPath), Path: mediaPath, Title: "a", AddedAt: 1})
	m, prog, fav, pl, items, tr, miss, err := ImportFlow(st, flow)
	if err != nil {
		t.Fatal(err)
	}
	if m != 1 || prog != 1 || fav != 1 || pl != 1 || items != 1 || tr != 1 || miss != 0 {
		t.Fatalf("counts: %d %d %d %d %d %d %d", m, prog, fav, pl, items, tr, miss)
	}
	got, _ := st.Get(mediaID(mediaPath))
	if got.Title != "Flow A" || !got.Trashed {
		t.Fatalf("media: %+v", got)
	}
	if st.Progress(got.ID, "default") != 12500 {
		t.Fatalf("progress: %d", st.Progress(got.ID, "default"))
	}
	if !st.IsFavorite(got.ID, "default") {
		t.Fatal("favorite missing")
	}
	ps, _ := st.Playlists()
	if len(ps) != 1 || ps[0].Count != 1 {
		t.Fatalf("playlist: %+v", ps)
	}
}
