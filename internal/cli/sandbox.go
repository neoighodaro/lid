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

// takeBackendArg folds a backend named as an argument into --backend, so the
// two spellings resolve the same way everywhere after.
func takeBackendArg(a *app.App, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if a.Backend != "" && a.Backend != args[0] {
		return failure.New("the backend is given twice, as %q and --backend %q", args[0], a.Backend).
			WithFix("pass it once")
	}
	a.Backend = args[0]
	return nil
}

// backendRow names the backend along with where it came from, when that is
// not simply the config file.
func backendRow(cfg *config.Config) string {
	switch cfg.BackendSource {
	case config.SourceFlag:
		return cfg.Backend + ", from --backend"
	case config.SourceSelection:
		return cfg.Backend + ", remembered until 'lid stop'"
	}
	return cfg.Backend
}

// otherBackendRunning names a backend other than cfg's that is running this
// project, or returns "". A backend that is not installed or does not answer
// counts as not running: one missing tool must not block the other.
func otherBackendRunning(a *app.App, cfg *config.Config) string {
	for _, name := range config.Backends {
		if name == cfg.Backend {
			continue
		}
		b := backend.Get(name)
		if b == nil || b.EnsureAvailable(a.SandboxHost()) != nil {
			continue
		}
		if state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{}); err == nil && state.Running {
			return name
		}
	}
	return ""
}

// ensureOnlyBackend keeps a project on one backend at a time.
func ensureOnlyBackend(a *app.App, cfg *config.Config) error {
	if other := otherBackendRunning(a, cfg); other != "" {
		return failure.New("the %s sandbox is running on %s", cfg.Name, other).
			WithFix("stop it with 'lid stop', or switch with 'lid restart %s'", cfg.Backend)
	}
	return nil
}

// rememberIfNamed records the backend only when the user named one: a plain
// start follows the file, so editing its backend: keeps working.
func rememberIfNamed(cfg *config.Config) error {
	if cfg.BackendSource != config.SourceFlag {
		return nil
	}
	return cfg.Remember()
}

// checkSelection refuses a --backend that disagrees with the backend the
// project was started on, which would point the command at a session that is
// not there.
func checkSelection(cfg *config.Config) error {
	if cfg.BackendSource == config.SourceFlag && cfg.Selected != "" && cfg.Selected != cfg.Backend {
		return failure.New("%s is on %s, not %s", cfg.Name, cfg.Selected, cfg.Backend).
			WithFix("run 'lid stop' to end it, or 'lid restart %s' to switch", cfg.Backend)
	}
	return nil
}

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
	for _, warning := range provisioned.Warnings {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(warning))
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
