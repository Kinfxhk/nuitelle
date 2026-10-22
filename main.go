package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/term"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var version = "0.1.0"

// Config is the on-disk configuration. It intentionally stays small.
type Config struct {
	Roots    []string `json:"roots"`
	Addr     string   `json:"addr"`
	DB       string   `json:"db"`
	CoverDir string   `json:"cover_dir"`

	WhisperBin      string `json:"whisper_bin"`
	WhisperModel    string `json:"whisper_model"`
	ModelDir        string `json:"model_dir"`
	TranslatorURL   string `json:"translator_url"`
	TranslatorModel string `json:"translator_model"`
	TranslatorKey   string `json:"translator_key"`
	VisionURL       string `json:"vision_url"`
	VisionModel     string `json:"vision_model"`
	AllowLAN        bool   `json:"allow_lan"`
	Token           string `json:"token"`
	PasswordHash    string `json:"password_hash"`
	SessionKey      string `json:"session_key"`
	TotpSecret      string `json:"totp_secret"`
	PluginDir       string `json:"plugin_dir"`
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil || d == "" {
		d = filepath.Join(homeDir(), ".config")
	}
	return filepath.Join(d, "vela")
}

func configPath() string { return filepath.Join(configDir(), "config.json") }

func defaultConfig() *Config {
	return &Config{
		Addr:            "127.0.0.1:8770",
		DB:              filepath.Join(configDir(), "vela.db"),
		CoverDir:        filepath.Join(configDir(), "covers"),
		WhisperModel:    "base",
		ModelDir:        filepath.Join(homeDir(), ".vela", "models"),
		TranslatorURL:   "http://127.0.0.1:11434/v1",
		TranslatorModel: "qwen2.5:7b",
		VisionURL:       "http://127.0.0.1:8100/v1",
		VisionModel:     "qwen3-vl-4b",
		PluginDir:       filepath.Join(configDir(), "plugins"),
	}
}

func loadConfig() (*Config, error) {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	c := defaultConfig()
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	return c, nil
}

// loadConfigOrEnv allows container use without a config file:
// VELA_ROOTS=/media VELA_DB=/data/vela.db vela serve
func loadConfigOrEnv() (*Config, error) {
	c, err := loadConfig()
	if err == nil {
		if v := os.Getenv("VELA_ROOTS"); v != "" {
			c.Roots = splitRoots(v)
		}
		if v := os.Getenv("VELA_ADDR"); v != "" {
			c.Addr = v
		}
		if v := os.Getenv("VELA_DB"); v != "" {
			c.DB = v
		}
		if v := os.Getenv("VELA_COVERS"); v != "" {
			c.CoverDir = v
		}
		return c, nil
	}
	roots := os.Getenv("VELA_ROOTS")
	if roots == "" {
		return nil, err
	}
	cfg := defaultConfig()
	cfg.Roots = splitRoots(roots)
	cfg.DB = envOr("VELA_DB", "/data/vela.db")
	cfg.CoverDir = envOr("VELA_COVERS", filepath.Join(filepath.Dir(cfg.DB), "covers"))
	cfg.Addr = envOr("VELA_ADDR", "127.0.0.1:8770")
	return cfg, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func splitRoots(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func saveConfig(c *Config) error {
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(configPath(), b, 0o600)
}

func lanIPs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				out = append(out, ipnet.IP.String())
			}
		}
	}
	return out
}

