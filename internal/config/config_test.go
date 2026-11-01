package config

import (
	"os"
	"path/filepath"
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
	if cfg.Space != DefaultSpace {
		t.Errorf("space = %q, want the default", cfg.Space)
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

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse("[[jobs]]\nname = \"a\"\nschedule = \"@daily\"\ncommand = [\"true\"]", "/home/me")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Space != DefaultSpace || cfg.Jobs[0].Keep != DefaultKeep {
		t.Errorf("got %+v, want the default space and keep", cfg)
	}
	if !spaceRe.MatchString(DefaultSpace) {
		t.Errorf("default space %q is not a space id", DefaultSpace)
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
		{"unknown key", "spaces = \"x\"", "unknown keys: spaces"},
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

func TestLoadNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`keep = "x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, "/home/me")
	if err == nil || !strings.HasPrefix(err.Error(), path+": ") {
		t.Errorf("err = %v, want it prefixed with the path", err)
	}
}

func TestJob(t *testing.T) {
	cfg := Config{Jobs: []Job{{Name: "a"}, {Name: "b"}}}
	if j, err := cfg.Job("b"); err != nil || j.Name != "b" {
		t.Errorf("Job(b) = %+v, %v", j, err)
	}
	if _, err := cfg.Job("c"); err == nil {
		t.Error("Job(c) succeeded, want an error")
	}
}
