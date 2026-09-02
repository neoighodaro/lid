package update

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

const CheckCommand = "__update-check"

func RunCheck(ctx context.Context, c *Client, statePath string, now time.Time) error {
	state := LoadState(statePath)
	state.LastChecked = now

	release, err := c.LatestRelease(ctx)
	if err != nil {
		_ = SaveState(statePath, state)
		return err
	}

	state.LatestVersion = release.TagName
	return SaveState(statePath, state)
}

func Spawn(executable string) error {
	cmd := checkCommand(executable)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// checkCommand builds the background check. It runs from the root rather than
// from the project, because it outlives the shell that started it and reads
// nothing relative: a directory it held would pin that directory against
// removal, and would stop answering altogether once a sandbox dropped it from
// the cache backing its shared mount.
func checkCommand(executable string) *exec.Cmd {
	cmd := exec.Command(executable, CheckCommand)
	cmd.Dir = "/"
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}
