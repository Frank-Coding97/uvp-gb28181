package integration

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"uvplatform.cn/uvp-gb28181/app/openapi/client"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
	"uvplatform.cn/uvp-gb28181/app/openapi/routes"
)

type openAPIContract struct {
	OpenAPI             string                                  `yaml:"openapi"`
	Info                contractInfo                            `yaml:"info"`
	SecretHandling      string                                  `yaml:"x-uvp-secret-handling"`
	SignatureContract   contractSignatureContract               `yaml:"x-uvp-signature-contract"`
	RetryPolicy         map[string]string                       `yaml:"x-uvp-retry-policy"`
	ErrorClassification map[string]int                          `yaml:"x-uvp-error-classification"`
	RequestLimits       contractRequestLimits                   `yaml:"x-uvp-request-limits"`
	Paths               map[string]map[string]contractOperation `yaml:"paths"`
	Components          contractComponents                      `yaml:"components"`
}

type contractInfo struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

type contractComponents struct {
	Parameters      map[string]contractParameter `yaml:"parameters"`
	Schemas         map[string]contractSchema    `yaml:"schemas"`
	SecuritySchemes map[string]contractSecurity  `yaml:"securitySchemes"`
}

type contractSecurity struct {
	Type string `yaml:"type"`
	In   string `yaml:"in"`
	Name string `yaml:"name"`
}

type contractSignatureContract struct {
	Algorithm       string            `yaml:"algorithm"`
	RequiredHeaders []string          `yaml:"requiredHeaders"`
	QueryAllowlist  []string          `yaml:"queryAllowlist"`
	PathRule        string            `yaml:"pathRule"`
	QueryRule       string            `yaml:"queryRule"`
	DuplicateRule   string            `yaml:"duplicateRule"`
	UTF8Rule        string            `yaml:"utf8Rule"`
	Canonical       canonicalContract `yaml:"canonical"`
}

type canonicalContract struct {
	LineSeparator    string            `yaml:"lineSeparator"`
	FinalLineHasNoLF bool              `yaml:"finalLineHasNoLF"`
	Lines            []string          `yaml:"lines"`
	HMACKey          string            `yaml:"hmacKey"`
	Audience         audienceContract  `yaml:"audience"`
	Timestamp        timestampContract `yaml:"timestamp"`
	Nonce            nonceContract     `yaml:"nonce"`
}

type audienceContract struct {
	Source         string `yaml:"source"`
	ClientSupplied bool   `yaml:"clientSupplied"`
}

type timestampContract struct {
	MaxSkewSeconds int    `yaml:"maxSkewSeconds"`
	Rule           string `yaml:"rule"`
}

type nonceContract struct {
	UniqueBy               string `yaml:"uniqueBy"`
	Persistent             bool   `yaml:"persistent"`
	SurvivesSecretRotation bool   `yaml:"survivesSecretRotation"`
}

type contractRequestLimits struct {
	BodyBytes            int    `yaml:"bodyBytes"`
	QueryBytes           int    `yaml:"queryBytes"`
	BodyHash             string `yaml:"bodyHash"`
	DuplicateJSONMembers string `yaml:"duplicateJsonMembers"`
}

type contractOperation struct {
	OperationID   string                      `yaml:"operationId"`
	Scope         string                      `yaml:"x-uvp-scope"`
	Enabled       *bool                       `yaml:"x-uvp-enabled"`
	CurrentStatus *int                        `yaml:"x-uvp-current-status"`
	TTLSeconds    *int                        `yaml:"x-uvp-expires-ttl-seconds"`
	ContentType   string                      `yaml:"x-uvp-content-type"`
	Parameters    []contractParameterRef      `yaml:"parameters"`
	Security      []map[string][]string       `yaml:"security"`
	RequestBody   *contractRequestBody        `yaml:"requestBody"`
	Responses     map[string]contractResponse `yaml:"responses"`
}

type contractParameterRef struct {
	Ref      string         `yaml:"$ref"`
	Name     string         `yaml:"name"`
	In       string         `yaml:"in"`
	Required bool           `yaml:"required"`
	Schema   contractSchema `yaml:"schema"`
}

