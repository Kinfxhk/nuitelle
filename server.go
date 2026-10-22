package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

var (
	scanMu      sync.Mutex
	scanRunning bool
)

const defaultLayout = `{
  "version": 1,
  "sidebar": [
    {"id":"home","label":"Home","icon":"home","route":"/","order":10},
    {"id":"library","label":"Library","icon":"grid","route":"/library","order":20},
    {"id":"tags","label":"Tags","icon":"tag","route":"/tags","order":30}
  ],
  "density": "comfortable"
}`

func Serve(cfg *Config, st *Store) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		scanMu.Lock()
		running := scanRunning
		scanMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "version": version, "scanning": running})
	})

	mux.HandleFunc("GET /api/home", func(w http.ResponseWriter, r *http.Request) {
		data, err := st.Home()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, data)
	})

	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, st.Stats())
	})

	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, r *http.Request) {
		tags, err := st.Tags()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"tags": tags})
	})

	mux.HandleFunc("GET /api/media", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		tag := r.URL.Query().Get("tag")
		sort := r.URL.Query().Get("sort")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		items, total, err := st.List(q, tag, sort, limit, offset)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": total})
	})

	mux.HandleFunc("GET /api/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"item": m, "position_ms": st.Progress(m.ID)})
	})

	mux.HandleFunc("GET /api/media/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil || m.Trashed {
			http.NotFound(w, r)
			return
		}
		if !allowedPath(cfg, m.Path) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f, err := os.Open(m.Path)
		if err != nil {
			http.Error(w, "cannot open media", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.Error(w, "cannot stat media", http.StatusInternalServerError)
			return
		}
		ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(m.Path)))
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition", "inline")
		http.ServeContent(w, r, m.Title, info.ModTime(), f)
	})

	mux.HandleFunc("GET /api/media/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(cfg.CoverDir, m.ID+".jpg")
		if _, err := os.Stat(path); err != nil {
			if err := makeCover(cfg, m); err != nil {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("POST /api/media/{id}/tags", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Add    []string `json:"add"`
			Remove []string `json:"remove"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		id := r.PathValue("id")
		if _, err := st.Get(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		if err := st.AddTags(id, body.Add); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		for _, n := range body.Remove {
			_ = st.RemoveTag(id, n)
		}
		m, _ := st.Get(id)
		writeJSON(w, 200, map[string]any{"item": m})
	})

	mux.HandleFunc("POST /api/media/{id}/progress", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PositionMs int64 `json:"position_ms"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		if err := st.SetProgress(r.PathValue("id"), body.PositionMs); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/media/{id}/trash", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		if err := trashMedia(cfg, st, m); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/media/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		if err := restoreMedia(st, m); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) {
		scanMu.Lock()
		if scanRunning {
			scanMu.Unlock()
			writeJSON(w, 200, map[string]bool{"started": false, "running": true})
			return
		}
		scanRunning = true
		scanMu.Unlock()
		var body struct {
			Full bool `json:"full"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		go func() {
			defer func() {
				scanMu.Lock()
				scanRunning = false
				scanMu.Unlock()
			}()
			for _, root := range cfg.Roots {
				_, _ = ScanRoot(st, root, body.Full)
			}
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	var subsJobs sync.Map

	mux.HandleFunc("GET /api/media/{id}/subtitles", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		subs, err := st.ListSubtitles(id)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		items := make([]map[string]any, 0, len(subs))
		for _, x := range subs {
			items = append(items, map[string]any{
				"lang": x.Lang, "kind": x.Kind, "model": x.Model, "created_at": x.CreatedAt,
				"url": "/api/media/" + id + "/subtitles/" + x.Lang + "?kind=" + x.Kind,
			})
		}
		status := map[string]any{"items": items}
		if v, ok := subsJobs.Load(id); ok {
			status["job"] = v
		}
		writeJSON(w, 200, status)
	})

	mux.HandleFunc("GET /api/media/{id}/subtitles/{lang}", func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			kind = "transcript"
		}
		sub, ok := st.GetSubtitle(r.PathValue("id"), kind, r.PathValue("lang"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(sub.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write([]byte(srtToVTT(string(b))))
	})

	mux.HandleFunc("POST /api/media/{id}/subtitles", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := st.Get(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		var body struct {
			Model     string `json:"model"`
			Lang      string `json:"lang"`
			Translate string `json:"translate"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
		if body.Lang == "" {
			body.Lang = "auto"
		}
		if body.Model == "" {
			body.Model = cfg.WhisperModel
		}
		if _, busy := subsJobs.Load(id); busy {
			writeJSON(w, 200, map[string]bool{"started": false, "running": true})
			return
		}
		subsJobs.Store(id, map[string]any{"running": true})
		go func() {
			_, err := GenerateSubtitles(cfg, st, id, SubOptions{Model: body.Model, Lang: body.Lang, Translate: body.Translate})
			if err != nil {
				subsJobs.Store(id, map[string]any{"running": false, "error": err.Error()})
			} else {
				subsJobs.Store(id, map[string]any{"running": false})
			}
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	mux.HandleFunc("GET /api/playlists", func(w http.ResponseWriter, r *http.Request) {
		ps, err := st.Playlists()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"playlists": ps})
	})

	mux.HandleFunc("POST /api/playlists", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
		p, err := st.AddPlaylist(body.Name)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, p)
	})

	mux.HandleFunc("DELETE /api/playlists/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := st.DeletePlaylist(r.PathValue("id")); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/playlists/{id}", func(w http.ResponseWriter, r *http.Request) {
		items, err := st.PlaylistItems(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})

	mux.HandleFunc("POST /api/playlists/{id}/items", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
		n, err := st.AddToPlaylist(r.PathValue("id"), body.IDs)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"added": n})
	})

	mux.HandleFunc("DELETE /api/playlists/{id}/items/{mid}", func(w http.ResponseWriter, r *http.Request) {
		if err := st.RemoveFromPlaylist(r.PathValue("id"), r.PathValue("mid")); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/playlists/{id}/order", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
		if err := st.ReorderPlaylist(r.PathValue("id"), body.IDs); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/collections", func(w http.ResponseWriter, r *http.Request) {
		cs, err := st.Collections()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"collections": cs})
	})

	mux.HandleFunc("POST /api/collections", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name, Query string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		c, err := st.AddCollection(body.Name, body.Query)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, c)
	})

	mux.HandleFunc("DELETE /api/collections/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := st.DeleteCollection(r.PathValue("id")); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/tags/rename", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ From, To string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
		if err := st.RenameTag(body.From, body.To); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/tags/merge", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ From, To string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
		if err := st.MergeTags(body.From, body.To); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/tags/alias", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Alias, Target string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
		if err := st.AddTagAlias(body.Alias, body.Target); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/tags/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := st.DeleteTag(r.PathValue("name")); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/duplicates", func(w http.ResponseWriter, r *http.Request) {
		groups, err := st.Duplicates(50)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"groups": groups})
	})

	mux.HandleFunc("GET /api/library/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, st.LibraryHealth())
	})

	mux.HandleFunc("POST /api/remap", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Old, New string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body)
		n, err := st.RemapPaths(body.Old, body.New)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"updated": n})
	})

	mux.HandleFunc("POST /api/bulk", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs    []string `json:"ids"`
			Action string   `json:"action"`
			Value  string   `json:"value"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		done, failed := 0, []string{}
		for _, id := range body.IDs {
			m, err := st.Get(id)
			if err != nil {
				failed = append(failed, id)
				continue
			}
			switch body.Action {
			case "tag":
				err = st.AddTags(id, []string{body.Value})
			case "untag":
				err = st.RemoveTag(id, body.Value)
			case "trash":
				err = trashMedia(cfg, st, m)
			case "restore":
				err = restoreMedia(st, m)
			default:
				err = fmt.Errorf("unknown action")
			}
			if err != nil {
				failed = append(failed, id)
			} else {
				done++
			}
		}
		writeJSON(w, 200, map[string]any{"done": done, "failed": failed})
	})

	mux.HandleFunc("GET /api/layout", func(w http.ResponseWriter, r *http.Request) {
		v := st.Layout()
		if v == "" {
			v = defaultLayout
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(v))
	})

	mux.HandleFunc("PUT /api/layout", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 0, 4096)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			b = append(b, buf[:n]...)
			if err != nil {
				break
			}
			if len(b) > 64<<10 {
				writeJSON(w, 413, map[string]string{"error": "layout too large"})
				return
			}
		}
		if err := st.SetLayout(string(b)); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				r.URL.Path = "/"
			}
		}
		w.Header().Set("X-Robots-Tag", "noindex")
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func allowedPath(cfg *Config, path string) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	for _, root := range cfg.Roots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if real == r || strings.HasPrefix(real, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func makeCover(cfg *Config, m Media) error {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.CoverDir, 0o700); err != nil {
		return err
	}
	out := filepath.Join(cfg.CoverDir, m.ID+".jpg")
	cmd := exec.Command(bin, "-v", "error", "-ss", "2", "-i", m.Path,
		"-frames:v", "1", "-vf", "scale=480:-2", "-q:v", "4", "-y", out)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func trashMedia(cfg *Config, st *Store, m Media) error {
	moved, err := moveToTrash(cfg, m)
	if err != nil {
		return err
	}
	if err := st.RecordTrash(m.ID, m.Path, moved); err != nil {
		return err
	}
	return st.SetTrashed(m.ID, true)
}

func restoreMedia(st *Store, m Media) error {
	orig, moved, ok := st.TrashRecord(m.ID)
	if ok {
		if _, err := os.Stat(moved); err == nil {
			if err := os.MkdirAll(filepath.Dir(orig), 0o700); err == nil {
				if err := os.Rename(moved, orig); err != nil {
					return err
				}
			}
		}
		_ = st.ClearTrashRecord(m.ID)
	}
	return st.SetTrashed(m.ID, false)
}

func moveToTrash(cfg *Config, m Media) (string, error) {
	var root string
	for _, r := range cfg.Roots {
		if strings.HasPrefix(m.Path, r+string(os.PathSeparator)) {
			root = r
			break
		}
	}
	if root == "" {
		return "", fmt.Errorf("file is outside configured folders")
	}
	dir := filepath.Join(root, ".vela-trash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().Unix(), filepath.Base(m.Path)))
	if err := os.Rename(m.Path, dst); err != nil {
		return "", err
	}
	return dst, nil
}
