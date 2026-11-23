package backend

import (
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func TestResumeArgsPerAgent(t *testing.T) {
	for _, c := range []struct {
		agent string
		mode  Resume
		want  string
	}{
		{config.AgentClaudeCode, ResumeLast, "--continue"},
		{config.AgentClaudeCode, ResumePick, "--resume"},
		{"codex", ResumeLast, "resume --last"},
		{"codex", ResumePick, "resume"},
		{"opencode", ResumeLast, "--continue"},
		{"shell", ResumeNone, ""},
	} {
		got, err := ResumeArgs(c.agent, c.mode)
		if err != nil {
			t.Errorf("%s %v: %v", c.agent, c.mode, err)
			continue
		}
		if strings.Join(got, " ") != c.want {
			t.Errorf("%s %v = %q, want %q", c.agent, c.mode, got, c.want)
		}
	}
}

func TestResumeArgsRefusesAnAgentWithNoWayToResume(t *testing.T) {
	for _, c := range []struct {
		agent string
		mode  Resume
	}{
		{"shell", ResumeLast},
		{"gemini", ResumeLast},
		{"opencode", ResumePick},
	} {
		if _, err := ResumeArgs(c.agent, c.mode); err == nil {
			t.Errorf("%s %v: want an error", c.agent, c.mode)
		}
	}
}

func TestSbxStartArgvPassesAgentArgsAfterTheSeparator(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")

	argv, err := Get("sbx").StartArgv(cfg, "", []string{"--continue"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); got != "sbx run --name lid -- --continue" {
		t.Errorf("argv = %q", got)
	}

	argv, _ = Get("sbx").StartArgv(cfg, "", nil)
	if got := strings.Join(argv, " "); got != "sbx run --name lid" {
		t.Errorf("argv = %q, want no separator without agent args", got)
	}
}

func TestNonoStartArgvPassesAgentArgsAfterTheAgent(t *testing.T) {
	cfg := cfgFor(t, "name: lid\n")

	argv, err := Get("nono").StartArgv(cfg, "/tmp/nono.jsonc", []string{"--continue"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv, " "); !strings.HasSuffix(got, " -- claude --continue") {
		t.Errorf("argv = %q", got)
	}
}