type contractParameter struct {
	Name        string         `yaml:"name"`
	In          string         `yaml:"in"`
	Required    bool           `yaml:"required"`
	Description string         `yaml:"description"`
	Schema      contractSchema `yaml:"schema"`
}

type contractRequestBody struct {
	Required bool                         `yaml:"required"`
	Content  map[string]contractMediaType `yaml:"content"`
}

type contractResponse struct {
	Description string                       `yaml:"description"`
	Enabled     *bool                        `yaml:"x-uvp-enabled"`
	Headers     map[string]contractHeader    `yaml:"headers"`
	Content     map[string]contractMediaType `yaml:"content"`
}

type contractHeader struct {
	Schema contractSchema `yaml:"schema"`
}

type contractMediaType struct {
	Schema contractSchema `yaml:"schema"`
}

type contractSchema struct {
	Ref                  string                    `yaml:"$ref"`
	Type                 string                    `yaml:"type"`
	Format               string                    `yaml:"format"`
	Pattern              string                    `yaml:"pattern"`
	Enum                 []string                  `yaml:"enum"`
	Required             []string                  `yaml:"required"`
	Properties           map[string]contractSchema `yaml:"properties"`
	Items                *contractSchema           `yaml:"items"`
	AdditionalProperties *bool                     `yaml:"additionalProperties"`
	MinLength            *int                      `yaml:"minLength"`
	MaxLength            *int                      `yaml:"maxLength"`
	Minimum              *int                      `yaml:"minimum"`
	Maximum              *int                      `yaml:"maximum"`
	Default              any                       `yaml:"default"`
	Nullable             bool                      `yaml:"nullable"`
}

type contractRoute struct {
	method string
	path   string
	scope  string
}

var publicContractRoutes = []contractRoute{
	{method: "get", path: "/openapi/v1/devices", scope: "device:list"},
	{method: "get", path: "/openapi/v1/devices/{deviceId}", scope: "device:detail"},
	{method: "get", path: "/openapi/v1/devices/{deviceId}/status", scope: "device:status"},
	{method: "get", path: "/openapi/v1/devices/{deviceId}/channels", scope: "channel:list"},
	{method: "get", path: "/openapi/v1/devices/{deviceId}/channels/{channelId}", scope: "channel:detail"},
	{method: "get", path: "/openapi/v1/devices/{deviceId}/channels/{channelId}/status", scope: "channel:status"},
	{method: "post", path: "/openapi/v1/devices/{deviceId}/channels/{channelId}/live-authorizations", scope: "play:live:apply"},
}

var requiredSignatureHeaders = []string{
	"X-UVP-Sign-Version",
	"X-UVP-Access-Key",
	"X-UVP-Timestamp",
	"X-UVP-Nonce",
	"X-UVP-Signature",
}

