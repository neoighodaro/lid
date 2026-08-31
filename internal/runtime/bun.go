package runtime

import "fmt"

type bun struct{}

func (bun) Name() string { return "bun" }

func (bun) Validate(version string) error {
	return requireVersion("bun", version)
}

func (bun) RootBlock(string) string { return "" }

func (bun) AgentBlock(version string) string {
	return fmt.Sprintf(`RUN curl -fsSL https://bun.sh/install | bash -s "bun-v%s"`, version)
}

func (bun) EnvBlock(string) string {
	return `ENV BUN_INSTALL="/home/agent/.bun"
ENV PATH="$BUN_INSTALL/bin:$PATH"`
}
