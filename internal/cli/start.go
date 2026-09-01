package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
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

			b, err := resolveBackend(a, cfg)
			if err != nil {
				return err
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

			provisioned, err := provisionSandbox(a, b, cfg, backend.Options{Rebuild: rebuild})
			if err != nil {
				return err
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
