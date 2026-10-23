package main

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type Feed struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	LastChecked int64  `json:"last_checked"`
	Items       int    `json:"items"`
}

const feedsSchema = `
CREATE TABLE IF NOT EXISTS feeds(
  id TEXT PRIMARY KEY,
  url TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  last_checked INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS feed_items(
  feed_id TEXT NOT NULL,
  guid TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  url TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(feed_id, guid)
);
`

func ensureFeedsSchema(db *sql.DB) error {
	_, err := db.Exec(feedsSchema)
	return err
}

func (s *Store) Feeds() ([]Feed, error) {
	rows, err := s.db.Query(`SELECT f.id,f.url,f.name,f.enabled,f.last_checked,
 COALESCE((SELECT COUNT(*) FROM feed_items i WHERE i.feed_id=f.id),0)
 FROM feeds f ORDER BY f.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feed
	for rows.Next() {
		var f Feed
		var en int
		if rows.Scan(&f.ID, &f.URL, &f.Name, &en, &f.LastChecked, &f.Items) == nil {
			f.Enabled = en == 1
			out = append(out, f)
		}
	}
	return out, rows.Err()
}

func (s *Store) AddFeed(feedURL, name string) (Feed, error) {
	u, err := url.Parse(strings.TrimSpace(feedURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Feed{}, fmt.Errorf("http(s) URL required")
	}
	if name == "" {
		name = u.Host
	}
	f := Feed{ID: fmt.Sprintf("f%d", time.Now().UnixNano()), URL: u.String(), Name: name, Enabled: true}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(`INSERT INTO feeds(id,url,name,enabled,created_at) VALUES(?,?,?,1,?)`, f.ID, f.URL, f.Name, time.Now().Unix())
	return f, err
}

func (s *Store) DeleteFeed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`DELETE FROM feed_items WHERE feed_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM feeds WHERE id=?`, id)
	return err
}

func (s *Store) MarkFeedChecked(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE feeds SET last_checked=? WHERE id=?`, time.Now().Unix(), id)
}

func (s *Store) FeedSeen(feedID, guid string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM feed_items WHERE feed_id=? AND guid=?`, feedID, guid).Scan(&n)
	return n > 0
}

func (s *Store) MarkFeedItem(feedID, guid, title, link string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`INSERT OR IGNORE INTO feed_items(feed_id,guid,title,url,created_at) VALUES(?,?,?,?,?)`, feedID, guid, title, link, time.Now().Unix())
}

type feedDoc struct {
	Channel struct {
		Items []struct {
			Title     string `xml:"title"`
			Link      string `xml:"link"`
			GUID      string `xml:"guid"`
			Enclosure struct {
				URL  string `xml:"url,attr"`
				Type string `xml:"type,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		ID string `xml:"id"`
	} `xml:"entry"`
}

// PollFeed fetches one feed and ingests new media URLs.
func PollFeed(ctx context.Context, cfg *Config, st *Store, f Feed) (ingested int, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", f.URL, nil)
	req.Header.Set("User-Agent", "Nuitelle/feeds")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return 0, err
	}
	var doc feedDoc
	if xml.Unmarshal(body, &doc) != nil {
		return 0, fmt.Errorf("not RSS/Atom")
	}
	type item struct{ guid, title, media string }
	var items []item
	for _, it := range doc.Channel.Items {
		media := it.Enclosure.URL
		if media == "" && isMediaURL(it.Link) {
			media = it.Link
		}
		guid := it.GUID
		if guid == "" {
			guid = media
		}
		if media != "" && guid != "" {
			items = append(items, item{guid, it.Title, media})
		}
	}
	for _, e := range doc.Entries {
		media := ""
		for _, l := range e.Links {
			if isMediaURL(l.Href) {
				media = l.Href
			}
		}
		guid := e.ID
		if guid == "" {
			guid = media
		}
		if media != "" && guid != "" {
			items = append(items, item{guid, e.Title, media})
		}
	}
	for _, it := range items {
		if ingested >= 10 {
			break
		}
		if st.FeedSeen(f.ID, it.guid) {
			continue
		}
		path, err := IngestURL(ctx, cfg, it.media)
		if err != nil {
			st.MarkFeedItem(f.ID, it.guid, it.title, it.media)
			continue
		}
		_, _ = ScanRoot(st, filepath.Dir(path), false)
		st.MarkFeedItem(f.ID, it.guid, it.title, it.media)
		ingested++
	}
	st.MarkFeedChecked(f.ID)
	_ = st.RebuildFTS()
	return ingested, nil
}

func isMediaURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	switch ext {
	case ".mp4", ".m4v", ".mkv", ".webm", ".mov", ".m3u8", ".ts":
		return true
	}
	return false
}
