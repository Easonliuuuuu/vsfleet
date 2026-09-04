package cli

import (
	"testing"
	"time"
)

func TestParseHumanDuration(t *testing.T) {
	for input, want := range map[string]time.Duration{"30d": 30 * 24 * time.Hour, "2w": 14 * 24 * time.Hour, "1w2d12h": 9*24*time.Hour + 12*time.Hour, "90m": 90 * time.Minute} {
		got, err := parseHumanDuration(input)
		if err != nil || got != want {
			t.Fatalf("%s: got %s err=%v want %s", input, got, err, want)
		}
	}
}
