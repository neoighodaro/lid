package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/panes"
)

var fakeBinDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lid-fake-bin")
	if err == nil {
		fakeBinDir, err = filepath.EvalSymlinks(dir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fakeBinary is a stand-in for a program on PATH, real enough to resolve.
func fakeBinary(name string) (string, error) {
	path := filepath.Join(fakeBinDir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return path, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755)
}

func isolatePanes(t *testing.T) {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
}

func panesProject(t *testing.T, extra string) string {
	t.Helper()
	isolatePanes(t)
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\nherdr:\n  panes: true\n"+extra)
	return dir
}

type endpoint struct {
	url   string
	port  int
	token string
}

var (
	promptURL   = regexp.MustCompile(`POST (http://[^ ]+:(\d+))/panes `)
	promptToken = regexp.MustCompile(`Bearer ([0-9a-f]+)'`)
)

func panesPromptOf(t *testing.T, argv []string) (string, bool) {
	t.Helper()
	for i, arg := range argv {
		if arg == "--append-system-prompt" {
			if i != len(argv)-2 {
				t.Errorf("--append-system-prompt is not the last flag: %q", argv)
			}
			return argv[i+1], true
		}
	}
	return "", false
}

func endpointOf(t *testing.T, argv []string) endpoint {
	t.Helper()
	prompt, ok := panesPromptOf(t, argv)
	if !ok {
		t.Fatalf("the agent was not told about panes: %q", argv)
	}
	u := promptURL.FindStringSubmatch(prompt)
	tok := promptToken.FindStringSubmatch(prompt)
	if u == nil || tok == nil {
		t.Fatalf("prompt has no URL or token:\n%s", prompt)
	}
	port, _ := strconv.Atoi(u[2])
	return endpoint{url: u[1], port: port, token: tok[1]}
}

func postPane(t *testing.T, e endpoint, token, command string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"command": command})
	req, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/panes", e.port), strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /panes: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func refuses(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

func probeIndex(h *sbxStartHost, fragment string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	last := -1
	for i, probe := range h.probes {
		if strings.Contains(strings.Join(probe, " "), fragment) {
			last = i
		}
	}
	return last
}

func panesDir(t *testing.T) string {
	t.Helper()
	dir, err := panes.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestStartOpensNoPanesListenerByDefault(t *testing.T) {
	isolatePanes(t)
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}

	runSbxStart(t, dir, h, "start")

	if _, ok := panesPromptOf(t, h.argv); ok {
		t.Errorf("the agent was told about panes nobody turned on: %q", h.argv)
	}
	if h.ran("localhost:") {
		t.Errorf("opened the firewall for a listener that should not exist: %v", h.probes)
	}
}

func TestStartOpensNoPanesListenerOutsideHerdr(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", settings: "{}"}

	res := runSbxStart(t, dir, h, "start")

	if _, ok := panesPromptOf(t, h.argv); ok {
		t.Errorf("the agent was told about panes outside Herdr: %q", h.argv)
	}
	if !strings.Contains(res.err, "herdr.panes is on, but lid is not running in Herdr") {
		t.Errorf("stderr does not say why panes are off:\n%s", res.err)
	}
	if h.ran("policy allow network --sandbox api localhost:") {
		t.Errorf("opened the firewall outside Herdr: %v", h.probes)
	}
}

func TestStartPanesWarnsForNonClaudeAgents(t *testing.T) {
	dir := panesProject(t, "agent: codex\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}

	res := runSbxStart(t, dir, h, "start")

	if _, ok := panesPromptOf(t, h.argv); ok {
		t.Errorf("a codex agent got a claude flag: %q", h.argv)
	}
	if !strings.Contains(res.err, "only claude-code can open panes") {
		t.Errorf("stderr does not say why panes are off:\n%s", res.err)
	}
	if h.ran("policy allow network --sandbox api localhost:") {
		t.Errorf("opened the firewall for an agent that cannot use it: %v", h.probes)
	}
}

func TestStartGivesTheAgentThePanesEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sandbox string
		args    []string
		after   string
	}{
		{"first start", "", []string{"start"}, ".lid-config-hash"},
		{"existing sandbox", "api", []string{"start"}, "sbx ls"},
		{"rebuild", "api", []string{"start", "--rebuild"}, ".lid-config-hash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := panesProject(t, "")
			h := &sbxStartHost{sandbox: tc.sandbox, settings: "{}", herdrID: "t7"}
			var e endpoint
			h.onAttach = func(argv []string) error {
				e = endpointOf(t, argv)
				allow := fmt.Sprintf("sbx policy allow network --sandbox api localhost:%d", e.port)
				at := probeIndex(h, allow)
				if at < 0 {
					t.Errorf("never allowed the sandbox to reach the listener: %v", h.probes)
				}
				if provisioned := probeIndex(h, tc.after); at < provisioned {
					t.Errorf("allowed the listener before provisioning finished: %v", h.probes)
				}
				if code := postPane(t, e, "wrong", "ls"); code != http.StatusUnauthorized {
					t.Errorf("a wrong token got %d", code)
				}
				return nil
			}

			runSbxStart(t, dir, h, tc.args...)

			if e.url != fmt.Sprintf("http://host.docker.internal:%d", e.port) {
				t.Errorf("url = %q, want the host as the sandbox sees it", e.url)
			}
			if len(e.token) != 64 {
				t.Errorf("token = %q", e.token)
			}
			prompt, _ := panesPromptOf(t, h.argv)
			if !strings.Contains(prompt, "-H 'Authorization: Bearer "+e.token+"'") ||
				!strings.HasPrefix(prompt, "You are running in a Herdr terminal.") ||
				!strings.HasSuffix(prompt, "Only open a pane when the user asks for one.") {
				t.Errorf("prompt:\n%s", prompt)
			}
			if i := indexOf(h.argv, "--"); i < 0 || h.argv[i+1] != "--append-system-prompt" {
				t.Errorf("the prompt flag does not go to the agent: %q", h.argv)
			}
		})
	}

	t.Run("nono", func(t *testing.T) {
		dir := panesProject(t, "backend: nono\n")
		h := &sbxStartHost{settings: "{}", herdrID: "t7"}
		var e endpoint
		var profile, body string
		h.onAttach = func(argv []string) error {
			e = endpointOf(t, argv)
			if code := postPane(t, e, "wrong", "ls"); code != http.StatusUnauthorized {
				t.Errorf("a wrong token got %d", code)
			}
			profile = profileArg(t, argv)
			body = read(t, profile)
			return nil
		}

		runSbxStart(t, dir, h, "start")

		if e.url != fmt.Sprintf("http://localhost:%d", e.port) {
			t.Errorf("url = %q, want localhost", e.url)
		}
		if h.ran("sbx policy") {
			t.Errorf("ran sbx for a nono session: %v", h.probes)
		}
		if filepath.Dir(profile) != panesDir(t) {
			t.Errorf("the agent runs under %s, not the session's private profile", profile)
		}
		if !strings.Contains(body, `"localhost"`) {
			t.Errorf("the session profile does not let the agent reach the listener:\n%s", body)
		}
		if strings.Contains(read(t, kitProfile(dir)), `"localhost"`) {
			t.Error("the project's profile carries the panes grant")
		}
	})
}

