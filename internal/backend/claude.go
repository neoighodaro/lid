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

// claudeLocalSettingsFile is the settings file a project keeps for itself,
// which is the only one lid writes when the agent runs in the user's own home.
const claudeLocalSettingsFile = ".claude/settings.local.json"

const (
	claudeThemeName   = "lid"
	claudeThemeFile   = ".claude/themes/" + claudeThemeName + ".json"
	customClaudeTheme = "custom:" + claudeThemeName
)

func claudeThemeID(cfg *config.Config) string {
	switch {
	case cfg.Claude.Theme == "":
		return ""
	case contains(claudeThemes, cfg.Claude.Theme):
		return cfg.Claude.Theme
	default:
		return customClaudeTheme
	}
}

func claudeThemeBody(cfg *config.Config) (string, error) {
	if claudeThemeID(cfg) != customClaudeTheme {
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
	return claudeSettingsWithTheme(cfg, claudeThemeID(cfg))
}

// claudeHostSettings is the part of the claude: block lid can honour when the
// agent runs under the user's own home rather than in an image lid built. A
// custom theme is a file lid writes into the sandbox's ~/.claude/themes, and
// that home is the user's own, so the theme is left as they have it.
func claudeHostSettings(cfg *config.Config) map[string]any {
	theme := claudeThemeID(cfg)
	if theme == customClaudeTheme {
		theme = ""
	}
	return claudeSettingsWithTheme(cfg, theme)
}

func claudeSettingsWithTheme(cfg *config.Config, theme string) map[string]any {
	want := map[string]any{}
	if theme != "" {
		want["theme"] = theme
		want["themeId"] = theme
	}
	if plugins := append(append([]string{}, cfg.Claude.Plugins.Install...), cfg.Claude.Plugins.InstallOptional...); len(plugins) > 0 {
		enabled := make(map[string]any, len(plugins))
		for _, plugin := range plugins {
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
	if body := jsonBody(current); body != "" {
		if err := decodeJSON(body, &existing); err != nil {
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
