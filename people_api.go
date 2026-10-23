package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// registerPeopleRoutes wires the People API. It lives in its own file so the
// main server table stays readable.
func registerPeopleRoutes(mux *http.ServeMux, cfg *Config, st *Store) {
	mux.HandleFunc("GET /api/people", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		sortBy := r.URL.Query().Get("sort")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		items, total, err := st.ListPeople(q, sortBy, limit, offset)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": total})
	})

	mux.HandleFunc("POST /api/people", func(w http.ResponseWriter, r *http.Request) {
		var in PersonInput
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		p, err := st.CreatePerson(in)
		if errors.Is(err, ErrPersonExists) {
			writeJSON(w, 409, map[string]any{"error": "person already exists", "item": p})
			return
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, map[string]any{"item": p})
	})

	mux.HandleFunc("GET /api/people/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := st.GetPerson(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		items, _, err := st.ListFiltered(MediaFilter{Person: p.ID, Sort: r.URL.Query().Get("sort"), Limit: 24})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"item": p, "media": items})
	})

	mux.HandleFunc("PATCH /api/people/{id}", func(w http.ResponseWriter, r *http.Request) {
		var patch PersonPatch
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&patch); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		p, err := st.UpdatePerson(r.PathValue("id"), patch)
		if errors.Is(err, ErrPersonExists) {
			writeJSON(w, 409, map[string]any{"error": "another person has that name", "item": p})
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"item": p})
	})

	mux.HandleFunc("DELETE /api/people/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		p, err := st.GetPerson(id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		if err := st.DeletePerson(id); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		removePersonImages(cfg.PeopleDir, p)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/people/{id}/image", func(w http.ResponseWriter, r *http.Request) {
		p, err := st.GetPerson(r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		data, code, msg := readPersonImageRequest(w, r)
		if msg != "" {
			writeJSON(w, code, map[string]string{"error": msg})
			return
		}
		imageName, thumbName, err := savePersonImageFiles(cfg.PeopleDir, p.ID, data)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := st.SetPersonImage(p.ID, imageName, thumbName); err != nil {
			removePersonImages(cfg.PeopleDir, Person{Image: imageName, Thumb: thumbName})
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Drop the files this upload replaced.
		for _, n := range []string{p.Image, p.Thumb} {
			if n == "" || n == imageName || n == thumbName {
				continue
			}
			if path, ok := personImageFile(cfg.PeopleDir, n); ok {
				os.Remove(path)
			}
		}
		fresh, _ := st.GetPerson(p.ID)
		writeJSON(w, 200, map[string]any{"item": fresh})
	})

	mux.HandleFunc("GET /api/people/{id}/image", func(w http.ResponseWriter, r *http.Request) {
		p, err := st.GetPerson(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name := p.Image
		if r.URL.Query().Get("size") == "thumb" && p.Thumb != "" {
			name = p.Thumb
		}
		path, ok := personImageFile(cfg.PeopleDir, name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(path); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeFile(w, r, path)
	})

	mux.HandleFunc("POST /api/media/{id}/people", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, err := st.Get(id); err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		var body struct {
			Add    []string `json:"add"`
			Remove []string `json:"remove"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		people, err := st.AssignPeople(id, body.Add, body.Remove)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"people": people})
	})
}

// readPersonImageRequest accepts either a multipart upload or a local path.
func readPersonImageRequest(w http.ResponseWriter, r *http.Request) ([]byte, int, string) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxPersonUploadBytes+(1<<20))
		if err := r.ParseMultipartForm(maxPersonUploadBytes + (1 << 20)); err != nil {
			return nil, 400, "malformed upload (max 15 MB)"
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, 400, "file field required"
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxPersonUploadBytes+1))
		if err != nil {
			return nil, 400, "cannot read upload"
		}
		if len(data) > maxPersonUploadBytes {
			return nil, 413, "image larger than 15 MB"
		}
		if len(data) == 0 {
			return nil, 400, "empty upload"
		}
		return data, 200, ""
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		return nil, 400, "bad json"
	}
	data, err := readPersonImageFile(body.Path)
	if err != nil {
		return nil, 400, err.Error()
	}
	return data, 200, ""
}
