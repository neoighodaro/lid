package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newStartCommand(a *app.App) *cobra.Command {
	var printOnly, rebuild bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the sandboxed agent for this project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}

			b := backend.Get(cfg.Backend)
			if b == nil {
				return failure.New("no backend named %q", cfg.Backend).At(cfg.Ref())
			}

			warnUnverifiedAgent(a, cfg)

			warnings, err := b.Validate(cfg)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintln(a.Err, ui.Err.Warn.Render(warning))
			}

			if printOnly {
				profile, err := b.Render(cfg)
				if err != nil {
					return err
				}
				fmt.Fprint(a.Out, profile)
				return nil
			}

			a.Zellij.RenameTab(cfg.Zellij.DisplayName)

			if err := b.EnsureAvailable(a.SandboxHost()); err != nil {
				return err
			}

			provisioned, err := b.Provision(a.SandboxHost(), cfg, backend.Options{Rebuild: rebuild})
			if err != nil {
				return err
			}
			for _, note := range provisioned.Notes {
				fmt.Fprintln(a.Err, ui.Err.Info.Render(note))
			}
			if provisioned.Changed {
				fmt.Fprintln(a.Err, ui.Err.Info.Render("Wrote "+provisioned.Ref))
			}
			if provisioned.Stale {
				fmt.Fprintln(a.Err, ui.Err.Warn.Render(cfg.Ref()+" has changed since this sandbox was created"))
				fmt.Fprintln(a.Err, ui.Err.Info.Render("  run 'lid start --rebuild' to rebuild it, which ends the sessions running in it"))
			}

			if err := b.PrepareAgent(a.SandboxHost(), cfg); err != nil {
				fmt.Fprintln(a.Err, ui.Err.Warn.Render(err.Error()))
			}

			argv, err := b.StartArgv(cfg, provisioned.Path)
			if err != nil {
				return err
			}
			return a.Launch(argv)
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "print the generated sandbox profile instead of starting the agent")
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "replace the sandbox before starting, ending any sessions running in it")
	return cmd
}
