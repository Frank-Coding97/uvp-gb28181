import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeDetail.vue"), "utf8");

describe("node service config route", () => {
  it("renders service config as the only route function", () => {
    expect(source).toContain('title="服务配置"');
    expect(source).toContain("NodeConfigView");
    expect(source).toContain("gb28181:zlm:config:update");
    expect(source).not.toContain("NodeRuntimePanel");
    expect(source).not.toContain("StatCard");
    expect(source).not.toContain("getZLMNode");
    expect(source).not.toContain("ZLMNodeActionDialog");
    expect(source).not.toContain("NodeForm");
  });

  it("keeps service config inside the shared content shell", () => {
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).toContain('@update:scope="switchNode"');
    expect(source).toContain("@dirty-change");
    expect(source).toContain("onBeforeRouteLeave");
    expect(source).toContain("onBeforeRouteUpdate");
    expect(source).toContain("返回节点管理");
  });

  it("keeps invalid node ids explicit instead of silently selecting another node", () => {
    expect(source).toContain("节点地址无效");
    expect(source).not.toContain("nodes[0]");
  });
});
