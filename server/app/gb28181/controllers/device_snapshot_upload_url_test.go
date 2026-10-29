package controllers

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"uvplatform.com/uvp-gb28181/app/deviceaccess"
)

func TestSnapshotUploadURLUsesRequestIngress(t *testing.T) {
	for _, base := range []string{"http://192.168.1.8:5177", "http://camera.example:51000", "https://camera.example:8443"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "http://backend:8280/", nil).WithContext(deviceaccess.WithBaseURL(context.Background(), base))
		got, err := snapshotUploadURL(c, "tok")
		require.NoError(t, err)
		require.Equal(t, base+"/api/gb28181/device-snapshots/uploads/tok/", got)
	}
}
func TestSnapshotUploadURLRejectsMissingOrLoopbackIngress(t *testing.T) {
	for _, base := range []string{"", "http://localhost:5177", "http://[::]:8280"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", nil).WithContext(deviceaccess.WithBaseURL(context.Background(), base))
		_, err := snapshotUploadURL(c, "tok")
		require.Error(t, err)
	}
}
