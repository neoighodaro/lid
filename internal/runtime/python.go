package runtime

import "fmt"

type python struct{}

func (python) Name() string { return "python" }

func (python) Validate(version string) error {
	return requireVersion("python", version)
}

// RootBlock is empty: python is installed per-user via uv (see AgentBlock).
//
// Installing it with apt only works when the requested minor happens to match
// the base image's distro default, since Ubuntu ships exactly one python3 minor
// per release. uv installs any pinned version from python-build-standalone.
func (python) RootBlock(string) string { return "" }

func (python) AgentBlock(version string) string {
	return fmt.Sprintf(`RUN curl -LsSf https://astral.sh/uv/install.sh | sh \
 && /home/agent/.local/bin/uv python install %s --default`, version)
}

func (python) EnvBlock(string) string {
	return `ENV PATH="/home/agent/.local/bin:$PATH"`
}
