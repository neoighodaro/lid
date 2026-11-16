package template

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
)

const (
	marketplacesDir = "marketplaces"
	MarketplaceRoot = "/opt/lid/marketplaces"
)

type marketplace struct {
	Add  string
	Copy string
}

func isLocalMarketplace(entry string) bool {
	for _, prefix := range []string{"/", "~/", "./", "../"} {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return entry == "~" || entry == "." || entry == ".."
}

func localMarketplacePath(cfg *config.Config, entry string) string {
	if entry == "~" || strings.HasPrefix(entry, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(entry, "~"))
	}
	if filepath.IsAbs(entry) {
		return entry
	}
	return filepath.Join(cfg.ProjectDir, entry)
}

func marketplaceName(cfg *config.Config, index int, entry string) string {
	return fmt.Sprintf("%d-%s", index, config.SanitizeName(filepath.Base(localMarketplacePath(cfg, entry))))
}

func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	return err == nil && !info.IsDir()
}

func MissingMarketplaces(cfg *config.Config) []string {
	var missing []string
	if !config.IsClaudeAgent(cfg.Agent) {
		return nil
	}
	for _, entry := range cfg.Claude.Plugins.Marketplaces {
		if isLocalMarketplace(entry) && !hasManifest(localMarketplacePath(cfg, entry)) {
			missing = append(missing, entry)
		}
	}
	return missing
}

func optionalInstall(plugin string) string {
	skip := fmt.Sprintf("echo %s", shellQuote("lid: skipped optional plugin "+plugin+": it is not available"))
	_, market, ok := strings.Cut(plugin, "@")
	if !ok || market == "" {
		return fmt.Sprintf("claude plugin install %s || %s", shellQuote(plugin), skip)
	}
	pattern := shellQuote(`"name": *"` + market + `"`)
	return fmt.Sprintf("if claude plugin marketplace list --json | grep -q %s; then claude plugin install %s; else %s; fi", pattern, shellQuote(plugin), skip)
}

func marketplaces(cfg *config.Config) []marketplace {
	var out []marketplace
	for i, entry := range cfg.Claude.Plugins.Marketplaces {
		if !isLocalMarketplace(entry) {
			out = append(out, marketplace{Add: entry})
			continue
		}
		if !hasManifest(localMarketplacePath(cfg, entry)) {
			continue
		}
		name := marketplaceName(cfg, i, entry)
		out = append(out, marketplace{Add: MarketplaceRoot + "/" + name, Copy: marketplacesDir + "/" + name})
	}
	return out
}

func copyMarketplaces(cfg *config.Config, kitDir string) error {
	root := filepath.Join(kitDir, marketplacesDir)
	if err := os.RemoveAll(root); err != nil {
		return failure.New("cannot clear the copied marketplaces").At(root).WithFix("%v", err)
	}
	if !config.IsClaudeAgent(cfg.Agent) {
		return nil
	}
	for i, entry := range cfg.Claude.Plugins.Marketplaces {
		if !isLocalMarketplace(entry) || !hasManifest(localMarketplacePath(cfg, entry)) {
			continue
		}
		if err := copyMarketplace(localMarketplacePath(cfg, entry), filepath.Join(root, marketplaceName(cfg, i, entry)), entry); err != nil {
			return err
		}
	}
	return nil
}

func copyMarketplace(src, dst, entry string) error {
	manifest := filepath.Join(src, ".claude-plugin", "marketplace.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return failure.New("marketplace %q has no .claude-plugin/marketplace.json", entry).
			At(src).
			WithFix("point it at the folder that holds .claude-plugin/marketplace.json")
	}
	var parsed struct {
		Plugins []struct {
			Name   string `json:"name"`
			Source any    `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return failure.New("cannot read marketplace %q", entry).At(manifest).WithFix("%v", err)
	}
	if err := copyFile(manifest, filepath.Join(dst, ".claude-plugin", "marketplace.json"), 0o644); err != nil {
		return err
	}
	for _, plugin := range parsed.Plugins {
		source, ok := plugin.Source.(string)
		if !ok || !strings.HasPrefix(source, "./") {
			continue
		}
		rel := filepath.Clean(source)
		if rel == "." || strings.HasPrefix(rel, "..") {
			return failure.New("plugin %q in marketplace %q points outside it", plugin.Name, entry).
				At(manifest).
				WithFix("keep each plugin's source inside the marketplace folder")
		}
		if err := copyTree(filepath.Join(src, rel), filepath.Join(dst, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return failure.New("cannot read the marketplace").At(path).WithFix("%v", err)
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return failure.New("cannot copy the marketplace").At(target).WithFix("%v", err)
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return failure.New("cannot read the marketplace").At(path).WithFix("%v", err)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return failure.New("cannot copy the marketplace").At(dst).WithFix("%v", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return failure.New("cannot read the marketplace").At(src).WithFix("%v", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return failure.New("cannot copy the marketplace").At(dst).WithFix("%v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return failure.New("cannot copy the marketplace").At(dst).WithFix("%v", err)
	}
	return out.Close()
}
