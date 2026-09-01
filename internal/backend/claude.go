package backend

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
)

const dashboardPlugin = "claude-dashboard"

func dashboardMarketplace(cfg *config.Config) string {
	if !config.IsClaudeAgent(cfg.Agent) {
		return ""
	}
	for _, entry := range cfg.Claude.Plugins.Install {
		if name, marketplace, ok := strings.Cut(entry, "@"); ok && name == dashboardPlugin {
			return marketplace
		}
	}
	return ""
}

func claudeSettings(cfg *config.Config) map[string]any {
	want := map[string]any{}
	if len(cfg.Claude.Plugins.Install) > 0 {
		enabled := make(map[string]any, len(cfg.Claude.Plugins.Install))
		for _, plugin := range cfg.Claude.Plugins.Install {
			enabled[plugin] = true
		}
		want["enabledPlugins"] = enabled
	}
	return mergeSettings(want, cfg.Claude.Settings)
}

func mergeSettings(dst, src map[string]any) map[string]any {
	merged := make(map[string]any, len(dst)+len(src))
	for key, value := range dst {
		merged[key] = value
	}
	for key, value := range src {
		if from, ok := value.(map[string]any); ok {
			if into, ok := merged[key].(map[string]any); ok {
				merged[key] = mergeSettings(into, from)
				continue
			}
		}
		merged[key] = value
	}
	return merged
}

func applySettings(current string, want map[string]any) (string, bool, error) {
	existing := map[string]any{}
	if trimmed := strings.TrimSpace(current); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &existing); err != nil {
			return "", false, failure.New("the agent's settings in the sandbox are not valid JSON, so lid will not overwrite them").
				WithFix("open a shell in the sandbox and fix ~/.claude/settings.json, or rebuild with 'lid start --rebuild'")
		}
	}

	merged := mergeSettings(existing, want)
	before, err := json.Marshal(existing)
	if err != nil {
		return "", false, settingsBug(err)
	}
	after, err := json.Marshal(merged)
	if err != nil {
		return "", false, settingsBug(err)
	}
	if bytes.Equal(before, after) {
		return "", false, nil
	}

	body, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return "", false, settingsBug(err)
	}
	return string(body), true, nil
}

func settingsBug(err error) error {
	return failure.New("cannot render the agent's settings: %v", err).
		WithFix("this is a bug in lid, not a problem with your config")
}
