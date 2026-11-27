package cli

import (
	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/panes"
	"github.com/spf13/cobra"
)

func newPaneRunCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:    "pane-run <file>",
		Short:  "Run the command stored in a lid pane file",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			argv, err := panes.TakeArgv(args[0])
			if err != nil {
				return err
			}
			return a.Launch(argv)
		},
	}
}
