package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranslateSRT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		// Echo the requested lines back as the translation.
		var lines []string
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "[") {
				start := strings.Index(m.Content, "[")
				json.Unmarshal([]byte(m.Content[start:]), &lines)
			}
		}
		out := map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"lines":` + mustJSON(lines) + `}`}}}}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	cfg := &Config{TranslatorURL: srv.URL, TranslatorModel: "test"}
	srt := "1\n00:00:01,000 --> 00:00:02,000\nHello\n\n2\n00:00:03,000 --> 00:00:04,000\nWorld\n"
	out, err := translateSRT(cfg, srt, "zh-Hant")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "00:00:03,000") {
		t.Fatalf("bad output: %q", out)
	}
}
