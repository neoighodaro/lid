package runtime

// Runtime emits the Dockerfile fragments that install one language runtime.
//
// The blocks are split by the user they run as: RootBlock lands before the
// image drops to the agent user (apt, /usr/local writes), AgentBlock after it
// (per-user installers like uv and rustup), and EnvBlock carries the ENV lines
// that put the result on PATH.
type Runtime interface {
	Name() string
	Validate(version string) error
	RootBlock(version string) string
	AgentBlock(version string) string
	EnvBlock(version string) string
}

type Options struct {
	PHPExtensions []string
}

var registry = map[string]func(Options) Runtime{
	"bun":    func(Options) Runtime { return bun{} },
	"go":     func(Options) Runtime { return golang{} },
	"node":   func(Options) Runtime { return node{} },
	"php":    func(o Options) Runtime { return php{extensions: o.PHPExtensions} },
	"pnpm":   func(Options) Runtime { return pnpm{} },
	"python": func(Options) Runtime { return python{} },
	"rust":   func(Options) Runtime { return rust{} },
}

// Get returns the runtime registered under name, or nil when there is none.
func Get(name string, opts Options) Runtime {
	newRuntime, ok := registry[name]
	if !ok {
		return nil
	}
	return newRuntime(opts)
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
