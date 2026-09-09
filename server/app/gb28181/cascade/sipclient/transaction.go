package sipclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// Credentials are intentionally passed only to the transport. They are never
// copied into a result or formatted by this package.
type Credentials struct {
	Username string
	Password string
}

// Transport is the transaction seam used by the per-platform actor. It keeps
// sipgo's network side effects out of transaction unit tests.
type Transport interface {
	Do(context.Context, *sip.Request) (*sip.Response, error)
	DoDigestAuth(context.Context, *sip.Request, *sip.Response, Credentials) (*sip.Response, error)
}

// SipgoTransport adapts the shared-UA sipgo client for cascade transactions.
type SipgoTransport struct {
	client *sipgo.Client
}

func NewSipgoTransport(client *sipgo.Client) (*SipgoTransport, error) {
	if client == nil {
		return nil, fmt.Errorf("cascade SIP transport client is unavailable")
	}
	return &SipgoTransport{client: client}, nil
}

func (t *SipgoTransport) Do(ctx context.Context, request *sip.Request) (*sip.Response, error) {
	if t == nil || t.client == nil {
		return nil, fmt.Errorf("cascade SIP transport client is unavailable")
	}
	return t.client.Do(ctx, request)
}

func (t *SipgoTransport) DoDigestAuth(ctx context.Context, request *sip.Request, response *sip.Response, credentials Credentials) (*sip.Response, error) {
	if t == nil || t.client == nil {
		return nil, fmt.Errorf("cascade SIP transport client is unavailable")
	}
	return t.client.DoDigestAuth(ctx, request, response, sipgo.DigestAuth{
		Username: credentials.Username,
		Password: credentials.Password,
	})
}

// KeepaliveEncoder owns the profile-aware MANSCDP wire representation.
// The SIP transaction layer deliberately does not construct standard XML.
type KeepaliveEncoder interface {
	EncodeKeepalive(protocol.Version) ([]byte, error)
}

// TransactionResult separates transport failures from a received SIP status.
// It is a transient value for the caller/actor; this package never persists it.
type TransactionResult struct {
	Response     *sip.Response
	TransportErr error
	BuildErr     error
	StatusCode   int
	Challenge    bool
	Retried      bool
	Success      bool
}

type TransactionClient struct {
	factory     *RequestFactory
	transport   Transport
	credentials Credentials
	timeout     time.Duration
}

func NewTransactionClient(factory *RequestFactory, transport Transport, credentials Credentials, timeout time.Duration) (*TransactionClient, error) {
	if factory == nil {
		return nil, fmt.Errorf("cascade SIP request factory is unavailable")
	}
	if transport == nil {
		return nil, fmt.Errorf("cascade SIP transport is unavailable")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("cascade SIP transaction timeout must be positive")
	}
	return &TransactionClient{factory: factory, transport: transport, credentials: credentials, timeout: timeout}, nil
}

func (c *TransactionClient) Register(ctx context.Context, expires int, callID string) TransactionResult {
	if c == nil || c.factory == nil {
		return TransactionResult{BuildErr: fmt.Errorf("cascade SIP transaction client is unavailable")}
	}
	request, err := c.factory.BuildRegister(expires, callID)
	if err != nil {
		return TransactionResult{BuildErr: err}
	}
	return c.execute(ctx, request)
}

func (c *TransactionClient) Logout(ctx context.Context, callID string) TransactionResult {
	return c.Register(ctx, 0, callID)
}

func (c *TransactionClient) Keepalive(ctx context.Context, callID string, encoder KeepaliveEncoder) TransactionResult {
	if c == nil || c.factory == nil {
		return TransactionResult{BuildErr: fmt.Errorf("cascade SIP transaction client is unavailable")}
	}
	if encoder == nil {
		return TransactionResult{BuildErr: fmt.Errorf("cascade keepalive encoder is unavailable")}
	}
	body, err := encoder.EncodeKeepalive(c.factory.Profile())
	if err != nil {
		return TransactionResult{BuildErr: err}
	}
	request, err := c.factory.BuildMessage(body, callID)
	if err != nil {
		return TransactionResult{BuildErr: err}
	}
	return c.execute(ctx, request)
}

func (c *TransactionClient) execute(parent context.Context, request *sip.Request) TransactionResult {
	if c == nil || c.transport == nil {
		return TransactionResult{BuildErr: fmt.Errorf("cascade SIP transaction client is unavailable")}
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	response, err := c.transport.Do(ctx, request)
	if err != nil {
		return TransactionResult{TransportErr: err}
	}
	result := transactionResult(response)
	if !ShouldRetryDigest(result.StatusCode, hasDigestChallenge(response), 0) {
		return result
	}

	result.Retried = true
	response, err = c.transport.DoDigestAuth(ctx, request, response, c.credentials)
	if err != nil {
		result.Response = nil
		result.StatusCode = 0
		result.Success = false
		result.TransportErr = err
		return result
	}
	next := transactionResult(response)
	next.Challenge = true
	next.Retried = true
	return next
}

func transactionResult(response *sip.Response) TransactionResult {
	if response == nil {
		return TransactionResult{TransportErr: fmt.Errorf("cascade SIP transport returned no response")}
	}
	return TransactionResult{
		Response:   response,
		StatusCode: int(response.StatusCode),
		Challenge:  isDigestStatus(response.StatusCode),
		Success:    isSuccessStatus(response.StatusCode),
	}
}

// ShouldRetryDigest is deliberately pure so the actor can use the same bound
// without making another network or timer decision.
func ShouldRetryDigest(statusCode int, hasChallengeHeader bool, attempts int) bool {
	return attempts == 0 && hasChallengeHeader && isDigestStatus(statusCode)
}

func isDigestStatus(statusCode int) bool {
	return statusCode == sip.StatusUnauthorized || statusCode == sip.StatusProxyAuthRequired
}

func hasDigestChallenge(response *sip.Response) bool {
	if response == nil {
		return false
	}
	var name string
	switch response.StatusCode {
	case sip.StatusUnauthorized:
		name = "WWW-Authenticate"
	case sip.StatusProxyAuthRequired:
		name = "Proxy-Authenticate"
	default:
		return false
	}
	header := response.GetHeader(name)
	return header != nil && strings.TrimSpace(header.Value()) != ""
}

func isSuccessStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

// RefreshDelay uses the engineering default of 80% of the granted expiry.
// It is pure because scheduling and fake clocks belong to the T8 actor.
func RefreshDelay(expires int) time.Duration {
	if expires <= 0 {
		return 0
	}
	seconds := int64(expires)/5*4 + int64(expires)%5*4/5
	if seconds < 1 {
		seconds = 1
	}
	const maxDurationSeconds = int64(^uint64(0)>>1) / int64(time.Second)
	if seconds > maxDurationSeconds {
		return time.Duration(^uint64(0) >> 1)
	}
	return time.Duration(seconds) * time.Second
}

func (c *TransactionClient) SendMessage(ctx context.Context, body []byte, callID string) TransactionResult {
	if c == nil || c.factory == nil {
		return TransactionResult{BuildErr: fmt.Errorf("cascade SIP transaction client is unavailable")}
	}
	request, err := c.factory.BuildMessage(body, callID)
	if err != nil {
		return TransactionResult{BuildErr: err}
	}
	return c.execute(ctx, request)
}
