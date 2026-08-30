import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeListPanel.vue"), "utf8");

describe("NodeListPanel", () => {
  it("keeps node governance controls and independent permissions in the panel", () => {
    expect(source).toContain("testZLMNodeConnection");
    expect(source).toContain("activateZLMNode");
    expect(source).toContain("ZLMNodeActionDialog");
    expect(source).toContain("gb28181:zlm:node:manage");
    expect(source).toContain("gb28181:zlm:node:kick");
    expect(source).toContain("gb28181:zlm:restart");
    expect(source).toContain("recoveryRequired");
    expect(source).toContain("autoOnDemandReady");
  });

  it("accepts canonical scope data and does not use a guessed node for writes", () => {
    expect(source).toContain("defineProps");
    expect(source).toContain("scope");
    expect(source).toContain("明确选择节点");
    expect(source).toMatch(/setScope|nodeId/);
    expect(source).not.toContain("nodes[0]");
  });

  it("refreshes from the parent scope and retains the last successful rows on errors", () => {
    expect(source).toContain("emit(\"refresh\")");
    expect(source).toContain("上一次节点列表");
    expect(source).toContain("impact");
    expect(source).toContain("fingerprint");
  });

  it("does not repeat cluster summary cards above the node table", () => {
    expect(source).not.toContain("StatCard");
    expect(source).not.toContain('class="kpi-row"');
  });
});
