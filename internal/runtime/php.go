package runtime

import (
	"fmt"
	"strings"
)

var phpVersions = []string{"8.4", "8.5"}

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
	var b strings.Builder

	if version == "8.5" {
		b.WriteString(resolutePinBlock)
	}

	exts := p.effectiveExtensions()
	pkgs := make([]string, 0, len(exts)+1)
	pkgs = append(pkgs, fmt.Sprintf("php%s-cli", version))
	for _, ext := range exts {
		pkgs = append(pkgs, fmt.Sprintf("php%s-%s", version, ext))
	}

	b.WriteString(fmt.Sprintf(`RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      %s \
 && rm -rf /var/lib/apt/lists/*
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer`, strings.Join(pkgs, " ")))

	return b.String()
}

func (php) AgentBlock(string) string { return "" }

func (php) EnvBlock(string) string {
	return `ENV COMPOSER_HOME="/home/agent/.composer"
ENV COMPOSER_IGNORE_PLATFORM_REQS=1
ENV PATH="$COMPOSER_HOME/vendor/bin:$PATH"`
}

// resolutePinBlock adds Ubuntu 26.04 (Resolute) repos with APT pinning so only
// PHP and its dependencies are upgraded. Ubuntu 25.04 (Plucky), which the base
// image ships, only carries up to PHP 8.4.
//
// Ubuntu serves amd64/i386 from archive.ubuntu.com and every other
// architecture from ports.ubuntu.com, so the mirror is resolved from the
// image's own dpkg architecture rather than hardcoded. HTTPS throughout: some
// networks silently drop plain HTTP to the mirror, hanging apt.
const resolutePinBlock = `RUN case "$(dpkg --print-architecture)" in \
      amd64|i386) MIRROR='https://archive.ubuntu.com/ubuntu/' ;; \
      *) MIRROR='https://ports.ubuntu.com/ubuntu-ports/' ;; \
    esac \
 && printf '%s\n' \
      'Types: deb' \
      "URIs: $MIRROR" \
      'Suites: resolute resolute-updates resolute-security' \
      'Components: main universe' \
      'Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg' \
    > /etc/apt/sources.list.d/resolute-php.sources \
 && printf '%s\n' \
      'Package: *' \
      'Pin: release n=resolute*' \
      'Pin-Priority: 100' \
      '' \
      'Package: php8.5* libc6 libc-bin libc-gconv-modules-extra libicu78' \
      'Pin: release n=resolute*' \
      'Pin-Priority: 500' \
    > /etc/apt/preferences.d/resolute-php
`
