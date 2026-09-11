import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeConfigView.vue"), "utf8");

describe("NodeConfigView", () => {
  it("reuses the existing config panel and freezes node switching while drafts exist", () => {
    expect(source).toContain("NodeConfigPanel");
    expect(source).toContain("dirty-change");
    expect(source).toContain("configDirty");
    expect(source).toContain("草稿");
    expect(source).toContain("不会回显");
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
});
