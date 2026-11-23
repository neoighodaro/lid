package cli

import (
	"strings"
	"testing"
)

func TestStartResumeContinuesTheLastSession(t *testing.T) {
	for _, args := range [][]string{{"start", "--resume"}, {"resume"}} {
		dir := apiProject(t, "sbx")
		h := &twoBackendHost{sbxList: sbxRunningAPI}

		if got, err := runTwo(t, dir, h, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, got.err)
		}
		if got := strings.Join(h.argv, " "); got != "sbx run --name api -- --continue" {
			t.Errorf("%v launched %q", args, got)
		}
	}
}

func TestResumePickOpensTheSessionPicker(t *testing.T) {
	for _, args := range [][]string{{"start", "--pick"}, {"resume", "--pick"}} {
		dir := apiProject(t, "nono")
		h := &twoBackendHost{}

		if got, err := runTwo(t, dir, h, args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, got.err)
		}
		if got := strings.Join(h.argv, " "); !strings.HasSuffix(got, " -- claude --resume") {
			t.Errorf("%v launched %q", args, got)
		}
	}
}

func TestResumeTakesABackend(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{}

	if got, err := runTwo(t, dir, h, "resume", "nono"); err != nil {
		t.Fatalf("resume nono: %v\n%s", err, got.err)
	}
	if len(h.argv) == 0 || h.argv[0] != "nono" {
		t.Errorf("launched %v, want nono", h.argv)
	}
}

func TestStartRefusesToResumeIntoARebuiltSandbox(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{sbxList: sbxRunningAPI}

	if _, err := runTwo(t, dir, h, "start", "--resume", "--rebuild"); err == nil {
		t.Fatal("want an error")
	}
	if h.argv != nil || len(h.execs) > 0 {
		t.Errorf("did work before refusing: execs %v, launched %v", h.execs, h.argv)
	}
}

func TestResumeRefusesAnAgentThatCannotResumeBeforeProvisioning(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: nono\nagent: shell\n")
	h := &twoBackendHost{}

	if _, err := runTwo(t, dir, h, "resume"); err == nil {
		t.Fatal("want an error")
	}
	if h.argv != nil || len(h.execs) > 0 {
		t.Errorf("did work before refusing: execs %v, launched %v", h.execs, h.argv)
	}
}
