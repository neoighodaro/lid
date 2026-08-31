package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/spf13/cobra"
)

func versionLine(a *app.App) string {
	line := "lid version " + a.Version
	switch {
	case a.Commit != "" && a.Date != "":
		return line + " (" + a.Commit + ", " + a.Date + ")"
	case a.Commit != "":
		return line + " (" + a.Commit + ")"
	default:
		return line
	}
}

func newVersionCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.Out, versionLine(a))
			return nil
		},
	}
}
