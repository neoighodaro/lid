package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neoighodaro/lid/internal/app"
	"github.com/spf13/cobra"
)

const commandDocsPath = "../../docs/commands.md"

func TestCommandDocsAreCurrent(t *testing.T) {
	root := NewRootCommand(&app.App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	want := renderCommandDocs(root)

	if os.Getenv("LID_UPDATE_DOCS") == "1" {
		if err := os.WriteFile(commandDocsPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	got, err := os.ReadFile(commandDocsPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v; run 'make docs'", filepath.Base(commandDocsPath), err)
	}
	if string(got) != want {
		t.Fatalf("docs/commands.md is out of date with the commands; run 'make docs'")
	}
}

func renderCommandDocs(root *cobra.Command) string {
	var b strings.Builder
	b.WriteString("# Commands\n\n")
	b.WriteString("<!-- Generated from the command tree by 'make docs'. Do not edit by hand. -->\n\n")
	b.WriteString("Every command reads `lid.yaml` from the current directory, or `.config/lid/lid.yaml` when there is none.\n\n")
	b.WriteString("## Global flags\n\n")
	writeFlags(&b, root.PersistentFlags().FlagUsages())

	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			if !sub.IsAvailableCommand() {
				continue
			}
			writeCommand(&b, sub)
			walk(sub)
		}
	}
	walk(root)
	return b.String()
}

func writeCommand(b *strings.Builder, cmd *cobra.Command) {
	b.WriteString("## " + cmd.CommandPath() + "\n\n")
	text := cmd.Long
	if text == "" {
		text = cmd.Short
	}
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	b.WriteString(text + "\n\n")
	b.WriteString("```\n" + cmd.UseLine() + "\n```\n\n")
	if len(cmd.Aliases) > 0 {
		b.WriteString("Aliases: `" + strings.Join(cmd.Aliases, "`, `") + "`\n\n")
	}
	if usages := cmd.NonInheritedFlags().FlagUsages(); usages != "" {
		writeFlags(b, usages)
	}
}

func writeFlags(b *strings.Builder, usages string) {
	b.WriteString("```\n" + strings.TrimRight(usages, "\n") + "\n```\n\n")
}
