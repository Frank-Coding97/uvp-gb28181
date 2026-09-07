package readiness

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProofBindsDirectionChallengeAndResponse(t *testing.T) {
	challenge, err := NewChallenge()
	require.NoError(t, err)
	proof := RequestProof("instance secret", challenge)
	require.True(t, VerifyRequest("instance secret", challenge, proof))
	require.False(t, VerifyRequest("other instance", challenge, proof))
	require.False(t, VerifyRequest("instance secret", "invalid", proof))
	body := []byte(`{"backend_ready":true}`)
	response := ResponseProof("instance secret", challenge, body)
	require.True(t, VerifyResponse("instance secret", challenge, body, response))
	require.False(t, VerifyResponse("instance secret", challenge, []byte(`{"backend_ready":false}`), response))
	require.False(t, VerifyResponse("instance secret", challenge, body, proof))
	second, err := NewChallenge()
	require.NoError(t, err)
	require.False(t, VerifyResponse("instance secret", second, body, response))
}
