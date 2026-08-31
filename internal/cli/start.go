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
	var printOnly bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Provision the sandbox for this project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}

			b := backend.Get(cfg.Backend)
			if b == nil {
				if cfg.BuildsImage() {
					return failure.New("the %s backend provisions from an image, not a profile", cfg.Backend).
						At(cfg.Ref()).
						WithFix("run 'lid build' instead")
				}
				return failure.New("no backend named %q", cfg.Backend).At(cfg.Ref())
			}

			warnUnverifiedAgent(a, cfg)

			warnings, err := b.Validate(cfg)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintln(a.Err, ui.Err.Warn.Render(cfg.Backend+" backend: "+warning))
			}

			profile, err := b.Render(cfg)
			if err != nil {
				return err
			}

			if !printOnly {
				return failure.New("provisioning the %s backend is not wired up yet", cfg.Backend).
					WithFix("run 'lid start --print' to see the profile lid would write")
			}

			fmt.Fprint(a.Out, profile)
			return nil
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "print the generated sandbox profile instead of provisioning it")
	return cmd
}
