package runtime

import "fmt"

type pnpm struct{}

func (pnpm) Name() string { return "pnpm" }

func (pnpm) Validate(version string) error {
	return requireVersion("pnpm", version)
}

func (pnpm) RootBlock(version string) string {
	return fmt.Sprintf("RUN npm install -g pnpm@%s", version)
}

func (pnpm) AgentBlock(string) string { return "" }
func (pnpm) EnvBlock(string) string   { return "" }
