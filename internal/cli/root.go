package cli

import (
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
	)

	return root
}

func Execute(version, commit, date string) error {
	a, err := app.New(version, commit, date)
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.Error.Render("Error: "+err.Error()))
		return err
	}

	if err := NewRootCommand(a).Execute(); err != nil {
		fmt.Fprintln(a.Err, ui.Error.Render("Error: "+err.Error()))
		return err
	}
	return nil
}
