package update

import (
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	DefaultRepo   = "neoighodaro/lid"
	CheckInterval = 24 * time.Hour
)

func Repo(getenv func(string) string) string {
	if repo := strings.TrimSpace(getenv("LID_UPDATE_REPO")); repo != "" {
		return repo
	}
	return DefaultRepo
}

func IsRelease(v string) bool {
	return semver.IsValid(v) && semver.Prerelease(v) == "" && semver.Build(v) == ""
}

func IsNewer(current, latest string) bool {
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}
	return semver.Compare(latest, current) > 0
}

func Suppressed(getenv func(string) string, tty bool, command string) bool {
	for _, name := range []string{"LID_NO_UPDATE_CHECK", "IS_SANDBOX", "CI"} {
		if getenv(name) != "" {
			return true
		}
	}
	if !tty {
		return true
	}
	switch command {
	case "update", CheckCommand, "version", "help":
		return true
	}
	return false
}
