package template

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/runtime"
)

func cfgFor(t *testing.T, body string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lid.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(dir, "")
	if err != nil {
		t.Fatalf("Load: %v\nconfig:\n%s", err, body)
	}
	return cfg
}

func render(t *testing.T, body string) string {
	t.Helper()
	out, err := Render(cfgFor(t, body))
	if err != nil {
		t.Fatalf("Render: %v\nconfig:\n%s", err, body)
	}
	return out
}

func renderErr(t *testing.T, cfg *config.Config) error {
	t.Helper()
	out, err := Render(cfg)
	if err == nil {
		t.Fatalf("Render: want error, got:\n%s", out)
	}
	return err
}

func TestRenderStartsFromTheDefaultBaseImage(t *testing.T) {
	out := render(t, "name: lid\n")

	if want := "FROM " + config.DefaultBaseImage; !strings.HasPrefix(out, want) {
		t.Errorf("Dockerfile does not start with %q:\n%s", want, out)
	}
}

func TestRenderHonoursTheConfiguredBaseImage(t *testing.T) {
	out := render(t, "name: lid\nbase_image: ubuntu:24.04\n")

	if !strings.HasPrefix(out, "FROM ubuntu:24.04") {
		t.Errorf("Dockerfile does not use the configured base image:\n%s", out)
	}
	if strings.Contains(out, config.DefaultBaseImage) {
		t.Errorf("Dockerfile still mentions the default base image:\n%s", out)
	}
}

func TestRenderInstallsConfiguredPackages(t *testing.T) {
	out := render(t, "name: lid\npackages:\n  - ripgrep\n  - jq\n")

	if !strings.Contains(out, "locales ripgrep jq") {
		t.Errorf("packages are not installed alongside locales:\n%s", out)
	}
}

func TestRenderWithoutPackagesStillInstallsLocales(t *testing.T) {
	out := render(t, "name: lid\n")

	if !strings.Contains(out, "--no-install-recommends locales \\\n") {
		t.Errorf("locales line is malformed without packages:\n%s", out)
	}
}

func TestRenderPlacesRuntimeBlocksOnTheRightSideOfUserAgent(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  node: 22\n  python: \"3.12\"\n")

	split := strings.Index(out, "\nUSER agent")
	if split < 0 {
		t.Fatalf("Dockerfile never drops to the agent user:\n%s", out)
	}
	root, agent := out[:split], out[split:]

	if !strings.Contains(root, "deb.nodesource.com") {
		t.Errorf("node install belongs before USER agent:\n%s", out)
	}
	if !strings.Contains(agent, "uv python install 3.12") {
		t.Errorf("python install belongs after USER agent:\n%s", out)
	}
	if !strings.Contains(agent, `ENV PATH="/home/agent/.local/bin:$PATH"`) {
		t.Errorf("env block belongs after USER agent:\n%s", out)
	}
}

func TestRenderIsDeterministicAcrossRuntimeOrder(t *testing.T) {
	body := "name: lid\nruntimes:\n  rust: 1.81.0\n  node: 22\n  go: \"1.23\"\n  bun: 1.1.0\n"

	first := render(t, body)
	for i := 0; i < 20; i++ {
		if got := render(t, body); got != first {
			t.Fatalf("Dockerfile varies between renders:\n%s\n---\n%s", first, got)
		}
	}
}

func TestRenderValidatesRuntimeVersions(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nruntimes:\n  php: \"9.9\"\n")

	err := renderErr(t, cfg)
	if !strings.Contains(err.Error(), "php") || !strings.Contains(err.Error(), "9.9") {
		t.Errorf("error = %v, want it to name the runtime and version", err)
	}
}

func TestRenderPassesPHPExtensionsToTheRuntime(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  php: \"8.5\"\nphp:\n  extensions:\n    - imagick\n")

	if !strings.Contains(out, "php8.5-imagick") {
		t.Errorf("configured php extension is missing:\n%s", out)
	}
}

func TestRenderRejectsAnUnknownRuntime(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	cfg.Runtimes = map[string]config.Scalar{"perl": "5"}

	if err := renderErr(t, cfg); !strings.Contains(err.Error(), "perl") {
		t.Errorf("error = %v, want it to name the runtime", err)
	}
}

func TestRenderKeepsClaudeSetupForTheClaudeAgent(t *testing.T) {
	out := render(t, "name: lid\nagent: claude-code\nclaude:\n  plugins:\n    marketplaces:\n      - obra/superpowers\n    install:\n      - superpowers@obra\n")

	for _, want := range []string{
		"RUN claude update || true",
		"RUN claude plugin marketplace add obra/superpowers",
		"RUN claude plugin install superpowers@obra",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Dockerfile missing %q:\n%s", want, out)
		}
	}
}

