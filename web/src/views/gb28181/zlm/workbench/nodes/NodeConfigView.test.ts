import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeConfigView.vue"), "utf8");

describe("NodeConfigView", () => {
  it("reuses the existing config panel and freezes node switching while drafts exist", () => {
    expect(source).toContain("NodeConfigPanel");
    expect(source).toContain("dirty-change");
    expect(source).toContain("configDirty");
    expect(source).toContain("草稿");
    expect(source).toContain("不会回显");
    expect(source).toContain("热更新项保存后立即生效，重启后生效项保存后需重启媒体节点");
    expect(source).toContain('<slot name="heading">');
  });

  it("keeps the accepted restart state machine visible until ready or failed", () => {
    for (const state of ["accepted", "waiting_offline", "waiting_heartbeat", "converging", "ready", "failed", "unknown"]) {
      expect(source).toContain(state);
    }
    expect(source).toContain("getZLMNodeRestartStatus");
    expect(source).toContain("restartZLMNode");
    expect(source).toContain("后端已受理，不代表节点已经恢复");
    expect(source).not.toMatch(/restartZLMNode[\s\S]{0,500}Message\.success/);
  });

  it("fills the workspace without creating an outer scrollbar", () => {
    expect(hasRuleBlock(source, ".node-config-view", "display: flex", "height: 100%", "min-height: 0", "overflow: hidden")).toBe(
      true
    );
  });

  it("keeps the restart spinner inline instead of letting it squeeze the status text", () => {
    // 回归（2026-10-03）：zlm-tokens.css 的「Arco-Design 全局自适应兜底」给所有
    // .arco-spin 设了 display:block + width:100%（为包裹布局容器的场景兜底）。
    // 重启状态区是 flex 行，满宽 spin 会吃掉整行，把 flex:1/min-width:0 的状态文字
    // 压成 0 宽 —— 表现为文字逐字竖排、区块高度被撑到 429px。
    // 必须在本页把它恢复成内容尺寸。
    expect(hasRuleBlock(source, ".restart-state :deep(.arco-spin)", "flex: none", "width: auto")).toBe(true);
  });
});
