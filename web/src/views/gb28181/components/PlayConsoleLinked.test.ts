import { flushPromises, mount } from "@vue/test-utils";
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
        data: { homePosition: { homeEnabled: true, homePresetId: 1, resetTime: 300 }, freshness: "fresh" }
      }),
    getPtzPreciseStatus: vi.fn(),
    updateHomePosition: vi.fn(),
    controlPtz: vi.fn(),
    controlPtzPrecise: vi.fn(),
    controlPtzExtended: vi.fn(),
    createPtzPreset: vi.fn(),
    callPtzPreset: vi.fn(),
    deletePtzPreset: vi.fn(),
    controlPtzCruise: vi.fn(),
    controlPtzAux: vi.fn(),
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
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("建立真实点播、读取概况、执行探针并在关闭时释放观看", async () => {
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
    expect(api.stopPlay).toHaveBeenCalledWith("stream-1");
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
    expect(source).toMatch(/\.aux-grid\.linked-aux-grid\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/s);
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
    expect(ptzDetail.findAll(".cruise-item")).toHaveLength(2);
    expect(ptzDetail.text()).toContain("更多 · 20");
    expect(ptzDetail.text()).toContain("2 / 20");

    // 预置位「更多」按钮存在(内容 slot 通过 a-popover teleport,不在 wrapper 内)
    const moreButton = wrapper.get("[data-testid='preset-more-btn']");
    expect(moreButton.text()).toContain("更多 · 20");
    const presetGrid = ptzDetail.get(".preset-grid");
    expect(presetGrid.element.lastElementChild?.querySelector("[data-testid='preset-more-btn']")).not.toBeNull();

    // 巡航轨迹仍走抽屉
    await wrapper.get("[data-testid='manage-cruises']").trigger("click");
    const manager = wrapper.get("[data-testid='asset-manager']");
    expect(manager.text()).toContain("巡航轨迹管理");
    expect(manager.findAll("[data-testid='asset-manager-row']")).toHaveLength(20);

    await wrapper.get("[data-testid='asset-manager-close']").trigger("click");
    await vi.advanceTimersByTimeAsync(200);
    await flushPromises();
    expect(wrapper.find("[data-testid='asset-manager']").exists()).toBe(false);

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

  it("未知 PTZ 能力仍允许尝试，未映射的辅助设备继续禁用", async () => {
    api.getControlCapabilities.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        basicPtz: { state: "unknown", reason: "设备未上报" },
        iFrame: { state: "unknown", reason: "设备未上报" },
        record: { state: "unknown", reason: "设备未上报" },
        guard: { state: "unknown", reason: "设备未上报" },
        alarmReset: { state: "unknown", reason: "设备未上报" },
        teleBoot: { state: "unknown", reason: "设备未上报" },
        dragZoom: { state: "unknown", reason: "设备未上报" }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    expect(wrapper.get("button[title='上']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='talk-button']").attributes("disabled")).toBeDefined();
    expect(wrapper.findAll(".linked-aux-grid .aux-btn").every(button => button.attributes("disabled") !== undefined)).toBe(true);
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
    api.controlPtzCruise.mockResolvedValueOnce({ code: 0, message: "", data: { action: "cruise_start" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get(".preset-item").trigger("click");
    await wrapper.get(".cruise-actions button").trigger("click");
    await flushPromises();
    expect(api.callPtzPreset).toHaveBeenCalledWith(channel.id, 1);
    expect(api.controlPtzCruise).toHaveBeenCalledWith(channel.id, { action: "start", trackId: 1 });
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
});
