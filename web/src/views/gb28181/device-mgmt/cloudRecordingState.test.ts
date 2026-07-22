import { describe, expect, it } from "vitest";
import { cloudRecordingStateMeta } from "./cloudRecordingState";

describe("cloudRecordingStateMeta", () => {
    it.each([
        ["disabled", "已关闭", "neutral", false],
        ["starting", "启动中", "warning", true],
        ["recording", "录像中", "success", false],
        ["waiting", "等待设备/流", "warning", false],
        ["stopping", "停止中", "warning", true]
    ])("maps %s", (state, label, tone, loading) => {
        expect(cloudRecordingStateMeta(state)).toMatchObject({ label, tone, loading });
    });

    it("exposes backend error for failed state", () => {
        expect(cloudRecordingStateMeta("failed", "ZLM 不可达")).toMatchObject({
            label: "失败",
            tone: "danger",
            tooltip: "ZLM 不可达"
        });
    });

    it("handles unknown state without throwing", () => {
        expect(cloudRecordingStateMeta("future-state")).toMatchObject({ label: "未知状态", tone: "neutral" });
    });
});
