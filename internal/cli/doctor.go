package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/neoighodaro/lid/internal/update"
	"github.com/spf13/cobra"
)

type checkLevel int

const (
	checkOK checkLevel = iota
	checkSkip
	checkWarn
	checkFail
)

type check struct {
	name   string
	level  checkLevel
	detail string
	fix    string
}

func newDoctorCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that lid, its backends and this project are ready to run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := runDoctor(a)
			printChecks(a.Out, checks)
			a.PendingUpdate = ""
			for _, c := range checks {
				if c.level == checkFail {
					return &app.ExitError{Code: 1}
				}
			}
			return nil
		},
	}
}

func runDoctor(a *app.App) []check {
	checks := []check{lidCheck(a)}

	cfg, err := a.Config()
	if err == nil {
		err = checkSelection(cfg)
	}
	if err != nil {
		checks = append(checks, failed("config", err))
		return append(checks, backendChecks(a, "")...)
	}

	checks = append(checks, check{name: "config", detail: configDetail(a, cfg)})
	checks = append(checks, backendChecks(a, cfg.Backend)...)

	b := backend.Get(cfg.Backend)
	if b == nil {
		return append(checks, failed("project", failure.New("no backend named %q", cfg.Backend).At(cfg.Ref())))
	}
	checks = append(checks, projectChecks(cfg, b)...)
	checks = append(checks, herdrChecks(a, cfg)...)

	if b.EnsureAvailable(a.SandboxHost()) != nil {
		return append(checks, check{name: "sandbox", level: checkSkip, detail: "not checked: " + cfg.Backend + " is not ready"})
	}
	return append(checks, sandboxCheck(a, cfg, b))
}

func lidCheck(a *app.App) check {
	c := check{name: "lid", detail: a.Version}
	if !update.IsRelease(a.Version) {
		c.detail += " (source build)"
		return c
	}
	latest := a.PendingUpdate
	if path, err := update.StatePath(); err == nil {
		if state := update.LoadState(path); update.IsNewer(a.Version, state.LatestVersion) {
			latest = state.LatestVersion
		}
	}
	if update.IsNewer(a.Version, latest) {
		c.level = checkWarn
		c.detail += ", " + latest + " is out"
		c.fix = "run 'lid update' to install it"
	}
	return c
}

func configDetail(a *app.App, cfg *config.Config) string {
	path := cfg.Path
	if rel, err := filepath.Rel(a.Cwd, path); err == nil {
		path = rel
	}
	return path + ", backend " + backendRow(cfg)
}

// backendChecks fails a missing backend only when nothing could run in its
// place: the one the project is on, or every one when none is installed.
func backendChecks(a *app.App, selected string) []check {
	checks := make([]check, 0, len(config.Backends))
	ready := 0
	for _, name := range config.Backends {
		if err := backend.Get(name).EnsureAvailable(a.SandboxHost()); err != nil {
			checks = append(checks, failed(name, err))
			continue
		}
		ready++
		checks = append(checks, check{name: name, detail: "ready"})
	}
	if ready == 0 {
		return checks
	}
	for i, name := range config.Backends {
		if checks[i].level != checkFail || name == selected {
			continue
		}
		checks[i].level = checkSkip
		checks[i].fix = ""
		if selected != "" {
			checks[i].detail += "; only needed for --backend " + name
		}
	}
	return checks
}

func projectChecks(cfg *config.Config, b backend.Backend) []check {
	var checks []check
	if !config.AgentVerified(cfg.Agent) {
		checks = append(checks, check{
			name:   "agent",
			level:  checkWarn,
			detail: "lid is not verified with " + cfg.Agent + "; no agent-specific setup is applied",
		})
	} else {
		checks = append(checks, check{name: "agent", detail: cfg.Agent})
	}

	for _, note := range cfg.Deprecations() {
		checks = append(checks, check{name: "config", level: checkWarn, detail: note})
	}

	warnings, err := b.Validate(cfg)
	if err != nil {
		return append(checks, failed("project", err))
	}
	if len(warnings) == 0 {
		return append(checks, check{name: "project", detail: "no problems in " + cfg.Ref()})
	}
	for _, warning := range warnings {
		checks = append(checks, check{name: "project", level: checkWarn, detail: warning})
	}
	return checks
}

func herdrChecks(a *app.App, cfg *config.Config) []check {
	if !cfg.Herdr.Panes {
		return nil
	}
	switch {
	case a.Herdr.TerminalID == "":
		return []check{{name: "herdr panes", level: checkSkip, detail: "on, but lid is not running in Herdr"}}
	case !config.IsClaudeAgent(cfg.Agent):
		return []check{{name: "herdr panes", level: checkWarn, detail: "on, but the agent is " + cfg.Agent + " and only claude-code can open panes"}}
	}
	return []check{{name: "herdr panes", detail: "on"}}
}

func sandboxCheck(a *app.App, cfg *config.Config, b backend.Backend) check {
	state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{Stale: true})
	if err != nil {
		return failed("sandbox", err)
	}
	c := check{name: "sandbox", detail: stateName(state)}
	switch {
	case !state.Exists:
		c.level = checkSkip
		c.fix = "run 'lid start' to create it"
	case state.Stale:
		c.level = checkWarn
		c.detail += "; " + cfg.Ref() + " has changed since it was created"
		c.fix = "run 'lid start --rebuild' to rebuild it, which ends the sessions running in it"
	}
	return c
}

func failed(name string, err error) check {
	c := check{name: name, level: checkFail, detail: err.Error()}
	var f *failure.Error
	if errors.As(err, &f) {
		c.detail = f.What
		if f.In != "" {
			c.detail += " (" + f.In + ")"
		}
		c.fix = f.Fix
	}
	return c
}

func printChecks(w io.Writer, checks []check) {
	width := 0
	for _, c := range checks {
		width = max(width, len(c.name))
	}
	for _, c := range checks {
		mark := map[checkLevel]string{
			checkOK:   ui.Out.Success.Render("✓"),
			checkSkip: ui.Out.Info.Render("-"),
			checkWarn: ui.Out.Warn.Render("!"),
			checkFail: ui.Out.Error.Render("✗"),
		}[c.level]
		fmt.Fprintf(w, "%s %s %s\n", mark, ui.Label.Render(fmt.Sprintf("%-*s", width, c.name)), c.detail)
		if c.fix != "" {
			fmt.Fprintf(w, "  %*s %s\n", width, "", ui.Out.Info.Render("fix: "+c.fix))
		}
	}
}