func TestOpenAPIContract(t *testing.T) {
	doc := loadOpenAPIContract(t)
	require.Equal(t, "3.0.3", doc.OpenAPI)
	require.NotEmpty(t, doc.Info.Title)
	require.NotEmpty(t, doc.Info.Version)
	secretHandling := strings.ToLower(doc.SecretHandling)
	require.Contains(t, secretHandling, "external business api")
	require.Contains(t, secretHandling, "administrative")
	require.Contains(t, secretHandling, "one-time")
	require.Contains(t, secretHandling, "server-side")
	require.Contains(t, secretHandling, "browser")
	require.Equal(t, "HMAC-SHA256", doc.SignatureContract.Algorithm)
	require.Equal(t, requiredSignatureHeaders, doc.SignatureContract.RequiredHeaders)
	require.Equal(t, []string{"keyword", "page", "pageSize", "status"}, doc.SignatureContract.QueryAllowlist)
	require.Contains(t, strings.ToLower(doc.SignatureContract.PathRule), "ascii")
	require.Contains(t, strings.ToLower(doc.SignatureContract.PathRule), "encoded")
	require.Contains(t, strings.ToLower(doc.SignatureContract.QueryRule), "rfc3986")
	require.Contains(t, strings.ToLower(doc.SignatureContract.QueryRule), "raw +")
	require.Contains(t, strings.ToLower(doc.SignatureContract.QueryRule), "semicolon")
	require.Contains(t, strings.ToLower(doc.SignatureContract.QueryRule), "%20")
	require.Contains(t, strings.ToLower(doc.SignatureContract.QueryRule), "valid")
	require.Contains(t, strings.ToLower(doc.SignatureContract.DuplicateRule), "duplicate")
	require.Contains(t, strings.ToLower(doc.SignatureContract.DuplicateRule), "only in headers")
	require.Contains(t, strings.ToLower(doc.SignatureContract.UTF8Rule), "utf-8")
	require.Equal(t, "LF", doc.SignatureContract.Canonical.LineSeparator)
	require.True(t, doc.SignatureContract.Canonical.FinalLineHasNoLF)
	require.Equal(t, []string{
		"UVP-HMAC-SHA256/1",
		"<AK>",
		"<Timestamp>",
		"<Nonce>",
		"<UPPERCASE HTTP Method>",
		"<CanonicalPath>",
		"<CanonicalQuery, possibly empty>",
		"<ContentType: application/json or empty>",
		"<BodySHA256, lowercase hexadecimal>",
		"<ServiceAudience, deployment-fixed OpenAPI audience>",
	}, doc.SignatureContract.Canonical.Lines)
	require.Contains(t, strings.ToLower(doc.SignatureContract.Canonical.HMACKey), "base64url")
	require.Contains(t, strings.ToLower(doc.SignatureContract.Canonical.HMACKey), "32 bytes")
	require.Equal(t, "deployment-fixed", doc.SignatureContract.Canonical.Audience.Source)
	require.False(t, doc.SignatureContract.Canonical.Audience.ClientSupplied)
	require.Equal(t, 300, doc.SignatureContract.Canonical.Timestamp.MaxSkewSeconds)
	require.Contains(t, strings.ToLower(doc.SignatureContract.Canonical.Timestamp.Rule), "absolute difference")
	require.Equal(t, "(client_id, nonce)", doc.SignatureContract.Canonical.Nonce.UniqueBy)
	require.True(t, doc.SignatureContract.Canonical.Nonce.Persistent)
	require.True(t, doc.SignatureContract.Canonical.Nonce.SurvivesSecretRotation)
	require.Contains(t, strings.ToLower(doc.RetryPolicy["GET"]), "fresh")
	require.Contains(t, strings.ToLower(doc.RetryPolicy["GET"]), "nonce")
	require.Contains(t, strings.ToLower(doc.RetryPolicy["POST"]), "do not")
	require.Contains(t, strings.ToLower(doc.RetryPolicy["POST"]), "timeout")
	require.Equal(t, map[string]int{
		"missingRequiredAuthHeader":    401,
		"malformedAuthHeader":          400,
		"unknownInactiveOrInvalidHMAC": 401,
		"expired":                      401,
		"replayed":                     401,
		"dependencyUnavailable":        503,
	}, doc.ErrorClassification)
	require.Equal(t, 65536, doc.RequestLimits.BodyBytes)
	require.Equal(t, 8192, doc.RequestLimits.QueryBytes)
	require.Contains(t, strings.ToLower(doc.RequestLimits.BodyHash), "original request bytes")
	require.Contains(t, strings.ToLower(doc.RequestLimits.DuplicateJSONMembers), "reject")
	require.Len(t, doc.Paths, len(publicContractRoutes))
	require.Contains(t, doc.Components.SecuritySchemes, "uvpHmac")
	require.Equal(t, contractSecurity{Type: "apiKey", In: "header", Name: "X-UVP-Signature"}, doc.Components.SecuritySchemes["uvpHmac"])
	errorSchema := resolveSchema(t, doc, contractSchema{Ref: "#/components/schemas/ErrorResponse"})
	require.Equal(t, []string{"INVALID_REQUEST", "AUTHENTICATION_FAILED", "REQUEST_EXPIRED", "REQUEST_REPLAYED", "CAPABILITY_DENIED", "RESOURCE_NOT_FOUND", "CONFLICT", "RATE_LIMITED", "QUOTA_EXCEEDED", "SERVICE_UNAVAILABLE"}, errorSchema.Properties["code"].Enum)
	require.True(t, errorSchema.Properties["data"].Nullable)

	gotScopes := make([]string, 0, len(publicContractRoutes))
	for _, route := range publicContractRoutes {
		operations, ok := doc.Paths[route.path]
		require.Truef(t, ok, "missing contract path %s", route.path)
		require.Len(t, operations, 1, "path %s must publish exactly one method", route.path)
		op, ok := operations[route.method]
		require.Truef(t, ok, "missing contract operation %s %s", route.method, route.path)
		require.NotEmpty(t, op.OperationID)
		require.Equal(t, route.scope, op.Scope)
		gotScopes = append(gotScopes, op.Scope)
		checkOperationContract(t, doc, route, op)
	}
	sort.Strings(gotScopes)
	wantScopes := client.SupportedScopes()
	sort.Strings(wantScopes)
	require.Equal(t, wantScopes, gotScopes, "contract scopes must equal the published capability catalog")

	checkPublicDTOs(t, doc)
	checkPublicRouteAST(t, doc)
	checkInstalledBoundary(t, doc)
}

