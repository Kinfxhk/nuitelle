package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Suggestion struct {
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
	Model      string  `json:"model"`
}

const suggestionSchema = `
CREATE TABLE IF NOT EXISTS tag_suggestion(
  media_id TEXT NOT NULL,
  name TEXT NOT NULL,
  confidence REAL NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY(media_id, name)
);
`

func (s *Store) AddSuggestions(mediaID string, tags []Suggestion, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range tags {
		name := s.ResolveTag(t.Name)
		if name == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO tag_suggestion(media_id,name,confidence,model,created_at) VALUES(?,?,?,?,?)
 ON CONFLICT(media_id,name) DO UPDATE SET confidence=excluded.confidence, model=excluded.model, created_at=excluded.created_at`,
			mediaID, name, t.Confidence, model, time.Now().Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSuggestions(mediaID string) ([]Suggestion, error) {
	rows, err := s.db.Query(`SELECT name,confidence,model FROM tag_suggestion WHERE media_id=? ORDER BY confidence DESC`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Suggestion
	for rows.Next() {
		var x Suggestion
		if rows.Scan(&x.Name, &x.Confidence, &x.Model) == nil {
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

func (s *Store) AcceptSuggestions(mediaID string, names []string) error {
	if err := s.AddTags(mediaID, names); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		if _, err := s.db.Exec(`DELETE FROM tag_suggestion WHERE media_id=? AND name=?`, mediaID, n); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RejectSuggestions(mediaID string, names []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range names {
		if _, err := s.db.Exec(`DELETE FROM tag_suggestion WHERE media_id=? AND name=?`, mediaID, n); err != nil {
			return err
		}
	}
	return nil
}

var genericTags = map[string]bool{
	"image": true, "photo": true, "object": true, "objects": true, "setting": true,
	"shot": true, "people": true, "person": true, "color": true, "colour": true,
	"scene": true, "picture": true, "background": true, "foreground": true,
	"tag": true, "description": true, "visual": true, "content": true,
}

// TagMedia extracts frames and asks a local vision model for tags.
func TagMedia(cfg *Config, st *Store, mediaID string, model string, frames int) ([]Suggestion, error) {
	m, err := st.Get(mediaID)
	if err != nil {
		return nil, fmt.Errorf("media not found")
	}
	url := cfg.VisionURL
	if url == "" {
		url = "http://127.0.0.1:8100/v1"
	}
	if model == "" {
		model = cfg.VisionModel
	}
	if model == "" {
		model = "qwen3-vl-4b"
	}
	if frames <= 0 || frames > 8 {
		frames = 3
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg required for AI tagging")
	}
	dir, err := os.MkdirTemp("", "nuitelle-vision-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	dur := float64(m.DurationMs) / 1000
	points := make([]float64, frames)
	for i := range points {
		points[i] = dur * float64(i+1) / float64(frames+1)
	}
	agg := map[string]float64{}
	for i, at := range points {
		out := filepath.Join(dir, fmt.Sprintf("f%d.jpg", i))
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-ss", fmt.Sprintf("%.2f", at), "-i", m.Path,
			"-frames:v", "1", "-vf", "scale=448:-2", "-q:v", "4", out)
		err := cmd.Run()
		cancel()
		if err != nil {
			continue
		}
		tags, err := visionTags(url, model, out)
		if err != nil {
			continue
		}
		for _, t := range tags {
			if t.Confidence > agg[t.Name] {
				agg[t.Name] = t.Confidence
			}
		}
	}
	if len(agg) == 0 {
		return nil, fmt.Errorf("vision model returned no tags (is %s running?)", url)
	}
	var out []Suggestion
	for name, conf := range agg {
		out = append(out, Suggestion{Name: name, Confidence: conf, Model: model})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > 8 {
		out = out[:8]
	}
	if err := st.AddSuggestions(m.ID, out, model); err != nil {
		return nil, err
	}
	return out, nil
}

func visionTags(baseURL, model, imagePath string) ([]Suggestion, error) {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, err
	}
	data := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
	prompt := `Label this frame for a personal media library. Reply with JSON only:
{"tags":[{"name":"short_tag","confidence":0.0}]}
Rules: 3-6 tags, concrete visual words (indoor, outdoor, closeup, wide, solo, two-people, night, day, red, blue, text, animation, landscape, food, animal, vehicle, building). Never use abstract words: image, photo, object, setting, shot, people, colour, color, scene.`
	payload := map[string]any{
		"model": model,
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": prompt},
				map[string]any{"type": "image_url", "image_url": map[string]string{"url": data}},
			},
		}},
		"temperature": 0,
		"max_tokens":  220,
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(strings.TrimRight(baseURL, "/")+"/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vision HTTP %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return nil, fmt.Errorf("vision response invalid")
	}
	var parsed struct {
		Tags []struct {
			Name       string  `json:"name"`
			Confidence float64 `json:"confidence"`
		} `json:"tags"`
	}
	if err := json.Unmarshal([]byte(extractJSON(out.Choices[0].Message.Content)), &parsed); err != nil {
		return nil, err
	}
	var tags []Suggestion
	seen := map[string]bool{}
	for _, t := range parsed.Tags {
		name := normalizeTag(t.Name)
		if name == "" || genericTags[name] || seen[name] {
			continue
		}
		if t.Confidence > 0 && t.Confidence < 0.10 {
			continue
		}
		seen[name] = true
		conf := t.Confidence
		if conf <= 0 || conf > 1 {
			conf = 0.5
		}
		tags = append(tags, Suggestion{Name: name, Confidence: conf})
	}
	return tags, nil
}

var nonTag = regexp.MustCompile(`[^a-z0-9-]+`)

func normalizeTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = nonTag.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// PendingSuggestion is one row in the review queue.
type PendingSuggestion struct {
	MediaID    string  `json:"media_id"`
	Title      string  `json:"title"`
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence"`
}

func (s *Store) PendingSuggestions(limit int) ([]PendingSuggestion, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT g.media_id, m.title, g.name, g.confidence
 FROM tag_suggestion g JOIN media m ON m.id=g.media_id
 WHERE m.trashed=0 ORDER BY g.confidence DESC, m.title LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingSuggestion
	for rows.Next() {
		var x PendingSuggestion
		if rows.Scan(&x.MediaID, &x.Title, &x.Name, &x.Confidence) == nil {
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

func (s *Store) MediaIDsUnder(prefix string) ([]string, error) {
	prefix = strings.TrimRight(prefix, "/")
	rows, err := s.db.Query(`SELECT id FROM media WHERE trashed=0 AND path LIKE ? ESCAPE '\' ORDER BY path`, escLike(prefix)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// TagBatch tags many items sequentially and stops when ctx is cancelled.
func TagBatch(ctx context.Context, cfg *Config, st *Store, ids []string, model string, frames int, onDone func(int)) (done int, failed int) {
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return done, failed
		default:
		}
		if _, err := TagMedia(cfg, st, id, model, frames); err != nil {
			failed++
			continue
		}
		done++
		if onDone != nil {
			onDone(done)
		}
	}
	return done, failed
}
