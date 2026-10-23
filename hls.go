package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var hlsJobs sync.Map

// HLSDir is the cache directory for one media item.
func HLSDir(cfg *Config, id string, height int) string {
	if height > 0 {
		return filepath.Join(cfg.CoverDir, "hls", fmt.Sprintf("%s_%dp", id, height))
	}
	return filepath.Join(cfg.CoverDir, "hls", id)
}

// StartHLS kicks off an HLS transcode if needed and returns the playlist path.
func StartHLS(cfg *Config, st *Store, id string, height int) (string, error) {
	m, err := st.Get(id)
	if err != nil {
		return "", fmt.Errorf("media not found")
	}
	dir := HLSDir(cfg, id, height)
	playlist := filepath.Join(dir, "index.m3u8")
	if _, err := os.Stat(playlist); err == nil {
		return playlist, nil
	}
	jobKey := fmt.Sprintf("%s:%d", id, height)
	if _, loaded := hlsJobs.LoadOrStore(jobKey, true); !loaded {
		go func() {
			defer hlsJobs.Delete(jobKey)
			_ = os.MkdirAll(dir, 0o755)
			bin, err := exec.LookPath("ffmpeg")
			if err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
			defer cancel()
			args := []string{"-v", "error", "-y", "-i", m.Path}
			if height > 0 {
				args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", height))
			}
			args = append(args,
				"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
				"-c:a", "aac", "-b:a", "128k",
				"-f", "hls", "-hls_time", "4", "-hls_playlist_type", "event",
				"-hls_segment_type", "fmp4", "-hls_flags", "independent_segments",
				"-hls_segment_filename", filepath.Join(dir, "seg%05d.m4s"),
				playlist)
			cmd := exec.CommandContext(ctx, bin, args...)
			_ = cmd.Run()
		}()
	}
	return playlist, nil
}

// WaitForHLS waits until the playlist exists (or timeout).
func WaitForHLS(playlist string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(playlist); err == nil {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// ServeHLSPath resolves a file inside the HLS cache.
func ServeHLSPath(cfg *Config, id, name string, height int) (string, bool) {
	dir := filepath.Clean(HLSDir(cfg, id, height))
	full := filepath.Join(dir, filepath.Clean("/"+name))
	if !strings.HasPrefix(full, dir+string(os.PathSeparator)) {
		return "", false
	}
	if _, err := os.Stat(full); err != nil {
		return "", false
	}
	return full, true
}
