package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/handoff"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

func newHandoffCommand(a *app.App) *cobra.Command {
	var port int
	var daemon, kill, installSkill, force bool

	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Run the broker that lets agents in different sandboxes ask each other questions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case kill && daemon:
				return failure.New("--detach and --kill ask for opposite things").
					WithFix("run 'lid handoff -K' to stop a broker, or 'lid handoff -D' to start one")
			case installSkill:
				return installHandoffSkill(a, force)
			case kill:
				return killBroker(a)
			case daemon:
				return detachBroker(a, brokerPort(a, port))
			default:
				return serveBroker(a, brokerPort(a, port))
			}
		},
	}

	cmd.Flags().IntVarP(&port, "port", "P", 0, "port to listen on (default from handoff.url, else "+strconv.Itoa(config.DefaultHandoffPort)+")")
	cmd.Flags().BoolVarP(&daemon, "detach", "D", false, "run the broker in the background")
	cmd.Flags().BoolVarP(&kill, "kill", "K", false, "stop a broker running in the background")
	cmd.Flags().BoolVar(&installSkill, "install-skill", false, "teach this project's agent to use handoff")
	cmd.Flags().BoolVar(&force, "force", false, "with --install-skill, replace a handoff skill lid did not write")
	return cmd
}

func installHandoffSkill(a *app.App, force bool) error {
	cfg, err := a.Config()
	if err != nil {
		return err
	}
	if err := checkSelection(cfg); err != nil {
		return err
	}
	path, err := backend.InstallHandoffSkill(a.SandboxHost(), cfg, force)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Err, ui.Err.Info.Render("Handoff skill installed in "+path))
	if !cfg.Handoff.Enabled {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render("handoff is off in "+cfg.Ref()+", so the agent has no broker to reach"))
		fmt.Fprintln(a.Err, ui.Err.Info.Render("  set 'handoff: enabled: true', then start the broker with 'lid handoff -D'"))
	}
	return nil
}

const detachEnv = "LID_HANDOFF_DETACHED"

func brokerPort(a *app.App, port int) int {
	if port > 0 {
		return port
	}
	if cfg, err := a.Config(); err == nil {
		return cfg.HandoffPort()
	}
	return config.DefaultHandoffPort
}

func serveBroker(a *app.App, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	store := handoff.NewStore(handoff.DefaultStorePath())

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return failure.New("cannot listen on %s", addr).
			WithFix("another broker may already be running; 'lid handoff -K' stops a detached one")
	}

	if os.Getenv(detachEnv) != "" {
		if err := writeBrokerFile(os.Getpid(), port); err != nil {
			return err
		}
		defer os.Remove(handoff.DefaultPidPath())
	}

	server := &http.Server{Handler: handoff.NewServer(store)}
	fmt.Fprintln(a.Err, ui.Err.Info.Render("Handoff broker on http://"+addr))
	fmt.Fprintln(a.Err, ui.Err.Info.Render("  board at http://"+addr+"/handoff"))
	fmt.Fprintln(a.Err, ui.Err.Info.Render("  threads in "+handoff.DefaultStorePath()))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}

	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	fmt.Fprintln(a.Err, ui.Err.Info.Render("Handoff broker stopped"))
	return nil
}

func detachBroker(a *app.App, port int) error {
	if b, running := runningBroker(); running {
		return failure.New("a handoff broker is already running on port %d (pid %d)", b.port, b.pid).
			WithFix("stop it with 'lid handoff -K' first")
	}

	self, err := os.Executable()
	if err != nil {
		return failure.New("cannot find the lid binary to detach").WithFix("%v", err)
	}
	if err := os.MkdirAll(handoff.Dir(), 0o755); err != nil {
		return failure.New("cannot create the handoff directory").At(handoff.Dir()).WithFix("%v", err)
	}
	log, err := os.OpenFile(handoff.DefaultLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return failure.New("cannot open the broker log").At(handoff.DefaultLogPath()).WithFix("%v", err)
	}
	defer log.Close()

	child := exec.Command(self, "handoff", "--port", strconv.Itoa(port))
	child.Env = append(os.Environ(), detachEnv+"=1")
	child.Stdout, child.Stderr = log, log
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		return failure.New("cannot start the broker in the background").WithFix("%v", err)
	}

	if err := awaitBroker(child.Process.Pid); err != nil {
		return err
	}

	fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("Handoff broker running in the background on port %d (pid %d)", port, child.Process.Pid)))
	fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("  board at http://127.0.0.1:%d/handoff", port)))
	fmt.Fprintln(a.Err, ui.Err.Info.Render("  log in "+handoff.DefaultLogPath()))
	fmt.Fprintln(a.Err, ui.Err.Info.Render("  stop it with 'lid handoff -K'"))
	return nil
}

func awaitBroker(pid int) error {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, running := runningBroker(); running && b.pid == pid {
			return nil
		}
		if gone, _ := processGone(pid); gone {
			return failure.New("the broker exited immediately").
				WithFix("its log says why: %s", handoff.DefaultLogPath())
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func killBroker(a *app.App) error {
	b, ok := readBrokerFile()
	if !ok {
		return failure.New("no handoff broker is running in the background").
			WithFix("start one with 'lid handoff -D'")
	}
	if gone, _ := processGone(b.pid); gone {
		os.Remove(handoff.DefaultPidPath())
		return failure.New("no handoff broker is running in the background").
			WithFix("cleared a stale pid file; start one with 'lid handoff -D'")
	}
	if !brokerAnswers(b.port) {
		return failure.New("pid %d is running but nothing answers as a broker on port %d", b.pid, b.port).
			WithFix("the pid file may name a recycled pid; check that process, then delete %s", handoff.DefaultPidPath())
	}
	if err := syscall.Kill(b.pid, syscall.SIGTERM); err != nil {
		return failure.New("cannot stop the broker (pid %d)", b.pid).WithFix("%v", err)
	}
	os.Remove(handoff.DefaultPidPath())
	fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("Handoff broker stopped (pid %d)", b.pid)))
	return nil
}

type broker struct {
	pid  int
	port int
}

func writeBrokerFile(pid, port int) error {
	path := handoff.DefaultPidPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return failure.New("cannot create the handoff directory").At(filepath.Dir(path)).WithFix("%v", err)
	}
	body := fmt.Sprintf("%d %d\n", pid, port)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return failure.New("cannot record the broker pid").At(path).WithFix("%v", err)
	}
	return nil
}

func readBrokerFile() (broker, bool) {
	data, err := os.ReadFile(handoff.DefaultPidPath())
	if err != nil {
		return broker{}, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return broker{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return broker{}, false
	}
	b := broker{pid: pid, port: config.DefaultHandoffPort}
	if len(fields) > 1 {
		if port, err := strconv.Atoi(fields[1]); err == nil && port > 0 {
			b.port = port
		}
	}
	return b, true
}

func runningBroker() (broker, bool) {
	b, ok := readBrokerFile()
	if !ok {
		return broker{}, false
	}
	if gone, err := processGone(b.pid); err != nil || gone {
		return b, false
	}
	return b, brokerAnswers(b.port)
}

func brokerAnswers(port int) bool {
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/handoff/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func processGone(pid int) (bool, error) {
	p, err := os.FindProcess(pid)
	if err != nil {
		return true, err
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return false, nil
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	return false, err
}
