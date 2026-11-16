package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
)

const (
	sbxRunningAPI  = `[{"id":"s1","name":"api","status":"running"}]`
	nonoRunningAPI = `[{"id":"n1","name":"api","profile":"api"}]`
)

// twoBackendHost answers sbx and nono separately, so a test can have one
// backend running while lid targets the other.
type twoBackendHost struct {
	noSbx    bool
	noNono   bool
	stdin    io.Reader
	sbxList  string
	nonoList string
	probes   [][]string
	execs    [][]string
	argv     []string
}

func (h *twoBackendHost) lookPath(name string) (string, error) {
	if (h.noSbx && (name == "sbx" || name == "docker")) || (h.noNono && name == "nono") {
		return "", errors.New("executable file not found in $PATH")
	}
	return "/opt/homebrew/bin/" + name, nil
}

func (h *twoBackendHost) output(argv []string) ([]byte, error) {
	h.probes = append(h.probes, argv)
	switch strings.Join(argv, " ") {
	case "nono --version":
		return []byte("nono 0.74.0\n"), nil
	case "sbx version":
		return []byte("sbx version v0.54.0 4bec4a5\n"), nil
	case "sbx ls --json":
		return []byte(orEmptyList(h.sbxList)), nil
	case "nono ps --json":
		return []byte(orEmptyList(h.nonoList)), nil
	}
	return []byte("Result: valid\n"), nil
}

func orEmptyList(list string) string {
	if list == "" {
		return "[]"
	}
	return list
}

func (h *twoBackendHost) exec(argv []string, _, _ io.Writer) error {
	h.execs = append(h.execs, argv)
	return nil
}

func (h *twoBackendHost) attach(argv []string, _ io.Reader, _, _ io.Writer) error {
	h.argv = argv
	return nil
}

func (h *twoBackendHost) probed(command string) bool {
	for _, argv := range h.probes {
		if strings.Join(argv, " ") == command {
			return true
		}
	}
	return false
}

func (h *twoBackendHost) ran(command string) bool {
	for _, argv := range h.execs {
		if strings.Join(argv, " ") == command {
			return true
		}
	}
	return false
}

func runTwo(t *testing.T, cwd string, h *twoBackendHost, args ...string) (result, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	stdin := h.stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	a := &app.App{
		Version: "v1.2.3",
		In:      stdin,
		Out:     &out,
		Err:     &errBuf,
		Cwd:     cwd,
		Host:    backend.Host{LookPath: h.lookPath, Output: h.output, Exec: h.exec},
		Attach:  h.attach,
	}
	root := NewRootCommand(a)
	root.SetArgs(args)
	err := root.Execute()
	return result{out: out.String(), err: errBuf.String()}, err
}

func apiProject(t *testing.T, backend string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: "+backend+"\n")
	return dir
}

func remembered(t *testing.T, dir string) string {
	t.Helper()
	sel, err := config.ReadSelection(dir)
	if err != nil {
		t.Fatal(err)
	}
	return sel.Backend
}

func TestStartRefusesWhileTheOtherBackendIsRunning(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{sbxList: sbxRunningAPI}

	_, err := runTwo(t, dir, h, "start", "nono")
	if err == nil || !strings.Contains(err.Error(), "running on sbx") || !strings.Contains(err.Error(), "lid restart nono") {
		t.Fatalf("err = %v, want the running sbx sandbox named with the way out", err)
	}
	if h.argv != nil {
		t.Errorf("launched %v anyway", h.argv)
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("remembered %q after a refused start", got)
	}
}

func TestStartWithNoSbxInstalledIgnoresSbx(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{noSbx: true}

	if got, err := runTwo(t, dir, h, "start", "nono"); err != nil {
		t.Fatalf("start nono: %v\n%s", err, got.err)
	}
	if h.argv == nil {
		t.Error("nothing launched")
	}
}

func TestStartRemembersTheBackendItLaunched(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{}

	if got, err := runTwo(t, dir, h, "start", "nono"); err != nil {
		t.Fatalf("start nono: %v\n%s", err, got.err)
	}
	if got := remembered(t, dir); got != config.BackendNono {
		t.Fatalf("remembered %q, want nono", got)
	}

	h.probes = nil
	if got, err := runTwo(t, dir, h, "status"); err != nil {
		t.Fatalf("status: %v\n%s", err, got.err)
	}
	if !h.probed("nono ps --json") || h.probed("sbx ls --json") {
		t.Errorf("status probed %v, want nono only", h.probes)
	}
}

func TestStartPrintRemembersNothing(t *testing.T) {
	dir := apiProject(t, "sbx")

	if _, err := runTwo(t, dir, &twoBackendHost{}, "start", "nono", "--print"); err != nil {
		t.Fatal(err)
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("remembered %q after --print", got)
	}
}

