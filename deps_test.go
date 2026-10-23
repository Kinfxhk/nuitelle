package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanInstallsDedupesAndSplitsManual(t *testing.T) {
	apt := &pkgMgr{"apt", true, []string{"apt-get", "install", "-y"}}
	pkgs, manual := planInstalls(apt, []toolDep{ffmpegDep, ffprobeDep, whisperDep})
	if !reflect.DeepEqual(pkgs, []string{"ffmpeg", "whisper.cpp"}) || len(manual) != 0 {
		t.Fatalf("apt plan = %v manual=%d", pkgs, len(manual))
	}
	winget := &pkgMgr{"winget", false, []string{"winget", "install"}}
	pkgs, manual = planInstalls(winget, []toolDep{ffmpegDep, whisperDep})
	if !reflect.DeepEqual(pkgs, []string{"Gyan.FFmpeg"}) || len(manual) != 1 || manual[0].label != "whisper.cpp" {
		t.Fatalf("winget plan = %v manual=%v", pkgs, manual)
	}
	pkgs, manual = planInstalls(nil, []toolDep{ffmpegDep})
	if len(pkgs) != 0 || len(manual) != 1 {
		t.Fatalf("no package manager: pkgs=%v manual=%d", pkgs, len(manual))
	}
}

func TestFoundUsesAnyAlias(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(b string) (string, error) {
		if b == "whisper-cpp" {
			return "/opt/bin/whisper-cpp", nil
		}
		return "", errors.New("missing")
	}
	if p, ok := whisperDep.found(); !ok || p != "/opt/bin/whisper-cpp" {
		t.Fatalf("found = %q %v", p, ok)
	}
	if _, ok := ffmpegDep.found(); ok {
		t.Fatal("ffmpeg should be missing")
	}
}

func TestDownloadModelVerifiesChecksum(t *testing.T) {
	body := []byte("fake ggml model")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	oldBase, oldSum := whisperModelBase, whisperModels["tiny"]
	defer func() { whisperModelBase, whisperModels["tiny"] = oldBase, oldSum }()
	whisperModelBase = srv.URL + "/"
	dir := t.TempDir()

	whisperModels["tiny"] = "0000"
	if _, err := downloadModel(dir, "tiny"); err == nil {
		t.Fatal("bad checksum accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "ggml-tiny.bin")); err == nil {
		t.Fatal("bad download left a model file in place")
	}

	h := sha256.Sum256(body)
	whisperModels["tiny"] = hex.EncodeToString(h[:])
	p, err := downloadModel(dir, "tiny")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != string(body) {
		t.Fatalf("model content = %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".ggml-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
