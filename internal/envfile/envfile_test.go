package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func parse(t *testing.T, body string) map[string]string {
	t.Helper()
	env, err := Parse(writeEnv(t, body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return env
}

func TestParseReadsKeyValuePairs(t *testing.T) {
	env := parse(t, "FOO=bar\nBAZ=qux\n")

	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
	if env["BAZ"] != "qux" {
		t.Errorf("BAZ = %q, want %q", env["BAZ"], "qux")
	}
}

func TestParseTrimsSurroundingWhitespace(t *testing.T) {
	env := parse(t, "  FOO =  bar  \n")

	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
}

func TestParseSkipsBlankLinesAndComments(t *testing.T) {
	env := parse(t, "\n# a comment\n\n   # indented comment\nFOO=bar\n")

	if len(env) != 1 {
		t.Errorf("len(env) = %d, want 1: %v", len(env), env)
	}
}

func TestParseStripsMatchingQuotes(t *testing.T) {
	env := parse(t, "DOUBLE=\"bar baz\"\nSINGLE='bar baz'\n")

	if env["DOUBLE"] != "bar baz" {
		t.Errorf("DOUBLE = %q, want %q", env["DOUBLE"], "bar baz")
	}
	if env["SINGLE"] != "bar baz" {
		t.Errorf("SINGLE = %q, want %q", env["SINGLE"], "bar baz")
	}
}

func TestParseKeepsMismatchedQuotes(t *testing.T) {
	env := parse(t, "FOO=\"bar\n")

	if env["FOO"] != "\"bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "\"bar")
	}
}

func TestParseHonoursExportPrefix(t *testing.T) {
	env := parse(t, "export FOO=bar\n")

	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
}

func TestParseDoesNotTreatExportAsPrefixWithoutSpace(t *testing.T) {
	env := parse(t, "exported=bar\n")

	if env["exported"] != "bar" {
		t.Errorf("exported = %q, want %q", env["exported"], "bar")
	}
}

func TestParseStripsInlineCommentFromUnquotedValue(t *testing.T) {
	env := parse(t, "FOO=bar # trailing note\n")

	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
}

func TestParseKeepsHashInsideQuotedValue(t *testing.T) {
	env := parse(t, "PASSWORD=\"a#b # c\"\n")

	if env["PASSWORD"] != "a#b # c" {
		t.Errorf("PASSWORD = %q, want %q", env["PASSWORD"], "a#b # c")
	}
}

func TestParseKeepsHashWithoutLeadingSpace(t *testing.T) {
	env := parse(t, "COLOR=#ff0000\n")

	if env["COLOR"] != "#ff0000" {
		t.Errorf("COLOR = %q, want %q", env["COLOR"], "#ff0000")
	}
}

func TestParseAllowsEmptyValue(t *testing.T) {
	env := parse(t, "FOO=\n")

	value, ok := env["FOO"]
	if !ok {
		t.Fatalf("FOO missing from %v", env)
	}
	if value != "" {
		t.Errorf("FOO = %q, want empty", value)
	}
}

func TestParseSkipsEmptyKeys(t *testing.T) {
	env := parse(t, "=orphan\n   =orphan\n")

	if len(env) != 0 {
		t.Errorf("len(env) = %d, want 0: %v", len(env), env)
	}
}

func TestParseSkipsLinesWithoutSeparator(t *testing.T) {
	env := parse(t, "JUST_A_WORD\nFOO=bar\n")

	if _, ok := env["JUST_A_WORD"]; ok {
		t.Error("JUST_A_WORD should be skipped")
	}
	if env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "bar")
	}
}

func TestParseKeepsEqualsInsideValue(t *testing.T) {
	env := parse(t, "TOKEN=a=b=c\n")

	if env["TOKEN"] != "a=b=c" {
		t.Errorf("TOKEN = %q, want %q", env["TOKEN"], "a=b=c")
	}
}

func TestParseLastAssignmentWins(t *testing.T) {
	env := parse(t, "FOO=first\nFOO=second\n")

	if env["FOO"] != "second" {
		t.Errorf("FOO = %q, want %q", env["FOO"], "second")
	}
}

func TestParseErrorsWhenFileMissing(t *testing.T) {
	_, err := Parse(filepath.Join(t.TempDir(), "absent"))
	if err == nil {
		t.Fatal("Parse: want error, got nil")
	}
}
