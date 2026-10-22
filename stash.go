package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// ImportStash copies titles and tags from a Stash database. Media are matched
// by file path; nothing is moved or rewritten on disk.
func ImportStash(v *Store, stashPath string) (matched, tagged, titles int, err error) {
	db, err := sql.Open("sqlite", "file:"+stashPath+"?mode=ro")
	if err != nil {
		return 0, 0, 0, err
	}
	defer db.Close()

	// Stash has used both scenes.path and scene_files over time.
	hasScenesPath := columnExists(db, "scenes", "path")
	pathExpr := "s.path"
	join := ""
	if !hasScenesPath {
		if !tableExists(db, "scene_files") {
			return 0, 0, 0, fmt.Errorf("unsupported stash schema: no scenes.path or scene_files")
		}
		pathExpr = "f.path"
		join = " JOIN scene_files f ON f.scene_id = s.id"
	}
	rows, err := db.Query(`SELECT s.id, COALESCE(s.title,''), COALESCE(s.studio_id,0), ` + pathExpr + ` FROM scenes s` + join)
	if err != nil {
		return 0, 0, 0, err
	}
	type scene struct {
		id, title, path string
		studio          int64
	}
	var scenes []scene
	for rows.Next() {
		var sc scene
		if rows.Scan(&sc.id, &sc.title, &sc.studio, &sc.path) == nil && sc.path != "" {
			scenes = append(scenes, sc)
		}
	}
	rows.Close()

	for _, sc := range scenes {
		m, ok := v.Existing(sc.path)
		if !ok {
			continue
		}
		matched++
		if sc.title != "" && sc.title != m.Title {
			if err := v.UpdateTitle(m.ID, sc.title); err == nil {
				titles++
			}
		}
		var names []string
		if tableExists(db, "scenes_tags") && tableExists(db, "tags") {
			r, err := db.Query(`SELECT t.name FROM scenes_tags st JOIN tags t ON t.id=st.tag_id WHERE st.scene_id=?`, sc.id)
			if err == nil {
				for r.Next() {
					var n string
					if r.Scan(&n) == nil && n != "" {
						names = append(names, n)
					}
				}
				r.Close()
			}
		}
		if tableExists(db, "performers_scenes") && tableExists(db, "performers") {
			r, err := db.Query(`SELECT p.name FROM performers_scenes ps JOIN performers p ON p.id=ps.performer_id WHERE ps.scene_id=?`, sc.id)
			if err == nil {
				for r.Next() {
					var n string
					if r.Scan(&n) == nil && n != "" {
						names = append(names, n)
					}
				}
				r.Close()
			}
		}
		if sc.studio > 0 && tableExists(db, "studios") {
			var n string
			if db.QueryRow(`SELECT name FROM studios WHERE id=?`, sc.studio).Scan(&n) == nil && n != "" {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			if err := v.AddTags(m.ID, names); err == nil {
				tagged++
			}
		}
	}
	return matched, tagged, titles, nil
}

func tableExists(db *sql.DB, name string) bool {
	var n int
	return db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n) == nil && n > 0
}

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk) == nil && strings.EqualFold(name, column) {
			return true
		}
	}
	return false
}
