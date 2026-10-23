package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Optional external tools. Nuitelle plays files without any of them; each one
// unlocks features. doctor reports what is missing and --fix installs it with
// the platform package manager.

type pkgMgr struct {
	name string   // brew | apt | dnf | pacman | winget
	sudo bool     // command needs root
	cmd  []string // install prefix; package names are appended
}

type toolDep struct {
	label   string            // shown to the user
	bins    []string          // any of these on PATH satisfies the check
	feature string            // what is lost without it
	pkgs    map[string]string // package manager -> package name
}

var toolDeps = []toolDep{ffmpegDep, ffprobeDep, whisperDep}

var (
	ffmpegDep = toolDep{"ffmpeg", []string{"ffmpeg"}, "covers, subtitles, transcode, AI frames",
		map[string]string{"brew": "ffmpeg", "apt": "ffmpeg", "dnf": "ffmpeg", "pacman": "ffmpeg", "winget": "Gyan.FFmpeg"}}
	ffprobeDep = toolDep{"ffprobe", []string{"ffprobe"}, "duration, resolution, codecs",
		map[string]string{"brew": "ffmpeg", "apt": "ffmpeg", "dnf": "ffmpeg", "pacman": "ffmpeg", "winget": "Gyan.FFmpeg"}}
	// No winget package for whisper.cpp: Windows users get a download hint instead.
	whisperDep = toolDep{"whisper.cpp", []string{"whisper-cli", "whisper-cpp", "whisper"}, "local subtitles",
		map[string]string{"brew": "whisper-cpp", "apt": "whisper.cpp", "dnf": "whisper-cpp", "pacman": "whisper-cpp"}}
)

// whisperModels maps model name -> SHA-256 of ggml-<name>.bin as published on
// huggingface.co/ggerganov/whisper.cpp (LFS oid).
var whisperModels = map[string]string{
	"tiny":           "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21",
	"base":           "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe",
	"small":          "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
	"medium":         "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208",
	"large-v3":       "64d182b440b98d5203c4f9bd541544d84c605196c4f7b845dfa11fb23594d1e2",
	"large-v3-turbo": "1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69",
}

var whisperModelBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" // var so tests can point at a local server

var lookPath = exec.LookPath // swapped in tests

func detectPkgMgr() *pkgMgr {
	have := func(b string) bool { _, err := lookPath(b); return err == nil }
	switch runtime.GOOS {
	case "darwin":
		if have("brew") {
			return &pkgMgr{"brew", false, []string{"brew", "install"}}
		}
	case "windows":
		if have("winget") {
			return &pkgMgr{"winget", false, []string{"winget", "install", "-e", "--accept-source-agreements", "--accept-package-agreements", "--id"}}
		}
	default:
		switch {
		case have("apt-get"):
			return &pkgMgr{"apt", true, []string{"apt-get", "install", "-y"}}
		case have("dnf"):
			return &pkgMgr{"dnf", true, []string{"dnf", "install", "-y"}}
		case have("pacman"):
			return &pkgMgr{"pacman", true, []string{"pacman", "-S", "--noconfirm", "--needed"}}
		}
	}
	return nil
}

func (d toolDep) found() (string, bool) {
	for _, b := range d.bins {
		if p, err := lookPath(b); err == nil {
			return p, true
		}
	}
	return "", false
}

// manualHint is printed when there is no package manager or no package.
func (d toolDep) manualHint() string {
	switch {
	case d.label == "whisper.cpp" && runtime.GOOS == "windows":
		return "download whisper-bin-x64.zip from https://github.com/ggml-org/whisper.cpp/releases and set whisper_bin in config.json"
	case runtime.GOOS == "darwin":
		return "install Homebrew (https://brew.sh), then run `nuitelle doctor --fix`"
	case d.label == "whisper.cpp":
		return "build from https://github.com/ggml-org/whisper.cpp (cmake -B build && cmake --build build) and set whisper_bin"
	default:
		return "install " + d.label + " with your package manager"
	}
}

// installCmd returns the argv to install pkgs, adding sudo when needed.
func installCmd(pm *pkgMgr, pkgs []string) []string {
	argv := append(append([]string{}, pm.cmd...), pkgs...)
	if pm.sudo && os.Geteuid() != 0 {
		argv = append([]string{"sudo"}, argv...)
	}
	return argv
}

// planInstalls groups missing tools into one install command, de-duplicating
// shared packages (ffmpeg provides ffprobe).
func planInstalls(pm *pkgMgr, missing []toolDep) (pkgs []string, manual []toolDep) {
	seen := map[string]bool{}
	for _, d := range missing {
		p, ok := "", false
		if pm != nil {
			p, ok = d.pkgs[pm.name]
		}
		if !ok {
			manual = append(manual, d)
			continue
		}
		if !seen[p] {
			seen[p] = true
			pkgs = append(pkgs, p)
		}
	}
	return pkgs, manual
}

