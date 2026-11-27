# Configuration

lid reads `lid.yaml` from the project directory, or `.config/lid/lid.yaml` when there is no `lid.yaml`. `--config` points it at another file. `lid init` writes a commented starter for either backend, and `lid config` prints the config as lid resolved it.

Unknown keys are errors, so a typo fails loudly instead of being ignored.

Each key below says which backend it applies to. Setting a key on a backend that cannot use it is an error, unless the entry says the key is ignored there.

## Changing the config of a running sandbox

On sbx, lid records a fingerprint of the config inside the sandbox. When the file changes, `lid start` reports the sandbox as stale, and `lid start --rebuild` replaces it. These keys are applied on every start and never make the sandbox stale: `memory`, `display_name`, `shell`, `claude.settings` and `claude.theme`.

nono builds nothing ahead of time, so every change applies on the next `lid start`.

## Project

### `name`

**Required.** The sandbox name: lowercase letters, digits and dashes, starting and ending with a letter or digit, at most 63 characters. `lid init` uses the directory name.

```yaml
name: my-project
```

### `agent`

The coding agent to run. Defaults to `claude-code`, the one lid is tested with. Also accepted: `codex`, `copilot`, `cursor`, `docker-agent`, `droid`, `gemini`, `kiro`, `opencode`, `shell`.

### `backend`

`sbx` (the default) or `nono`. See [backends](backends.md). `lid start <backend>` or `--backend` overrides it for one project until `lid stop`.

### `display_name`

The name of the terminal tab lid starts in.

```yaml
display_name: my-project
```

### `zellij.display_name`

Deprecated. Use `display_name`.

## Image (sbx only)

### `base_image`

The image the generated Dockerfile builds on. Defaults to `docker/sandbox-templates:claude-code-minimal`.

### `template`

A ready-made template image. lid skips Dockerfile generation, so `base_image`, `packages`, `runtimes`, `bridges` and `scripts.on_start` cannot be set with it. Install those in your own Dockerfile instead.

```yaml
template: docker.io/my-org/custom-template:v1
```

### `packages`

Debian packages installed with `apt-get`.

```yaml
packages:
  - ripgrep
  - jq
```

### `php.extensions`

PHP extensions installed on top of `php-cli`. Needs the `php` runtime. Ignored on nono.

```yaml
php:
  extensions:
    - sqlite3
```

## Runtimes

### `runtimes`

Both backends. A map of runtime to version. On sbx, lid installs that version in the image. On nono, lid installs nothing: declaring a runtime grants the host's toolchain and lets its package manager write to its cache.

Known runtimes: `bun`, `go`, `node`, `php`, `pnpm`, `python`, `rust`. `pnpm` needs `node`.

```yaml
runtimes:
  node: "24"
  pnpm: "11"
  go: "1.27"
```

Quote versions. An unquoted `3.10` is kept as written, but quotes make the intent clear.

## Workspace

### `workspace`

sbx only, and ignored on nono. A second path for the project inside the sandbox, which must be absolute. It's a symlink to the mirrored host path, where shells and `on_start` scripts begin.

```yaml
workspace: /workspace
```

### `clone`

sbx only. Work on a git clone on the sandbox's own disk instead of the host's shared copy. The agent's commits come back over the `sandbox-<name>` git remote rather than appearing in your editor. This is useful when a dev server churns files, because the shared mount can go stale under load.

```yaml
clone: true
```

### `extra_workspaces`

sbx only. Host directories mounted beside the project. Write each one as `HOST`, `HOST:ro`, `HOST:SANDBOX_PATH` or `HOST:SANDBOX_PATH:ro`. Host paths start with `/` or `~`, and sandbox paths are absolute. Without a `SANDBOX_PATH`, the directory keeps its host path inside the sandbox. On nono, grant host directories with `paths` instead.

```yaml
extra_workspaces:
  - ~/Developer/shared-lib:ro
  - ~/datasets:/workspace/data
```

### `resources.cpus`

sbx only. CPUs for the sandbox. Without it, sbx gives the sandbox every host CPU.

### `resources.memory`

