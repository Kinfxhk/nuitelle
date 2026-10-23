package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// StartWatcher rescans the library a few seconds after files change.
func StartWatcher(ctx context.Context, cfg *Config, st *Store) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	addTree := func(root string) {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			base := d.Name()
			if path != root && (strings.HasPrefix(base, ".") || base == "_transcoded" || base == ".vela-trash") {
				return filepath.SkipDir
			}
			_ = w.Add(path)
			return nil
		})
	}
	for _, root := range cfg.Roots {
		addTree(root)
	}
	defer w.Close()

	var mu sync.Mutex
	var timer *time.Timer
	schedule := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(3*time.Second, func() {
			log.Print("watch: rescanning")
			scan := func() {
				for _, root := range cfg.Roots {
					_, _ = ScanRoot(st, root, false)
				}
				_ = st.RebuildFTS()
			}
			scan()
			// A second pass catches files that were still being copied.
			time.AfterFunc(12*time.Second, scan)
		})
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			if strings.HasSuffix(strings.ToLower(ev.Name), ".part") || strings.Contains(ev.Name, ".part.mp4") {
				continue
			}
			if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
				addTree(ev.Name)
			}
			ext := strings.ToLower(filepath.Ext(ev.Name))
			if videoExt[ext] || strings.HasSuffix(strings.ToLower(ev.Name), ".srt") || strings.HasSuffix(strings.ToLower(ev.Name), ".vtt") {
				schedule()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("watch: %v", err)
		}
	}
}
