package cli

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/neoighodaro/lid/internal/app"
	"github.com/neoighodaro/lid/internal/backend"
	"github.com/neoighodaro/lid/internal/config"
	"github.com/neoighodaro/lid/internal/failure"
	"github.com/neoighodaro/lid/internal/ui"
	"github.com/spf13/cobra"
)

// maxAllowMinutes caps a timed allow. The countdown holds the terminal open for
// its whole window, so a long one is a session the user cannot use.
const maxAllowMinutes = 30

// countdownWidth is the widest the bar gets. A narrow terminal shrinks it,
// since a countdown that wraps cannot be redrawn in place.
const countdownWidth = 24

// countdownTick is how often the countdown redraws and re-checks the deadline.
var countdownTick = time.Second

func newNetworkCommand(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Change what this project's sandbox can reach on the network",
		Long: "Change what this project's sandbox can reach on the network.\n\n" +
			"These rules apply to the sandbox that is already up, so a blocked request can be\n" +
			"unblocked without restarting the agent. They last only as long as the sandbox\n" +
			"unless --persist writes them back to the config.",
	}

	cmd.AddCommand(
		newNetworkAllowCommand(a),
		newNetworkDenyCommand(a),
		newNetworkRemoveCommand(a),
	)
	return cmd
}

func newNetworkAllowCommand(a *app.App) *cobra.Command {
	var persist bool
	var minutes int

	cmd := &cobra.Command{
		Use:   "allow <host>...",
		Short: "Let the sandbox reach one or more hosts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkTimedAllow(minutes, persist); err != nil {
				return err
			}
			return runNetwork(a, backend.NetworkAllow, args, persist, time.Duration(minutes)*time.Minute)
		},
	}

	cmd.Flags().BoolVar(&persist, "persist", false, "also add the hosts to 'network:' in the config, so later sandboxes get them too")
	cmd.Flags().IntVar(&minutes, "mins", 0, fmt.Sprintf("take the rule away again after N minutes (1-%d); waits here showing a countdown, and cannot be combined with --persist", maxAllowMinutes))
	return cmd
}

func newNetworkDenyCommand(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "deny <host>...",
		Short: "Stop the sandbox reaching one or more hosts",
		Long: "Stop the sandbox reaching one or more hosts.\n\n" +
			"A deny lasts as long as the sandbox does. The config lists what is allowed and has\n" +
			"nowhere to record a denial, so there is no --persist here.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNetwork(a, backend.NetworkDeny, args, false, 0)
		},
	}
}

func newNetworkRemoveCommand(a *app.App) *cobra.Command {
	var persist bool

	cmd := &cobra.Command{
		Use:   "remove <host>...",
		Short: "Take back rules already made for one or more hosts",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNetwork(a, backend.NetworkRemove, args, persist, 0)
		},
	}

	cmd.Flags().BoolVar(&persist, "persist", false, "also drop the hosts from 'network:' in the config")
	return cmd
}

// checkTimedAllow rejects a --mins the command cannot honour. A zero means the
// flag went unused.
func checkTimedAllow(minutes int, persist bool) error {
	if minutes == 0 {
		return nil
	}
	if persist {
		return failure.New("--mins and --persist ask for opposite things").
			WithFix("--mins takes the rule away again, so there is nothing worth saving to the config")
	}
	if minutes < 1 || minutes > maxAllowMinutes {
		return failure.New("--mins is %d, which is outside 1 to %d", minutes, maxAllowMinutes).
			WithFix("for longer than that, add the host to 'network:' in the config instead")
	}
	return nil
}

// checkShellExpanded turns back a pattern the shell expanded before lid saw it.
// A host that is also the name of a file in the directory the command was run
// from is almost certainly what a bare '*' became on the way here, and letting
// it through writes one firewall rule per file.
func checkShellExpanded(dir string, domains []string) error {
	var files []string
	for _, domain := range domains {
		if _, err := os.Lstat(filepath.Join(dir, domain)); err == nil {
			files = append(files, domain)
		}
	}
	if len(files) == 0 {
		return nil
	}

	named := "names a file here, not a host"
	if len(files) > 1 {
		named = "name files here, not hosts"
	}
	return failure.New("%s %s", strings.Join(files, ", "), named).
		WithFix("your shell expanded a pattern before lid saw it; quote it, as in: lid network allow '*.example.com'")
}

func runNetwork(a *app.App, action backend.NetworkAction, domains []string, persist bool, window time.Duration) error {
	if err := checkShellExpanded(a.Cwd, domains); err != nil {
		return err
	}
	for _, domain := range domains {
		if err := config.ValidateDomain(domain); err != nil {
			return err
		}
	}

	cfg, err := a.Config()
	if err != nil {
		return err
	}

	b, err := resolveBackend(a, cfg)
	if err != nil {
		return err
	}
	if err := b.EnsureAvailable(a.SandboxHost()); err != nil {
		return err
	}

	state, err := b.Status(a.SandboxHost(), cfg, backend.StatusOptions{})
	if err != nil {
		return err
	}

	if !state.Exists && !persist {
		return noSandboxFor(cfg, action)
	}

	listed := strings.Join(domains, ", ")
	applied, deferred := false, ""
	if state.Exists {
		err := b.Network(a.SandboxHost(), cfg, action, domains)
		switch {
		case err == nil:
			applied = true
		// A backend that only decides policy at launch has not failed at
		// anything when --persist is the whole point of the command.
		case errors.Is(err, backend.ErrNetworkAtLaunch) && persist:
			deferred = err.Error()
			var f *failure.Error
			if errors.As(err, &f) {
				deferred = f.What
			}
		default:
			return err
		}
	}

	if persist {
		if err := persistNetwork(cfg, action, domains); err != nil {
			return err
		}
		fmt.Fprintln(a.Err, ui.Err.Success.Render("Updated "+cfg.Ref()))
	}

	switch {
	case applied:
		fmt.Fprintln(a.Err, ui.Err.Success.Render(appliedLine(action, listed)))
	case deferred != "":
		fmt.Fprintln(a.Err, ui.Err.Warn.Render(deferred))
		fmt.Fprintln(a.Err, ui.Err.Info.Render("  run 'lid start' to launch a session under the new policy"))
	default:
		fmt.Fprintln(a.Err, ui.Err.Info.Render("No sandbox is up, so this takes effect the next time you run 'lid start'"))
	}

	// A timed allow only gets here with a live sandbox: --mins rules out
	// --persist, so the guard above has already turned back an absent one.
	if window > 0 && action == backend.NetworkAllow && state.Exists {
		return awaitRevert(a, b, cfg, domains, window)
	}
	return nil
}

