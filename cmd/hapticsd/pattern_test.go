package main

import "testing"

func TestPatternsInRange(t *testing.T) {
	names := []string{"steady", "firm", "wave", "build", "hold", "drop", "beat", "surge", "drift"}
	for _, n := range names {
		for _, tt := range []float64{0, 0.5, 3, 10, 19, 23.9, 60} {
			lo, hi := evalPattern(n, tt)
			if lo < 0 || lo > 1 || hi < 0 || hi > 1 {
				t.Fatalf("%s@%.1f -> %.2f %.2f", n, tt, lo, hi)
			}
		}
	}
}

func TestHoldDrops(t *testing.T) {
	lo, _ := evalPattern("hold", 22.5)
	if lo > 0.2 {
		t.Fatalf("hold did not drop: %.2f", lo)
	}
	lo2, _ := evalPattern("drop", 10.5)
	if lo2 != 0 {
		t.Fatalf("drop not zero: %.2f", lo2)
	}
}
