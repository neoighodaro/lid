package backend

import (
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func TestGetReturnsTheNonoBackend(t *testing.T) {
	b := Get(config.BackendNono)
	if b == nil {
		t.Fatal("Get(nono) = nil")
	}
	if b.Name() != config.BackendNono {
		t.Errorf("Name() = %q, want %q", b.Name(), config.BackendNono)
	}
}

func TestGetUnknownBackendReturnsNil(t *testing.T) {
	if b := Get("podman"); b != nil {
		t.Errorf("Get(podman) = %v, want nil", b)
	}
}

func TestEveryRegisteredBackendIsOneConfigAccepts(t *testing.T) {
	for _, name := range Names() {
		if !contains(config.Backends, name) {
			t.Errorf("registry has %q, which config.Backends does not list", name)
		}
	}
}

func TestNamesIsSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("Names() is not sorted: %v", names)
		}
	}
}

// nono announces new releases on the same output lid reads its JSON from, so a
// session list arrived behind three lines of update notice and lid called the
// whole thing unreadable.
func TestASessionListIsStillReadWhenTheToolAnnouncesAnUpdateFirst(t *testing.T) {
	out := `  update 0.75.0 available (current: 0.74.0)
  $ brew upgrade nono
  https://github.com/always-further/nono/releases/tag/v0.75.0

[
  {"session_id": "833c724a63df9fef", "name": "hey-eco", "status": "running"}
]`

	sessions, err := parseList[struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}](out, "nono ps --json", "sessions")
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Name != "hey-eco" || sessions[0].Status != "running" {
		t.Fatalf("got %+v, want the one hey-eco session", sessions)
	}
}

func TestAListIsStillReadWhenTheToolPrintsSomethingAfterTheJSON(t *testing.T) {
	out := "[{\"name\": \"hey-eco\"}]\nupdate 0.75.0 available\n"

	list, err := parseList[struct {
		Name string `json:"name"`
	}](out, "nono ps --json", "sessions")
	if err != nil {
		t.Fatalf("parseList: %v", err)
	}
	if len(list) != 1 || list[0].Name != "hey-eco" {
		t.Fatalf("got %+v, want the one hey-eco entry", list)
	}
}

func TestOutputWithNoJSONInItIsStillReportedByItsFirstLine(t *testing.T) {
	_, err := parseList[struct{}]("nono: unknown flag --json\nusage: nono ps", "nono ps --json", "sessions")
	if err == nil {
		t.Fatal("want an error for output that carries no JSON")
	}
	if !strings.Contains(err.Error(), "unknown flag --json") {
		t.Fatalf("got %q, want the first line of what the tool printed", err.Error())
	}
}
