package controllers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/response"
	"uvplatform.com/uvp-gb28181/internal/httpsconfig"
)

func TestHTTPSUnsupportedEnvironmentCannotApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := app.Response
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() { app.Response = old })
	c := &HTTPSManagementController{manager: httpsconfig.New("", "darwin")}
	r := gin.New()
	r.POST("/apply", c.Apply)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/apply", strings.NewReader("anything")))
	require.NotEqual(t, 200, w.Code)
}
func TestHTTPSCertificateRejectsMismatchedOrOversizedUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := app.Response
	app.Response = response.NewResponseHandler()
	t.Cleanup(func() { app.Response = old })
	c := &HTTPSManagementController{manager: httpsconfig.New("", "darwin")}
	for _, size := range []int{8, httpsconfig.MaxMaterialBytes + 1} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for _, field := range []string{"certificate", "privateKey"} {
			part, err := writer.CreateFormFile(field, "../../secret.pem")
			require.NoError(t, err)
			_, err = part.Write(bytes.Repeat([]byte("a"), size))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		r := gin.New()
		r.POST("/certificate", c.Certificate)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/certificate", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.NotEqual(t, 200, w.Code)
		require.NotContains(t, w.Body.String(), "aaaa")
	}
}