sbx only. Memory for the sandbox in binary units, such as `512m` or `8g`, with a minimum of `512m`. Without it, sbx gives the sandbox half the host's memory.

```yaml
resources:
  cpus: 4
  memory: 8g
```

## Host access (nono only)

A nono session is caged around the project directory. Anything outside it has to be granted here. When a session ends, nono prints what it refused, and that output is usually where these entries come from. Paths start with `/` or `~`.

### `paths.read`

Directories the agent may read.

### `paths.read_file`

Single files the agent may read.

### `paths.allow`

Directories the agent may also write to.

### `paths.deny`

Directories to refuse even when a grant above would include them.

```yaml
paths:
  read:
    - ~/Developer/dotfiles/configs/claude
  read_file:
    - ~/Developer/dotfiles/configs/claude/CLAUDE.md
  allow:
    - ~/.cache/my-tool
  deny:
    - ~/.ssh
```

### `allow_commands`

Commands nono denies by default that this project needs. nono lifts them a group at a time, so allowing one command unblocks the rest of its group. `lid start` prints what each entry widens.

```yaml
allow_commands:
  - npm
```

## Network

### `network`

Both backends. Hosts the sandbox may reach. Wildcards such as `*.npmjs.org` are allowed.

On sbx, this list is the whole allowlist, plus the agent's own API and anything `handoff` and `bridges` need. On nono, it adds to `network_profile`.

`lid network allow` changes a running sandbox without restarting it, and `--persist` writes the host back here.

```yaml
network:
  - "*.npmjs.org"
  - api.github.com
```

### `network_profile`

nono only. The base network policy: `minimal` (LLM endpoints only), `developer` (adds package registries, GitHub, GitLab, sigstore and documentation), `enterprise` (adds cloud provider APIs), or an agent's own profile (`claude-code`, `codex`, `opencode`). Without it, nono uses the profile for the configured agent.

### `audit`

nono only. Keep nono's audit trail. lid turns it off by default because its network log holds 4,096 requests, and once a long session fills it, nono warns on every request after that.

### `ports`

Both backends. Ports published from the sandbox: `PORT`, `HOST:PORT`, or either form followed by `/PROTOCOL` (`tcp`, `tcp4`, `tcp6`, `udp`, `udp4`, `udp6`). nono cannot remap ports, so a `HOST:PORT` entry listens on `PORT`.

```yaml
ports:
  - 3000
  - "8080:80"
  - "5432:5432/tcp"
```

### `bridges.name`, `bridges.port`, `bridges.host_port`, `bridges.env`

sbx only, and ignored on nono. A bridge makes a service on your host reachable inside the sandbox as `name:port`, so the project's `.env` needs no sandbox-specific edits. lid opens `host_port` in the network policy and installs `socat`. `env` names a variable to point at the bridge for runtimes that mount `/etc/hosts` read-only.

```yaml
bridges:
  - name: postgres
    port: 5432
    host_port: 5432
    env: DB_HOST
```

## Environment

### `env`

Both backends. Inline environment variables. These override `env_file`.

```yaml
env:
  NODE_ENV: development
```

### `env_file`

Both backends. Environment files loaded in order, with later files winning.

```yaml
env_file:
  - .env
  - .env.sandbox
```

## Lifecycle

### `scripts.on_create`

Commands run once after the sandbox is created. On nono, they run on your host from the project directory the first time lid starts the project.

### `scripts.on_start`

Commands run on every start, including a resume after a stop. They must be idempotent. On sbx, they can't be combined with `template`.

```yaml
scripts:
  on_create:
    - pnpm install
  on_start:
    - ./scripts/warm-cache.sh
```

## Shell

### `shell.directory`

The working directory for `lid shell`.

### `shell.aliases`

Aliases for the interactive `lid shell`, added alongside the ones already defined. A one-off `lid shell -- <command>` gets the directory but not the aliases. On nono, setting either key switches the shell to bash so lid can hand it an init file.

```yaml
shell:
  directory: /home/agent/project
  aliases:
    g: git
```

## Agent

### `memory`

sbx only, and ignored on nono. Instructions injected into the agent's memory file.

