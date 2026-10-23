package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Media is one indexed file. Path is never sent to the browser.
type Media struct {
	ID         string   `json:"id"`
	Path       string   `json:"-"`
	Title      string   `json:"title"`
	Size       int64    `json:"size"`
	MtimeNS    int64    `json:"-"`
	DurationMs int64    `json:"duration_ms"`
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Vcodec     string   `json:"vcodec"`
	Acodec     string   `json:"acodec"`
	AddedAt    int64    `json:"added_at"`
	Trashed    bool     `json:"trashed"`
	Tags       []string `json:"tags"`
	PositionMs int64    `json:"position_ms,omitempty"`
	Favorite   bool     `json:"favorite"`
}

type Store struct {
	db  *sql.DB
	mu  sync.Mutex // single writer, per SQLite WAL
	dir string
}

const schema = `
CREATE TABLE IF NOT EXISTS media(
  id TEXT PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  mtime_ns INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  vcodec TEXT NOT NULL DEFAULT '',
  acodec TEXT NOT NULL DEFAULT '',
  added_at INTEGER NOT NULL,
  trashed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS media_added ON media(added_at DESC);
CREATE TABLE IF NOT EXISTS tags(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS media_tags(
  media_id TEXT NOT NULL,
  tag_id INTEGER NOT NULL,
  PRIMARY KEY(media_id, tag_id)
);
CREATE TABLE IF NOT EXISTS progress(
  profile_id TEXT NOT NULL DEFAULT 'default',
  media_id TEXT NOT NULL,
  position_ms INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(profile_id, media_id)
);
CREATE TABLE IF NOT EXISTS profiles(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS kv(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS trash(
  media_id TEXT PRIMARY KEY,
  orig_path TEXT NOT NULL,
  trash_path TEXT NOT NULL,
  at INTEGER NOT NULL
);
`

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(subsSchema); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureCatalogSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureFeedsSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureLibrarySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(suggestionSchema); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(peopleSchema); err != nil {
		db.Close()
		return nil, err
	}
	db.Exec(`ALTER TABLE media ADD COLUMN phash TEXT NOT NULL DEFAULT ''`)
	// Migrate single-user progress to per-profile rows.
	var hasProfile int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('progress') WHERE name='profile_id'`).Scan(&hasProfile)
	if hasProfile == 0 {
		_, _ = db.Exec(`ALTER TABLE progress RENAME TO progress_old`)
		_, _ = db.Exec(`CREATE TABLE progress(profile_id TEXT NOT NULL DEFAULT 'default', media_id TEXT NOT NULL, position_ms INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, PRIMARY KEY(profile_id, media_id))`)
		_, _ = db.Exec(`INSERT OR IGNORE INTO progress(profile_id,media_id,position_ms,updated_at) SELECT 'default',media_id,position_ms,updated_at FROM progress_old`)
		_, _ = db.Exec(`DROP TABLE progress_old`)
	}
	db.Exec(`ALTER TABLE playlists ADD COLUMN archived INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`INSERT OR IGNORE INTO profiles(id,name,created_at) VALUES('default','Default',strftime('%s','now'))`)
	db.Exec(`INSERT OR IGNORE INTO profile_favorites(profile_id,media_id,created_at)
 SELECT 'default', mt.media_id, strftime('%s','now') FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE t.name='favorite'`)
	if _, err := db.Exec(`UPDATE jobs SET state='interrupted' WHERE state='running'`); err != nil {
		db.Close()
		return nil, err
	}
	st := &Store{db: db, dir: filepath.Dir(path)}
	var mediaCount, ftsCount int64
	_ = db.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&mediaCount)
	_ = db.QueryRow(`SELECT COUNT(*) FROM media_fts`).Scan(&ftsCount)
	if mediaCount > 0 && ftsCount == 0 {
		_ = st.RebuildFTS()
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Upsert(m Media) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO media(id,path,title,size,mtime_ns,duration_ms,width,height,vcodec,acodec,added_at,trashed)
VALUES(?,?,?,?,?,?,?,?,?,?,?,0)
ON CONFLICT(path) DO UPDATE SET size=excluded.size,mtime_ns=excluded.mtime_ns,
 duration_ms=excluded.duration_ms,width=excluded.width,height=excluded.height,
 vcodec=excluded.vcodec,acodec=excluded.acodec`,
		m.ID, m.Path, m.Title, m.Size, m.MtimeNS, m.DurationMs, m.Width, m.Height, m.Vcodec, m.Acodec, m.AddedAt)
	if err == nil {
		s.refreshFTSLocked(m.ID)
	}
	return err
}

func (s *Store) refreshFTSLocked(id string) {
	var title, path string
	if err := s.db.QueryRow(`SELECT title,path FROM media WHERE id=?`, id).Scan(&title, &path); err != nil {
		return
	}
	var tags string
	_ = s.db.QueryRow(`SELECT COALESCE(GROUP_CONCAT(t.name,' '),'') FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=?`, id).Scan(&tags)
	s.db.Exec(`DELETE FROM media_fts WHERE id=?`, id)
	s.db.Exec(`INSERT INTO media_fts(id,title,body) VALUES(?,?,?)`, id, title, strings.ToLower(title+" "+path+" "+tags))
}

func (s *Store) StartJob(id, kind, mediaID string, total int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO jobs(id,kind,media_id,state,progress,error,updated_at) VALUES(?,?,?,'running',0,'',?)
 ON CONFLICT(id) DO UPDATE SET state='running', progress=0, error='', updated_at=excluded.updated_at`,
		id, kind, mediaID, time.Now().Unix())
	return err
}

