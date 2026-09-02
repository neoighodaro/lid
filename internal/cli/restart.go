package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newRestartCommand(a *app.App) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Replace this project's sandbox, then start the agent in the new one",
		Long: "Replace this project's sandbox, then start the agent in the new one.\n\n" +
			"The old sandbox is destroyed, so whatever is in it that is not part of your\n" +
			"project goes with it. This asks first unless --force says not to.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.Config()
			if err != nil {
				return err
			}

			b, err := resolveBackend(a, cfg)
			if err != nil {
				return err
			}
			if err := b.EnsureAvailable(a.SandboxHost()); err != nil {
				return err
			}

			state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{})
			if err != nil {
				return err
			}
			if state.Exists && !force && !confirmReplace(a, cfg.Name) {
				fmt.Fprintln(a.Err, ui.Err.Info.Render("Left the "+cfg.Name+" sandbox where it was"))
				return nil
			}

			a.Zellij.RenameTab(cfg.Zellij.DisplayName)

			provisioned, err := provisionSandbox(cmd, a, b, cfg, backend.Options{Rebuild: true})
			if err != nil {
				return err
			}

			argv, err := b.StartArgv(cfg, provisioned.Path)
			if err != nil {
				return err
			}
			return a.Launch(argv)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "replace the sandbox without asking first")
	return cmd
}

// confirmReplace asks before a sandbox is destroyed. Only a plain yes goes
// ahead, so a restart run where nobody is there to answer leaves the sandbox
// and the sessions in it standing.
func confirmReplace(a *app.App, name string) bool {
	fmt.Fprintln(a.Err, ui.Err.Warn.Render("Replacing the "+name+" sandbox destroys whatever is in it that is not part of your project"))
	fmt.Fprint(a.Err, ui.Err.Info.Render("Continue? [y/N] "))

	answer, entered := readAnswer(a.In)
	if !entered {
		fmt.Fprintln(a.Err)
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")
}

// readAnswer reads one short line a byte at a time, and reports whether it
// reached the end of it. A buffered reader would read past the newline, and
// this same stdin goes on to the session restart is about to launch, so
// anything read ahead here would be keystrokes that session never sees.
func readAnswer(r io.Reader) (string, bool) {
	const most = 64

	answer := make([]byte, 0, most)
	buf := make([]byte, 1)
	for len(answer) < most {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return strings.TrimSpace(string(answer)), true
			}
			answer = append(answer, buf[0])
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(string(answer)), false
}
