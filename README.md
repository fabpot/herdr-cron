# herdr-cron

Runs commands on a schedule in [herdr-core](https://github.com/herdrdev/herdr-core)
terminals, where the Herdr app shows them.

Each run is a new tab in a dedicated app space. A supervisor runs the command
in that tab, reports its outcome as an OSC 7501 program status (`done` or
`error` with the exit code), and keeps the tab open for a while so you can read
what happened. Programs that report their own OSC 7501 status, such as coding
agents, show it while they run.

## Install

```
go install github.com/fabpot/herdr-cron@latest
```

`hc` must be in `PATH` (herdr-core installs it in `~/.local/bin`).

## Configure

`~/.config/herdr-cron/config.toml`:

```toml
# How long a finished run stays visible, unless a job sets its own (default: 72h)
keep = "72h"

[[jobs]]
name = "nightly-review"
schedule = "0 3 * * 1-5"
dir = "~/src/app"
command = ["/home/me/.local/bin/claude", "-p", "Review the commits merged yesterday and report any bug you find."]

[[jobs]]
name = "weather"
schedule = "@hourly"
command = ["/usr/bin/curl", "-fsS", "https://wttr.in/Paris?format=3"]
keep = "2h"
```

* `name`: 1 to 20 characters among `a-z`, `0-9`, `-` and `_`.
* `schedule`: a five-field cron expression, a descriptor such as `@daily` or
  `@every 6h`, optionally prefixed with `CRON_TZ=Europe/Paris`.
* `dir`: where the command runs (default: your home directory).
* `command`: the program and its arguments; prefer absolute paths, as the
  command gets herdr-core's environment.
* `keep`: how long this job's finished runs stay visible, overriding the
  global `keep`.

A job is skipped while its previous run is still running.

## Run

Start the scheduler as a systemd user service (`~/.config/systemd/user/herdr-cron.service`):

```ini
[Unit]
Description=herdr-cron scheduler

[Service]
ExecStart=%h/go/bin/herdr-cron serve
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Restart=on-failure

[Install]
WantedBy=default.target
```

```
systemctl --user enable --now herdr-cron
```

Restart it after changing the configuration. Enable lingering
(`loginctl enable-linger`) to keep it running after you log out.

Start a run by hand with `herdr-cron run <job>`.

## In the Herdr app

Runs land in a space that the app creates on the first run; rename it once
(for instance "Scheduled") to recognize it. Its first tab is pinned: it asks
you not to close it, because the app deletes a space, name included, when its
last tab closes. If it is closed anyway, or turned into a plain shell when
herdr-core restarts, it comes back with the next run. To use an existing space instead,
set `space` to its id, read from the `herdr.space` label of one of its
terminals (`hc get <terminal>`).

A finished run's tab closes once `keep` is over; close it earlier from the
app.