func (s *Store) UpdateJob(id string, progress float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE jobs SET progress=?, updated_at=? WHERE id=?`, progress, time.Now().Unix(), id)
}

func (s *Store) FinishJob(id, state, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE jobs SET state=?, error=?, updated_at=? WHERE id=?`, state, errMsg, time.Now().Unix(), id)
}

func (s *Store) Jobs() ([]map[string]any, error) {
	rows, err := s.db.Query(`SELECT id,kind,media_id,state,progress,error,updated_at FROM jobs ORDER BY updated_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, kind, mid, state, e string
		var prog float64
		var at int64
		if rows.Scan(&id, &kind, &mid, &state, &prog, &e, &at) == nil {
			out = append(out, map[string]any{"id": id, "kind": kind, "media_id": mid, "state": state, "progress": prog, "error": e, "updated_at": at})
		}
	}
	return out, rows.Err()
}

// RebuildFTS recreates the search index; call after a scan.
func (s *Store) RebuildFTS() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM media_fts`); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id,title,path FROM media WHERE trashed=0`)
	if err != nil {
		return err
	}
	type row struct{ id, title, path string }
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.title, &r.path) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	for _, r := range all {
		var tags string
		_ = s.db.QueryRow(`SELECT COALESCE(GROUP_CONCAT(t.name,' '),'') FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=?`, r.id).Scan(&tags)
		s.db.Exec(`INSERT INTO media_fts(id,title,body) VALUES(?,?,?)`, r.id, r.title, strings.ToLower(r.title+" "+r.path+" "+tags))
	}
	return nil
}

func (s *Store) UpdateTitle(id, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE media SET title=? WHERE id=?`, title, id)
	if err == nil {
		s.refreshFTSLocked(id)
	}
	return err
}

