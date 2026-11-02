// Package config loads and validates the herdr-cron configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/robfig/cron/v3"
)

// DefaultSpace is the Herdr app space runs land in when none is configured.
// The app creates it on the first run; naming it in the app keeps the name.
const DefaultSpace = "sp7ec9c6c3a3d77a70c0a7dc6169e694fe"

// DefaultKeep is how long a finished run stays visible.
const DefaultKeep = 72 * time.Hour

// MaxNameLength leaves room for the run suffix in a terminal name, which
// herdr-core limits to 32 characters.
const MaxNameLength = 20

var (
	nameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	spaceRe = regexp.MustCompile(`^sp[0-9a-f]{32}$`)
)

// Config is a validated configuration.
type Config struct {
	Space string
	Jobs  []Job
}

// Job is one scheduled command.
type Job struct {
	Name     string
	Schedule cron.Schedule
	Dir      string
	Command  []string
	// Keep is how long a finished run stays visible.
	Keep time.Duration
}

// Job returns the job with the given name.
func (c Config) Job(name string) (Job, error) {
	for _, j := range c.Jobs {
		if j.Name == name {
			return j, nil
		}
	}
	return Job{}, fmt.Errorf("no job named %q in the configuration", name)
}

type file struct {
	Space string    `toml:"space"`
	Keep  string    `toml:"keep"`
	Jobs  []fileJob `toml:"jobs"`
}

type fileJob struct {
	Name     string   `toml:"name"`
	Schedule string   `toml:"schedule"`
	Dir      string   `toml:"dir"`
	Command  []string `toml:"command"`
	Keep     string   `toml:"keep"`
}

// DefaultPath is where the configuration lives unless told otherwise.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "herdr-cron", "config.toml"), nil
}

// Load reads and validates the configuration at path.
func Load(path, home string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg, err := Parse(string(data), home)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse validates a configuration; home expands a leading "~" in job dirs
// and is the default dir.
func Parse(data, home string) (Config, error) {
	var f file
	md, err := toml.Decode(data, &f)
	if err != nil {
		return Config{}, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
	}

	cfg := Config{Space: DefaultSpace}
	if f.Space != "" {
		if !spaceRe.MatchString(f.Space) {
			return Config{}, fmt.Errorf("space %q must be a Herdr app space id: \"sp\" followed by 32 hex digits", f.Space)
		}
		cfg.Space = f.Space
	}
	keep, err := parseKeep(f.Keep, DefaultKeep)
	if err != nil {
		return Config{}, err
	}

	var errs []error
	seen := map[string]bool{}
	for i, fj := range f.Jobs {
		job, err := validateJob(fj, home, keep)
		if err != nil {
			errs = append(errs, fmt.Errorf("job #%d: %w", i+1, err))
			continue
		}
		if seen[job.Name] {
			errs = append(errs, fmt.Errorf("job #%d: name %q is already used", i+1, job.Name))
			continue
		}
		seen[job.Name] = true
		cfg.Jobs = append(cfg.Jobs, job)
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateJob(fj fileJob, home string, defaultKeep time.Duration) (Job, error) {
	if !nameRe.MatchString(fj.Name) || len(fj.Name) > MaxNameLength {
		return Job{}, fmt.Errorf("name %q must be 1 to %d characters among a-z, 0-9, \"-\" and \"_\", starting with a letter or digit", fj.Name, MaxNameLength)
	}
	schedule, err := cron.ParseStandard(fj.Schedule)
	if err != nil {
		return Job{}, fmt.Errorf("%s: schedule %q: %w", fj.Name, fj.Schedule, err)
	}
	if len(fj.Command) == 0 || fj.Command[0] == "" {
		return Job{}, fmt.Errorf("%s: command must list the program and its arguments", fj.Name)
	}
	dir := fj.Dir
	switch {
	case dir == "" || dir == "~":
		dir = home
	case strings.HasPrefix(dir, "~/"):
		dir = filepath.Join(home, dir[2:])
	}
	if !filepath.IsAbs(dir) {
		return Job{}, fmt.Errorf("%s: dir %q must be absolute or start with \"~/\"", fj.Name, fj.Dir)
	}
	keep, err := parseKeep(fj.Keep, defaultKeep)
	if err != nil {
		return Job{}, fmt.Errorf("%s: %w", fj.Name, err)
	}
	return Job{Name: fj.Name, Schedule: schedule, Dir: dir, Command: fj.Command, Keep: keep}, nil
}

func parseKeep(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	keep, err := time.ParseDuration(value)
	if err != nil || keep < 0 {
		return 0, fmt.Errorf("keep %q must be a duration such as \"72h\" or \"30m\"", value)
	}
	return keep, nil
}
