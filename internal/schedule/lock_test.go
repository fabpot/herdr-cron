package schedule

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fabpot/herdr-cron/internal/config"
	"github.com/fabpot/herdr-cron/internal/hc"
)

type pausingTerminals struct{ fakeTerminals }

func (f *pausingTerminals) Start(ctx context.Context, o hc.StartOptions) (string, error) {
	if o.Labels[LabelJob] != "" {
		_, _ = fmt.Fprintln(os.Stdout, "starting")
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
	return f.fakeTerminals.Start(ctx, o)
}

func TestLaunchSerializesProcesses(t *testing.T) {
	if home := os.Getenv("HERDR_CRON_TEST_HOME"); home != "" {
		l := NewLauncher(config.Config{Space: space}, &pausingTerminals{}, "/usr/bin/herdr-cron", "/usr/bin/hc", home)
		if _, err := l.Launch(t.Context(), job); err != nil {
			t.Fatal(err)
		}
		return
	}

	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "shell-cache"))
	terms := &fakeTerminals{}
	l := NewLauncher(config.Config{Space: space}, terms, "/usr/bin/herdr-cron", "/usr/bin/hc", home)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestLaunchSerializesProcesses$")
	cmd.Env = append(os.Environ(), "HERDR_CRON_TEST_HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, "service-cache"))
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = in.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "starting\n" {
		t.Fatalf("launch readiness = %q, err = %v", line, err)
	}

	if _, err := l.Launch(t.Context(), job); !errors.Is(err, ErrRunning) {
		t.Errorf("concurrent launch err = %v, want ErrRunning", err)
	}
	if len(terms.started) != 0 {
		t.Errorf("started %d terminals during another process's launch", len(terms.started))
	}
	other := job
	other.Name = "other"
	if _, err := l.Launch(t.Context(), other); err != nil {
		t.Fatalf("unrelated job: %v", err)
	}

	_ = in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("launch process: %v", err)
	}
	if _, err := l.Launch(t.Context(), job); err != nil {
		t.Fatalf("launch after the process exits: %v", err)
	}
}
