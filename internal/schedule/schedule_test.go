package schedule

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/fabpot/herdr-cron/internal/config"
	"github.com/fabpot/herdr-cron/internal/hc"
)

type fakeTerminal struct {
	hc.Terminal
	labels map[string]string
}

type fakeTerminals struct {
	terms   []fakeTerminal
	started []hc.StartOptions
	ops     []string
}

func (f *fakeTerminals) Start(_ context.Context, o hc.StartOptions) (string, error) {
	f.started = append(f.started, o)
	f.ops = append(f.ops, "start")
	addr := "s" + strconv.Itoa(len(f.started))
	f.terms = append(f.terms, fakeTerminal{hc.Terminal{Address: addr, Argv: o.Argv}, o.Labels})
	return addr, nil
}

func (f *fakeTerminals) Get(_ context.Context, address string) (hc.Terminal, error) {
	for _, t := range f.terms {
		if t.Address == address {
			return t.Terminal, nil
		}
	}
	return hc.Terminal{}, errors.New("no terminal " + address)
}

func (f *fakeTerminals) Kill(_ context.Context, address string) error {
	f.ops = append(f.ops, "kill "+address)
	f.terms = slices.DeleteFunc(f.terms, func(t fakeTerminal) bool { return t.Address == address })
	return nil
}

func (f *fakeTerminals) List(_ context.Context, labels map[string]string) ([]hc.Terminal, error) {
	var list []hc.Terminal
	for _, t := range f.terms {
		matches := true
		for k, v := range labels {
			matches = matches && t.labels[k] == v
		}
		if matches {
			list = append(list, hc.Terminal{Address: t.Address})
		}
	}
	return list, nil
}

const space = "sp00000000000000000000000000000001"

var pinArgv = []string{"/usr/bin/herdr-cron", "pin"}

func pinTab(addr, space string, argv ...string) fakeTerminal {
	return fakeTerminal{hc.Terminal{Address: addr, Argv: argv}, map[string]string{LabelPin: "true", "herdr.space": space}}
}

func runTab(addr, job, state string, argv ...string) fakeTerminal {
	return fakeTerminal{hc.Terminal{Address: addr, Argv: argv}, map[string]string{LabelJob: job, LabelState: state}}
}

func newTestLauncher(t *testing.T, terms Terminals) *Launcher {
	t.Helper()
	return &Launcher{
		Terminals:  terms,
		Space:      space,
		Supervisor: []string{"/usr/bin/herdr-cron", "supervise", "--hc", "/usr/bin/hc"},
		Pin:        pinArgv,
		PinDir:     "/home/me",
		Now:        func() time.Time { return time.Date(2026, 11, 2, 3, 0, 5, 0, time.UTC) },
		NewID:      func() string { return "0123" },
		LockDir:    t.TempDir(),
	}
}

var job = config.Job{Name: "review", Dir: "/src/app", Command: []string{"void", "agent", "review.md"}, Keep: 48 * time.Hour}

func TestLaunch(t *testing.T) {
	terms := &fakeTerminals{terms: []fakeTerminal{pinTab("t1", space, pinArgv...)}}
	addr, err := newTestLauncher(t, terms).Launch(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "s1" {
		t.Errorf("address = %q, want s1", addr)
	}
	if len(terms.started) != 1 {
		t.Fatalf("started %d terminals, want only the run", len(terms.started))
	}
	got := terms.started[0]
	if got.Name != "review-1102-030005" || got.Dir != "/src/app" {
		t.Errorf("name = %q, dir = %q", got.Name, got.Dir)
	}
	wantLabels := map[string]string{
		"herdr.space": space,
		"herdr.tab":   "tb0123",
		LabelJob:      "review",
		LabelState:    StateWorking,
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
	terms := &fakeTerminals{terms: []fakeTerminal{
		pinTab("t1", space, pinArgv...),
		runTab("t2", "other", StateWorking),
		runTab("t3", "review", StateDone),
		runTab("t4", "review", StateWorking, "/usr/bin/fish"),
	}}
	l := newTestLauncher(t, terms)
	if _, err := l.Launch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	l.Supervisor[0] = "/new/bin/herdr-cron"
	l.Supervisor[3] = "/new/bin/hc"
	if _, err := l.Launch(t.Context(), job); !errors.Is(err, ErrRunning) {
		t.Errorf("err = %v, want ErrRunning", err)
	}
	if want := []string{"start"}; !slices.Equal(terms.ops, want) {
		t.Errorf("ops = %q, want %q", terms.ops, want)
	}
}

func TestLaunchStartsTheMissingPinFirst(t *testing.T) {
	terms := &fakeTerminals{terms: []fakeTerminal{pinTab("t1", "sp00000000000000000000000000000002", pinArgv...)}}
	if _, err := newTestLauncher(t, terms).Launch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if len(terms.started) != 2 {
		t.Fatalf("started %d terminals, want the pin and the run", len(terms.started))
	}
	pin := terms.started[0]
	wantLabels := map[string]string{LabelPin: "true", "herdr.space": space, "herdr.tab": "tb0123"}
	if !maps.Equal(pin.Labels, wantLabels) || pin.Dir != "/home/me" || pin.Name != "" || !slices.Equal(pin.Argv, pinArgv) {
		t.Errorf("pin = %+v", pin)
	}
	if terms.started[1].Labels[LabelJob] != "review" {
		t.Errorf("second terminal = %+v, want the run", terms.started[1])
	}
}

func TestLaunchReplacesAPinNoLongerRunningThePinProgram(t *testing.T) {
	// herdr-core gives a kept terminal a shell when it restarts.
	terms := &fakeTerminals{terms: []fakeTerminal{pinTab("t2", space, "/usr/bin/fish")}}
	l := newTestLauncher(t, terms)
	if _, err := l.Launch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if want := []string{"start", "start"}; !slices.Equal(terms.ops, want) {
		t.Fatalf("ops = %q, want %q", terms.ops, want)
	}
	if got := terms.started[0]; got.Labels[LabelPin] != "true" || !slices.Equal(got.Argv, pinArgv) {
		t.Errorf("first terminal = %+v, want the pin", got)
	}

	terms.ops = nil
	other := job
	other.Name = "other"
	if _, err := l.Launch(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	if want := []string{"kill t2", "start"}; !slices.Equal(terms.ops, want) {
		t.Errorf("next launch ops = %q, want %q", terms.ops, want)
	}
}

func TestLaunchKillsADuplicatePin(t *testing.T) {
	terms := &fakeTerminals{terms: []fakeTerminal{pinTab("t1", space, pinArgv...), pinTab("t2", space, pinArgv...)}}
	if _, err := newTestLauncher(t, terms).Launch(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if want := []string{"kill t2", "start"}; !slices.Equal(terms.ops, want) {
		t.Errorf("ops = %q, want %q", terms.ops, want)
	}
}