func TestRenderOmitsClaudeSetupForOtherAgents(t *testing.T) {
	out := render(t, "name: lid\nagent: codex\nclaude:\n  plugins:\n    install:\n      - superpowers@obra\n")

	if strings.Contains(out, "RUN claude") {
		t.Errorf("Dockerfile runs claude setup for a non-claude agent:\n%s", out)
	}
	if !strings.Contains(out, "USER agent") {
		t.Errorf("Dockerfile must still drop to the agent user:\n%s", out)
	}
}

func TestRenderRefusesACustomTemplate(t *testing.T) {
	cfg := cfgFor(t, "name: lid\ntemplate: my-template\n")

	if err := renderErr(t, cfg); !strings.Contains(err.Error(), "my-template") {
		t.Errorf("error = %v, want it to name the template", err)
	}
}

func TestRenderRefusesABackendThatBuildsNoImage(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nbackend: nono\n")

	if err := renderErr(t, cfg); !strings.Contains(err.Error(), "nono") {
		t.Errorf("error = %v, want it to name the backend", err)
	}
}

func TestRenderEndsWithASingleTrailingNewline(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  node: 22\n")

	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("Dockerfile should end in exactly one newline, got %q", out[len(out)-5:])
	}
}

type recorder struct {
	calls [][]string
	err   error
}

func (r *recorder) exec(argv []string, _, _ io.Writer) error {
	r.calls = append(r.calls, argv)
	return r.err
}

