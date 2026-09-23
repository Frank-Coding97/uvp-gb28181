package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/audit"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

func TestOpenAPIGatewayRejectedAuditIsBoundedAndVerified(t *testing.T) {
	gate, db, secret := gatewayFixture(t)
	signature := ""
	for i := 0; i < 300; i++ {
		response := gatewayCall(t, gate, secret, strings.Repeat("a", 32), func(r *http.Request) {
			signature = r.Header.Get("X-UVP-Signature")
			r.Header.Set("X-UVP-Signature", strings.Repeat("0", 64))
			r.Header.Set("X-Forwarded-For", "203.0.113.99")
		})
		require.Equal(t, 401, response.Code)
	}
	snapshot := gate.RejectedSummary()
	require.Len(t, snapshot.Events, audit.RejectedEventCapacity)
	require.EqualValues(t, 300, snapshot.Counts[audit.ReasonAuthenticationFailed])
	for _, event := range snapshot.Events {
		require.Zero(t, event.ClientID)
		require.Equal(t, "192.0.2.1", event.Source)
	}
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret)
	require.NotContains(t, string(encoded), signature)
	require.NotContains(t, string(encoded), "uvp_")
	require.NoError(t, db.Where("client_id = ?", 1).Delete(&models.ClientScope{}).Error)
	response := gatewayCall(t, gate, secret, strings.Repeat("b", 32), nil)
	require.Equal(t, 403, response.Code)
	snapshot = gate.RejectedSummary()
	require.EqualValues(t, 1, snapshot.Events[len(snapshot.Events)-1].ClientID)
	require.Equal(t, audit.ReasonCapabilityDenied, snapshot.Events[len(snapshot.Events)-1].Reason)
	for _, model := range []any{&models.Nonce{}, &models.Audit{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestOpenAPIGatewayAdmittedOutcomeKeepsDurableAudit(t *testing.T) {
	gate, db, secret := gatewayFixture(t)
	gate.read = func(context.Context, *gorm.DB, metadataInput) (any, error) { return map[string]bool{"ok": true}, nil }
	gate.complete = func(context.Context, string, string, time.Duration) error { return errors.New("private failure") }
	require.Equal(t, 503, gatewayCall(t, gate, secret, strings.Repeat("c", 32), nil).Code)
	require.Empty(t, gate.RejectedSummary().Events, "accepted work must retain its separate durable outcome")
	var row models.Audit
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, "started", row.Result)
}
