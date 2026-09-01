package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/neoighodaro/lid/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Replace this lid binary with the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(a, cmd.Context())
		},
	}
}

func newUpdateCheckCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:    update.CheckCommand,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := update.StatePath()
			if err != nil {
				return nil
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			_ = update.RunCheck(ctx, update.NewClient(update.Repo(os.Getenv)), path, time.Now())
			return nil
		},
	}
}

func runUpdate(a *app.App, ctx context.Context) error {
	if !update.IsRelease(a.Version) {
		return failure.New("this lid was built from source, not installed from a release (%s)", a.Version).
			WithFix("update it the way you built it, with 'git pull && make build'")
	}

	target, err := update.Target()
	if err != nil {
		return failure.New("cannot tell which file this lid is running from").
			WithFix("%v", err)
	}
	if manager, ok := update.ManagedBy(target); ok {
		return failure.New("this lid was installed by %s, which keeps its own copy", manager.Name).
			At(target).
			WithFix("upgrade it with '%s'", manager.Upgrade)
	}
	if err := update.Writable(target); err != nil {
		return failure.New("cannot write over this lid binary").
			At(target).
			WithFix("run 'sudo lid update', or reinstall it somewhere you own")
	}

	client := update.NewClient(update.Repo(os.Getenv))
	release, err := client.LatestRelease(ctx)
	switch {
	case errors.Is(err, update.ErrNoReleases):
		return failure.New("%s has published no releases yet", client.Repo).
			WithFix("set LID_UPDATE_REPO if lid lives somewhere else")
	case err != nil:
		return failure.New("cannot ask github for the latest release: %v", err).
			WithFix("check your network, then try again")
	}

	if !update.IsNewer(a.Version, release.TagName) {
		fmt.Fprintln(a.Out, ui.Success.Render("lid is already up to date ("+a.Version+")"))
		return nil
	}

	fmt.Fprintln(a.Err, ui.Err.Info.Render("Updating "+a.Version+" → "+release.TagName))

	asset := update.PlatformAsset(release.TagName)
	assetURL, ok := release.AssetURL(asset)
	if !ok {
		return failure.New("%s does not ship a build for this machine", release.TagName).
			WithFix("this platform needs %s; open an issue, or build from source", asset)
	}
	sumsURL, ok := release.AssetURL("checksums.txt")
	if !ok {
		return failure.New("%s ships no checksums.txt, so lid cannot verify what it downloaded", release.TagName).
			WithFix("install it by hand from the release page, or wait for a release that has one")
	}

	tarball, err := client.Download(ctx, assetURL)
	if err != nil {
		return failure.New("cannot download %s: %v", asset, err).
			WithFix("check your network, then try again")
	}
	checksums, err := client.Download(ctx, sumsURL)
	if err != nil {
		return failure.New("cannot download the checksums for %s: %v", release.TagName, err).
			WithFix("check your network, then try again")
	}
	if err := update.VerifyChecksum(checksums, asset, tarball); err != nil {
		return failure.New("what lid downloaded is not what %s published: %v", release.TagName, err).
			WithFix("try again; if it keeps failing, install from the release page by hand")
	}

	binary, err := update.ExtractBinary(tarball)
	if err != nil {
		return failure.New("cannot unpack %s: %v", asset, err).
			WithFix("install it by hand from the release page")
	}
	if err := update.Replace(target, binary); err != nil {
		return failure.New("cannot install the new lid: %v", err).
			At(target).
			WithFix("check who owns that file, then try again")
	}

	fmt.Fprintln(a.Out, ui.Success.Render("Updated to "+release.TagName))
	warnStaleBroker(a)
	return nil
}

func warnStaleBroker(a *app.App) {
	b, ok := readBrokerFile()
	if !ok {
		return
	}
	if gone, err := processGone(b.pid); err != nil || gone {
		return
	}
	fmt.Fprintln(a.Err, ui.Err.Warn.Render(fmt.Sprintf("the handoff broker (pid %d) is still running the old lid", b.pid)))
	fmt.Fprintln(a.Err, ui.Err.Info.Render("  restart it with 'lid handoff -K' then 'lid handoff -D'"))
}
