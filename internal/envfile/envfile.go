package envfile

import (
	"bufio"
	"os"
	"strings"

	"github.com/neoighodaro/lid/internal/failure"
)

func Parse(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, failure.New("cannot read the env file").At(path).
			WithFix("check the env_file entry in your config, or create the file")
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseLine(scanner.Text())
		if !ok {
			continue
		}
		env[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, failure.New("cannot finish reading the env file: %v", err).At(path)
	}
	return env, nil
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")

	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)
	if unquoted, wasQuoted := unquote(value); wasQuoted {
		return key, unquoted, true
	}
	return key, stripInlineComment(value), true
}

func unquote(value string) (string, bool) {
	if len(value) < 2 {
		return value, false
	}
	quote := value[0]
	if quote != '"' && quote != '\'' {
		return value, false
	}
	if value[len(value)-1] != quote {
		return value, false
	}
	return value[1 : len(value)-1], true
}

func stripInlineComment(value string) string {
	if i := strings.Index(value, " #"); i >= 0 {
		return strings.TrimSpace(value[:i])
	}
	return value
}
