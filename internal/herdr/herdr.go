// Package herdr names the Herdr tab lid was started from, the way the zellij
// package does for zellij tabs.
package herdr

import (
	"encoding/json"
	"os"
	"os/exec"
)

const tabPrefix = "\uf023 " // nerd-font lock, as zellij tabs get

// Session is the Herdr terminal lid runs in. Herdr sets HC_TERMINAL_ID in
// every terminal it runs; outside Herdr it is empty and Session does nothing.
type Session struct {
	TerminalID string
	Run        func(argv []string) ([]byte, error)
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

func run(argv []string) ([]byte, error) {
	return exec.Command(argv[0], argv[1:]...).Output()
}
