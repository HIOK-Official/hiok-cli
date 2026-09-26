// Command hiok is the command-line client for HIOK Cloud.
package main

import (
	"os"

	"github.com/HIOK-Official/hiok-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout))
}
