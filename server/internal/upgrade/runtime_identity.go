package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	RuntimeIdentityPath        = "/internal/updater/readiness"
	maxRuntimeIdentityPayload  = 8 << 10
	defaultRuntimeProbeTimeout = 180 * time.Second
)

// RuntimeIdentity is the candidate process identity observed by the updater.
// It binds the live process to the task that put it into candidate mode.
type RuntimeIdentity struct {
	Version         string `json:"version"`
	BuildID         string `json:"build_id"`
	TaskID          string `json:"task_id"`
	DatabaseReady   bool   `json:"database_ready"`
	MigrationDigest string `json:"migration_digest,omitempty"`
	DatabaseDialect string `json:"database_dialect,omitempty"`
}

// RuntimeIdentityProbe only talks to an administrator-registered loopback
// endpoint. URL validation prevents release data from selecting its target.
type RuntimeIdentityProbe struct {
	URL      string
	Client   *http.Client
	Attempts int
	Interval time.Duration
	Timeout  time.Duration
}

// Validate requires consecutive matching responses. A listening port alone is
// never sufficient to admit a candidate release.
func (p RuntimeIdentityProbe) Validate(ctx context.Context, expected RuntimeIdentity) error {
	if err := validateExpectedRuntimeIdentity(expected); err != nil {
		return err
	}
	endpoint, err := parseRuntimeIdentityURL(p.URL)
	if err != nil {
		return err
	}
	client := runtimeIdentityHTTPClient(p.Client)
	return validateRuntimeIdentityResponses(ctx, expected, p.Attempts, p.Interval, p.Timeout, func(ctx context.Context) (RuntimeIdentity, error) {
		return p.fetch(ctx, client, endpoint)
	})
}

type runtimeIdentityFetcher func(context.Context) (RuntimeIdentity, error)

// validateRuntimeIdentityResponses is shared by the host loopback probe and
// the Compose in-container command. Transient startup failures reset the
// consecutive-success count, while a different candidate is never retried.
func validateRuntimeIdentityResponses(ctx context.Context, expected RuntimeIdentity, requestedAttempts int, requestedInterval, requestedTimeout time.Duration, fetch runtimeIdentityFetcher) error {
	if fetch == nil {
		return errors.New("candidate runtime identity fetcher is required")
	}
	attempts := requestedAttempts
	if attempts <= 0 {
		attempts = 3
	}
	interval := requestedInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	timeout := requestedTimeout
	if timeout <= 0 {
		timeout = defaultRuntimeProbeTimeout
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	matching := 0
	var lastErr error
	for {
		actual, err := fetch(probeCtx)
		if err != nil {
			matching = 0
			lastErr = err
		} else if err := compareRuntimeIdentity(expected, actual); err != nil {
			var mismatch *runtimeIdentityMismatchError
			if errors.As(err, &mismatch) {
				return fmt.Errorf("candidate runtime identity: %w", err)
			}
			matching = 0
			lastErr = err
		} else {
			matching++
			if matching >= attempts {
				return nil
			}
		}

		if probeCtx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("candidate runtime identity did not produce %d consecutive matching responses within %s: %w", attempts, timeout, lastErr)
		}
		timer := time.NewTimer(interval)
		select {
		case <-probeCtx.Done():
			timer.Stop()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("candidate runtime identity did not produce %d consecutive matching responses within %s: %w", attempts, timeout, lastErr)
		case <-timer.C:
		}
	}
}

func validateExpectedRuntimeIdentity(identity RuntimeIdentity) error {
	if strings.TrimSpace(identity.Version) == "" || strings.TrimSpace(identity.BuildID) == "" {
		return errors.New("candidate runtime version and build ID are required")
	}
	if !validTaskID(identity.TaskID) {
		return errors.New("candidate runtime task ID is invalid")
	}
	if !identity.DatabaseReady {
		return errors.New("candidate runtime database readiness is required")
	}
	return nil
}

func parseRuntimeIdentityURL(raw string) (*url.URL, error) {
	endpoint, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("registered readiness URL is invalid: %w", err)
	}
	if endpoint.Scheme != "http" || endpoint.Opaque != "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return nil, errors.New("registered readiness URL must be a plain loopback HTTP URL")
	}
	if endpoint.Path != RuntimeIdentityPath || endpoint.RawPath != "" {
		return nil, fmt.Errorf("registered readiness URL path must be %s", RuntimeIdentityPath)
	}
	host := endpoint.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		return nil, errors.New("registered readiness URL host must be numeric loopback")
	}
	port, err := strconv.Atoi(endpoint.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("registered readiness URL must include a valid port")
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, errors.New("registered readiness URL host must be loopback")
	}
	return endpoint, nil
}

func runtimeIdentityHTTPClient(source *http.Client) *http.Client {
	if source == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		return &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	client := *source
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func (p RuntimeIdentityProbe) fetch(ctx context.Context, client *http.Client, endpoint *url.URL) (RuntimeIdentity, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return RuntimeIdentity{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return RuntimeIdentity{}, fmt.Errorf("readiness endpoint returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRuntimeIdentityPayload+1))
	if err != nil {
		return RuntimeIdentity{}, err
	}
	if len(body) > maxRuntimeIdentityPayload {
		return RuntimeIdentity{}, errors.New("readiness response exceeds size limit")
	}
	var identity RuntimeIdentity
	if err := decodeStrictJSON(body, &identity); err != nil {
		return RuntimeIdentity{}, fmt.Errorf("decode readiness response: %w", err)
	}
	return identity, nil
}

type runtimeIdentityMismatchError struct{ message string }

func (e *runtimeIdentityMismatchError) Error() string { return e.message }

func runtimeIdentityMismatch(format string, args ...any) error {
	return &runtimeIdentityMismatchError{message: fmt.Sprintf(format, args...)}
}

func compareRuntimeIdentity(expected, actual RuntimeIdentity) error {
	if actual.Version != expected.Version {
		return runtimeIdentityMismatch("runtime version %q does not match task %q", actual.Version, expected.Version)
	}
	if actual.BuildID != expected.BuildID {
		return runtimeIdentityMismatch("runtime build ID %q does not match task %q", actual.BuildID, expected.BuildID)
	}
	if actual.TaskID != expected.TaskID {
		return runtimeIdentityMismatch("runtime task ID %q does not match candidate %q", actual.TaskID, expected.TaskID)
	}
	if !actual.DatabaseReady {
		return errors.New("runtime database is not ready")
	}
	if expected.MigrationDigest != "" && actual.MigrationDigest != expected.MigrationDigest {
		return runtimeIdentityMismatch("runtime migration digest does not match expected target")
	}
	if expected.DatabaseDialect != "" && actual.DatabaseDialect != expected.DatabaseDialect {
		return runtimeIdentityMismatch("runtime database dialect does not match expected target")
	}
	return nil
}
