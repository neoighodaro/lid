package runtime

import (
	"fmt"
	"strings"
)

type golang struct{}

func (golang) Name() string { return "go" }

func (golang) Validate(version string) error {
	return requireVersion("go", version)
}

func (golang) RootBlock(version string) string {
	if strings.Count(version, ".") >= 2 {
		return fmt.Sprintf(`RUN ARCH=$(dpkg --print-architecture) \
 && curl -fsSL https://go.dev/dl/go%s.linux-$ARCH.tar.gz | tar -C /usr/local -xzf -`, version)
	}
	return fmt.Sprintf(`RUN ARCH=$(dpkg --print-architecture) \
 && GOVER=$(curl -fsSL "https://go.dev/dl/?mode=json&include=all" | grep -oE '"go%s(\.[0-9]+)?"' | tr -d '"' | sed 's/^go//' | sort -V | tail -1) \
 && test -n "$GOVER" \
 && curl -fsSL "https://go.dev/dl/go$GOVER.linux-$ARCH.tar.gz" | tar -C /usr/local -xzf -`, version)
}

func (golang) AgentBlock(string) string { return "" }

func (golang) EnvBlock(string) string {
	return `ENV GOPATH="/home/agent/go"
ENV PATH="$GOPATH/bin:/usr/local/go/bin:$PATH"`
}
