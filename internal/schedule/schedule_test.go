package schedule

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/fabpot/herdr-cron/internal/config"
	"github.com/fabpot/herdr-cron/internal/hc"
	"github.com/rs/zerolog"
)

type fakeTerminals struct {
	running []hc.Terminal
	pinned  []hc.Terminal
	listed  []map[string]string
	started []hc.StartOptions
}

func (f *fakeTerminals) Start(_ context.Context, o hc.StartOptions) (string, error) {
	f.started = append(f.started, o)
	return "t9", nil
}

func (f *fakeTerminals) List(_ context.Context, labels map[string]string) ([]hc.Terminal, error) {
	f.listed = append(f.listed, labels)
	if labels[LabelPin] != "" {
		return f.pinned, nil
	}
	return f.running, nil
}

func newTestLauncher(terms *fakeTerminals) *Launcher {
	return &Launcher{
		Terminals:  terms,
		Space:      "sp00000000000000000000000000000001",
		Supervisor: []string{"/usr/bin/herdr-cron", "supervise", "--hc", "/usr/bin/hc"},
		Pin:        []string{"/usr/bin/herdr-cron", "pin"},
		PinDir:     "/home/me",
		Now:        func() time.Time { return time.Date(2026, 11, 2, 3, 0, 5, 0, time.UTC) },
		NewID:      func() string { return "0123" },
	}
}

var job = config.Job{Name: "review", Schedule: "@daily", Dir: "/src/app", Command: []string{"void", "agent", "review.md"}, Keep: 48 * time.Hour}

func TestLaunch(t *testing.T) {
	terms := &fakeTerminals{pinned: []hc.Terminal{{Address: "t1"}}}
	addr, err := newTestLauncher(terms).Launch(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "t9" {
		t.Errorf("address = %q, want t9", addr)
	}
	if want := map[string]string{LabelJob: "review", LabelState: StateRunning}; !maps.Equal(terms.listed[1], want) {
		t.Errorf("overlap check listed %v, want %v", terms.listed[1], want)
	}
	if len(terms.started) != 1 {
		t.Fatalf("started %d terminals, want only the run", len(terms.started))
	}
	got := terms.started[0]
	if got.Name != "review-1102-030005" || got.Dir != "/src/app" {
		t.Errorf("name = %q, dir = %q", got.Name, got.Dir)
	}
	wantLabels := map[string]string{
		"herdr.space": "sp00000000000000000000000000000001",
		"herdr.tab":   "tb0123",
		LabelJob:      "review",
		LabelState:    StateRunning,
	}
	if !maps.Equal(got.Labels, wantLabels) {
		t.Errorf("labels = %v, want %v", got.Labels, wantLabels)
	}
	wantArgv := []string{"/usr/bin/herdr-cron", "supervise", "--hc", "/usr/bin/hc", "--job", "review", "--keep", "48h0m0s", "--", "void", "agent", "review.md"}
	if !slices.Equal(got.Argv, wantArgv) {
		t.Errorf("argv = %q\nwant   %q", got.Argv, wantArgv)
	}
}

func TestLaunchSkipsARunningJob(t *testing.T) {
	terms := &fakeTerminals{running: []hc.Terminal{{Address: "t4"}}, pinned: []hc.Terminal{{Address: "t1"}}}
	_, err := newTestLauncher(terms).Launch(t.Context(), job)
	if !errors.Is(err, ErrRunning) {
		t.Errorf("err = %v, want ErrRunning", err)
	}
	if len(terms.started) != 0 {
		t.Error("a run started while the previous one was running")
	}
}

func TestLaunchStartsTheMissingPinFirst(t *testing.T) {
	terms := &fakeTerminals{}
	if _, err := newTestLauncher(terms).Launch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if len(terms.started) != 2 {
		t.Fatalf("started %d terminals, want the pin and the run", len(terms.started))
	}
	if want := map[string]string{LabelPin: "true", "herdr.space": "sp00000000000000000000000000000001"}; !maps.Equal(terms.listed[0], want) {
		t.Errorf("pin check listed %v, want %v", terms.listed[0], want)
	}
	pin := terms.started[0]
	wantLabels := map[string]string{LabelPin: "true", "herdr.space": "sp00000000000000000000000000000001", "herdr.tab": "tb0123"}
	if !maps.Equal(pin.Labels, wantLabels) || pin.Dir != "/home/me" || pin.Name != "" || !slices.Equal(pin.Argv, []string{"/usr/bin/herdr-cron", "pin"}) {
		t.Errorf("pin = %+v", pin)
	}
	if terms.started[1].Labels[LabelJob] != "review" {
		t.Errorf("second terminal = %+v, want the run", terms.started[1])
	}
}

func TestServeStartsThePin(t *testing.T) {
	terms := &fakeTerminals{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Serve(ctx, nil, newTestLauncher(terms), zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if len(terms.started) != 1 || terms.started[0].Labels[LabelPin] != "true" {
		t.Errorf("started %+v, want the pin", terms.started)
	}
}

func TestServeStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() { done <- Serve(ctx, []config.Job{job}, newTestLauncher(&fakeTerminals{}), zerolog.Nop()) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after the context was canceled")
	}
}

func TestServeRejectsAnInvalidSchedule(t *testing.T) {
	bad := job
	bad.Schedule = "never"
	if err := Serve(t.Context(), []config.Job{bad}, newTestLauncher(&fakeTerminals{}), zerolog.Nop()); err == nil {
		t.Error("Serve accepted an invalid schedule")
	}
}
