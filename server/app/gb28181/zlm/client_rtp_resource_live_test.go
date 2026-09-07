//go:build openapi_live

package zlm

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestOpenAPIRtpResourceTLSIsolatedProcess(t *testing.T) {
	if os.Getenv("UVP_OPENAPI_TEST_ZLM_FIXTURE") != "isolated-loopback" {
		if os.Getenv("UVP_OPENAPI_INTEGRATION_REQUIRED") == "1" {
			t.Fatal("explicit isolated TLS fixture required")
		}
		t.Skip("no isolated media process; not a native acceptance pass")
	}
	port, err := strconv.Atoi(os.Getenv("UVP_OPENAPI_TEST_ZLM_TLS_PORT"))
	require.NoError(t, err)
	require.True(t, port > 0 && port <= 65535)
	boot := os.Getenv("UVP_OPENAPI_TEST_ZLM_BOOT")
	require.True(t, validRuntimeNonce(boot))
	secret := os.Getenv("UVP_OPENAPI_TEST_ZLM_SECRET")
	require.NotEmpty(t, secret)
	caFile := os.Getenv("UVP_OPENAPI_TEST_ZLM_CA_FILE")
	require.True(t, filepath.IsAbs(caFile))
	require.Equal(t, "cert.pem", filepath.Base(caFile))
	require.True(t, strings.HasPrefix(filepath.Base(filepath.Dir(caFile)), "uvp-runtime-http-"))
	ca, err := os.ReadFile(caFile)
	require.NoError(t, err)
	block, _ := pem.Decode(ca)
	require.NotNil(t, block)
	certificate, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(ca))
	control, err := NewOpenAPIRuntimeControl(node.Node{ID: 1, Revision: 1, MediaServerUUID: "runtime-contract-test", APISecret: secret},
		OpenAPIControlTLS{Endpoint: "https://127.0.0.1:" + strconv.Itoa(port) + "/index/api", Roots: roots,
			SPKISHA256: sha256.Sum256(certificate.RawSubjectPublicKeyInfo)})
	require.NoError(t, err)
	defer control.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	identity, err := control.GetRuntimeIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, boot, identity.BootNonce)
	id, err := NewRtpResourceID(time.Now())
	require.NoError(t, err)
	request := RtpResourceOpenRequest{RtpResourceSelector: RtpResourceSelector{
		BootNonce: boot, ResourceID: id, VHost: "__defaultVhost__", App: "rtp", Stream: "go-fence-" + id[15:],
	}, LocalIP: "127.0.0.1", SSRC: 12345}
	created, err := control.OpenRtpServerIfMatch(ctx, request)
	require.NoError(t, err)
	require.Equal(t, RtpResourceCreated, created.Result)
	require.Positive(t, created.Port)
	repeated, err := control.OpenRtpServerIfMatch(ctx, request)
	require.NoError(t, err)
	require.Equal(t, RtpResourceExisting, repeated.Result)
	require.Equal(t, created.Port, repeated.Port)
	wrong := request.RtpResourceSelector
	wrong.BootNonce = "0" + boot[1:]
	if wrong.BootNonce == boot {
		wrong.BootNonce = "1" + boot[1:]
	}
	result, err := control.CloseRtpServerIfMatch(ctx, wrong)
	require.NoError(t, err)
	require.Equal(t, RtpResourceRuntimeMismatch, result)
	other := request
	other.ResourceID, err = NewRtpResourceID(time.Now())
	require.NoError(t, err)
	other.Stream += "-other"
	createdOther, err := control.OpenRtpServerIfMatch(ctx, other)
	require.NoError(t, err)
	require.Equal(t, RtpResourceCreated, createdOther.Result)
	result, err = control.CloseRtpServerIfMatch(ctx, request.RtpResourceSelector)
	require.NoError(t, err)
	require.Equal(t, RtpResourceShutdownScheduled, result)
	result, err = control.CloseRtpServerIfMatch(ctx, request.RtpResourceSelector)
	require.NoError(t, err)
	require.Equal(t, RtpResourceClosePending, result)
	repeated, err = control.OpenRtpServerIfMatch(ctx, request)
	require.NoError(t, err)
	require.Equal(t, RtpResourceRetired, repeated.Result)
	repeated, err = control.OpenRtpServerIfMatch(ctx, other)
	require.NoError(t, err)
	require.Equal(t, RtpResourceExisting, repeated.Result)
	result, err = control.CloseRtpServerIfMatch(ctx, other.RtpResourceSelector)
	require.NoError(t, err)
	require.Equal(t, RtpResourceShutdownScheduled, result)
	t.Log("verified TLS RTP identity/fence interop passed; pending is not terminal and product dispatch stays disabled")
}
