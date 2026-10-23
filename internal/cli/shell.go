package cli

import (
	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/spf13/cobra"
)

func newShellCommand(a *app.App) *cobra.Command {
	var rebuild bool

	cmd := &cobra.Command{
		Use:     "shell [-- command ...]",
		Aliases: []string{"ssh"},
		Short:   "Open a shell in this project's sandbox, or run one command in it",
		Long: "Open a shell in this project's sandbox, or run one command in it.\n\n" +
			"The sandbox is created first if it is not there yet, exactly as 'lid start' would.\n" +
			"An interactive shell picks up the aliases and directory from the shell: block;\n" +
			"a one-off command gets the directory, but bash expands no aliases for it.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}
			if err := checkSelection(cfg); err != nil {
				return err
			}

			b, err := resolveBackend(a, cfg)
			if err != nil {
				return err
			}

			state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{})
			running := err == nil && state.Running
			if !running {
				if err := ensureOnlyBackend(a, cfg); err != nil {
					return err
				}
			}

			provisioned, err := provisionSandbox(cmd, a, b, cfg, backend.Options{Rebuild: rebuild})
			if err != nil {
				return err
			}
			if !running {
				if err := rememberIfNamed(cfg); err != nil {
					return err
				}
			}

			argv, err := b.ShellArgv(a.SandboxHost(), cfg, provisioned.Path, args)
			if err != nil {
				return err
			}
			return a.Launch(argv)
		},
	}

	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "replace the sandbox before opening the shell, ending any sessions running in it")
	return cmd
}
