package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

func TestLoggingBaseHTTPModels(t *testing.T) {
	t.Run("treeLogsKeepRequestCorrelation", func(t *testing.T) {
		logger, observed := modelTestLogger(t)
		ctx := logging.WithContext(context.Background(), logger)

		departmentParent := uint(10)
		departmentCycle := uint(20)
		departments := SysDepartmentList{
			{BaseModel: BaseModel{ID: 10}, Name: "root", Sort: intPtr(1)},
			{BaseModel: BaseModel{ID: 11}, ParentID: &departmentParent, Name: "child", Sort: intPtr(2)},
			{BaseModel: BaseModel{ID: 20}, ParentID: &departmentCycle, Name: "cycle"},
			{BaseModel: BaseModel{ID: 30}, ParentID: uintPtr(99), Name: "orphan"},
		}
		roots := departments.BuildTree(ctx)
		if len(roots) != 1 || roots[0].ID != 10 || len(roots[0].Children) != 1 || roots[0].Children[0].ID != 11 {
			t.Fatalf("department tree changed: %#v", roots)
		}
		assertModelEvents(t, observed, "models.sysdepartment.tree_cycle", "models.sysdepartment.tree_orphan", map[string]any{
			"request_id": "models-test-request",
		})
	})

	t.Run("menuAndRoleTreesKeepBehavior", func(t *testing.T) {
		logger, observed := modelTestLogger(t)
		ctx := logging.WithContext(context.Background(), logger)

		menus := SysMenuList{
			{BaseModel: BaseModel{ID: 1}, ParentID: 0, Name: "root", Sort: 1},
			{BaseModel: BaseModel{ID: 2}, ParentID: 1, Name: "child", Sort: 2},
			{BaseModel: BaseModel{ID: 3}, ParentID: 3, Name: "cycle"},
			{BaseModel: BaseModel{ID: 4}, ParentID: 99, Name: "orphan"},
		}
		menuRoots := menus.BuildTree(ctx)
		if len(menuRoots) != 1 || menuRoots[0].ID != 1 || len(menuRoots[0].Children) != 1 || menuRoots[0].Children[0].ID != 2 {
			t.Fatalf("menu tree changed: %#v", menuRoots)
		}

		roles := SysRoleList{
			{BaseModel: BaseModel{ID: 10}, ParentID: 0, Name: "root", Sort: 1},
			{BaseModel: BaseModel{ID: 11}, ParentID: 10, Name: "child", Sort: 2},
			{BaseModel: BaseModel{ID: 12}, ParentID: 12, Name: "cycle"},
			{BaseModel: BaseModel{ID: 13}, ParentID: 99, Name: "orphan"},
		}
		roleRoots := roles.BuildTree(ctx)
		if len(roleRoots) != 1 || roleRoots[0].ID != 10 || len(roleRoots[0].Children) != 1 || roleRoots[0].Children[0].ID != 11 {
			t.Fatalf("role tree changed: %#v", roleRoots)
		}
		assertModelEvents(t, observed, "models.sysmenu.tree_cycle", "models.sysmenu.tree_orphan", map[string]any{
			"request_id": "models-test-request",
		})
		assertModelEvents(t, observed, "models.sysrole.tree_cycle", "models.sysrole.tree_orphan", map[string]any{
			"request_id": "models-test-request",
		})
	})

	t.Run("areaLoadFailureIsSafeAndScoped", func(t *testing.T) {
		logger, observed := modelTestLogger(t)
		ctx := logging.WithContext(context.Background(), logger)
		oldWorkingDirectory, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		temporaryDirectory := t.TempDir()
		if err := os.Chdir(temporaryDirectory); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(oldWorkingDirectory) })

		oldOnce, oldInstance := once, instance
		once, instance = sync.Once{}, nil
		t.Cleanup(func() { once, instance = oldOnce, oldInstance })

		if got := GetAreaListInstance(ctx); !got.IsEmpty() {
			t.Fatalf("missing area file returned %#v", got)
		}
		assertModelEvents(t, observed, "models.area.load_failed", map[string]any{
			"request_id": "models-test-request",
		})
		for _, entry := range observed.All() {
			if entry.ContextMap()["event"] != "models.area.load_failed" {
				continue
			}
			if value, ok := entry.ContextMap()["error"]; ok {
				errorText := fmt.Sprint(value)
				if filepath.IsAbs(errorText) || containsAny(errorText, "no such file", "resource/public/area/area.json") {
					t.Fatalf("area error exposed unsafe detail: %s", errorText)
				}
			}
		}
	})
}

func modelTestLogger(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()
	previous := app.ZapLog
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() { app.ZapLog = previous })
	core, observed := observer.New(zap.DebugLevel)
	return zap.New(core).With(zap.String("request_id", "models-test-request")), observed
}

func assertModelEvents(t *testing.T, observed *observer.ObservedLogs, events ...any) {
	t.Helper()
	wantFields := map[string]any{}
	if len(events) > 0 {
		if last, ok := events[len(events)-1].(map[string]any); ok {
			wantFields = last
			events = events[:len(events)-1]
		}
	}
	wanted := make(map[string]bool, len(events))
	for _, event := range events {
		name, ok := event.(string)
		if !ok {
			t.Fatalf("event expectation must be string, got %T", event)
		}
		wanted[name] = false
	}
	for _, entry := range observed.All() {
		eventName, _ := entry.ContextMap()["event"].(string)
		if _, ok := wanted[eventName]; !ok {
			continue
		}
		fields := entry.ContextMap()
		for key, value := range wantFields {
			if fields[key] != value {
				t.Errorf("event %s field %s = %v, want %v", entry.Message, key, fields[key], value)
			}
		}
		wanted[eventName] = true
	}
	for event, found := range wanted {
		if !found {
			t.Errorf("event %s not found in %v", event, observed.All())
		}
	}
}

func intPtr(value int) *int { return &value }

func uintPtr(value uint) *uint { return &value }

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
