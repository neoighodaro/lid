package cli

import (
	"fmt"
	"sort"
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

			for _, row := range configRows(cfg) {
				fmt.Fprintf(a.Out, "%s %s\n", ui.Label.Render(fmt.Sprintf("%-9s", row[0])), row[1])
			}
			return nil
		},
	}
}

func configRows(cfg *config.Config) [][2]string {
	rows := [][2]string{
		{"config", cfg.Path},
		{"name", cfg.Name},
		{"agent", cfg.Agent},
		{"backend", cfg.Backend},
	}

	optional := [][2]string{
		{"template", cfg.Template},
		{"workspace", cfg.Workspace},
		{"packages", strings.Join(cfg.Packages, ", ")},
		{"runtimes", joinRuntimes(cfg.Runtimes)},
		{"ports", joinScalars(cfg.Ports)},
		{"network", strings.Join(cfg.Network, ", ")},
	}
	for _, row := range optional {
		if row[1] != "" {
			rows = append(rows, row)
		}
	}
	return rows
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
