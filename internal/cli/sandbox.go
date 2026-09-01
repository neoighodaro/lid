package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

// resolveBackend picks the backend the config names and reports everything it
// cannot honour, so a session never starts on a silent misunderstanding.
func resolveBackend(a *app.App, cfg *config.Config) (backend.Backend, error) {
	b := backend.Get(cfg.Backend)
	if b == nil {
		return nil, failure.New("no backend named %q", cfg.Backend).At(cfg.Ref())
	}

	warnUnverifiedAgent(a, cfg)

	warnings, err := b.Validate(cfg)
	if err != nil {
		return nil, err
	}
	for _, warning := range warnings {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(warning))
	}
	return b, nil
}

// provisionSandbox brings the sandbox up and configures the agent in it. Both
// 'lid start' and 'lid shell' go through here, so a shell opened from cold gets
// the same sandbox the agent would have been handed. cmd is the command doing
// the asking, so the stale-config hint names the flag the user can actually
// reach from where they are.
func provisionSandbox(cmd *cobra.Command, a *app.App, b backend.Backend, cfg *config.Config, opts backend.Options) (backend.Provisioned, error) {
	if err := b.EnsureAvailable(a.SandboxHost()); err != nil {
		return backend.Provisioned{}, err
	}

	provisioned, err := b.Provision(a.SandboxHost(), cfg, opts)
	if err != nil {
		return backend.Provisioned{}, err
	}
	for _, note := range provisioned.Notes {
		fmt.Fprintln(a.Err, ui.Err.Info.Render(note))
	}
	if provisioned.Changed {
		fmt.Fprintln(a.Err, ui.Err.Info.Render("Wrote "+provisioned.Ref))
	}
	if provisioned.Stale {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(cfg.Ref()+" has changed since this sandbox was created"))
		fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("  run '%s --rebuild' to rebuild it, which ends the sessions running in it", cmd.CommandPath())))
	}

	if err := b.PrepareAgent(a.SandboxHost(), cfg); err != nil {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(err.Error()))
	}

	return provisioned, nil
}
