package runtime

import "fmt"

func requireVersion(name, version string) error {
	if version == "" {
		return fmt.Errorf("%s version cannot be empty", name)
	}
	return nil
}

func rejectFloating(name, version string, floating ...string) error {
	for _, f := range floating {
		if version == f {
			return fmt.Errorf("%s has no %q version: pin an exact one instead", name, f)
		}
	}
	return nil
}
