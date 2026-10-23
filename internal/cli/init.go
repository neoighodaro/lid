package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

const kitIgnoreEntry = ".config/lid/.kit/"

func newInitCommand(a *app.App) *cobra.Command {
	var name string
	var runtimes []string

	cmd := &cobra.Command{
		Use:   "init [backend]",
		Short: "Create a starter lid config",
		Long:  "Create a starter lid config for a backend, defaulting to " + config.DefaultBackend + ".",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := takeBackendArg(a, args); err != nil {
				return err
			}
			backend := config.DefaultBackend
			if a.Backend != "" {
				backend = a.Backend
			}
			return runInit(a, name, backend, runtimes)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "sandbox name (defaults to the directory name)")
	cmd.Flags().StringSliceVar(&runtimes, "runtimes", nil, "runtimes to pin, like node@24,go@1.27 ("+config.BackendSbx+" only)")

	return cmd
}

func runInit(a *app.App, name, backend string, runtimes []string) error {
	if existing, err := config.Discover(a.Cwd); err == nil {
		fmt.Fprintln(a.Out, ui.Info.Render("Config already exists at "+existing))
		return nil
	}

	if err := config.ValidateBackend(backend); err != nil {
		return err
	}

	name, err := initName(a.Cwd, name)
	if err != nil {
		return err
	}

	pins, err := initRuntimes(backend, runtimes)
	if err != nil {
		return err
	}

	body, err := config.Starter(config.StarterOptions{Name: name, Backend: backend, Runtimes: pins})
	if err != nil {
		return err
	}

	configDir := filepath.Join(a.Cwd, ".config", "lid")
	if err := os.MkdirAll(filepath.Join(configDir, ".kit"), 0o755); err != nil {
		return failure.New("cannot create the config directory").At(configDir).WithFix("%v", err)
	}

	path := filepath.Join(configDir, "lid.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return failure.New("cannot write the starter config").At(path).WithFix("%v", err)
	}

	if err := ensureGitignored(a.Cwd, kitIgnoreEntry); err != nil {
		return err
	}

	fmt.Fprintln(a.Out, ui.Success.Render("Created "+path))
	fmt.Fprintln(a.Out, ui.Info.Render("Edit it, then run 'lid start' to create your sandbox."))
	return nil
}

func initRuntimes(backend string, entries []string) ([]config.RuntimePin, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	if backend != config.BackendSbx {
		return nil, failure.New("the %s backend installs no runtimes, so --runtimes has nothing to pin", backend).
			WithFix("drop the flag and add 'runtimes:' by hand to grant the host's toolchains, or run 'lid init %s'", config.BackendSbx)
	}
	return config.ParseRuntimePins(entries)
}

func initName(cwd, flag string) (string, error) {
	if flag != "" {
		if err := config.ValidateName(flag); err != nil {
			return "", err
		}
		return flag, nil
	}

	name := config.SanitizeName(filepath.Base(cwd))
	if name == "" {
		return "", failure.New("cannot derive a sandbox name from the directory %q", filepath.Base(cwd)).
			WithFix("pass --name")
	}
	return name, nil
}

func ensureGitignored(projectDir, entry string) error {
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); err != nil {
		return nil
	}

	path := filepath.Join(projectDir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return failure.New("cannot read .gitignore").At(path).WithFix("%v", err)
	}

	content := string(existing)
	if strings.Contains(content, entry) {
		return nil
	}

	var b strings.Builder
	if content != "" {
		b.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("# lid\n")
	b.WriteString(entry + "\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return failure.New("cannot update .gitignore").At(path).WithFix("%v", err)
	}
	return nil
}
