# Commands

<!-- Generated from the command tree by 'make docs'. Do not edit by hand. -->

Every command reads `lid.yaml` from the current directory, or `.config/lid/lid.yaml` when there is none.

## Global flags

```
  -b, --backend string   backend to run this project on (nono or sbx)
  -C, --config string    path to config file (overrides discovery)
```

## lid build

Build the sandbox image for this project.

```
lid build [flags]
```

```
      --print   print the generated Dockerfile instead of building it
```

## lid config

Show the resolved configuration.

```
lid config
```

## lid doctor

Check that lid, its backends and this project are ready to run.

```
lid doctor
```

## lid handoff

Run the broker that lets agents in different sandboxes ask each other questions.

```
lid handoff [flags]
```

```
  -D, --detach          run the broker in the background
      --force           with --install-skill, replace a handoff skill lid did not write
      --install-skill   teach this project's agent to use handoff
  -K, --kill            stop a broker running in the background
  -P, --port int        port to listen on (default from handoff.url, else 8787)
```

## lid init

Create a starter lid config for a backend, defaulting to sbx.

```
lid init [backend] [flags]
```

```
      --name string        sandbox name (defaults to the directory name)
      --runtimes strings   runtimes to pin, like node@24,go@1.27 (sbx only)
```

## lid network

Change what this project's sandbox can reach on the network.

These rules apply to the sandbox that is already up, so a blocked request can be
unblocked without restarting the agent. They last only as long as the sandbox
unless --persist writes them back to the config.

```
lid network
```

## lid network allow

Let the sandbox reach one or more hosts.

```
lid network allow <host>... [flags]
```

```
      --mins int   take the rule away again after N minutes (1-30); waits here showing a countdown, and cannot be combined with --persist
      --persist    also add the hosts to 'network:' in the config, so later sandboxes get them too
```

## lid network deny

Stop the sandbox reaching one or more hosts.

A deny lasts as long as the sandbox does. The config lists what is allowed and has
nowhere to record a denial, so there is no --persist here.

```
lid network deny <host>...
```

## lid network remove

Take back rules already made for one or more hosts.

```
lid network remove <host>... [flags]
```

```
      --persist   also drop the hosts from 'network:' in the config
```

## lid restart

Replace this project's sandbox, then start the agent in the new one.

The old sandbox is destroyed, so whatever is in it that is not part of your
project goes with it. This asks first unless --force says not to.

'lid restart <backend>' switches: it stops the session on the other backend
first, then starts on this one and remembers it until 'lid stop'.

```
lid restart [backend] [flags]
```

```
  -f, --force   replace the sandbox without asking first
```

## lid resume

Start the sandboxed agent, resuming its last session in this project.

The same as 'lid start --resume'. --pick lets you choose which session.
On sbx the sessions live in the sandbox, so none survive 'lid restart'.

```
lid resume [backend] [flags]
```

```
      --pick   choose an earlier session to resume
```

## lid shell

Open a shell in this project's sandbox, or run one command in it.

The sandbox is created first if it is not there yet, exactly as 'lid start' would.
An interactive shell picks up the aliases and directory from the shell: block;
a one-off command gets the directory, but bash expands no aliases for it.

```
lid shell [-- command ...] [flags]
```

Aliases: `ssh`

```
      --rebuild   replace the sandbox before opening the shell, ending any sessions running in it
```

## lid start

Start the sandboxed agent for this project.

Name a backend, as in 'lid start nono', to run on it instead of the one in the
config; lid remembers it for this project until 'lid stop'.

--resume picks up the agent's last session in this project, and --pick lets
you choose which one. 'lid resume' is the same as 'lid start --resume'.

```
lid start [backend] [flags]
```

```
      --pick      choose an earlier session to resume
      --print     print the generated sandbox profile instead of starting the agent
      --rebuild   replace the sandbox before starting, ending any sessions running in it
      --resume    resume the agent's last session in this project
```

## lid status

Show whether this project's sandbox exists and is running.

```
lid status
```

## lid stop

Stop this project's sandbox, leaving it there to start again.

```
lid stop
```

## lid update

Replace this lid binary with the latest release.

```
lid update
```

## lid version

Print the version number.

```
lid version
```

