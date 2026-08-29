package management

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxMediaIdentityFieldLength is deliberately bounded before an identity
	// reaches a ZLM client or a database key. The limit is in Unicode code
	// points, so a Chinese stream name is not penalised for its UTF-8 width.
	MaxMediaIdentityFieldLength = 255

	// DefaultPageSize and MaxPageSize are shared by all management list APIs.
	DefaultPageSize = 20
	MaxPageSize     = 100

	// MaxResponseItems bounds a list returned by a ZLM API that has no native
	// pagination. A truncated result must be marked explicitly by Page.
	MaxResponseItems = 1000
)

// MediaIdentity is the complete ZLMediaKit media tuple. App and Stream are
// intentionally not URL path segments: they may contain slashes, spaces and
// Unicode and must be sent as query/body values after validation.
type MediaIdentity struct {
	Schema string `json:"schema"`
	Vhost  string `json:"vhost"`
	App    string `json:"app"`
	Stream string `json:"stream"`
}

// Validate checks the wire identity without normalising any value. In
// particular, leading/trailing spaces in a non-empty app, vhost or stream are
// preserved because they are part of the identity supplied by ZLM.
func (m MediaIdentity) Validate() error {
	fields := make(map[string]string)
	validateIdentityField(fields, "schema", m.Schema)
	validateIdentityField(fields, "vhost", m.Vhost)
	validateIdentityField(fields, "app", m.App)
	validateIdentityField(fields, "stream", m.Stream)

	if _, ok := fields["schema"]; !ok && !validSchema(m.Schema) {
		fields["schema"] = "must be a valid media schema"
	}
	if len(fields) == 0 {
		return nil
	}
	return newValidationError(fields)
}

// QueryValues returns an URL-encoded representation suitable for query
// parameters. It does not build a path and does not mutate the identity.
// Callers that accept external input should call Validate first.
func (m MediaIdentity) QueryValues() url.Values {
	values := make(url.Values, 4)
	values.Set("schema", m.Schema)
	values.Set("vhost", m.Vhost)
	values.Set("app", m.App)
	values.Set("stream", m.Stream)
	return values
}

// IsZero reports whether no identity component was supplied.
func (m MediaIdentity) IsZero() bool {
	return m == MediaIdentity{}
}

func validateIdentityField(fields map[string]string, name, value string) {
	if value == "" || strings.TrimSpace(value) == "" {
		fields[name] = "must not be empty"
		return
	}
	if !utf8.ValidString(value) {
		fields[name] = "must be valid UTF-8"
		return
	}
	if utf8.RuneCountInString(value) > MaxMediaIdentityFieldLength {
		fields[name] = "exceeds the maximum length"
		return
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			fields[name] = "must not contain control characters"
			return
		}
	}
}

func validSchema(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > MaxMediaIdentityFieldLength {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if !isASCIILetter(r) {
				return false
			}
			continue
		}
		if !isASCIILetter(r) && !isASCIIDigit(r) && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// PageRequest is the common request shape for management lists.
type PageRequest struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// Normalize applies the fixed default and upper bound. Invalid non-positive
// values fall back to the default so a list endpoint never accidentally turns
// into an unbounded query.
func (r PageRequest) Normalize() PageRequest {
	if r.Page < 1 {
		r.Page = 1
	}
	if r.PageSize < 1 {
		r.PageSize = DefaultPageSize
	}
	if r.PageSize > MaxPageSize {
		r.PageSize = MaxPageSize
	}
	return r
}

// NormalizePageRequest is the function form for handlers that do not retain
// a PageRequest value.
func NormalizePageRequest(request PageRequest) PageRequest {
	return request.Normalize()
}

// Page is a bounded, JSON-safe list response. When Truncated is true, Total
// is the number of observed items after the response cap, not a claim about
// the complete number of resources on the ZLM node.
type Page[T any] struct {
	List      []T   `json:"list"`
	Total     int64 `json:"total"`
	Page      int   `json:"page"`
	PageSize  int   `json:"pageSize"`
	Truncated bool  `json:"truncated"`
}

// Paginate applies the shared response cap and returns one in-memory page.
func Paginate[T any](items []T, request PageRequest) Page[T] {
	return PaginateWithLimit(items, request, MaxResponseItems)
}

// PaginateWithLimit is used when an endpoint has a stricter response limit.
// A non-positive limit uses the shared cap; no endpoint can raise the global
// cap accidentally.
func PaginateWithLimit[T any](items []T, request PageRequest, responseLimit int) Page[T] {
	if responseLimit < 1 || responseLimit > MaxResponseItems {
		responseLimit = MaxResponseItems
	}
	normalized := request.Normalize()
	truncated := len(items) > responseLimit
	if truncated {
		items = items[:responseLimit]
	}

	page := Page[T]{
		List:      make([]T, 0),
		Total:     int64(len(items)),
		Page:      normalized.Page,
		PageSize:  normalized.PageSize,
		Truncated: truncated,
	}
	start, ok := pageOffset(normalized.Page, normalized.PageSize)
	if !ok || start >= len(items) {
		return page
	}
	end := start + normalized.PageSize
	if end > len(items) {
		end = len(items)
	}
	page.List = items[start:end]
	return page
}

func pageOffset(page, pageSize int) (int, bool) {
	if page < 1 || pageSize < 1 {
		return 0, false
	}
	// Avoid integer overflow for a hostile page number. A page beyond the
	// observed bounded list is simply empty.
	if page-1 > int(^uint(0)>>1)/pageSize {
		return 0, false
	}
	return (page - 1) * pageSize, true
}
