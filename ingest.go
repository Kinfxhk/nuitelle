package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxIngestBytes = 50 << 30 // 50 GiB safety cap

// IngestURL downloads one URL into <root>/_ingest and returns the local path.
// HLS playlists are remuxed with ffmpeg; direct files are streamed as-is.
func IngestURL(ctx context.Context, cfg *Config, rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("only http(s) URLs are supported")
	}
	if len(cfg.Roots) == 0 {
		return "", fmt.Errorf("no library root")
	}
	dir := filepath.Join(cfg.Roots[0], "_ingest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := sanitizeName(filepath.Base(u.Path))
	if name == "" || name == "." || name == "/" {
		name = fmt.Sprintf("ingest-%d.mp4", time.Now().Unix())
	}
	out := filepath.Join(dir, name)

	if strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") {
		bin, err := exec.LookPath("ffmpeg")
		if err != nil {
			return "", fmt.Errorf("HLS needs ffmpeg")
		}
		out = strings.TrimSuffix(out, ".m3u8") + ".mp4"
		cmd := exec.CommandContext(ctx, bin, "-v", "error", "-y", "-i", rawURL, "-c", "copy", out)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("ffmpeg: %w", err)
		}
		return out, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Vela/1 ingest")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := out + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxIngestBytes+1))
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if n > maxIngestBytes {
		os.Remove(tmp)
		return "", fmt.Errorf("file exceeds 50 GiB cap")
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return out, nil
}

func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "..", "_")
	if len(s) > 120 {
		s = s[len(s)-120:]
	}
	return s
}
