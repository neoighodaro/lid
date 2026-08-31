package backend

import (
	"os/exec"
	"strings"
)

type Host struct {
	LookPath func(name string) (string, error)
	Output   func(argv []string) ([]byte, error)
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
