package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type probeStreams struct {
	Streams []struct {
		Index     int    `json:"index"`
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Tags      struct {
			Language string `json:"language"`
			Title    string `json:"title"`
		} `json:"tags"`
	} `json:"streams"`
}

// DiscoverEmbedded registers subtitle streams inside the container.
func DiscoverEmbedded(st *Store, m Media) {
	bin, err := exec.LookPath("ffprobe")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-v", "error", "-print_format", "json", "-show_streams", m.Path).Output()
	if err != nil {
		return
	}
	var p probeStreams
	if json.Unmarshal(out, &p) != nil {
		return
	}
	n := 0
	for _, s := range p.Streams {
		if s.CodecType != "subtitle" {
			continue
		}
		lang := strings.ToLower(strings.TrimSpace(s.Tags.Language))
		if lang == "" || lang == "und" {
			lang = "und"
		}
		_ = st.AddSubtitle(Subtitle{
			MediaID: m.ID, Lang: lang, Kind: "embedded", Path: "",
			Model: fmt.Sprintf("embedded:%d", n), CreatedAt: time.Now().Unix(),
		})
		n++
	}
}

// EnsureEmbeddedSRT extracts one subtitle stream to a cached SRT file.
func EnsureEmbeddedSRT(cfg *Config, st *Store, sub Subtitle) (string, bool) {
	parts := strings.SplitN(sub.Model, ":", 2)
	if len(parts) != 2 {
		return "", false
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", false
	}
	m, err := st.Get(sub.MediaID)
	if err != nil {
		return "", false
	}
	dir := filepath.Join(cfg.CoverDir, "subs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false
	}
	out := filepath.Join(dir, fmt.Sprintf("%s.embedded%d.%s.srt", sub.MediaID, idx, sub.Lang))
	if _, err := os.Stat(out); err == nil {
		return out, true
	}
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-y", "-i", m.Path,
		"-map", fmt.Sprintf("0:s:%d", idx), "-c:s", "srt", out)
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return out, true
}
