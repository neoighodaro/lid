package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
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

			for _, row := range [][2]string{
				{"config", cfg.Path},
				{"name", cfg.Name},
				{"agent", cfg.Agent},
			} {
				fmt.Fprintf(a.Out, "%s %s\n", ui.Label.Render(fmt.Sprintf("%-8s", row[0])), row[1])
			}
			return nil
		},
	}
}
