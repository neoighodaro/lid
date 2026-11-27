package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newConfigCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the resolved configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}

			warnUnverifiedAgent(a, cfg)

			printRows(a.Out, configRows(cfg))
			return nil
		},
	}
}

func printRows(w io.Writer, rows [][2]string) {
	width := labelWidth(rows)
	for _, row := range rows {
		fmt.Fprintf(w, "%s %s\n", ui.Label.Render(fmt.Sprintf("%-*s", width, row[0])), row[1])
	}
}

func labelWidth(rows [][2]string) int {
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	return width
}

func configBackendRow(cfg *config.Config) string {
	if cfg.BackendSource == config.SourceFile {
		return cfg.Backend + ", from " + filepath.Base(cfg.Path)
	}
	return backendRow(cfg)
}

func configRows(cfg *config.Config) [][2]string {
	rows := [][2]string{
		{"config", cfg.Path},
		{"name", cfg.Name},
		{"agent", cfg.Agent},
		{"backend", configBackendRow(cfg)},
	}

	for _, row := range [][2]string{
		{"base_image", cfg.BaseImage},
		{"template", cfg.Template},
		{"workspace", cfg.Workspace},
		{"clone", boolRow(cfg.Clone)},
		{"resources.cpus", cpusRow(cfg.Resources.CPUs)},
		{"resources.memory", cfg.Resources.Memory},
		{"extra_workspaces", strings.Join(cfg.ExtraWorkspaces, ", ")},
		{"packages", strings.Join(cfg.Packages, ", ")},
		{"runtimes", joinRuntimes(cfg.Runtimes)},
		{"php.extensions", strings.Join(cfg.PHP.Extensions, ", ")},
		{"ports", joinScalars(cfg.Ports)},
		{"env", joinScalarKeys(cfg.Env)},
		{"env_file", strings.Join(cfg.EnvFile, ", ")},
		{"network", strings.Join(cfg.Network, ", ")},
		{"network_profile", cfg.NetworkProfile},
		{"audit", boolRow(cfg.Audit)},
		{"allow_commands", strings.Join(cfg.AllowCommands, ", ")},
		{"paths.read", strings.Join(cfg.Paths.Read, ", ")},
		{"paths.read_file", strings.Join(cfg.Paths.ReadFile, ", ")},
		{"paths.allow", strings.Join(cfg.Paths.Allow, ", ")},
		{"paths.deny", strings.Join(cfg.Paths.Deny, ", ")},
		{"scripts.on_create", strings.Join(cfg.Scripts.OnCreate, ", ")},
		{"scripts.on_start", strings.Join(cfg.Scripts.OnStart, ", ")},
		{"herdr.panes", boolRow(cfg.Herdr.Panes)},
		{"handoff", handoffRow(cfg)},
		{"bridges", joinBridges(cfg.Bridges)},
		{"mcp", strings.Join(cfg.MCP, ", ")},
		{"shell.directory", cfg.Shell.Directory},
		{"shell.aliases", joinKeys(cfg.Shell.Aliases)},
		{"claude.theme", cfg.Claude.Theme},
		{"claude.marketplaces", strings.Join(cfg.Claude.Plugins.Marketplaces, ", ")},
		{"claude.install", strings.Join(cfg.Claude.Plugins.Install, ", ")},
		{"claude.install_optional", strings.Join(cfg.Claude.Plugins.InstallOptional, ", ")},
		{"claude.settings", joinAnyKeys(cfg.Claude.Settings)},
		{"display_name", cfg.DisplayName},
		{"zellij.display_name", cfg.Zellij.DisplayName},
		{"herdr.display_name", cfg.Herdr.DisplayName},
		{"memory", lineCount(cfg.Memory)},
	} {
		if row[1] != "" {
			rows = append(rows, row)
		}
	}
	return rows
}

func cpusRow(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// boolRow renders a flag the way every other row reads: the empty string means
// the key is unset, which is what keeps it out of the listing.
func boolRow(on bool) string {
	if !on {
		return ""
	}
	return "on"
}

func handoffRow(cfg *config.Config) string {
	if !cfg.Handoff.Enabled {
		return ""
	}
	return "on, " + cfg.HandoffURL()
}

func lineCount(memory string) string {
	memory = strings.TrimSpace(memory)
	if memory == "" {
		return ""
	}
	lines := len(strings.Split(memory, "\n"))
	if lines == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", lines)
}

func joinBridges(bridges []config.Bridge) string {
	parts := make([]string, 0, len(bridges))
	for _, b := range bridges {
		part := fmt.Sprintf("%s:%d from host %d", b.Name, b.Port, b.HostPort)
		if b.Env != "" {
			part += " as " + b.Env
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func joinRuntimes(runtimes map[string]config.Scalar) string {
	names := make([]string, 0, len(runtimes))
	for name := range runtimes {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+" "+string(runtimes[name]))
	}
	return strings.Join(parts, ", ")
}

func joinScalars(values []config.Scalar) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, string(v))
	}
	return strings.Join(parts, ", ")
}

func joinScalarKeys(values map[string]config.Scalar) string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return sortedJoin(names)
}

func joinKeys(values map[string]string) string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return sortedJoin(names)
}

func joinAnyKeys(values map[string]any) string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	return sortedJoin(names)
}

func sortedJoin(names []string) string {
	sort.Strings(names)
	return strings.Join(names, ", ")
}
