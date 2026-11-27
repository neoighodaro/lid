package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/ui"
)

// nameTab names the terminal tab lid was started from, in zellij or Herdr,
// after that terminal's display name, and says when a name sits under a
// deprecated key.
func nameTab(a *app.App, cfg *config.Config) {
	for _, note := range cfg.Deprecations() {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(note))
	}
	a.Zellij.RenameTab(cfg.ZellijTabName())
	a.Herdr.RenameTab(cfg.HerdrTabName())
}