func noSandboxFor(cfg *config.Config, action backend.NetworkAction) *failure.Error {
	err := failure.New("there is no %s sandbox to change", cfg.Name)
	if action == backend.NetworkDeny {
		return err.WithFix("start one with 'lid start'; a deny lasts only as long as the sandbox, so it cannot be saved to the config")
	}
	return err.WithFix("start one with 'lid start', or pass --persist to write this to the config for next time")
}

func appliedLine(action backend.NetworkAction, listed string) string {
	switch action {
	case backend.NetworkAllow:
		return "The sandbox can now reach " + listed
	case backend.NetworkDeny:
		return "The sandbox can no longer reach " + listed
	default:
		return "Took back the rules for " + listed
	}
}

func persistNetwork(cfg *config.Config, action backend.NetworkAction, domains []string) error {
	switch action {
	case backend.NetworkAllow:
		return config.AddNetworkDomains(cfg.Path, domains)
	case backend.NetworkRemove:
		return config.RemoveNetworkDomains(cfg.Path, domains)
	}
	return nil
}

// awaitRevert holds the terminal for the length of the window, then takes the
// allow away again. It reverts on interruption too, so the rule cannot outlive
// the command that made it.
func awaitRevert(a *app.App, b backend.Backend, cfg *config.Config, domains []string, total time.Duration) error {
	listed := strings.Join(domains, ", ")
	deadline := time.Now().Add(total)
	live := isTTY(a.Err)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	ticker := time.NewTicker(countdownTick)
	defer ticker.Stop()

	if live {
		drawCountdown(a, listed, 0, total)
	} else {
		fmt.Fprintln(a.Err, ui.Err.Info.Render(fmt.Sprintf("Taking it back again in %s", formatRemaining(total))))
	}

	interrupted := false
	for waiting := true; waiting; {
		select {
		case <-signals:
			interrupted, waiting = true, false
		case now := <-ticker.C:
			remaining := deadline.Sub(now)
			if remaining <= 0 {
				waiting = false
				remaining = 0
			}
			if live {
				drawCountdown(a, listed, total-remaining, total)
			}
		}
	}

	if live {
		fmt.Fprint(a.Err, "\r\033[K")
	}

	if err := b.Network(a.SandboxHost(), cfg, backend.NetworkRemove, domains); err != nil {
		return failure.New("the allow for %s could not be taken back", listed).
			WithFix("take it back yourself with 'lid network remove %s'", listed)
	}

	if interrupted {
		fmt.Fprintln(a.Err, ui.Err.Warn.Render("Interrupted, and took back the allow for "+listed))
	} else {
		fmt.Fprintln(a.Err, ui.Err.Success.Render("Time is up, and the allow for "+listed+" is gone"))
	}
	return nil
}

func drawCountdown(a *app.App, listed string, elapsed, total time.Duration) {
	fmt.Fprint(a.Err, "\r\033[K"+ui.Err.Accent.Render(countdownLine(listed, elapsed, total, terminalWidth(a.Err))))
}

// countdownLine leads with the time left and trails with the hosts, then cuts
// itself to the width of the terminal. A line that wraps cannot be redrawn in
// place, because the carriage return only reaches the start of the last row,
// so what is worth reading has to come before what gets cut.
func countdownLine(listed string, elapsed, total time.Duration, width int) string {
	bar := countdownWidth
	if bar*2 > width {
		bar = width / 3
	}
	if bar < 1 {
		bar = 1
	}
	line := fmt.Sprintf("%s left  [%s]  %s", formatRemaining(total-elapsed), countdownBar(elapsed, total, bar), listed)
	return clampLine(line, width)
}

// clampLine cuts a line to width columns, marking where it was cut.
func clampLine(line string, width int) string {
	runes := []rune(line)
	if width < 1 || len(runes) <= width {
		return line
	}
	if width == 1 {
		return "\u2026"
	}
	return string(runes[:width-1]) + "\u2026"
}

// terminalWidth is how many columns the countdown has to stay inside, one short
// of the real width so drawing the last column never wraps on its own.
func terminalWidth(w io.Writer) int {
	const assumed = 80

	file, ok := w.(*os.File)
	if !ok {
		return assumed
	}
	width, _, err := term.GetSize(file.Fd())
	if err != nil || width < 2 {
		return assumed
	}
	return width - 1
}

// countdownBar fills as elapsed approaches total, and is always exactly width
// runes wide so redrawing it in place never leaves the tail of a longer bar.
func countdownBar(elapsed, total time.Duration, width int) string {
	if width < 1 {
		width = 1
	}
	filled := 0
	if total > 0 {
		filled = int(math.Max(0, math.Min(1, float64(elapsed)/float64(total))) * float64(width))
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatRemaining(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(math.Ceil(d.Seconds()))
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
