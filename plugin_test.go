package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPluginClassify(t *testing.T) {
	dir := t.TempDir()
	pdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(pdir, "manifest.json"), []byte(`{"id":"demo","version":"1","hooks":["scan.classify"]}`), 0o644)
	os.WriteFile(filepath.Join(pdir, "plugin.js"), []byte(`function scanClassify(i){ return {tags:["Demo Tag","second"]}; }`), 0o644)
	ps, err := LoadPlugins(dir)
	if err != nil || len(ps) != 1 {
		t.Fatalf("load: %v %v", ps, err)
	}
	tags, err := ps[0].Classify("clip", "/x/clip.mp4")
	if err != nil || len(tags) != 2 || tags[0] != "demo-tag" {
		t.Fatalf("tags: %v %v", tags, err)
	}
}

func TestPluginTimeoutGuard(t *testing.T) {
	p := Plugin{ID: "x", Hooks: []string{"scan.classify"}, Script: `function scanClassify(){ while(true){} }`}
	if _, err := p.Classify("a", "b"); err == nil {
		t.Fatal("infinite loop was not interrupted")
	}
}

func TestPluginMetadata(t *testing.T) {
	p := Plugin{ID: "m", Hooks: []string{"metadata.fetch"}, Script: `function metadataFetch(i){ return {title:"New Title", tags:["Meta Tag"]}; }`}
	title, tags, err := p.Metadata("old", "/x.mp4")
	if err != nil || title != "New Title" || len(tags) != 1 || tags[0] != "meta-tag" {
		t.Fatalf("metadata: %q %v %v", title, tags, err)
	}
}