func loadOpenAPIContract(t *testing.T) openAPIContract {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "api", "openapi-v1.yaml")
	body, err := os.ReadFile(path)
	require.NoError(t, err, "read machine-readable OpenAPI contract")
	var doc openAPIContract
	require.NoError(t, yaml.Unmarshal(body, &doc), "decode machine-readable OpenAPI contract")
	return doc
}

func checkOperationContract(t *testing.T, doc openAPIContract, route contractRoute, operation contractOperation) {
	t.Helper()
	parameters := make(map[string]contractParameter)
	for _, ref := range operation.Parameters {
		parameter := contractParameter{Name: ref.Name, In: ref.In, Required: ref.Required, Schema: ref.Schema}
		if ref.Ref != "" {
			const prefix = "#/components/parameters/"
			require.Truef(t, strings.HasPrefix(ref.Ref, prefix), "unsupported parameter ref %s", ref.Ref)
			resolved, ok := doc.Components.Parameters[strings.TrimPrefix(ref.Ref, prefix)]
			require.Truef(t, ok, "missing parameter ref %s", ref.Ref)
			parameter = resolved
		}
		require.NotEmpty(t, parameter.Name)
		require.NotEmpty(t, parameter.In)
		parameters[parameter.Name] = parameter
	}
	for _, name := range requiredSignatureHeaders {
		parameter, ok := parameters[name]
		require.Truef(t, ok, "operation %s missing %s", route.scope, name)
		require.Equal(t, "header", parameter.In)
		require.Truef(t, parameter.Required, "operation %s must require %s", route.scope, name)
		schema := resolveSchema(t, doc, parameter.Schema)
		switch name {
		case "X-UVP-Sign-Version":
			require.Equal(t, []string{"1"}, schema.Enum)
		case "X-UVP-Access-Key":
			require.Equal(t, "^uvp_[0-9a-f]{32}$", schema.Pattern)
		case "X-UVP-Timestamp":
			require.Equal(t, "^(0|[1-9][0-9]{0,18})$", schema.Pattern)
			require.Contains(t, parameter.Description, "9223372036854775807")
		case "X-UVP-Nonce":
			require.Equal(t, "^[0-9a-f]{32}$", schema.Pattern)
		case "X-UVP-Signature":
			require.Equal(t, "^[0-9a-f]{64}$", schema.Pattern)
		}
	}
	if route.method == "post" {
		require.Equal(t, "application/json", operation.ContentType)
		require.NotNil(t, operation.RequestBody)
		require.True(t, operation.RequestBody.Required)
		require.Len(t, operation.RequestBody.Content, 1)
		require.Contains(t, operation.RequestBody.Content, "application/json")
		bodySchema := resolveSchema(t, doc, operation.RequestBody.Content["application/json"].Schema)
		require.Equal(t, "object", bodySchema.Type)
		require.Equal(t, []string{"protocol"}, bodySchema.Required)
		require.Equal(t, []string{"https-flv", "wss-flv"}, bodySchema.Properties["protocol"].Enum)
		require.NotNil(t, bodySchema.AdditionalProperties)
		require.False(t, *bodySchema.AdditionalProperties)
	} else {
		require.Nil(t, operation.RequestBody)
		require.Empty(t, operation.ContentType)
	}

	var queryNames []string
	for name, parameter := range parameters {
		if parameter.In == "query" {
			queryNames = append(queryNames, name)
		}
	}
	sort.Strings(queryNames)
	wantQuery := []string(nil)
	if route.scope == "device:list" || route.scope == "channel:list" {
		wantQuery = []string{"keyword", "page", "pageSize", "status"}
	}
	require.Equal(t, wantQuery, queryNames)
	for _, name := range queryNames {
		parameter := parameters[name]
		require.False(t, parameter.Required)
		schema := resolveSchema(t, doc, parameter.Schema)
		switch name {
		case "keyword":
			require.Equal(t, "string", schema.Type)
			require.NotNil(t, schema.MaxLength)
			require.Equal(t, 100, *schema.MaxLength)
			require.Contains(t, parameter.Description, "empty value is valid")
		case "page":
			require.Equal(t, "integer", schema.Type)
			require.Equal(t, 1, intValue(schema.Default))
			require.Equal(t, 1, *schema.Minimum)
			require.Contains(t, parameter.Description, "empty value is rejected")
		case "pageSize":
			require.Equal(t, "integer", schema.Type)
			require.Equal(t, 20, intValue(schema.Default))
			require.Equal(t, 1, *schema.Minimum)
			require.Equal(t, 100, *schema.Maximum)
			require.Contains(t, parameter.Description, "empty value is rejected")
		case "status":
			require.Equal(t, []string{"online", "offline", "unknown"}, schema.Enum)
			require.Contains(t, parameter.Description, "empty value is rejected")
		}
	}
	for _, name := range []string{"deviceId", "channelId"} {
		if parameter, ok := parameters[name]; ok {
			require.Equal(t, "path", parameter.In)
			require.True(t, parameter.Required)
			require.Equal(t, "^[0-9]{20}$", resolveSchema(t, doc, parameter.Schema).Pattern)
		}
	}

	require.NotEmpty(t, operation.Security)
	require.Contains(t, operation.Security[0], "uvpHmac")
	require.NotContains(t, operation.Responses, "409", "metadata/media public contract must not classify replay as 409")
	require.Contains(t, operation.Responses["401"].Description, "REQUEST_REPLAYED")
	for _, status := range []string{"200", "400", "401", "403", "404", "405", "429", "503"} {
		response, ok := operation.Responses[status]
		require.Truef(t, ok, "%s %s missing response %s", route.method, route.path, status)
		cacheControl, ok := response.Headers["Cache-Control"]
		require.True(t, ok)
		require.Equal(t, []string{"no-store"}, cacheControl.Schema.Enum)
		if status == "405" {
			require.Empty(t, response.Content, "HEAD/405 must not promise a response body")
			continue
		}
		require.Len(t, response.Content, 1)
		require.Contains(t, response.Content, "application/json")
		if status == "429" {
			retryAfter, ok := response.Headers["Retry-After"]
			require.True(t, ok)
			require.Equal(t, "integer", retryAfter.Schema.Type)
			require.NotNil(t, retryAfter.Schema.Minimum)
			require.Equal(t, 1, *retryAfter.Schema.Minimum)
		}
	}
	successSchema := resolveResponseSchema(t, doc, operation.Responses["200"])
	require.Equal(t, "object", successSchema.Type)
	require.Equal(t, []string{"OK"}, successSchema.Properties["code"].Enum)
	require.Equal(t, []string{"success"}, successSchema.Properties["message"].Enum)
	if route.scope == "play:live:apply" {
		require.NotNil(t, operation.Enabled)
		require.False(t, *operation.Enabled)
		require.NotNil(t, operation.CurrentStatus)
		require.Equal(t, 503, *operation.CurrentStatus)
		require.NotNil(t, operation.TTLSeconds)
		require.Equal(t, 120, *operation.TTLSeconds)
		require.Contains(t, strings.ToLower(operation.Responses["200"].Description), "disabled")
		require.NotNil(t, operation.Responses["200"].Enabled)
		require.False(t, *operation.Responses["200"].Enabled)
	}
	for _, status := range []string{"400", "401", "403", "404", "429", "503"} {
		require.Equal(t, "#/components/schemas/ErrorResponse", operation.Responses[status].Content["application/json"].Schema.Ref)
		schema := resolveResponseSchema(t, doc, operation.Responses[status])
		require.Equal(t, "object", schema.Type)
		require.Equal(t, []string{"code", "message", "requestId", "data"}, schema.Required)
	}
}