func TestAPlainStartRemembersNothing(t *testing.T) {
	dir := apiProject(t, "nono")
	h := &twoBackendHost{}

	if got, err := runTwo(t, dir, h, "start"); err != nil {
		t.Fatalf("start: %v\n%s", err, got.err)
	}
	if h.argv == nil || h.argv[0] != "nono" {
		t.Errorf("launched %v, want nono", h.argv)
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("remembered %q, want nothing for a start that named no backend", got)
	}
}

func TestAFailedStartRemembersNothing(t *testing.T) {
	dir := apiProject(t, "sbx")

	if _, err := runTwo(t, dir, &twoBackendHost{noNono: true}, "start", "nono"); err == nil {
		t.Fatal("start nono without nono installed succeeded")
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("remembered %q after a failed start", got)
	}
}

func startedOnNono(t *testing.T) string {
	t.Helper()
	dir := apiProject(t, "sbx")
	if got, err := runTwo(t, dir, &twoBackendHost{}, "start", "nono"); err != nil {
		t.Fatalf("start nono: %v\n%s", err, got.err)
	}
	return dir
}

func TestStopForgetsTheBackend(t *testing.T) {
	dir := startedOnNono(t)

	got, err := runTwo(t, dir, &twoBackendHost{}, "stop")
	if err != nil {
		t.Fatalf("stop: %v\n%s", err, got.err)
	}
	if !strings.Contains(got.err, "not running") {
		t.Errorf("stderr = %q, want it to say the session is not running", got.err)
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("still remembered %q after stop", got)
	}
}

func TestStopStopsTheRememberedBackend(t *testing.T) {
	dir := startedOnNono(t)
	h := &twoBackendHost{nonoList: nonoRunningAPI}

	if got, err := runTwo(t, dir, h, "stop"); err != nil {
		t.Fatalf("stop: %v\n%s", err, got.err)
	}
	if !h.ran("nono stop n1") || h.ran("sbx stop api") {
		t.Errorf("ran %v, want nono stop n1 only", h.execs)
	}
}

func TestStopForgetsEvenWhenTheBackendIsMissing(t *testing.T) {
	dir := startedOnNono(t)

	if _, err := runTwo(t, dir, &twoBackendHost{noNono: true}, "stop"); err == nil {
		t.Error("stop with nono missing succeeded")
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("still remembered %q", got)
	}
}

func TestStopForgetsWhenTheConfigNoLongerLoads(t *testing.T) {
	dir := startedOnNono(t)
	write(t, dir, "lid.yaml", "name: api\nbackends:\n  nono:\n    packages: [jq]\n")

	if _, err := runTwo(t, dir, &twoBackendHost{}, "stop"); err == nil {
		t.Error("stop with an invalid config succeeded")
	}
	if _, err := os.Stat(config.SelectionPath(dir)); !os.IsNotExist(err) {
		t.Errorf("backend.json still there: %v", err)
	}
}

func TestAFlagThatDisagreesWithTheRecordIsRefused(t *testing.T) {
	dir := startedOnNono(t)

	for _, args := range [][]string{
		{"stop", "-b", "sbx"},
		{"status", "-b", "sbx"},
		{"network", "allow", "x.com", "-b", "sbx"},
		{"handoff", "--install-skill", "-b", "sbx"},
		{"shell", "-b", "sbx"},
	} {
		_, err := runTwo(t, dir, &twoBackendHost{}, args...)
		if err == nil || !strings.Contains(err.Error(), "is on nono, not sbx") {
			t.Errorf("%v: err = %v, want the disagreement refused", args, err)
		}
	}
	if got := remembered(t, dir); got != config.BackendNono {
		t.Errorf("remembered %q, want nono left alone", got)
	}
}

func TestShellFromColdRemembersTheBackend(t *testing.T) {
	dir := apiProject(t, "sbx")

	if got, err := runTwo(t, dir, &twoBackendHost{}, "shell", "-b", "nono"); err != nil {
		t.Fatalf("shell -b nono: %v\n%s", err, got.err)
	}
	if got := remembered(t, dir); got != config.BackendNono {
		t.Errorf("remembered %q, want nono", got)
	}
}

func TestShellRefusesWhileTheOtherBackendIsRunning(t *testing.T) {
	dir := apiProject(t, "sbx")

	_, err := runTwo(t, dir, &twoBackendHost{sbxList: sbxRunningAPI}, "shell", "-b", "nono")
	if err == nil || !strings.Contains(err.Error(), "running on sbx") {
		t.Errorf("err = %v, want the running sbx sandbox named", err)
	}
}

func TestSbxCommandsPrintNoNonoWarnings(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\nmemory: be nice\nbackends:\n  nono:\n    paths:\n      read: [/opt]\n")

	got, err := runTwo(t, dir, &twoBackendHost{}, "start", "--print")
	if err != nil {
		t.Fatalf("start --print: %v\n%s", err, got.err)
	}
	if strings.Contains(got.err, "ignoring") || strings.Contains(got.err, "nono") {
		t.Errorf("stderr carries a nono warning during an sbx command:\n%s", got.err)
	}
}

