import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeDetail.vue"), "utf8");

describe("node service config route", () => {
  it("keeps service config focused and does not expose emergency session eviction", () => {
    expect(source).toContain('title="服务配置"');
    expect(source).toContain("NodeConfigView");
    expect(source).toContain("gb28181:zlm:config:update");
    expect(source).not.toContain("NodeRuntimePanel");
    expect(source).not.toContain("StatCard");
    expect(source).not.toContain("ZLMNodeActionDialog");
    expect(source).not.toContain("gb28181:zlm:node:kick");
    expect(source).not.toContain('action="kick"');
    expect(source).not.toContain("紧急操作");
    expect(source).not.toContain("紧急终止全部会话");
    expect(source).not.toContain("NodeForm");
  });

  it("keeps service config inside the shared content shell without a redundant node selector", () => {
    expect(source).toContain("MediaWorkspaceShell");
    expect(source).toContain(':show-scope="false"');
    expect(source).not.toContain('@update:scope="switchNode"');
    expect(source).not.toContain("function switchNode");
    expect(source).toContain(':node-id="nodeId"');
    expect(source).toContain("@dirty-change");
    expect(source).toContain("onBeforeRouteLeave");
    expect(source).toContain("onBeforeRouteUpdate");
    expect(source).toContain("返回节点管理");
  });

  it("uses one compact page heading instead of repeating the service config title", () => {
    expect(source.match(/<h2>服务配置<\/h2>/g) ?? []).toHaveLength(0);
    expect(source).toContain("service-config-toolbar__node");
    expect(source).toContain("<template #heading>");
    expect(source).toContain("热更新项保存后立即生效，重启后生效项保存后需重启媒体节点");
    expect(source).toContain("Secret 不会回显");
  });

  it("keeps invalid node ids explicit instead of silently selecting another node", () => {
    expect(source).toContain("节点地址无效");
    expect(source).not.toContain("nodes[0]");
  });

  it("constrains the detail content so the inner table owns vertical scrolling", () => {
    expect(
      hasRuleBlock(source, ".node-service-config-view", "display: flex", "height: 100%", "min-height: 0", "overflow: hidden")
    ).toBe(true);
  });
});
