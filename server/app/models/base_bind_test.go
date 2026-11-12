package models

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type baseBindFixture struct {
	ID    int    `form:"id" uri:"id" json:"id"`
	Name  string `form:"name" json:"name"`
	Limit int    `form:"limit" json:"limit"`
}

func newBaseBindContext(method, target, contentType, body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		c.Request.Header.Set("Content-Type", contentType)
	}
	return c
}

func withBaseBindID(c *gin.Context, value string) {
	c.Params = gin.Params{{Key: "id", Value: value}}
}

func TestBaseRequestBindGETAndValidationErrors(t *testing.T) {
	request := &BaseRequest{}

	c := newBaseBindContext(http.MethodGet, "/items/42?name=query&limit=7", "", "")
	withBaseBindID(c, "42")
	var got baseBindFixture
	require.NoError(t, request.Bind(c, &got))
	require.Equal(t, baseBindFixture{ID: 42, Name: "query", Limit: 7}, got)

	queryError := newBaseBindContext(http.MethodGet, "/items/42?limit=bad", "", "")
	withBaseBindID(queryError, "42")
	require.Error(t, request.Bind(queryError, &baseBindFixture{}))

	uriError := newBaseBindContext(http.MethodGet, "/items/not-an-int?limit=7", "", "")
	withBaseBindID(uriError, "not-an-int")
	require.Error(t, request.Bind(uriError, &baseBindFixture{}))
}

func TestBaseRequestBindJSONChecksURIThenBindsBody(t *testing.T) {
	c := newBaseBindContext(http.MethodPost, "/items/42", "application/json", `{"name":"json","limit":9}`)
	withBaseBindID(c, "42")
	var got baseBindFixture
	require.NoError(t, (&BaseRequest{}).Bind(c, &got))
	require.Equal(t, baseBindFixture{ID: 42, Name: "json", Limit: 9}, got)

	badURI := newBaseBindContext(http.MethodPost, "/items/not-an-int", "application/json", `{"name":"json"}`)
	withBaseBindID(badURI, "not-an-int")
	require.Error(t, (&BaseRequest{}).Bind(badURI, &baseBindFixture{}))
}

func TestBaseRequestBindFormAndOtherContentTypes(t *testing.T) {
	form := newBaseBindContext(http.MethodPost, "/items/42?limit=7", "application/x-www-form-urlencoded", "name=form&limit=9")
	withBaseBindID(form, "42")
	var formGot baseBindFixture
	require.NoError(t, (&BaseRequest{}).Bind(form, &formGot))
	require.Equal(t, 42, formGot.ID)
	require.Equal(t, "form", formGot.Name)
	require.Equal(t, 9, formGot.Limit)

	other := newBaseBindContext(http.MethodPost, "/items/42?name=other&limit=7", "text/plain", "ignored")
	withBaseBindID(other, "42")
	var otherGot baseBindFixture
	require.NoError(t, (&BaseRequest{}).Bind(other, &otherGot))
	require.Equal(t, baseBindFixture{ID: 42, Name: "other", Limit: 7}, otherGot)
}
