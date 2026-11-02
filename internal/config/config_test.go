package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	cfg, err := Parse(`
keep = "24h"

[[jobs]]
name = "nightly-review"
schedule = "0 3 * * 1-5"
dir = "~/src/app"
command = ["void", "agent", "agents/review.md"]

[[jobs]]
name = "hourly"
schedule = "@every 6h"
command = ["true"]
keep = "90m"
`, "/home/me")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Space != DefaultSpace || !spaceRe.MatchString(cfg.Space) {
		t.Errorf("space = %q, want the default, a valid space id", cfg.Space)
	}
	if len(cfg.Jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(cfg.Jobs))
	}
	review := cfg.Jobs[0]
	if review.Dir != "/home/me/src/app" {
		t.Errorf("dir = %q, want ~ expanded", review.Dir)
	}
	if !slices.Equal(review.Command, []string{"void", "agent", "agents/review.md"}) {
		t.Errorf("command = %q", review.Command)
	}
	if cfg.Jobs[1].Dir != "/home/me" {
		t.Errorf("dir = %q, want home by default", cfg.Jobs[1].Dir)
	}
	if review.Keep != 24*time.Hour {
		t.Errorf("keep = %s, want the global 24h", review.Keep)
	}
	if cfg.Jobs[1].Keep != 90*time.Minute {
		t.Errorf("keep = %s, want the job's own 90m", cfg.Jobs[1].Keep)
	}
}

func TestParseErrors(t *testing.T) {
	job := func(fields string) string {
		return "[[jobs]]\n" + fields + "\n"
	}
	valid := `name = "a"` + "\n" + `schedule = "@daily"` + "\n" + `command = ["true"]`
	tests := []struct {
		name, data, want string
	}{
		{"unknown job key", job(valid + "\ncmd = [\"x\"]"), "unknown keys: jobs.cmd"},
		{"bad space", `space = "Scheduled"`, "must be a Herdr app space id"},
		{"bad keep", `keep = "3 days"`, "must be a duration"},
		{"negative keep", `keep = "-1h"`, "must be a duration"},
		{"bad job keep", job(valid + "\nkeep = \"2 days\""), "a: keep \"2 days\" must be a duration"},
		{"bad name", job(strings.Replace(valid, `"a"`, `"Nightly"`, 1)), "name \"Nightly\" must be"},
		{"long name", job(strings.Replace(valid, `"a"`, `"`+strings.Repeat("a", MaxNameLength+1)+`"`, 1)), "must be 1 to 20"},
		{"bad schedule", job(strings.Replace(valid, "@daily", "every day", 1)), "schedule \"every day\""},
		{"no command", job(strings.Replace(valid, `["true"]`, `[]`, 1)), "command must list"},
		{"relative dir", job(valid + "\ndir = \"src\""), "must be absolute"},
		{"duplicate", job(valid) + job(valid), "name \"a\" is already used"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.data, "/home/me")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
