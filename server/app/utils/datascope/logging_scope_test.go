package datascope

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type loggingScopeRequestIDKey struct{}

type loggingScopeContextProbe struct {
	mu  sync.Mutex
	ids []string
}

func (p *loggingScopeContextProbe) register(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Callback().Query().Before("gorm:query").Register("logging_scope_test:request_context", func(tx *gorm.DB) {
		requestID, _ := tx.Statement.Context.Value(loggingScopeRequestIDKey{}).(string)
		p.mu.Lock()
		p.ids = append(p.ids, requestID)
		p.mu.Unlock()
	})
	require.NoError(t, err)
}

func (p *loggingScopeContextProbe) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ids...)
}

func attachLoggingScopeRequestContext(c *gin.Context, requestID string) {
	request := httptest.NewRequest("GET", "/", nil)
	c.Request = request.WithContext(context.WithValue(request.Context(), loggingScopeRequestIDKey{}, requestID))
}

func TestLoggingScopeRequestContext(t *testing.T) {
	t.Run("owner department lookup", func(t *testing.T) {
		db, c := newVisibilityTestDB(t)
		attachLoggingScopeRequestContext(c, "scope-owner-1")
		probe := &loggingScopeContextProbe{}
		probe.register(t, db)

		deptIDs, needFilter := GetOwnerDeptIDsWithDB(c, db)
		require.Equal(t, []uint{1}, deptIDs)
		require.True(t, needFilter)
		requestIDs := probe.snapshot()
		require.NotEmpty(t, requestIDs)
		for _, requestID := range requestIDs {
			require.Equal(t, "scope-owner-1", requestID)
		}
	})

	t.Run("visibility lookup", func(t *testing.T) {
		db, c := newVisibilityTestDB(t)
		attachLoggingScopeRequestContext(c, "scope-visibility-1")
		probe := &loggingScopeContextProbe{}
		probe.register(t, db)

		var ids []uint
		err := db.Model(&gbmodels.GbDevice{}).
			Scopes(VisibilityScope(c, "owner_dept_id", "device_id")).
			Order("id ASC").Pluck("id", &ids).Error
		require.NoError(t, err)
		require.Equal(t, []uint{1, 2}, ids)
		requestIDs := probe.snapshot()
		require.NotEmpty(t, requestIDs)
		for _, requestID := range requestIDs {
			require.Equal(t, "scope-visibility-1", requestID)
		}
	})
}
