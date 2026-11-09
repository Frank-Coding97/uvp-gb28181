package integration

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	gbplay "uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/openapi/cloudrecord"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
	"uvplatform.com/uvp-gb28181/app/openapi/devicecatalog"
	"uvplatform.com/uvp-gb28181/app/openapi/deviceevents"
	"uvplatform.com/uvp-gb28181/app/openapi/devicequery"
	"uvplatform.com/uvp-gb28181/app/openapi/play"
	"uvplatform.com/uvp-gb28181/app/openapi/recording"
	"uvplatform.com/uvp-gb28181/app/openapi/resource"
	"uvplatform.com/uvp-gb28181/app/openapi/voice"
)

func responseExampleDocument(t *testing.T) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../../../api/openapi-v1.yaml")
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal(b, &doc))
	b, err = json.Marshal(doc)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &doc))
	return doc
}
func exampleResolve(t *testing.T, doc, s map[string]any) map[string]any {
	t.Helper()
	seen := map[string]bool{}
	for {
		ref, ok := s["$ref"].(string)
		if !ok {
			break
		}
		require.False(t, seen[ref], "cyclic schema reference %s", ref)
		seen[ref] = true
		var n any = doc
		for _, p := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			n = n.(map[string]any)[p]
		}
		s = n.(map[string]any)
	}
	return s
}
func checkResponseExample(t *testing.T, doc, s map[string]any, v any, path string) {
	t.Helper()
	s = exampleResolve(t, doc, s)
	if v == nil {
		require.Equal(t, true, s["nullable"], path)
		return
	}
	switch s["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		require.True(t, ok, path)
		props, _ := s["properties"].(map[string]any)
		for _, r := range arrayOf(s["required"]) {
			require.Contains(t, obj, r, path)
		}
		for k, value := range obj {
			field, ok := props[k].(map[string]any)
			if !ok {
				if dynamic, ok := s["additionalProperties"].(map[string]any); ok {
					checkResponseExample(t, doc, dynamic, value, path+"."+k)
					continue
				}
				require.NotEqual(t, false, s["additionalProperties"], path+"."+k)
				continue
			}
			checkResponseExample(t, doc, field, value, path+"."+k)
		}
	case "array":
		items, ok := v.([]any)
		require.True(t, ok, path)
		for _, item := range items {
			checkResponseExample(t, doc, s["items"].(map[string]any), item, path+"[]")
		}
	case "string":
		value, ok := v.(string)
		require.True(t, ok, path)
		require.NotEqual(t, "example", value, path+" must use a domain example")
		if s["format"] == "date-time" {
			_, err := time.Parse(time.RFC3339Nano, value)
			require.NoError(t, err, path)
		}
		if pattern, ok := s["pattern"].(string); ok {
			require.Regexp(t, regexp.MustCompile(pattern), value, path)
		}
	case "integer", "number":
		n, ok := v.(float64)
		require.True(t, ok, path)
		if s["type"] == "integer" {
			require.Equal(t, float64(int64(n)), n, path)
		}
		if min, ok := s["minimum"].(float64); ok {
			require.GreaterOrEqual(t, n, min, path)
		}
		if max, ok := s["maximum"].(float64); ok {
			require.LessOrEqual(t, n, max, path)
		}
	case "boolean":
		_, ok := v.(bool)
		require.True(t, ok, path)
	}
	if values := arrayOf(s["enum"]); len(values) > 0 {
		require.Contains(t, values, v, path)
	}
}
func arrayOf(v any) []any { a, _ := v.([]any); return a }

func TestEveryOpenAPIActionHasValidatedSuccessExamples(t *testing.T) {
	doc := responseExampleDocument(t)
	for path, item := range doc["paths"].(map[string]any) {
		for method, raw := range item.(map[string]any) {
			t.Run(method+" "+path, func(t *testing.T) {
				op := raw.(map[string]any)
				for code, r := range op["responses"].(map[string]any) {
					if !strings.HasPrefix(code, "2") {
						continue
					}
					response := exampleResolve(t, doc, r.(map[string]any))
					content, _ := response["content"].(map[string]any)
					media, ok := content["application/json"].(map[string]any)
					if !ok {
						continue
					}
					schema := media["schema"].(map[string]any)
					if e, ok := media["example"]; ok {
						checkResponseExample(t, doc, schema, e, code)
						continue
					}
					examples, _ := media["examples"].(map[string]any)
					require.NotEmpty(t, examples, "every action needs an explicit success example")
					for name, e := range examples {
						checkResponseExample(t, doc, schema, e.(map[string]any)["value"], code+"/"+name)
					}
				}
			})
		}
	}
}

