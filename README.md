# lid

Let your agents run wild without running wild.

lid runs a coding agent, such as Claude Code, in a sandbox set up for your project. One `lid.yaml` describes the tools, network access and host directories the agent gets, and `lid start` does the rest.

There are two backends:

- **sbx** (the default) runs the agent in a [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) microVM, built from an image lid generates.
- **nono** runs the agent as a host process caged by [nono](https://nono.sh).

## Install

Download the tarball for your platform from the [releases](https://github.com/neoighodaro/lid/releases), and put `lid` on your `PATH`. After that, `lid update` keeps it current.

To build from source with Go:

```sh
make install   # builds ./lid and installs it to ~/Developer/bin
```

## Quickstart

```sh
cd my-project
lid init          # writes lid.yaml for sbx; 'lid init nono' for nono
lid doctor        # checks lid, the backends and this project
lid start         # builds the sandbox and starts the agent
```

Later:

```sh
lid resume        # pick up the agent's last session
lid shell         # open a shell in the sandbox
lid stop          # stop it, keeping it to start again
```

A minimal sbx config:

```yaml
name: my-project
runtimes:
  node: "24"
network:
  - "*.npmjs.org"
scripts:
  on_create:
    - npm install
```

## Docs

- [Configuration](docs/configuration.md): every key in `lid.yaml`
- [Backends](docs/backends.md): sbx vs nono, and when to use which
- [Commands](docs/commands.md): every command and flag
