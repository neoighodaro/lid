package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnlyAnExactTagCountsAsAReleaseBuild(t *testing.T) {
	for version, want := range map[string]bool{
		"v1.2.3":            true,
		"v0.1.0":            true,
		"dev":               false,
		"v1.2.3-4-g8a17b2a": false,
		"v1.2.3-dirty":      false,
		"8a17b2a-dirty":     false,
		"v1.2.3+build":      false,
		"v1.2.3-rc1":        false,
		"":                  false,
	} {
		if got := IsRelease(version); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v: a build from source must never install over itself", version, got, want)
		}
	}
}

func TestOnlyAHigherVersionIsWorthInstalling(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v1.2.3", "v1.2.4", true},
		{"v1.2.3", "v2.0.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.2", false},
		{"dev", "v1.2.3", false},
		{"v1.2.3", "not-a-version", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestTheUpdateCheckStaysOutOfSandboxesPipesAndItsOwnCommands(t *testing.T) {
	none := func(string) string { return "" }
	set := func(name string) func(string) string {
		return func(want string) string {
			if want == name {
				return "1"
			}
			return ""
		}
	}

	if Suppressed(none, true, "start") {
		t.Error("an interactive 'lid start' on a real terminal never checks for updates")
	}
	for _, name := range []string{"LID_NO_UPDATE_CHECK", "IS_SANDBOX", "CI"} {
		if !Suppressed(set(name), true, "start") {
			t.Errorf("%s does not stop the update check", name)
		}
	}
	if !Suppressed(none, false, "start") {
		t.Error("lid piped into another command still prints an update notice, which corrupts what it writes")
	}
	for _, command := range []string{"update", CheckCommand, "version"} {
		if !Suppressed(none, true, command) {
			t.Errorf("'lid %s' triggers its own background update check", command)
		}
	}
}

func TestTheRepositoryCanBePointedSomewhereElse(t *testing.T) {
	if got := Repo(func(string) string { return "" }); got != DefaultRepo {
		t.Errorf("Repo() = %q, want %q", got, DefaultRepo)
	}
	env := func(name string) string {
		if name == "LID_UPDATE_REPO" {
			return " someone/fork "
		}
		return ""
	}
	if got := Repo(env); got != "someone/fork" {
		t.Errorf("Repo() = %q, want the repository from the environment", got)
	}
}

func TestACheckIsDueOnlyOnceTheIntervalHasPassed(t *testing.T) {
	now := time.Now()
	if !(State{}).Due(now, CheckInterval) {
		t.Error("a lid that has never checked is not due, so it would never check at all")
	}
	if (State{LastChecked: now.Add(-time.Hour)}).Due(now, CheckInterval) {
		t.Error("lid checked github an hour ago and wants to again, which hammers the API")
	}
	if !(State{LastChecked: now.Add(-25 * time.Hour)}).Due(now, CheckInterval) {
		t.Error("yesterday's check still counts as fresh, so a new release goes unnoticed")
	}
}

func TestACorruptStateFileReadsAsNeverHavingChecked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.json")

	if state := LoadState(path); !state.LastChecked.IsZero() || state.LatestVersion != "" {
		t.Errorf("state = %+v, want a zero state for a file that is not there", state)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state := LoadState(path); !state.LastChecked.IsZero() {
		t.Error("a corrupt cache file was trusted, so lid could announce a version nobody published")
	}
}

func TestTheCheckIsRecordedEvenWhenGithubCannotBeReached(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.json")
	now := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()

	c := NewClient("neoighodaro/lid")
	c.BaseURL = server.URL

	if err := RunCheck(context.Background(), c, path, now); err == nil {
		t.Fatal("a failing check reported success")
	}
	if state := LoadState(path); state.LastChecked.IsZero() {
		t.Error("a failed check was not recorded, so lid would retry github on every single command")
	}
}

func TestASuccessfulCheckRemembersTheVersionItFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "v9.9.9"}`))
	}))
	defer server.Close()

	c := NewClient("neoighodaro/lid")
	c.BaseURL = server.URL

	if err := RunCheck(context.Background(), c, path, time.Now()); err != nil {
		t.Fatalf("RunCheck: %v", err)
	}
	if state := LoadState(path); state.LatestVersion != "v9.9.9" {
		t.Errorf("LatestVersion = %q, want the tag github reported", state.LatestVersion)
	}
}

// TestTheBackgroundCheckDoesNotSitInTheDirectoryItWasStartedFrom points Spawn at
// a script that records where it ran. The check outlives the shell that started
// it, and a sandbox that drops the cache behind its shared mount strands any
// process still holding a directory on it, so the check must not hold one.
func TestTheBackgroundCheckDoesNotSitInTheDirectoryItWasStartedFrom(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if here == "/" {
		t.Skip("the test already runs from the root, so it cannot tell the two apart")
	}

	dir := t.TempDir()
	record := filepath.Join(dir, "cwd")
	script := filepath.Join(dir, "fake")
	body := "#!/bin/sh\nif [ \"$1\" = \"" + CheckCommand + "\" ]; then pwd > \"" + record + "\"; fi\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Spawn(script); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(record)
		if err != nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if got := strings.TrimSpace(string(data)); got != "/" {
			t.Fatalf("the background check ran from %q, so it holds a directory it never reads", got)
		}
		return
	}
	t.Fatal("the background check never ran")
}
