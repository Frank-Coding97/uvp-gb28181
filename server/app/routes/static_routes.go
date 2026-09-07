package routes

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type staticRouteConfig struct {
	WebRoot    string
	PublicPath string
	PublicRoot string
	UploadRoot string
}

func registerConfiguredStaticRoutes(engine *gin.Engine) {
	publicRoot := app.ConfigYml.GetString("httpserver.serverroot")
	uploadRoot := filepath.Join(publicRoot, "uploads")
	if app.UploadPath != "" {
		uploadRoot = app.UploadPath
	}
	registerStaticRoutes(engine, staticRouteConfig{
		WebRoot:    app.WebPath,
		PublicPath: app.ConfigYml.GetString("httpserver.serverrootpath"),
		PublicRoot: publicRoot,
		UploadRoot: uploadRoot,
	})
}

func registerStaticRoutes(engine *gin.Engine, config staticRouteConfig) {
	config.PublicPath = normalizeStaticRoutePath(config.PublicPath)
	engine.GET(config.PublicPath+"/*filepath", servePublicFile(config))
	engine.HEAD(config.PublicPath+"/*filepath", servePublicFile(config))
	engine.NoRoute(serveWebFileOrNotFound(config))
}

func normalizeStaticRoutePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/public"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	value = strings.TrimRight(value, "/")
	if value == "" {
		return "/public"
	}
	return value
}

func servePublicFile(config staticRouteConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}

		requestPath, ok := decodedRequestPath(c.Request)
		if !ok || requestPath == config.PublicPath {
			c.Status(http.StatusNotFound)
			return
		}
		relative, ok := routeRelativePath(requestPath, config.PublicPath)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}

		root := config.PublicRoot
		if relative == "uploads" || strings.HasPrefix(relative, "uploads/") {
			root = config.UploadRoot
			if relative == "uploads" {
				relative = ""
			} else {
				relative = strings.TrimPrefix(relative, "uploads/")
			}
		}
		file, err := resolveStaticFile(root, relative)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(file)
	}
}

func serveWebFileOrNotFound(config staticRouteConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}

		requestPath, ok := decodedRequestPath(c.Request)
		if !ok || isNonSPARoute(requestPath, config.PublicPath) {
			c.Status(http.StatusNotFound)
			return
		}

		relative := strings.TrimPrefix(requestPath, "/")
		if file, err := resolveStaticFile(config.WebRoot, relative); err == nil {
			c.File(file)
			return
		}

		if !isPageRequest(requestPath, c.Request) {
			c.Status(http.StatusNotFound)
			return
		}
		index, err := resolveStaticFile(config.WebRoot, "index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(index)
	}
}

func decodedRequestPath(request *http.Request) (string, bool) {
	if request == nil || request.URL == nil {
		return "", false
	}
	escaped := request.URL.EscapedPath()
	decoded, err := url.PathUnescape(escaped)
	if err != nil || decoded == "" || !strings.HasPrefix(decoded, "/") {
		return "", false
	}
	if strings.ContainsAny(decoded, "\x00\\") {
		return "", false
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return "", false
		}
		// A colon is not valid in a URL path component for this package and
		// would otherwise permit Windows alternate data stream syntax.
		if strings.ContainsRune(segment, ':') {
			return "", false
		}
	}
	return decoded, true
}

func routeRelativePath(requestPath, routePath string) (string, bool) {
	if requestPath == routePath {
		return "", true
	}
	prefix := routePath + "/"
	if !strings.HasPrefix(requestPath, prefix) {
		return "", false
	}
	relative := strings.TrimPrefix(requestPath, prefix)
	if relative == "" {
		return "", false
	}
	return relative, true
}

func isNonSPARoute(requestPath, publicPath string) bool {
	for _, prefix := range []string{
		"/api",
		"/index/hook",
		"/index/api",
		"/swagger",
		"/debug",
		"/viewCache",
		"/hls",
		"/rtsp",
		"/rtc",
		"/live",
		"/ws",
		"/data",
		"/config",
		"/logs",
		"/resource",
		"/run",
		"/backups",
		"/recordings",
		publicPath,
	} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}

func acceptsHTML(request *http.Request) bool {
	return strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/html")
}

func isPageRequest(requestPath string, request *http.Request) bool {
	if requestPath == "/" {
		return true
	}
	if !acceptsHTML(request) {
		return false
	}
	lastSlash := strings.LastIndexByte(requestPath, '/')
	return !strings.Contains(requestPath[lastSlash+1:], ".")
}

func resolveStaticFile(root, relative string) (string, error) {
	if strings.TrimSpace(root) == "" || relative == "" || strings.HasPrefix(relative, "/") {
		return "", errors.New("invalid static file path")
	}
	if strings.ContainsAny(relative, "\x00\\") {
		return "", errors.New("invalid static file path")
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidateAbs := filepath.Join(rootAbs, filepath.FromSlash(relative))
	if !withinStaticRoot(rootAbs, candidateAbs) {
		return "", errors.New("static file escapes root")
	}

	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	candidateReal, err := filepath.EvalSymlinks(candidateAbs)
	if err != nil || !withinStaticRoot(rootReal, candidateReal) {
		return "", errors.New("static file escapes resolved root")
	}
	info, err := os.Stat(candidateReal)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("static target is not a regular file")
	}
	return candidateReal, nil
}

func withinStaticRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
