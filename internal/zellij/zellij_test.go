package zellij

import (
	"errors"
	"strings"
	"testing"
)

// fakeZellij answers the two questions lid asks before renaming a tab, and
// records the commands it was given so a test can name the one it cares about.
type fakeZellij struct {
	clients string
	tabs    string
	broken  bool
	calls   [][]string
}

func (f *fakeZellij) session(paneID string) Session {
	return Session{
		PaneID: paneID,
		Run: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			if f.broken {
				return nil, errors.New("exit status 1")
			}
			switch {
			case strings.Contains(strings.Join(argv, " "), "list-clients"):
				return []byte(f.clients), nil
			case strings.Contains(strings.Join(argv, " "), "list-tabs"):
				return []byte(f.tabs), nil
			}
			return nil, nil
		},
	}
}

func (f *fakeZellij) renamedTo() (string, bool) {
	for _, call := range f.calls {
		if len(call) == 4 && call[2] == "rename-tab" {
			return call[3], true
		}
	}
	return "", false
}

// focused is a list-clients table whose one client sits in pane 5.
const focused = "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n1         terminal_5     lid start\n"

func tabsJSON(name string, floating bool) string {
	value := "false"
	if floating {
		value = "true"
	}
	return `[{"name": "other", "active": false, "are_floating_panes_visible": false},
	          {"name": "` + name + `", "active": true, "are_floating_panes_visible": ` + value + `}]`
}

func TestRenameTabNamesTheTabTheStartWasLaunchedFrom(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", false)}

	f.session("5").RenameTab("api")

	name, ok := f.renamedTo()
	if !ok {
		t.Fatalf("the tab was never renamed: %v", f.calls)
	}
	if !strings.HasSuffix(name, "api") {
		t.Errorf("renamed to %q, want it to carry the display name", name)
	}
}

func TestRenameTabDoesNothingOutsideZellij(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", false)}

	f.session("").RenameTab("api")

	if len(f.calls) != 0 {
		t.Errorf("ran zellij from a plain terminal: %v", f.calls)
	}
}

func TestRenameTabDoesNothingWithoutADisplayName(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", false)}

	f.session("5").RenameTab("")

	if len(f.calls) != 0 {
		t.Errorf("ran zellij with nothing to rename the tab to: %v", f.calls)
	}
}

func TestRenameTabLeavesTheTabTheUserSwitchedToAlone(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", false)}

	f.session("9").RenameTab("api")

	if name, ok := f.renamedTo(); ok {
		t.Errorf("renamed %q while another pane held the focus", name)
	}
}

func TestRenameTabLeavesATabDrivenFromAFloatingPaneAlone(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", true)}

	f.session("5").RenameTab("api")

	if name, ok := f.renamedTo(); ok {
		t.Errorf("renamed %q from a transient floating pane", name)
	}
}

func TestRenameTabDoesNotRepeatANameTheTabAlreadyHas(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON(tabPrefix+"api", false)}

	f.session("5").RenameTab("api")

	if name, ok := f.renamedTo(); ok {
		t.Errorf("renamed %q to the name it already had", name)
	}
}

func TestRenameTabGivesUpQuietlyWhenZellijCannotAnswer(t *testing.T) {
	f := &fakeZellij{broken: true}

	f.session("5").RenameTab("api")

	if name, ok := f.renamedTo(); ok {
		t.Errorf("renamed %q on a session it could not read", name)
	}
}

func TestRenameTabIgnoresTabsItCannotParse(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: "not json"}

	f.session("5").RenameTab("api")

	if name, ok := f.renamedTo(); ok {
		t.Errorf("renamed %q from output it did not understand", name)
	}
}

func TestRenameTabIgnoresAClientListWithNoRows(t *testing.T) {
	f := &fakeZellij{clients: "CLIENT_ID ZELLIJ_PANE_ID RUNNING_COMMAND\n", tabs: tabsJSON("Tab #1", false)}

	f.session("5").RenameTab("api")

	if name, ok := f.renamedTo(); ok {
		t.Errorf("renamed %q with no client focused anywhere", name)
	}
}

func TestRenameTabDoesNotMatchAPaneIdByPrefix(t *testing.T) {
	f := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", false)}

	f.session("5").RenameTab("api")
	if _, ok := f.renamedTo(); !ok {
		t.Fatal("the matching pane was not recognised at all")
	}

	other := &fakeZellij{clients: focused, tabs: tabsJSON("Tab #1", false)}
	other.session("55").RenameTab("api")
	if name, ok := other.renamedTo(); ok {
		t.Errorf("renamed %q for pane 55 while pane 5 held the focus", name)
	}
}