func indexOf(argv []string, want string) int {
	for i, arg := range argv {
		if arg == want {
			return i
		}
	}
	return -1
}

func TestStartPanesWithResume(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}

	runSbxStart(t, dir, h, "start", "--resume")

	i := indexOf(h.argv, "--")
	if i < 0 || len(h.argv) != i+4 || h.argv[i+1] != "--continue" || h.argv[i+2] != "--append-system-prompt" {
		t.Errorf("argv = %q, want the resume flag then the prompt", h.argv)
	}
}

func TestStartClosesPanesListenerWhenTheAgentExits(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit error
	}{
		{"normal exit", nil},
		{"attach error", errors.New("sbx run failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := panesProject(t, "")
			h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
			var e endpoint
			h.onAttach = func(argv []string) error {
				e = endpointOf(t, argv)
				if refuses(e.port) {
					t.Error("the listener was not up while the agent ran")
				}
				return tc.exit
			}

			_, err := runSbxStartErr(t, dir, h, "start")
			if !errors.Is(err, tc.exit) {
				t.Fatalf("start: %v", err)
			}

			if !refuses(e.port) {
				t.Errorf("port %d still answers after the session ended", e.port)
			}
			if !h.ran(fmt.Sprintf("sbx policy rm network --sandbox api --resource localhost:%d", e.port)) {
				t.Errorf("the firewall rule outlived the session: %v", h.probes)
			}
			if entries, _ := os.ReadDir(panesDir(t)); len(entries) != 0 {
				t.Errorf("left files behind: %v", entries)
			}
		})
	}
}

