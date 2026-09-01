package backend

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/handoff"
)

// nonoFake records the host commands lid would run for a nono session, so the
// script steps can be asserted without running anything.
type nonoFake struct {
	fail  string
	calls [][]string
}

func (f *nonoFake) host() Host {
	return Host{
		LookPath: func(name string) (string, error) { return "/usr/local/bin/" + name, nil },
		Output: func(argv []string) ([]byte, error) {
			f.calls = append(f.calls, argv)
			return nil, nil
		},
		Exec: func(argv []string, stdout, stderr io.Writer) error {
			f.calls = append(f.calls, argv)
			if f.fail != "" && strings.Contains(strings.Join(argv, " "), f.fail) {
				return errors.New("exit status 1")
			}
			return nil
		},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func (f *nonoFake) lines() string {
	var out []string
	for _, call := range f.calls {
		out = append(out, strings.Join(call, " "))
	}
	return strings.Join(out, "\n")
}

func (f *nonoFake) count(fragment string) int {
	n := 0
	for _, call := range f.calls {
		if strings.Contains(strings.Join(call, " "), fragment) {
			n++
		}
	}
	return n
}

func TestHandoffGivesTheAgentItsNameAndTheBrokerAddress(t *testing.T) {
	env := handoffEnv(sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n"))

	if env[handoffSandboxV] != "api" {
		t.Errorf("%s = %q, want the sandbox name threads are addressed to", handoffSandboxV, env[handoffSandboxV])
	}
	if env[handoffURLVar] != "http://host.docker.internal:8787" {
		t.Errorf("%s = %q, want the host loopback as a sandbox sees it", handoffURLVar, env[handoffURLVar])
	}
}

func TestHandoffStaysOutOfTheWayWhenItIsOff(t *testing.T) {
	if env := handoffEnv(sbxCfg(t, "name: api\n")); len(env) != 0 {
		t.Errorf("env = %v, want nothing when handoff is not enabled", env)
	}
}

func TestHandoffKeepsTheConfiguredURL(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n  url: http://host.docker.internal:9000\n")

	if got := handoffEnv(cfg)[handoffURLVar]; got != "http://host.docker.internal:9000" {
		t.Errorf("%s = %q, want the configured url", handoffURLVar, got)
	}
	if got := handoffResource(cfg); got != "localhost:9000" {
		t.Errorf("resource = %q, want the rule to follow the configured port", got)
	}
}

func TestSbxOpensTheBrokerPortAndTheBridgePorts(t *testing.T) {
	fake := &sbxFake{}
	cfg := sbxCfg(t, "name: api\nnetwork:\n  - example.com\nhandoff:\n  enabled: true\nbridges:\n  - name: db\n    port: 5432\n    host_port: 15432\n")

	if _, err := Get(config.BackendSbx).Provision(fake.host(), cfg, Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !fake.ran("policy", "allow", "network", "example.com,localhost:8787,localhost:15432") {
		t.Errorf("no rule allowed the broker and the bridge host port:\n%s", fake.lines())
	}
}

func TestSbxTellsTheAgentHowToReachTheBroker(t *testing.T) {
	fake := &sbxFake{}
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n")

	if _, err := Get(config.BackendSbx).Provision(fake.host(), cfg, Options{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !fake.ran("create", "LID_HANDOFF_URL=http://host.docker.internal:8787") || !fake.ran("create", "LID_SANDBOX=api") {
		t.Errorf("the sandbox was created without the handoff environment:\n%s", fake.lines())
	}
}

// The broker being reachable is not enough: without the skill nothing tells the
// agent to watch for questions, so they sit unanswered.
func TestSbxInstallsTheSkillThatTeachesTheAgentToWatch(t *testing.T) {
	fake := &sbxFake{hash: "x"}
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n")

	if err := Get(config.BackendSbx).PrepareAgent(fake.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if !fake.ran(".claude/skills/handoff/SKILL.md") {
		t.Errorf("the handoff skill was not written:\n%s", fake.lines())
	}
	if !fake.ran("waiting_on=$LID_SANDBOX") {
		t.Errorf("the skill does not tell the agent what to watch:\n%s", fake.lines())
	}
}

func TestSbxWritesNoHandoffSkillWhenHandoffIsOff(t *testing.T) {
	fake := &sbxFake{hash: "x"}

	if err := Get(config.BackendSbx).PrepareAgent(fake.host(), sbxCfg(t, "name: api\n")); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if fake.ran("skills/handoff") {
		t.Errorf("a handoff skill was installed without handoff enabled:\n%s", fake.lines())
	}
}

func TestNonoLetsTheSessionReachTheBroker(t *testing.T) {
	profile := render(t, "name: api\nbackend: nono\nhandoff:\n  enabled: true\n")

	if !strings.Contains(profile, `"localhost"`) {
		t.Errorf("profile does not allow the broker host:\n%s", profile)
	}
	if !strings.Contains(profile, "LID_HANDOFF_URL") || !strings.Contains(profile, "http://localhost:8787") {
		t.Errorf("profile does not carry the handoff environment:\n%s", profile)
	}
}

// A nono session is a host process, so its broker is on plain localhost — not
// the host.docker.internal a container has to go through.
func TestNonoPointsAtLocalhostRatherThanTheDockerHostAlias(t *testing.T) {
	cfg := cfgFor(t, "name: api\nbackend: nono\nhandoff:\n  enabled: true\n")

	if got := cfg.HandoffURL(); got != "http://localhost:8787" {
		t.Errorf("HandoffURL = %q, want plain localhost for a host process", got)
	}
}

func TestNonoRunsOnCreateOnceAndOnStartEveryTime(t *testing.T) {
	fake := &nonoFake{}
	cfg := cfgFor(t, "name: api\nbackend: nono\nscripts:\n  on_create:\n    - composer install\n  on_start:\n    - echo hi\n")

	for i := 0; i < 2; i++ {
		if err := Get(config.BackendNono).PrepareAgent(fake.host(), cfg); err != nil {
			t.Fatalf("PrepareAgent %d: %v", i, err)
		}
	}

	if got := fake.count("composer install"); got != 1 {
		t.Errorf("on_create ran %d times across two starts, want once", got)
	}
	if got := fake.count("echo hi"); got != 2 {
		t.Errorf("on_start ran %d times across two starts, want both", got)
	}
}

// Adding an on_create command to a project that is already set up must not run
// it: "once" means once per project, not once per command.
func TestNonoDoesNotRunOnCreateAddedAfterTheProjectWasSetUp(t *testing.T) {
	fake := &nonoFake{}
	before := cfgFor(t, "name: api\nbackend: nono\n")
	if err := Get(config.BackendNono).PrepareAgent(fake.host(), before); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	after := *before
	after.Scripts.OnCreate = []string{"composer install"}
	if err := Get(config.BackendNono).PrepareAgent(fake.host(), &after); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}

	if fake.count("composer install") != 0 {
		t.Errorf("on_create ran against an already-created project:\n%s", fake.lines())
	}
}

func TestNonoRunsScriptsInTheProjectDirectory(t *testing.T) {
	fake := &nonoFake{}
	cfg := cfgFor(t, "name: api\nbackend: nono\nscripts:\n  on_start:\n    - pwd\n")

	if err := Get(config.BackendNono).PrepareAgent(fake.host(), cfg); err != nil {
		t.Fatalf("PrepareAgent: %v", err)
	}
	if !strings.Contains(fake.lines(), "cd \""+cfg.ProjectDir+"\"") {
		t.Errorf("the command did not run in the project directory:\n%s", fake.lines())
	}
}

func TestNonoStopsWhenAnOnCreateCommandFails(t *testing.T) {
	fake := &nonoFake{fail: "migrate"}
	cfg := cfgFor(t, "name: api\nbackend: nono\nscripts:\n  on_create:\n    - migrate\n")

	err := Get(config.BackendNono).PrepareAgent(fake.host(), cfg)
	if err == nil {
		t.Fatal("PrepareAgent: want an error when the create step fails")
	}
	if _, statErr := os.Stat(filepath.Join(cfg.KitDir(), "nono-created")); statErr == nil {
		t.Error("the project was marked created even though on_create failed")
	}
}

// The two script hooks are unrelated, and the session comes up either way, so a
// broken create step must not also take away the start step.
func TestNonoStillRunsOnStartWhenOnCreateFails(t *testing.T) {
	fake := &nonoFake{fail: "migrate"}
	cfg := cfgFor(t, "name: api\nbackend: nono\nscripts:\n  on_create:\n    - migrate\n  on_start:\n    - echo hi\n")

	if err := Get(config.BackendNono).PrepareAgent(fake.host(), cfg); err == nil {
		t.Fatal("PrepareAgent: want the create failure reported")
	}
	if fake.count("echo hi") != 1 {
		t.Errorf("on_start was skipped because on_create failed:\n%s", fake.lines())
	}
}

func TestSbxNamesEachAllowedHostOnce(t *testing.T) {
	cfg := sbxCfg(t, `name: api
network:
  - localhost:8787
  - example.com
handoff:
  enabled: true
bridges:
  - name: db
    port: 5432
    host_port: 5432
  - name: replica
    port: 5433
    host_port: 5432
`)
	allow := sbxNetworkAllow(cfg)
	seen := map[string]int{}
	for _, entry := range allow {
		seen[entry]++
	}
	for entry, n := range seen {
		if n > 1 {
			t.Errorf("%s appears %d times in %v", entry, n, allow)
		}
	}
	if !contains(allow, "localhost:8787") || !contains(allow, "localhost:5432") {
		t.Errorf("allow = %v, want the broker and the bridge host ports", allow)
	}
}

// The handoff variables are lid's, not the project's. A bad env file loses the
// project's own variables either way, but dropping these would hand the agent a
// broker address it does not have.
func TestSbxStillNamesTheBrokerWhenAnEnvFileIsUnreadable(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n")
	cfg.EnvFile = []string{"nowhere.env"}

	argv := strings.Join(sbxCreateArgv(cfg), " ")
	if !strings.Contains(argv, handoffURLVar+"=") {
		t.Errorf("argv = %s, want the broker address", argv)
	}
}

func TestInstallingTheSkillOnTheHostRefusesToClobberSomeoneElses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, filepath.FromSlash(handoffSkillFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("my own handoff skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := cfgFor(t, "name: api\nbackend: nono\nhandoff:\n  enabled: true\n")
	fake := &nonoFake{}

	if _, err := InstallHandoffSkill(fake.host(), cfg, false); err == nil {
		t.Fatal("InstallHandoffSkill: want a refusal over a skill lid did not write")
	}
	if body, _ := os.ReadFile(path); string(body) != "my own handoff skill\n" {
		t.Error("the existing skill was overwritten anyway")
	}

	if _, err := InstallHandoffSkill(fake.host(), cfg, true); err != nil {
		t.Fatalf("InstallHandoffSkill --force: %v", err)
	}
	if body, _ := os.ReadFile(path); string(body) != handoffSkill {
		t.Error("--force did not replace the skill")
	}
}

func TestInstallingTheSkillTwiceIsNotAClobber(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := cfgFor(t, "name: api\nbackend: nono\nhandoff:\n  enabled: true\n")
	fake := &nonoFake{}

	path, err := InstallHandoffSkill(fake.host(), cfg, false)
	if err != nil {
		t.Fatalf("InstallHandoffSkill: %v", err)
	}
	if _, err := InstallHandoffSkill(fake.host(), cfg, false); err != nil {
		t.Fatalf("InstallHandoffSkill again: %v", err)
	}
	if body, _ := os.ReadFile(path); string(body) != handoffSkill {
		t.Error("the second install did not leave the skill in place")
	}
}

func TestInstallingTheSkillInASandboxWritesItThere(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n")
	fake := &sbxFake{hash: "written"}

	if _, err := InstallHandoffSkill(fake.host(), cfg, false); err != nil {
		t.Fatalf("InstallHandoffSkill: %v", err)
	}
	if !strings.Contains(fake.lines(), handoffSkillFile) {
		t.Errorf("the skill was not written into the sandbox:\n%s", fake.lines())
	}
}

// nono cannot install the skill on its own, so the note is the only thing that
// tells anyone the command exists — and it has to stop once it has been run.
func TestNonoSaysHowToInstallTheSkillUntilItIsInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := cfgFor(t, "name: api\nbackend: nono\nhandoff:\n  enabled: true\n")

	if got := handoffSkillNote(cfg); len(got) != 1 {
		t.Fatalf("notes = %v, want the install hint", got)
	}
	if _, err := InstallHandoffSkill((&nonoFake{}).host(), cfg, false); err != nil {
		t.Fatalf("InstallHandoffSkill: %v", err)
	}
	if got := handoffSkillNote(cfg); len(got) != 0 {
		t.Errorf("notes = %v, want silence once it is installed", got)
	}
}

func TestASandboxNeedsNoInstallHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := sbxCfg(t, "name: api\nhandoff:\n  enabled: true\n")
	if got := handoffSkillNote(cfg); len(got) != 0 {
		t.Errorf("notes = %v, want none: sbx installs the skill on every start", got)
	}
}

func skillMonitorLoop(t *testing.T) string {
	t.Helper()
	section := handoffSkill[strings.Index(handoffSkill, "## Start monitoring"):]
	start := strings.Index(section, "```bash\n")
	if start < 0 {
		t.Fatal("the skill has no monitor loop")
	}
	block := section[start+len("```bash\n"):]
	end := strings.Index(block, "```")
	if end < 0 {
		t.Fatal("the monitor loop is unterminated")
	}
	// sleep 5 is the retry pause for a dead broker; the test cannot wait it out.
	return strings.Replace(block[:end], "sleep 5", "sleep 0.2", 1)
}

func runSkillMonitor(t *testing.T, url, name string) *lockedBuffer {
	t.Helper()
	script := filepath.Join(t.TempDir(), "monitor.sh")
	if err := os.WriteFile(script, []byte(skillMonitorLoop(t)), 0o755); err != nil {
		t.Fatal(err)
	}
	out := lockedBuffer{}
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "LID_HANDOFF_URL="+url, "LID_SANDBOX="+name)
	cmd.Stdout = &out
	// Its own process group: killing bash alone leaves curl parked on a long
	// poll, and the server cannot close with that connection still open.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})
	return &out
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForLine(t *testing.T, out *lockedBuffer, want string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if got := out.String(); strings.Contains(got, want) {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waited %s for %q, got:\n%s", within, want, out.String())
	return ""
}

func TestTheSkillsMonitorLoopDeliversATurnOnce(t *testing.T) {
	store := handoff.NewStore(filepath.Join(t.TempDir(), "threads.json"))
	server := httptest.NewServer(handoff.NewServer(store))
	t.Cleanup(server.Close)

	out := runSkillMonitor(t, server.URL, "api")
	time.Sleep(200 * time.Millisecond)

	thread, err := store.Open("web", "api", "auth header", "which one?")
	if err != nil {
		t.Fatal(err)
	}
	waitForLine(t, out, "handoff: web — auth header ["+thread.ID+"]", 5*time.Second)

	// The cursor is what keeps an unanswered thread from re-firing every poll.
	time.Sleep(700 * time.Millisecond)
	if n := strings.Count(out.String(), thread.ID); n != 1 {
		t.Errorf("the thread was reported %d times, want once:\n%s", n, out.String())
	}
}

// A monitor that only reports questions looks the same whether nobody is asking
// or the broker died, so the agent keeps saying it is watching.
func TestTheSkillsMonitorLoopSaysWhenTheBrokerIsGone(t *testing.T) {
	store := handoff.NewStore(filepath.Join(t.TempDir(), "threads.json"))
	var down atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		handoff.NewServer(store).ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	out := runSkillMonitor(t, server.URL, "api")
	down.Store(true)
	waitForLine(t, out, "broker unreachable", 5*time.Second)

	if n := strings.Count(out.String(), "broker unreachable"); n != 1 {
		t.Errorf("said it %d times, want once until it recovers:\n%s", n, out.String())
	}

	down.Store(false)
	waitForLine(t, out, "broker reachable again", 5*time.Second)
}
