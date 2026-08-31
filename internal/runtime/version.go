package runtime

import "github.com/neoighodaro/lid/internal/failure"

func requireVersion(name, version string) error {
	if version == "" {
		return failure.New("the %s runtime has no version", name).
			WithFix("pin one, like '%s: \"1.2\"'", name)
	}
	return nil
}

func rejectFloating(name, version string, floating ...string) error {
	for _, f := range floating {
		if version == f {
			return failure.New("%s has no %q version", name, f).
				WithFix("pin an exact version instead")
		}
	}
	return nil
}
