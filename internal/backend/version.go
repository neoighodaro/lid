package backend

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/neoighodaro/lid/internal/failure"
)

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

type minVersion struct {
	tool    string
	command string
	min     string
	why     string
	which   string
}

func checkMinVersion(v minVersion, output string) error {
	found := versionRe.FindStringSubmatch(output)
	if found == nil {
		return failure.New("cannot read a version from '%s', which printed %q", v.command, firstLine(output)).
			WithFix("check that the %s on your PATH is %s", v.tool, v.which)
	}

	want := versionRe.FindStringSubmatch(v.min)
	for i := 1; i <= 3; i++ {
		got, _ := strconv.Atoi(found[i])
		min, _ := strconv.Atoi(want[i])
		if got > min {
			return nil
		}
		if got < min {
			return failure.New("%s %s is older than the %s %s", v.tool, strings.Join(found[1:4], "."), v.min, v.why).
				WithFix("update %s, then run this again", v.tool)
		}
	}
	return nil
}
