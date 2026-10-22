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
	encoder := pickEncoder(bin, cfg.TranscodeEncoder)
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
	args := []string{"-v", "error", "-y", "-i", m.Path}
	switch encoder {
	case "videotoolbox":
		br := "800k"
		switch {
		case height >= 1080:
			br = "4M"
		case height >= 720:
			br = "2.5M"
		case height >= 480:
			br = "1.5M"
		}
		args = append(args, "-c:v", "h264_videotoolbox", "-b:v", br, "-vf", fmt.Sprintf("scale=-2:%d", height))
	case "vaapi":
		args = append(args, "-vaapi_device", "/dev/dri/renderD128",
			"-vf", fmt.Sprintf("format=nv12,hwupload,scale_vaapi=w=-2:h=%d", height),
			"-c:v", "h264_vaapi", "-qp", "22")
	case "nvenc":
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p5", "-cq", "23", "-vf", fmt.Sprintf("scale=-2:%d", height))
	default:
		args = append(args, "-c:v", "libx264", "-crf", "20", "-preset", "veryfast", "-vf", fmt.Sprintf("scale=-2:%d", height))
	}
	args = append(args, "-c:a", "aac", "-b:a", "160k", "-movflags", "+faststart", tmp)
	cmd := exec.CommandContext(ctx, bin, args...)
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

// pickEncoder resolves "auto" to the first hardware encoder ffmpeg reports.
func pickEncoder(bin, want string) string {
	has := func(name string) bool {
		out, _ := exec.Command(bin, "-hide_banner", "-encoders").Output()
		return strings.Contains(string(out), name)
	}
	if want != "" && want != "auto" {
		return want
	}
	if has("h264_videotoolbox") {
		return "videotoolbox"
	}
	if has("h264_nvenc") {
		return "nvenc"
	}
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil && has("h264_vaapi") {
		return "vaapi"
	}
	return "libx264"
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
