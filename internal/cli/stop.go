package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop this project's sandbox, leaving it there to start again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, b, err := lookUpBackend(a)
			if err != nil {
				return err
			}

			state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{})
			if err != nil {
				return err
			}
			if !state.Exists {
				fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("there is no %s sandbox to stop", cfg.Name)))
				return nil
			}
			if !state.Running {
				fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("the %s sandbox is not running", cfg.Name)))
				return nil
			}

			if err := b.Stop(a.SandboxHost(), cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, ui.Success.Render("Stopped "+cfg.Name))
			return nil
		},
	}
}
