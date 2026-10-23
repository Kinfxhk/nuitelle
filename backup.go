package main

import (
	"fmt"
	"strings"
	"time"
)

type BackupMedia struct {
	ID         string   `json:"id"`
	Path       string   `json:"path"`
	Title      string   `json:"title"`
	Size       int64    `json:"size"`
	MtimeNS    int64    `json:"mtime_ns"`
	DurationMs int64    `json:"duration_ms"`
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Vcodec     string   `json:"vcodec"`
	Acodec     string   `json:"acodec"`
	AddedAt    int64    `json:"added_at"`
	Tags       []string `json:"tags"`
}

type BackupPlaylist struct {
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

type Backup struct {
	Version     int              `json:"version"`
	ExportedAt  int64            `json:"exported_at"`
	Layout      string           `json:"layout"`
	Media       []BackupMedia    `json:"media"`
	Collections []Collection     `json:"collections"`
	Playlists   []BackupPlaylist `json:"playlists"`
}

// ExportAll dumps metadata only. Media files are never copied.
func (s *Store) ExportAll() (*Backup, error) {
	b := &Backup{Version: 1, ExportedAt: time.Now().Unix(), Layout: s.Layout()}
	rows, err := s.db.Query(`SELECT id,path,title,size,mtime_ns,duration_ms,width,height,vcodec,acodec,added_at FROM media WHERE trashed=0 ORDER BY path`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m BackupMedia
		if rows.Scan(&m.ID, &m.Path, &m.Title, &m.Size, &m.MtimeNS, &m.DurationMs, &m.Width, &m.Height, &m.Vcodec, &m.Acodec, &m.AddedAt) == nil {
			m.Tags = s.tagsOf(m.ID)
			b.Media = append(b.Media, m)
		}
	}
	rows.Close()
	if cs, err := s.Collections(); err == nil {
		b.Collections = cs
	}
	ps, _ := s.Playlists()
	for _, p := range ps {
		items, _ := s.PlaylistItems(p.ID)
		bp := BackupPlaylist{Name: p.Name}
		for _, it := range items {
			bp.Items = append(bp.Items, it.ID)
		}
		b.Playlists = append(b.Playlists, bp)
	}
	return b, nil
}

// ImportAll is idempotent and runs in one transaction: media merge by path,
// playlists by name. A failure rolls the whole import back.
func (s *Store) ImportAll(b *Backup) (mediaCount, playlistCount int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	resolve := func(name string) string {
		var target string
		if tx.QueryRow(`SELECT target FROM tag_alias WHERE alias=?`, name).Scan(&target) == nil {
			return target
		}
		return name
	}

	for _, m := range b.Media {
		title := m.Title
		if title == "" {
			title = titleOf(m.Path)
		}
		var existing string
		if tx.QueryRow(`SELECT id FROM media WHERE path=?`, m.Path).Scan(&existing) == nil {
			if _, err := tx.Exec(`UPDATE media SET title=?,size=?,mtime_ns=?,duration_ms=?,width=?,height=?,vcodec=?,acodec=? WHERE id=?`,
				title, m.Size, m.MtimeNS, m.DurationMs, m.Width, m.Height, m.Vcodec, m.Acodec, existing); err != nil {
				return 0, 0, err
			}
			m.ID = existing
		} else {
			id := m.ID
			if id == "" {
				id = mediaID(m.Path)
			}
			if _, err := tx.Exec(`INSERT INTO media(id,path,title,size,mtime_ns,duration_ms,width,height,vcodec,acodec,added_at,trashed)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,0)`, id, m.Path, title, m.Size, m.MtimeNS, m.DurationMs, m.Width, m.Height, m.Vcodec, m.Acodec, m.AddedAt); err != nil {
				return 0, 0, err
			}
			m.ID = id
		}
		for _, raw := range m.Tags {
			name := resolve(raw)
			if _, err := tx.Exec(`INSERT OR IGNORE INTO tags(name) VALUES(?)`, name); err != nil {
				return 0, 0, err
			}
			var tid int64
			if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, name).Scan(&tid); err != nil {
				return 0, 0, err
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO media_tags(media_id,tag_id) VALUES(?,?)`, m.ID, tid); err != nil {
				return 0, 0, err
			}
		}
		mediaCount++
	}

	for _, c := range b.Collections {
		var n int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM collections WHERE name=?`, c.Name).Scan(&n)
		if n == 0 {
			id := c.ID
			if id == "" {
				id = fmt.Sprintf("c%d", time.Now().UnixNano())
			}
			if _, err := tx.Exec(`INSERT INTO collections(id,name,query,created_at) VALUES(?,?,?,?)`, id, c.Name, c.Query, time.Now().Unix()); err != nil {
				return 0, 0, err
			}
		}
	}

	for _, p := range b.Playlists {
		var pid string
		err := tx.QueryRow(`SELECT id FROM playlists WHERE name=?`, p.Name).Scan(&pid)
		if err != nil {
			pid = fmt.Sprintf("p%d", time.Now().UnixNano())
			if _, err := tx.Exec(`INSERT INTO playlists(id,name,created_at) VALUES(?,?,?)`, pid, p.Name, time.Now().Unix()); err != nil {
				return 0, 0, err
			}
		}
		var ord int
		_ = tx.QueryRow(`SELECT COALESCE(MAX(ord),0)+1 FROM playlist_items WHERE playlist_id=?`, pid).Scan(&ord)
		for _, mid := range p.Items {
			var exists int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM media WHERE id=?`, mid).Scan(&exists)
			if exists == 0 {
				continue
			}
			if _, err := tx.Exec(`INSERT OR IGNORE INTO playlist_items(playlist_id,media_id,ord) VALUES(?,?,?)`, pid, mid, ord); err != nil {
				return 0, 0, err
			}
			ord++
		}
		playlistCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	_ = s.rebuildFTSLocked()
	return mediaCount, playlistCount, nil
}

// rebuildFTSLocked rebuilds the index assuming the caller holds s.mu.
func (s *Store) rebuildFTSLocked() error {
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

func fallback(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
