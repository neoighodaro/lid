package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/zellij"
)

type App struct {
	Version string
	Commit  string
	Date    string

	In  io.Reader
	Out io.Writer
	Err io.Writer

	Cwd        string
	ConfigPath string

	Exec func(argv []string, stdout, stderr io.Writer) error

	Attach func(argv []string, stdin io.Reader, stdout, stderr io.Writer) error
	Host   backend.Host
	Zellij zellij.Session

	cfgOnce sync.Once
	cfg     *config.Config
	cfgErr  error
}

func New(version, commit, date string) (*App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &App{
		Version: version,
		Commit:  commit,
		Date:    date,
		In:      os.Stdin,
		Out:     os.Stdout,
		Err:     os.Stderr,
		Cwd:     cwd,
		Zellij:  zellij.New(),
	}, nil
}

func (a *App) SandboxHost() backend.Host {
	h := a.Host
	if h.Exec == nil {
		h.Exec = a.Exec
	}
	if h.Stdout == nil {
		h.Stdout = a.Out
	}
	if h.Stderr == nil {
		h.Stderr = a.Err
	}
	return h
}

func (a *App) Config() (*config.Config, error) {
	a.cfgOnce.Do(func() {
		a.cfg, a.cfgErr = config.Load(a.Cwd, a.ConfigPath)
	})
	return a.cfg, a.cfgErr
}

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

func (a *App) Launch(argv []string) error {
	if a.Attach != nil {
		return a.Attach(argv, a.In, a.Out, a.Err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = a.In
	cmd.Stdout = a.Out
	cmd.Stderr = a.Err

	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return &ExitError{Code: exit.ExitCode()}
	}
	return err
}
