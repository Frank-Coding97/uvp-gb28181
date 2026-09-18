package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type openAPITransportContractDocument struct {
	Transport openAPITransportContract `yaml:"x-uvp-transport-contract"`
}

type openAPITransportContract struct {
	HTTPVersion string               `yaml:"httpVersion"`
	POST        openAPIPOSTTransport `yaml:"post"`
}

type openAPIPOSTTransport struct {
	Body           openAPIPOSTBody `yaml:"body"`
	AutomaticRetry bool            `yaml:"automaticRetry"`
}

type openAPIPOSTBody struct {
	ContentLength    openAPIContentLengthPolicy `yaml:"contentLength"`
	TransferEncoding openAPIEncodingPolicy      `yaml:"transferEncoding"`
	ContentEncoding  openAPIEncodingPolicy      `yaml:"contentEncoding"`
	ContentType      string                     `yaml:"contentType"`
	JSON             openAPIJSONBodyPolicy      `yaml:"json"`
}

type openAPIContentLengthPolicy struct {
	Required bool   `yaml:"required"`
	Minimum  int    `yaml:"minimum"`
	Maximum  int    `yaml:"maximum"`
	Value    string `yaml:"value"`
	Signed   bool   `yaml:"signed"`
}

type openAPIEncodingPolicy struct {
	Allowed bool `yaml:"allowed"`
	Chunked bool `yaml:"chunked"`
}

type openAPIJSONBodyPolicy struct {
	ExactFields           []string `yaml:"exactFields"`
	RejectUnknownFields   bool     `yaml:"rejectUnknownFields"`
	RejectDuplicateFields bool     `yaml:"rejectDuplicateFields"`
}

func TestOpenAPITransportContractRequiresFixedLengthJSONPOST(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../../../..", "api", "openapi-v1.yaml"))
	require.NoError(t, err)

	var document openAPITransportContractDocument
	require.NoError(t, yaml.Unmarshal(body, &document))
	require.Equal(t, "HTTP/1.1", document.Transport.HTTPVersion)
	require.True(t, document.Transport.POST.Body.ContentLength.Required)
	require.Equal(t, 1, document.Transport.POST.Body.ContentLength.Minimum)
	require.Equal(t, 65536, document.Transport.POST.Body.ContentLength.Maximum)
	require.Equal(t, "exact body byte length", document.Transport.POST.Body.ContentLength.Value)
	require.False(t, document.Transport.POST.Body.ContentLength.Signed)
	require.False(t, document.Transport.POST.Body.TransferEncoding.Allowed)
	require.False(t, document.Transport.POST.Body.TransferEncoding.Chunked)
	require.False(t, document.Transport.POST.Body.ContentEncoding.Allowed)
	require.Equal(t, "application/json", document.Transport.POST.Body.ContentType)
	require.Equal(t, []string{"protocol"}, document.Transport.POST.Body.JSON.ExactFields)
	require.True(t, document.Transport.POST.Body.JSON.RejectUnknownFields)
	require.True(t, document.Transport.POST.Body.JSON.RejectDuplicateFields)
	require.False(t, document.Transport.POST.AutomaticRetry)
}
