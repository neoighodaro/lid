package runtime

import "fmt"

func requireVersion(name, version string) error {
	if version == "" {
		return fmt.Errorf("%s version cannot be empty", name)
	}
	return nil
}
