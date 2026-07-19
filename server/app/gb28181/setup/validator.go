package setup

import (
	"fmt"
	"net"
)

type ValidationError struct {
	Fields map[string]string `json:"fields"`
	cause  error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid SIP configuration: %d field(s)", len(e.Fields))
}

func (e *ValidationError) Unwrap() error { return e.cause }

func ValidateSIPConfigRequest(req SaveSIPConfigRequest, hasExistingPassword bool) error {
	fields := make(map[string]string)
	if !req.DeploymentMode.Valid() {
		fields["deploymentMode"] = "must be lan or public"
	}
	if !validIPv4(req.ListenIP, true) {
		fields["listenIp"] = "must be a valid IPv4 address"
	}
	if !validIPv4(req.AdvertiseIP, false) {
		fields["advertiseIp"] = "must be a concrete non-loopback IPv4 address"
	}
	if req.Port < 1 || req.Port > 65535 {
		fields["port"] = "must be between 1 and 65535"
	}
	if len(req.ServerID) != 20 || !asciiDigits(req.ServerID) {
		fields["serverId"] = "must contain exactly 20 digits"
	}
	if len(req.Domain) != 10 || !asciiDigits(req.Domain) {
		fields["domain"] = "must contain exactly 10 digits"
	}
	var cause error
	if req.Password == nil {
		if !hasExistingPassword {
			fields["password"] = "is required"
			cause = ErrSIPPasswordRequired
		}
	} else if len(*req.Password) < 6 {
		fields["password"] = "must contain at least 6 characters"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields, cause: cause}
	}
	return nil
}

func validIPv4(value string, allowWildcard bool) bool {
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil || value != ip.To4().String() {
		return false
	}
	if ip.IsUnspecified() {
		return allowWildcard
	}
	return !ip.IsLoopback()
}

func asciiDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
