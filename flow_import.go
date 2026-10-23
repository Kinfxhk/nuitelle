package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"
)

// ImportFlow copies media rows, progress, favourites, active playlists and
// trash state from a Flow database. Media are matched by path.
func ImportFlow(v *Store, flowPath string) (media, progress, favs, playlists, items, trashed, missing int, err error) {
	db, err := sql.Open("sqlite", "file:"+flowPath+"?mode=ro")
	if err != nil {
		return
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id,path,title,name,bytes,added FROM media`)
	if err != nil {
		return
	}
	idMap := map[string]string{}
	for rows.Next() {
		var fid, path, title, name string
		var bytes int64
		var added float64
		if rows.Scan(&fid, &path, &title, &name, &bytes, &added) != nil || path == "" {
			continue
		}
		if title == "" {
			title = name
		}
		if title == "" {
			title = titleOf(path)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			missing++
			continue
		}
		nid := mediaID(path)
		if ex, ok := v.Existing(path); ok {
			nid = ex.ID
			if ex.Title == "" || ex.Title == titleOf(path) {
				_ = v.UpdateTitle(nid, title)
			}
		} else {
			_ = v.Upsert(Media{ID: nid, Path: path, Title: title, Size: bytes, MtimeNS: int64(added * 1e9), AddedAt: int64(added)})
		}
		idMap[fid] = nid
		media++
	}
	rows.Close()

	// Progress and favourites.
	ur, err := db.Query(`SELECT id,favorite,position FROM user_media`)
	if err == nil {
		for ur.Next() {
			var fid string
			var fav int
			var pos float64
			if ur.Scan(&fid, &fav, &pos) != nil {
				continue
			}
			nid, ok := idMap[fid]
			if !ok {
				continue
			}
			if pos > 0 {
				_ = v.SetProgress(nid, "default", int64(pos*1000))
				progress++
			}
			if fav == 1 {
				if on, _ := v.ToggleFavorite(nid, "default"); on {
					favs++
				}
			}
		}
		ur.Close()
	}

	// Active playlists only.
	pr, err := db.Query(`SELECT id,name FROM playlists WHERE archived=0`)
	if err == nil {
		existing, _ := v.Playlists()
		byName := map[string]string{}
		for _, p := range existing {
			byName[p.Name] = p.ID
		}
		for pr.Next() {
			var pid, name string
			if pr.Scan(&pid, &name) != nil || name == "" {
				continue
			}
			npid, ok := byName[name]
			if !ok {
				np, err := v.AddPlaylist(name)
				if err != nil {
					continue
				}
				npid = np.ID
				byName[name] = npid
			}
			ir, err := db.Query(`SELECT media_id FROM playlist_items WHERE playlist_id=? ORDER BY ordinal`, pid)
			if err == nil {
				var ids []string
				for ir.Next() {
					var fid string
					if ir.Scan(&fid) == nil {
						if nid, ok := idMap[fid]; ok {
							ids = append(ids, nid)
						}
					}
				}
				ir.Close()
				if len(ids) > 0 {
					n, _ := v.AddToPlaylist(npid, ids)
					items += n
				}
			}
			playlists++
		}
		pr.Close()
	}

	// Trash state.
	tr, err := db.Query(`SELECT media_id FROM trash`)
	if err == nil {
		for tr.Next() {
			var fid string
			if tr.Scan(&fid) == nil {
				if nid, ok := idMap[fid]; ok {
					_ = v.SetTrashed(nid, true)
					trashed++
				}
			}
		}
		tr.Close()
	}
	_ = v.RebuildFTS()
	_ = time.Now()
	return
}

var _ = fmt.Sprintf
