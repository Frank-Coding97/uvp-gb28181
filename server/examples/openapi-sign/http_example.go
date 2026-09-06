package openapisign

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPTimeout = 10 * time.Second

var errHTTPExampleCall = errors.New("OpenAPI HTTP example call failed")

// HTTPResult is the response returned by the minimal server-to-server example.
type HTTPResult struct {
	StatusCode int
	Body       []byte
	Attempts   int
}

// Get performs one signed GET and retries at most once for a 429, 503, or
// transport timeout. Every attempt is signed with a fresh timestamp and nonce.
func Get(ctx context.Context, client *http.Client, baseURL, path, rawQuery, accessKey, secretKey, audience string) (HTTPResult, error) {
	return newHTTPExampleCaller(client).get(ctx, baseURL, path, rawQuery, accessKey, secretKey, audience)
}

// Post performs one signed JSON POST. It never retries because a transport
// failure leaves the result unknown and a response status may be application
// state rather than a safe duplicate request.
func Post(ctx context.Context, client *http.Client, baseURL, path, rawQuery, accessKey, secretKey, audience string, body []byte) (HTTPResult, error) {
	return newHTTPExampleCaller(client).post(ctx, baseURL, path, rawQuery, accessKey, secretKey, audience, body)
}

type httpExampleCaller struct {
	client *http.Client
	now    func() time.Time
	nonce  func() (string, error)
}

func newHTTPExampleCaller(client *http.Client) *httpExampleCaller {
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &httpExampleCaller{
		client: &copy,
		now:    time.Now,
		nonce:  randomNonce,
	}
}

func (c *httpExampleCaller) get(ctx context.Context, baseURL, path, rawQuery, accessKey, secretKey, audience string) (HTTPResult, error) {
	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		request, err := c.signedRequest(ctx, http.MethodGet, baseURL, path, rawQuery, accessKey, secretKey, audience, nil)
		if err != nil {
			return HTTPResult{Attempts: attempt}, err
		}
		response, body, err := c.do(request)
		if err != nil {
			if attempt < maxAttempts && isRetryableTimeout(err) && ctx.Err() == nil {
				continue
			}
			return HTTPResult{Attempts: attempt}, errHTTPExampleCall
		}
		result := HTTPResult{StatusCode: response.StatusCode, Body: body, Attempts: attempt}
		if attempt < maxAttempts && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable) {
			continue
		}
		return result, nil
	}
	return HTTPResult{Attempts: maxAttempts}, errHTTPExampleCall
}

func (c *httpExampleCaller) post(ctx context.Context, baseURL, path, rawQuery, accessKey, secretKey, audience string, body []byte) (HTTPResult, error) {
	request, err := c.signedRequest(ctx, http.MethodPost, baseURL, path, rawQuery, accessKey, secretKey, audience, body)
	if err != nil {
		return HTTPResult{Attempts: 1}, err
	}
	response, responseBody, err := c.do(request)
	if err != nil {
		return HTTPResult{Attempts: 1}, errHTTPExampleCall
	}
	return HTTPResult{StatusCode: response.StatusCode, Body: responseBody, Attempts: 1}, nil
}

func (c *httpExampleCaller) signedRequest(ctx context.Context, method, baseURL, path, rawQuery, accessKey, secretKey, audience string, body []byte) (*http.Request, error) {
	if ctx == nil || c == nil || c.client == nil || c.now == nil || c.nonce == nil {
		return nil, errInvalidInput
	}
	requestURL, err := exampleRequestURL(baseURL, path, rawQuery)
	if err != nil {
		return nil, errInvalidInput
	}
	nonce, err := c.nonce()
	if err != nil {
		return nil, errHTTPExampleCall
	}
	contentType := ""
	if method == http.MethodPost {
		contentType = "application/json"
	}
	input := Request{
		Method:      method,
		Path:        path,
		RawQuery:    rawQuery,
		ContentType: contentType,
		Body:        body,
		AccessKey:   accessKey,
		Timestamp:   strconv.FormatInt(c.now().Unix(), 10),
		Nonce:       nonce,
		Audience:    audience,
	}
	signature, err := Sign(input, secretKey)
	if err != nil {
		return nil, errInvalidInput
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, errInvalidInput
	}
	request.Header.Set("X-UVP-Sign-Version", "1")
	request.Header.Set("X-UVP-Access-Key", accessKey)
	request.Header.Set("X-UVP-Timestamp", input.Timestamp)
	request.Header.Set("X-UVP-Nonce", nonce)
	request.Header.Set("X-UVP-Signature", signature)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return request, nil
}

func (c *httpExampleCaller) do(request *http.Request) (*http.Response, []byte, error) {
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	if response.Body == nil {
		return nil, nil, errHTTPExampleCall
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, nil, readErr
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}
	return response, body, nil
}

func exampleRequestURL(baseURL, path, rawQuery string) (string, error) {
	if validatePath(path) != nil || strings.ContainsAny(path, "?#") {
		return "", errInvalidInput
	}
	if _, err := canonicalQuery(rawQuery); err != nil {
		return "", errInvalidInput
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.Opaque != "" {
		return "", errInvalidInput
	}
	if base.Path != "" && base.Path != "/" || base.RawPath != "" {
		return "", errInvalidInput
	}
	base.Path = path
	base.RawPath = ""
	base.RawQuery = rawQuery
	requestURL := base.String()
	parsed, err := url.Parse(requestURL)
	if err != nil || parsed.Path != path || parsed.RawQuery != rawQuery || parsed.Fragment != "" {
		return "", errInvalidInput
	}
	return requestURL, nil
}

func randomNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errHTTPExampleCall
	}
	return hex.EncodeToString(raw[:]), nil
}

func isRetryableTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
