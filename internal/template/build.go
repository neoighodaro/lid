package template

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/neoighodaro/lid/internal/config"
)

// Exec runs argv to completion, streaming its output. It is a field on Builder
// so tests can assert what would be run without a docker daemon present.
type Exec func(argv []string, stdout, stderr io.Writer) error

type Builder struct {
	Exec   Exec
	Stdout io.Writer
	Stderr io.Writer
}

func (b *Builder) Build(cfg *config.Config) error {
	dockerfile, err := Render(cfg)
	if err != nil {
		return err
	}

	kitDir := cfg.KitDir()
	if err := os.MkdirAll(kitDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", kitDir, err)
	}
	path := filepath.Join(kitDir, "Dockerfile")
	if err := os.WriteFile(path, []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	argv := []string{"docker", "build", "--pull", "-t", cfg.SandboxImageName(), kitDir}
	if err := b.exec()(argv, b.Stdout, b.Stderr); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	return nil
}

func (b *Builder) exec() Exec {
	if b.Exec != nil {
		return b.Exec
	}
	return runCommand
}

// runCommand streams the command's output as it arrives. Buffering it means a
// build stalled on an unreachable apt mirror shows nothing at all, which looks
// like an inexplicable freeze.
func runCommand(argv []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
