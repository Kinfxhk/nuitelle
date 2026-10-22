package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TranscodeOne writes a smaller H.264/AAC copy into <root>/_transcoded and
// returns the new path. The original is left untouched.
func TranscodeOne(ctx context.Context, cfg *Config, st *Store, id string, height int) (string, error) {
	m, err := st.Get(id)
	if err != nil {
		return "", fmt.Errorf("media not found")
	}
	if height < 144 || height > 2160 {
		height = 720
	}
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg required")
	}
	var root string
	for _, r := range cfg.Roots {
		if strings.HasPrefix(m.Path, r+string(os.PathSeparator)) {
			root = r
			break
		}
	}
	if root == "" {
		return "", fmt.Errorf("file outside roots")
	}
	dir := filepath.Join(root, "_transcoded")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(m.Path), filepath.Ext(m.Path))
	out := filepath.Join(dir, fmt.Sprintf("%s.%dp.mp4", base, height))
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	tmp := out + ".part.mp4"
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-y", "-i", m.Path,
		"-vf", fmt.Sprintf("scale=-2:%d", height),
		"-c:v", "libx264", "-crf", "20", "-preset", "veryfast",
		"-c:a", "aac", "-b:a", "160k", "-movflags", "+faststart", tmp)
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg transcode failed")
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return out, nil
}

// TranscodeBatch transcodes ids sequentially and reports progress.
func TranscodeBatch(ctx context.Context, cfg *Config, st *Store, ids []string, height int, onDone func(int)) (done, failed int) {
	for _, id := range ids {
		select {
		case <-ctx.Done():
			return done, failed
		default:
		}
		tctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
		_, err := TranscodeOne(tctx, cfg, st, id, height)
		cancel()
		if err != nil {
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