func resolveResponseSchema(t *testing.T, doc openAPIContract, response contractResponse) contractSchema {
	t.Helper()
	require.Contains(t, response.Content, "application/json")
	return resolveSchema(t, doc, response.Content["application/json"].Schema)
}

func resolveSchema(t *testing.T, doc openAPIContract, schema contractSchema) contractSchema {
	t.Helper()
	if schema.Ref == "" {
		return schema
	}
	const prefix = "#/components/schemas/"
	require.Truef(t, strings.HasPrefix(schema.Ref, prefix), "unsupported schema ref %s", schema.Ref)
	resolved, ok := doc.Components.Schemas[strings.TrimPrefix(schema.Ref, prefix)]
	require.Truef(t, ok, "missing schema ref %s", schema.Ref)
	return resolved
}

func intValue(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case uint64:
		return int(number)
	case int64:
		return int(number)
	case float64:
		return int(number)
	case uint:
		return int(number)
	default:
		return -1
	}
}

func checkPublicDTOs(t *testing.T, doc openAPIContract) {
	t.Helper()
	for _, dto := range []struct {
		name   string
		typeOf reflect.Type
	}{
		{name: "Device", typeOf: reflect.TypeOf(resource.Device{})},
		{name: "DevicePage", typeOf: reflect.TypeOf(resource.DevicePage{})},
		{name: "DeviceStatus", typeOf: reflect.TypeOf(resource.DeviceStatus{})},
		{name: "Channel", typeOf: reflect.TypeOf(resource.Channel{})},
		{name: "ChannelPage", typeOf: reflect.TypeOf(resource.ChannelPage{})},
		{name: "ChannelStatus", typeOf: reflect.TypeOf(resource.ChannelStatus{})},
	} {
		schema := resolveSchema(t, doc, contractSchema{Ref: "#/components/schemas/" + dto.name})
		require.Equal(t, jsonFieldNames(dto.typeOf), propertyNames(schema.Properties), dto.name+" properties drifted from the public DTO")
		require.NotContains(t, schema.Properties, "ownerDeptId")
		require.NotContains(t, schema.Properties, "snapshotUrl")
		require.NotContains(t, schema.Properties, "secretKey")
	}
	for _, name := range []string{"Device", "Channel", "DeviceStatus", "ChannelStatus"} {
		schema := resolveSchema(t, doc, contractSchema{Ref: "#/components/schemas/" + name})
		status := schema.Properties["status"]
		require.Equal(t, []string{"online", "offline", "unknown"}, status.Enum, name+" status enum drifted")
	}
}

