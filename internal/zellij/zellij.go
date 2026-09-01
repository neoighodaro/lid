package zellij

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

const tabPrefix = " "

type Session struct {
	PaneID string
	Run    func(argv []string) ([]byte, error)
}

type tab struct {
	Name            string `json:"name"`
	Active          bool   `json:"active"`
	FloatingVisible bool   `json:"are_floating_panes_visible"`
}

func New() Session {
	return Session{PaneID: os.Getenv("ZELLIJ_PANE_ID"), Run: run}
}

func (s Session) RenameTab(display string) {
	if display == "" || s.PaneID == "" || s.Run == nil {
		return
	}
	if !s.focused() {
		return
	}

	active, ok := s.activeTab()
	if !ok || active.FloatingVisible {
		return
	}

	name := tabPrefix + display
	if active.Name == name {
		return
	}
	s.Run([]string{"zellij", "action", "rename-tab", name})
}

func (s Session) focused() bool {
	out, err := s.Run([]string{"zellij", "action", "list-clients"})
	if err != nil {
		return false
	}
	return focusesPane(string(out), s.PaneID)
}

func (s Session) activeTab() (tab, bool) {
	out, err := s.Run([]string{"zellij", "action", "list-tabs", "--state", "--json"})
	if err != nil {
		return tab{}, false
	}

	var tabs []tab
	if err := json.Unmarshal(out, &tabs); err != nil {
		return tab{}, false
	}
	for _, t := range tabs {
		if t.Active {
			return t, true
		}
	}
	return tab{}, false
}

func focusesPane(listClients, paneID string) bool {
	want := "terminal_" + paneID
	lines := strings.Split(listClients, "\n")
	if len(lines) < 2 {
		return false
	}
	for _, line := range lines[1:] {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[1] == want {
			return true
		}
	}
	return false
}

func run(argv []string) ([]byte, error) {
	return exec.Command(argv[0], argv[1:]...).Output()
}
