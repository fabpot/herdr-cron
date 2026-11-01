package hc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls  [][]string
	output string
	err    error
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
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
	if !slices.Equal(r.calls[0], want) {
		t.Errorf("call = %q\nwant  %q", r.calls[0], want)
	}
}

func TestStartWithoutAddress(t *testing.T) {
	c := &Client{Bin: "hc", Runner: &fakeRunner{output: `{}`}}
	if _, err := c.Start(t.Context(), StartOptions{Dir: "/", Argv: []string{"true"}}); err == nil {
		t.Error("Start succeeded without an address")
	}
}

func TestList(t *testing.T) {
	r := &fakeRunner{output: `{"hidden_finished":0,"terminals":[{"address":"t3","state":"live","turn":0}]}`}
	c := &Client{Bin: "hc", Runner: r}
	terms, err := c.List(t.Context(), map[string]string{"job": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 1 || terms[0].Address != "t3" {
		t.Errorf("terminals = %+v", terms)
	}
	if want := []string{"hc", "ls", "--json", "--label", "job=a"}; !slices.Equal(r.calls[0], want) {
		t.Errorf("call = %q, want %q", r.calls[0], want)
	}
}

func TestLabel(t *testing.T) {
	r := &fakeRunner{output: "not json, ignored"}
	c := &Client{Bin: "hc", Runner: r}
	if err := c.Label(t.Context(), "t3", map[string]string{"state": "done", "exit": "0"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"hc", "label", "--json", "t3", "exit=0", "state=done"}; !slices.Equal(r.calls[0], want) {
		t.Errorf("call = %q, want %q", r.calls[0], want)
	}
}

func TestErrorsNameTheCommand(t *testing.T) {
	c := &Client{Bin: "hc", Runner: &fakeRunner{err: errors.New("core unreachable")}}
	_, err := c.List(t.Context(), nil)
	if err == nil || !strings.HasPrefix(err.Error(), "hc ls: core unreachable") {
		t.Errorf("err = %v", err)
	}
	c.Runner = &fakeRunner{output: "oops"}
	if _, err := c.List(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "unexpected answer") {
		t.Errorf("err = %v", err)
	}
}