func (s *Store) SetPHash(id, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE media SET phash=? WHERE id=?`, hash, id)
	return err
}

func (s *Store) Existing(path string) (Media, bool) {
	var m Media
	err := s.db.QueryRow(`SELECT id,path,title,size,mtime_ns,duration_ms,width,height,vcodec,acodec,added_at,trashed FROM media WHERE path=?`, path).
		Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt, &m.Trashed)
	if err != nil {
		return m, false
	}
	return m, true
}

func (s *Store) Get(id string) (Media, error) {
	var m Media
	err := s.db.QueryRow(`SELECT id,path,title,size,mtime_ns,duration_ms,width,height,vcodec,acodec,added_at,trashed FROM media WHERE id=?`, id).
		Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt, &m.Trashed)
	if err != nil {
		return m, err
	}
	m.Tags = s.tagsOf(id)
	return m, nil
}

func (s *Store) tagsOf(id string) []string {
	rows, err := s.db.Query(`SELECT t.name FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=? ORDER BY t.name`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func escLike(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return q
}

// MediaFilter is the library query: text, tag, person and ordering.
type MediaFilter struct {
	Q      string
	Tag    string
	Person string // one id, or "id,id" for OR
	Sort   string
	Limit  int
	Offset int
}

// List returns media with optional text and tag filters.
func (s *Store) List(q, tag, sort string, limit, offset int) ([]Media, int64, error) {
	return s.ListFiltered(MediaFilter{Q: q, Tag: tag, Sort: sort, Limit: limit, Offset: offset})
}

// ListFiltered returns media matching a filter plus the total match count.
func (s *Store) ListFiltered(f MediaFilter) ([]Media, int64, error) {
	q, tag, sort := f.Q, f.Tag, f.Sort
	limit, offset := f.Limit, f.Offset
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"m.trashed=0"}
	args := []any{}
	if q != "" {
		if len([]rune(q)) >= 3 {
			where = append(where, `m.id IN (SELECT id FROM media_fts WHERE media_fts MATCH ?)`)
			args = append(args, `"`+strings.ReplaceAll(q, `"`, `""`)+`"`)
		} else {
			where = append(where, `m.title LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escLike(q)+"%")
		}
	}
	if tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=m.id AND t.name=?)`)
		args = append(args, tag)
	}
	if ids := personIDs(f.Person); len(ids) > 0 {
		ph := make([]string, len(ids))
		for i, id := range ids {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, `m.id IN (SELECT media_id FROM media_people WHERE person_id IN (`+strings.Join(ph, ",")+`))`)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM media m WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "m.added_at DESC, m.title"
	switch sort {
	case "name":
		order = "m.title COLLATE NOCASE ASC"
	case "size":
		order = "m.size DESC"
	case "duration":
		order = "m.duration_ms DESC"
	}
	args = append(args, limit, offset)
	rows, err := s.db.Query(`SELECT m.id,m.path,m.title,m.size,m.mtime_ns,m.duration_ms,m.width,m.height,m.vcodec,m.acodec,m.added_at,m.trashed,
 COALESCE((SELECT GROUP_CONCAT(t.name, char(31)) FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=m.id),'')
 FROM media m WHERE `+clause+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		var tagstr string
		if err := rows.Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt, &m.Trashed, &tagstr); err != nil {
			return nil, 0, err
		}
		if tagstr != "" {
			m.Tags = strings.Split(tagstr, string(rune(31)))
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

func (s *Store) AddTags(id string, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		n = s.ResolveTag(n)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags(name) VALUES(?)`, n); err != nil {
			return err
		}
		var tid int64
		if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, n).Scan(&tid); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO media_tags(media_id,tag_id) VALUES(?,?)`, id, tid); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.refreshFTSLocked(id)
	return nil
}

func (s *Store) RemoveTag(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM media_tags WHERE media_id=? AND tag_id=(SELECT id FROM tags WHERE name=?)`, id, name)
	if err == nil {
		s.refreshFTSLocked(id)
	}
	return err
}

func (s *Store) SetTrashed(id string, v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	if v {
		n = 1
	}
	_, err := s.db.Exec(`UPDATE media SET trashed=? WHERE id=?`, n, id)
	return err
}

// RecordTrash stores where a file was moved so restore can put it back.
func (s *Store) RecordTrash(id, orig, moved string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO trash(media_id,orig_path,trash_path,at) VALUES(?,?,?,?)
 ON CONFLICT(media_id) DO UPDATE SET orig_path=excluded.orig_path, trash_path=excluded.trash_path, at=excluded.at`,
		id, orig, moved, time.Now().Unix())
	return err
}

func (s *Store) TrashRecord(id string) (orig, moved string, ok bool) {
	err := s.db.QueryRow(`SELECT orig_path,trash_path FROM trash WHERE media_id=?`, id).Scan(&orig, &moved)
	return orig, moved, err == nil
}

func (s *Store) ClearTrashRecord(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM trash WHERE media_id=?`, id)
	return err
}

func (s *Store) SetProgress(id, profile string, ms int64) error {
	if profile == "" {
		profile = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO progress(profile_id,media_id,position_ms,updated_at) VALUES(?,?,?,?)
 ON CONFLICT(profile_id,media_id) DO UPDATE SET position_ms=excluded.position_ms, updated_at=excluded.updated_at`, profile, id, ms, time.Now().Unix())
	return err
}

func (s *Store) Progress(id, profile string) int64 {
	if profile == "" {
		profile = "default"
	}
	var p int64
	_ = s.db.QueryRow(`SELECT position_ms FROM progress WHERE profile_id=? AND media_id=?`, profile, id).Scan(&p)
	return p
}

