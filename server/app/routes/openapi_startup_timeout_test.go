package routes

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

type startupTimeoutConfig struct {
	app.YmlConfigInterf
	seconds int
}

func (c startupTimeoutConfig) GetInt(key string) int {
	if key == "openapi.startup_timeout_seconds" {
		return c.seconds
	}
	return 0
}

func TestOpenAPIStartupTimeoutUsesDefaultAndBounds(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int
		expected time.Duration
	}{
		{name: "missing uses default", seconds: 0, expected: 60 * time.Second},
		{name: "below minimum uses default", seconds: 14, expected: 60 * time.Second},
		{name: "minimum is accepted", seconds: 15, expected: 15 * time.Second},
		{name: "configured value", seconds: 90, expected: 90 * time.Second},
		{name: "maximum is accepted", seconds: 300, expected: 300 * time.Second},
		{name: "above maximum uses default", seconds: 301, expected: 60 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, openAPIStartupTimeout(startupTimeoutConfig{seconds: tt.seconds}))
		})
	}
}
