import { afterEach, describe, expect, it, vi } from "vitest";
import {
  evaluateProtocolCompatibility,
  readBrowserCodecCapabilities,
  type BrowserCodecCapabilities,
  type PlaybackCodecTrack
} from "./playbackCompatibility";

const rtc: BrowserCodecCapabilities = {
  webrtc: true,
  video: [{ mimeType: "video/H264", clockRate: 90000 }],
  audio: [
    { mimeType: "audio/opus", clockRate: 48000, channels: 2 },
    { mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }
  ]
};
const video = (codec = "H265"): PlaybackCodecTrack => ({ kind: "video", codec, ready: true });
const audio = (codec = "PCMA", sampleRate = 8000): PlaybackCodecTrack => ({
  kind: "audio",
  codec,
  ready: true,
  sampleRate
});

afterEach(() => vi.unstubAllGlobals());

describe("播放协议编码兼容性", () => {
  it("浏览器明确缺少 H265 接收能力时禁用 WebRTC 并解释原因", () => {
    const result = evaluateProtocolCompatibility("webrtc", [video()], rtc, true);
    expect(result.disabledReason).toContain("H265");
    expect(result.disabledReason).toContain("WebRTC");
    expect(result.disabledReason).toContain("WS-FLV");
  });

  it.each(["H265", "H.265", "HEVC", "hvc1.1.6.L93.B0"])("浏览器支持 H265 时允许 %s WebRTC", codec => {
    const capabilities = { ...rtc, video: [...rtc.video!, { mimeType: "video/H265", clockRate: 90000 }] };
    expect(evaluateProtocolCompatibility("webrtc", [video(codec)], capabilities, true).disabledReason).toBeUndefined();
  });

  it.each(["H264", "H.264", "AVC", "avc1.640028"])("识别 %s 别名并允许对应 WebRTC", codec => {
    expect(evaluateProtocolCompatibility("webrtcs", [video(codec)], rtc, true).disabledReason).toBeUndefined();
  });

  it("能力缺失、编码未知、轨道未 ready 时保留尝试入口", () => {
    for (const capabilities of [
      { ...rtc, video: null },
      { ...rtc, video: [] }
    ]) {
      expect(evaluateProtocolCompatibility("webrtc", [video()], capabilities, true).disabledReason).toBeUndefined();
    }
    for (const tracks of [[], [video("—")], [video("future-codec")], [{ ...video(), ready: false }]]) {
      expect(evaluateProtocolCompatibility("webrtc", tracks, rtc, true).disabledReason).toBeUndefined();
    }
  });

  it("FLV 的 H265/G711 不受浏览器原生接收能力限制", () => {
    for (const protocol of ["ws-flv", "http-flv", "wss-flv", "https-flv"]) {
      expect(evaluateProtocolCompatibility(protocol, [video(), audio()], rtc, true)).toEqual({});
    }
  });

  it("HLS 不凭 H265 编码名禁用，G711 音频风险仅警告", () => {
    for (const protocol of ["hls", "https-hls"]) {
      const result = evaluateProtocolCompatibility(protocol, [video(), audio()], rtc, true);
      expect(result.disabledReason).toBeUndefined();
      expect(result.warning).toContain("PCMA");
      expect(result.warning).toContain("声音");
    }
  });

  it("WebRTC 音频编码缺失及采样率不匹配只提示，不阻止视频", () => {
    for (const track of [audio("AAC", 48000), audio("PCMA", 16000)]) {
      const result = evaluateProtocolCompatibility("webrtc", [video("H264"), track], rtc, true);
      expect(result.disabledReason).toBeUndefined();
      expect(result.warning).toContain(track.codec);
      expect(result.warning).toContain("声音");
    }
    expect(evaluateProtocolCompatibility("webrtc", [video("H264"), audio()], rtc, true)).toEqual({});
  });

  it("单声道 Opus 不因浏览器声明双声道能力而误报", () => {
    expect(evaluateProtocolCompatibility("webrtc", [video("H264"), { ...audio("OPUS", 48000), channels: 1 }], rtc, true)).toEqual(
      {}
    );
  });

  it("区分音频采样率与 RTP 时钟，检查 G711 的声道数", () => {
    const capabilities = { ...rtc, audio: [...rtc.audio!, { mimeType: "audio/G722", clockRate: 8000, channels: 1 }] };
    for (const track of [audio("G722", 16000), audio("OPUS", 16000)]) {
      expect(evaluateProtocolCompatibility("webrtc", [video("H264"), track], capabilities, true)).toEqual({});
    }
    const result = evaluateProtocolCompatibility("webrtc", [video("H264"), { ...audio(), channels: 2 }], rtc, true);
    expect(result.disabledReason).toBeUndefined();
    expect(result.warning).toContain("PCMA");
  });

  it("关闭音频、没有音轨、音轨未 ready 或音频能力未知时不警告", () => {
    expect(evaluateProtocolCompatibility("webrtc", [video("H264"), audio("AAC")], rtc, false)).toEqual({});
    expect(evaluateProtocolCompatibility("hls", [video(), audio()], rtc, false)).toEqual({});
    for (const tracks of [[video("H264")], [video("H264"), { ...audio("AAC"), ready: false }]]) {
      expect(evaluateProtocolCompatibility("webrtc", tracks, rtc, true)).toEqual({});
    }
    expect(evaluateProtocolCompatibility("webrtc", [video("H264"), audio("AAC")], { ...rtc, audio: null }, true)).toEqual({});
  });
});

describe("浏览器接收能力探测", () => {
  it("分别读取视频和音频的实际接收能力", () => {
    vi.stubGlobal("RTCPeerConnection", class {});
    const getCapabilities = vi.fn(kind => ({ codecs: rtc[kind as "video" | "audio"] }));
    vi.stubGlobal("RTCRtpReceiver", { getCapabilities });
    expect(readBrowserCodecCapabilities()).toEqual(rtc);
    expect(getCapabilities.mock.calls).toEqual([["video"], ["audio"]]);
  });

  it("API 缺失、异常和空列表保持未知，不拿 RTX/FEC 当解码能力", () => {
    vi.stubGlobal("RTCPeerConnection", class {});
    for (const receiver of [
      undefined,
      {
        getCapabilities: () => {
          throw new Error("unavailable");
        }
      },
      { getCapabilities: () => ({ codecs: [] }) },
      { getCapabilities: () => ({ codecs: [{ mimeType: "video/rtx" }] }) }
    ]) {
      vi.stubGlobal("RTCRtpReceiver", receiver);
      expect(readBrowserCodecCapabilities()).toEqual({ webrtc: true, video: null, audio: null });
    }
  });

  it("浏览器完全缺少 WebRTC 时给出明确原因", () => {
    vi.stubGlobal("RTCPeerConnection", undefined);
    const capabilities = readBrowserCodecCapabilities();
    expect(evaluateProtocolCompatibility("webrtc", [], capabilities, false).disabledReason).toContain("WebRTC");
    expect(evaluateProtocolCompatibility("ws-flv", [video()], capabilities, false)).toEqual({});
  });
});
