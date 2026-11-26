package herdr

import (
	"errors"
	"testing"
)

// fakeHerdr answers `herdr-app ls` with a canned layout and records the
// commands it was given so a test can name the one it cares about.
type fakeHerdr struct {
	ls     string
	broken bool
	calls  [][]string
}

func (f *fakeHerdr) session(terminalID string) Session {
	return Session{
		TerminalID: terminalID,
		Run: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			if f.broken {
				return nil, errors.New("exit status 1")
			}
			if len(argv) >= 2 && argv[1] == "ls" {
				return []byte(f.ls), nil
			}
			return nil, nil
		},
	}
}

// renamed reports the tab id and name of the tab.rename lid ran, if any.
func (f *fakeHerdr) renamed() (id, name string, ok bool) {
	for _, call := range f.calls {
		if len(call) == 7 && call[1] == "do" && call[2] == "tab.rename" {
			return call[4], call[6], true
		}
	}
	return "", "", false
}

// lsReply is an `herdr-app ls` reply with terminal t3 in tab tb2 of the second
// space, which is not the active one, and t1 in the active tab tb1.
func lsReply(tb2Name string) string {
	return `{"spaces": [
	  {"id": "sp1", "name": "One", "active": true, "tabs": [
	    {"id": "tb1", "name": "shell", "active": true, "terminals": [
	      {"terminal": "t1", "pane": "p1", "name": null, "sub_agents": []}
	    ]}
	  ]},
	  {"id": "sp2", "name": "Two", "active": false, "tabs": [
	    {"id": "tb2", "name": "` + tb2Name + `", "active": true, "terminals": [
	      {"terminal": "t2", "pane": "p2", "name": null, "sub_agents": []},
	      {"terminal": "t3", "pane": "p3", "name": null, "sub_agents": []}
	    ]}
	  ]}
	]}`
}

func TestRenameTabNamesTheTabTheStartWasLaunchedFrom(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("2")}

	f.session("t3").RenameTab("api")

	id, name, ok := f.renamed()
	if !ok {
		t.Fatalf("the tab was never renamed: %v", f.calls)
	}
	if id != "tb2" {
		t.Errorf("renamed tab %q, want tb2, the one holding terminal t3", id)
	}
	if name != tabPrefix+"api" {
		t.Errorf("renamed to %q, want %q", name, tabPrefix+"api")
	}
}

func TestRenameTabDoesNothingOutsideHerdr(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("2")}

	f.session("").RenameTab("api")

	if len(f.calls) != 0 {
		t.Errorf("ran herdr-app from a plain terminal: %v", f.calls)
	}
}

func TestRenameTabDoesNothingWithoutADisplayName(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("2")}

	f.session("t3").RenameTab("")

	if len(f.calls) != 0 {
		t.Errorf("ran herdr-app with nothing to rename the tab to: %v", f.calls)
	}
}

func TestRenameTabDoesNotRepeatANameTheTabAlreadyHas(t *testing.T) {
	f := &fakeHerdr{ls: lsReply(tabPrefix + "api")}

	f.session("t3").RenameTab("api")

	if _, name, ok := f.renamed(); ok {
		t.Errorf("renamed the tab to %q, the name it already had", name)
	}
}

func TestRenameTabLeavesEveryTabAloneWhenItsTerminalIsNotListed(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("2")}

	f.session("t9").RenameTab("api")

	if id, _, ok := f.renamed(); ok {
		t.Errorf("renamed tab %q though terminal t9 is in none of them", id)
	}
}

func TestRenameTabFindsATerminalRunningAsASubAgent(t *testing.T) {
	f := &fakeHerdr{ls: `{"spaces": [{"id": "sp1", "tabs": [
	  {"id": "tb1", "name": "1", "terminals": [
	    {"terminal": "t1", "pane": "p1", "sub_agents": [{"terminal": "t4"}]}
	  ]}
	]}]}`}

	f.session("t4").RenameTab("api")

	if id, _, ok := f.renamed(); !ok || id != "tb1" {
		t.Errorf("renamed tab %q (ok=%v), want tb1, the tab its parent sits in", id, ok)
	}
}

func TestRenameTabGivesUpQuietlyWhenHerdrCannotAnswer(t *testing.T) {
	f := &fakeHerdr{broken: true}

	f.session("t3").RenameTab("api")

	if id, _, ok := f.renamed(); ok {
		t.Errorf("renamed tab %q on a window it could not read", id)
	}
}

func TestRenameTabIgnoresALayoutItCannotParse(t *testing.T) {
	f := &fakeHerdr{ls: "not json"}

	f.session("t3").RenameTab("api")

	if id, _, ok := f.renamed(); ok {
		t.Errorf("renamed tab %q from output it did not understand", id)
	}
}
