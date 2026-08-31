package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/template"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newBuildCommand(a *app.App) *cobra.Command {
	var printOnly bool

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the sandbox image for this project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}

			warnUnverifiedAgent(a, cfg)
			warnIgnoredClaudeConfig(a, cfg)

			if printOnly {
				dockerfile, err := template.Render(cfg)
				if err != nil {
					return err
				}
				fmt.Fprint(a.Out, dockerfile)
				return nil
			}

			builder := &template.Builder{Exec: a.Exec, Stdout: a.Out, Stderr: a.Err}
			if err := builder.Build(cfg); err != nil {
				return err
			}

			fmt.Fprintln(a.Out, ui.Success.Render("Built "+cfg.SandboxImageName()))
			return nil
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "print the generated Dockerfile instead of building it")
	return cmd
}

func warnIgnoredClaudeConfig(a *app.App, cfg *config.Config) {
	if config.IsClaudeAgent(cfg.Agent) || !cfg.Claude.IsSet() {
		return
	}
	fmt.Fprintln(a.Err, ui.Warn.Render("ignoring the claude: section: it only applies to the claude-code agent, and this project uses "+cfg.Agent+"."))
}
