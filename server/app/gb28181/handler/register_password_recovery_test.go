package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/device"
	gbsecurity "uvplatform.cn/uvp-gb28181/app/gb28181/security"
)

func TestRegisterRepeatedWrongPasswordDoesNotBlockCorrectedPassword(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP"} {
		t.Run(transport, func(t *testing.T) {
			runtime := gbsecurity.NewRuntime(gbsecurity.DefaultPolicy(), nil, nil, []byte("password-recovery-test"))
			handler := NewRegisterHandler(securityTestCfg())
			handler.SetSecurity(runtime)
			registerCalls := 0
			handler.handleRegister = func(context.Context, device.RegisterInfo, int) (bool, error) {
				registerCalls++
				return false, nil
			}
			nonce, err := runtime.IssueNonceForSource("198.51.100.23")
			require.NoError(t, err)
			for i := 1; i <= 100; i++ {
				request := authorizedRegisterRequest(t, nonce, "mistyped-password")
				request.SetTransport(transport)
				setRegisterCSeq(request, i)
				tx := &captureServerTransaction{}
				handler.Handle(request, tx)
				require.Equal(t, sip.StatusUnauthorized, tx.response.StatusCode)
			}
			require.Zero(t, registerCalls)
			require.False(t, runtime.Admission().IsBanned("198.51.100.23"))
			require.Empty(t, runtime.Bans())
			for _, event := range runtime.Events() {
				require.Equal(t, gbsecurity.ScopeDevice, event.RiskScope)
				require.Equal(t, gbsecurity.ReasonDigestFailure, event.Reason)
			}

			freshNonce, err := runtime.IssueNonceForSource("198.51.100.23")
			require.NoError(t, err)
			corrected := authorizedRegisterRequest(t, freshNonce, securityTestPassword)
			corrected.SetTransport(transport)
			setRegisterCSeq(corrected, 101)
			tx := &captureServerTransaction{}
			handler.Handle(corrected, tx)
			require.Equal(t, sip.StatusOK, tx.response.StatusCode)
			require.Equal(t, 1, registerCalls)
			require.Empty(t, runtime.Bans())
			_, trusted := runtime.TrustedEndpoint(securityTestDeviceID)
			require.True(t, trusted)
		})
	}
}
