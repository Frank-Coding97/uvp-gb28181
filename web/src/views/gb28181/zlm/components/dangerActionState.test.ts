import { describe, expect, it } from "vitest";

import { createDangerActionSnapshot, dangerActionSnapshotMatches } from "./dangerActionState";

describe("ZLM danger action snapshot", () => {
  it("captures the opening node, target, impact, reason policy and confirmation phrase", () => {
    const snapshot = createDangerActionSnapshot({
      contextVersion: 4,
      nodeId: 7,
      nodeName: "边缘节点 A",
      targetKey: "rtsp/__defaultVhost__/live/camera",
      targetLabel: "live/camera",
      impacts: ["中断 3 个观看会话", "停止 MP4 录像"],
      confirmPhrase: "关闭 live/camera",
      requireReason: true
    });

    expect(snapshot).toMatchObject({
      contextVersion: 4,
      nodeId: 7,
      nodeName: "边缘节点 A",
      targetLabel: "live/camera",
      impacts: ["中断 3 个观看会话", "停止 MP4 录像"],
      confirmPhrase: "关闭 live/camera",
      requireReason: true
    });
  });

  it("requires reopening when node context or target changes", () => {
    const snapshot = createDangerActionSnapshot({
      contextVersion: 4,
      nodeId: 7,
      nodeName: "边缘节点 A",
      targetKey: "target-a",
      targetLabel: "目标 A",
      impacts: [],
      confirmPhrase: "确认",
      requireReason: false
    });

    expect(dangerActionSnapshotMatches(snapshot, { contextVersion: 4, nodeId: 7, targetKey: "target-a" })).toBe(true);
    expect(dangerActionSnapshotMatches(snapshot, { contextVersion: 5, nodeId: 8, targetKey: "target-a" })).toBe(false);
    expect(dangerActionSnapshotMatches(snapshot, { contextVersion: 4, nodeId: 7, targetKey: "target-b" })).toBe(false);
  });

  it("requires reopening when the impact fingerprint changes", () => {
    const snapshot = createDangerActionSnapshot({
      contextVersion: 4,
      nodeId: 7,
      nodeName: "边缘节点 A",
      targetKey: "target-a",
      targetLabel: "目标 A",
      fingerprint: "fingerprint-before",
      impacts: [],
      confirmPhrase: "确认",
      requireReason: false
    });

    expect(dangerActionSnapshotMatches(snapshot, {
      contextVersion: 4,
      nodeId: 7,
      targetKey: "target-a",
      fingerprint: "fingerprint-after"
    })).toBe(false);
  });
});