func jsonFieldNames(typeOf reflect.Type) map[string]struct{} {
	fields := make(map[string]struct{}, typeOf.NumField())
	for i := 0; i < typeOf.NumField(); i++ {
		tag := strings.Split(typeOf.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			fields[tag] = struct{}{}
		}
	}
	return fields
}

func propertyNames(properties map[string]contractSchema) map[string]struct{} {
	names := make(map[string]struct{}, len(properties))
	for name := range properties {
		names[name] = struct{}{}
	}
	return names
}

type publicRouteRegistration struct {
	method string
	path   string
	scope  string
}

func checkPublicRouteAST(t *testing.T, doc openAPIContract) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	routeFile := filepath.Join(filepath.Dir(file), "..", "routes", "public.go")
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, routeFile, nil, 0)
	require.NoError(t, err, "parse real public route registrar")

	registrations := make([]publicRouteRegistration, 0, len(doc.Paths))
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "GET" && selector.Sel.Name != "POST") {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != "private" || len(call.Args) < 2 {
			return true
		}
		pathLiteral, ok := call.Args[0].(*ast.BasicLit)
		if !ok || pathLiteral.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(pathLiteral.Value)
		require.NoError(t, err)
		handler, ok := call.Args[len(call.Args)-1].(*ast.CallExpr)
		if !ok {
			return true
		}
		handlerSelector, ok := handler.Fun.(*ast.SelectorExpr)
		if !ok || handlerSelector.Sel.Name != "Handler" {
			return true
		}
		gateway, ok := handlerSelector.X.(*ast.Ident)
		if !ok || gateway.Name != "gateway" || len(handler.Args) != 1 {
			return true
		}
		scopeLiteral, ok := handler.Args[0].(*ast.BasicLit)
		if !ok || scopeLiteral.Kind != token.STRING {
			return true
		}
		scope, err := strconv.Unquote(scopeLiteral.Value)
		require.NoError(t, err)
		registrations = append(registrations, publicRouteRegistration{
			method: strings.ToLower(selector.Sel.Name),
			path:   path,
			scope:  scope,
		})
		return true
	})

	require.Len(t, registrations, len(doc.Paths), "real public registrar route count drifted from the contract")
	seen := make(map[string]struct{}, len(registrations))
	for _, registration := range registrations {
		path := strings.NewReplacer(":deviceId", "{deviceId}", ":channelId", "{channelId}").Replace(registration.path)
		key := registration.method + " " + path
		_, duplicate := seen[key]
		require.Falsef(t, duplicate, "duplicate real public route %s", key)
		seen[key] = struct{}{}
		operations, ok := doc.Paths[path]
		require.Truef(t, ok, "real public route %s is absent from the contract", key)
		operation, ok := operations[registration.method]
		require.Truef(t, ok, "real public route %s has no contract operation", key)
		require.Equal(t, registration.scope, operation.Scope, "real public route scope drifted for %s", key)
	}
}