func TestStartRemovesThePanesRuleOnSignal(t *testing.T) {
	dir := panesProject(t, "")
	bin := t.TempDir()
	args := filepath.Join(t.TempDir(), "args")
	write(t, bin, "sbx", "#!/bin/sh\ntrap 'exit 0' HUP\nprintf '%s\\n' \"$@\" > \""+args+".tmp\"\nmv \""+args+".tmp\" \""+args+"\"\ni=0; while [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done\n")
	if err := os.Chmod(filepath.Join(bin, "sbx"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7", noAttach: true}
	h.onExec = func(argv []string) {
		if strings.Contains(strings.Join(argv, " "), "policy rm") {
			syscall.Kill(os.Getpid(), syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
		}
	}

	ports := make(chan int, 1)
	go func() {
		port := regexp.MustCompile(`:(\d+)/panes`)
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			data, err := os.ReadFile(args)
			if err != nil {
				continue
			}
			m := port.FindSubmatch(data)
			if m == nil {
				break
			}
			n, _ := strconv.Atoi(string(m[1]))
			ports <- n
			syscall.Kill(os.Getpid(), syscall.SIGHUP)
			return
		}
		ports <- 0
	}()

	res, err := runSbxStartErr(t, dir, h, "start")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, res.err)
	}
	port := <-ports
	if port == 0 {
		t.Fatal("the agent never saw the panes endpoint")
	}
	if !refuses(port) {
		t.Errorf("port %d still answers after SIGHUP", port)
	}
	if !h.ran(fmt.Sprintf("sbx policy rm network --sandbox api --resource localhost:%d", port)) {
		t.Errorf("the firewall rule outlived SIGHUP: %v", h.probes)
	}
}

func TestStartRemovesALeftoverPanesRule(t *testing.T) {
	dir := panesProject(t, "")
	pd := panesDir(t)
	write(t, pd, "api.4242.port", strconv.Itoa(deadPID(t)))
	write(t, pd, "api.4343.port", strconv.Itoa(os.Getpid()))
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
	h.onAttach = func(argv []string) error {
		e := endpointOf(t, argv)
		rm := probeIndex(h, "sbx policy rm network --sandbox api --resource localhost:4242")
		if rm < 0 || rm > probeIndex(h, fmt.Sprintf("policy allow network --sandbox api localhost:%d", e.port)) {
			t.Errorf("the leftover rule was not removed before the new one went in: %v", h.probes)
		}
		return nil
	}

	runSbxStart(t, dir, h, "start")

	if _, err := os.Stat(filepath.Join(pd, "api.4242.port")); err == nil {
		t.Error("the leftover record was kept")
	}
	if h.ran("localhost:4343") {
		t.Errorf("removed the rule of a session that is still running: %v", h.probes)
	}
	if _, err := os.Stat(filepath.Join(pd, "api.4343.port")); err != nil {
		t.Error("dropped the record of a session that is still running")
	}
}

func TestStopRemovesALeftoverPanesRule(t *testing.T) {
	dir := panesProject(t, "")
	pd := panesDir(t)
	write(t, pd, "api.4242.port", strconv.Itoa(deadPID(t)))
	h := &sbxStartHost{sandbox: "api", list: sbxRunningAPI}

	runSbxStart(t, dir, h, "stop")

	if !h.ran("sbx policy rm network --sandbox api --resource localhost:4242") {
		t.Errorf("stop left the rule in place: %v", h.probes)
	}
	if _, err := os.Stat(filepath.Join(pd, "api.4242.port")); err == nil {
		t.Error("the leftover record was kept")
	}
}