func usage() {
	fmt.Fprintf(os.Stderr, `Vela %s - local-first personal media library

Usage:
  vela init                 Create config (interactive)
  vela scan [--full]        Index configured folders
  vela serve [--addr HOST]  Start the web UI and API
  vela tag <id> [--model qwen3-vl-2b] [--frames 3]
                            Suggest tags with a local vision model
  vela subs <id> [--model M] [--lang auto] [--translate zh-Hant]
                            Generate subtitles locally (whisper.cpp)
  vela passwd               Set or clear the web password
  vela totp                 Enable TOTP (prints a secret for your authenticator)
  vela plugins              List installed JS plugins
  vela import-stash --db DB  Import titles/tags from a Stash database
  vela export [--out FILE]  Dump metadata (no media files)
  vela import [--in FILE]   Merge a metadata dump
  vela doctor               Check the installation
  vela version              Print version

Config: %s
`, version, configPath())
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init":
		cmdInit()
	case "scan":
		cmdScan(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "doctor":
		cmdDoctor()
	case "subs":
		cmdSubs(os.Args[2:])
	case "tag":
		cmdTag(os.Args[2:])
	case "passwd":
		cmdPasswd()
	case "totp":
		cmdTotp()
	case "plugins":
		cmdPlugins()
	case "import-stash":
		cmdImportStash(os.Args[2:])
	case "export":
		cmdExport(os.Args[2:])
	case "import":
		cmdImport(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("vela", version)
	default:
		usage()
		os.Exit(2)
	}
}

func ask(r *bufio.Reader, prompt, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", prompt, def)
	} else {
		fmt.Printf("%s: ", prompt)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func cmdInit() {
	r := bufio.NewReader(os.Stdin)
	c := defaultConfig()
	if old, err := loadConfig(); err == nil {
		c = old
	}
	fmt.Println("Vela setup")
	fmt.Println("-----------")
	fmt.Println("Point Vela at one or more folders you own. Nothing is uploaded.")
	roots := ask(r, "Media folders (comma separated)", strings.Join(c.Roots, ","))
	var clean []string
	for _, p := range strings.Split(roots, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			fmt.Println("  skip (bad path):", p)
			continue
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			fmt.Println("  skip (not a directory):", abs)
			continue
		}
		clean = append(clean, abs)
	}
	if len(clean) == 0 {
		fmt.Println("No valid folders. Nothing written.")
		os.Exit(1)
	}
	c.Roots = clean
	c.Addr = ask(r, "Listen address", c.Addr)
	if err := saveConfig(c); err != nil {
		fmt.Println("write config:", err)
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	st.Close()
	fmt.Println()
	fmt.Println("Saved:", configPath())
	fmt.Println("Next: vela scan && vela serve")
	fmt.Println("UI:   http://" + c.Addr)
}

func cmdScan(args []string) {
	full := false
	for _, a := range args {
		if a == "--full" {
			full = true
		}
	}
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	ps, _ := LoadPlugins(c.PluginDir)
	SetPlugins(ps)
	total := 0
	for _, root := range c.Roots {
		n, err := ScanRoot(st, root, full)
		if err != nil {
			fmt.Printf("%s: %v\n", root, err)
			continue
		}
		fmt.Printf("%s: %d files\n", root, n)
		total += n
	}
	fmt.Println("Indexed:", total)
	if err := st.RebuildFTS(); err != nil {
		fmt.Println("search index:", err)
	}
}

func cmdServe(args []string) {
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--addr" && i+1 < len(args) {
			c.Addr = args[i+1]
		}
		if args[i] == "--lan" {
			c.AllowLAN = true
			if c.Addr == "127.0.0.1:8770" {
				c.Addr = "0.0.0.0:8770"
			}
		}
	}
	ps, _ := LoadPlugins(c.PluginDir)
	SetPlugins(ps)
	if c.AllowLAN && c.Token == "" {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err == nil {
			c.Token = hex.EncodeToString(buf)
		}
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	fmt.Println("Vela listening on http://" + c.Addr)
	fmt.Println("Roots:", strings.Join(c.Roots, ", "))
	if c.AllowLAN {
		fmt.Println("LAN mode: API requires the token below")
		for _, ip := range lanIPs() {
			fmt.Printf("  http://%s:8770/#token=%s\n", ip, c.Token)
		}
	}
	if err := Serve(c, st); err != nil {
		fmt.Println("server:", err)
		os.Exit(1)
	}
}

func cmdPasswd() {
	c, err := loadConfig()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	fmt.Print("New password (empty to disable): ")
	pw, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if len(pw) == 0 {
		c.PasswordHash = ""
		if err := saveConfig(c); err != nil {
			fmt.Println("save:", err)
			os.Exit(1)
		}
		fmt.Println("Password disabled.")
		return
	}
	fmt.Print("Repeat: ")
	again, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if string(pw) != string(again) {
		fmt.Println("Passwords do not match.")
		os.Exit(1)
	}
	h, err := hashPassword(string(pw))
	if err != nil {
		fmt.Println("hash:", err)
		os.Exit(1)
	}
	c.PasswordHash = h
	if c.SessionKey == "" {
		c.SessionKey = newSessionKey()
	}
	if err := saveConfig(c); err != nil {
		fmt.Println("save:", err)
		os.Exit(1)
	}
	fmt.Println("Password set. Restart the server, then sign in in the browser.")
}

func cmdImportStash(args []string) {
	path := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--db" && i+1 < len(args) {
			path = args[i+1]
		}
	}
	if path == "" {
		fmt.Println("usage: vela import-stash --db /path/stash-go.sqlite")
		os.Exit(2)
	}
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	matched, tagged, titles, err := ImportStash(st, path)
	if err != nil {
		fmt.Println("stash import:", err)
		os.Exit(1)
	}
	fmt.Printf("matched %d media, %d titles, %d tagged\n", matched, titles, tagged)
}

func cmdPlugins() {
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	ps, _ := LoadPlugins(c.PluginDir)
	if len(ps) == 0 {
		fmt.Println("No plugins in", c.PluginDir)
		return
	}
	for _, p := range ps {
		fmt.Printf("%-24s %-10s %s\n", p.ID, p.Version, strings.Join(p.Hooks, ","))
	}
}

func cmdTotp() {
	c, err := loadConfig()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	if c.TotpSecret == "" {
		c.TotpSecret = newTOTPSecret()
		if err := saveConfig(c); err != nil {
			fmt.Println("save:", err)
			os.Exit(1)
		}
	}
	fmt.Println("TOTP secret:", c.TotpSecret)
	fmt.Printf("otpauth://totp/Vela?secret=%s&issuer=Vela\n", c.TotpSecret)
	fmt.Println("Add it to your authenticator; login will ask for the 6-digit code.")
	fmt.Println("To disable, remove totp_secret from the config.")
}

