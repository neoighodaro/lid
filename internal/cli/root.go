package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/neoighodaro/lid/internal/update"
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

	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		noteUpdate(a, cmd.Name())
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
		newRestartCommand(a),
		newShellCommand(a),
		newStatusCommand(a),
		newStopCommand(a),
		newNetworkCommand(a),
		newHandoffCommand(a),
		newUpdateCommand(a),
		newUpdateCheckCommand(a),
	)

	return root
}

func Execute(version, commit, date string) int {
	a, err := app.New(version, commit, date)
	if err != nil {
		fmt.Fprintln(os.Stderr, ui.RenderError(err))
		return 1
	}
	code := exitCode(a, NewRootCommand(a).Execute())
	printUpdateNotice(a)
	return code
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

func isTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func noteUpdate(a *app.App, command string) {
	if update.Suppressed(os.Getenv, isTTY(a.Out), command) || !update.IsRelease(a.Version) {
		return
	}

	path, err := update.StatePath()
	if err != nil {
		return
	}

	state := update.LoadState(path)
	if update.IsNewer(a.Version, state.LatestVersion) {
		a.PendingUpdate = state.LatestVersion
	}
	if state.Due(time.Now(), update.CheckInterval) {
		if self, err := os.Executable(); err == nil {
			_ = update.Spawn(self)
		}
	}
}

func printUpdateNotice(a *app.App) {
	if a.PendingUpdate == "" {
		return
	}
	fmt.Fprintln(a.Err, ui.Err.Warn.Render("lid "+a.PendingUpdate+" is out; you are on "+a.Version))
	fmt.Fprintln(a.Err, ui.Err.Info.Render("  run 'lid update' to install it"))
}
