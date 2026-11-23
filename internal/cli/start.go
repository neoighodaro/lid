package cli

import (
	"fmt"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/spf13/cobra"
)

func newStartCommand(a *app.App) *cobra.Command {
	var printOnly, rebuild, resume, pick bool

	cmd := &cobra.Command{
		Use:   "start [backend]",
		Short: "Start the sandboxed agent for this project",
		Long: "Start the sandboxed agent for this project.\n\n" +
			"Name a backend, as in 'lid start nono', to run on it instead of the one in the\n" +
			"config; lid remembers it for this project until 'lid stop'.\n\n" +
			"--resume picks up the agent's last session in this project, and --pick lets\n" +
			"you choose which one. 'lid resume' is the same as 'lid start --resume'.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return start(cmd, a, args, startOptions{print: printOnly, rebuild: rebuild, resume: resumeMode(resume, pick)})
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "print the generated sandbox profile instead of starting the agent")
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "replace the sandbox before starting, ending any sessions running in it")
	cmd.Flags().BoolVar(&resume, "resume", false, "resume the agent's last session in this project")
	cmd.Flags().BoolVar(&pick, "pick", false, "choose an earlier session to resume")
	return cmd
}

func newResumeCommand(a *app.App) *cobra.Command {
	var pick bool

	cmd := &cobra.Command{
		Use:   "resume [backend]",
		Short: "Start the sandboxed agent, resuming its last session in this project",
		Long: "Start the sandboxed agent, resuming its last session in this project.\n\n" +
			"The same as 'lid start --resume'. --pick lets you choose which session.\n" +
			"On sbx the sessions live in the sandbox, so none survive 'lid restart'.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return start(cmd, a, args, startOptions{resume: resumeMode(true, pick)})
		},
	}

	cmd.Flags().BoolVar(&pick, "pick", false, "choose an earlier session to resume")
	return cmd
}

type startOptions struct {
	print   bool
	rebuild bool
	resume  backend.Resume
}

func resumeMode(resume, pick bool) backend.Resume {
	switch {
	case pick:
		return backend.ResumePick
	case resume:
		return backend.ResumeLast
	}
	return backend.ResumeNone
}

func start(cmd *cobra.Command, a *app.App, args []string, opts startOptions) error {
	if opts.rebuild && opts.resume != backend.ResumeNone {
		return failure.New("cannot resume a session in a sandbox --rebuild replaces").
			WithFix("drop --rebuild to resume, or --resume to start fresh")
	}
	if err := takeBackendArg(a, args); err != nil {
		return err
	}
	cfg, err := a.Config()
	if err != nil {
		return err
	}

	b, err := resolveBackend(a, cfg)
	if err != nil {
		return err
	}

	if opts.print {
		profile, err := b.Render(cfg)
		if err != nil {
			return err
		}
		fmt.Fprint(a.Out, profile)
		return nil
	}

	agentArgs, err := backend.ResumeArgs(cfg.Agent, opts.resume)
	if err != nil {
		return err
	}

	if err := ensureOnlyBackend(a, cfg); err != nil {
		return err
	}

	a.Zellij.RenameTab(cfg.Zellij.DisplayName)

	provisioned, err := provisionSandbox(cmd, a, b, cfg, backend.Options{Rebuild: opts.rebuild})
	if err != nil {
		return err
	}
	if err := rememberIfNamed(cfg); err != nil {
		return err
	}

	argv, err := b.StartArgv(cfg, provisioned.Path, agentArgs)
	if err != nil {
		return err
	}
	return a.Launch(argv)
}
