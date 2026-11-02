// Package hc drives herdr-core through its hc command line.
package hc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"
)

// Runner runs a program and returns its standard output.
type Runner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Client talks to the local herdr-core.
type Client struct {
	Bin    string
	Runner Runner
}

// New returns a client running the hc binary at bin.
func New(bin string) *Client {
	return &Client{Bin: bin, Runner: execRunner{}}
}

// StartOptions describes a terminal to start.
type StartOptions struct {
	Name   string
	Dir    string
	Labels map[string]string
	Argv   []string
}

// notFoundExit is the exit code of hc when the terminal does not exist.
const notFoundExit = 3

type exitCoder interface{ ExitCode() int }

// Terminal is a live terminal, as listed by hc. Only Get fills Argv.
type Terminal struct {
	Address string   `json:"address"`
	Argv    []string `json:"argv"`
}

// Start runs argv in a new terminal without a parent, removed once it exits,
// and returns its address.
func (c *Client) Start(ctx context.Context, o StartOptions) (string, error) {
	args := []string{"run", "--bg", "--no-parent", "--rm", "--json", "--cwd", o.Dir}
	if o.Name != "" {
		args = append(args, "--name", o.Name)
	}
	args = append(args, labelArgs(o.Labels)...)
	args = append(args, "--")
	args = append(args, o.Argv...)
	var res struct {
		Address string `json:"address"`
	}
	if err := c.call(ctx, &res, args...); err != nil {
		return "", err
	}
	if res.Address == "" {
		return "", fmt.Errorf("hc run: no terminal address in the answer")
	}
	return res.Address, nil
}

// List returns the live terminals carrying all the given labels.
func (c *Client) List(ctx context.Context, labels map[string]string) ([]Terminal, error) {
	var res struct {
		Terminals []Terminal `json:"terminals"`
	}
	if err := c.call(ctx, &res, append([]string{"ls", "--json"}, labelArgs(labels)...)...); err != nil {
		return nil, err
	}
	return res.Terminals, nil
}

// Get returns a terminal with the argv of its program.
func (c *Client) Get(ctx context.Context, address string) (Terminal, error) {
	var t Terminal
	err := c.call(ctx, &t, "get", "--json", address)
	return t, err
}

// Kill ends a terminal's program; a terminal already gone is not an error.
func (c *Client) Kill(ctx context.Context, address string) error {
	err := c.call(ctx, nil, "kill", "--json", address)
	// hc reports a terminal started with --rm as not found once killed.
	var e exitCoder
	if errors.As(err, &e) && e.ExitCode() == notFoundExit {
		return nil
	}
	return err
}

// Label sets labels on a terminal.
func (c *Client) Label(ctx context.Context, address string, labels map[string]string) error {
	return c.call(ctx, nil, append([]string{"label", "--json", address}, pairs(labels)...)...)
}

func (c *Client) call(ctx context.Context, out any, args ...string) error {
	data, err := c.Runner.Output(ctx, c.Bin, args...)
	if err != nil {
		return fmt.Errorf("hc %s: %w", args[0], err)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("hc %s: unexpected answer %q: %w", args[0], data, err)
	}
	return nil
}

func labelArgs(labels map[string]string) []string {
	var args []string
	for _, p := range pairs(labels) {
		args = append(args, "--label", p)
	}
	return args
}

func pairs(labels map[string]string) []string {
	var pairs []string
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		pairs = append(pairs, k+"="+labels[k])
	}
	return pairs
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}