func (s *Store) AddMarker(mediaID string, tMs int64, label string) (string, error) {
	id := fmt.Sprintf("mk%d", time.Now().UnixNano())
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO markers(id,media_id,t_ms,label,created_at) VALUES(?,?,?,?,?)`, id, mediaID, tMs, label, time.Now().Unix())
	return id, err
}

func (s *Store) Markers(mediaID string) ([]map[string]any, error) {
	rows, err := s.db.Query(`SELECT id,t_ms,label FROM markers WHERE media_id=? ORDER BY t_ms`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, label string
		var t int64
		if rows.Scan(&id, &t, &label) == nil {
			out = append(out, map[string]any{"id": id, "t_ms": t, "label": label})
		}
	}
	return out, rows.Err()
}

func (s *Store) DeleteMarker(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM markers WHERE id=?`, id)
	return err
}

func (s *Store) Trashed(limit int) ([]Media, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT id,path,title,size,mtime_ns,duration_ms,width,height,vcodec,acodec,added_at,trashed
 FROM media WHERE trashed=1 ORDER BY added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if rows.Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt, &m.Trashed) == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

func (s *Store) ToggleFavorite(mediaID, profile string) (bool, error) {
	if profile == "" {
		profile = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM profile_favorites WHERE profile_id=? AND media_id=?`, profile, mediaID).Scan(&n)
	if n > 0 {
		_, err := s.db.Exec(`DELETE FROM profile_favorites WHERE profile_id=? AND media_id=?`, profile, mediaID)
		return false, err
	}
	_, err := s.db.Exec(`INSERT INTO profile_favorites(profile_id,media_id,created_at) VALUES(?,?,?)`, profile, mediaID, time.Now().Unix())
	return true, err
}

func (s *Store) IsFavorite(mediaID, profile string) bool {
	if profile == "" {
		profile = "default"
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM profile_favorites WHERE profile_id=? AND media_id=?`, profile, mediaID).Scan(&n)
	return n > 0
}

func (s *Store) Favorites(profile string, limit int) ([]Media, error) {
	if profile == "" {
		profile = "default"
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT m.id,m.path,m.title,m.size,m.mtime_ns,m.duration_ms,m.width,m.height,m.vcodec,m.acodec,m.added_at,m.trashed
 FROM profile_favorites f JOIN media m ON m.id=f.media_id
 WHERE f.profile_id=? AND m.trashed=0 ORDER BY f.created_at DESC LIMIT ?`, profile, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if rows.Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt, &m.Trashed) == nil {
			out = append(out, m)
		}
	}
	rows.Close()
	for i := range out {
		out[i].Tags = s.tagsOf(out[i].ID)
	}
	return out, nil
}

func (s *Store) Profiles() ([]map[string]any, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at FROM profiles ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name string
		var at int64
		if rows.Scan(&id, &name, &at) == nil {
			out = append(out, map[string]any{"id": id, "name": name})
		}
	}
	return out, rows.Err()
}

func (s *Store) AddProfile(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name required")
	}
	id := fmt.Sprintf("u%d", time.Now().UnixNano())
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO profiles(id,name,created_at) VALUES(?,?,?)`, id, name, time.Now().Unix())
	return id, err
}

func (s *Store) Tags() ([]map[string]any, error) {
	rows, err := s.db.Query(`SELECT t.name, COUNT(mt.media_id) FROM tags t LEFT JOIN media_tags mt ON mt.tag_id=t.id GROUP BY t.id ORDER BY COUNT(mt.media_id) DESC, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var n string
		var c int64
		if rows.Scan(&n, &c) == nil {
			out = append(out, map[string]any{"name": n, "count": c})
		}
	}
	return out, rows.Err()
}

func (s *Store) Stats() map[string]any {
	var files, totalBytes, totalMs int64
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(size),0), COALESCE(SUM(duration_ms),0) FROM media WHERE trashed=0`).Scan(&files, &totalBytes, &totalMs)
	var tags int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&tags)
	return map[string]any{"files": files, "bytes": totalBytes, "duration_ms": totalMs, "tags": tags}
}

func (s *Store) Layout() string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM kv WHERE key='layout'`).Scan(&v); err != nil {
		return ""
	}
	return v
}

func (s *Store) SetLayout(v string) error {
	var tmp any
	if !json.Valid([]byte(v)) {
		return fmt.Errorf("layout is not valid JSON")
	}
	tmp = v
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO kv(key,value) VALUES('layout',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, tmp)
	return err
}
