package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellRCKeepsTheUsersOwnBashrc(t *testing.T) {
	rc := shellRC(sbxCfg(t, "name: api\n"), "/work")

	if !strings.Contains(rc, `. "$HOME/.bashrc"`) {
		t.Errorf("rc drops the shell the user already configured:\n%s", rc)
	}
}

func TestShellRCDefinesTheConfiguredAliases(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nshell:\n  aliases:\n    g: git\n    ll: ls -la\n")

	rc := shellRC(cfg, "/work")

	for _, want := range []string{"alias g='git'", "alias ll='ls -la'"} {
		if !strings.Contains(rc, want) {
			t.Errorf("rc missing %q:\n%s", want, rc)
		}
	}
}

func TestShellRCOrdersAliasesSoTheFileIsStable(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nshell:\n  aliases:\n    zz: echo z\n    aa: echo a\n")

	rc := shellRC(cfg, "/work")

	if strings.Index(rc, "alias aa=") > strings.Index(rc, "alias zz=") {
		t.Errorf("aliases are not sorted, so the file churns between runs:\n%s", rc)
	}
}

func TestShellRCQuotesAnAliasThatCarriesAQuote(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nshell:\n  aliases:\n    say: echo 'hi'\n")

	rc := shellRC(cfg, "/work")

	if strings.Contains(rc, "alias say='echo 'hi''") {
		t.Errorf("the quote in the alias body escapes its own quoting:\n%s", rc)
	}
	if !strings.Contains(rc, `alias say='echo '\''hi'\'''`) {
		t.Errorf("rc missing the escaped alias:\n%s", rc)
	}
}

func TestShellRCStartsInTheGivenDirectory(t *testing.T) {
	rc := shellRC(sbxCfg(t, "name: api\n"), "/work")

	if !strings.Contains(rc, "cd '/work'") {
		t.Errorf("rc does not enter the working directory:\n%s", rc)
	}
}

func TestShellDirectoryPrefersTheConfiguredOne(t *testing.T) {
	cfg := sbxCfg(t, "name: api\nshell:\n  directory: /home/agent/project\n")

	if got := shellDir(cfg, "/fallback"); got != "/home/agent/project" {
		t.Errorf("shellDir = %q, want the configured directory", got)
	}
}

func TestShellDirectoryFallsBackWhenUnset(t *testing.T) {
	if got := shellDir(sbxCfg(t, "name: api\n"), "/fallback"); got != "/fallback" {
		t.Errorf("shellDir = %q, want the fallback", got)
	}
}

func TestSbxShellWritesTheRCIntoTheSandbox(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}
	cfg := sbxCfg(t, "name: api\nshell:\n  aliases:\n    g: git\n")

	if _, err := Get("sbx").ShellArgv(f.host(), cfg, "", nil); err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if !f.ran("sbx exec", "api", "alias g='git'") {
		t.Errorf("the aliases never reached the sandbox: %v", f.calls)
	}
}

func TestSbxShellStartsBashUnderTheRC(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}
	cfg := sbxCfg(t, "name: api\n")

	argv, err := Get("sbx").ShellArgv(f.host(), cfg, "", nil)
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	want := "sbx exec -it -w " + cfg.ProjectDir + " api -- bash --rcfile /home/agent/.lid-shellrc -i"
	if got := strings.Join(argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// Without a pseudo-TTY bash comes up with no job control, which is not a shell
// anyone can work in: no ctrl-C, no fg, no line editing.
func TestSbxShellAsksForATerminal(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}

	argv, err := Get("sbx").ShellArgv(f.host(), sbxCfg(t, "name: api\n"), "", nil)
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if !contains(argv, "-it") {
		t.Errorf("argv = %v, want a pseudo-TTY for the interactive shell", argv)
	}
}

// A one-off command keeps stdin, but takes no terminal: 'lid shell -- ls | head'
// has to stay pipeable.
func TestSbxShellKeepsStdinButNoTerminalForACommand(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}

	argv, err := Get("sbx").ShellArgv(f.host(), sbxCfg(t, "name: api\n"), "", []string{"ls"})
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if !contains(argv, "-i") {
		t.Errorf("argv = %v, want stdin held open for the command", argv)
	}
	if contains(argv, "-it") || contains(argv, "-t") {
		t.Errorf("argv = %v, want no terminal for a command that may be piped", argv)
	}
}

func TestSbxShellStartsInTheWorkspaceWhenThereIsOne(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}
	cfg := sbxCfg(t, "name: api\nworkspace: /home/agent/project\n")

	argv, err := Get("sbx").ShellArgv(f.host(), cfg, "", nil)
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if !contains(argv, "-w") || !contains(argv, "/home/agent/project") {
		t.Errorf("argv = %v, want the shell to start in the workspace", argv)
	}
}