func cmdExport(args []string) {
	out := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--out" && i+1 < len(args) {
			out = args[i+1]
		}
	}
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	b, err := st.ExportAll()
	if err != nil {
		fmt.Println("export:", err)
		os.Exit(1)
	}
	data, _ := json.MarshalIndent(b, "", "  ")
	if out == "" {
		fmt.Println(string(data))
		return
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		fmt.Println("write:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", out, len(data), "bytes")
}

func cmdImport(args []string) {
	in := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--in" && i+1 < len(args) {
			in = args[i+1]
		}
	}
	if in == "" {
		fmt.Println("usage: vela import --in FILE")
		os.Exit(2)
	}
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	raw, err := os.ReadFile(in)
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	var b Backup
	if err := json.Unmarshal(raw, &b); err != nil {
		fmt.Println("parse:", err)
		os.Exit(1)
	}
	ms, ps, err := st.ImportAll(&b)
	if err != nil {
		fmt.Println("import:", err)
		os.Exit(1)
	}
	if b.Layout != "" {
		_ = st.SetLayout(b.Layout)
	}
	_ = st.RebuildFTS()
	fmt.Printf("imported %d media, %d playlists\n", ms, ps)
}

func cmdTag(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: vela tag <media-id> | --folder <path> [--model qwen3-vl-4b] [--frames 3]")
		os.Exit(2)
	}
	id := ""
	folder := ""
	model := ""
	frames := 3
	if args[0] != "--folder" {
		id = args[0]
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "--frames":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &frames)
				i++
			}
		case "--folder":
			if i+1 < len(args) {
				folder = args[i+1]
				i++
			}
		}
	}
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	if folder != "" {
		ids, err := st.MediaIDsUnder(folder)
		if err != nil || len(ids) == 0 {
			fmt.Println("no media under", folder)
			os.Exit(1)
		}
		done, failed := TagBatch(context.Background(), c, st, ids, model, frames, nil)
		fmt.Printf("tagged %d, failed %d\n", done, failed)
		return
	}
	tags, err := TagMedia(c, st, id, model, frames)
	if err != nil {
		fmt.Println("tag:", err)
		os.Exit(1)
	}
	for _, t := range tags {
		fmt.Printf("%-24s %.2f\n", t.Name, t.Confidence)
	}
	fmt.Println("Suggestions stored; accept them in the UI.")
}

func cmdSubs(args []string) {
	if len(args) == 0 {
		fmt.Println("usage: vela subs <media-id> [--model base] [--lang auto] [--translate zh-Hant]")
		os.Exit(2)
	}
	id := args[0]
	opt := SubOptions{Lang: "auto"}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--model":
			if i+1 < len(args) {
				opt.Model = args[i+1]
				i++
			}
		case "--lang":
			if i+1 < len(args) {
				opt.Lang = args[i+1]
				i++
			}
		case "--translate":
			if i+1 < len(args) {
				opt.Translate = args[i+1]
				i++
			}
		}
	}
	c, err := loadConfigOrEnv()
	if err != nil {
		fmt.Println("No config. Run: vela init")
		os.Exit(1)
	}
	st, err := OpenStore(c.DB)
	if err != nil {
		fmt.Println("open database:", err)
		os.Exit(1)
	}
	defer st.Close()
	subs, err := GenerateSubtitles(c, st, id, opt)
	if err != nil {
		fmt.Println("subs:", err)
		os.Exit(1)
	}
	for _, s := range subs {
		fmt.Printf("%s/%s  %s  (%s)\n", s.Kind, s.Lang, s.Path, s.Model)
	}
}

func cmdDoctor() {
	ok := true
	c, err := loadConfig()
	if err != nil {
		fmt.Println("✗ config: not found — run `vela init`")
		os.Exit(1)
	}
	fmt.Println("✓ config:", configPath())
	for _, root := range c.Roots {
		st, err := os.Stat(root)
		if err != nil || !st.IsDir() {
			fmt.Println("✗ folder:", root)
			ok = false
			continue
		}
		f, err := os.Open(root)
		if err != nil {
			fmt.Println("✗ folder not readable:", root)
			ok = false
			continue
		}
		f.Close()
		fmt.Println("✓ folder:", root)
	}
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Println("✓ ffmpeg found (covers + prob+transcode)")
	} else {
		fmt.Println("! ffmpeg missing — covers disabled, playback still works")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		fmt.Println("! ffprobe missing — duration/dimensions unknown")
	}
	if st, err := OpenStore(c.DB); err == nil {
		fmt.Println("✓ database:", c.DB)
		st.Close()
	} else {
		fmt.Println("✗ database:", err)
		ok = false
	}
	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		fmt.Println("✗ port busy:", c.Addr)
		ok = false
	} else {
		ln.Close()
		fmt.Println("✓ port free:", c.Addr)
	}
	if !ok {
		os.Exit(1)
	}
}
