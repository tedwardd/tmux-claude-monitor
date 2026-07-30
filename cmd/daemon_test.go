package cmd

import (
	"testing"
	"time"
)

func TestComputeBackoff(t *testing.T) {
	cases := []struct {
		fails int
		want  time.Duration
	}{
		{0, 30 * time.Second},
		{1, 60 * time.Second},
		{3, 240 * time.Second},
		{4, 300 * time.Second},  // exceeds cap, clamped
		{10, 300 * time.Second}, // shift clamp boundary
		{63, 300 * time.Second}, // would overflow int64 shift if unclamped
		{64, 300 * time.Second}, // shift == bit width: 1<<64 is 0 if unclamped
		{1000, 300 * time.Second},
	}
	for _, c := range cases {
		got := computeBackoff(c.fails)
		if got != c.want {
			t.Errorf("computeBackoff(%d) = %v, want %v", c.fails, got, c.want)
		}
	}
}

func TestComputeBackoffAlwaysPositive(t *testing.T) {
	for _, fails := range []int{0, 1, 2, 5, 62, 63, 64, 65, 128, 1 << 20} {
		if got := computeBackoff(fails); got <= 0 {
			t.Errorf("computeBackoff(%d) = %v, must be positive (would panic time.Ticker.Reset)", fails, got)
		}
	}
}