```yaml
memory: |
  Run the tests with 'make test'.
```

### `claude.theme`

One of Claude Code's themes (`dark`, `light`, `dark-ansi`, `light-ansi`, `dark-daltonized`, `light-daltonized`), or a path to a theme file to install. On nono, a theme name is applied the same way as `claude.settings`, but a theme file is ignored because the session reads themes from your own `~/.claude`.

### `claude.settings`

Merged into Claude Code's `settings.json` on sbx. On nono, lid merges them into the project's `.claude/settings.local.json` when that file exists, and its values win. lid never creates that file and never touches the settings in your home.

```yaml
claude:
  settings:
    alwaysThinkingEnabled: true
```

### `claude.plugins.marketplaces`

sbx only. Plugin marketplaces to add.

### `claude.plugins.install`

sbx only. Plugins to install.

### `claude.plugins.install_optional`

sbx only. Plugins installed when their marketplace is available and skipped otherwise. Use this for plugins from a local marketplace folder that not everyone has.

```yaml
claude:
  plugins:
    marketplaces:
      - anthropics/claude-plugins-official
    install:
      - superpowers@claude-plugins-official
```

### `mcp`

sbx only, and ignored on nono. Host MCP servers the agent may use, named as they were registered with `sbx mcp add`. The list is fixed when the sandbox is created. An empty list gives the agent none.

```yaml
mcp:
  - my-server
```

## Herdr panes

### `herdr.panes`

Both backends. Off by default. Lets the agent open a new Herdr pane beside it that runs a command inside the same sandbox, in the project directory. Ask it to "run the dev server in another pane" and the pane appears. You can type into it, stop it with Ctrl-C and rerun the command. When the command exits, the pane stays open as a sandbox shell.

```yaml
herdr:
  panes: true
```

It only takes effect when lid runs inside Herdr and the agent is `claude-code`. Otherwise lid notes why and starts the agent as usual. `lid doctor` shows whether it is on and whether lid is in Herdr.

How it works: for each session, `lid start` listens on a random localhost port behind a random token and tells the agent the address through its system prompt. The agent can only ask for a pane running a command. It can't read the pane's output, type into it or close it. To see the output, the agent writes it to a file with `tee`. lid builds the pane from the config it loaded at start, so editing `lid.yaml` mid-session doesn't change what a pane runs.

- On sbx, lid adds a `localhost:<port>` network rule for the sandbox while the session runs and removes it when the session ends. If lid is killed, the next `lid start` or `lid stop` removes the leftover rule.
- On nono, lid runs the agent and each pane with `--open-port <port>`, which lets them reach the listener's port and no other localhost port. The session uses a private copy of the nono profile that the agent can't edit. lid also refuses to open panes when `sbx`, `nono`, `herdr-app` or `lid` itself lives inside the project. Don't give the agent write access through `paths.allow` to a directory that holds one of them, such as `~/.local/bin`. It could replace the program, and lid would then run that program on your host.
- Panes start with Herdr's environment, not the one `lid start` ran with.
- The token is passed on the agent's command line. On a Mac shared with other users, they can read it with `ps`.

## Handoff

### `handoff.enabled`

Both backends. Lets the agent ask agents in other sandboxes questions and answer theirs. Run the broker on your host with `lid handoff -D`. On nono, run `lid handoff --install-skill` once to teach the agent how to use it.

### `handoff.url`

Where the broker listens. Defaults to `http://host.docker.internal:8787` on sbx and `http://localhost:8787` on nono.

```yaml
handoff:
  enabled: true
```

## Per-backend sections

### `backends`

Keys that apply to one backend only, under `backends.sbx` or `backends.nono`. When that backend is active, its section merges onto the shared config: lists add to the shared lists, maps merge, and single values replace. lid validates every section, so a mistake in one shows up before you switch to that backend.

```yaml
network:
  - api.github.com
backends:
  nono:
    paths:
      read:
        - ~/Developer/shared-lib
  sbx:
    resources:
      memory: 8g
```

When a section sets `template`, keep `runtimes` and `scripts.on_start` out of the shared config.
