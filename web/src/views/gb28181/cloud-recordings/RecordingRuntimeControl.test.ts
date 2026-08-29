import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cloud-recordings/components/RecordingRuntimeControl.vue"), "utf8");

describe("recording runtime control", () => {
  it("uses only typed UVP recording APIs and the complete media identity", () => {
    expect(source).toContain("getZLMRecordingStatus");
    expect(source).toContain("preflightStartZLMRecording");
    expect(source).toContain("startZLMRecording");
    expect(source).toContain("preflightStopZLMRecording");
    expect(source).toContain("stopZLMRecording");
    expect(source).toContain("preflightForceStopZLMRecording");
    expect(source).toContain("forceStopZLMRecording");
    expect(source).toContain("Schema");
    expect(source).toContain("VHost");
    expect(source).toContain("Stream ID");
    expect(source).not.toMatch(/\/index\/api\//i);
    expect(source).not.toMatch(/apiSecret|[?&]secret=/i);
  });

  it("separates normal control from force stop permission and confirmation", () => {
    expect(source).toContain('gb28181:recording:control');
    expect(source).toContain('gb28181:recording:force-stop');
    expect(source).toContain("ZLMDangerActionDialog");
    expect(source).toContain("普通停止仅释放当前账号创建的手工录制，不会关闭录像计划或持续录像");
    expect(source).toContain("强制停止必须单独授权、填写理由并重新确认影响指纹");
  });

  it("labels HLS as runtime-only and reuses the existing schedule page", () => {
    expect(source).toContain("HLS 仅为节点运行态分片能力，不进入云录像文件目录");
    expect(source).toContain("/gb28181/recording-schedules");
    expect(source).toContain("recordingScheduleQuery");
    expect(source).toContain("router.push");
  });
});
