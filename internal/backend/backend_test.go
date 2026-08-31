package backend

import (
	"testing"

	"github.com/neoighodaro/lid/internal/config"
)

func TestGetReturnsTheNonoBackend(t *testing.T) {
	b := Get(config.BackendNono)
	if b == nil {
		t.Fatal("Get(nono) = nil")
	}
	if b.Name() != config.BackendNono {
		t.Errorf("Name() = %q, want %q", b.Name(), config.BackendNono)
	}
}

func TestGetUnknownBackendReturnsNil(t *testing.T) {
	if b := Get("podman"); b != nil {
		t.Errorf("Get(podman) = %v, want nil", b)
	}
}

func TestEveryRegisteredBackendIsOneConfigAccepts(t *testing.T) {
	for _, name := range Names() {
		if !contains(config.Backends, name) {
			t.Errorf("registry has %q, which config.Backends does not list", name)
		}
	}
}

func TestNamesIsSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("Names() is not sorted: %v", names)
		}
	}
}
