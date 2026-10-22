package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// Plugin is a small JavaScript bundle with a manifest. Scripts run in a bare
// goja VM: no filesystem, no network, no timers. Only the hook argument and
// return value cross the boundary.
type Plugin struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Hooks   []string `json:"hooks"`
	UI      string   `json:"ui"`
	UITitle string   `json:"ui_title"`
	Dir     string   `json:"-"`
	Script  string   `json:"-"`
}

var activePlugins []Plugin

func LoadPlugins(dir string) ([]Plugin, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // missing folder is fine
	}
	var out []Plugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pdir := filepath.Join(dir, e.Name())
		mb, err := os.ReadFile(filepath.Join(pdir, "manifest.json"))
		if err != nil {
			continue
		}
		var p Plugin
		if json.Unmarshal(mb, &p) != nil || p.ID == "" {
			continue
		}
		js, err := os.ReadFile(filepath.Join(pdir, "plugin.js"))
		if err != nil && p.UI == "" {
			continue
		}
		p.Dir, p.Script = pdir, string(js)
		out = append(out, p)
	}
	return out, nil
}

func SetPlugins(ps []Plugin) { activePlugins = ps }

func (p Plugin) hasHook(name string) bool {
	for _, h := range p.Hooks {
		if h == name {
			return true
		}
	}
	return false
}

// Classify runs scan.classify and returns normalised tags.
func (p Plugin) Classify(title, path string) (tags []string, err error) {
	if !p.hasHook("scan.classify") {
		return nil, nil
	}
	vm := goja.New()
	timer := time.AfterFunc(2*time.Second, func() { vm.Interrupt("timeout") })
	defer timer.Stop()
	if _, err := vm.RunString(p.Script); err != nil {
		return nil, err
	}
	fn, ok := goja.AssertFunction(vm.Get("scanClassify"))
	if !ok {
		return nil, fmt.Errorf("scanClassify missing")
	}
	res, err := fn(goja.Undefined(), vm.ToValue(map[string]any{"title": title, "path": path}))
	if err != nil {
		return nil, err
	}
	var out struct {
		Tags []string `json:"tags"`
	}
	b, _ := json.Marshal(res.Export())
	if json.Unmarshal(b, &out) != nil {
		return nil, fmt.Errorf("bad result")
	}
	seen := map[string]bool{}
	for _, t := range out.Tags {
		n := normalizeTag(t)
		if n == "" || seen[n] || genericTags[n] {
			continue
		}
		seen[n] = true
		tags = append(tags, n)
		if len(tags) >= 10 {
			break
		}
	}
	return tags, nil
}

// ApplyPlugins tags one scanned item using every plugin with the scan hook.
func ApplyPlugins(st *Store, mediaID, title, path string) {
	for _, p := range activePlugins {
		tags, err := p.Classify(title, path)
		if err != nil || len(tags) == 0 {
			continue
		}
		_ = st.AddTags(mediaID, tags)
	}
}

func (p Plugin) String() string {
	return fmt.Sprintf("%s %s (%s)", p.ID, p.Version, strings.Join(p.Hooks, ","))
}
