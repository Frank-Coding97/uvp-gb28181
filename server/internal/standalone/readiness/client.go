package readiness

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Check verifies a fresh response from the process started by this launcher.
// The supplied client is copied so redirect policy cannot leak the request proof.
func Check(ctx context.Context, client *http.Client, baseURL, secret string, pid int) (Status, error) {
	var state Status
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !net.ParseIP(u.Hostname()).IsLoopback() || secret == "" || pid <= 0 {
		return state, errors.New("invalid local backend readiness target")
	}
	challenge, err := NewChallenge()
	if err != nil {
		return state, err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/standalone/ready"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return state, err
	}
	req.Header.Set(ChallengeHeader, challenge)
	req.Header.Set(ProofHeader, RequestProof(secret, challenge))
	if client == nil {
		client = &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	}
	localClient := *client
	localClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := localClient.Do(req)
	if err != nil {
		return state, errors.New("backend readiness request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 || !VerifyResponse(secret, challenge, body, response.Header.Get(ProofHeader)) {
		return state, errors.New("backend readiness response authentication failed")
	}
	if err = json.Unmarshal(body, &state); err != nil {
		return Status{}, errors.New("invalid backend readiness response")
	}
	if state.PID != pid {
		return Status{}, errors.New("backend readiness process mismatch")
	}
	if response.StatusCode != http.StatusOK || !state.BackendReady || !state.DatabaseReady || !state.RedisReady || !state.AuthorizationReady {
		return state, errors.New("backend dependencies are not ready")
	}
	return state, nil
}
