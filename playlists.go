package main

import (
	"fmt"
	"strings"
	"time"
)

const playlistSchema = `
CREATE TABLE IF NOT EXISTS playlists(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS playlist_items(
  playlist_id TEXT NOT NULL,
  media_id TEXT NOT NULL,
  ord INTEGER NOT NULL,
  PRIMARY KEY(playlist_id, media_id)
);
`

type Playlist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) Playlists() ([]Playlist, error) {
	rows, err := s.db.Query(`SELECT p.id,p.name,p.created_at,
 COALESCE((SELECT COUNT(*) FROM playlist_items i WHERE i.playlist_id=p.id),0)
 FROM playlists p ORDER BY p.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.Count) == nil {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) AddPlaylist(name string) (Playlist, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Playlist{}, fmt.Errorf("name required")
	}
	p := Playlist{ID: fmt.Sprintf("p%d", time.Now().UnixNano()), Name: name, CreatedAt: time.Now().Unix()}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO playlists(id,name,created_at) VALUES(?,?,?)`, p.ID, p.Name, p.CreatedAt)
	return p, err
}

func (s *Store) DeletePlaylist(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM playlists WHERE id=?`, id)
	return err
}

func (s *Store) PlaylistItems(id string) ([]Media, error) {
	rows, err := s.db.Query(`SELECT m.id,m.path,m.title,m.size,m.mtime_ns,m.duration_ms,m.width,m.height,m.vcodec,m.acodec,m.added_at,m.trashed
 FROM playlist_items i JOIN media m ON m.id=i.media_id
 WHERE i.playlist_id=? AND m.trashed=0 ORDER BY i.ord`, id)
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
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Attach tags after closing the cursor: the pool has one connection.
	for i := range out {
		out[i].Tags = s.tagsOf(out[i].ID)
	}
	return out, nil
}

func (s *Store) AddToPlaylist(id string, mediaIDs []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var next int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(ord),0)+1 FROM playlist_items WHERE playlist_id=?`, id).Scan(&next)
	added := 0
	for _, mid := range mediaIDs {
		res, err := s.db.Exec(`INSERT OR IGNORE INTO playlist_items(playlist_id,media_id,ord) VALUES(?,?,?)`, id, mid, next)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
			next++
		}
	}
	return added, nil
}

func (s *Store) RemoveFromPlaylist(id, mediaID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM playlist_items WHERE playlist_id=? AND media_id=?`, id, mediaID); err != nil {
		return err
	}
	// Renumber so ord stays dense after a removal.
	rows, err := s.db.Query(`SELECT media_id FROM playlist_items WHERE playlist_id=? ORDER BY ord`, id)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var m string
		if rows.Scan(&m) == nil {
			ids = append(ids, m)
		}
	}
	rows.Close()
	for i, m := range ids {
		s.db.Exec(`UPDATE playlist_items SET ord=? WHERE playlist_id=? AND media_id=?`, i+1, id, m)
	}
	return nil
}

func (s *Store) ReorderPlaylist(id string, mediaIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, mid := range mediaIDs {
		if _, err := tx.Exec(`UPDATE playlist_items SET ord=? WHERE playlist_id=? AND media_id=?`, i+1, id, mid); err != nil {
			return err
		}
	}
	return tx.Commit()
}
