package runtime

import (
	"fmt"
	"strings"
)

type node struct{}

func (node) Name() string { return "node" }

func (node) Validate(version string) error {
	return requireVersion("node", version)
}

func (node) RootBlock(version string) string {
	if strings.Contains(version, ".") {
		return fmt.Sprintf(`RUN ARCH=$(dpkg --print-architecture) \
 && if [ "$ARCH" = "amd64" ]; then ARCH="x64"; fi \
 && curl -fsSL https://nodejs.org/dist/v%s/node-v%s-linux-$ARCH.tar.gz | tar -xz -C /usr/local --strip-components=1`, version, version)
	}
	return fmt.Sprintf(`RUN curl -fsSL https://deb.nodesource.com/setup_%s.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && rm -rf /var/lib/apt/lists/*`, version)
}

func (node) AgentBlock(string) string { return "" }
func (node) EnvBlock(string) string   { return "" }
