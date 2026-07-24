import { flushPromises, mount } from "@vue/test-utils";
import { Message, Modal } from "@arco-design/web-vue";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => {
  const presets = Array.from({ length: 20 }, (_, index) => ({
    presetId: index + 1,
    name: `预置位 ${index + 1}`,
    updatedAt: "2026-07-22T10:00:00Z"
  }));
  const cruises = Array.from({ length: 20 }, (_, index) => ({ trackId: index + 1, name: `巡航 ${index + 1}`, enabled: true }));
  return {
    startPlay: vi
      .fn()
      .mockResolvedValue({
        code: 0,
        message: "",
        data: {
          streamId: "stream-1",
          ssrc: "0102030405",
          app: "rtp",
          wsflvUrl: "ws://zlm/rtp/stream-1.live.flv",
          httpFlvUrl: "",
          hlsUrl: "",
          expireAt: 0
        }
      }),
    stopPlay: vi.fn().mockResolvedValue({ code: 0, message: "", data: null }),
    getStreamMonitor: vi
      .fn()
      .mockResolvedValue({
        code: 0,
        message: "",
        data: {
          streamId: "stream-1",
          collectedAt: "2026-07-22T10:00:00Z",
          status: "online",
          node: { id: 1, name: "ZLM", host: "127.0.0.1" },
          quality: { bitrateKbps: 2048 },
          network: { bytesSpeed: 256000, totalBytes: 1024, readerCount: 2, totalReaderCount: 3, aliveSecond: 10 },
          tracks: [
            {
              kind: "video",
              codec: "H264",
              ready: true,
              frames: 100,
              duration: 4,
              loss: null,
              width: 1920,
              height: 1080,
              fps: 25,
              keyFrames: 4,
              gopSize: 25,
              gopIntervalMs: 1000,
              sampleRate: 0,
              channels: 0,
              sampleBit: 0
            }
          ],
          recording: { mp4: false, hls: false }
        }
      }),
    runStreamProbe: vi.fn(),
    getControlCapabilities: vi
      .fn()
      .mockResolvedValue({
        code: 0,
        message: "",
        data: {
          basicPtz: { state: "supported", reason: "" },
          iFrame: { state: "supported", reason: "" },
          record: { state: "supported", reason: "" },
          guard: { state: "supported", reason: "" },
          alarmReset: { state: "supported", reason: "" },
          teleBoot: { state: "supported", reason: "" },
          dragZoom: { state: "supported", reason: "" },
          broadcast: { state: "supported", reason: "" },
          talk: { state: "supported", reason: "" }
        }
      }),
    listPtzPresets: vi.fn().mockResolvedValue({ code: 0, message: "", data: { list: presets, freshness: "fresh" } }),
    listCruiseTracks: vi.fn().mockResolvedValue({ code: 0, message: "", data: { list: cruises, freshness: "fresh" } }),
    getHomePosition: vi
      .fn()
      .mockResolvedValue({
        code: 0,
        message: "",
        data: {
          homePosition: {
            enabled: true,
            resetTime: 300,
            presetId: 1,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          },
          controlSupport: { status: "supported", reason: "设备已确认控制能力" },
          querySupport: { status: "supported", reason: "设备已确认查询能力" },
          freshness: "fresh",
          control: { status: "idle", operationId: null, action: null, errorCode: null, deadlineAt: null },
          refresh: { status: "idle", operationId: null, errorCode: null, deadlineAt: null }
        }
      }),
    getPtzOperation: vi.fn(),
    getPtzPreciseStatus: vi.fn(),
    updateHomePosition: vi.fn(),
    controlPtz: vi.fn(),
    controlPtzPrecise: vi.fn(),
    controlPtzExtended: vi.fn(),
    createPtzPreset: vi.fn(),
    callPtzPreset: vi.fn(),
    deletePtzPreset: vi.fn(),
    controlPtzCruise: vi.fn(),
    createCruiseTrack: vi.fn(),
    controlPtzWiper: vi.fn(),
    controlDevice: vi.fn(),
    createTalkSession: vi.fn(),
    deleteTalkSession: vi.fn()
  };
});

vi.mock("@/api/gb28181", () => api);
vi.mock("./PlayWindow.vue", () => ({
  default: {
    props: ["url"],
    emits: ["error"],
    template: "<button data-testid='play-window' :data-url='url' @click=\"$emit('error', '拉流超时')\" />"
  }
}));

import PlayConsoleLinked from "./PlayConsoleLinked.vue";

const channel = {
  id: 1,
  channelId: "0411212755",
  deviceId: "34020000001320000001",
  name: "园区北门",
  ptzType: 1,
  status: 1
};

function homeResponse(overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: {
      homePosition: {
        enabled: true,
        resetTime: 300,
        presetId: 1,
        confirmedAt: "2026-07-22T10:00:00Z",
        source: "device_query",
        verification: "verified"
      },
      controlSupport: { status: "supported", reason: "设备已确认控制能力" },
      querySupport: { status: "supported", reason: "设备已确认查询能力" },
      freshness: "fresh",
      control: { status: "idle", operationId: null, action: null, errorCode: null, deadlineAt: null },
      refresh: { status: "idle", operationId: null, errorCode: null, deadlineAt: null },
      ...overrides
    }
  };
}

