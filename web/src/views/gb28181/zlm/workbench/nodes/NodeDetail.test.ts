import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeDetail.vue"), "utf8");

describe("node service config route", () => {
  it("keeps restart in service config and moves emergency eviction into node detail", () => {
    expect(source).toContain('title="服务配置"');
    expect(source).toContain("NodeConfigView");
    expect(source).toContain("gb28181:zlm:config:update");
    expect(source).not.toContain("NodeRuntimePanel");
    expect(source).not.toContain("StatCard");
    expect(source).toContain("ZLMNodeActionDialog");
    expect(source).toContain("gb28181:zlm:node:kick");
    expect(source).toContain('action="kick"');
    expect(source).toContain("紧急终止全部会话");
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
    expect(source).toContain('<template #heading>');
    expect(source).toContain("只允许热更新项进入提交");
    expect(source).toContain("Secret 不会回显");
  });

  it("keeps invalid node ids explicit instead of silently selecting another node", () => {
    expect(source).toContain("节点地址无效");
    expect(source).not.toContain("nodes[0]");
  });
});
