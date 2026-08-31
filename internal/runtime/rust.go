package runtime

type rust struct{}

func (rust) Name() string { return "rust" }

func (rust) Validate(version string) error {
	return requireVersion("rust", version)
}

func (rust) RootBlock(string) string { return "" }

func (rust) AgentBlock(string) string {
	return `RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y`
}

func (rust) EnvBlock(string) string {
	return `ENV PATH="/home/agent/.cargo/bin:$PATH"`
}
