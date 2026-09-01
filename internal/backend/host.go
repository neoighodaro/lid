package backend

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

type Host struct {
	LookPath func(name string) (string, error)
	Output   func(argv []string) ([]byte, error)
	Exec     func(argv []string, stdout, stderr io.Writer) error
	Stdout   io.Writer
	Stderr   io.Writer
}

func (h Host) exec(argv []string) error {
	stdout, stderr := h.Stdout, h.Stderr
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if h.Exec != nil {
		return h.Exec(argv, stdout, stderr)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func (h Host) lookPath(name string) (string, error) {
	if h.LookPath != nil {
		return h.LookPath(name)
	}
	return exec.LookPath(name)
}

func (h Host) output(argv []string) (string, error) {
	if h.Output != nil {
		out, err := h.Output(argv)
		return string(out), err
	}
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	return string(out), err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
