package supervise

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/fabpot/herdr-cron/internal/schedule"
)

type fakeLabeler struct {
	address string
	labels  map[string]string
	err     error
}

func (f *fakeLabeler) Label(_ context.Context, address string, labels map[string]string) error {
	f.address, f.labels = address, labels
	return f.err
}

type harness struct {
	sup      *Supervisor
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	labeler  *fakeLabeler
	lingered time.Duration
}

func newHarness() *harness {
	h := &harness{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, labeler: &fakeLabeler{}, lingered: -1}
	now := time.Date(2026, 11, 2, 3, 0, 0, 0, time.UTC)
	h.sup = &Supervisor{
		Stdin:    strings.NewReader(""),
		Stdout:   h.stdout,
		Stderr:   h.stderr,
		Labeler:  h.labeler,
		Terminal: "t9",
		Now: func() time.Time {
			now = now.Add(time.Minute)
			return now
		},
		Linger: func(_ context.Context, d time.Duration) { h.lingered = d },
	}
	return h
}

func status(state, title, msg string) string {
	s := "\x1b]7501;state=" + state + ":app=herdr-cron:title=" + base64.StdEncoding.EncodeToString([]byte(title))
	if msg != "" {
		s += ":msg=" + base64.StdEncoding.EncodeToString([]byte(msg))
	}
	return s + "\x1b\\"
}

func TestRun(t *testing.T) {
	tests := []struct {
		name     string
		argv     []string
		code     int
		state    string
		outcome  string
		inStdout string
		inStderr string
	}{
		{"success", []string{"sh", "-c", "echo hello"}, 0, schedule.StateDone, "exit 0 after 1m0s", "hello\n", ""},
		{"failure", []string{"sh", "-c", "echo oops >&2; exit 3"}, 3, schedule.StateError, "exit 3 after 1m0s", "", "oops\n"},
		{"signaled", []string{"sh", "-c", "kill -TERM $$"}, 143, schedule.StateError, "exit 143 after 1m0s", "", ""},
		{"not found", []string{"/nonexistent/program"}, exitNotStarted, schedule.StateError, "exit 127 after 1m0s", "", "/nonexistent/program"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			code := h.sup.Run(t.Context(), Options{Job: "review", Keep: 2 * time.Hour, Argv: tt.argv})
			if code != tt.code {
				t.Errorf("code = %d, want %d", code, tt.code)
			}
			out := h.stdout.String()
			if !strings.HasPrefix(out, status("working", "review", "")) {
				t.Errorf("stdout does not start with the working status: %q", out)
			}
			final := "\x1b]7501;state=clear\x1b\\" + status(tt.state, "review", tt.outcome)
			if !strings.Contains(out, final) {
				t.Errorf("stdout lacks the final status %q: %q", final, out)
			}
			if !strings.Contains(out, tt.inStdout) || !strings.Contains(h.stderr.String(), tt.inStderr) {
				t.Errorf("stdout = %q, stderr = %q", out, h.stderr.String())
			}
			if !strings.Contains(out, "review: "+tt.outcome+"; this tab closes on Mon Nov 2 05:03") {
				t.Errorf("stdout lacks the summary: %q", out)
			}
			wantLabels := map[string]string{schedule.LabelState: tt.state}
			if h.labeler.address != "t9" || !maps.Equal(h.labeler.labels, wantLabels) {
				t.Errorf("labeled %s with %v, want t9 with %v", h.labeler.address, h.labeler.labels, wantLabels)
			}
			if h.lingered != 2*time.Hour {
				t.Errorf("lingered %s, want 2h", h.lingered)
			}
		})
	}
}

func TestRunWithoutLabeler(t *testing.T) {
	h := newHarness()
	h.sup.Labeler = nil
	if code := h.sup.Run(t.Context(), Options{Job: "a", Keep: time.Hour, Argv: []string{"true"}}); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if h.lingered != time.Hour {
		t.Errorf("lingered %s, want 1h", h.lingered)
	}
}

func TestRunReportsALabelingFailure(t *testing.T) {
	h := newHarness()
	h.labeler.err = errors.New("hc label: core unreachable")
	if code := h.sup.Run(t.Context(), Options{Job: "a", Keep: time.Hour, Argv: []string{"true"}}); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(h.stderr.String(), "core unreachable") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
	if h.lingered != time.Hour {
		t.Errorf("lingered %s, want 1h", h.lingered)
	}
}

func TestCancelStopsTheCommandWithoutLingering(t *testing.T) {
	h := newHarness()
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	code := h.sup.Run(ctx, Options{Job: "a", Keep: time.Hour, Argv: []string{"sleep", "30"}})
	if time.Since(start) > 10*time.Second {
		t.Fatal("the command was not stopped")
	}
	if code != 143 {
		t.Errorf("code = %d, want 143", code)
	}
	if h.lingered != -1 || h.labeler.labels != nil {
		t.Error("a canceled run lingered or recorded an outcome")
	}
}
