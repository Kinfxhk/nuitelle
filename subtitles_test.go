package main

import "testing"

func TestSRTToVTT(t *testing.T) {
	in := "1\n00:00:01,000 --> 00:00:03,500\nHello\n\n2\n00:00:04,000 --> 00:00:05,000\nWorld\n"
	out := srtToVTT(in)
	if len(out) < 9 || out[:6] != "WEBVTT" {
		t.Fatalf("missing header: %q", out)
	}
	if !contains(out, "00:00:01.000 --> 00:00:03.500") {
		t.Fatalf("timestamps not converted: %q", out)
	}
	if contains(out, "\n1\n") {
		t.Fatalf("cue index kept: %q", out)
	}
}

func TestParseRenderSRT(t *testing.T) {
	s := "1\n00:00:01,000 --> 00:00:02,000\nA\nB\n\n2\n00:00:03,000 --> 00:00:04,000\nC\n"
	cues := parseSRT(s)
	if len(cues) != 2 {
		t.Fatalf("want 2 cues, got %d", len(cues))
	}
	if cues[0].Timing != "00:00:01,000 --> 00:00:02,000" || len(cues[0].Lines) != 2 {
		t.Fatalf("cue 0: %+v", cues[0])
	}
	if got := renderSRT(cues); !contains(got, "00:00:03,000 --> 00:00:04,000") {
		t.Fatalf("render lost timing: %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