func confirm(r *bufio.Reader, prompt string, yes bool) bool {
	if yes {
		return true
	}
	fmt.Printf("%s [Y/n] ", prompt)
	line, err := r.ReadString('\n')
	if err != nil {
		return false // no terminal: never install silently
	}
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "" || a == "y" || a == "yes"
}

func runVisible(argv []string) error {
	fmt.Println("  $", strings.Join(argv, " "))
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// downloadModel fetches ggml-<name>.bin into dir, verifying SHA-256 before the
// file is moved into place so a partial or tampered download never looks valid.
func downloadModel(dir, name string) (string, error) {
	sum, ok := whisperModels[name]
	if !ok {
		return "", fmt.Errorf("unknown model %q", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, "ggml-"+name+".bin")
	tmp, err := os.CreateTemp(dir, ".ggml-"+name+"-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", whisperModelBase+"ggml-"+name+".bin", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmp.Close()
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		tmp.Close()
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, label: "ggml-" + name + ".bin"}
	_, err = io.Copy(io.MultiWriter(tmp, h, pw), resp.Body)
	fmt.Println()
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sum {
		return "", fmt.Errorf("checksum mismatch for %s (got %s)", name, got[:12])
	}
	_ = os.Chmod(tmp.Name(), 0o644) // CreateTemp makes 0600; models are shared, not secret
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

type progressWriter struct {
	total, n int64
	label    string
	last     time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	if time.Since(p.last) > 300*time.Millisecond || p.n == p.total {
		p.last = time.Now()
		if p.total > 0 {
			fmt.Printf("\r  %s  %d%%  (%d / %d MB)", p.label, p.n*100/p.total, p.n>>20, p.total>>20)
		} else {
			fmt.Printf("\r  %s  %d MB", p.label, p.n>>20)
		}
	}
	return len(b), nil
}

// checkTools prints tool status and, with fix, installs what is missing.
// It returns false only when fixing was requested and something is still missing.
func checkTools(c *Config, fix, yes bool) bool {
	in := bufio.NewReader(os.Stdin)
	var missing []toolDep
	for _, d := range toolDeps {
		if p, ok := d.found(); ok {
			fmt.Printf("✓ %s: %s\n", d.label, p)
		} else {
			fmt.Printf("! %s missing — %s unavailable\n", d.label, d.feature)
			missing = append(missing, d)
		}
	}
	if c.WhisperBin != "" {
		if _, err := os.Stat(c.WhisperBin); err != nil {
			fmt.Println("! whisper_bin in config does not exist:", c.WhisperBin)
		}
	}

	if fix && len(missing) > 0 {
		pm := detectPkgMgr()
		pkgs, manual := planInstalls(pm, missing)
		if len(pkgs) > 0 {
			argv := installCmd(pm, pkgs)
			if confirm(in, "Install "+strings.Join(pkgs, ", ")+" with "+pm.name+"?", yes) {
				if pm.name == "apt" { // fresh images often have an empty package index
					_ = runVisible(installCmd(&pkgMgr{"apt", true, []string{"apt-get", "update"}}, nil))
				}
				if err := runVisible(argv); err != nil {
					fmt.Println("✗ install failed:", err)
				}
			}
		}
		for _, d := range manual {
			fmt.Printf("→ %s: %s\n", d.label, d.manualHint())
		}
		missing = missing[:0]
		for _, d := range toolDeps {
			if _, ok := d.found(); !ok {
				missing = append(missing, d)
			}
		}
	}

	// Whisper model: only worth checking when whisper.cpp itself is present.
	modelOK := true
	if _, ok := whisperDep.found(); ok || c.WhisperBin != "" {
		name := c.WhisperModel
		if name == "" {
			name = "base"
		}
		if p, err := whisperModelPath(c, name); err == nil {
			fmt.Println("✓ whisper model:", p)
		} else if _, known := whisperModels[name]; fix && known {
			dir := c.ModelDir
			if dir == "" {
				dir = filepath.Join(homeDir(), ".nuitelle", "models")
			}
			if confirm(in, fmt.Sprintf("Download whisper model %q to %s?", name, dir), yes) {
				if p, err := downloadModel(dir, name); err != nil {
					fmt.Println("✗ model download:", err)
					modelOK = false
				} else {
					fmt.Println("✓ whisper model:", p)
				}
			} else {
				modelOK = false
			}
		} else {
			fmt.Printf("! whisper model %q missing — run `nuitelle doctor --fix`\n", name)
			modelOK = !fix
		}
	}
	return !fix || (len(missing) == 0 && modelOK)
}