func TestStartPanesListenerIsPerSession(t *testing.T) {
	dir := panesProject(t, "")
	first := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
	second := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t9"}
	var a, b endpoint
	second.onAttach = func(argv []string) error {
		b = endpointOf(t, argv)
		return nil
	}
	first.onAttach = func(argv []string) error {
		a = endpointOf(t, argv)
		runSbxStart(t, dir, second, "start")
		if code := postPane(t, a, b.token, "ls"); code != http.StatusUnauthorized {
			t.Errorf("one session's token opened a pane in another: %d", code)
		}
		if refuses(a.port) {
			t.Error("the second session closed the first one's listener")
		}
		return nil
	}

	runSbxStart(t, dir, first, "start")

	if a.port == b.port || a.token == b.token {
		t.Errorf("sessions share an endpoint: %+v %+v", a, b)
	}
	if second.ran(fmt.Sprintf("localhost:%d", a.port)) {
		t.Errorf("the second session touched the first one's rule: %v", second.probes)
	}
}

func paneSplitArgv(t *testing.T, split []string) []string {
	t.Helper()
	var req struct {
		Direction string   `json:"direction"`
		Pane      string   `json:"pane"`
		Argv      []string `json:"argv"`
	}
	if err := json.Unmarshal([]byte(split[3]), &req); err != nil {
		t.Fatalf("pane.split request: %v", err)
	}
	if req.Pane != "p7" || req.Direction != "right" {
		t.Errorf("split %+v, want right of lid's own pane", req)
	}
	return req.Argv
}

func TestOpenPaneKeepsTheCommandOutOfHerdrArgv(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
	command := "echo hi; touch /tmp/pwned $(id)"
	var stored []string
	h.onAttach = func(argv []string) error {
		e := endpointOf(t, argv)
		if code := postPane(t, e, e.token, command); code != http.StatusCreated {
			t.Fatalf("POST got %d", code)
		}
		if len(h.splits) != 1 {
			t.Fatalf("splits = %v", h.splits)
		}
		got := paneSplitArgv(t, h.splits[0])
		exe, _ := os.Executable()
		if len(got) != 3 || got[0] != exe || got[1] != "pane-run" || filepath.Dir(got[2]) != panesDir(t) {
			t.Fatalf("herdr argv = %q", got)
		}
		var err error
		if stored, err = panes.TakeArgv(got[2]); err != nil {
			t.Fatalf("TakeArgv: %v", err)
		}
		return nil
	}

	runSbxStart(t, dir, h, "start")

	if indexOf(stored, backend.PaneCmdVar+"="+command) < 0 {
		t.Errorf("stored argv = %q, want the command as one element", stored)
	}
	if sbx, _ := fakeBinary("sbx"); stored[0] != sbx {
		t.Errorf("stored argv runs %q, want sbx by its full path", stored[0])
	}
}

func TestOpenPaneFailureLeavesNoPaneFile(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7", splitErr: errors.New("herdr is gone")}
	h.onAttach = func(argv []string) error {
		e := endpointOf(t, argv)
		if code := postPane(t, e, e.token, "ls"); code != http.StatusBadGateway {
			t.Errorf("POST got %d, want 502", code)
		}
		entries, _ := os.ReadDir(panesDir(t))
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".port") {
				t.Errorf("left %s behind", entry.Name())
			}
		}
		return nil
	}

	runSbxStart(t, dir, h, "start")
}

func kitProfile(dir string) string {
	return filepath.Join(dir, ".config", "lid", ".kit", "nono.jsonc")
}

func profileArg(t *testing.T, argv []string) string {
	t.Helper()
	i := indexOf(argv, "--profile")
	if i < 0 {
		t.Fatalf("argv has no profile: %q", argv)
	}
	return argv[i+1]
}

