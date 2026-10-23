package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop this project's sandbox, leaving it there to start again",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.Backend != "" {
				if err := config.ValidateBackend(a.Backend); err != nil {
					return err
				}
			}
			path, err := config.Resolve(a.Cwd, a.ConfigPath)
			if err != nil {
				return err
			}
			sel, err := config.ReadSelection(a.Cwd)
			if err != nil {
				return err
			}
			cfg, err := a.Config()
			if err != nil {
				if sel.AppliesTo(path) {
					forget(a, func() error { return config.ForgetSelection(a.Cwd) })
					fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("forgot that this project was on %s, since its config no longer loads", sel.Backend)))
				}
				return err
			}
			if err := checkSelection(cfg); err != nil {
				return err
			}
			if cfg.Selected != "" {
				defer forget(a, cfg.Forget)
			}

			b := backend.Get(cfg.Backend)
			if b == nil {
				return failure.New("no backend named %q", cfg.Backend).At(cfg.Ref())
			}
			if err := b.EnsureAvailable(a.SandboxHost()); err != nil {
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

// forget clears the remembered backend. Failing to is worth a warning, not a
// failed stop: the session it was remembered for has already gone.
func forget(a *app.App, clear func() error) {
	if err := clear(); err != nil {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(err.Error()))
	}
}
