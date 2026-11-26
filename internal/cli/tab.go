package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/ui"
)

// nameTab names the terminal tab lid was started from, in zellij or Herdr,
// after the project's display name, and says when that name sits under a
// deprecated key.
func nameTab(a *app.App, cfg *config.Config) {
	for _, note := range cfg.Deprecations() {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(note))
	}
	name := cfg.TabName()
	a.Zellij.RenameTab(name)
	a.Herdr.RenameTab(name)
}
