package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/app"
)

func doctor(t *testing.T, dir string, h *twoBackendHost) (string, error) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	got, err := runTwo(t, dir, h, "doctor")
	return got.out, err
}

func exitedWith(t *testing.T, err error, code int) {
	t.Helper()
	var exit *app.ExitError
	switch {
	case code == 0 && err != nil:
		t.Fatalf("doctor: %v, want success", err)
	case code != 0 && (!errors.As(err, &exit) || exit.Code != code):
		t.Fatalf("doctor: %v, want exit %d", err, code)
	}
}

func lineFor(t *testing.T, out, name string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] == name {
			return line
		}
	}
	t.Fatalf("no %s line in:\n%s", name, out)
	return ""
}

func TestDoctorPassesWhenEverythingIsReady(t *testing.T) {
	out, err := doctor(t, apiProject(t, "sbx"), &twoBackendHost{})
	exitedWith(t, err, 0)
	for _, name := range []string{"lid", "config", "nono", "sbx", "agent", "project", "sandbox"} {
		lineFor(t, out, name)
	}
	if line := lineFor(t, out, "sbx"); !strings.HasPrefix(line, "✓") {
		t.Errorf("sbx line = %q, want it to pass", line)
	}
	if line := lineFor(t, out, "sandbox"); !strings.Contains(line, "not created") {
		t.Errorf("sandbox line = %q, want 'not created'", line)
	}
}

func TestDoctorFailsWhenTheProjectsBackendIsMissing(t *testing.T) {
	out, err := doctor(t, apiProject(t, "sbx"), &twoBackendHost{noSbx: true})
	exitedWith(t, err, 1)
	if line := lineFor(t, out, "sbx"); !strings.HasPrefix(line, "✗") {
		t.Errorf("sbx line = %q, want it to fail", line)
	}
	if line := lineFor(t, out, "sandbox"); !strings.Contains(line, "not checked") {
		t.Errorf("sandbox line = %q, want it skipped", line)
	}
}

func TestDoctorToleratesAMissingBackendTheProjectDoesNotUse(t *testing.T) {
	out, err := doctor(t, apiProject(t, "sbx"), &twoBackendHost{noNono: true})
	exitedWith(t, err, 0)
	line := lineFor(t, out, "nono")
	if !strings.HasPrefix(line, "-") || !strings.Contains(line, "only needed for --backend nono") {
		t.Errorf("nono line = %q, want it skipped as optional", line)
	}
}

func TestDoctorWithoutConfigNeedsOneBackend(t *testing.T) {
	out, err := doctor(t, t.TempDir(), &twoBackendHost{noSbx: true})
	exitedWith(t, err, 1)
	if line := lineFor(t, out, "config"); !strings.HasPrefix(line, "✗") {
		t.Errorf("config line = %q, want it to fail", line)
	}
	if line := lineFor(t, out, "sbx"); !strings.HasPrefix(line, "-") {
		t.Errorf("sbx line = %q, want it skipped while nono is ready", line)
	}
}

func TestDoctorFailsEveryBackendWhenNoneIsInstalled(t *testing.T) {
	out, err := doctor(t, t.TempDir(), &twoBackendHost{noSbx: true, noNono: true})
	exitedWith(t, err, 1)
	for _, name := range []string{"sbx", "nono"} {
		if line := lineFor(t, out, name); !strings.HasPrefix(line, "✗") {
			t.Errorf("%s line = %q, want it to fail", name, line)
		}
	}
}

func TestDoctorReportsARunningSandbox(t *testing.T) {
	out, err := doctor(t, apiProject(t, "sbx"), &twoBackendHost{sbxList: sbxRunningAPI})
	exitedWith(t, err, 0)
	if line := lineFor(t, out, "sandbox"); !strings.Contains(line, "running") {
		t.Errorf("sandbox line = %q, want running\n%s", line, out)
	}
}
