import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { ZLM_WORKSPACES } from "./mediaRoutes";

const root = resolve(process.cwd(), "src/views/gb28181/zlm/workbench");

describe("single-menu ZLM workbench", () => {
  it("groups ZLM operations into five system-sidebar entries and keeps recordings outside", () => {
    expect(ZLM_WORKSPACES.map(item => [item.key, item.title, item.path])).toEqual([
      ["overview", "运行总览", "/media/overview"],
      ["monitoring", "流与会话", "/media/monitoring"],
      ["ingress", "接入管理", "/media/ingress"],
      ["nodes", "节点管理", "/media/nodes"],
      ["scheduling", "调度管理", "/media/scheduling"]
    ]);
    expect(ZLM_WORKSPACES.map(item => item.key)).not.toContain("recordings");
  });

  it("leaves primary navigation to the system sidebar and keeps only local views in the shell", () => {
    const shell = readFileSync(resolve(root, "MediaWorkspaceShell.vue"), "utf8");
    const scopeAt = shell.indexOf("<MediaScopeBar");
    const secondaryTabsAt = shell.indexOf("media-workspace-shell__tabs");
    const panelAt = shell.indexOf('data-shell-block="panel"');

    expect(scopeAt).toBeGreaterThan(0);
    expect(secondaryTabsAt).toBeGreaterThan(scopeAt);
    expect(panelAt).toBeGreaterThan(secondaryTabsAt);
    expect(shell).not.toContain("MediaWorkspaceTabs");
    expect(shell).not.toContain("media-workspace-shell__sidebar");
  });

  it("preserves one selected node while switching local views", () => {
    const route = readFileSync(resolve(root, "useMediaWorkspaceRoute.ts"), "utf8");
    const systemMenu = readFileSync(resolve(process.cwd(), "src/layout/components/Menu/index.vue"), "utf8");

    expect(route).toContain("resolveDefaultZLMNodeId");
    expect(route).toContain("contextStore.selectNode");
    expect(route).toContain("query: { ...route.query, nodeId: String(next) }");
    expect(route).toContain("router.replace({ path: definition.path, query: { ...route.query, view } })");
    expect(route).not.toContain("router.push({ path: definition.path, query: { view } })");
    expect(systemMenu).toContain('path.startsWith("/media/")');
    expect(systemMenu).toContain("query: { nodeId }");
  });

  it("uses one stable outer tab identity for every workbench sub-route", () => {
    const routeStore = readFileSync(resolve(process.cwd(), "src/store/modules/route-config.ts"), "utf8");
    expect(routeStore).toContain("resolveMediaWorkbenchTabGroup");
    expect(routeStore).toContain('title: "流媒体管理"');
  });

  it("makes the overview a concrete-node runtime screen", () => {
    const overview = readFileSync(resolve(root, "MediaOverview.vue"), "utf8");
    expect(overview).toContain("RuntimeSummaryPanel");
    expect(overview).toContain(':requires-node="true"');
    expect(overview).toContain(':allow-all="false"');
    expect(overview).toContain(":node-id=");
  });
});
