//go:build openapi_live

package zlm

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

// The harness creates a dedicated loopback ZLM; no business node/address is
// accepted. Missing opt-in is a skip, never a passed native media gate.
func TestOpenAPIRuntimeControlIsolatedProcess(t *testing.T) {
	if os.Getenv("UVP_OPENAPI_TEST_ZLM_FIXTURE") != "isolated-loopback" {
		if os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1" {
			t.Fatal("isolated runtime fixture is required")
		}
		t.Skip("requires a dedicated loopback runtime fixture")
	}
	port, err := strconv.Atoi(os.Getenv("UVP_OPENAPI_TEST_ZLM_PORT"))
	require.NoError(t, err)
	require.True(t, port > 0 && port <= 65535)
	expected := os.Getenv("UVP_OPENAPI_TEST_ZLM_BOOT")
	require.True(t, validRuntimeNonce(expected))
	secret := os.Getenv("UVP_OPENAPI_TEST_ZLM_SECRET")
	require.NotEmpty(t, secret)
	c := NewClientForNode(&node.Node{Host: "127.0.0.1", APIPort: port, APISecret: secret})
	identity, err := c.GetRuntimeIdentity(context.Background())
	require.NoError(t, err)
	require.Equal(t, expected, identity.BootNonce, "must be the exact fixture process")
	if previous := os.Getenv("UVP_OPENAPI_TEST_ZLM_PREVIOUS_BOOT"); previous != "" {
		require.NotEqual(t, previous, identity.BootNonce, "real process restart must replace boot identity")
		result, err := c.KickSessionIfMatch(context.Background(), previous, "9223372036854775807--1")
		require.NoError(t, err)
		require.Equal(t, KickRuntimeMismatch, result)
	}
	result, err := c.KickSessionIfMatch(context.Background(), expected, "9223372036854775807--1")
	require.NoError(t, err)
	require.Equal(t, KickNotFound, result)
	wrong := "0" + expected[1:]
	if wrong == expected {
		wrong = "1" + expected[1:]
	}
	result, err = c.KickSessionIfMatch(context.Background(), wrong, "9223372036854775807--1")
	require.NoError(t, err)
	require.Equal(t, KickRuntimeMismatch, result)
}
