package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

// WriteRuntimeReadiness is used inside a registered Compose backend container.
// It queries that container's existing process rather than initializing a
// second database/runtime merely to inspect readiness.
func WriteRuntimeReadiness(ctx context.Context, listenAddress string, output io.Writer) error {
	if output == nil {
		return errors.New("readiness output is required")
	}
	host, port, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return err
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("invalid backend listen port")
	}
	loopback := "127.0.0.1"
	if host != "" {
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsUnspecified() && !ip.IsLoopback()) {
			return errors.New("backend does not listen on loopback")
		}
		if ip.To4() == nil {
			loopback = "::1"
		}
	}
	endpoint, err := parseRuntimeIdentityURL("http://" + net.JoinHostPort(loopback, port) + RuntimeIdentityPath)
	if err != nil {
		return err
	}
	identity, err := (RuntimeIdentityProbe{}).fetch(ctx, runtimeIdentityHTTPClient(nil), endpoint)
	if err != nil {
		return fmt.Errorf("read in-container backend readiness: %w", err)
	}
	if err := validateExpectedRuntimeIdentity(identity); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(identity)
}
