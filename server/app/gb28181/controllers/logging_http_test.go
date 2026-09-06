package controllers

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/packages"
	"gorm.io/gorm"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/management"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func TestLoggingGBHTTPBusinessResults(t *testing.T) {
	id := 1
	for _, tc := range []struct {
		name         string
		handler      func(*gin.Context)
		status, code int
		success      bool
	}{
		{"cloud_success", func(c *gin.Context) { catalogSuccess(c, 200, gin.H{"fixture": "unchanged"}) }, 200, 0, true},
		{"cloud_failure", func(c *gin.Context) { catalogFailure(c, 502, "fixture") }, 502, 1, false},
		{"cruise_partial", func(c *gin.Context) {
			(&DeviceMgmtController{}).respondCruiseCreate(c, &gbmodels.GbChannel{ChannelID: "channel"}, cruiseTrackCreateRequest{TrackID: &id}, nil, 1, "partial", "fixture", false)
		}, 200, 4200, false},
		{"management_success", func(c *gin.Context) { writeManagementSuccess(c, nil) }, 200, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/fixture", nil)
			tc.handler(c)
			result, ok := response.BusinessResult(c)
			code, success := result.Code, result.Success
			if !ok || code != tc.code || success != tc.success {
				t.Fatalf("metadata = %d/%t/%t, want %d/%t", code, success, ok, tc.code, tc.success)
			}
			var body struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || body.Code != tc.code {
				t.Fatal("response contract changed")
			}
		})
	}
}

func TestLoggingGBHTTPContextWiring(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}, ".", "../favorites", "../cascade/controller")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			t.Fatalf("type-check errors: %v", pkg.Errors)
		}
		for _, f := range pkg.Syntax {
			var fallbackStart, fallbackEnd token.Pos
			if filepath.Base(pkg.Fset.Position(f.Pos()).Filename) == "zlm_management.go" {
				for _, decl := range f.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "requestContext" {
						fallbackStart, fallbackEnd = fn.Pos(), fn.End()
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sig, ok := pkg.TypesInfo.TypeOf(call.Fun).(*types.Signature); ok {
					for i, arg := range call.Args {
						if i >= sig.Params().Len() {
							break
						}
						pt := types.TypeString(sig.Params().At(i).Type(), func(p *types.Package) string { return p.Path() })
						at := pkg.TypesInfo.TypeOf(arg)
						if at != nil && pt == "context.Context" && types.TypeString(at, func(p *types.Package) string { return p.Path() }) == "*github.com/gin-gonic/gin.Context" {
							t.Errorf("Gin context passed to standard context at %s", pkg.Fset.Position(arg.Pos()))
						}
					}
				}
				if s, ok := call.Fun.(*ast.SelectorExpr); ok {
					if id, ok := s.X.(*ast.Ident); ok && pkg.Name == "controllers" && id.Name == "context" && s.Sel.Name == "Background" && !(call.Pos() >= fallbackStart && call.End() <= fallbackEnd) {
						t.Errorf("request background context at %s", pkg.Fset.Position(call.Pos()))
					}
					if receiver, ok := s.X.(*ast.SelectorExpr); ok {
						if id, ok := receiver.X.(*ast.Ident); ok && id.Name == "app" && receiver.Sel.Name == "ZapLog" {
							t.Errorf("global request logger at %s", pkg.Fset.Position(call.Pos()))
						}
					}
				}
				return true
			})
		}
	}
}

type loggingCatalogTrigger struct{ scope context.Context }

func (t *loggingCatalogTrigger) Trigger(ctx context.Context, _, _, _ string) { t.scope = ctx }

func TestLoggingAsyncContextCatalogTrigger(t *testing.T) {
	_, db := newCustomGroupRouter(t)
	device := gbmodels.GbDevice{DeviceID: "34020000002000000001", Status: gbmodels.DeviceStatusOnline, IP: "192.0.2.10", Port: 5060, Transport: "UDP"}
	if err := db.Create(&device).Error; err != nil {
		t.Fatal(err)
	}
	controller := NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	trigger := &loggingCatalogTrigger{}
	controller.SetCatalogTrigger(trigger)
	core, entries := observer.New(zap.InfoLevel)
	parent, cancel := context.WithCancel(logging.WithContext(context.Background(), logging.WithIdentity(zap.New(core), zap.String("request_id", "catalog-request"))))
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/catalog", nil).WithContext(parent)
	c.Params = gin.Params{{Key: "id", Value: strconv.FormatUint(uint64(device.ID), 10)}}
	controller.RefreshDeviceCatalog(c)
	cancel()
	c.Request = httptest.NewRequest("GET", "/reused", nil)
	if trigger.scope == nil || trigger.scope.Err() != nil {
		t.Fatal("catalog work cancelled with request")
	}
	logging.FromContext(trigger.scope, nil).Info("catalog scope")
	if entries.Len() != 1 || entries.All()[0].ContextMap()["request_id"] != "catalog-request" {
		t.Fatal("catalog scope lost correlation")
	}
}

func TestLoggingGBHTTPManagementErrorCode(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/management", nil)
	writeManagementError(c, management.NewValidationError(map[string]string{"fixture": "required"}))
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	result, ok := response.BusinessResult(c)
	if !ok || result.Success || result.StringCode != body.Code || result.StringCode == "" || w.Code != 400 {
		t.Fatal("management error metadata differs from response")
	}
}
