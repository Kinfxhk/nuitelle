package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

type nfoDoc struct {
	Title string   `xml:"title"`
	Plot  string   `xml:"plot"`
	Tags  []string `xml:"tag"`
}

// ApplyNFO reads a same-name .nfo file and applies its title and tags.
func ApplyNFO(st *Store, m Media) {
	stem := strings.TrimSuffix(m.Path, filepath.Ext(m.Path))
	b, err := os.ReadFile(stem + ".nfo")
	if err != nil {
		return
	}
	var d nfoDoc
	if xml.Unmarshal(b, &d) != nil {
		return
	}
	if t := strings.TrimSpace(d.Title); t != "" && t != m.Title {
		_ = st.UpdateTitle(m.ID, t)
	}
	var tags []string
	for _, t := range d.Tags {
		if n := normalizeTag(t); n != "" {
			tags = append(tags, n)
		}
	}
	if len(tags) > 0 {
		_ = st.AddTags(m.ID, tags)
	}
}

// LocalPoster returns a same-folder poster image if one exists.
func LocalPoster(m Media) string {
	stem := strings.TrimSuffix(m.Path, filepath.Ext(m.Path))
	dir := filepath.Dir(m.Path)
	for _, c := range []string{
		stem + "-poster.jpg", stem + ".jpg", stem + "-poster.png",
		filepath.Join(dir, "poster.jpg"), filepath.Join(dir, "folder.jpg"),
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
