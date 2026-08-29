import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildFFmpegCreateRequest,
  ffmpegCreateDecision,
  ffmpegURLText
} from "./ffmpegSourcesState";

describe("FFmpeg source state", () => {
  it("accepts only a listed template and typed parameters", () => {
    const result = buildFFmpegCreateRequest({
      templateKey: "ffmpeg.cmd_hd",
      srcUrl: "rtsp://camera.example/live",
      dstUrl: "rtmp://media.example/live/camera",
      timeoutMs: "3000",
      enableHls: true,
      enableMp4: false
    }, ["ffmpeg.cmd_hd"]);
    expect(result.errors).toEqual({});
    expect(result.request).toMatchObject({ templateKey: "ffmpeg.cmd_hd", timeoutMs: 3000, enableHls: true, enableMp4: false });

    const invalid = buildFFmpegCreateRequest({
      templateKey: "ffmpeg.unknown",
      srcUrl: "rtsp://camera.example/live",
      dstUrl: "rtmp://media.example/live/camera",
      timeoutMs: "3000.5",
      enableHls: false,
      enableMp4: false
    }, ["ffmpeg.cmd_hd"]);
    expect(invalid.request).toBeUndefined();
    expect(invalid.errors).toMatchObject({ templateKey: expect.any(String), timeoutMs: expect.any(String) });
  });

  it("distinguishes unsupported, unknown and missing-template disable reasons", () => {
    expect(ffmpegCreateDecision("supported", ["ffmpeg.cmd_hd"], true)).toMatchObject({ allowed: true });
    expect(ffmpegCreateDecision("unsupported", ["ffmpeg.cmd_hd"], true)).toMatchObject({ allowed: false, reason: expect.stringContaining("不支持") });
    expect(ffmpegCreateDecision("unknown", ["ffmpeg.cmd_hd"], true)).toMatchObject({ allowed: false, reason: expect.stringContaining("未探测") });
    expect(ffmpegCreateDecision("supported", [], true)).toMatchObject({ allowed: false, reason: expect.stringContaining("模板") });
  });

  it("renders only safe URL summaries", () => {
    expect(ffmpegURLText({ summary: "rtsp://camera.example", fingerprint: "sha256" })).toBe("rtsp://camera.example");
  });

  it("contains no shell command input or raw URL disclosure surface", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/FFmpegSources.vue"), "utf8");
    const form = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/FFmpegSourceForm.vue"), "utf8");
    expect(source).toContain("listZLMFFmpegSources");
    expect(source).toContain("preflightDeleteZLMFFmpegSource");
    expect(source).toContain("useZLMRuntimePolling");
    expect(form).toContain("templateKey");
    expect(form).not.toMatch(/shell|command|命令文本|textarea/i);
    expect(source).not.toContain(":title=");
    expect(source).not.toContain("index/api");
  });
});
