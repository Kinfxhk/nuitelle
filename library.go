package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const librarySchema = `
CREATE TABLE IF NOT EXISTS collections(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  query TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tag_alias(
  alias TEXT PRIMARY KEY,
  target TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS media_size ON media(size);
CREATE TABLE IF NOT EXISTS jobs(
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  media_id TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  progress REAL NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS media_fts USING fts5(id UNINDEXED, title, body, tokenize='trigram');
`

func ensureLibrarySchema(db *sql.DB) error {
	if _, err := db.Exec(librarySchema); err != nil {
		return err
	}
	_, err := db.Exec(playlistSchema)
	return err
}

type Collection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) Collections() ([]Collection, error) {
	rows, err := s.db.Query(`SELECT id,name,query,created_at FROM collections ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		if rows.Scan(&c.ID, &c.Name, &c.Query, &c.CreatedAt) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

func (s *Store) AddCollection(name, query string) (Collection, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Collection{}, fmt.Errorf("name required")
	}
	c := Collection{ID: fmt.Sprintf("c%d", time.Now().UnixNano()), Name: name, Query: query, CreatedAt: time.Now().Unix()}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO collections(id,name,query,created_at) VALUES(?,?,?,?)`, c.ID, c.Name, c.Query, c.CreatedAt)
	return c, err
}

func (s *Store) DeleteCollection(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM collections WHERE id=?`, id)
	return err
}

// ResolveTag follows one alias hop.
func (s *Store) ResolveTag(name string) string {
	var target string
	if err := s.db.QueryRow(`SELECT target FROM tag_alias WHERE alias=?`, name).Scan(&target); err == nil {
		return target
	}
	return name
}

func (s *Store) AddTagAlias(alias, target string) error {
	alias, target = strings.TrimSpace(alias), strings.TrimSpace(target)
	if alias == "" || target == "" || alias == target {
		return fmt.Errorf("invalid alias")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO tag_alias(alias,target) VALUES(?,?) ON CONFLICT(alias) DO UPDATE SET target=excluded.target`, alias, target)
	return err
}

func (s *Store) tagID(name string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM tags WHERE name=?`, name).Scan(&id)
	return id, err
}

// RenameTag renames a tag; if the target exists it merges into it.
func (s *Store) RenameTag(from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return fmt.Errorf("invalid rename")
	}
	if _, err := s.tagID(to); err == nil {
		return s.MergeTags(from, to)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE tags SET name=? WHERE name=?`, to, from); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE tag_alias SET target=? WHERE target=?`, to, from); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MergeTags(from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return fmt.Errorf("invalid merge")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var toID int64
	if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, to).Scan(&toID); err != nil {
		return fmt.Errorf("target tag not found")
	}
	var fromID int64
	if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, from).Scan(&fromID); err != nil {
		return fmt.Errorf("source tag not found")
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO media_tags(media_id,tag_id) SELECT media_id,? FROM media_tags WHERE tag_id=?`, toID, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM media_tags WHERE tag_id=?`, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tags WHERE id=?`, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE tag_alias SET target=? WHERE target=?`, to, from); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteTag(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, name).Scan(&id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM media_tags WHERE tag_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tags WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tag_alias WHERE target=?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

type DuplicateGroup struct {
	Size       int64    `json:"size"`
	DurationMs int64    `json:"duration_ms"`
	Count      int      `json:"count"`
	IDs        []string `json:"ids"`
	Titles     []string `json:"titles"`
}

func (s *Store) Duplicates(limit int) ([]DuplicateGroup, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT size,duration_ms,COUNT(*) FROM media WHERE trashed=0 AND size>0
 GROUP BY size,duration_ms HAVING COUNT(*)>1 ORDER BY size DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DuplicateGroup
	for rows.Next() {
		var g DuplicateGroup
		if rows.Scan(&g.Size, &g.DurationMs, &g.Count) == nil {
			out = append(out, g)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Resolve members after closing the outer cursor: the pool has one connection.
	for i := range out {
		out[i].IDs, out[i].Titles, _ = s.mediaBySizeDuration(out[i].Size, out[i].DurationMs)
	}
	return out, nil
}

func (s *Store) mediaBySizeDuration(size, dur int64) ([]string, []string, error) {
	rows, err := s.db.Query(`SELECT id,title FROM media WHERE trashed=0 AND size=? AND duration_ms=? ORDER BY added_at`, size, dur)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids, titles []string
	for rows.Next() {
		var id, t string
		if rows.Scan(&id, &t) == nil {
			ids = append(ids, id)
			titles = append(titles, t)
		}
	}
	return ids, titles, rows.Err()
}

// Home returns the three shelves the default UI shows.
func (s *Store) Home() (map[string]any, error) {
	continueRows, err := s.db.Query(`SELECT m.id,m.path,m.title,m.size,m.mtime_ns,m.duration_ms,m.width,m.height,m.vcodec,m.acodec,m.added_at,m.trashed,
 COALESCE((SELECT GROUP_CONCAT(t.name, char(31)) FROM media_tags mt JOIN tags t ON t.id=mt.tag_id WHERE mt.media_id=m.id),''),
 p.position_ms
 FROM progress p JOIN media m ON m.id=p.media_id
 WHERE m.trashed=0 AND p.position_ms>5000 AND (m.duration_ms=0 OR p.position_ms < m.duration_ms*95/100)
 ORDER BY p.updated_at DESC LIMIT 12`)
	if err != nil {
		return nil, err
	}
	var cont []Media
	for continueRows.Next() {
		var m Media
		var tags string
		if continueRows.Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt, &m.Trashed, &tags, &m.PositionMs) == nil {
			if tags != "" {
				m.Tags = strings.Split(tags, string(rune(31)))
			}
			cont = append(cont, m)
		}
	}
	continueRows.Close()
	recent, _, _ := s.List("", "", "new", 12, 0)
	favs, _, _ := s.List("", "favorite", "new", 12, 0)
	return map[string]any{"continue": cont, "recent": recent, "favorites": favs}, nil
}

// RemapPaths rewrites a path prefix after a folder/NAS move.
func (s *Store) RemapPaths(oldPrefix, newPrefix string) (int64, error) {
	oldPrefix = strings.TrimRight(oldPrefix, "/")
	newPrefix = strings.TrimRight(newPrefix, "/")
	if oldPrefix == "" || newPrefix == "" {
		return 0, fmt.Errorf("prefixes required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE media SET path=? || substr(path,?) WHERE path LIKE ? ESCAPE '\'`,
		newPrefix, len(oldPrefix)+1, escLike(oldPrefix)+"%")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LibraryHealth reports cheap, index-level problems. Filesystem checks are in the API layer.
func (s *Store) LibraryHealth() map[string]any {
	var total, zero, noDur, trashed int64
	_ = s.db.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN size=0 THEN 1 ELSE 0 END), SUM(CASE WHEN duration_ms=0 THEN 1 ELSE 0 END), SUM(CASE WHEN trashed=1 THEN 1 ELSE 0 END) FROM media`).
		Scan(&total, &zero, &noDur, &trashed)
	var orphanTags int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tags t WHERE NOT EXISTS (SELECT 1 FROM media_tags mt WHERE mt.tag_id=t.id)`).Scan(&orphanTags)
	return map[string]any{"files": total, "zero_byte": zero, "no_duration": noDur, "trashed": trashed, "orphan_tags": orphanTags}
}
