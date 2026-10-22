package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Subtitle is one generated track for a media item.
type Subtitle struct {
	MediaID   string `json:"-"`
	Lang      string `json:"lang"`
	Kind      string `json:"kind"` // transcript | translation
	Path      string `json:"-"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
}

type SubOptions struct {
	Model     string // tiny|base|small|medium|large-v3 or an absolute .bin path
	Lang      string // auto or ISO code
	Translate string // "" or target language
}

const subsSchema = `
CREATE TABLE IF NOT EXISTS subtitle(
  media_id TEXT NOT NULL,
  lang TEXT NOT NULL,
  kind TEXT NOT NULL,
  path TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY(media_id, lang, kind)
);
`

func (s *Store) AddSubtitle(sub Subtitle) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO subtitle(media_id,lang,kind,path,model,created_at) VALUES(?,?,?,?,?,?)
 ON CONFLICT(media_id,lang,kind) DO UPDATE SET path=excluded.path, model=excluded.model, created_at=excluded.created_at`,
		sub.MediaID, sub.Lang, sub.Kind, sub.Path, sub.Model, sub.CreatedAt)
	return err
}

func (s *Store) ListSubtitles(mediaID string) ([]Subtitle, error) {
	rows, err := s.db.Query(`SELECT media_id,lang,kind,path,model,created_at FROM subtitle WHERE media_id=? ORDER BY kind, lang`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subtitle
	for rows.Next() {
		var x Subtitle
		if rows.Scan(&x.MediaID, &x.Lang, &x.Kind, &x.Path, &x.Model, &x.CreatedAt) == nil {
			out = append(out, x)
		}
	}
	return out, rows.Err()
}

func (s *Store) GetSubtitle(mediaID, kind, lang string) (Subtitle, bool) {
	var x Subtitle
	err := s.db.QueryRow(`SELECT media_id,lang,kind,path,model,created_at FROM subtitle WHERE media_id=? AND kind=? AND lang=?`, mediaID, kind, lang).
		Scan(&x.MediaID, &x.Lang, &x.Kind, &x.Path, &x.Model, &x.CreatedAt)
	return x, err == nil
}

func whisperBinary(cfg *Config) (string, error) {
	if cfg.WhisperBin != "" {
		return cfg.WhisperBin, nil
	}
	for _, name := range []string{"whisper-cli", "whisper-cpp", "whisper"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("whisper-cli not found: install whisper.cpp or set whisper_bin")
}

func whisperModelPath(cfg *Config, model string) (string, error) {
	if model == "" {
		model = cfg.WhisperModel
	}
	if model == "" {
		model = "base"
	}
	if strings.ContainsRune(model, os.PathSeparator) {
		if _, err := os.Stat(model); err != nil {
			return "", err
		}
		return model, nil
	}
	dir := cfg.ModelDir
	if dir == "" {
		dir = filepath.Join(homeDir(), ".vela", "models")
	}
	p := filepath.Join(dir, "ggml-"+model+".bin")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("model %q not found at %s (download ggml-%s.bin)", model, p, model)
	}
	return p, nil
}

// GenerateSubtitles extracts audio, runs whisper.cpp, and stores SRT tracks.
// Translation to English uses whisper's --translate; other targets use a local
// OpenAI-compatible LLM (config translator_url / translator_model).
func GenerateSubtitles(cfg *Config, st *Store, mediaID string, opt SubOptions) ([]Subtitle, error) {
	m, err := st.Get(mediaID)
	if err != nil {
		return nil, fmt.Errorf("media not found")
	}
	bin, err := whisperBinary(cfg)
	if err != nil {
		return nil, err
	}
	modelPath, err := whisperModelPath(cfg, opt.Model)
	if err != nil {
		return nil, err
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg required for subtitle generation")
	}
	if opt.Lang == "" {
		opt.Lang = "auto"
	}

	work, err := os.MkdirTemp("", "vela-subs-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	wav := filepath.Join(work, "audio.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-y", "-i", m.Path, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("extract audio: %v: %s", err, truncate(string(out), 200))
	}

	prefix := filepath.Join(work, "out")
	args := []string{"-m", modelPath, "-f", wav, "-osrt", "-of", prefix, "-l", opt.Lang, "-np"}
	stderr, err := runWhisper(ctx, bin, args)
	if err != nil {
		return nil, err
	}
	lang := opt.Lang
	if m := regexp.MustCompile(`auto-detected language: (\w+)`).FindStringSubmatch(stderr); len(m) == 2 {
		lang = m[1]
	}
	srtPath := prefix + ".srt"
	body, err := os.ReadFile(srtPath)
	if err != nil {
		return nil, fmt.Errorf("whisper produced no subtitle: %s", truncate(stderr, 300))
	}
	first, err := saveSubtitle(cfg, st, m.ID, lang, "transcript", string(body), filepath.Base(modelPath))
	if err != nil {
		return nil, err
	}
	subs := []Subtitle{first}

	if opt.Translate != "" {
		if opt.Translate == "en" {
			enPrefix := filepath.Join(work, "en")
			args := []string{"-m", modelPath, "-f", wav, "-osrt", "-of", enPrefix, "-l", lang, "--translate", "-np"}
			if _, err := runWhisper(ctx, bin, args); err == nil {
				if b, err := os.ReadFile(enPrefix + ".srt"); err == nil {
					if s, err := saveSubtitle(cfg, st, m.ID, "en", "translation", string(b), filepath.Base(modelPath)); err == nil {
						subs = append(subs, s)
					}
				}
			}
		} else {
			translated, err := translateSRT(cfg, string(body), opt.Translate)
			if err != nil {
				return subs, fmt.Errorf("translate: %w", err)
			}
			if s, err := saveSubtitle(cfg, st, m.ID, opt.Translate, "translation", translated, cfg.TranslatorModel); err == nil {
				subs = append(subs, s)
			}
		}
	}
	return subs, nil
}

func runWhisper(ctx context.Context, bin string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var errBuf bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return errBuf.String(), fmt.Errorf("whisper: %v: %s", err, truncate(errBuf.String(), 300))
	}
	return errBuf.String(), nil
}

