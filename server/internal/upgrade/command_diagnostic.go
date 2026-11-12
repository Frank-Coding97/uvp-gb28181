package upgrade

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"uvplatform.com/uvp-gb28181/app/utils/logging"
)

// Keep only a bounded stderr tail. Successful startup output can include
// one-time credentials and must never be copied into task diagnostics.
type commandErrorTail struct{ data []byte }

func (b *commandErrorTail) Write(p []byte) (int, error) {
	const limit = 8 << 10
	n := len(p)
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		if excess := len(b.data) + len(p) - limit; excess > 0 {
			b.data = b.data[excess:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func runCommandWithDiagnostic(cmd *exec.Cmd) error {
	var stderr commandErrorTail
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(logging.SanitizeErrorDetail(string(stderr.data)))
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}
