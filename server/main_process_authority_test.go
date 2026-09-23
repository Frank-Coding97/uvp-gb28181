package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

func TestProcessAuthorityFailureReason(t *testing.T) {
	require.Equal(t, "already_running", processAuthorityFailureReason(
		fmt.Errorf("wrapped: %w", processauthority.ErrLocalAuthorityBusy)))
	require.Equal(t, "state_unavailable", processAuthorityFailureReason(
		fmt.Errorf("wrapped: %w", processauthority.ErrLocalAuthorityUnavailable)))
	require.Equal(t, "database_domain_mismatch", processAuthorityFailureReason(
		fmt.Errorf("wrapped: %w", processauthority.ErrProcessAuthorityDomainMismatch)))
}
