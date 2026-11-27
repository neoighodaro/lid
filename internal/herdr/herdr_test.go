package herdr

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
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

func (f *fakeHerdr) splitSession(terminalID, reply string) Session {
	s := f.session(terminalID)
	inner := s.Run
	s.Run = func(argv []string) ([]byte, error) {
		if len(argv) >= 3 && argv[2] == "pane.split" {
			f.calls = append(f.calls, argv)
			return []byte(reply), nil
		}
		return inner(argv)
	}
	return s
}

func (f *fakeHerdr) split() []string {
	for _, call := range f.calls {
		if len(call) >= 3 && call[2] == "pane.split" {
			return call
		}
	}
	return nil
}

func TestSplitPaneTargetsLidsTerminal(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("shell")}
	argv := []string{"/bin/lid", "pane-run", "/x; $(id)"}

	if err := f.splitSession("t3", `{"pane":"p9","terminal":"t9"}`).SplitPane(argv); err != nil {
		t.Fatalf("SplitPane: %v", err)
	}

	call := f.split()
	if len(call) != 4 {
		t.Fatalf("split call = %q", call)
	}
	var got struct {
		Direction string   `json:"direction"`
		Pane      string   `json:"pane"`
		Argv      []string `json:"argv"`
	}
	if err := json.Unmarshal([]byte(call[3]), &got); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if got.Direction != "right" || got.Pane != "p3" || !reflect.DeepEqual(got.Argv, argv) {
		t.Errorf("payload = %+v", got)
	}
}

func TestSplitPaneFindsLidsTerminalAmongSubAgents(t *testing.T) {
	f := &fakeHerdr{ls: `{"spaces":[{"tabs":[{"id":"tb1","name":"x","terminals":[
	  {"terminal":"t1","pane":"p1","sub_agents":[{"terminal":"t7","pane":"p7","sub_agents":[]}]}]}]}]}`}

	if err := f.splitSession("t7", `{"pane":"p9","terminal":"t9"}`).SplitPane([]string{"a"}); err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	if !strings.Contains(f.split()[3], `"pane":"p7"`) {
		t.Errorf("split = %q, want pane p7", f.split())
	}
}

func TestSplitPaneOutsideHerdrErrors(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("shell")}

	if err := f.splitSession("", `{"pane":"p9"}`).SplitPane([]string{"a"}); err == nil {
		t.Error("SplitPane outside Herdr returned nil")
	}
	if len(f.calls) != 0 {
		t.Errorf("ran %v outside Herdr", f.calls)
	}
}

func TestSplitPaneErrorsWhenLidsPaneIsUnknown(t *testing.T) {
	for name, ls := range map[string]string{
		"not found":  lsReply("shell"),
		"empty pane": `{"spaces":[{"tabs":[{"id":"tb1","terminals":[{"terminal":"t3","pane":""}]}]}]}`,
	} {
		f := &fakeHerdr{ls: ls}
		id := "t404"
		if name == "empty pane" {
			id = "t3"
		}
		if err := f.splitSession(id, `{"pane":"p9"}`).SplitPane([]string{"a"}); err == nil {
			t.Errorf("%s: SplitPane returned nil", name)
		}
		if f.split() != nil {
			t.Errorf("%s: pane.split ran: %q", name, f.split())
		}
	}
}

func TestSplitPaneErrorsWhenHerdrOpensNoPane(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("shell")}

	if err := f.splitSession("t3", `{"pane":"","terminal":""}`).SplitPane([]string{"a"}); err == nil {
		t.Error("SplitPane accepted a reply with no pane")
	}
}

func TestSplitPaneRunsTheHerdrAppItWasGiven(t *testing.T) {
	f := &fakeHerdr{ls: lsReply("shell")}
	s := f.splitSession("t3", `{"pane":"p9","terminal":"t9"}`)
	s.Bin = "/opt/herdr/bin/herdr-app"

	if err := s.SplitPane([]string{"a"}); err != nil {
		t.Fatalf("SplitPane: %v", err)
	}
	for _, call := range f.calls {
		if call[0] != s.Bin {
			t.Errorf("ran %q, want %s, not herdr-app from PATH", call, s.Bin)
		}
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %q, want ls then pane.split", f.calls)
	}
}
