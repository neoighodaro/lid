// Package herdr names the Herdr tab lid was started from, the way the zellij
// package does for zellij tabs.
package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const tabPrefix = "🔒 " // an emoji, not the nerd-font lock zellij tabs get: Herdr's tab bar has no nerd font

// Session is the Herdr terminal lid runs in. Herdr sets HC_TERMINAL_ID in
// every terminal it runs; outside Herdr it is empty and Session does nothing.
type Session struct {
	TerminalID string
	Run        func(argv []string) ([]byte, error)
	Bin        string
}

type layout struct {
	Spaces []struct {
		Tabs []tab `json:"tabs"`
	} `json:"spaces"`
}

type tab struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Terminals []terminal `json:"terminals"`
}

type terminal struct {
	Terminal  string     `json:"terminal"`
	Pane      string     `json:"pane"`
	SubAgents []terminal `json:"sub_agents"`
}

func New() Session {
	return Session{TerminalID: os.Getenv("HC_TERMINAL_ID"), Run: run}
}

// RenameTab names the tab holding lid's terminal after display. It targets the
// tab by id, so it is right even when the person has moved to another tab.
func (s Session) RenameTab(display string) {
	if display == "" || s.TerminalID == "" || s.Run == nil {
		return
	}

	t, ok := s.ownTab()
	if !ok {
		return
	}

	name := tabPrefix + display
	if t.Name == name {
		return
	}
	s.Run([]string{"herdr-app", "do", "tab.rename", "--id", t.ID, "--name", name})
}

// SplitPane opens a pane to the right of lid's own, running argv. It never
// falls back to the focused pane: with lid's pane unknown it does nothing.
func (s Session) SplitPane(argv []string) error {
	if s.TerminalID == "" || s.Run == nil {
		return errors.New("not running inside Herdr")
	}

	pane := s.ownPane()
	if pane == "" {
		return errors.New("cannot find lid's pane in herdr-app ls")
	}

	req, err := json.Marshal(struct {
		Direction string   `json:"direction"`
		Pane      string   `json:"pane"`
		Argv      []string `json:"argv"`
	}{"right", pane, argv})
	if err != nil {
		return err
	}

	out, err := s.Run([]string{s.bin(), "do", "pane.split", string(req)})
	if err != nil {
		return fmt.Errorf("herdr-app pane.split: %w", err)
	}
	var reply struct {
		Pane string `json:"pane"`
	}
	if err := json.Unmarshal(out, &reply); err != nil || reply.Pane == "" {
		return errors.New("herdr-app pane.split opened no pane")
	}
	return nil
}

func (s Session) bin() string {
	if s.Bin != "" {
		return s.Bin
	}
	return "herdr-app"
}

func (s Session) ownPane() string {
	out, err := s.Run([]string{s.bin(), "ls"})
	if err != nil {
		return ""
	}
	var l layout
	if err := json.Unmarshal(out, &l); err != nil {
		return ""
	}
	for _, sp := range l.Spaces {
		for _, t := range sp.Tabs {
			if pane := paneOf(t.Terminals, s.TerminalID); pane != "" {
				return pane
			}
		}
	}
	return ""
}

func paneOf(terminals []terminal, id string) string {
	for _, t := range terminals {
		if t.Terminal == id {
			return t.Pane
		}
		if pane := paneOf(t.SubAgents, id); pane != "" {
			return pane
		}
	}
	return ""
}

func (s Session) ownTab() (tab, bool) {
	out, err := s.Run([]string{"herdr-app", "ls"})
	if err != nil {
		return tab{}, false
	}

	var l layout
	if err := json.Unmarshal(out, &l); err != nil {
		return tab{}, false
	}
	for _, sp := range l.Spaces {
		for _, t := range sp.Tabs {
			if holds(t.Terminals, s.TerminalID) {
				return t, true
			}
		}
	}
	return tab{}, false
}

func holds(terminals []terminal, id string) bool {
	for _, t := range terminals {
		if t.Terminal == id || holds(t.SubAgents, id) {
			return true
		}
	}
	return false
}

const runTimeout = 15 * time.Second

func run(argv []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}
