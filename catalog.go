package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type CatalogItem struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Cover     string `json:"cover"`
	Overview  string `json:"overview"`
	InLibrary bool   `json:"in_library"`
}

const catalogSchema = `
CREATE TABLE IF NOT EXISTS wishlist(
  provider TEXT NOT NULL,
  item_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  year INTEGER NOT NULL DEFAULT 0,
  cover TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY(provider, item_id)
);
`

func ensureCatalogSchema(db *sql.DB) error {
	_, err := db.Exec(catalogSchema)
	return err
}

func (s *Store) AddWishlist(it CatalogItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR IGNORE INTO wishlist(provider,item_id,title,year,cover,created_at) VALUES(?,?,?,?,?,?)`,
		it.Provider, it.ID, it.Title, it.Year, it.Cover, time.Now().Unix())
	return err
}

func (s *Store) RemoveWishlist(provider, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM wishlist WHERE provider=? AND item_id=?`, provider, id)
	return err
}

func (s *Store) Wishlist() ([]CatalogItem, error) {
	rows, err := s.db.Query(`SELECT provider,item_id,title,year,cover FROM wishlist ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogItem
	for rows.Next() {
		var it CatalogItem
		if rows.Scan(&it.Provider, &it.ID, &it.Title, &it.Year, &it.Cover) == nil {
			out = append(out, it)
		}
	}
	return out, rows.Err()
}

// InLibrary reports a loose title match so catalog results can show a badge.
func (s *Store) InLibrary(title string) bool {
	if strings.TrimSpace(title) == "" {
		return false
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM media WHERE trashed=0 AND lower(title) LIKE ?`, "%"+strings.ToLower(title)+"%").Scan(&n)
	return n > 0
}

// SearchCatalog queries every plugin with the catalog.search hook.
func SearchCatalog(st *Store, query string) ([]CatalogItem, error) {
	var out []CatalogItem
	seen := map[string]bool{}
	for _, p := range activePlugins {
		items, err := p.Catalog(query)
		if err != nil {
			continue
		}
		for _, it := range items {
			it.Provider = p.ID
			key := it.Provider + ":" + it.ID
			if it.ID == "" || seen[key] {
				continue
			}
			seen[key] = true
			it.InLibrary = st.InLibrary(it.Title)
			out = append(out, it)
		}
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out, nil
}

var _ = fmt.Sprintf
