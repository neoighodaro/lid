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
		Use:   "start [backend]",
		Short: "Start the sandboxed agent for this project",
		Long: "Start the sandboxed agent for this project.\n\n" +
			"Name a backend, as in 'lid start nono', to run on it instead of the one in the\n" +
			"config; lid remembers it for this project until 'lid stop'.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := takeBackendArg(a, args); err != nil {
				return err
			}
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

			if err := ensureOnlyBackend(a, cfg); err != nil {
				return err
			}

			a.Zellij.RenameTab(cfg.Zellij.DisplayName)

			provisioned, err := provisionSandbox(cmd, a, b, cfg, backend.Options{Rebuild: rebuild})
			if err != nil {
				return err
			}
			if err := rememberIfNamed(cfg); err != nil {
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
