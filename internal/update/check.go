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
	cmd := exec.Command(executable, CheckCommand)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
