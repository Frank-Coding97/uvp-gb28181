package routes

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

var cascadeSnapshotUpload struct {
	sync.RWMutex
	handler gin.HandlerFunc
}

func SetCascadeSnapshotUploadHandler(handler gin.HandlerFunc) {
	cascadeSnapshotUpload.Lock()
	defer cascadeSnapshotUpload.Unlock()
	cascadeSnapshotUpload.handler = handler
}

func serveCascadeSnapshotUpload(c *gin.Context) {
	cascadeSnapshotUpload.RLock()
	handler := cascadeSnapshotUpload.handler
	cascadeSnapshotUpload.RUnlock()
	if handler == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	handler(c)
}
