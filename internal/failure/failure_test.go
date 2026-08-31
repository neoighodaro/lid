package failure

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorReadsAsOneSentenceWhenFlattened(t *testing.T) {
	err := &Error{What: "the nono backend cannot use packages", In: "lid.yaml", Fix: "install them on your host"}

	got := err.Error()
	if !strings.Contains(got, "cannot use packages") || !strings.Contains(got, "install them on your host") {
		t.Errorf("Error() = %q, want both the problem and the fix", got)
	}
}

func TestErrorOmitsAnEmptyFix(t *testing.T) {
	err := &Error{What: "no lid.yaml found"}

	if got := err.Error(); got != "no lid.yaml found" {
		t.Errorf("Error() = %q, want just the problem", got)
	}
}

func TestErrorSurvivesWrapping(t *testing.T) {
	wrapped := fmt.Errorf("start: %w", &Error{What: "boom", Fix: "duck"})

	var target *Error
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As did not find the failure through a wrap")
	}
	if target.Fix != "duck" {
		t.Errorf("Fix = %q, want it preserved through the wrap", target.Fix)
	}
}

func TestNewBuildsAFailureFromAFormattedProblem(t *testing.T) {
	err := New("unknown backend %q", "sbxx")

	if err.What != `unknown backend "sbxx"` {
		t.Errorf("What = %q, want the formatted problem", err.What)
	}
}

func TestLocationAndFixAreChainable(t *testing.T) {
	err := New("boom").At("lid.yaml").WithFix("try %s", "again")

	if err.In != "lid.yaml" || err.Fix != "try again" {
		t.Errorf("chained builder lost data: %+v", err)
	}
}

func TestAtNamesALocationTheValidatorDidNotKnow(t *testing.T) {
	err := At(New("boom"), "lid.yaml")

	var f *Error
	if !errors.As(err, &f) {
		t.Fatalf("At lost the failure: %v", err)
	}
	if f.In != "lid.yaml" {
		t.Errorf("In = %q, want lid.yaml", f.In)
	}
}

func TestAtLeavesALocationTheErrorAlreadyKnows(t *testing.T) {
	err := At(New("boom").At(".env"), "lid.yaml")

	var f *Error
	errors.As(err, &f)
	if f.In != ".env" {
		t.Errorf("In = %q, want the location the error already carried", f.In)
	}
}

func TestAtPassesAPlainErrorThrough(t *testing.T) {
	plain := errors.New("boom")

	if got := At(plain, "lid.yaml"); got != plain {
		t.Errorf("At returned %v, want the error unchanged", got)
	}
}
