import { describe, expect, it } from "vitest";
import type { ZLMOwnershipSnapshot, ZLMRecordingResult } from "@/api/gb28181-zlm-runtime";
import {
  buildRecordingTarget,
  recordingImpactItems,
  recordingScheduleQuery,
  recordingStatusPresentation
} from "./recordingRuntimeState";

describe("recording runtime state", () => {
  it("requires and normalizes the complete media identity", () => {
    expect(buildRecordingTarget({ nodeId: 7, schema: " rtsp ", vhost: " __defaultVhost__ ", app: " live ", stream: " camera/1 " })).toEqual({
      target: { nodeId: 7, media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "camera/1" } },
      errors: {}
    });
    expect(buildRecordingTarget({ nodeId: 0, schema: "", vhost: "", app: "", stream: "" }).errors).toEqual({
      nodeId: "请选择媒体节点",
      schema: "请输入 Schema",
      vhost: "请输入 VHost",
      app: "请输入 App",
      stream: "请输入 Stream ID"
    });
  });

  it("renders ownership and plan impacts from the immutable preflight", () => {
    const snapshot: ZLMOwnershipSnapshot = {
      target: { nodeId: 7, media: { schema: "rtsp", vhost: "v", app: "live", stream: "camera/1" } },
      present: true,
      presenceKnown: true,
      status: "owned",
      fingerprint: "fp",
      owners: [
        { type: "recording_plan", confidence: "proven", owner: "夜间计划" },
        { type: "continuous_recording", confidence: "proven", owner: "通道 11" }
      ],
      impacts: [{ resourceType: "recording_session", resourceKey: "session-9", reason: "录像会话正在写入" }]
    };
    const items = recordingImpactItems(snapshot);
    expect(items.join("\n")).toContain("录像计划");
    expect(items.join("\n")).toContain("持续录像");
    expect(items.join("\n")).toContain("session-9");
  });

  it("keeps unknown ownership separate from actual recorder state", () => {
    const result = {
      state: "unknown",
      externalState: "recording",
      recording: true,
      retryable: false
    } as ZLMRecordingResult;
    expect(recordingStatusPresentation(result)).toEqual({
      label: "ZLM 正在录制，手工归属未知",
      tone: "warning",
      readyToStopNormally: false
    });
  });

  it("carries only node and stream context to the existing schedule page", () => {
    expect(recordingScheduleQuery({
      nodeId: 7,
      media: { schema: "rtsp", vhost: "__defaultVhost__", app: "live", stream: "34020000001320000001" }
    })).toEqual({ nodeId: "7", stream: "34020000001320000001" });
  });
});
