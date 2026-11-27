# Backends

lid runs the agent on one of two backends. Both keep it away from what it shouldn't touch, but they do it differently.

| | sbx | nono |
|---|---|---|
| What it is | A [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) microVM, built from an image lid generates | A host process caged by [nono](https://nono.sh) |
| Filesystem | The project, plus any `extra_workspaces` | The project, plus whatever `paths` grants |
| Tools | Installed in the image from `packages` and `runtimes` | Your host's own toolchain |
| Network | Allowlist: `network`, plus the agent's API | `network_profile`, plus `network` |
| CPUs and memory | `resources`, or sbx's defaults | The host's |
| Agent sessions | Live in the sandbox, so they are lost on `lid restart` | Live in your home |
| Config changes | Make the sandbox stale until `lid start --rebuild` | Apply on the next start |

## Choosing a backend

Use **sbx**, the default, when you want the agent fully isolated. It gets its own machine, its own installed tools and nothing from your home that you didn't put there. It costs an image build and some disk.

Use **nono** when you want the agent to use the tools already on your machine with no image to build, and you're fine with it running as you within the limits you set.

`lid init nono` writes a nono starter config. To try a project on the other backend without editing the config, run `lid start nono` or `lid start sbx`. lid remembers that choice for the project until `lid stop`. Keys that only one backend uses can go under [`backends:`](configuration.md#backends).

## sbx

Install Docker Sandboxes from <https://docs.docker.com/ai/sandboxes/>.

- `lid build` builds the image, and `lid start` builds it too when needed.
- `lid start --rebuild` replaces the sandbox. `lid restart` does the same after asking first. Both destroy anything in the sandbox that isn't part of your project.
- `lid network allow <host>` opens a host on the running sandbox, and `--persist` saves it to the config.
- `clone: true` keeps the project on the sandbox's own disk. The agent's commits then come back over the `sandbox-<name>` git remote.
- With [`herdr.panes`](configuration.md#herdrpanes), the sandbox reaches lid's pane listener through `host.docker.internal`, using a network rule lid adds for the session and removes afterwards.

## nono

Install nono from <https://nono.sh>.

- When a session ends, nono prints what it refused. Add those entries to `paths` or `allow_commands`.
- Scripts run on your host, from the project directory.
- lid merges the `claude.settings` block into `.claude/settings.local.json` only when that file already exists.
- `lid start --print` shows the nono profile lid generates.
- Every profile lid generates denies lid's own cache directory, so the agent can't touch lid's session files.
- With [`herdr.panes`](configuration.md#herdrpanes) on, the agent runs under a private per-session copy of the profile, with `--open-port` for the listener's port only.
- With [`handoff`](configuration.md#handoffenabled) on and the broker on your Mac, the session gets `--open-port` for the broker's port only.

Run `lid doctor` to check that lid, both backends and the current project are ready.
