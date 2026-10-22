package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"time"
)

// computePHash returns a 64-bit average hash of the first frame, as 16 hex chars.
func computePHash(path string) (string, error) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-v", "error", "-ss", "0", "-i", path,
		"-frames:v", "1", "-vf", "scale=16:16,format=gray", "-f", "rawvideo", "-pix_fmt", "gray", "-")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	b := out.Bytes()
	if len(b) < 256 {
		return "", fmt.Errorf("short frame")
	}
	b = b[:256]
	sum := 0
	for _, v := range b {
		sum += int(v)
	}
	avg := sum / 256
	var packed [32]byte
	for i, v := range b {
		if int(v) >= avg {
			packed[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	return hex.EncodeToString(packed[:]), nil
}

// EnsureHashes fills in missing perceptual hashes, up to limit items.
func EnsureHashes(st *Store, limit int) int {
	rows, err := st.db.Query(`SELECT id,path FROM media WHERE phash='' AND trashed=0 LIMIT ?`, limit)
	if err != nil {
		return 0
	}
	type item struct{ id, path string }
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.path) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	n := 0
	for _, it := range items {
		h, err := computePHash(it.path)
		if err != nil || h == "" {
			continue
		}
		_ = st.SetPHash(it.id, h)
		n++
	}
	return n
}