func TestStopIgnoresAnAgentTheBackendCannotLaunch(t *testing.T) {
	dir := startedOnNono(t)
	write(t, dir, "lid.yaml", "name: api\nbackend: sbx\nagent: gemini\n")

	if got, err := runTwo(t, dir, &twoBackendHost{}, "stop"); err != nil {
		t.Errorf("stop: %v\n%s", err, got.err)
	}
}

func TestRestartSwitchesBackendAfterAsking(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{sbxList: sbxRunningAPI, stdin: strings.NewReader("y\n")}

	if got, err := runTwo(t, dir, h, "restart", "nono"); err != nil {
		t.Fatalf("restart nono: %v\n%s", err, got.err)
	}
	if !h.ran("sbx stop api") {
		t.Errorf("ran %v, want the sbx sandbox stopped", h.execs)
	}
	if h.argv == nil || h.argv[0] != "nono" {
		t.Errorf("launched %v, want nono", h.argv)
	}
	if got := remembered(t, dir); got != config.BackendNono {
		t.Errorf("remembered %q, want nono", got)
	}
}

func TestRestartSwitchLeftAloneWhenTheAnswerIsNo(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{sbxList: sbxRunningAPI, stdin: strings.NewReader("n\n")}

	if got, err := runTwo(t, dir, h, "restart", "nono"); err != nil {
		t.Fatalf("restart nono: %v\n%s", err, got.err)
	}
	if h.ran("sbx stop api") || h.argv != nil {
		t.Errorf("ran %v and launched %v after a no", h.execs, h.argv)
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("remembered %q after a no", got)
	}
}

func TestRestartForceSwitchesWithoutAsking(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{sbxList: sbxRunningAPI}

	if got, err := runTwo(t, dir, h, "restart", "nono", "-f"); err != nil {
		t.Fatalf("restart nono -f: %v\n%s", err, got.err)
	}
	if !h.ran("sbx stop api") || h.argv == nil {
		t.Errorf("ran %v, launched %v; want a switch", h.execs, h.argv)
	}
}

func TestRestartTakesTheBackendFlagToo(t *testing.T) {
	dir := apiProject(t, "sbx")
	h := &twoBackendHost{sbxList: sbxRunningAPI}

	if got, err := runTwo(t, dir, h, "restart", "-b", "nono", "-f"); err != nil {
		t.Fatalf("restart -b nono -f: %v\n%s", err, got.err)
	}
	if !h.ran("sbx stop api") || remembered(t, dir) != config.BackendNono {
		t.Errorf("ran %v; want sbx stopped and nono remembered", h.execs)
	}
}

func TestAPlainRestartIgnoresTheOtherBackend(t *testing.T) {
	dir := apiProject(t, "nono")
	h := &twoBackendHost{sbxList: sbxRunningAPI}

	if got, err := runTwo(t, dir, h, "restart", "-f"); err != nil {
		t.Fatalf("restart -f: %v\n%s", err, got.err)
	}
	if h.ran("sbx stop api") {
		t.Error("a plain restart stopped the sbx sandbox")
	}
	if got := remembered(t, dir); got != "" {
		t.Errorf("remembered %q after a plain restart", got)
	}
}

func TestStopKeepsTheRecordWhenOnlyTheCommandLineIsWrong(t *testing.T) {
	for _, args := range [][]string{
		{"stop", "-b", "sbox"},
		{"stop", "-C", "missing.yaml"},
	} {
		dir := startedOnNono(t)
		if _, err := runTwo(t, dir, &twoBackendHost{}, args...); err == nil {
			t.Errorf("%v succeeded", args)
		}
		if got := remembered(t, dir); got != config.BackendNono {
			t.Errorf("%v: remembered %q, want nono kept", args, got)
		}
	}
}

func TestStopLeavesACorruptRecordForTheUserToDelete(t *testing.T) {
	dir := startedOnNono(t)
	write(t, dir, ".config/lid/.kit/backend.json", "{")

	if _, err := runTwo(t, dir, &twoBackendHost{}, "stop"); err == nil || !strings.Contains(err.Error(), "delete it") {
		t.Errorf("err = %v, want the corrupt record reported", err)
	}
	if _, err := os.Stat(config.SelectionPath(dir)); err != nil {
		t.Errorf("backend.json was removed: %v", err)
	}
}

func TestStopSaysWhenItForgetsBecauseTheConfigNoLongerLoads(t *testing.T) {
	dir := startedOnNono(t)
	write(t, dir, "lid.yaml", "name: api\nbackends:\n  nono:\n    packages: [jq]\n")

	got, _ := runTwo(t, dir, &twoBackendHost{}, "stop")
	if !strings.Contains(got.err, "forgot") {
		t.Errorf("stderr:\n%s\nwant the forgotten backend mentioned", got.err)
	}
}
