package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiscoverSidecars registers subtitles that sit next to the media file:
// name.srt, name.en.srt, name.zh-Hant.vtt, name.ass ...
func DiscoverSidecars(st *Store, m Media) {
	stem := strings.TrimSuffix(m.Path, filepath.Ext(m.Path))
	dir := filepath.Dir(m.Path)
	base := filepath.Base(stem)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, base+".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".srt" && ext != ".vtt" && ext != ".ass" && ext != ".ssa" {
			continue
		}
		lang := strings.TrimSuffix(strings.TrimPrefix(name, base+"."), filepath.Ext(name))
		lang = strings.TrimPrefix(lang, ".")
		if lang == "" {
			lang = "und"
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		_ = st.AddSubtitle(Subtitle{
			MediaID: m.ID, Lang: strings.ToLower(lang), Kind: "external",
			Path: full, Model: "sidecar", CreatedAt: info.ModTime().Unix(),
		})
	}
}

// EnsureSubtitleVTT converts non-VTT sidecars into cached SRT so the same
// WebVTT path can serve everything.
func EnsureSubtitleVTT(cfg *Config, sub Subtitle) (string, bool) {
	ext := strings.ToLower(filepath.Ext(sub.Path))
	switch ext {
	case ".vtt":
		return sub.Path, true
	case ".srt":
		return sub.Path, true
	case ".ass", ".ssa":
		dir := filepath.Join(cfg.CoverDir, "subs")
		_ = os.MkdirAll(dir, 0o700)
		out := filepath.Join(dir, sub.MediaID+"."+sub.Lang+".srt")
		if _, err := os.Stat(out); err == nil {
			return out, true
		}
		if _, err := runFFmpegConvert(sub.Path, out); err != nil {
			return "", false
		}
		return out, true
	}
	return "", false
}

func runFFmpegConvert(in, out string) (string, error) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, "-v", "error", "-y", "-i", in, out)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out, nil
}