func saveSubtitle(cfg *Config, st *Store, mediaID, lang, kind, body, model string) (Subtitle, error) {
	dir := filepath.Join(cfg.CoverDir, "subs")
	if cfg.CoverDir == "" {
		dir = filepath.Join(homeDir(), ".vela", "subs")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Subtitle{}, err
	}
	path := filepath.Join(dir, mediaID+"."+kind+"."+lang+".srt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return Subtitle{}, err
	}
	sub := Subtitle{MediaID: mediaID, Lang: lang, Kind: kind, Path: path, Model: model, CreatedAt: time.Now().Unix()}
	if err := st.AddSubtitle(sub); err != nil {
		return Subtitle{}, err
	}
	return sub, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- SRT → VTT ---

var srtTime = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)

func srtToVTT(srt string) string {
	s := strings.ReplaceAll(srt, "\r\n", "\n")
	s = srtTime.ReplaceAllString(s, "$1.$2")
	// Drop numeric cue indices (keep the timing line and text).
	lines := strings.Split(s, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			out = append(out, "")
			continue
		}
		if _, err := strconv.Atoi(l); err == nil && i+1 < len(lines) && strings.Contains(lines[i+1], "-->") {
			continue
		}
		out = append(out, lines[i])
	}
	return "WEBVTT\n\n" + strings.Join(out, "\n")
}

// --- SRT translation via a local OpenAI-compatible endpoint ---

type cue struct {
	Timing string
	Lines  []string
}

func parseSRT(s string) []cue {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []cue
	for _, block := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(s), -1) {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 2 {
			continue
		}
		i := 0
		if !strings.Contains(lines[0], "-->") {
			i = 1
		}
		if i >= len(lines) || !strings.Contains(lines[i], "-->") {
			continue
		}
		out = append(out, cue{Timing: strings.TrimSpace(lines[i]), Lines: lines[i+1:]})
	}
	return out
}

func renderSRT(cues []cue) string {
	var b strings.Builder
	for i, c := range cues {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString("\n")
		b.WriteString(c.Timing)
		b.WriteString("\n")
		b.WriteString(strings.Join(c.Lines, "\n"))
		b.WriteString("\n\n")
	}
	return b.String()
}

func translateSRT(cfg *Config, srt, target string) (string, error) {
	cues := parseSRT(srt)
	if len(cues) == 0 {
		return "", fmt.Errorf("no cues parsed")
	}
	url := cfg.TranslatorURL
	if url == "" {
		url = "http://127.0.0.1:11434/v1"
	}
	model := cfg.TranslatorModel
	if model == "" {
		model = "qwen2.5:7b"
	}
	client := &http.Client{Timeout: 180 * time.Second}
	for start := 0; start < len(cues); start += 20 {
		end := start + 20
		if end > len(cues) {
			end = len(cues)
		}
		var texts []string
		for _, c := range cues[start:end] {
			texts = append(texts, strings.Join(c.Lines, " "))
		}
		payload := map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": "You translate subtitle lines. Reply with JSON only: {\"lines\":[\"...\"]} in the same order. Keep meaning and tone; no notes."},
				{"role": "user", "content": fmt.Sprintf("Translate to %s:\n%s", target, mustJSON(texts))},
			},
			"temperature": 0,
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", strings.TrimRight(url, "/")+"/chat/completions", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		if cfg.TranslatorKey != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.TranslatorKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("translator: %w", err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("translator HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &out); err != nil || len(out.Choices) == 0 {
			return "", fmt.Errorf("translator response invalid")
		}
		content := out.Choices[0].Message.Content
		var parsed struct {
			Lines []string `json:"lines"`
		}
		if err := json.Unmarshal([]byte(extractJSON(content)), &parsed); err != nil {
			return "", fmt.Errorf("translator returned non-JSON: %s", truncate(content, 160))
		}
		for i := range parsed.Lines {
			idx := start + i
			if idx < len(cues) && parsed.Lines[i] != "" {
				cues[idx].Lines = []string{parsed.Lines[i]}
			}
		}
	}
	return renderSRT(cues), nil
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func extractJSON(s string) string {
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}
