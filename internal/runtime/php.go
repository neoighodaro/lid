package runtime

import (
	"fmt"
	"strings"
)

var phpVersions = []string{"8.5"}

type php struct {
	extensions []string
}

func (php) Name() string { return "php" }

func (php) Validate(version string) error {
	if err := requireVersion("php", version); err != nil {
		return err
	}
	if !contains(phpVersions, version) {
		return fmt.Errorf("unsupported php version %q: must be one of %s", version, strings.Join(phpVersions, ", "))
	}
	return nil
}

func (p php) effectiveExtensions() []string {
	seen := make(map[string]bool, len(p.extensions))
	result := make([]string, 0, len(p.extensions))
	for _, ext := range p.extensions {
		if !seen[ext] {
			seen[ext] = true
			result = append(result, ext)
		}
	}
	return result
}

func (p php) RootBlock(version string) string {
	exts := p.effectiveExtensions()
	pkgs := make([]string, 0, len(exts)+1)
	pkgs = append(pkgs, fmt.Sprintf("php%s-cli", version))
	for _, ext := range exts {
		pkgs = append(pkgs, fmt.Sprintf("php%s-%s", version, ext))
	}

	return fmt.Sprintf(`RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      %s \
 && rm -rf /var/lib/apt/lists/*
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer`, strings.Join(pkgs, " "))
}

func (php) AgentBlock(string) string { return "" }

func (php) EnvBlock(string) string {
	return `ENV COMPOSER_HOME="/home/agent/.composer"
ENV COMPOSER_IGNORE_PLATFORM_REQS=1
ENV PATH="$COMPOSER_HOME/vendor/bin:$PATH"`
}
