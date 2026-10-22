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
  media_id TEXT PRIMARY KEY,
  position_ms INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
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
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(subsSchema); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureLibrarySchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, dir: filepath.Dir(path)}, nil
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

// List returns media with optional text and tag filters.
func (s *Store) List(q, tag, sort string, limit, offset int) ([]Media, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	where := []string{"m.trashed=0"}
	args := []any{}
	if q != "" {
		where = append(where, `m.title LIKE ? ESCAPE '\'`)
		args = append(args, "%"+escLike(q)+"%")
	}
	if tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=m.id AND t.name=?)`)
		args = append(args, tag)
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
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		n = s.ResolveTag(n)
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO tags(name) VALUES(?)`, n); err != nil {
			return err
		}
		var tid int64
		if err := s.db.QueryRow(`SELECT id FROM tags WHERE name=?`, n).Scan(&tid); err != nil {
			return err
		}
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO media_tags(media_id,tag_id) VALUES(?,?)`, id, tid); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveTag(id, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM media_tags WHERE media_id=? AND tag_id=(SELECT id FROM tags WHERE name=?)`, id, name)
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

func (s *Store) SetProgress(id string, ms int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO progress(media_id,position_ms,updated_at) VALUES(?,?,?)
 ON CONFLICT(media_id) DO UPDATE SET position_ms=excluded.position_ms, updated_at=excluded.updated_at`, id, ms, time.Now().Unix())
	return err
}

func (s *Store) Progress(id string) int64 {
	var p int64
	_ = s.db.QueryRow(`SELECT position_ms FROM progress WHERE media_id=?`, id).Scan(&p)
	return p
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
