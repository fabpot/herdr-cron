// Package cli is the herdr-cron command line.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/fabpot/herdr-cron/internal/config"
	"github.com/fabpot/herdr-cron/internal/hc"
	"github.com/fabpot/herdr-cron/internal/schedule"
	"github.com/fabpot/herdr-cron/internal/supervise"
	"github.com/rs/zerolog"
	"github.com/symfony-cli/console"
	"github.com/symfony-cli/terminal"
)

// New returns the herdr-cron application.
func New(version string) *console.Application {
	return &console.Application{
		Name:    "herdr-cron",
		Usage:   "Run commands on a schedule in herdr-core terminals shown by the Herdr app",
		Version: version,
		Commands: []*console.Command{
			serveCommand(),
			runCommand(),
			superviseCommand(),
			pinCommand(),
		},
	}
}

var configFlag = &console.StringFlag{
	Name:  "config",
	Usage: "Configuration file (default: ~/.config/herdr-cron/config.toml)",
}

func serveCommand() *console.Command {
	return &console.Command{
		Name:  "serve",
		Usage: "Run the scheduler until stopped",
		Flags: []console.Flag{configFlag},
		Action: func(c *console.Context) error {
			cfg, launcher, err := setup(c)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			log := terminal.Logger
			if log.GetLevel() > zerolog.InfoLevel {
				log = log.Level(zerolog.InfoLevel)
			}
			schedule.Serve(ctx, cfg.Jobs, launcher, log)
			return nil
		},
	}
}

func runCommand() *console.Command {
	return &console.Command{
		Name:  "run",
		Usage: "Start a run of a job now",
		Flags: []console.Flag{configFlag},
		Args:  console.ArgDefinition{{Name: "job", Description: "The job name"}},
		Action: func(c *console.Context) error {
			cfg, launcher, err := setup(c)
			if err != nil {
				return err
			}
			job, err := cfg.Job(c.Args().Get("job"))
			if err != nil {
				return err
			}
			addr, err := launcher.Launch(context.Background(), job)
			if err != nil {
				return err
			}
			_, _ = terminal.Printfln("Started <info>%s</> in terminal <info>%s</>", job.Name, addr)
			return nil
		},
	}
}

func superviseCommand() *console.Command {
	return &console.Command{
		Name:        "supervise",
		Usage:       "Run a job command in its terminal (started by serve and run)",
		Hidden:      console.Hide,
		FlagParsing: console.FlagParsingSkippedAfterFirstArg,
		Flags: []console.Flag{
			&console.StringFlag{Name: "job", Required: true},
			&console.DurationFlag{Name: "keep", DefaultValue: config.DefaultKeep},
			&console.StringFlag{Name: "hc", Usage: "The hc binary recording the outcome on this terminal"},
		},
		Args: console.ArgDefinition{{Name: "command", Slice: true}},
		Action: func(c *console.Context) error {
			argv := c.Args().Tail()
			if len(argv) > 0 && argv[0] == "--" {
				argv = argv[1:]
			}
			if len(argv) == 0 {
				return fmt.Errorf("no command to run")
			}
			var labeler supervise.Labeler
			if bin := c.String("hc"); bin != "" {
				labeler = hc.New(bin)
			}
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			// Ctrl+C belongs to the command: catching it keeps it from ending the supervisor.
			signal.Notify(make(chan os.Signal, 1), os.Interrupt)

			code := supervise.New(labeler, os.Getenv("HC_TERMINAL_ID")).Run(ctx, supervise.Options{
				Job:  c.String("job"),
				Keep: c.Duration("keep"),
				Argv: argv,
			})
			if code != 0 {
				return console.Exit("", code)
			}
			return nil
		},
	}
}

const pinMessage = `herdr-cron: do not close this tab.

It keeps this space and its name: the Herdr app deletes a space when its last
tab closes. Scheduled runs open as new tabs next to it.
`

func pinCommand() *console.Command {
	return &console.Command{
		Name:   "pin",
		Usage:  "Hold the pinned tab keeping the app space open (started by serve and run)",
		Hidden: console.Hide,
		Action: func(_ *console.Context) error {
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			signal.Notify(make(chan os.Signal, 1), os.Interrupt)
			_, _ = terminal.Print(pinMessage)
			<-ctx.Done()
			return nil
		},
	}
}

func setup(c *console.Context) (config.Config, *schedule.Launcher, error) {
	path := c.String("config")
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return config.Config{}, nil, err
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return config.Config{}, nil, err
	}
	cfg, err := config.Load(path, home)
	if err != nil {
		return config.Config{}, nil, err
	}
	bin, err := exec.LookPath("hc")
	if err != nil {
		return config.Config{}, nil, fmt.Errorf("hc not found, install herdr-core or add its directory (usually ~/.local/bin) to PATH")
	}
	self, err := os.Executable()
	if err != nil {
		return config.Config{}, nil, err
	}
	launcher, err := schedule.NewLauncher(cfg, hc.New(bin), self, bin, home)
	return cfg, launcher, err
}
