// Package schedule starts jobs in herdr-core terminals, on time or by hand.
package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/fabpot/herdr-cron/internal/config"
	"github.com/fabpot/herdr-cron/internal/hc"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
)

// Labels set on every run terminal.
const (
	LabelJob   = "herdr-cron.job"
	LabelState = "herdr-cron.state"
	LabelExit  = "herdr-cron.exit"
	LabelPin   = "herdr-cron.pin"

	StateWorking = "working"
	StateDone    = "done"
	StateError   = "error"
)

// ErrRunning is returned when the previous run of a job is still running.
var ErrRunning = errors.New("the previous run is still running")

// Terminals is what the launcher needs from herdr-core.
type Terminals interface {
	Start(ctx context.Context, o hc.StartOptions) (string, error)
	List(ctx context.Context, labels map[string]string) ([]hc.Terminal, error)
	Get(ctx context.Context, address string) (hc.Terminal, error)
	Kill(ctx context.Context, address string) error
}

// Launcher starts runs as tabs of a Herdr app space, each under a
// supervisor that keeps the tab open once the command ends.
type Launcher struct {
	Terminals Terminals
	Space     string
	// Supervisor is the argv prefix running the supervisor; the job name,
	// keep duration and command are appended.
	Supervisor []string
	// Pin is the argv of the program holding the space's pinned tab.
	Pin    []string
	PinDir string
	Now    func() time.Time
	NewID  func() string
}

// NewLauncher wires a launcher for the configuration; self is the
// herdr-cron executable, hcBin the hc one.
func NewLauncher(cfg config.Config, terminals Terminals, self, hcBin, home string) *Launcher {
	return &Launcher{
		Terminals:  terminals,
		Space:      cfg.Space,
		Supervisor: []string{self, "supervise", "--hc", hcBin},
		Pin:        []string{self, "pin"},
		PinDir:     home,
		Now:        time.Now,
		NewID:      randomID,
	}
}

// EnsurePin starts the space's pinned tab unless it is running. The app
// deletes a space, name included, when its last tab closes: the pinned tab
// keeps it. A pinned tab no longer running the pin program, like a kept
// terminal herdr-core gave a shell when it restarted, is replaced: a new pin
// starts first, and the stale one is killed by the next call. Extra pins,
// started by concurrent launches, are killed too.
func (l *Launcher) EnsurePin(ctx context.Context) error {
	labels := map[string]string{LabelPin: "true", "herdr.space": l.Space}
	pins, err := l.Terminals.List(ctx, labels)
	if err != nil {
		return err
	}
	running := false
	var stale []string
	for _, p := range pins {
		t, err := l.Terminals.Get(ctx, p.Address)
		if err != nil {
			return err
		}
		if !running && slices.Equal(t.Argv, l.Pin) {
			running = true
		} else {
			stale = append(stale, p.Address)
		}
	}
	if !running {
		labels = maps.Clone(labels)
		labels["herdr.tab"] = "tb" + l.NewID()
		// Stale pins wait for a later call: the app adopts a new tab some time
		// after it starts, and killing them now would empty the space.
		_, err := l.Terminals.Start(ctx, hc.StartOptions{Dir: l.PinDir, Labels: labels, Argv: l.Pin})
		return err
	}
	for _, addr := range stale {
		if err := l.Terminals.Kill(ctx, addr); err != nil {
			return err
		}
	}
	return nil
}

// Launch starts a run of the job, after the space's pinned tab, and returns
// its terminal address, or ErrRunning when its previous run has not finished.
func (l *Launcher) Launch(ctx context.Context, job config.Job) (string, error) {
	if err := l.EnsurePin(ctx); err != nil {
		return "", err
	}
	running, err := l.Terminals.List(ctx, map[string]string{LabelJob: job.Name, LabelState: StateWorking})
	if err != nil {
		return "", err
	}
	if len(running) > 0 {
		return "", fmt.Errorf("%s: %w in %s", job.Name, ErrRunning, running[0].Address)
	}

	return l.Terminals.Start(ctx, hc.StartOptions{
		Name: job.Name + "-" + l.Now().Format("0102-150405"),
		Dir:  job.Dir,
		Labels: map[string]string{
			// An app space and tab id adopt the terminal as a new tab there.
			"herdr.space": l.Space,
			"herdr.tab":   "tb" + l.NewID(),
			LabelJob:      job.Name,
			LabelState:    StateWorking,
		},
		Argv: slices.Concat(l.Supervisor, []string{"--job", job.Name, "--keep", job.Keep.String(), "--"}, job.Command),
	})
}

// Serve launches every job on its schedule until ctx is done.
func Serve(ctx context.Context, jobs []config.Job, l *Launcher, log zerolog.Logger) {
	c := cron.New()
	for _, job := range jobs {
		c.Schedule(job.Schedule, cron.FuncJob(func() {
			addr, err := l.Launch(ctx, job)
			if err != nil {
				log.Error().Err(err).Str("job", job.Name).Msg("run not started")
				return
			}
			log.Info().Str("job", job.Name).Str("terminal", addr).Msg("run started")
		}))
	}
	c.Start()
	log.Info().Int("jobs", len(jobs)).Msg("scheduler started")
	<-ctx.Done()
	<-c.Stop().Done()
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