func checkInstalledBoundary(t *testing.T, doc openAPIContract) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := gin.New()
	require.NoError(t, routes.InstallPublicBoundary(root, nil, nil))
	for _, route := range publicContractRoutes {
		operation := doc.Paths[route.path][route.method]
		path := strings.ReplaceAll(route.path, "{deviceId}", "34020000001320000001")
		path = strings.ReplaceAll(path, "{channelId}", "34020000001320000002")
		request := httptest.NewRequest(strings.ToUpper(route.method), path, nil)
		response := httptest.NewRecorder()
		root.ServeHTTP(response, request)
		require.Equal(t, 503, response.Code, "%s %s must be an installed fail-closed route", route.method, route.path)
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		var envelope struct {
			Code string `json:"code"`
			Data any    `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.Equal(t, "SERVICE_UNAVAILABLE", envelope.Code)
		if route.scope == "play:live:apply" {
			require.NotNil(t, operation.Enabled)
			require.False(t, *operation.Enabled)
		}
	}
	wireServer := httptest.NewServer(root)
	defer wireServer.Close()
	headResponse, err := wireServer.Client().Head(wireServer.URL + "/openapi/v1/devices")
	require.NoError(t, err)
	defer headResponse.Body.Close()
	require.Equal(t, 405, headResponse.StatusCode)
	require.Equal(t, "no-store", headResponse.Header.Get("Cache-Control"))
	headBody, err := io.ReadAll(headResponse.Body)
	require.NoError(t, err)
	require.Empty(t, headBody)
	response := httptest.NewRecorder()
	root.ServeHTTP(response, httptest.NewRequest("GET", "/api/gb28181/openapi-clients", nil))
	require.Equal(t, 404, response.Code, "private management routes must not enter the public contract")
}
