package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterHookRoutesIncludesStreamNotFoundFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterHookRoutes(engine)

	body := []byte(`{"mediaServerId":"node-a","vhost":"__defaultVhost__","app":"rtp","schema":"fmp4","stream":"37010301021320000014_37010301021320000001","params":"?uvp_play_token=token"}`)
	req := httptest.NewRequest(http.MethodPost, "/index/hook/on_stream_not_found?cap=capability", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
	require.EqualValues(t, -1, payload["code"])
}
