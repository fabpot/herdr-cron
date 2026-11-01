// Command herdr-cron runs commands on a schedule in herdr-core terminals.
package main

import (
	"os"

	"github.com/fabpot/herdr-cron/internal/cli"
)

var version = "dev"

func main() {
	_ = cli.New(version).Run(os.Args)
}
