# Planning & Specs
- NEVER commit plans or specs to the repository
- NEVER commit while implementing plans in the superpowers skill or any other skill
- Don't litter code with comments
- Plans, SDD, Specs must only exist inside `.claude/docs` not `.superpowers`, `docs`, `plans`, etc. and not committed

# Sandbox
- You might be running inside a sandbox where some commands (e.g. `zellij`, host-only tools) are unavailable or behave differently
- Check with the `IS_SANDBOX` env var: if `IS_SANDBOX=1` you are sandboxed
- When a command fails because you are sandboxed, ask the user to run it on their host and paste the output back — do not assume the command is broken

# Building
- ALWAYS build after making code changes
- To build, run `go build -o lid .` — this writes the `./lid` artifact that `~/.local/bin/lid` symlinks to
- Do NOT rely on `go build ./...` to test changes — it only compile-checks and discards the binary, so the installed/symlinked binary stays stale
- NEVER write the real `./lid` artifact from inside the sandbox (`IS_SANDBOX=1`): the sandbox is Linux, so the binary cannot run on the user's Mac, and it overwrites the host's working artifact that `~/.local/bin/lid` symlinks to. The user builds and runs on their Mac.
- To build inside sandbox, run `make mac-arm64` — it cross-compiles to `dist/lid-darwin-arm64`, stamps version/commit/date via ldflags, and is safe from inside the sandbox because it never touches `./lid`. `make mac` builds arm64 and amd64 together.
- Go's internal linker emits an ad-hoc code signature for darwin/arm64 even when cross-compiling from Linux, so `dist/lid-darwin-arm64` executes on Apple Silicon as-is. Leave it in `dist/` for the user to install — the `./lid` prohibition above still stands.
- Keep the dependency tree cgo-free: a cgo dependency breaks `make mac-arm64` from the sandbox, since there is no macOS toolchain here.