func TestBuildWritesTheDockerfileIntoTheKitDir(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	rec := &recorder{}

	if err := (&Builder{Exec: rec.exec}).Build(cfg); err != nil {
		t.Fatalf("Build: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg.KitDir(), "Dockerfile"))
	if err != nil {
		t.Fatalf("reading generated Dockerfile: %v", err)
	}
	if !strings.HasPrefix(string(data), "FROM ") {
		t.Errorf("generated Dockerfile looks wrong:\n%s", data)
	}
}

func TestBuildInvokesDockerBuildWithTheImageNameAndKitDir(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	rec := &recorder{}

	if err := (&Builder{Exec: rec.exec}).Build(cfg); err != nil {
		t.Fatalf("Build: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("ran %d commands, want 1: %v", len(rec.calls), rec.calls)
	}
	want := []string{"docker", "build", "--pull", "-t", cfg.SandboxImageName(), cfg.KitDir()}
	if strings.Join(rec.calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", rec.calls[0], want)
	}
}

func TestBuildReportsAFailedDockerBuild(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")
	rec := &recorder{err: errors.New("exit status 1")}

	err := (&Builder{Exec: rec.exec}).Build(cfg)
	if err == nil {
		t.Fatal("Build: want error, got nil")
	}
	if !strings.Contains(err.Error(), "docker build") {
		t.Errorf("error = %v, want it to name the failing command", err)
	}
}

func TestBuildDoesNotRunDockerWhenRenderingFails(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nruntimes:\n  php: \"9.9\"\n")
	rec := &recorder{}

	if err := (&Builder{Exec: rec.exec}).Build(cfg); err == nil {
		t.Fatal("Build: want error, got nil")
	}
	if len(rec.calls) != 0 {
		t.Errorf("ran %v, want nothing", rec.calls)
	}
}

func TestRenderInstallsNodeBeforePnpm(t *testing.T) {
	out := render(t, "name: lid\nruntimes:\n  pnpm: 9.12.0\n  node: 22\n")

	node := strings.Index(out, "nodesource")
	pnpm := strings.Index(out, "npm install -g pnpm")
	if node < 0 || pnpm < 0 {
		t.Fatalf("expected both runtimes in the Dockerfile:\n%s", out)
	}
	if node > pnpm {
		t.Errorf("pnpm is installed before the npm it needs:\n%s", out)
	}
}

// The starter config is the first thing a user sees, so every version it
// suggests has to be one the runtimes actually accept. This guards the whole
// commented block, which no other test reads.
func TestStarterConfigSuggestsInstallableRuntimeVersions(t *testing.T) {
	starter, err := config.Starter(config.StarterOptions{Name: "probe", Backend: config.BackendSbx})
	if err != nil {
		t.Fatalf("Starter: %v", err)
	}

	suggestion := regexp.MustCompile(`^\s*#\s*([a-z]+):\s*"?([^"\s]+)"?\s*$`)
	inRuntimes := false
	found := 0

	for _, line := range strings.Split(starter, "\n") {
		if strings.HasPrefix(line, "runtimes:") {
			inRuntimes = true
			continue
		}
		if inRuntimes && strings.TrimSpace(line) == "" {
			break
		}
		m := suggestion.FindStringSubmatch(line)
		if !inRuntimes || m == nil {
			continue
		}
		name, version := m[1], m[2]
		r := runtime.Get(name, runtime.Options{})
		if r == nil {
			t.Errorf("starter suggests runtime %q, which is not registered", name)
			continue
		}
		if err := r.Validate(version); err != nil {
			t.Errorf("starter suggests %s: %q, which is invalid: %v", name, version, err)
		}
		found++
	}

	if found == 0 {
		t.Fatal("no runtime suggestions found in the starter config")
	}
}

func TestRenderSelectsTheLocaleItGenerates(t *testing.T) {
	out := render(t, "name: lid\n")

	locale := regexp.MustCompile(`locale\.gen && locale-gen`)
	if !locale.MatchString(out) {
		t.Fatalf("locale generation changed shape, update this test:\n%s", out)
	}
	if !strings.Contains(out, "ENV LANG=en_US.UTF-8") {
		t.Errorf("locale is generated but never selected, so the sandbox runs under C:\n%s", out)
	}
}

func TestRenderNamesTheConfigWhenARuntimeVersionIsWrong(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nruntimes:\n  php: \"8.5.1\"\n")

	_, err := Render(cfg)

	var f *failure.Error
	if !errors.As(err, &f) {
		t.Fatalf("error is not a *failure.Error: %v", err)
	}
	if f.In == "" {
		t.Errorf("the error does not say which file to edit: %+v", f)
	}
}

// hookScript pulls the generated session-hook RUN out of the Dockerfile and
// turns it back into the shell line docker would execute, so the tests can run
// the real emitted text rather than a reconstruction of it.
func hookScript(t *testing.T, dockerfile string) string {
	t.Helper()
	start := strings.Index(dockerfile, "RUN printf")
	if start < 0 {
		t.Fatalf("no session hook in:\n%s", dockerfile)
	}
	body := dockerfile[start+len("RUN "):]
	if end := strings.Index(body, "\nUSER "); end >= 0 {
		body = body[:end]
	}
	return strings.ReplaceAll(body, "\\\n", " ")
}

// hookContents runs the generated RUN and returns what it appended, which is
// what the sandbox actually sources — the Dockerfile text is quoted for printf,
// so asserting against it would be asserting against the wrong layer.
func hookContents(t *testing.T, dockerfile string) string {
	t.Helper()
	hook := filepath.Join(t.TempDir(), "sandbox-persistent.sh")
	script := strings.Replace(hookScript(t, dockerfile), ">> /etc/sandbox-persistent.sh", ">> "+hook, 1)
	if out, err := exec.Command("bash", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("the generated hook is not valid shell: %v\n%s\nscript:\n%s", err, out, script)
	}
	body, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestABridgeIsCopiedInAndBroughtUpEverySession(t *testing.T) {
	out, err := Render(cfgFor(t, "name: api\nbridges:\n  - name: db\n    port: 5432\n    host_port: 15432\n    env: DB_HOST\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(out, "COPY bridge.sh "+BridgeScriptPath) {
		t.Errorf("the bridge helper is not copied into the image:\n%s", out)
	}
	if !strings.Contains(hookContents(t, out), BridgeScriptPath+" db 5432 15432 DB_HOST") {
		t.Errorf("the bridge is not brought up with its config:\n%s", out)
	}
}

func TestAnOnStartCommandRunsInTheProjectDirectory(t *testing.T) {
	cfg := cfgFor(t, "name: api\nscripts:\n  on_start:\n    - php artisan serve\n")
	out, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(hookContents(t, out), "cd '"+cfg.ProjectDir+"' && php artisan serve") {
		t.Errorf("on_start does not run in the project:\n%s", out)
	}
}

func TestAWorkspaceIsWhereOnStartRuns(t *testing.T) {
	out, err := Render(cfgFor(t, "name: api\nworkspace: /workspace/api\nscripts:\n  on_start:\n    - pwd\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(hookContents(t, out), "cd '/workspace/api' && pwd") {
		t.Errorf("on_start ignores the workspace it was linked into:\n%s", out)
	}
}

func TestNoSessionHookWithoutSomethingToRun(t *testing.T) {
	out, err := Render(cfgFor(t, "name: api\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(out, "sandbox-persistent.sh") {
		t.Errorf("an empty session hook was written anyway:\n%s", out)
	}
	if strings.Contains(out, "COPY bridge.sh") {
		t.Errorf("the bridge helper was copied in with no bridges configured:\n%s", out)
	}
}

// The emitted RUN is a printf whose arguments are single-quoted shell. A
// command carrying a quote of its own has to survive that intact.
func TestTheGeneratedHookIsWhatARealShellWrites(t *testing.T) {
	cfg := cfgFor(t, "name: api\nscripts:\n  on_start:\n    - echo 'it worked'\n")
	out, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	written := hookContents(t, out)
	if !strings.Contains(written, `echo 'it worked'`) {
		t.Errorf("the quoted command did not survive:\n%s", written)
	}
	if !strings.Contains(written, "export LID_ON_START=1") {
		t.Errorf("the guard is missing:\n%s", written)
	}
}

// The hook is sourced by every non-interactive bash. Without the guard, an
// on_start command that itself runs bash would re-source it and recurse until
// the sandbox dies.
func TestTheHookRefusesToRunItselfAgainInAChildShell(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	cfg := cfgFor(t, "name: api\nscripts:\n  on_start:\n    - bash -c 'true'\n")
	out, err := Render(cfg)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	hook := filepath.Join(dir, "hook.sh")
	build := strings.Replace(hookScript(t, out), ">> /etc/sandbox-persistent.sh", ">> "+hook, 1)
	if output, err := exec.Command("bash", "-c", build).CombinedOutput(); err != nil {
		t.Fatalf("cannot write the hook: %v\n%s", err, output)
	}
	// Count every time the block runs, then source it the way BASH_ENV does.
	body, err := os.ReadFile(hook)
	if err != nil {
		t.Fatal(err)
	}
	counted := strings.Replace(string(body), "export LID_ON_START=1", "export LID_ON_START=1; echo x >> "+counter, 1)
	if err := os.WriteFile(hook, []byte(counted), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", ". "+hook)
	cmd.Env = append(os.Environ(), "BASH_ENV="+hook)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sourcing the hook failed: %v\n%s", err, output)
	}

	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("the block never ran: %v", err)
	}
	if got := strings.Count(string(runs), "x"); got != 1 {
		t.Errorf("the block ran %d times, want once: the guard did not hold", got)
	}
}

func TestABridgeBringsSocatWithIt(t *testing.T) {
	out, err := Render(cfgFor(t, "name: api\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if !strings.Contains(out, "socat") {
		t.Errorf("a bridge was configured but socat is not installed:\n%s", out)
	}
}

func TestSocatIsNotInstalledTwiceWhenItWasAskedFor(t *testing.T) {
	out, err := Render(cfgFor(t, "name: api\npackages:\n  - socat\nbridges:\n  - name: db\n    port: 5432\n    host_port: 5432\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Count(out, "socat") != 1 {
		t.Errorf("socat appears %d times, want once:\n%s", strings.Count(out, "socat"), out)
	}
}

// bridgeHarness runs the real bridge script against stubbed system tools, with a
// fixed process list standing in for what is already running.
func bridgeHarness(t *testing.T, procs string, args ...string) (started string, log string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("pgrep", "#!/usr/bin/env bash\nprintf '%s\\n' \"$FAKE_PROCS\" | grep -qE \"$2\"\n")
	write("socat", "#!/usr/bin/env bash\necho \"$@\" >> \"$SOCAT_LOG\"\n")
	write("setsid", "#!/usr/bin/env bash\n[ \"$1\" = -f ] && shift\nexec \"$@\"\n")
	write("sudo", "#!/usr/bin/env bash\nexec \"$@\"\n")

	script := filepath.Join(dir, "bridge.sh")
	if err := os.WriteFile(script, []byte(bridgeScript), 0o755); err != nil {
		t.Fatal(err)
	}

	socatLog := filepath.Join(dir, "socat.log")
	bridgeLog := filepath.Join(dir, "bridge.log")
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_PROCS="+procs,
		"SOCAT_LOG="+socatLog,
		"LID_HOSTS_FILE="+filepath.Join(dir, "hosts"),
		"CLAUDE_ENV_FILE="+filepath.Join(dir, "env.sh"),
		"LID_BRIDGE_LOG="+bridgeLog,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bridge.sh: %v\n%s", err, out)
	}
	body, _ := os.ReadFile(socatLog)
	logged, _ := os.ReadFile(bridgeLog)
	return string(body), string(logged)
}

// A listener on 33060 must not pass for one on 3306. Without the comma the
// liveness check matches on the prefix, and the shorter bridge is reported up
// while nothing is listening for it.
func TestABridgeIsNotConfusedByALongerPortThatStartsWithItsOwn(t *testing.T) {
	started, log := bridgeHarness(t,
		"socat TCP-LISTEN:33060,bind=127.0.0.1,reuseaddr,fork TCP:host.docker.internal:13306",
		"db", "3306", "13306")

	if !strings.Contains(started, "TCP-LISTEN:3306,") {
		t.Errorf("the bridge was not started; the 33060 listener was taken for it\nlog:\n%s", log)
	}
}

func TestABridgeThatIsAlreadyUpIsLeftAlone(t *testing.T) {
	started, log := bridgeHarness(t,
		"socat TCP-LISTEN:3306,bind=127.0.0.1,reuseaddr,fork TCP:host.docker.internal:13306",
		"db", "3306", "13306")

	if started != "" {
		t.Errorf("a second socat was started for a bridge already up: %s\nlog:\n%s", started, log)
	}
}