describe("PlayConsoleLinked 双区联动", () => {
  beforeEach(() => {
    api.startPlay.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        streamId: "stream-1",
        ssrc: "0102030405",
        app: "rtp",
        wsflvUrl: "ws://zlm/rtp/stream-1.live.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    api.getStreamMonitor.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        streamId: "stream-1",
        collectedAt: "2026-07-22T10:00:00Z",
        status: "online",
        node: { id: 1, name: "ZLM", host: "127.0.0.1" },
        quality: { bitrateKbps: 2048 },
        network: { bytesSpeed: 256000, totalBytes: 1024, readerCount: 2, totalReaderCount: 3, aliveSecond: 10 },
        tracks: [{
          kind: "video", codec: "H264", ready: true, frames: 100, duration: 4, loss: null,
          width: 1920, height: 1080, fps: 25, keyFrames: 4, gopSize: 25, gopIntervalMs: 1000,
          sampleRate: 0, channels: 0, sampleBit: 0
        }],
        recording: { mp4: false, hls: false }
      }
    });
    api.controlDevice.mockResolvedValue({ code: 0, message: "", data: { operationId: "op-1", action: "accepted", status: "accepted" } });
    api.controlPtz.mockResolvedValue({ code: 0, message: "", data: { action: "accepted", status: "sent" } });
    api.controlPtzWiper.mockResolvedValue({ code: 0, message: "", data: { operationId: "wiper-1", action: "accepted", status: "sent" } });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("建立真实点播、读取概况、执行探针并在关闭时仅销毁本地播放器", async () => {
    api.runStreamProbe.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        nodeId: 1,
        nodeName: "ZLM",
        completedAt: "2026-07-22T10:00:03Z",
        summary: { sampleDurationMs: 3000, frameCount: 246, totalBytes: 786432, averageBitrateKbps: 2097.1 },
        video: { codec: "H264", frameCount: 76, keyFrameCount: 3, fps: 25.3, gop: 25, averageIntervalMs: 39.8 },
        audio: { codec: "PCMA", frameCount: 170, keyFrameCount: 0, fps: null, gop: null, averageIntervalMs: 20 },
        timestamps: { videoDtsIntervalMeanMs: 39.8, arrivalJitterMs: 3.2, ptsDtsMaxMs: 0, avArrivalSkewMaxMs: 18 },
        timeline: [
          {
            sequence: 1,
            trackType: "video",
            codec: "H264",
            keyFrame: true,
            configFrame: false,
            relativeTimeMs: 0,
            frameSize: 1024
          }
        ],
        health: { status: "ok", issues: [], thresholds: { largeArrivalGapMs: 500, keyFrameWindowMs: 3000 } }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(api.startPlay).toHaveBeenCalledWith(channel.deviceId, channel.channelId);
    expect(api.getStreamMonitor).toHaveBeenCalledWith("stream-1");
    await wrapper.get("[data-testid='linked-tab-stream']").trigger("click");
    expect(wrapper.get("[data-testid='linked-detail-stream']").text()).toContain("2048 kbps");

    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    await wrapper.get("[data-testid='probe-start']").trigger("click");
    await flushPromises();
    expect(api.runStreamProbe).toHaveBeenCalledWith("stream-1");
    expect(wrapper.get("[data-testid='linked-detail-probe']").text()).toContain("76");
    expect(wrapper.get("[data-testid='linked-detail-probe']").text()).toContain("H264");

    await wrapper.setProps({ visible: false });
    await flushPromises();
    expect(wrapper.find("[data-testid='play-window']").exists()).toBe(false);
    expect(api.stopPlay).not.toHaveBeenCalled();
    expect(api.deleteTalkSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("点播请求尚未返回时关闭弹窗也不补发停播请求", async () => {
    let resolveStart!: (value: any) => void;
    api.startPlay.mockReturnValueOnce(new Promise(resolve => { resolveStart = resolve; }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.setProps({ visible: false });
    resolveStart({
      code: 0,
      message: "",
      data: { streamId: "stream-late", ssrc: "late", app: "rtp", wsflvUrl: "ws://zlm/late.flv", httpFlvUrl: "", hlsUrl: "", expireAt: 0 }
    });
    await flushPromises();

    expect(api.stopPlay).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("完整映射 ZLM 监控响应中的网络、轨道与录制字段", async () => {
    api.getStreamMonitor.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: "0192087380",
        collectedAt: "2026-07-23T02:57:38.305747Z",
        status: "online",
        node: { id: 2, name: "zlm-220", host: "192.168.10.220" },
        quality: { bitrateKbps: 1084.272 },
        network: { bytesSpeed: 135534, totalBytes: 8662847, readerCount: 0, totalReaderCount: 1, aliveSecond: 69 },
        tracks: [
          { kind: "audio", codec: "PCMA", ready: true, frames: 3453, duration: 69020, loss: 0, width: 0, height: 0, fps: 0, keyFrames: 0, gopSize: 0, gopIntervalMs: 0, sampleRate: 8000, channels: 1, sampleBit: 16 },
          { kind: "video", codec: "H264", ready: true, frames: 2070, duration: 69033, loss: 0, width: 1280, height: 720, fps: 30, keyFrames: 84, gopSize: 25, gopIntervalMs: 846, sampleRate: 0, channels: 0, sampleBit: 0 }
        ],
        recording: { mp4: false, hls: true }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-stream']").trigger("click");

    const side = wrapper.get("[data-testid='linked-side-stream']").text();
    const detail = wrapper.get("[data-testid='linked-detail-stream']").text();
    expect(side).toContain("1280×720 · 30 fps · 2070 帧");
    expect(side).toContain("8000 Hz · 1 声道 · 3453 帧");
    expect(side).toContain("132.4 KB/s");
    expect(side).toContain("累计 8.26 MB");
    expect(side).toContain("HLS");
    expect(detail).toContain("1084.3 kbps");
    expect(detail).toContain("0.0%");
    expect(detail).toContain("累计 1");
    wrapper.unmount();
  });

  it("右侧保留高频操作，流信息也使用统一的底部详情区", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel }
    });

    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    expect(wrapper.find(".hud").exists()).toBe(false);

    const ptzSide = wrapper.get("[data-testid='linked-side-ptz']");
    const ptzDetail = wrapper.get("[data-testid='linked-detail-ptz']");
    expect(ptzSide.text()).toContain("按住对讲");
    expect(ptzSide.text()).not.toContain("预置位");
    expect(ptzSide.text()).not.toContain("巡航轨迹");
    expect(ptzDetail.text()).toContain("预置位");
    expect(ptzDetail.text()).toContain("巡航轨迹");
    expect(ptzDetail.text()).toContain("看守位");

    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    const probeSide = wrapper.get("[data-testid='linked-side-probe']");
    const probeDetail = wrapper.get("[data-testid='linked-detail-probe']");
    expect(probeSide.text()).toContain("开始 3 秒检测");
    expect(probeSide.text()).toContain("时间戳监控");
    expect(probeSide.text()).toContain("帧到达抖动");
    expect(probeSide.text()).not.toContain("轨道详情");
    expect(probeDetail.text()).toContain("视频探针详情");
    expect(probeDetail.text()).toContain("帧到达时间线");
    expect(probeDetail.text()).not.toContain("时间戳监控");
    expect(probeDetail.text()).not.toContain("视频 DTS 间隔");

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    const advancedSide = wrapper.get("[data-testid='linked-side-advanced']");
    const advancedDetail = wrapper.get("[data-testid='linked-detail-advanced']");
    expect(advancedSide.text()).toContain("设备控制");
    expect(advancedSide.text()).not.toContain("亮度");
    expect(advancedDetail.text()).toContain("亮度");
    expect(advancedDetail.text()).toContain("接口待接入");

    await wrapper.get("[data-testid='linked-tab-stream']").trigger("click");
    const streamSide = wrapper.get("[data-testid='linked-side-stream']");
    const streamDetail = wrapper.get("[data-testid='linked-detail-stream']");
    expect(streamSide.text()).toContain("媒体节点");
    expect(streamSide.text()).toContain("媒体参数");
    expect(streamSide.text()).toContain("数据速率");
    expect(streamSide.text()).toContain("录制状态");
    expect(streamSide.text()).not.toContain("当前观看");
    expect(streamDetail.text()).toContain("当前观看");
    expect(streamDetail.text()).toContain("输出码率");
    expect(streamDetail.text()).toContain("视频接收丢包");
    expect(streamDetail.text()).toContain("音频接收丢包");

    wrapper.unmount();
  });

  it("联动详情横跨弹窗并统一使用紧凑高度", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");

    expect(source).toMatch(/\.linked-info-bar\s*\{[^}]*grid-column:\s*1\s*\/\s*-1/s);
    expect(source).toContain("--linked-detail-height: 148px");
    expect(source).toContain('width="min(1280px, calc(100vw - 32px))"');
    expect(source).toMatch(/\.console-body\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+336px/s);
    expect(source).toContain("sidebar-stream");
    expect(source).toContain('data-testid="linked-detail-stream"');
    expect(source).not.toContain("phase === 'playing' && activeTab !== 'stream'");
    expect(source).toMatch(/\.linked-detail\s*\{[^}]*height:\s*var\(--linked-detail-height\)/s);
    expect(source).toMatch(/\.linked-card\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(source).toMatch(/\.preset-tile-more\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(source).toMatch(/\.linked-stream-metrics\s*\{[^}]*grid-template-columns:\s*repeat\(4,\s*minmax\(0,\s*1fr\)\)/s);
    expect(source).toMatch(/\.linked-probe-layout\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/s);
    expect(source).toMatch(/\.wiper-actions\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/s);
    expect(source).toMatch(
      /\.sidebar\s+\[data-testid="linked-side-advanced"\]\s+\.adv-actions\s*\{[^}]*grid-template-columns:\s*1fr/s
    );
  });

  it("大量预置位和巡航通过摘要与管理抽屉承载", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel }
    });

    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    const ptzDetail = wrapper.get("[data-testid='linked-detail-ptz']");
    expect(ptzDetail.findAll(".preset-item")).toHaveLength(8);
    // 巡航轨迹也走 3×3 grid,20 条同样折叠为 8 tile + 1 「更多」chip
    expect(ptzDetail.findAll(".cruise-item")).toHaveLength(8);
    expect(ptzDetail.findAll("[data-testid='preset-more-btn']")).toHaveLength(1);
    expect(ptzDetail.findAll("[data-testid='cruise-more-btn']")).toHaveLength(1);

    // 预置位与巡航「更多」按钮同款文案
    expect(wrapper.get("[data-testid='preset-more-btn']").text()).toContain("更多 · 20");
    expect(wrapper.get("[data-testid='cruise-more-btn']").text()).toContain("更多 · 20");

    wrapper.unmount();
  });

  it("使用 ZLM 存活时长并在概况刷新失败后标记数据陈旧", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await vi.advanceTimersByTimeAsync(10);
    await flushPromises();

    await wrapper.get("[data-testid='linked-tab-stream']").trigger("click");
    expect(wrapper.get(".session-badge").text()).toContain("00:10");
    api.getStreamMonitor.mockRejectedValueOnce(new Error("node unavailable"));
    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(wrapper.find(".session-badge.warn").text()).toContain("监控数据过期");
    wrapper.unmount();
  });

  it("播放器错误进入可重试错误态", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='play-window']").trigger("click");
    expect(wrapper.text()).toContain("点播未完成");
    expect(wrapper.text()).toContain("拉流超时");
    wrapper.unmount();
  });

  it("窗口失焦会停止正在执行的连续云台动作", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const up = wrapper.get("button[title='上']");
    await up.trigger("pointerdown");
    window.dispatchEvent(new Event("blur"));
    await flushPromises();
    expect(api.controlPtz).toHaveBeenNthCalledWith(1, channel.id, expect.objectContaining({ action: "up" }));
    expect(api.controlPtz).toHaveBeenNthCalledWith(2, channel.id, expect.objectContaining({ action: "stop" }));
    wrapper.unmount();
  });

  it("对讲建立阶段松手会回收迟到的后端会话且不再申请麦克风", async () => {
    let resolveCreate!: (value: any) => void;
    api.createTalkSession.mockReturnValueOnce(new Promise(resolve => { resolveCreate = resolve; }));
    const getUserMedia = vi.fn();
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const talkButton = wrapper.get("[data-testid='talk-button']");
    talkButton.element.dispatchEvent(new Event("pointerdown"));
    await flushPromises();
    await talkButton.trigger("pointerup");
    resolveCreate({
      code: 0,
      message: "",
      data: { sessionId: "talk-late", state: "reserved", nodeId: 1, nodeName: "ZLM", sourceStream: "talk", recvStream: "recv", ssrc: "1", publishUrl: "https://zlm/whip", publishToken: "secret", expiresAt: "" }
    });
    await flushPromises();

    expect(getUserMedia).not.toHaveBeenCalled();
    expect(api.deleteTalkSession).toHaveBeenCalledWith(channel.id, "talk-late");
    wrapper.unmount();
  });

  it("对讲建立阶段关闭弹窗不会补发删除会话请求", async () => {
    let resolveCreate!: (value: any) => void;
    api.createTalkSession.mockReturnValueOnce(new Promise(resolve => { resolveCreate = resolve; }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("pointerdown"));
    await flushPromises();
    await wrapper.setProps({ visible: false });
    resolveCreate({
      code: 0,
      message: "",
      data: { sessionId: "talk-after-close", state: "reserved", nodeId: 1, nodeName: "ZLM", sourceStream: "talk", recvStream: "recv", ssrc: "1", publishUrl: "https://zlm/whip", publishToken: "secret", expiresAt: "" }
    });
    await flushPromises();

    expect(api.deleteTalkSession).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("设备明确上报不支持时仍允许尝试控制，由设备响应决定结果", async () => {
    api.getControlCapabilities.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        basicPtz: { state: "unsupported", reason: "厂商上报不支持" },
        iFrame: { state: "unsupported", reason: "厂商上报不支持" },
        record: { state: "unsupported", reason: "厂商上报不支持" },
        guard: { state: "unsupported", reason: "厂商上报不支持" },
        alarmReset: { state: "unsupported", reason: "厂商上报不支持" },
        teleBoot: { state: "unsupported", reason: "厂商上报不支持" },
        dragZoom: { state: "unsupported", reason: "厂商上报不支持" },
        broadcast: { state: "unsupported", reason: "厂商上报不支持" },
        talk: { state: "unsupported", reason: "厂商上报不支持" }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const up = wrapper.get("button[title='上']");
    expect(up.attributes("disabled")).toBeUndefined();
    await up.trigger("pointerdown");
    await flushPromises();
    expect(api.controlPtz).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "up" }));

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    const record = wrapper.get("[data-testid='advanced-record']");
    expect(record.attributes("disabled")).toBeUndefined();
    await record.trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "record_start" }));

    expect(wrapper.get("[data-testid='wiper-on']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='talk-button']").attributes("disabled")).toBeDefined();
    expect(wrapper.find(".capability-warn").exists()).toBe(false);
    wrapper.unmount();
  });

  it("快速切换通道时忽略旧点播响应并释放迟到的观看会话", async () => {
    let resolveOld!: (value: any) => void;
    api.startPlay
      .mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve; }))
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { streamId: "stream-2", ssrc: "2", app: "rtp", wsflvUrl: "ws://zlm/stream-2.flv", httpFlvUrl: "", hlsUrl: "", expireAt: 0 }
      });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.setProps({ channel: { ...channel, id: 2, channelId: "0411212756", name: "园区南门" } });
    await flushPromises();
    expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe("ws://zlm/stream-2.flv");

    resolveOld({
      code: 0,
      message: "",
      data: { streamId: "stream-old", ssrc: "1", app: "rtp", wsflvUrl: "ws://zlm/stream-old.flv", httpFlvUrl: "", hlsUrl: "", expireAt: 0 }
    });
    await flushPromises();
    expect(api.stopPlay).toHaveBeenCalledWith("stream-old");
    expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe("ws://zlm/stream-2.flv");
    wrapper.unmount();
  });

  it("预置位和巡航使用后端专用资源接口", async () => {
    api.callPtzPreset.mockResolvedValueOnce({ code: 0, message: "", data: { action: "call_preset" } });
    api.controlPtzCruise.mockResolvedValue({ code: 0, message: "", data: { action: "cruise_start", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get(".preset-item").trigger("click");
    await wrapper.get(".cruise-item").trigger("click");
    await flushPromises();
    await wrapper.get(".cruise-item").trigger("click");
    await flushPromises();
    expect(api.callPtzPreset).toHaveBeenCalledWith(channel.id, 1);
    expect(api.controlPtzCruise).toHaveBeenNthCalledWith(1, channel.id, { action: "start", trackId: 1 });
    expect(api.controlPtzCruise).toHaveBeenNthCalledWith(2, channel.id, { action: "stop", trackId: 1 });
    expect(api.controlPtzCruise.mock.calls.flatMap(([, body]) => [body.action])).not.toEqual(expect.arrayContaining(["pause", "resume"]));
    wrapper.unmount();
  });

  it("设备录制和布防按本次已知状态切换开始与停止动作", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    const recordButton = wrapper.get("[data-testid='advanced-record']");
    await recordButton.trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({ action: "record_start" }));
    expect(recordButton.text()).toContain("停止设备录制");

    await recordButton.trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({ action: "record_stop" }));

    const guardButton = wrapper.get("[data-testid='advanced-guard']");
    await guardButton.trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({ action: "guard_set" }));
    expect(guardButton.text()).toContain("撤防");
    wrapper.unmount();
  });

  it("3D 定位使用画面拖框换算后的真实坐标", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-drag-zoom']").trigger("click");
    const layer = wrapper.get("[data-testid='drag-zoom-layer']");
    vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, left: 0, top: 0, right: 800, bottom: 450, width: 800, height: 450, toJSON: () => ({})
    } as DOMRect);
    await layer.trigger("pointerdown", { clientX: 200, clientY: 100, pointerId: 1, button: 0 });
    await layer.trigger("pointermove", { clientX: 600, clientY: 300, pointerId: 1 });
    await layer.trigger("pointerup", { clientX: 600, clientY: 300, pointerId: 1 });
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({
      action: "drag_zoom_in",
      region: { length: 1920, width: 1080, midPointX: 960, midPointY: 480, lengthX: 960, lengthY: 480 }
    }));
    wrapper.unmount();
  });

  it("预置位为空时展示 arco 风空态,顶部保存按钮打开 dialog", async () => {
    api.listPtzPresets.mockResolvedValueOnce({ code: 0, message: "", data: { list: [], freshness: "fresh" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const emptyCard = wrapper.get("[data-testid='preset-empty']");
    expect(emptyCard.text()).toContain("暂无预置位");

    const saveBtn = wrapper.get("[data-testid='preset-save-btn']");
    expect(saveBtn.text()).toContain("添加");

    await saveBtn.trigger("click");
    await flushPromises();

    const dialog = wrapper.get("[data-testid='preset-save-dialog']");
    expect(dialog.text()).toContain("#1");
    const input = dialog.get("[data-testid='preset-save-name-input']");
    expect(input.attributes("modelvalue")).toBe("预置位 1");
    wrapper.unmount();
  });

  it("有数据时顶部保存按钮同样打开 dialog(下一个可用编号)", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='preset-save-btn']").trigger("click");
    await flushPromises();

    const dialog = wrapper.get("[data-testid='preset-save-dialog']");
    expect(dialog.text()).toContain("#21");
    wrapper.unmount();
  });

  it("巡航「添加」按钮:空预置位时 disabled,有预置位时可打开 modal", async () => {
    // 空预置位场景:按钮 disabled + title 提示
    api.listPtzPresets.mockResolvedValueOnce({ code: 0, message: "", data: { list: [], freshness: "fresh" } });
    const emptyWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const addBtn = emptyWrapper.get("[data-testid='cruise-add-btn']");
    expect(addBtn.attributes("disabled")).toBeDefined();
    expect(addBtn.attributes("title")).toContain("需要先添加预置位");
    emptyWrapper.unmount();

    // 有预置位场景:按钮 enabled + 点击打开 modal + 默认 trackId = nextCruiseTrackId (20 条巡航 → 21)
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const enabledBtn = wrapper.get("[data-testid='cruise-add-btn']");
    expect(enabledBtn.attributes("disabled")).toBeUndefined();
    await enabledBtn.trigger("click");
    await flushPromises();
    const dialog = wrapper.get("[data-testid='cruise-save-dialog']");
    expect(dialog).toBeTruthy();
    const trackIdInput = dialog.get("[data-testid='cruise-save-track-id']");
    expect(trackIdInput.attributes("modelvalue")).toBe("21");
    const nameInput = dialog.get("[data-testid='cruise-save-name-input']");
    expect(nameInput.attributes("modelvalue")).toBe("巡航 21");
    // 默认 1 个站点(预置位 1)
    expect(dialog.findAll("[data-testid='cruise-stop-row']")).toHaveLength(1);
    expect(dialog.get("[data-testid='cruise-stop-add-btn']").text()).toContain("添加巡航点");
    expect(dialog.get("[data-testid='cruise-save-speed']").attributes("min")).toBe("1");
    expect(dialog.get("[data-testid='cruise-save-dwell']").attributes("min")).toBe("1");
    expect(dialog.text()).toContain("无统一物理单位");
    expect(dialog.text()).toContain("国标单位为秒");
    expect(dialog.text()).not.toContain("0 表示");
    expect(dialog.text()).toContain("最长 68 分 15 秒");
    wrapper.unmount();
  });

  it("巡航站点最多 32 个且列表内部滚动,添加入口保持醒目", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");
    await flushPromises();

    const addStop = wrapper.get("[data-testid='cruise-stop-add-btn']");
    for (let index = 1; index < 32; index += 1) await addStop.trigger("click");

    const dialog = wrapper.get("[data-testid='cruise-save-dialog']");
    expect(dialog.findAll("[data-testid='cruise-stop-row']")).toHaveLength(32);
    expect(addStop.attributes("disabled")).toBeDefined();
    expect(addStop.text()).toContain("已达到 32 站上限");

    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(source).toMatch(/\.cruise-stops-list\s*\{[^}]*max-height:\s*clamp\(168px,\s*30vh,\s*260px\)[^}]*overflow-y:\s*auto/s);
    expect(source).toMatch(/\.cruise-stop-add\s*\{[^}]*width:\s*100%[^}]*min-height:\s*44px/s);
    expect(source).toMatch(/@media \(max-width:\s*560px\)\s*\{[^}]*\.cruise-save-form\s*\{[^}]*max-height:\s*calc\(100dvh - 210px\)/s);
    wrapper.unmount();
  });

  it("巡航新建 dialog 提交调 createCruiseTrack 并按选中的预置位顺序透传", async () => {
    api.createCruiseTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { channelId: "C", trackId: 21, totalStops: 1, completedStops: 1, status: "accepted", steps: [] }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");
    await flushPromises();

    // 直接触发 beforeOk(a-modal 底部按钮在测试 stub 下不便点击)
    const vm = wrapper.vm as unknown as {
      handleSaveCruiseBeforeOk: (done: (ok: boolean) => void) => Promise<void>;
      cruiseDraft: { trackId: number; replaceExisting: boolean };
    };
    expect(typeof vm.handleSaveCruiseBeforeOk).toBe("function");
    await new Promise<void>((resolve) => {
      vm.handleSaveCruiseBeforeOk((ok) => { expect(ok).toBe(true); resolve(); });
    });
    await flushPromises();
    expect(api.createCruiseTrack).toHaveBeenCalledWith(channel.id, expect.objectContaining({
      trackId: 21,
      stops: [{ presetId: 1 }],
      speed: 128,
      dwellSec: 5,
      replaceExisting: false,
    }));
    wrapper.unmount();
  });

  it("速度和停留关闭下发后由前端映射为接口哨兵值 0", async () => {
    api.createCruiseTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { channelId: "C", trackId: 21, totalStops: 1, completedStops: 1, status: "accepted", steps: [] }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");
    await flushPromises();

    const vm = wrapper.vm as unknown as {
      handleSaveCruiseBeforeOk: (done: (ok: boolean) => void) => Promise<void>;
      cruiseDraft: { sendSpeed: boolean; sendDwell: boolean };
    };
    vm.cruiseDraft.sendSpeed = false;
    vm.cruiseDraft.sendDwell = false;
    await new Promise<void>((resolve) => {
      vm.handleSaveCruiseBeforeOk((ok) => { expect(ok).toBe(true); resolve(); });
    });
    await flushPromises();

    expect(api.createCruiseTrack).toHaveBeenCalledWith(channel.id, expect.objectContaining({ speed: 0, dwellSec: 0 }));
    wrapper.unmount();
  });

  it("创建后短轮询本地缓存,设备确认后自动解除未验证状态", async () => {
    vi.useFakeTimers();
    api.listCruiseTracks
      .mockResolvedValueOnce({ code: 0, message: "", data: { list: [], freshness: "fresh" } })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          list: [{ trackId: 1, name: "巡航 1", enabled: false, detail: JSON.stringify({ source: "reconcile-pending", stops: [{ presetId: 1 }] }) }],
          freshness: "stale"
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { list: [{ trackId: 1, name: "巡航 1", enabled: true, detail: JSON.stringify({ source: "device-query", stops: [{ presetId: 1 }] }) }], freshness: "fresh" }
      });
    api.createCruiseTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { channelId: "C", trackId: 1, totalStops: 1, completedStops: 1, status: "sent", steps: [] }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");
    const vm = wrapper.vm as unknown as {
      handleSaveCruiseBeforeOk: (done: (ok: boolean) => void) => Promise<void>;
    };
    await new Promise<void>((resolve) => {
      vm.handleSaveCruiseBeforeOk((ok) => { expect(ok).toBe(true); resolve(); });
    });
    await flushPromises();
    expect(wrapper.get("[data-testid='cruise-tile-1']").text()).toContain("未验证");

    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    const confirmedTile = wrapper.get("[data-testid='cruise-tile-1']");
    expect(confirmedTile.text()).not.toContain("未验证");
    expect(confirmedTile.get(".cruise-item").attributes("disabled")).toBeUndefined();
    expect(api.listCruiseTracks).toHaveBeenLastCalledWith(channel.id, false);
    wrapper.unmount();
  });

  it("巡航配置下发期间锁定整个弹窗且不能取消关闭", async () => {
    let resolveCreate!: (value: any) => void;
    api.createCruiseTrack.mockReturnValueOnce(new Promise(resolve => { resolveCreate = resolve; }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");

    const vm = wrapper.vm as unknown as {
      handleSaveCruiseBeforeOk: (done: (ok: boolean) => void) => Promise<void>;
      closeSaveCruiseDialog: () => void;
    };
    const submission = vm.handleSaveCruiseBeforeOk(() => undefined);
    await wrapper.vm.$nextTick();

    const dialog = wrapper.get("[data-testid='cruise-save-dialog']");
    expect(dialog.get("[data-testid='cruise-save-track-id']").attributes("disabled")).toBeDefined();
    expect(dialog.get("[data-testid='cruise-save-name-input']").attributes("disabled")).toBeDefined();
    expect(dialog.get("[data-testid='cruise-stop-add-btn']").attributes("disabled")).toBeDefined();
    expect(dialog.get("[data-testid='cruise-save-speed']").attributes("disabled")).toBeDefined();
    expect(dialog.get("[data-testid='cruise-save-dwell']").attributes("disabled")).toBeDefined();
    expect(dialog.get("[data-testid='cruise-send-speed']").attributes("disabled")).toBeDefined();
    expect(dialog.get("[data-testid='cruise-send-dwell']").attributes("disabled")).toBeDefined();
    vm.closeSaveCruiseDialog();
    await wrapper.vm.$nextTick();
    expect(wrapper.find("[data-testid='cruise-save-dialog']").exists()).toBe(true);

    resolveCreate({
      code: 0,
      message: "",
      data: { channelId: "C", trackId: 21, totalStops: 1, completedStops: 1, status: "sent", steps: [] }
    });
    await submission;
    wrapper.unmount();
  });

  it("巡航允许标准轨迹号 0", async () => {
    api.createCruiseTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { channelId: "C", trackId: 0, totalStops: 1, completedStops: 1, status: "sent", steps: [] }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");
    await flushPromises();

    const vm = wrapper.vm as unknown as {
      cruiseDraft: { trackId: number };
      handleSaveCruiseBeforeOk: (done: (ok: boolean) => void) => Promise<void>;
    };
    vm.cruiseDraft.trackId = 0;
    await new Promise<void>((resolve) => {
      vm.handleSaveCruiseBeforeOk((ok) => { expect(ok).toBe(true); resolve(); });
    });
    expect(api.createCruiseTrack).toHaveBeenCalledWith(channel.id, expect.objectContaining({ trackId: 0 }));
    wrapper.unmount();
  });

  it("轨迹号 0 可展示、启动和停止", async () => {
    api.listCruiseTracks.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { list: [{ trackId: 0, name: "零号巡航", enabled: true }], freshness: "fresh" }
    });
    api.controlPtzCruise.mockResolvedValue({ code: 0, message: "", data: { action: "cruise_start", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    expect(wrapper.find("[data-testid='cruise-tile-0']").exists()).toBe(true);

    await wrapper.get("[data-testid='cruise-tile-0'] .cruise-item").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='cruise-running-chip']").exists()).toBe(true);
    await wrapper.get("[data-testid='cruise-tile-0'] .cruise-item").trigger("click");
    await flushPromises();
    expect(api.controlPtzCruise).toHaveBeenNthCalledWith(1, channel.id, { action: "start", trackId: 0 });
    expect(api.controlPtzCruise).toHaveBeenNthCalledWith(2, channel.id, { action: "stop", trackId: 0 });
    wrapper.unmount();
  });

  it("设备列表确认存在后解除旧的待对账 source", async () => {
    api.listCruiseTracks.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        list: [{
          trackId: 0,
          name: "已确认零号巡航",
          enabled: true,
          detail: JSON.stringify({ source: "reconcile-pending", stops: [{ presetId: 1 }] })
        }],
        freshness: "fresh"
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const tile = wrapper.get("[data-testid='cruise-tile-0']");
    const tooltip = wrapper.get("[data-testid='cruise-tile-tooltip-0']");
    expect(tile.classes()).not.toContain("disabled");
    expect(tile.get(".cruise-item").attributes("disabled")).toBeUndefined();
    expect(tooltip.attributes("content")).not.toContain("待设备对账");
    expect(tile.get(".cruise-item").attributes("title")).toBeUndefined();
    wrapper.unmount();
  });

  it("replaceExisting=true 时允许用已有轨迹号重建", async () => {
    api.createCruiseTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { channelId: "C", trackId: 1, totalStops: 1, completedStops: 1, status: "sent", steps: [] }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='cruise-add-btn']").trigger("click");
    await flushPromises();

    const vm = wrapper.vm as unknown as {
      cruiseDraft: { trackId: number; replaceExisting: boolean };
      handleSaveCruiseBeforeOk: (done: (ok: boolean) => void) => Promise<void>;
    };
    vm.cruiseDraft.trackId = 1;
    vm.cruiseDraft.replaceExisting = true;
    await new Promise<void>((resolve) => {
      vm.handleSaveCruiseBeforeOk((ok) => { expect(ok).toBe(true); resolve(); });
    });
    expect(api.createCruiseTrack).toHaveBeenCalledWith(channel.id, expect.objectContaining({ trackId: 1, replaceExisting: true }));
    wrapper.unmount();
  });

  it("切换通道会清除巡航运行态", async () => {
    api.controlPtzCruise.mockResolvedValue({ code: 0, message: "", data: { action: "cruise_start", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get(".cruise-item").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='cruise-running-chip']").exists()).toBe(true);

    const nextChannel = { ...channel, id: 2, channelId: "0411212756", name: "园区南门" };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();
    expect(wrapper.find("[data-testid='cruise-running-chip']").exists()).toBe(false);

    await wrapper.get(".cruise-item").trigger("click");
    await flushPromises();
    expect(api.controlPtzCruise).toHaveBeenLastCalledWith(nextChannel.id, { action: "start", trackId: 1 });
    wrapper.unmount();
  });

  it("忽略切换通道后迟到的巡航响应", async () => {
    let resolveCruise!: (value: any) => void;
    api.controlPtzCruise.mockReturnValueOnce(new Promise(resolve => { resolveCruise = resolve; }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get(".cruise-item").trigger("click");

    const nextChannel = { ...channel, id: 2, channelId: "0411212756", name: "园区南门" };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();
    resolveCruise({ code: 0, message: "", data: { action: "cruise_start", status: "sent" } });
    await flushPromises();
    expect(wrapper.find("[data-testid='cruise-running-chip']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("解析标准巡航详情并保留 freshness,加载失败不伪装成空列表", async () => {
    api.listCruiseTracks.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        list: [{
          trackId: 7,
          name: "标准巡航",
          enabled: false,
          detail: JSON.stringify({ trackId: 7, sumNum: 1, source: "reconcile-pending", cruisePoints: [{ presetIndex: 3, stayTime: 5, speed: 8 }] })
        }],
        freshness: "stale",
        refreshOperationId: "refresh-1"
      }
    });
    api.controlPtzCruise.mockResolvedValueOnce({ code: 0, message: "", data: { action: "cruise_start", status: "sent" } });
    const warning = vi.spyOn(Modal, "warning").mockImplementation((config: any) => {
      void config.onOk?.();
      return {} as any;
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    expect(api.listCruiseTracks).toHaveBeenCalledWith(channel.id, true);

    const pendingTooltip = wrapper.get("[data-testid='cruise-tile-tooltip-7']");
    const pendingTile = wrapper.get("[data-testid='cruise-tile-7']");
    expect(pendingTooltip.attributes("content")).toContain("配置指令已发送");
    expect(pendingTooltip.attributes("content")).toContain("GB/T 28181-2022");
    expect(pendingTooltip.attributes("mouse-enter-delay")).toBe("80");
    expect(pendingTile.attributes("title")).toBeUndefined();
    expect(pendingTile.get(".cruise-item").attributes("title")).toBeUndefined();
    expect(pendingTile.text()).toContain("未验证");
    expect(pendingTile.get(".cruise-item").attributes("disabled")).toBeUndefined();
    await pendingTile.get(".cruise-item").trigger("click");
    await flushPromises();
    expect(warning).toHaveBeenCalledWith(expect.objectContaining({ title: "试运行未验证轨迹" }));
    expect(api.controlPtzCruise).toHaveBeenCalledWith(channel.id, { action: "start", trackId: 7 });

    const vm = wrapper.vm as unknown as { openAssetManager: (tab: "cruise") => void };
    vm.openAssetManager("cruise");
    await wrapper.vm.$nextTick();
    expect(wrapper.get("[data-testid='asset-manager']").text()).toContain("1 个点位");
    expect(wrapper.get("[data-testid='asset-manager']").text()).toContain("停留 5s");
    expect(wrapper.get("[data-testid='asset-manager']").text()).toContain("未验证");
    expect(wrapper.get("[data-testid='cruise-manager-tooltip-7']").attributes("mouse-enter-delay")).toBe("80");

    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(source).toContain("const RESOURCE_TOOLTIP_ENTER_DELAY_MS = 80;");
    expect(source.match(/:mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"/g)).toHaveLength(6);
    wrapper.unmount();
    warning.mockRestore();

    api.listCruiseTracks.mockRejectedValueOnce(new Error("device unavailable"));
    const failedWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    expect(failedWrapper.get("[data-testid='cruise-load-error']").text()).toContain("加载巡航轨迹失败");
    expect(failedWrapper.find("[data-testid='cruise-empty']").exists()).toBe(false);
    failedWrapper.unmount();
  });

  it("预置位主卡片、更多列表和资源管理统一使用快速提示", async () => {
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel },
      global: {
        stubs: {
          "a-popover": { template: "<div><slot /><slot name='content' /></div>" }
        }
      }
    });
    await flushPromises();

    const tileTooltip = wrapper.get("[data-testid='preset-tile-tooltip-1']");
    expect(tileTooltip.attributes("mouse-enter-delay")).toBe("80");
    expect(tileTooltip.attributes("content")).toContain("#1 预置位 1");
    expect(tileTooltip.get(".preset-item").attributes("title")).toBeUndefined();
    expect(wrapper.get(".preset-tile .preset-tile-del").attributes("title")).toBe("删除 #1");

    const popoverTooltip = wrapper.get("[data-testid='preset-popover-tooltip-1']");
    expect(popoverTooltip.attributes("mouse-enter-delay")).toBe("80");
    expect(popoverTooltip.get(".preset-popover-name").attributes("title")).toBeUndefined();
    expect(wrapper.get(".preset-popover-call").attributes("title")).toBe("调用此预置位");
    expect(wrapper.get(".preset-popover-del").attributes("title")).toBe("删除此预置位");

    const vm = wrapper.vm as unknown as { openAssetManager: (tab: "preset") => void };
    vm.openAssetManager("preset");
    await wrapper.vm.$nextTick();
    const managerTooltip = wrapper.get("[data-testid='preset-manager-tooltip-1']");
    expect(managerTooltip.attributes("mouse-enter-delay")).toBe("80");
    expect(managerTooltip.attributes("content")).toContain("#1 预置位 1");
    expect(wrapper.get(".asset-manager-delete").attributes("title")).toBe("删除预置位");

    wrapper.unmount();
  });

  it("仅保留国标编号 1 的雨刷开启和关闭命令", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find(".linked-aux-grid").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("灯光");
    expect(wrapper.text()).not.toContain("红外");
    expect(wrapper.text()).not.toContain("加热");
    expect(wrapper.get("[data-testid='wiper-control']").text()).toContain("国标辅助编号 1");
    expect(wrapper.get("[data-testid='wiper-on']").classes()).toContain("btn-ghost");
    expect(wrapper.get("[data-testid='wiper-off']").classes()).toContain("btn-ghost");
    expect(wrapper.find("[data-testid='wiper-control'] .btn-primary").exists()).toBe(false);

    await wrapper.get("[data-testid='wiper-on']").trigger("click");
    await flushPromises();
    expect(api.controlPtzWiper).toHaveBeenNthCalledWith(1, channel.id, { action: "on" });

    await wrapper.get("[data-testid='wiper-off']").trigger("click");
    await flushPromises();
    expect(api.controlPtzWiper).toHaveBeenNthCalledWith(2, channel.id, { action: "off" });

    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(source).not.toContain("controlPtzAux");
    expect(source).not.toContain("auxSwitches");
    expect(source).not.toContain("auxiliaryIds");
    expect(source).not.toContain(".capability-warn");

    wrapper.unmount();
  });

  describe("home position state", () => {
    it("空配置和加载失败都不会伪造成设备已关闭", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({ homePosition: null, freshness: "unknown" }));
      const emptyWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(emptyWrapper.get("[data-testid='home-phase']").text()).toContain("待查询");
      expect(emptyWrapper.get("[data-testid='home-confirmed-status']").text()).toContain("尚无设备确认配置");
      expect(emptyWrapper.get("[data-testid='home-confirmed-status']").text()).not.toContain("已关闭");
      emptyWrapper.unmount();

      api.getHomePosition.mockRejectedValueOnce(new Error("network unavailable"));
      const failedWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(failedWrapper.get("[data-testid='home-phase']").text()).toContain("加载失败");
      expect(failedWrapper.get("[data-testid='home-confirmed-status']").text()).toContain("尚无设备确认配置");
      expect(failedWrapper.get("[data-testid='home-confirmed-status']").text()).not.toContain("已关闭");
      failedWrapper.unmount();
    });

    it("Toggle 只修改草稿，明确启用和关闭都保留最后确认值", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: {
          enabled: true,
          resetTime: 300,
          presetId: 0,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      }));
      const enabledWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(enabledWrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(enabledWrapper.get("[data-testid='home-confirmed-status']").text()).toContain("设备确认已启用");
      expect((enabledWrapper.get("[data-testid='home-toggle']").element as HTMLInputElement).checked).toBe(true);
      await enabledWrapper.get("[data-testid='home-toggle']").setValue(false);
      expect(enabledWrapper.get("[data-testid='home-confirmed-status']").text()).toContain("设备确认已启用");
      expect(enabledWrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      enabledWrapper.unmount();

      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: {
          enabled: false,
          resetTime: null,
          presetId: null,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      }));
      const disabledWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(disabledWrapper.get("[data-testid='home-phase']").text()).toContain("已关闭");
      expect(disabledWrapper.get("[data-testid='home-confirmed-status']").text()).toContain("设备确认已关闭");
      expect((disabledWrapper.get("[data-testid='home-toggle']").element as HTMLInputElement).checked).toBe(false);
      disabledWrapper.unmount();
    });

    it.each([
      { resetTime: 0, expected: "0 秒" },
      { resetTime: 9, expected: "9 秒" },
      { resetTime: 3601, expected: "3601 秒" },
      { resetTime: null, expected: "未返回" }
    ])("无损展示 #0 和入向等待时间 $resetTime", async ({ resetTime, expected }) => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: {
          enabled: true,
          resetTime,
          presetId: 0,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const confirmed = wrapper.get("[data-testid='home-confirmed-values']").text();
      expect(confirmed).toContain("#0");
      expect(confirmed).toContain(expected);
      expect(wrapper.find("[data-testid='home-preset'] option[value='0']").exists()).toBe(true);
      expect(wrapper.get("[data-testid='home-range-warning']").text()).toContain("超出平台可编辑范围");
      wrapper.unmount();
    });

    it("恢复控制 pending 与 unknown，但 T10 不启动 operation 轮询", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        control: {
          status: "pending",
          operationId: "control-pending",
          action: "home_position",
          errorCode: null,
          deadlineAt: "2026-07-22T10:00:15Z"
        }
      }));
      const pendingWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(pendingWrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("true");
      expect(pendingWrapper.get("[data-testid='home-operation-id']").text()).toContain("control-pending");
      expect(pendingWrapper.get("[data-testid='home-save']").attributes("disabled")).toBeDefined();
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      pendingWrapper.unmount();

      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        control: {
          status: "unknown",
          operationId: "control-unknown",
          action: "home_position",
          errorCode: "TRANSPORT_UNKNOWN",
          deadlineAt: null
        }
      }));
      const unknownWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(unknownWrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(unknownWrapper.get("[data-testid='home-phase']").text()).toContain("结果未知");
      expect(unknownWrapper.get("[data-testid='home-operation-id']").text()).toContain("control-unknown");
      expect(unknownWrapper.get("[data-testid='home-confirmed-status']").text()).toContain("设备确认已启用");
      expect(unknownWrapper.get("[data-testid='home-save']").attributes("disabled")).toBeUndefined();
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      unknownWrapper.unmount();
    });

    it("恢复查询 pending 且不重新发 refresh", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: {
          enabled: true,
          resetTime: 60,
          presetId: 0,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "control_ack",
          verification: "unverified"
        },
        refresh: {
          status: "pending",
          operationId: "reconcile-pending",
          errorCode: null,
          deadlineAt: "2026-07-22T10:00:15Z"
        }
      }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(wrapper.get("[data-testid='home-operation-id']").text()).toContain("reconcile-pending");
      expect(wrapper.get("[data-testid='home-verification']").text()).toContain("设备已确认，查询未验证");
      expect(api.getHomePosition).toHaveBeenCalledWith(channel.id);
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      wrapper.unmount();
    });

    it("能力 unknown/unsupported 仅提示，离线才禁用人工操作", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        controlSupport: { status: "unsupported", reason: "厂商 profile 未声明控制" },
        querySupport: { status: "unknown", reason: "尚未收到合法查询应答" }
      }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(wrapper.get("[data-testid='home-control-support']").text()).toContain("厂商 profile 未声明控制");
      expect(wrapper.get("[data-testid='home-query-support']").text()).toContain("尚未收到合法查询应答");
      expect(wrapper.get("[data-testid='home-support-risk']").text()).toContain("仍可尝试下发");
      expect(wrapper.get("[data-testid='home-save']").attributes("disabled")).toBeUndefined();
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();

      await wrapper.setProps({ channel: { ...channel, status: 0 } });
      expect(wrapper.get("[data-testid='home-save']").attributes("disabled")).toBeDefined();
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeDefined();
      wrapper.unmount();
    });
  });

  it("关闭看守位时无需预置位和等待时间即可保存", async () => {
    api.updateHomePosition.mockResolvedValueOnce({ code: 0, message: "", data: { operationId: "home-off" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const homeCard = wrapper.findAll(".linked-card").find(card => card.text().includes("看守位"));
    expect(homeCard).toBeDefined();
    await homeCard!.get("input[type='checkbox']").setValue(false);

    const saveButton = homeCard!.get("button");
    expect(saveButton.attributes("disabled")).toBeUndefined();
    await saveButton.trigger("click");
    await flushPromises();

    expect(api.updateHomePosition).toHaveBeenCalledWith(channel.id, { enabled: false });
    wrapper.unmount();
  });

  it("开启看守位时校验预置位和等待时间", async () => {
    api.getHomePosition.mockResolvedValueOnce({
      ...homeResponse({
        homePosition: {
          enabled: false,
          resetTime: null,
          presetId: null,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      })
    });
    api.updateHomePosition.mockResolvedValueOnce({ code: 0, message: "", data: { operationId: "home-on" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const homeCard = wrapper.findAll(".linked-card").find(card => card.text().includes("看守位"));
    expect(homeCard).toBeDefined();
    await homeCard!.get("input[type='checkbox']").setValue(true);
    const saveButton = homeCard!.get("button");
    expect(saveButton.attributes("disabled")).toBeDefined();

    await homeCard!.get("select").setValue("1");
    await homeCard!.get("input[type='number']").setValue(9);
    expect(saveButton.attributes("disabled")).toBeDefined();

    await homeCard!.get("input[type='number']").setValue(10);
    expect(saveButton.attributes("disabled")).toBeUndefined();
    await saveButton.trigger("click");
    await flushPromises();

    expect(api.updateHomePosition).toHaveBeenCalledWith(channel.id, { enabled: true, resetTime: 10, presetId: 1 });
    wrapper.unmount();
  });

  it("切换设备后隔离新旧雨刷请求状态和结果提示", async () => {
    let resolveOld!: (value: any) => void;
    let resolveCurrent!: (value: any) => void;
    api.controlPtzWiper
      .mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve; }))
      .mockReturnValueOnce(new Promise(resolve => { resolveCurrent = resolve; }));
    const successSpy = vi.spyOn(Message, "success");
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='wiper-on']").trigger("click");
    expect(wrapper.get("[data-testid='wiper-off']").attributes("disabled")).toBeDefined();

    const nextChannel = { ...channel, id: 2, channelId: "0411212756", name: "园区南门" };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();
    expect(wrapper.get("[data-testid='wiper-off']").attributes("disabled")).toBeUndefined();

    await wrapper.get("[data-testid='wiper-off']").trigger("click");
    expect(api.controlPtzWiper).toHaveBeenNthCalledWith(2, nextChannel.id, { action: "off" });
    expect(wrapper.get("[data-testid='wiper-on']").attributes("disabled")).toBeDefined();

    resolveOld({ code: 0, message: "", data: { operationId: "old-wiper" } });
    await flushPromises();
    expect(successSpy).not.toHaveBeenCalled();
    expect(wrapper.get("[data-testid='wiper-on']").attributes("disabled")).toBeDefined();

    resolveCurrent({ code: 0, message: "", data: { operationId: "current-wiper" } });
    await flushPromises();
    expect(successSpy).toHaveBeenCalledTimes(1);
    expect(wrapper.get("[data-testid='wiper-on']").attributes("disabled")).toBeUndefined();

    successSpy.mockRestore();
    wrapper.unmount();
  });
});
