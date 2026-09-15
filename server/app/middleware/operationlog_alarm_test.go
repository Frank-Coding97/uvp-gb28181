package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/models"
)

func TestAlarmBatchDeleteCanOverrideOperationType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/gb28181/alarms/batch-delete", nil)
	require.Equal(t, models.OperationCreate, getOperationType(ctx))
	MarkDeleteOperation(ctx)
	require.Equal(t, models.OperationDelete, getOperationType(ctx))
}
