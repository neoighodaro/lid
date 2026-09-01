package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

var claudeThemes = []string{
	"dark",
	"dark-ansi",
	"dark-daltonized",
	"light",
	"light-ansi",
	"light-daltonized",
}

const (
	claudeThemeName = "lid"
	claudeThemeFile = ".claude/themes/" + claudeThemeName + ".json"
)

func claudeThemeID(cfg *config.Config) string {
	switch {
	case cfg.Claude.Theme == "":
		return ""
	case contains(claudeThemes, cfg.Claude.Theme):
		return cfg.Claude.Theme
	default:
		return "custom:" + claudeThemeName
	}
}

func claudeThemeBody(cfg *config.Config) (string, error) {
	if claudeThemeID(cfg) != "custom:"+claudeThemeName {
		return "", nil
	}
	path := cfg.Claude.Theme
	if !filepath.IsAbs(path) {
		path = filepath.Join(cfg.ProjectDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", failure.New("cannot read the theme %q", cfg.Claude.Theme).
			At(cfg.Ref()).
			WithFix("claude.theme is a path to a theme file, or one of %s", strings.Join(claudeThemes, ", "))
	}
	if !json.Valid(body) {
		return "", failure.New("the theme %q is not valid JSON", cfg.Claude.Theme).
			At(cfg.Ref()).
			WithFix("Claude Code reads a theme as JSON, so it would ignore this one")
	}
	return string(body), nil
}

func claudeSettings(cfg *config.Config) map[string]any {
	want := map[string]any{}
	if theme := claudeThemeID(cfg); theme != "" {
		want["theme"] = theme
		want["themeId"] = theme
	}
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
