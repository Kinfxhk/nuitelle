package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type BlacklistRule struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"`
	MaxSecs int    `json:"max_seconds"`
	Note    string `json:"note"`
	Enabled bool   `json:"enabled"`
	re      *regexp.Regexp
}

const blacklistSchema = `
CREATE TABLE IF NOT EXISTS blacklist(
  id TEXT PRIMARY KEY,
  pattern TEXT NOT NULL,
  max_seconds INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);
`

func ensureBlacklistSchema(db *sql.DB) error {
	_, err := db.Exec(blacklistSchema)
	return err
}

func (s *Store) AddBlacklist(pattern string, maxSecs int, note string) (BlacklistRule, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return BlacklistRule{}, fmt.Errorf("pattern required")
	}
	if _, err := regexp.Compile("(?i)" + pattern); err != nil {
		return BlacklistRule{}, fmt.Errorf("bad regex: %v", err)
	}
	r := BlacklistRule{ID: fmt.Sprintf("b%d", time.Now().UnixNano()), Pattern: pattern, MaxSecs: maxSecs, Note: note, Enabled: true}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO blacklist(id,pattern,max_seconds,note,enabled,created_at) VALUES(?,?,?,?,1,?)`, r.ID, r.Pattern, r.MaxSecs, r.Note, time.Now().Unix())
	return r, err
}

func (s *Store) Blacklist() ([]BlacklistRule, error) {
	rows, err := s.db.Query(`SELECT id,pattern,max_seconds,note,enabled FROM blacklist WHERE enabled=1 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlacklistRule
	for rows.Next() {
		var r BlacklistRule
		var en int
		if rows.Scan(&r.ID, &r.Pattern, &r.MaxSecs, &r.Note, &en) == nil {
			r.Enabled = en == 1
			r.re = regexp.MustCompile("(?i)" + r.Pattern)
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

func (s *Store) RemoveBlacklist(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM blacklist WHERE id=?`, id)
	return err
}

var activeBlacklist []BlacklistRule

func ReloadBlacklist(st *Store) {
	activeBlacklist, _ = st.Blacklist()
}

// MatchBlacklist reports whether a file looks like an advert.
func MatchBlacklist(title, path string, durationMs int64) bool {
	for _, r := range activeBlacklist {
		if r.re == nil || !r.re.MatchString(title) && !r.re.MatchString(path) {
			continue
		}
		if r.MaxSecs > 0 && durationMs > int64(r.MaxSecs)*1000 {
			continue
		}
		return true
	}
	return false
}
