package sipclient

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
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

type AudioDialog interface {
	WaitAnswer(context.Context) error
	AnswerSDP() []byte
	Ack(context.Context) error
	Bye(context.Context) error
	Close() error
	MatchesRequest(*sip.Request) bool
	ReadBye(*sip.Request, sip.ServerTransaction) error
}

type AudioDialogTransport interface {
	WriteAudioDialog(context.Context, *sip.Request, Credentials) (AudioDialog, error)
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

func (t *SipgoTransport) WriteAudioDialog(ctx context.Context, request *sip.Request, credentials Credentials) (AudioDialog, error) {
	if t == nil || t.client == nil || request == nil || request.Contact() == nil {
		return nil, fmt.Errorf("cascade audio dialog is unavailable")
	}
	cache := sipgo.NewDialogClientCache(t.client, *request.Contact())
	session, err := cache.WriteInvite(ctx, request)
	if err != nil {
		return nil, err
	}
	return &sipgoAudioDialog{session: session, credentials: credentials}, nil
}

type sipgoAudioDialog struct {
	session     *sipgo.DialogClientSession
	credentials Credentials
}

func (d *sipgoAudioDialog) WaitAnswer(ctx context.Context) error {
	return d.session.WaitAnswer(ctx, sipgo.AnswerOptions{Username: d.credentials.Username, Password: d.credentials.Password})
}
func (d *sipgoAudioDialog) AnswerSDP() []byte {
	if d == nil || d.session == nil || d.session.InviteResponse == nil || !d.session.InviteResponse.IsSuccess() {
		return nil
	}
	return append([]byte(nil), d.session.InviteResponse.Body()...)
}
func (d *sipgoAudioDialog) Ack(ctx context.Context) error { return d.session.Ack(ctx) }
func (d *sipgoAudioDialog) Bye(ctx context.Context) error { return d.session.Bye(ctx) }
func (d *sipgoAudioDialog) Close() error                  { return d.session.Close() }
func (d *sipgoAudioDialog) MatchesRequest(request *sip.Request) bool {
	if d == nil || d.session == nil || request == nil || request.Method != sip.BYE || request.CallID() == nil || request.CSeq() == nil || request.CSeq().MethodName != sip.BYE || d.session.InviteRequest.CallID() == nil || d.session.InviteResponse == nil {
		return false
	}
	from, to := request.From(), request.To()
	inviteFrom, responseTo := d.session.InviteRequest.From(), d.session.InviteResponse.To()
	if from == nil || to == nil || inviteFrom == nil || responseTo == nil || request.CallID().Value() != d.session.InviteRequest.CallID().Value() {
		return false
	}
	fromTag, _ := from.Params.Get("tag")
	inviteFromTag, _ := inviteFrom.Params.Get("tag")
	toTag, _ := to.Params.Get("tag")
	remoteTag, _ := responseTo.Params.Get("tag")
	return fromTag != "" && toTag != "" && fromTag == remoteTag && toTag == inviteFromTag
}
func (d *sipgoAudioDialog) ReadBye(request *sip.Request, tx sip.ServerTransaction) error {
	if !d.MatchesRequest(request) {
		return fmt.Errorf("cascade audio BYE dialog mismatch")
	}
	return d.session.ReadBye(request, tx)
}

// KeepaliveEncoder owns the profile-aware MANSCDP wire representation.
// The SIP transaction layer deliberately does not construct standard XML.
type KeepaliveEncoder interface {
	EncodeKeepalive(protocol.Version) ([]byte, error)
}

// TransactionResult separates transport failures from a received SIP status.
// It is a transient value for the caller/actor; this package never persists it.
type TransactionResult struct {
	Response            *sip.Response
	TransportErr        error
	BuildErr            error
	StatusCode          int
	Challenge           bool
	Retried             bool
	Success             bool
	ServerDate          time.Time
	ServerDateFound     bool
	ServerDateErr       error
	GrantedExpires      int
	GrantedExpiresFound bool
	GrantedExpiresErr   error
	// ReportedGBVersion is read only from the final successful REGISTER
	// response. An unknown or malformed value is retained as an error and never
	// implicitly changes the active profile.
	ReportedGBVersion      string
	ReportedGBVersionFound bool
	NegotiatedVersion      protocol.Version
	NegotiatedVersionFound bool
	NegotiatedVersionErr   error
}

type TransactionClient struct {
	factory     *RequestFactory
	transport   Transport
	credentials Credentials
	timeout     time.Duration
}

func (c *TransactionClient) SetProfile(profile protocol.Version) error {
	if c == nil || c.factory == nil {
		return fmt.Errorf("cascade SIP request factory is unavailable")
	}
	return c.factory.SetProfile(profile)
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
	seen := map[string]struct{}{request.Recipient.String(): {}}
	for redirects := 0; ; redirects++ {
		result := c.execute(ctx, request)
		if result.StatusCode != sip.StatusMovedTemporarily {
			return result
		}
		if redirects >= 3 {
			result.BuildErr = fmt.Errorf("cascade REGISTER redirect limit exceeded")
			return result
		}
		target, redirectErr := c.factory.registerRedirectTarget(result.Response)
		if redirectErr != nil {
			result.BuildErr = redirectErr
			return result
		}
		key := strings.ToLower(target.String())
		if _, exists := seen[key]; exists {
			result.BuildErr = fmt.Errorf("cascade REGISTER redirect loop")
			return result
		}
		seen[key] = struct{}{}
		request, err = c.factory.buildRedirectRegister(expires, callID, target)
		if err != nil {
			result.BuildErr = err
			return result
		}
	}
}

func (c *TransactionClient) Logout(ctx context.Context, callID string) TransactionResult {
	return c.Register(ctx, 0, callID)
}

func (f *RequestFactory) registerRedirectTarget(response *sip.Response) (sip.Uri, error) {
	if f == nil || response == nil {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect response is unavailable")
	}
	contacts := response.GetHeaders("Contact")
	if len(contacts) != 1 {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect must contain one Contact")
	}
	contact, ok := contacts[0].(*sip.ContactHeader)
	if !ok {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect Contact is invalid")
	}
	target := contact.Address
	if !strings.EqualFold(target.Scheme, "sip") || target.Wildcard || target.User != f.identity.UpstreamServerID || target.Password != "" || target.Host == "" || target.UriParams.Length() != 0 || target.Headers.Length() != 0 {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect Contact is not allowed")
	}
	if !strings.EqualFold(target.Host, f.identity.UpstreamDomain) && !strings.EqualFold(target.Host, f.identity.Host) {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect host is outside the configured upstream trust")
	}
	if target.Port < 0 || target.Port > 65535 || target.Port == 0 && target.Host == "" {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect port is invalid")
	}
	if target.Port == 0 {
		target.Port = 5060
	}
	if !validHostToken(target.Host) {
		return sip.Uri{}, fmt.Errorf("cascade REGISTER redirect host is invalid")
	}
	return target, nil
}

func (f *RequestFactory) buildRedirectRegister(expires int, callID string, target sip.Uri) (*sip.Request, error) {
	request, err := f.BuildRegister(expires, callID)
	if err != nil {
		return nil, err
	}
	request.Recipient = *target.Clone()
	request.SetDestination(net.JoinHostPort(target.Host, strconv.Itoa(target.Port)))
	return request, nil
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
	if request.Method == sip.REGISTER && result.Success {
		annotateRegisterVersion(&result, response)
		annotateRegisterDate(&result, response)
		result.GrantedExpires, result.GrantedExpiresFound, result.GrantedExpiresErr = grantedRegisterExpires(request, response)
		if result.GrantedExpiresErr != nil {
			result.Success = false
		}
	}
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
	if request.Method == sip.REGISTER && next.Success {
		annotateRegisterVersion(&next, response)
		annotateRegisterDate(&next, response)
		next.GrantedExpires, next.GrantedExpiresFound, next.GrantedExpiresErr = grantedRegisterExpires(request, response)
		if next.GrantedExpiresErr != nil {
			next.Success = false
		}
	}
	next.Challenge = true
	next.Retried = true
	return next
}

func annotateRegisterDate(result *TransactionResult, response *sip.Response) {
	if result == nil || response == nil {
		return
	}
	headers := response.GetHeaders("Date")
	if len(headers) == 0 {
		return
	}
	result.ServerDateFound = true
	if len(headers) != 1 {
		result.ServerDateErr = fmt.Errorf("ambiguous REGISTER Date header")
		return
	}
	parsed, err := time.Parse(time.RFC1123, strings.TrimSpace(headers[0].Value()))
	if err != nil {
		result.ServerDateErr = fmt.Errorf("invalid REGISTER Date header: %w", err)
		return
	}
	zone, offset := parsed.Zone()
	if zone != "GMT" || offset != 0 {
		result.ServerDateErr = fmt.Errorf("REGISTER Date must use GMT")
		return
	}
	result.ServerDate = parsed.UTC()
}

func annotateRegisterVersion(result *TransactionResult, response *sip.Response) {
	if result == nil || response == nil {
		return
	}
	header := response.GetHeader("X-GB-Ver")
	if header == nil {
		return
	}
	raw := strings.TrimSpace(header.Value())
	result.ReportedGBVersion = raw
	result.ReportedGBVersionFound = true
	parsed := protocol.ResolveAdvertisedVersion(raw)
	if parsed.WarningCode != protocol.WarningNone {
		result.NegotiatedVersionErr = fmt.Errorf("unsupported X-GB-Ver %q: %s", raw, parsed.WarningCode)
		return
	}
	result.NegotiatedVersion = parsed.Version
	result.NegotiatedVersionFound = true
}

func grantedRegisterExpires(request *sip.Request, response *sip.Response) (int, bool, error) {
	if request == nil || response == nil {
		return 0, false, nil
	}
	if response.Contact() != nil && request.Contact() != nil && sameContactBinding(request.Contact(), response.Contact()) {
		if raw, ok := response.Contact().Params.Get("expires"); ok {
			value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
			if err != nil {
				return 0, true, fmt.Errorf("invalid REGISTER Contact expires")
			}
			return int(value), true, nil
		}
	}
	if header := response.GetHeader("Expires"); header != nil {
		value, err := strconv.ParseUint(strings.TrimSpace(header.Value()), 10, 32)
		if err != nil {
			return 0, true, fmt.Errorf("invalid REGISTER Expires")
		}
		return int(value), true, nil
	}
	return 0, false, nil
}

func sameContactBinding(want, got *sip.ContactHeader) bool {
	if want == nil || got == nil {
		return false
	}
	return strings.EqualFold(want.Address.User, got.Address.User) &&
		strings.EqualFold(normalizeContactHost(want.Address.Host), normalizeContactHost(got.Address.Host)) &&
		contactPort(want.Address) == contactPort(got.Address)
}

func normalizeContactHost(host string) string {
	if parsed := net.ParseIP(strings.Trim(host, "[]")); parsed != nil {
		return parsed.String()
	}
	return strings.ToLower(strings.TrimSpace(host))
}

func contactPort(uri sip.Uri) int {
	if uri.Port > 0 {
		return uri.Port
	}
	return 5060
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

// MessageFitsUDP includes the SIP envelope and headroom for transaction-added headers.
func (c *TransactionClient) MessageFitsUDP(body []byte) bool {
	request, err := c.factory.BuildMessage(body, "catalog-18446744073709551615-9223372036854775807")
	return err == nil && len(request.String())+96 <= sip.UDPMTUSize-200
}
