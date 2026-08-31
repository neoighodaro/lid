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
	if err := cli.Execute(version, commit, date); err != nil {
		os.Exit(1)
	}
}
