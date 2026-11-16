# Planning & Specs
- NEVER commit plans or specs to the repository
- NEVER commit while implementing plans in the superpowers skill or any other skill
- Don't litter code with comments
- Plans, SDD, Specs must only exist inside `.claude/docs` not `.superpowers`, `docs`, `plans`, etc. and not committed

# Sandbox
- You might be running inside a sandbox where some commands (e.g. `zellij`, host-only tools) are unavailable or behave differently
- Check with the `IS_SANDBOX` env var: if `IS_SANDBOX=1` you are sandboxed
- When a command fails because you are sandboxed, ask the user to run it on their host and paste the output back — do not assume the command is broken

# Releasing
- Pushing a `v*` tag runs `.github/workflows/release.yml`: tests, `make assets`, then a GitHub release with the tarballs and `checksums.txt` that `lid update` downloads
- Only tag when the user asks

# Building
- ALWAYS build and install after making code changes
- On the host, run `make install` — it builds `./lid` stamped with version/commit/date and installs it to `~/Developer/bin/lid`, the lid on the user's PATH
- `~/Developer/bin/lid` is a real file, not a symlink: `lid update` replaces whichever file is running. Never turn it back into a symlink into this repo, or updates would overwrite the repo's build output
- A source build reports a `git describe` version (e.g. `v0.0.1-3-gabc1234-dirty`), so `lid update` asks before replacing it with a release; that is expected
- Do NOT rely on `go build ./...` to test changes — it only compile-checks and discards the binary, so the installed binary stays stale
- NEVER run `make build` or `make install` inside the sandbox (`IS_SANDBOX=1`): the sandbox is Linux, so the binary cannot run on the user's Mac. Both refuse there.
- To build inside sandbox, run `make mac-arm64` — it cross-compiles to `dist/lid-darwin-arm64`, stamps version/commit/date via ldflags, and is safe from inside the sandbox because it never touches `./lid`. `make mac` builds arm64 and amd64 together.
- Go's internal linker emits an ad-hoc code signature for darwin/arm64 even when cross-compiling from Linux, so `dist/lid-darwin-arm64` executes on Apple Silicon as-is. Leave it in `dist/` and tell the user to run `make install` on the host — the sandbox prohibition above still stands.
- Keep the dependency tree cgo-free: a cgo dependency breaks `make mac-arm64` from the sandbox, since there is no macOS toolchain here.
