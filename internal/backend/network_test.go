package backend

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// execHost records the commands lid runs against a live sandbox, so these tests
// never need sbx installed.
type execHost struct {
	fail bool
	runs [][]string
}

func (e *execHost) host() Host {
	return Host{
		Exec: func(argv []string, _, _ io.Writer) error {
			e.runs = append(e.runs, argv)
			if e.fail {
				return errors.New("exit status 1")
			}
			return nil
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func (e *execHost) lines() []string {
	lines := make([]string, 0, len(e.runs))
	for _, run := range e.runs {
		lines = append(lines, strings.Join(run, " "))
	}
	return lines
}

func TestSbxAllowsSeveralHostsInOneCall(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: sbx\n")
	h := &execHost{}

	if err := Get("sbx").Network(h.host(), cfg, NetworkAllow, []string{"github.com", "pypi.org"}); err != nil {
		t.Fatalf("Network: %v", err)
	}

	want := []string{"sbx policy allow network --sandbox api github.com,pypi.org"}
	if got := h.lines(); len(got) != 1 || got[0] != want[0] {
		t.Errorf("ran %v, want %v", got, want)
	}
}

func TestSbxDeniesThroughTheDenyRule(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: sbx\n")
	h := &execHost{}

	if err := Get("sbx").Network(h.host(), cfg, NetworkDeny, []string{"evil.example"}); err != nil {
		t.Fatalf("Network: %v", err)
	}

	want := "sbx policy deny network --sandbox api evil.example"
	if got := h.lines(); len(got) != 1 || got[0] != want {
		t.Errorf("ran %v, want [%q]", got, want)
	}
}

// 'sbx policy rm' names one resource at a time, so removing several hosts is
// several calls, not one comma-joined argument sbx would read as a hostname.
func TestSbxRemovesOneHostPerCall(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: sbx\n")
	h := &execHost{}

	if err := Get("sbx").Network(h.host(), cfg, NetworkRemove, []string{"github.com", "pypi.org"}); err != nil {
		t.Fatalf("Network: %v", err)
	}

	want := []string{
		"sbx policy rm network --sandbox api --resource github.com",
		"sbx policy rm network --sandbox api --resource pypi.org",
	}
	got := h.lines()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ran %v, want %v", got, want)
	}
}

func TestSbxScopesEveryRuleToThisProjectsSandbox(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: sbx\n")

	for _, action := range []NetworkAction{NetworkAllow, NetworkDeny, NetworkRemove} {
		h := &execHost{}
		if err := Get("sbx").Network(h.host(), cfg, action, []string{"github.com"}); err != nil {
			t.Fatalf("Network(%s): %v", action, err)
		}
		for _, line := range h.lines() {
			if !strings.Contains(line, "--sandbox api") {
				t.Errorf("%s ran %q, which changes policy for every sandbox on the machine", action, line)
			}
		}
	}
}

func TestSbxWithNoHostsRunsNothing(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: sbx\n")
	h := &execHost{}

	if err := Get("sbx").Network(h.host(), cfg, NetworkAllow, nil); err != nil {
		t.Fatalf("Network: %v", err)
	}

	if len(h.runs) != 0 {
		t.Errorf("ran %v, want nothing", h.lines())
	}
}

func TestSbxReportsARuleItCouldNotApply(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: sbx\n")
	h := &execHost{fail: true}

	err := Get("sbx").Network(h.host(), cfg, NetworkAllow, []string{"github.com"})

	f := wantFailure(t, err)
	if !strings.Contains(f.What, "github.com") || !strings.Contains(f.What, "api") {
		t.Errorf("What = %q, want the host and the sandbox named", f.What)
	}
	if f.Fix == "" {
		t.Error("a failed policy change left the user with no next step")
	}
}

// nono decides policy when a session launches. Reporting success for a change
// that never reached the running session would be worse than refusing.
func TestNonoRefusesToChangePolicyUnderARunningSession(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\n")

	for _, action := range []NetworkAction{NetworkAllow, NetworkDeny, NetworkRemove} {
		err := Get("nono").Network(Host{}, cfg, action, []string{"github.com"})

		f := wantFailure(t, err)
		if !errors.Is(err, ErrNetworkAtLaunch) {
			t.Errorf("%s refusal cannot be told apart from a real failure", action)
		}
		if !strings.Contains(f.Fix, "network:") || !strings.Contains(f.Fix, "lid start") {
			t.Errorf("%s fix = %q, want it to point at the config and 'lid start'", action, f.Fix)
		}
		if f.In != cfg.Ref() {
			t.Errorf("%s in = %q, want %q", action, f.In, cfg.Ref())
		}
	}
}
