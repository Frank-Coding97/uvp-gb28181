package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// ConfigureTrustedProxies sets only explicitly configured proxy addresses.
// An empty list disables forwarded-header trust and uses the direct peer address.
func ConfigureTrustedProxies(engine *gin.Engine, proxies []string) error {
	clean := make([]string, 0, len(proxies))
	for _, proxy := range proxies {
		if value := strings.TrimSpace(proxy); value != "" {
			clean = append(clean, value)
		}
	}
	return engine.SetTrustedProxies(clean)
}
