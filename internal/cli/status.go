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

func newStatusCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether this project's sandbox exists and is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, b, err := lookUpBackend(a)
			if err != nil {
				return err
			}

			state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{Stale: true})
			if err != nil {
				return err
			}

			rows := [][2]string{
				{"sandbox", cfg.Name},
				{"backend", backendRow(cfg)},
				{"agent", cfg.Agent},
				{"state", stateName(state)},
			}
			printRows(a.Out, append(rows, state.Rows...))

			if !state.Exists {
				fmt.Fprintln(a.Err, ui.Err.Info.Render("run 'lid start' to create it"))
			}
			if state.Stale {
				fmt.Fprintln(a.Err, ui.Err.Warn.Render(cfg.Ref()+" has changed since this sandbox was created"))
				fmt.Fprintln(a.Err, ui.Err.Info.Render("  run 'lid start --rebuild' to rebuild it, which ends the sessions running in it"))
			}
			return nil
		},
	}
}

func lookUpBackend(a *app.App) (*config.Config, backend.Backend, error) {
	cfg, err := a.Config()
	if err != nil {
		return nil, nil, err
	}
	if err := checkSelection(cfg); err != nil {
		return nil, nil, err
	}

	b := backend.Get(cfg.Backend)
	if b == nil {
		return nil, nil, failure.New("no backend named %q", cfg.Backend).At(cfg.Ref())
	}
	if err := b.EnsureAvailable(a.SandboxHost()); err != nil {
		return nil, nil, err
	}
	return cfg, b, nil
}

func stateName(state backend.State) string {
	switch {
	case !state.Exists:
		return "not created"
	case state.Running:
		return "running"
	default:
		return "stopped"
	}
}
