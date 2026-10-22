package main

import "time"

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

// ImportAll is idempotent: media merge by path, playlists by name.
func (s *Store) ImportAll(b *Backup) (mediaCount, playlistCount int, err error) {
	for _, m := range b.Media {
		m.Title = fallback(m.Title, titleOf(m.Path))
		_ = s.Upsert(Media{ID: m.ID, Path: m.Path, Title: m.Title, Size: m.Size, MtimeNS: m.MtimeNS,
			DurationMs: m.DurationMs, Width: m.Width, Height: m.Height, Vcodec: m.Vcodec, Acodec: m.Acodec, AddedAt: m.AddedAt})
		if _, err := s.Get(m.ID); err != nil {
			// Upsert kept another ID for this path; resolve it by path.
			if ex, ok := s.Existing(m.Path); ok {
				m.ID = ex.ID
			}
		}
		if len(m.Tags) > 0 {
			_ = s.AddTags(m.ID, m.Tags)
		}
		mediaCount++
	}
	existingCollections, _ := s.Collections()
	collNames := map[string]bool{}
	for _, c := range existingCollections {
		collNames[c.Name] = true
	}
	for _, c := range b.Collections {
		if collNames[c.Name] {
			continue
		}
		if _, err := s.AddCollection(c.Name, c.Query); err == nil {
			collNames[c.Name] = true
		}
	}
	existing, _ := s.Playlists()
	byName := map[string]string{}
	for _, p := range existing {
		byName[p.Name] = p.ID
	}
	for _, p := range b.Playlists {
		id, ok := byName[p.Name]
		if !ok {
			np, err := s.AddPlaylist(p.Name)
			if err != nil {
				continue
			}
			id = np.ID
		}
		if len(p.Items) > 0 {
			_, _ = s.AddToPlaylist(id, p.Items)
		}
		playlistCount++
	}
	return mediaCount, playlistCount, nil
}

func fallback(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
