package hc

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"
)

type fakeRunner struct {
	call   []string
	output string
	err    error
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.call = append([]string{name}, args...)
	return []byte(f.output), f.err
}

func TestStart(t *testing.T) {
	r := &fakeRunner{output: `{"address":"t7","state":"live"}`}
	c := &Client{Bin: "/bin/hc", Runner: r}
	addr, err := c.Start(t.Context(), StartOptions{
		Name:   "job-1",
		Dir:    "/src",
		Labels: map[string]string{"b": "2", "a": "1"},
		Argv:   []string{"sh", "-c", "--label x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if addr != "t7" {
		t.Errorf("address = %q, want t7", addr)
	}
	want := []string{"/bin/hc", "run", "--bg", "--no-parent", "--rm", "--json", "--cwd", "/src", "--name", "job-1", "--label", "a=1", "--label", "b=2", "--", "sh", "-c", "--label x"}
	if !slices.Equal(r.call, want) {
		t.Errorf("call = %q\nwant  %q", r.call, want)
	}
}

func TestList(t *testing.T) {
	r := &fakeRunner{output: `{"hidden_finished":0,"terminals":[{"address":"peer/t3","state":"live"},{"address":"t3","state":"live","turn":0},{"address":"other/t4","state":"live"}]}`}
	c := &Client{Bin: "hc", Runner: r}
	terms, err := c.List(t.Context(), map[string]string{"job": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 1 || terms[0].Address != "t3" {
		t.Errorf("terminals = %+v", terms)
	}
	if want := []string{"hc", "ls", "--json", "--label", "job=a"}; !slices.Equal(r.call, want) {
		t.Errorf("call = %q, want %q", r.call, want)
	}
}

func TestLabel(t *testing.T) {
	r := &fakeRunner{output: "not json, ignored"}
	c := &Client{Bin: "hc", Runner: r}
	if err := c.Label(t.Context(), "t3", map[string]string{"state": "done", "exit": "0"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"hc", "label", "--json", "t3", "exit=0", "state=done"}; !slices.Equal(r.call, want) {
		t.Errorf("call = %q, want %q", r.call, want)
	}
}

func TestGet(t *testing.T) {
	r := &fakeRunner{output: `{"address":"t3","argv":["/usr/bin/fish"],"keep":true,"state":"live"}`}
	c := &Client{Bin: "hc", Runner: r}
	term, err := c.Get(t.Context(), "t3")
	if err != nil {
		t.Fatal(err)
	}
	if term.Address != "t3" || !slices.Equal(term.Argv, []string{"/usr/bin/fish"}) {
		t.Errorf("terminal = %+v", term)
	}
	if want := []string{"hc", "get", "--json", "t3"}; !slices.Equal(r.call, want) {
		t.Errorf("call = %q, want %q", r.call, want)
	}
}

type exitError int

func (e exitError) Error() string { return "exit status " + strconv.Itoa(int(e)) }
func (e exitError) ExitCode() int { return int(e) }

func TestKillIgnoresATerminalAlreadyGone(t *testing.T) {
	c := &Client{Bin: "hc", Runner: &fakeRunner{err: fmt.Errorf("%w: t3 was removed", exitError(3))}}
	if err := c.Kill(t.Context(), "t3"); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	c.Runner = &fakeRunner{err: exitError(1)}
	if err := c.Kill(t.Context(), "t3"); err == nil {
		t.Error("Kill hid a failure")
	}
}
