// Package supervise runs a job's command in its terminal, reports its outcome
// through OSC 7501, and keeps the terminal alive so its tab stays visible.
package supervise

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fabpot/herdr-cron/internal/schedule"
)

// App identifies herdr-cron in program status reports.
const App = "herdr-cron"

// ExitNotStarted is the exit code when the command cannot be started.
const ExitNotStarted = 127

// Labeler sets labels on a terminal.
type Labeler interface {
	Label(ctx context.Context, address string, labels map[string]string) error
}

// Supervisor runs one command.
type Supervisor struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Labeler records the outcome on Terminal; both are optional.
	Labeler  Labeler
	Terminal string
	Now      func() time.Time
	// Linger waits for d or until ctx is done.
	Linger func(ctx context.Context, d time.Duration)
}

// New returns a supervisor attached to the process's standard streams.
func New(labeler Labeler, terminal string) *Supervisor {
	return &Supervisor{
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Labeler:  labeler,
		Terminal: terminal,
		Now:      time.Now,
		Linger:   linger,
	}
}

// Options describes a supervised run.
type Options struct {
	Job  string
	Keep time.Duration
	Argv []string
}

// Run runs the command and returns its exit code once the keep duration is
// over. Canceling ctx stops the command and returns at once.
func (s *Supervisor) Run(ctx context.Context, o Options) int {
	started := s.Now()
	s.report(schedule.StateWorking, o.Job, "")

	code := s.exec(ctx, o.Argv)
	if ctx.Err() != nil {
		return code
	}

	state := schedule.StateDone
	if code != 0 {
		state = schedule.StateError
	}
	outcome := fmt.Sprintf("exit %d after %s", code, s.Now().Sub(started).Round(time.Second))
	// Drop the records the command reported so they do not outlive it.
	_, _ = io.WriteString(s.Stdout, "\x1b]7501;state=clear\x1b\\")
	s.report(state, o.Job, outcome)
	if s.Labeler != nil && s.Terminal != "" {
		if err := s.Labeler.Label(ctx, s.Terminal, map[string]string{schedule.LabelState: state, schedule.LabelExit: strconv.Itoa(code)}); err != nil {
			_, _ = fmt.Fprintf(s.Stderr, "herdr-cron: %v\n", err)
		}
	}

	_, _ = fmt.Fprintf(s.Stdout, "\nherdr-cron: %s: %s; this tab closes on %s\n", o.Job, outcome, s.Now().Add(o.Keep).Format("Mon Jan 2 15:04"))
	s.Linger(ctx, o.Keep)
	return code
}

func (s *Supervisor) exec(ctx context.Context, argv []string) int {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s.Stdin, s.Stdout, s.Stderr
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(s.Stderr, "herdr-cron: %v\n", err)
		return ExitNotStarted
	}

	err := cmd.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return exitErr.ExitCode()
}

func (s *Supervisor) report(state, title, msg string) {
	var b strings.Builder
	b.WriteString("\x1b]7501;state=" + state + ":app=" + App)
	b.WriteString(":title=" + base64.StdEncoding.EncodeToString([]byte(title)))
	if msg != "" {
		b.WriteString(":msg=" + base64.StdEncoding.EncodeToString([]byte(msg)))
	}
	b.WriteString("\x1b\\")
	_, _ = io.WriteString(s.Stdout, b.String())
}

func linger(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
