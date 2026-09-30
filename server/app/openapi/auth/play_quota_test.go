package auth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlayQuotaFailureIsRetryable429(t *testing.T) {
	response := playFailure("request-1", ErrPlayQuotaExceeded)
	require.Equal(t, http.StatusTooManyRequests, response.status)
	require.Equal(t, "QUOTA_EXCEEDED", response.code)
	require.Positive(t, response.retryAfter)
	require.NotEqual(t, http.StatusServiceUnavailable, response.status)
}

func TestPlayFailureAuditReasonMatchesResponse(t *testing.T) {
	for _, err := range []error{ErrPlayNotFound, ErrPlayInvalid, ErrPlayConflict, ErrPlayQuotaExceeded, ErrPlayUnavailable} {
		response := playFailure("request-1", err)
		require.Equal(t, response.code, responseAuditCode(response, "SERVICE_UNAVAILABLE"))
	}
}
