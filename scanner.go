package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var videoExt = map[string]bool{
	".mp4": true, ".m4v": true, ".mkv": true, ".webm": true,
	".mov": true, ".avi": true, ".ts": true, ".m2ts": true, ".wmv": true,
}

// ScanRoot indexes one folder. Unchanged files are skipped unless full is set.
func ScanRoot(st *Store, root string, full bool) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, keep walking
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "_trash" || name == ".vela-trash") {
				return filepath.SkipDir
			}
			return nil
		}
		if !videoExt[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Skip files still being copied: sparse or zero-length.
		if info.Size() == 0 {
			return nil
		}
		if old, ok := st.Existing(path); ok && !full && old.MtimeNS == info.ModTime().UnixNano() && old.Size == info.Size() {
			return nil
		}
		m := Media{
			ID:      mediaID(path),
			Path:    path,
			Title:   titleOf(path),
			Size:    info.Size(),
			MtimeNS: info.ModTime().UnixNano(),
			AddedAt: time.Now().Unix(),
		}
		probe(&m)
		if err := st.Upsert(m); err != nil {
			return nil
		}
		ApplyPlugins(st, m.ID, m.Title, m.Path)
		count++
		return nil
	})
	return count, err
}

func mediaID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

func titleOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

type probeResult struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

func probe(m *Media) {
	bin, err := exec.LookPath("ffprobe")
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", m.Path).Output()
	if err != nil {
		return
	}
	var pr probeResult
	if json.Unmarshal(out, &pr) != nil {
		return
	}
	if f, err := strconv.ParseFloat(pr.Format.Duration, 64); err == nil {
		m.DurationMs = int64(f * 1000)
	}
	for _, s := range pr.Streams {
		switch s.CodecType {
		case "video":
			if m.Vcodec == "" {
				m.Vcodec = s.CodecName
				m.Width, m.Height = s.Width, s.Height
			}
		case "audio":
			if m.Acodec == "" {
				m.Acodec = s.CodecName
			}
		}
	}
}
