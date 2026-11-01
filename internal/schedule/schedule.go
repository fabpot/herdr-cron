// Package schedule starts jobs in herdr-core terminals, on time or by hand.
package schedule

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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

	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
)

// ErrRunning is returned when the previous run of a job is still running.
var ErrRunning = errors.New("the previous run is still running")

// Terminals is what the launcher needs from herdr-core.
type Terminals interface {
	Start(ctx context.Context, o hc.StartOptions) (string, error)
	List(ctx context.Context, labels map[string]string) ([]hc.Terminal, error)
}

// Launcher starts runs as tabs of a Herdr app space, each under a
// supervisor that keeps the tab open once the command ends.
type Launcher struct {
	Terminals Terminals
	Space     string
	Keep      time.Duration
	// Supervisor is the argv prefix running the supervisor; the job name,
	// keep duration and command are appended.
	Supervisor []string
	Now        func() time.Time
	NewID      func() string
}

// NewLauncher wires a launcher for the configuration.
func NewLauncher(cfg config.Config, terminals Terminals, supervisor []string) *Launcher {
	return &Launcher{
		Terminals:  terminals,
		Space:      cfg.Space,
		Keep:       cfg.Keep,
		Supervisor: supervisor,
		Now:        time.Now,
		NewID:      randomID,
	}
}

// Launch starts a run of the job and returns its terminal address, or
// ErrRunning when its previous run has not finished.
func (l *Launcher) Launch(ctx context.Context, job config.Job) (string, error) {
	running, err := l.Terminals.List(ctx, map[string]string{LabelJob: job.Name, LabelState: StateRunning})
	if err != nil {
		return "", err
	}
	if len(running) > 0 {
		return "", fmt.Errorf("%s: %w in %s", job.Name, ErrRunning, running[0].Address)
	}

	argv := append(append([]string{}, l.Supervisor...), "--job", job.Name, "--keep", l.Keep.String(), "--")
	return l.Terminals.Start(ctx, hc.StartOptions{
		Name: job.Name + "-" + l.Now().Format("0102-150405"),
		Dir:  job.Dir,
		Labels: map[string]string{
			// An app space and tab id adopt the terminal as a new tab there.
			"herdr.space": l.Space,
			"herdr.tab":   "tb" + l.NewID(),
			LabelJob:      job.Name,
			LabelState:    StateRunning,
		},
		Argv: append(argv, job.Command...),
	})
}

// Serve launches every job on its schedule until ctx is done.
func Serve(ctx context.Context, jobs []config.Job, l *Launcher, log zerolog.Logger) error {
	c := cron.New()
	for _, job := range jobs {
		if _, err := c.AddFunc(job.Schedule, func() {
			addr, err := l.Launch(ctx, job)
			if err != nil {
				log.Error().Err(err).Str("job", job.Name).Msg("run not started")
				return
			}
			log.Info().Str("job", job.Name).Str("terminal", addr).Msg("run started")
		}); err != nil {
			return fmt.Errorf("%s: %w", job.Name, err)
		}
	}
	c.Start()
	log.Info().Int("jobs", len(jobs)).Msg("scheduler started")
	<-ctx.Done()
	<-c.Stop().Done()
	return nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
