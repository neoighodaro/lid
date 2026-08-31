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
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

const kitIgnoreEntry = ".config/lid/.kit/"

func newInitCommand(a *app.App) *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a starter lid config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(a, name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "sandbox name (defaults to the directory name)")

	return cmd
}

func runInit(a *app.App, name string) error {
	if existing, err := config.Discover(a.Cwd); err == nil {
		fmt.Fprintln(a.Out, ui.Info.Render("Config already exists at "+existing))
		return nil
	}

	name, err := initName(a.Cwd, name)
	if err != nil {
		return err
	}

	body, err := config.Starter(name)
	if err != nil {
		return err
	}

	configDir := filepath.Join(a.Cwd, ".config", "lid")
	if err := os.MkdirAll(filepath.Join(configDir, ".kit"), 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", configDir, err)
	}

	path := filepath.Join(configDir, "lid.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}

	if err := ensureGitignored(a.Cwd, kitIgnoreEntry); err != nil {
		return err
	}

	fmt.Fprintln(a.Out, ui.Success.Render("Created "+path))
	fmt.Fprintln(a.Out, ui.Info.Render("Edit it, then run 'lid start' to create your sandbox."))
	return nil
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
		return "", fmt.Errorf("cannot derive a name from %q: pass --name", filepath.Base(cwd))
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
		return fmt.Errorf("cannot read %s: %w", path, err)
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
		return fmt.Errorf("cannot update %s: %w", path, err)
	}
	return nil
}
