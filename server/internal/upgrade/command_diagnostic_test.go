package upgrade

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandFailurePreservesBoundedRedactedStderr(t *testing.T) {
	err := (OSCommandRunner{}).Run(context.Background(), "sh", "-c", "printf 'password=example-secret permission denied' >&2; exit 7")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("lost exit cause: %v", err)
	}
	if !strings.Contains(err.Error(), "permission denied") || strings.Contains(err.Error(), "example-secret") {
		t.Fatalf("unsafe or missing diagnostic: %v", err)
	}
	err = (OSCommandRunner{}).Run(context.Background(), "sh", "-c", "head -c 20000 /dev/zero | tr '\\000' x >&2; printf ' tail failure' >&2; exit 1")
	if len(err.Error()) > 10000 || !strings.Contains(err.Error(), "tail failure") {
		t.Fatalf("unbounded or missing tail: %d", len(err.Error()))
	}
}
