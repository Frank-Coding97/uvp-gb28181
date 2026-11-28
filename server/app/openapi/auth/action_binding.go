package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// bindActionRequest keeps the transport bytes immutable for HMAC verification.
// Only validated target fields are projected into domain parameters; the
// remaining JSON/query is still checked by the existing business adapters.
func bindActionRequest(q *gatewayRequest) error {
	if q.bound {
		return nil
	}
	operation, ok := contractOperation(q.scope)
	if !ok || q.method != operation.Method {
		return ErrDenied
	}
	params := make(map[string]string, len(operation.TargetParameters))
	if q.method == http.MethodGet {
		if len(q.body) != 0 {
			return ErrDenied
		}
		values, err := url.ParseQuery(q.rawQuery)
		if err != nil {
			return ErrDenied
		}
		allowed := make(map[string]bool, len(operation.QueryAllowlist))
		for _, name := range operation.QueryAllowlist {
			allowed[name] = true
		}
		for name, items := range values {
			if !allowed[name] || len(items) != 1 {
				return ErrDenied
			}
		}
		for _, name := range operation.TargetParameters {
			value := values.Get(name)
			if !validActionTarget(name, value) {
				return ErrDenied
			}
			params[name] = value
			values.Del(name)
		}
		q.businessQuery = values.Encode()
	} else {
		if q.rawQuery != "" || len(q.body) == 0 || validateJSONBody(q.body) != nil || validateCatalogObject(operation, q.body) != nil {
			return ErrDenied
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(q.body, &fields) != nil || fields == nil {
			return ErrDenied
		}
		for _, name := range operation.TargetParameters {
			var value string
			if name == "trackId" {
				var number json.Number
				if json.Unmarshal(fields[name], &number) != nil {
					return ErrDenied
				}
				value = number.String()
			} else if json.Unmarshal(fields[name], &value) != nil {
				return ErrDenied
			}
			if !validActionTarget(name, value) {
				return ErrDenied
			}
			params[name] = value
			delete(fields, name)
		}
		if q.scope == "device:record-query" {
			pagination := url.Values{}
			if raw, ok := fields["cursor"]; ok {
				var cursor string
				if json.Unmarshal(raw, &cursor) != nil || cursor == "" || !utf8.ValidString(cursor) || len(cursor) > 256 {
					return ErrDenied
				}
				pagination.Set("cursor", cursor)
				delete(fields, "cursor")
			}
			if raw, ok := fields["pageSize"]; ok {
				var size int
				if json.Unmarshal(raw, &size) != nil || size < 1 || size > 100 {
					return ErrDenied
				}
				pagination.Set("pageSize", strconv.Itoa(size))
				delete(fields, "pageSize")
			}
			q.businessQuery = pagination.Encode()
			if pagination.Has("cursor") {
				if len(fields) != 0 {
					return ErrDenied
				}
			} else if len(fields["startTime"]) == 0 || len(fields["endTime"]) == 0 || string(fields["startTime"]) == "null" || string(fields["endTime"]) == "null" {
				return ErrDenied
			}
		}
		q.businessBody, _ = json.Marshal(fields)
		if q.scope == "device:record-query" && len(fields) == 0 {
			q.businessBody = nil
		}
		if operation.BusinessRequestSchema == "{}" && !isPTZRequestScope(q.scope) {
			q.businessBody = nil
		}
	}
	q.params = params
	q.deviceID = params["deviceId"]
	q.channelID = params["channelId"]
	q.presetID = params["presetId"]
	q.trackID = params["trackId"]
	q.operationID = params["operationId"]
	q.bound = true
	return nil
}

var actionPatterns = map[string]*regexp.Regexp{
	"deviceId":       regexp.MustCompile(`^[0-9]{20}$`),
	"channelId":      regexp.MustCompile(`^[0-9]{20}$`),
	"presetId":       regexp.MustCompile(`^[0-9]{1,3}$`),
	"fileId":         regexp.MustCompile(`^[1-9][0-9]{0,19}$`),
	"alarmId":        regexp.MustCompile(`^[1-9][0-9]{0,19}$`),
	"snapshotId":     regexp.MustCompile(`^[1-9][0-9]{0,19}$`),
	"subscriptionId": regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`),
	"fileName":       regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`),
}

func validActionTarget(name, value string) bool {
	if pattern := actionPatterns[name]; pattern != nil {
		return pattern.MatchString(value)
	}
	if name == "trackId" {
		n, err := strconv.Atoi(value)
		return err == nil && n >= 0 && n <= 255 && strconv.Itoa(n) == value
	}
	return value != "" && utf8.ValidString(value) && len(value) <= 64 && !strings.ContainsAny(value, "/\\\r\n\x00")
}
func dispatchBody(q gatewayRequest) []byte {
	if q.bound {
		return q.businessBody
	}
	return q.body
}
func dispatchQuery(q gatewayRequest) string {
	if q.bound {
		return q.businessQuery
	}
	return q.rawQuery
}
