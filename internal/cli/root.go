package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

const tagline = "Let your agents run wild without running wild."

func NewRootCommand(a *app.App) *cobra.Command {
	root := &cobra.Command{
		Use:   "lid",
		Short: tagline,
		Long: ui.RenderBanner(ui.Banner{
			Version:  a.Version,
			Subtitle: tagline,
		}),
		Version:       a.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetOut(a.Out)
	root.SetErr(a.Err)
	if a.In != nil {
		root.SetIn(a.In)
	}

	root.PersistentFlags().StringVarP(&a.ConfigPath, "config", "C", "", "path to config file (overrides discovery)")
	root.SetVersionTemplate(versionLine(a) + "\n")
	root.Flags().BoolP("version", "V", false, "version for lid")

	root.AddCommand(
		newVersionCommand(a),
		newInitCommand(a),
		newConfigCommand(a),
		newBuildCommand(a),
		newStartCommand(a),
		newShellCommand(a),
		newHandoffCommand(a),
	)

	return root
}

func Execute(version, commit, date string) int {
	a, err := app.New(version, commit, date)
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.RenderError(err))
		return 1
	}
	return exitCode(a, NewRootCommand(a).Execute())
}

func exitCode(a *app.App, err error) int {
	if err == nil {
		return 0
	}

	var exit *app.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}

	fmt.Fprintln(a.Err, ui.RenderError(err))
	return 1
}
