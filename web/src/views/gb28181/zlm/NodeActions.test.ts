import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  isNodeImpactPreflight,
  nodeActionCopy,
  nodeImpactItems,
  nodeImpactItemsFromError,
  type NodeImpactPreflightLike
} from "./nodeActionState";

const preflight: NodeImpactPreflightLike = {
  nodeId: 7,
  action: "maintenance",
  impact: { streams: 3, recordings: 2, sessions: 5, truncated: true },
  fingerprint: "fp-7",
  observedAt: "2026-08-30T00:00:00Z"
};

describe("node impact actions", () => {
  it("only accepts the backend fingerprint for the exact node and action", () => {
    expect(isNodeImpactPreflight(preflight, 7, "maintenance")).toBe(true);
    expect(isNodeImpactPreflight({ ...preflight, fingerprint: "" }, 7, "maintenance")).toBe(false);
    expect(isNodeImpactPreflight(preflight, 8, "maintenance")).toBe(false);
    expect(isNodeImpactPreflight(preflight, 7, "kick")).toBe(false);
  });

  it("renders every backend impact count and truncation warning without guessing safety", () => {
    expect(nodeImpactItems(preflight)).toEqual([
      "活动流 3 路",
      "录制任务 2 个",
      "网络会话 5 个",
      "影响结果已截断，实际数量可能更多"
    ]);
  });

  it("uses an action-specific immutable confirmation phrase", () => {
    expect(nodeActionCopy("delete", "zlm-a")).toMatchObject({ confirmPhrase: "删除 zlm-a", actionLabel: "删除节点" });
  });

  it("shows backend conflict impacts when the error envelope supplies them", () => {
    expect(nodeImpactItemsFromError({
      response: { data: { data: { impact: { streams: 4, recordings: 1, sessions: 6, truncated: false } } } }
    })).toEqual(["活动流 4 路", "录制任务 1 个", "网络会话 6 个"]);
  });

  it("wires both node pages through the shared danger dialog instead of native confirmation", () => {
    const list = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeList.vue"), "utf8");
    const detail = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeDetail.vue"), "utf8");
    const dialog = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/ZLMNodeActionDialog.vue"), "utf8");
    expect(list).toContain("ZLMNodeActionDialog");
    expect(detail).toContain("ZLMNodeActionDialog");
    expect(list).not.toContain("Modal.warning");
    expect(detail).not.toContain("Modal.warning");
    expect(dialog).toContain("ZLMDangerActionDialog");
    expect(dialog).toContain("fingerprint");
    expect(list).toContain("gb28181:zlm:node:manage");
    expect(list).toContain("gb28181:zlm:node:kick");
    expect(detail).toContain("gb28181:zlm:restart");
  });
});