func TestRichResponseSchemasDescribeEverySerializedDTOField(t *testing.T) {
	doc := responseExampleDocument(t)
	cases := []struct {
		name string
		dto  any
	}{
		{"LivePlayback", gbplay.Result{}},
		{"Device", resource.Device{}},
		{"Channel", resource.Channel{}},
		{"DeviceStatus", resource.DeviceStatus{}},
		{"ChannelStatus", resource.ChannelStatus{}},
		{"DeviceStatisticsResult", resource.DeviceStatistics{}},
		{"ChannelStatisticsResult", resource.ChannelStatistics{}},
		{"GetLiveSessionResult", play.LiveView{}},
		{"ListLiveSessionsResult", play.LivePage{}},
		{"DeviceSubscriptionView", deviceevents.ConfiguredSubscription{}},
		{"SharedSIPSubscription", deviceevents.UpstreamSubscription{}},
		{"ChannelControlStatus", devicequery.ChannelControlStatus{}},
		{"DeviceAlarm", devicequery.DeviceAlarm{}},
		{"CloudRecord", cloudrecord.File{}},
		{"CloudRecordPage", cloudrecord.Page{}},
		{"CloudRecordDates", cloudrecord.Dates{}},
		{"OpenAPIDeviceRecordQueryResult", recording.RecordPage{}},
		{"OpenAPIDevicePlaybackCreateResult", recording.PlaybackView{}},
		{"RecordQueryOptions", recording.RecordQueryOptions{}},
		{"DeviceEvent", deviceevents.Event{}},
		{"VoiceSession", voice.SessionView{}},
		{"DeviceCatalogProgress", devicecatalog.RefreshProgress{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
			raw, ok := schemas[tc.name].(map[string]any)
			require.True(t, ok, "missing schema")
			schema := exampleResolve(t, doc, raw)
			props, _ := schema["properties"].(map[string]any)
			typ := reflect.TypeOf(tc.dto)
			for i := 0; i < typ.NumField(); i++ {
				name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
				if name != "" && name != "-" {
					require.Contains(t, props, name, "actual DTO field is undocumented")
				}
			}
		})
	}
}

// Published permission/catalog metadata must describe the same business data
// as the public API reference, including nested fields and nullable values.
func TestPublishedResponseMetadataMatchesOpenAPIReference(t *testing.T) {
	doc := responseExampleDocument(t)
	byScope := map[string]map[string]any{}
	for _, item := range doc["paths"].(map[string]any) {
		for _, raw := range item.(map[string]any) {
			op := raw.(map[string]any)
			byScope[op["x-uvp-scope"].(string)] = op
		}
	}
	var expand func(any) any
	expand = func(value any) any {
		switch v := value.(type) {
		case map[string]any:
			v = exampleResolve(t, doc, v)
			result := map[string]any{}
			for k, child := range v {
				result[k] = expand(child)
			}
			return result
		case []any:
			result := make([]any, len(v))
			for i, child := range v {
				result[i] = expand(child)
			}
			return result
		default:
			return value
		}
	}
	for _, operation := range contracts.Operations() {
		t.Run(operation.Scope, func(t *testing.T) {
			op := byScope[operation.Scope]
			response := exampleResolve(t, doc, op["responses"].(map[string]any)["200"].(map[string]any))
			content, ok := response["content"].(map[string]any)
			if !ok {
				return
			}
			media, ok := content["application/json"].(map[string]any)
			if !ok {
				return
			}
			schema := exampleResolve(t, doc, media["schema"].(map[string]any))
			expected := expand(schema["properties"].(map[string]any)["data"])
			var actual any
			require.NoError(t, json.Unmarshal([]byte(operation.ResponseSchema), &actual))
			require.Equal(t, expected, actual)
		})
	}
}

func TestResponseExamplesRespectCompletedActions(t *testing.T) {
	doc := responseExampleDocument(t)
	for _, route := range []string{"subscription/create", "subscription/renew", "subscription/cancel", "talk/stop", "broadcast/stop"} {
		t.Run(route, func(t *testing.T) {
			op := doc["paths"].(map[string]any)["/openapi/v1/"+route].(map[string]any)["post"].(map[string]any)
			response := op["responses"].(map[string]any)["200"].(map[string]any)
			media := response["content"].(map[string]any)["application/json"].(map[string]any)
			samples := []any{}
			if sample, ok := media["example"]; ok {
				samples = append(samples, sample)
			}
			if named, ok := media["examples"].(map[string]any); ok {
				for _, sample := range named {
					samples = append(samples, sample.(map[string]any)["value"])
				}
			}
			require.NotEmpty(t, samples)
			for _, sample := range samples {
				data := sample.(map[string]any)["data"].(map[string]any)
				switch route {
				case "subscription/create", "subscription/renew":
					require.Equal(t, true, data["enabled"])
					require.Equal(t, "enabled", data["status"])
				case "subscription/cancel":
					require.Equal(t, false, data["enabled"])
					require.Equal(t, "cancelled", data["status"])
				default:
					require.Equal(t, "ended", data["state"])
					require.NotNil(t, data["endedAt"])
					require.Nil(t, data["media"])
				}
			}
		})
	}
}
