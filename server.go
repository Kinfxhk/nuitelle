package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
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
    {"id":"people","label":"People","icon":"person","route":"/people","order":22},
    {"id":"tags","label":"Tags","icon":"tag","route":"/tags","order":30}
  ],
  "density": "comfortable"
}`

func profileOf(r *http.Request) string {
	p := r.Header.Get("X-Nuitelle-Profile")
	if p == "" {
		p = r.Header.Get("X-Vela-Profile")
	}
	if p == "" {
		return "default"
	}
	return p
}

var (
	loginMu    sync.Mutex
	loginFails = map[string]struct {
		count int
		until time.Time
	}{}
)

func checkLoginRate(ip string) bool {
	loginMu.Lock()
	defer loginMu.Unlock()
	st, ok := loginFails[ip]
	if ok && time.Now().Before(st.until) {
		return false
	}
	return true
}

func noteLoginFailure(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	st := loginFails[ip]
	st.count++
	if st.count >= 5 {
		st.count = 0
		st.until = time.Now().Add(5 * time.Minute)
	}
	loginFails[ip] = st
}

func clearLoginFailures(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginFails, ip)
}

// buildMux registers every route. Tests drive it directly with httptest.
func buildMux(cfg *Config, st *Store) *http.ServeMux {
	mux := http.NewServeMux()
	registerPeopleRoutes(mux, cfg, st)

	mux.HandleFunc("GET /api/auth-info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"password": cfg.PasswordHash != "", "totp": cfg.TotpSecret != ""})
	})

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !checkLoginRate(ip) {
			writeJSON(w, 429, map[string]string{"error": "too many attempts"})
			return
		}
		var body struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		if !checkPassword(cfg.PasswordHash, body.Password) {
			noteLoginFailure(ip)
			writeJSON(w, 401, map[string]string{"error": "wrong password"})
			return
		}
		if cfg.TotpSecret != "" && !verifyTOTP(cfg.TotpSecret, body.Code) {
			noteLoginFailure(ip)
			writeJSON(w, 401, map[string]string{"error": "wrong code"})
			return
		}
		if cfg.SessionKey == "" {
			cfg.SessionKey = newSessionKey()
		}
		clearLoginFailures(ip)
		setSessionCookie(w, cfg)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		clearSessionCookie(w)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		scanMu.Lock()
		running := scanRunning
		scanMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "version": version, "scanning": running})
	})

	mux.HandleFunc("GET /api/home", func(w http.ResponseWriter, r *http.Request) {
		data, err := st.Home(profileOf(r))
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
		person := r.URL.Query().Get("person")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		items, total, err := st.ListFiltered(MediaFilter{Q: q, Tag: tag, Person: person, Sort: sort, Limit: limit, Offset: offset})
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
		people, _ := st.PeopleForMedia(m.ID)
		writeJSON(w, 200, map[string]any{"item": m, "people": people, "position_ms": st.Progress(m.ID, profileOf(r)), "favorite": st.IsFavorite(m.ID, profileOf(r))})
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
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", m.Title+filepath.Ext(m.Path)))
		} else {
			w.Header().Set("Content-Disposition", "inline")
		}
		http.ServeContent(w, r, m.Title, info.ModTime(), f)
	})

	mux.HandleFunc("GET /api/media/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if local := LocalPoster(m); local != "" {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			http.ServeFile(w, r, local)
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
		if err := st.SetProgress(r.PathValue("id"), profileOf(r), body.PositionMs); err != nil {
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
			_ = st.RebuildFTS()
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	var subsJobs sync.Map

	var visionJobs sync.Map

	var batchJobs sync.Map

	mux.HandleFunc("GET /api/suggestions", func(w http.ResponseWriter, r *http.Request) {
		items, err := st.PendingSuggestions(300)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := map[string]any{"items": items}
		if v, ok := batchJobs.Load("current"); ok {
			out["job"] = v
		}
		writeJSON(w, 200, out)
	})

	var batchCancel context.CancelFunc

	mux.HandleFunc("POST /api/ai/tag-batch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs    []string `json:"ids"`
			Folder string   `json:"folder"`
			Model  string   `json:"model"`
			Frames int      `json:"frames"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		ids := body.IDs
		if len(ids) == 0 && body.Folder != "" {
			ids, _ = st.MediaIDsUnder(body.Folder)
		}
		if len(ids) == 0 {
			writeJSON(w, 400, map[string]string{"error": "no media"})
			return
		}
		if _, busy := batchJobs.Load("current"); busy {
			writeJSON(w, 200, map[string]any{"started": false, "running": true})
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		batchCancel = cancel
		_ = st.StartJob("batch", "batch", "", len(ids))
		batchJobs.Store("current", map[string]any{"running": true, "total": len(ids), "done": 0})
		go func(ids []string) {
			done, failed := TagBatch(ctx, cfg, st, ids, body.Model, body.Frames, func(n int) {
				st.UpdateJob("batch", float64(n)/float64(len(ids)))
				batchJobs.Store("current", map[string]any{"running": true, "total": len(ids), "done": n})
			})
			state := "done"
			if ctx.Err() != nil {
				state = "cancelled"
			}
			st.FinishJob("batch", state, "")
			batchJobs.Store("current", map[string]any{"running": false, "total": len(ids), "done": done, "failed": failed})
		}(ids)
		writeJSON(w, 200, map[string]any{"started": true, "total": len(ids)})
	})

	mux.HandleFunc("GET /api/export", func(w http.ResponseWriter, r *http.Request) {
		b, err := st.ExportAll()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		data, _ := json.MarshalIndent(b, "", "  ")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=nuitelle-backup.json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(data)
	})

	mux.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		var b Backup
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		ms, ps, err := st.ImportAll(&b)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if b.Layout != "" {
			_ = st.SetLayout(b.Layout)
		}
		_ = st.RebuildFTS()
		writeJSON(w, 200, map[string]any{"media": ms, "playlists": ps})
	})

	mux.HandleFunc("POST /api/subtitles/batch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs    []string `json:"ids"`
			Target string   `json:"target"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || len(body.IDs) == 0 || body.Target == "" {
			writeJSON(w, 400, map[string]string{"error": "ids and target required"})
			return
		}
		st.StartJob("subtitles", "subtitles", "", len(body.IDs))
		go func() {
			done, failed := 0, 0
			for _, id := range body.IDs {
				if _, err := TranslateMediaSubtitle(cfg, st, id, body.Target); err != nil {
					failed++
				} else {
					done++
				}
				st.UpdateJob("subtitles", float64(done+failed)/float64(len(body.IDs)))
			}
			st.FinishJob("subtitles", "done", fmt.Sprintf("%d ok, %d failed", done, failed))
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	mux.HandleFunc("POST /api/transcode", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDs    []string `json:"ids"`
			Height int      `json:"height"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || len(body.IDs) == 0 {
			writeJSON(w, 400, map[string]string{"error": "ids required"})
			return
		}
		if body.Height == 0 {
			body.Height = 720
		}
		st.StartJob("transcode", "transcode", "", len(body.IDs))
		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done, failed := TranscodeBatch(ctx, cfg, st, body.IDs, body.Height, func(n int) {
				st.UpdateJob("transcode", float64(n)/float64(len(body.IDs)))
			})
			for _, root := range cfg.Roots {
				_, _ = ScanRoot(st, root, false)
			}
			_ = st.RebuildFTS()
			st.FinishJob("transcode", "done", fmt.Sprintf("%d ok, %d failed", done, failed))
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	mux.HandleFunc("POST /api/ingest", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil || body.URL == "" {
			writeJSON(w, 400, map[string]string{"error": "url required"})
			return
		}
		st.StartJob("ingest", "ingest", "", 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			path, err := IngestURL(ctx, cfg, body.URL)
			if err != nil {
				st.FinishJob("ingest", "error", err.Error())
				return
			}
			_, _ = ScanRoot(st, filepath.Dir(path), false)
			_ = st.RebuildFTS()
			st.FinishJob("ingest", "done", "")
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	mux.HandleFunc("POST /api/media/{id}/subtitle-fetch", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		saved := 0
		for _, p := range activePlugins {
			lang, srt, err := p.Subtitle(m.Title, m.Path)
			if err != nil || srt == "" {
				continue
			}
			if _, err := saveSubtitle(cfg, st, m.ID, lang, "plugin", srt, p.ID); err == nil {
				saved++
			}
		}
		writeJSON(w, 200, map[string]int{"saved": saved})
	})

	mux.HandleFunc("POST /api/media/{id}/metadata-fetch", func(w http.ResponseWriter, r *http.Request) {
		m, err := st.Get(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		applied := map[string]any{}
		for _, p := range activePlugins {
			title, tags, err := p.Metadata(m.Title, m.Path)
			if err != nil || (title == "" && len(tags) == 0) {
				continue
			}
			if title != "" && title != m.Title {
				_ = st.UpdateTitle(m.ID, title)
				applied["title"] = title
			}
			if len(tags) > 0 {
				_ = st.AddTags(m.ID, tags)
				applied["tags"] = tags
			}
		}
		updated, _ := st.Get(m.ID)
		writeJSON(w, 200, map[string]any{"applied": applied, "item": updated})
	})

	mux.HandleFunc("POST /api/media/{id}/favorite", func(w http.ResponseWriter, r *http.Request) {
		on, err := st.ToggleFavorite(r.PathValue("id"), profileOf(r))
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"favorite": on})
	})

	mux.HandleFunc("GET /api/media/{id}/markers", func(w http.ResponseWriter, r *http.Request) {
		ms, err := st.Markers(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"markers": ms})
	})

	mux.HandleFunc("POST /api/media/{id}/markers", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TMs   int64  `json:"t_ms"`
			Label string `json:"label"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		id, err := st.AddMarker(r.PathValue("id"), body.TMs, strings.TrimSpace(body.Label))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": id})
	})

	mux.HandleFunc("DELETE /api/markers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := st.DeleteMarker(r.PathValue("id")); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/favorites", func(w http.ResponseWriter, r *http.Request) {
		items, err := st.Favorites(profileOf(r), 500)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
	})

	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		ps, err := st.Profiles()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"profiles": ps})
	})

	mux.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		id, err := st.AddProfile(body.Name)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": id})
	})

	mux.HandleFunc("GET /api/plugins", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"plugins": activePlugins})
	})

	mux.HandleFunc("GET /plugin/{id}/{file...}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var dir string
		for _, p := range activePlugins {
			if p.ID == id {
				dir = p.Dir
				break
			}
		}
		if dir == "" {
			http.NotFound(w, r)
			return
		}
		rel := filepath.Clean("/" + r.PathValue("file"))
		full := filepath.Join(dir, rel)
		if !strings.HasPrefix(full, filepath.Clean(dir)+string(os.PathSeparator)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, full)
	})

	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := st.Jobs()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"jobs": jobs})
	})

	mux.HandleFunc("POST /api/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "batch" && batchCancel != nil {
			batchCancel()
			writeJSON(w, 200, map[string]bool{"cancelled": true})
			return
		}
		writeJSON(w, 404, map[string]string{"error": "unknown job"})
	})

	mux.HandleFunc("GET /api/media/{id}/suggestions", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		items, err := st.ListSuggestions(id)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := map[string]any{"items": items}
		if v, ok := visionJobs.Load(id); ok {
			out["job"] = v
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("POST /api/media/{id}/ai-tag", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := st.Get(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		var body struct {
			Model  string `json:"model"`
			Frames int    `json:"frames"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		if _, busy := visionJobs.Load(id); busy {
			writeJSON(w, 200, map[string]bool{"started": false, "running": true})
			return
		}
		visionJobs.Store(id, map[string]any{"running": true})
		go func() {
			_, err := TagMedia(cfg, st, id, body.Model, body.Frames)
			if err != nil {
				visionJobs.Store(id, map[string]any{"running": false, "error": err.Error()})
			} else {
				visionJobs.Store(id, map[string]any{"running": false})
			}
		}()
		writeJSON(w, 200, map[string]bool{"started": true})
	})

	mux.HandleFunc("POST /api/media/{id}/suggestions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Accept []string `json:"accept"`
			Reject []string `json:"reject"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body)
		id := r.PathValue("id")
		if len(body.Accept) > 0 {
			if err := st.AcceptSuggestions(id, body.Accept); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
		}
		if len(body.Reject) > 0 {
			_ = st.RejectSuggestions(id, body.Reject)
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/media/{id}/subtitles", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if m, err := st.Get(id); err == nil {
			DiscoverSidecars(st, m)
			DiscoverEmbedded(st, m)
		}
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
		var path string
		if sub.Kind == "embedded" {
			path, ok = EnsureEmbeddedSRT(cfg, st, sub)
		} else {
			path, ok = EnsureSubtitleVTT(cfg, sub)
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if strings.HasSuffix(strings.ToLower(path), ".vtt") {
			w.Write(b)
			return
		}
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
		EnsureHashes(st, 300)
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

	mux.HandleFunc("GET /api/library/missing", func(w http.ResponseWriter, r *http.Request) {
		all, err := st.AllMediaPaths()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var items []map[string]string
		for _, m := range all {
			if _, err := os.Stat(m.Path); err != nil {
				items = append(items, map[string]string{"id": m.ID, "title": m.Title})
				if len(items) >= 500 {
					break
				}
			}
		}
		writeJSON(w, 200, map[string]any{"count": len(items), "items": items})
	})

	mux.HandleFunc("POST /api/library/missing/cleanup", func(w http.ResponseWriter, r *http.Request) {
		all, err := st.AllMediaPaths()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		removed := 0
		for _, m := range all {
			if _, err := os.Stat(m.Path); err != nil {
				if st.RemoveMedia(m.ID) == nil {
					removed++
				}
			}
		}
		_ = st.RebuildFTS()
		writeJSON(w, 200, map[string]int{"removed": removed})
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
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(normalizeLayout(st.Layout())))
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
		var lay struct {
			Version int `json:"version"`
			Sidebar []struct {
				ID     string `json:"id"`
				Label  string `json:"label"`
				URL    string `json:"url"`
				Type   string `json:"type"`
				Order  int    `json:"order"`
				Hidden bool   `json:"hidden"`
			} `json:"sidebar"`
			Theme json.RawMessage `json:"theme"`
		}
		if err := json.Unmarshal(b, &lay); err != nil {
			writeJSON(w, 400, map[string]string{"error": "layout must be an object"})
			return
		}
		if len(lay.Sidebar) > 50 {
			writeJSON(w, 400, map[string]string{"error": "too many sidebar items"})
			return
		}
		for _, it := range lay.Sidebar {
			if len(it.ID) > 64 || len(it.Label) > 80 {
				writeJSON(w, 400, map[string]string{"error": "sidebar item too long"})
				return
			}
			if it.URL != "" {
				u, err := url.Parse(it.URL)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
					writeJSON(w, 400, map[string]string{"error": "link must be http(s)"})
					return
				}
			}
		}
		if lay.Version == 0 {
			lay.Version = 1
		}
		canon, _ := json.Marshal(lay)
		if err := st.SetLayout(string(canon)); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	return mux
}

// Serve runs the HTTP server until it stops.
func Serve(cfg *Config, st *Store) error {
	mux := buildMux(cfg, st)
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/" {
			if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
				r.URL.Path = "/"
			}
		}
		w.Header().Set("X-Robots-Tag", "noindex")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           securityMiddleware(cfg, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return srv.ListenAndServe()
}

// securityMiddleware blocks DNS rebinding and cross-site writes.
func securityMiddleware(cfg *Config, next http.Handler) http.Handler {
	hostName := func(h string) string {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return strings.ToLower(host)
		}
		return strings.ToLower(h)
	}
	loopback := func(h string) bool {
		switch hostName(h) {
		case "127.0.0.1", "localhost", "::1", "[::1]":
			return true
		}
		return false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopback(r.Host) && !cfg.AllowLAN {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			okOrigin := loopback(u.Host)
			if cfg.AllowLAN && hostName(u.Host) == hostName(r.Host) {
				okOrigin = true
			}
			if !okOrigin {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		protected := strings.HasPrefix(r.URL.Path, "/api/") &&
			r.URL.Path != "/api/health" && r.URL.Path != "/api/login" && r.URL.Path != "/api/logout" && r.URL.Path != "/api/auth-info"
		if protected && (cfg.Token != "" || cfg.PasswordHash != "") {
			authed := false
			if cfg.Token != "" {
				got := r.Header.Get("X-Nuitelle-Token")
				if got == "" {
					got = r.URL.Query().Get("token")
				}
				if subtle.ConstantTimeCompare([]byte(got), []byte(cfg.Token)) == 1 {
					authed = true
				}
			}
			if !authed && sessionCookie(r, cfg) {
				authed = true
			}
			if !authed {
				writeJSON(w, 401, map[string]string{"error": "authentication required"})
				return
			}
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			ct := r.Header.Get("Content-Type")
			if strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
				http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// normalizeLayout upgrades old layouts and keeps the sidebar usable.
func normalizeLayout(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return defaultLayout
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return defaultLayout
	}
	if _, ok := m["version"]; !ok {
		m["version"] = 1
	}
	var src []any
	if v, ok := m["sidebar"].([]any); ok {
		src = v
	}
	seen := map[string]bool{}
	var out []any
	for i, it := range src {
		obj, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := obj["id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if v, ok := obj["order"].(float64); !ok || v <= 0 {
			obj["order"] = (i + 1) * 10
		}
		out = append(out, obj)
	}
	if !seen["people"] {
		out = append(out, map[string]any{"id": "people", "label": "People", "order": 22})
	}
	if !seen["favorites"] {
		out = append(out, map[string]any{"id": "favorites", "label": "Favourites", "order": 25})
	}
	m["sidebar"] = out
	b, _ := json.Marshal(m)
	return string(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
		rel, err := filepath.Rel(r, real)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".." && !filepath.IsAbs(rel)) {
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
	dir := filepath.Join(root, ".nuitelle-trash")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().Unix(), filepath.Base(m.Path)))
	if err := os.Rename(m.Path, dst); err != nil {
		return "", err
	}
	return dst, nil
}
