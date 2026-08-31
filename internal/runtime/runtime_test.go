package runtime

import (
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func get(t *testing.T, name string) Runtime {
	t.Helper()
	r := Get(name, Options{})
	if r == nil {
		t.Fatalf("Get(%q) = nil, want a runtime", name)
	}
	return r
}

func TestEveryConfigRuntimeIsRegistered(t *testing.T) {
	for _, name := range config.Runtimes {
		r := get(t, name)
		if r.Name() != name {
			t.Errorf("Get(%q).Name() = %q, want %q", name, r.Name(), name)
		}
	}
}

func TestRegistryHasNoRuntimesConfigRejects(t *testing.T) {
	for name := range registry {
		if !contains(config.Runtimes, name) {
			t.Errorf("registry has %q, which config.Runtimes does not list", name)
		}
	}
}

func TestGetUnknownRuntimeReturnsNil(t *testing.T) {
	if r := Get("perl", Options{}); r != nil {
		t.Errorf("Get(perl) = %v, want nil", r)
	}
}

func TestEveryRuntimeRejectsAnEmptyVersion(t *testing.T) {
	for _, name := range config.Runtimes {
		if err := get(t, name).Validate(""); err == nil {
			t.Errorf("%s.Validate(\"\") = nil, want an error", name)
		}
	}
}

func TestEveryRuntimeEmitsAtLeastOneBlock(t *testing.T) {
	versions := map[string]string{
		"bun": "1.1.0", "go": "1.23.4", "node": "22", "php": "8.5",
		"pnpm": "9.12.0", "python": "3.12", "rust": "1.81.0",
	}
	for _, name := range config.Runtimes {
		r := get(t, name)
		v := versions[name]
		if r.RootBlock(v) == "" && r.AgentBlock(v) == "" {
			t.Errorf("%s emits neither a root nor an agent block", name)
		}
	}
}

func TestGetReturnsAFreshInstanceEachCall(t *testing.T) {
	first := Get("php", Options{PHPExtensions: []string{"imagick"}})
	second := Get("php", Options{})

	if block := second.RootBlock("8.5"); strings.Contains(block, "imagick") {
		t.Errorf("second Get leaked the first call's extensions:\n%s", block)
	}
	if block := first.RootBlock("8.5"); !strings.Contains(block, "php8.5-imagick") {
		t.Errorf("first Get lost its extensions:\n%s", block)
	}
}

func TestPHPRejectsUnsupportedVersions(t *testing.T) {
	err := get(t, "php").Validate("9.9")
	if err == nil {
		t.Fatal("php.Validate(9.9) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "8.5") {
		t.Errorf("error = %v, want it to list the supported versions", err)
	}
}

func TestPHPSupportedVersionsAreListedInStableOrder(t *testing.T) {
	first := get(t, "php").Validate("9.9").Error()
	for i := 0; i < 50; i++ {
		if got := get(t, "php").Validate("9.9").Error(); got != first {
			t.Fatalf("error text varies between calls:\n%s\n%s", first, got)
		}
	}
}

func TestPHPInstallsNoExtensionsByDefault(t *testing.T) {
	block := get(t, "php").RootBlock("8.5")

	for _, want := range []string{"php8.5-cli", "composer"} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
	if got := strings.Count(block, "php8.5-") - strings.Count(block, "php8.5-cli"); got != 0 {
		t.Errorf("block installs %d extensions, want none:\n%s", got, block)
	}
}

func TestPHPInstallsConfiguredExtensionsWithoutDuplicating(t *testing.T) {
	block := Get("php", Options{PHPExtensions: []string{"imagick", "redis", "imagick"}}).RootBlock("8.5")

	if strings.Count(block, "php8.5-imagick") != 1 {
		t.Errorf("imagick appears %d times, want 1:\n%s", strings.Count(block, "php8.5-imagick"), block)
	}
	if !strings.Contains(block, "php8.5-redis") {
		t.Errorf("block missing a configured extension:\n%s", block)
	}
}

func TestGoResolvesTheLatestPatchForAPinnedMinor(t *testing.T) {
	block := get(t, "go").RootBlock("1.23")

	if !strings.Contains(block, "go.dev/dl/?mode=json") {
		t.Errorf("a major.minor pin must resolve its patch at build time:\n%s", block)
	}
}

func TestGoDownloadsAFullVersionDirectly(t *testing.T) {
	block := get(t, "go").RootBlock("1.23.4")

	if strings.Contains(block, "mode=json") {
		t.Errorf("a full version needs no lookup:\n%s", block)
	}
	if !strings.Contains(block, "go1.23.4.linux-$ARCH.tar.gz") {
		t.Errorf("block does not download the pinned version:\n%s", block)
	}
}

func TestNodeUsesTheTarballForAFullVersion(t *testing.T) {
	block := get(t, "node").RootBlock("22.9.0")

	if !strings.Contains(block, "nodejs.org/dist/v22.9.0") {
		t.Errorf("block does not fetch the pinned tarball:\n%s", block)
	}
}

func TestNodeUsesNodesourceForAMajorOnly(t *testing.T) {
	block := get(t, "node").RootBlock("22")

	if !strings.Contains(block, "deb.nodesource.com/setup_22.x") {
		t.Errorf("block does not use the nodesource repo:\n%s", block)
	}
}

func TestNodeKeepsTheBundledNpm(t *testing.T) {
	for _, version := range []string{"22", "22.9.0"} {
		if block := get(t, "node").RootBlock(version); strings.Contains(block, "rm -rf /usr") && strings.Contains(block, "npm") {
			t.Errorf("node %s removes npm, which the sandbox should keep:\n%s", version, block)
		}
	}
}

func TestNoRuntimeUsesCorepack(t *testing.T) {
	for _, name := range config.Runtimes {
		r := get(t, name)
		for _, block := range []string{r.RootBlock("1.0.0"), r.AgentBlock("1.0.0"), r.EnvBlock("1.0.0")} {
			if strings.Contains(strings.ToLower(block), "corepack") {
				t.Errorf("%s uses corepack, which node has deprecated:\n%s", name, block)
			}
		}
	}
}

func TestPnpmInstallsGloballyWithNpm(t *testing.T) {
	block := get(t, "pnpm").RootBlock("9.12.0")

	if !strings.Contains(block, "npm install -g pnpm@9.12.0") {
		t.Errorf("pnpm is not installed with npm:\n%s", block)
	}
}

func TestPythonInstallsViaUvAsTheAgentUser(t *testing.T) {
	r := get(t, "python")

	if r.RootBlock("3.12") != "" {
		t.Errorf("python needs no root block:\n%s", r.RootBlock("3.12"))
	}
	if !strings.Contains(r.AgentBlock("3.12"), "uv python install 3.12 --default") {
		t.Errorf("block does not install the pinned version with uv:\n%s", r.AgentBlock("3.12"))
	}
}

func TestPythonKeepsATrailingZeroInTheVersion(t *testing.T) {
	if block := get(t, "python").AgentBlock("3.10"); !strings.Contains(block, "install 3.10 ") {
		t.Errorf("version was mangled:\n%s", block)
	}
}

func TestRuntimesThatNeedPathPutItInTheEnvBlock(t *testing.T) {
	for _, name := range []string{"bun", "go", "python", "rust"} {
		if block := get(t, name).EnvBlock("1.0.0"); !strings.Contains(block, "PATH=") {
			t.Errorf("%s does not extend PATH:\n%s", name, block)
		}
	}
}

func TestRustRejectsFloatingChannels(t *testing.T) {
	rust := Get("rust", Options{})

	for _, channel := range []string{"stable", "beta", "nightly", "latest"} {
		if err := rust.Validate(channel); err == nil {
			t.Errorf("rust accepted %q, which the layer cache would freeze invisibly", channel)
		}
	}
	if err := rust.Validate("1.98.0"); err != nil {
		t.Errorf("rust 1.98.0 rejected: %v", err)
	}
}

func TestBunPinnedVersionInstallsThatReleaseTag(t *testing.T) {
	block := Get("bun", Options{}).AgentBlock("1.2.3")

	if !strings.Contains(block, `"bun-v1.2.3"`) {
		t.Errorf("bun 1.2.3 does not pin the release tag:\n%s", block)
	}
}

func TestRustInstallsTheRequestedToolchain(t *testing.T) {
	block := Get("rust", Options{}).AgentBlock("1.75.0")

	if !strings.Contains(block, "--default-toolchain 1.75.0") {
		t.Errorf("rust 1.75.0 silently installs a different toolchain:\n%s", block)
	}
}

func TestPHPOnlySupportsWhatTheBaseImageCarries(t *testing.T) {
	php := Get("php", Options{})

	if err := php.Validate("8.5"); err != nil {
		t.Errorf("php 8.5 rejected: %v", err)
	}
	if err := php.Validate("8.4"); err == nil {
		t.Error("php 8.4 accepted, but ubuntu 26.04 carries no php8.4-cli")
	}
}

func TestPHPInstallsFromTheBaseImageRepos(t *testing.T) {
	block := Get("php", Options{}).RootBlock("8.5")

	for _, unwanted := range []string{"ppa:", "sources.list.d", "preferences.d", "Pin-Priority"} {
		if strings.Contains(block, unwanted) {
			t.Errorf("php reaches for a third-party repo (%s) it does not need:\n%s", unwanted, block)
		}
	}
	if !strings.Contains(block, "php8.5-cli") {
		t.Errorf("php does not install the cli:\n%s", block)
	}
}

func TestRuntimesRejectLatestWhereItIsNotAVersion(t *testing.T) {
	for _, name := range []string{"bun", "go", "node", "python"} {
		if err := Get(name, Options{}).Validate("latest"); err == nil {
			t.Errorf("%s accepted \"latest\", which fails deep inside docker build", name)
		}
	}
}
