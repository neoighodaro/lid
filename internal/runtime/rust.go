package runtime

import "fmt"

type rust struct{}

func (rust) Name() string { return "rust" }

func (rust) Validate(version string) error {
	if err := requireVersion("rust", version); err != nil {
		return err
	}
	return rejectFloating("rust", version, "latest", "stable", "beta", "nightly")
}

func (rust) RootBlock(string) string { return "" }

func (rust) AgentBlock(version string) string {
	return fmt.Sprintf(`RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain %s`, version)
}

func (rust) EnvBlock(string) string {
	return `ENV PATH="/home/agent/.cargo/bin:$PATH"`
}