// The rc file leaves the directory to sbx's own -w, so a shell.directory that
// does not exist fails visibly instead of silently landing somewhere else.
func TestSbxShellLeavesTheDirectoryToTheSandbox(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}
	cfg := sbxCfg(t, "name: api\nshell:\n  directory: /home/agent/project\n")

	if _, err := Get("sbx").ShellArgv(f.host(), cfg, "", nil); err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if f.ran("sbx exec", "cd '/home/agent/project'") {
		t.Errorf("the rc file still cds, duplicating what -w already did: %v", f.calls)
	}
}

func TestSbxShellRunsACommandWithoutTouchingTheRC(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}
	cfg := sbxCfg(t, "name: api\nworkspace: /home/agent/project\n")

	argv, err := Get("sbx").ShellArgv(f.host(), cfg, "", []string{"npm", "test"})
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if got, want := strings.Join(argv, " "), "sbx exec -i -w /home/agent/project api -- bash -lc npm test"; got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
	if f.ran("lid-shellrc") {
		t.Errorf("a one-off command wrote the interactive rc file: %v", f.calls)
	}
}

func TestSbxShellQuotesACommandArgumentWithSpaces(t *testing.T) {
	f := &sbxFake{sandboxes: []string{"api"}, hash: "written"}

	argv, err := Get("sbx").ShellArgv(f.host(), sbxCfg(t, "name: api\n"), "", []string{"git", "commit", "-m", "a message"})
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	if !strings.Contains(argv[len(argv)-1], `git commit -m 'a message'`) {
		t.Errorf("argv = %q, want the argument to survive as one word", argv[len(argv)-1])
	}
}

func TestNoBackendCallsTheShellBlockIgnored(t *testing.T) {
	for _, name := range Names() {
		cfg := cfgFor(t, "name: api\nbackend: "+name+"\nshell:\n  directory: /work\n  aliases:\n    ll: ls -la\n")

		warnings, err := Get(name).Validate(cfg)
		if err != nil {
			t.Fatalf("%s Validate: %v", name, err)
		}
		for _, warning := range warnings {
			if strings.Contains(warning, "shell") || strings.Contains(warning, "aliases") {
				t.Errorf("%s calls the shell block ignored, but 'lid shell' honours it: %q", name, warning)
			}
		}
	}
}

func TestNonoShellHandsOverTheUsersOwnShellWhenNothingIsConfigured(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nbackend: nono\n")

	argv, err := Get("nono").ShellArgv(Host{}, cfg, "/tmp/nono.jsonc", nil)
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	got := strings.Join(argv, " ")
	for _, want := range []string{"nono shell", "--profile /tmp/nono.jsonc", "--name lid", "--workdir " + cfg.ProjectDir, "--allow-cwd"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv %q is missing %q", got, want)
		}
	}
	if strings.Contains(got, " -- ") {
		t.Errorf("argv = %q, want no command after the separator", got)
	}
}

func TestNonoShellRunsBashUnderTheRCWhenTheShellBlockIsSet(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nbackend: nono\nshell:\n  aliases:\n    g: git\n")

	argv, err := Get("nono").ShellArgv(Host{}, cfg, "/tmp/nono.jsonc", nil)
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	rc := filepath.Join(cfg.KitDir(), "shellrc")
	got := strings.Join(argv, " ")
	if !strings.Contains(got, "nono run") {
		t.Errorf("argv = %q, want a run that can carry the rc file", got)
	}
	if !strings.HasSuffix(got, "-- bash --rcfile "+rc+" -i") {
		t.Errorf("argv = %q, want it to start bash under %s", got, rc)
	}
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatalf("the rc file the shell starts under was never written: %v", err)
	}
	if !strings.Contains(string(body), "alias g='git'") {
		t.Errorf("rc missing the configured alias:\n%s", body)
	}
}

func TestNonoShellRunsACommandInTheProject(t *testing.T) {
	cfg := cfgFor(t, "name: lid\nbackend: nono\n")

	argv, err := Get("nono").ShellArgv(Host{}, cfg, "/tmp/nono.jsonc", []string{"go", "test", "./..."})
	if err != nil {
		t.Fatalf("ShellArgv: %v", err)
	}

	got := strings.Join(argv, " ")
	if !strings.Contains(got, "nono run") {
		t.Errorf("argv = %q, want a run rather than an interactive shell", got)
	}
	if !strings.HasSuffix(got, "-- bash -lc cd "+shellQuote(cfg.ProjectDir)+" && go test ./...") {
		t.Errorf("argv = %q, want it to run the command in the project", got)
	}
}