func TestPaneArgvUsesAPrivateProfileCopy(t *testing.T) {
	dir := panesProject(t, "backend: nono\n")
	kit := kitProfile(dir)
	h := &sbxStartHost{settings: "{}", herdrID: "t7"}
	var private, agent, rendered string
	h.onAttach = func(argv []string) error {
		agent = profileArg(t, argv)
		rendered = read(t, agent)
		write(t, filepath.Dir(kit), "nono.jsonc", `{"extends": "nothing", "network": {"block": false}}`)
		e := endpointOf(t, argv)
		if code := postPane(t, e, e.token, "ls"); code != http.StatusCreated {
			t.Fatalf("POST got %d", code)
		}
		stored, err := panes.TakeArgv(paneSplitArgv(t, h.splits[0])[2])
		if err != nil {
			t.Fatal(err)
		}
		private = profileArg(t, stored)
		if filepath.Dir(private) != panesDir(t) {
			t.Errorf("profile %s is not in the private dir", private)
		}
		info, err := os.Stat(private)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("private profile: %v %v", info, err)
		}
		if got := read(t, private); got != rendered || !strings.Contains(got, `"localhost"`) {
			t.Errorf("the pane's profile is not the session's:\n%s", got)
		}
		return nil
	}

	runSbxStart(t, dir, h, "start")

	if private == "" {
		t.Fatal("no pane opened")
	}
	for _, path := range []string{private, agent} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s outlived the session", path)
		}
	}
}

func TestNonoWithoutPanesGetsNoLoopbackGrant(t *testing.T) {
	for name, tc := range map[string]struct {
		extra   string
		herdrID string
	}{
		"outside Herdr":    {"backend: nono\n", ""},
		"non-claude agent": {"backend: nono\nagent: codex\n", "t7"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := panesProject(t, tc.extra)
			h := &sbxStartHost{settings: "{}", herdrID: tc.herdrID}

			runSbxStart(t, dir, h, "start")

			if got := profileArg(t, h.argv); got != kitProfile(dir) {
				t.Errorf("the agent runs under %s, want the project's profile", got)
			}
			if strings.Contains(read(t, kitProfile(dir)), `"localhost"`) {
				t.Error("the profile grants loopback with no listener to reach")
			}
		})
	}
}

func TestRestartKeepsPanes(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", list: sbxRunningAPI, settings: "{}", herdrID: "t7"}
	var e endpoint
	h.onAttach = func(argv []string) error {
		e = endpointOf(t, argv)
		if probeIndex(h, fmt.Sprintf("sbx policy allow network --sandbox api localhost:%d", e.port)) < 0 {
			t.Errorf("restart never allowed the listener: %v", h.probes)
		}
		return nil
	}

	runSbxStart(t, dir, h, "restart", "--force")

	if e.port == 0 {
		t.Fatal("restart did not tell the agent about panes")
	}
	if !h.ran(fmt.Sprintf("sbx policy rm network --sandbox api --resource localhost:%d", e.port)) {
		t.Errorf("the rule outlived the restarted session: %v", h.probes)
	}
	if !refuses(e.port) {
		t.Error("the listener outlived the restarted session")
	}
}

func TestLeftoverPrivateProfilesAreSwept(t *testing.T) {
	for _, command := range []string{"start", "stop"} {
		t.Run(command, func(t *testing.T) {
			dir := panesProject(t, "backend: nono\n")
			pd := panesDir(t)
			dead := fmt.Sprintf("profile-%d-aa.jsonc", deadPID(t))
			live := fmt.Sprintf("profile-%d-bb.jsonc", os.Getpid())
			write(t, pd, dead, "{}")
			write(t, pd, live, "{}")
			h := &sbxStartHost{settings: "{}", herdrID: "t7", list: "[]"}

			runSbxStart(t, dir, h, command)

			if _, err := os.Stat(filepath.Join(pd, dead)); err == nil {
				t.Error("a dead session's profile was kept")
			}
			if _, err := os.Stat(filepath.Join(pd, live)); err != nil {
				t.Error("a live session's profile was removed")
			}
		})
	}
}

