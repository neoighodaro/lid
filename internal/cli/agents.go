package cli

import (
	"fmt"
	"strings"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/ui"
)

// warnUnverifiedAgent notes on stderr that lid has done no work for the
// configured agent, so stdout stays clean for anything parsing it.
func warnUnverifiedAgent(a *app.App, cfg *config.Config) {
	if config.AgentVerified(cfg.Agent) {
		return
	}
	fmt.Fprintln(a.Err, ui.Err.Warn.Render(
		"lid is not verified with the "+cfg.Agent+" agent; no agent-specific setup is applied."))
	fmt.Fprintln(a.Err, ui.Err.Info.Render(
		"  Verified: "+strings.Join(config.VerifiedAgents, ", ")))
}
