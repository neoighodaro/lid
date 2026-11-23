package backend

import (
	"sort"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
)

type Resume int

const (
	ResumeNone Resume = iota
	ResumeLast
	ResumePick
)

var resumeArgs = map[string]map[Resume][]string{
	config.AgentClaudeCode: {ResumeLast: {"--continue"}, ResumePick: {"--resume"}},
	"codex":                {ResumeLast: {"resume", "--last"}, ResumePick: {"resume"}},
	"opencode":             {ResumeLast: {"--continue"}},
}

// ResumeArgs are the arguments that make an agent pick up an earlier session
// instead of starting a new one.
func ResumeArgs(agent string, mode Resume) ([]string, error) {
	if mode == ResumeNone {
		return nil, nil
	}
	if args, ok := resumeArgs[agent][mode]; ok {
		return args, nil
	}

	var can []string
	for name, modes := range resumeArgs {
		if _, ok := modes[mode]; ok {
			can = append(can, name)
		}
	}
	sort.Strings(can)

	what := "resume a session"
	if mode == ResumePick {
		what = "pick a session to resume"
	}
	return nil, failure.New("lid cannot %s for agent %q", what, agent).
		WithFix("only %s can, or run 'lid start' for a new session", strings.Join(can, ", "))
}
