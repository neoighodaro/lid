package app

import (
	"io"
	"os"
	"sync"

	"github.com/neoighodaro/lid/internal/config"
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
	}, nil
}

func (a *App) Config() (*config.Config, error) {
	a.cfgOnce.Do(func() {
		a.cfg, a.cfgErr = config.Load(a.Cwd, a.ConfigPath)
	})
	return a.cfg, a.cfgErr
}