func TestPrivateProfileDeniesTheAgentLidsPaneDir(t *testing.T) {
	dir := panesProject(t, "backend: nono\n")
	h := &sbxStartHost{settings: "{}", herdrID: "t7"}
	var body string
	h.onAttach = func(argv []string) error {
		body = read(t, profileArg(t, argv))
		return nil
	}

	runSbxStart(t, dir, h, "start")

	for name, profile := range map[string]string{"session": body, "project": read(t, kitProfile(dir))} {
		for _, want := range []string{`"$HOME/lid"`, `"$HOME/lid/panes"`} {
			if !strings.Contains(profile, want) {
				t.Errorf("the %s profile lets the agent write %s:\n%s", name, want, profile)
			}
		}
	}
}

func TestOpenPaneRefusesARewrittenPrivateProfile(t *testing.T) {
	dir := panesProject(t, "backend: nono\n")
	h := &sbxStartHost{settings: "{}", herdrID: "t7"}
	h.onAttach = func(argv []string) error {
		profile := profileArg(t, argv)
		if err := os.WriteFile(profile, []byte(`{"extends": "nothing"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		e := endpointOf(t, argv)
		if code := postPane(t, e, e.token, "ls"); code != http.StatusBadGateway {
			t.Errorf("POST got %d, want 502", code)
		}
		if len(h.splits) != 0 {
			t.Errorf("opened a pane under a rewritten profile: %v", h.splits)
		}
		return nil
	}

	runSbxStart(t, dir, h, "start")
}

func TestPanesRefuseBinariesTheAgentCanWrite(t *testing.T) {
	for _, name := range []string{"sbx in the project", "herdr-app linked into the project", "sbx in an extra workspace"} {
		t.Run(name, func(t *testing.T) {
			extra := t.TempDir()
			dir := panesProject(t, "extra_workspaces:\n  - "+extra+":ro\n")
			h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
			switch name {
			case "sbx in the project":
				write(t, dir, "bin/sbx", "#!/bin/sh\n")
				h.bins = map[string]string{"sbx": filepath.Join(dir, "bin", "sbx")}
			case "herdr-app linked into the project":
				write(t, dir, "bin/herdr-app", "#!/bin/sh\n")
				link := filepath.Join(t.TempDir(), "herdr-app")
				if err := os.Symlink(filepath.Join(dir, "bin", "herdr-app"), link); err != nil {
					t.Fatal(err)
				}
				h.bins = map[string]string{"herdr-app": link}
			case "sbx in an extra workspace":
				write(t, extra, "sbx", "#!/bin/sh\n")
				h.bins = map[string]string{"sbx": filepath.Join(extra, "sbx")}
			}

			res := runSbxStart(t, dir, h, "start")

			if _, ok := panesPromptOf(t, h.argv); ok {
				t.Errorf("panes are on with a binary the agent can replace: %q", h.argv)
			}
			if !strings.Contains(res.err, "which the agent can write") {
				t.Errorf("stderr does not say why panes are off:\n%s", res.err)
			}
			if h.ran("localhost:") {
				t.Errorf("opened the firewall for panes that are off: %v", h.probes)
			}
		})
	}
}

func TestOpenPaneRunsTheBinariesFoundAtStart(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
	sbx, _ := fakeBinary("sbx")
	herdrApp, _ := fakeBinary("herdr-app")
	var stored []string
	h.onAttach = func(argv []string) error {
		write(t, dir, "bin/sbx", "#!/bin/sh\n")
		write(t, dir, "bin/herdr-app", "#!/bin/sh\n")
		h.mu.Lock()
		h.bins = map[string]string{"sbx": filepath.Join(dir, "bin", "sbx"), "herdr-app": filepath.Join(dir, "bin", "herdr-app")}
		h.mu.Unlock()
		e := endpointOf(t, argv)
		if code := postPane(t, e, e.token, "ls"); code != http.StatusCreated {
			t.Fatalf("POST got %d", code)
		}
		if got := h.splits[0][0]; got != herdrApp {
			t.Errorf("split ran %s, want the herdr-app found at start, %s", got, herdrApp)
		}
		var err error
		if stored, err = panes.TakeArgv(paneSplitArgv(t, h.splits[0])[2]); err != nil {
			t.Fatal(err)
		}
		return nil
	}

	runSbxStart(t, dir, h, "start")

	if stored[0] != sbx {
		t.Errorf("the pane runs %s, want the sbx found at start, %s", stored[0], sbx)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, probe := range h.probes {
		line := strings.Join(probe, " ")
		if strings.Contains(line, ".lid-shellrc") || strings.Contains(line, "policy") || strings.Contains(line, "herdr-app") {
			if strings.HasPrefix(probe[0], dir) {
				t.Errorf("ran %q from the project", probe)
			}
		}
		if strings.Contains(line, ".lid-shellrc") && probe[0] != sbx {
			t.Errorf("wrote the pane's rc file with %q, want %s", probe[0], sbx)
		}
	}
}

func TestPanesFallBackWhenNonoRejectsTheSessionProfile(t *testing.T) {
	dir := panesProject(t, "backend: nono\n")
	h := &sbxStartHost{settings: "{}", herdrID: "t7", badNono: true}

	res := runSbxStart(t, dir, h, "start")

	if _, ok := panesPromptOf(t, h.argv); ok {
		t.Errorf("the agent was told about panes nono would not allow: %q", h.argv)
	}
	if got := profileArg(t, h.argv); got != kitProfile(dir) {
		t.Errorf("the agent runs under %s, want the project's profile", got)
	}
	if !strings.Contains(res.err, "nono rejected the session's profile") {
		t.Errorf("stderr does not say why panes are off:\n%s", res.err)
	}
	if entries, _ := os.ReadDir(panesDir(t)); len(entries) != 0 {
		t.Errorf("left files behind: %v", entries)
	}
}

func TestStartRecordsThePanesRuleBeforeAddingIt(t *testing.T) {
	dir := panesProject(t, "")
	h := &sbxStartHost{sandbox: "api", settings: "{}", herdrID: "t7"}
	allowed := false
	h.onExec = func(argv []string) {
		line := strings.Join(argv, " ")
		if !strings.Contains(line, "policy allow network --sandbox api localhost:") {
			return
		}
		allowed = true
		port := line[strings.LastIndex(line, ":")+1:]
		data, err := os.ReadFile(filepath.Join(panesDir(t), "api."+port+".port"))
		if err != nil || string(data) != strconv.Itoa(os.Getpid()) {
			t.Errorf("the rule went in before its record: %q %v", data, err)
		}
		syscall.Kill(os.Getpid(), syscall.SIGHUP)
		time.Sleep(50 * time.Millisecond)
	}

	runSbxStart(t, dir, h, "start")

	if !allowed {
		t.Fatal("never allowed the listener")
	}
}

func TestStartSweepsLeftoverPaneFilesWithPanesOff(t *testing.T) {
	isolatePanes(t)
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	pd := panesDir(t)
	write(t, pd, "api.4242.port", strconv.Itoa(deadPID(t)))
	profile := fmt.Sprintf("profile-%d-aa.jsonc", deadPID(t))
	write(t, pd, profile, "{}")
	h := &sbxStartHost{sandbox: "api", settings: "{}"}

	runSbxStart(t, dir, h, "start")

	if !h.ran("sbx policy rm network --sandbox api --resource localhost:4242") {
		t.Errorf("start left the rule in place: %v", h.probes)
	}
	for _, name := range []string{"api.4242.port", profile} {
		if _, err := os.Stat(filepath.Join(pd, name)); err == nil {
			t.Errorf("%s was kept", name)
		}
	}
}

func TestStartWithPanesOffCreatesNoPaneDir(t *testing.T) {
	isolatePanes(t)
	dir := t.TempDir()
	write(t, dir, "lid.yaml", "name: api\n")
	h := &sbxStartHost{sandbox: "api", settings: "{}"}

	runSbxStart(t, dir, h, "start")

	path, err := panes.Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("created %s for a session without panes", path)
	}
	if h.ran("policy rm") {
		t.Errorf("swept rules that cannot exist: %v", h.probes)
	}
}
