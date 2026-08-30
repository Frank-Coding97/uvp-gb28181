import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeDetail.vue"), "utf8");

describe("canonical node detail", () => {
  it("supports overview/runtime/config without rewriting a missing initial view", () => {
    for (const view of ["overview", "runtime", "config"]) expect(source).toContain(view);
    expect(source).toContain("resolveNodeDetailView");
    expect(source).not.toMatch(/watch\([^\n]*route\.query\.view[\s\S]{0,500}router\.replace/);
  });

  it("uses route id runtime and the shared config/recovery action components", () => {
    expect(source).toContain("NodeRuntimePanel");
    expect(source).toContain("NodeConfigView");
    expect(source).toContain("ZLMNodeActionDialog");
    expect(source).toContain("getZLMNode");
    expect(source).toContain("gb28181:zlm:config:update");
    expect(source).toContain("activateZLMNode");
    expect(source).toContain("testZLMNodeConnection");
    expect(source).toContain("openAction('maintenance')");
    expect(source).toContain("runtimeVisited");
    expect(source).toContain("configVisited");
    expect(source).toContain("@dirty-change");
    expect(source).toContain("onBeforeRouteLeave");
    expect(source).toContain("onBeforeRouteUpdate");
    expect(source).toContain("v-show=\"currentView === 'config'\"");
  });

  it("keeps unauthorized detail explicit instead of silently selecting another node", () => {
    expect(source).toContain("没有节点详情权限");
    expect(source).toContain("返回节点列表");
    expect(source).not.toContain("nodes[0]");
  });
});
