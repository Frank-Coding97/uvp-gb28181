//go:build windows

package launcher

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func t20AssertBrowserCapabilities(t *testing.T, browser *t18EdgeBrowser, origin, installDir string) {
	t.Helper()
	for _, page := range []struct{ route, text, name string }{
		{"/security-preview", "系统防火墙不支持，应用层继续拦截", "security"},
		{"/system/codegen", "Windows 单机版不支持开发代码生成", "codegen"},
		{"/system/pluginsmanager", "Windows 单机版不支持插件结构管理", "plugins"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := browser.navigate(ctx, origin+"/#"+page.route); err != nil {
			cancel()
			t.Fatal("capability page navigation failed")
		}
		encoded, _ := json.Marshal(page.text)
		if err := t19WaitEval(ctx, browser.cdp, "document.body.innerText.includes("+string(encoded)+")"); err != nil {
			t18LogBrowserState(t, browser.cdp)
			cancel()
			t.Fatalf("capability state unavailable: %s", page.name)
		}
		if err := t19WaitEval(ctx, browser.cdp, `!document.getAnimations().some(animation => animation.playState === 'running' && animation.effect && animation.effect.getTiming().iterations !== Infinity)`); err != nil {
			cancel()
			t.Fatalf("capability page transition did not settle: %s", page.name)
		}
		state, err := browser.cdp.evalState(ctx)
		if err != nil || state.ServerErrorToastVisible || state.PermissionDeniedVisible {
			cancel()
			t.Fatalf("capability page reported an API error: %s", page.name)
		}
		if page.name != "security" {
			expression := `!performance.getEntriesByType('resource').some(entry => { const path = new URL(entry.name).pathname; return ['/api/codegen/','/api/sysGen/','/api/pluginsmanager/'].some(prefix => path.startsWith(prefix)); })`
			if err := browser.cdp.evalBool(ctx, expression); err != nil {
				cancel()
				t.Fatal("unsupported developer page requested a structure API")
			}
		}
		if err := browser.captureScreenshot(ctx, filepath.Join(installDir, "t20-"+page.name+".png")); err != nil {
			cancel()
			t.Fatal("capability screenshot unavailable")
		}
		cancel()
	}
}
