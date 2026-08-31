package main

import (
	"os"

	"github.com/neoighodaro/lid/internal/cli"
)

// Overridden at build time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(cli.Execute(version, commit, date))
}
