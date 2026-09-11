package controllers

import (
	"bytes"
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"go/ast"
	"go/types"
	"golang.org/x/tools/go/packages"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/passwordhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
	"uvplatform.cn/uvp-gb28181/app/utils/tokenhelper"
)

type loggingConfig map[string]interface{}

func (v loggingConfig) Get(k string) interface{} { return v[k] }
func TestLoggingBaseHTTPFailure(t *testing.T) {
	cfg, err := logging.ParseConfig(loggingConfig{"logs.outputs": []string{"stdout"}, "logs.stdoutformat": "json"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	rt, err := logging.NewRuntime(logging.Options{Config: cfg, Sinks: map[string]zapcore.WriteSyncer{"stdout": zapcore.AddSync(&b)}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	oldLog, oldResponse := app.ZapLog, app.Response
	app.ZapLog = rt.Root
	app.Response = response.NewResponseHandler()
	defer func() { app.ZapLog, app.Response = oldLog, oldResponse }()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil).WithContext(logging.WithContext(context.Background(), logging.WithIdentity(rt.Root, zap.String("request_id", "base-test"))))
	Common{}.Fail(c, "fixture-secret", errors.New("fixture-secret"))
	if !strings.Contains(b.String(), `"request_id":"base-test"`) || !strings.Contains(b.String(), `"event":"http.operation_failed"`) {
		t.Fatalf("failure not correlated: %s", b.String())
	}
	if strings.Contains(b.String(), "fixture-secret") {
		t.Fatal("error leaked")
	}
	b.Reset()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("abort behavior changed")
			}
		}()
		Common{}.FailAndAbort(c, "fixture-secret", errors.New("fixture-secret"))
	}()
	if strings.Count(b.String(), `"event":"http.operation_failed"`) != 1 || strings.Contains(b.String(), "fixture-secret") {
		t.Fatal("aborted failure unsafe or duplicated")
	}
}

// Type-check every production controller and its service calls, so a newly
// registered handler cannot silently pass Gin's non-forwarding Context to DB.
func TestLoggingBaseHTTPContextWiring(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}, ".", "../service", "../models")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			t.Fatalf("type-check errors: %v", pkg.Errors)
		}
		for _, f := range pkg.Syntax {
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
					if id, ok := s.X.(*ast.Ident); ok && pkg.Name == "controllers" && id.Name == "context" && s.Sel.Name == "Background" {
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

type scopedLockCache struct {
	app.CacheInterf
	cancel context.CancelFunc
	scopes []context.Context
	values map[string]string
}

func (c *scopedLockCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	c.scopes = append(c.scopes, ctx)
	c.cancel()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var n int64
	for _, k := range keys {
		if _, ok := c.values[k]; ok {
			n++
		}
	}
	return n, nil
}
func (c *scopedLockCache) Get(ctx context.Context, key string) (string, error) {
	c.scopes = append(c.scopes, ctx)
	return c.values[key], ctx.Err()
}
func (c *scopedLockCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	c.scopes = append(c.scopes, ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	c.values[key] = value
	return nil
}
func TestLoggingBaseHTTPLockAccountingContext(t *testing.T) {
	db, _ := setupLoginControllerTest(t, 1)
	hash, err := passwordhelper.HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.User{BaseModel: models.BaseModel{ID: 7}, Username: "alice", Password: hash, Status: 1}).Error; err != nil {
		t.Fatal(err)
	}
	core, entries := observer.New(zap.InfoLevel)
	root := zap.New(core)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	parent = logging.WithContext(parent, logging.WithIdentity(root, zap.String("request_id", "lock-request")))
	cache := &scopedLockCache{CacheInterf: app.Cache, cancel: cancel, values: map[string]string{}}
	app.Cache = cache
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"alice","password":"wrong-password"}`)).WithContext(parent)
	c.Request.Header.Set("Content-Type", "application/json")
	controller := newAuthControllerWithDependencies(&fakeAuthSessionLifecycle{}, &tokenhelper.TokenService{JWTSecret: "test_secret", TokenExpire: 3600, RefreshExpire: 86400}, time.Hour)
	func() { defer func() { _ = recover() }(); controller.Login(c) }()
	if len(cache.scopes) != 4 {
		t.Fatalf("lock accounting calls %d", len(cache.scopes))
	}
	for _, ctx := range cache.scopes {
		if ctx.Err() != nil {
			t.Error("client cancellation altered lock accounting")
		}
		logging.FromContext(ctx, nil).Info("lock scope")
	}
	for _, entry := range entries.All() {
		if entry.ContextMap()["request_id"] != "lock-request" {
			t.Error("lock accounting lost request scope")
		}
	}
	if cache.values["account_locked:alice"] != "1" {
		t.Fatal("failed login did not lock account")
	}
}
