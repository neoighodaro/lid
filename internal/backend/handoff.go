package backend

import (
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
)

//go:embed handoff-skill.md
var handoffSkill string

const (
	handoffSkillFile = ".claude/skills/handoff/SKILL.md"
	handoffSandboxV  = "LID_SANDBOX"
	handoffURLVar    = "LID_HANDOFF_URL"
)

func handoffEnv(cfg *config.Config) map[string]string {
	if !cfg.Handoff.Enabled {
		return nil
	}
	return map[string]string{
		handoffSandboxV: cfg.Name,
		handoffURLVar:   cfg.HandoffURL(),
	}
}

// handoffResource names the broker the way a network policy does: a broker on
// the host is reached through host.docker.internal but allowed as localhost.
func handoffResource(cfg *config.Config) string {
	host := "localhost"
	if u, err := url.Parse(cfg.HandoffURL()); err == nil && !hostIsLocal(u.Hostname()) {
		host = u.Hostname()
	}
	return fmt.Sprintf("%s:%d", host, cfg.HandoffPort())
}

func hostIsLocal(host string) bool {
	switch host {
	case "", "localhost", "host.docker.internal", "127.0.0.1", "::1":
		return true
	}
	return false
}

func handoffAgentWarning(cfg *config.Config) []string {
	if !cfg.Handoff.Enabled || config.IsClaudeAgent(cfg.Agent) {
		return nil
	}
	return []string{fmt.Sprintf("handoff is on, but the skill lid installs is written for %s: %s gets the broker at %s and nothing telling it how to use it",
		config.AgentClaudeCode, cfg.Agent, cfg.HandoffURL())}
}

func InstallHandoffSkill(h Host, cfg *config.Config, force bool) (string, error) {
	if !config.IsClaudeAgent(cfg.Agent) {
		return "", failure.New("the handoff skill is written for %s, not %s", config.AgentClaudeCode, cfg.Agent).
			At(cfg.Ref()).
			WithFix("the API in it is plain HTTP; teach your agent to reach %s yourself", cfg.HandoffURL())
	}
	if cfg.BuildsImage() {
		if err := sbxWriteHandoffSkill(h, cfg); err != nil {
			return "", err
		}
		return fmt.Sprintf("~/%s in the %s sandbox", handoffSkillFile, cfg.Name), nil
	}
	return installHandoffSkillOnHost(force)
}

func installHandoffSkillOnHost(force bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", failure.New("cannot find your home directory").WithFix("%v", err)
	}
	path := filepath.Join(home, filepath.FromSlash(handoffSkillFile))

	switch existing, err := os.ReadFile(path); {
	case err == nil && strings.TrimRight(string(existing), "\n") == strings.TrimRight(handoffSkill, "\n"):
		return path, nil
	case err == nil && !force:
		return "", failure.New("a different handoff skill is already installed").
			At(path).
			WithFix("read it first, then re-run with --force to replace it")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", failure.New("cannot create the skills directory").At(filepath.Dir(path)).WithFix("%v", err)
	}
	if err := os.WriteFile(path, []byte(handoffSkill), 0o644); err != nil {
		return "", failure.New("cannot write the handoff skill").At(path).WithFix("%v", err)
	}
	return path, nil
}

func handoffSkillNote(cfg *config.Config) []string {
	if !cfg.Handoff.Enabled || cfg.BuildsImage() || !config.IsClaudeAgent(cfg.Agent) {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(handoffSkillFile))); err == nil {
		return nil
	}
	return []string{"the handoff broker is reachable, but nono installs no skill in your home: 'lid handoff --install-skill' does"}
}
