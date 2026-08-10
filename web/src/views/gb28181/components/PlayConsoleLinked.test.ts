import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
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
    getDeviceStatus: vi.fn().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
        freshness: "unknown",
        refreshOperationId: null,
        refreshError: null
      }
    }),
    fetchPTZDefaultSpeedConfig: vi.fn().mockResolvedValue({ code: 0, message: "", data: { level: 6 } }),
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
    controlDevice: vi.fn(),
    createTalkSession: vi.fn(),
    getTalkSession: vi.fn(),
    deleteTalkSession: vi.fn()
  };
});

vi.mock("@/api/gb28181", () => api);
vi.mock("./PlayWindow.vue", () => ({
  default: {
    props: ["url", "zlmWebrtc"],
    emits: ["error"],
    template: "<button class='play-window' data-testid='play-window' :data-url='url' :data-zlm-webrtc='String(Boolean(zlmWebrtc))' @click=\"$emit('error', '拉流超时')\" />"
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

function operationResponse(
  status: "queued" | "sent" | "accepted" | "rejected" | "timeout" | "unknown" | "cancelled",
  operationId: string,
  deadlineAt: string | null,
  errorCode: string | null = null
) {
  return {
    code: 0,
    message: "",
    data: {
      operationId,
      status,
      errorCode,
      errorMessage: errorCode,
      completedAt: ["accepted", "rejected", "timeout", "unknown", "cancelled"].includes(status)
        ? "2026-07-22T10:00:05Z"
        : null,
      deadlineAt
    }
  };
}

async function requestDeviceStatus(wrapper: VueWrapper) {
  await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
  await wrapper.get(".advanced-status-refresh").trigger("click");
  await flushPromises();
}

describe("PlayConsoleLinked 双区联动", () => {
  beforeEach(() => {
    api.getHomePosition.mockReset();
    api.getPtzOperation.mockReset();
    api.getDeviceStatus.mockReset();
    api.updateHomePosition.mockReset();
    api.fetchPTZDefaultSpeedConfig.mockReset();
    api.fetchPTZDefaultSpeedConfig.mockResolvedValue({ code: 0, message: "", data: { level: 6 } });
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
    api.getHomePosition.mockResolvedValue(homeResponse());
    api.getDeviceStatus.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
        freshness: "unknown",
        refreshOperationId: null,
        refreshError: null
      }
    });
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted", "home-default", null));
    api.updateHomePosition.mockResolvedValue({
      code: 0,
      message: "",
      data: { operationId: "home-default", sn: 1, channelId: channel.channelId, action: "home_position", status: "queued" }
    });
    api.createTalkSession.mockResolvedValue({
      code: 0,
      message: "",
      data: { sessionId: "talk-1", mode: "broadcast", state: "reserved", nodeId: 1, nodeName: "ZLM", sourceStream: "talk", recvStream: "recv", ssrc: "1", publishUrl: "https://zlm/whip", publishToken: "secret", expiresAt: "" }
    });
    api.getTalkSession.mockResolvedValue({ code: 0, message: "", data: { sessionId: "talk-1", mode: "broadcast", state: "active", expiresAt: "" } });
    api.deleteTalkSession.mockResolvedValue({ code: 0, message: "", data: { sessionId: "talk-1", state: "ended" } });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it("拖拽摇杆按八方向发送云台指令，松手停止且不展示绝对角度", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const joystick = wrapper.get(".joystick-stage");
    expect(joystick.findAll(".joystick-dot")).toHaveLength(8);
    expect(joystick.findAll(".joystick-label.diagonal")).toHaveLength(4);
    vi.spyOn(joystick.element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, top: 0, left: 0, right: 176, bottom: 176, width: 176, height: 176, toJSON: () => ({}),
    } as DOMRect);
    const pointerDown = new MouseEvent("pointerdown", { bubbles: true, clientX: 88, clientY: 20 });
    Object.defineProperty(pointerDown, "pointerId", { value: 1 });
    joystick.element.dispatchEvent(pointerDown);
    await flushPromises();

    expect(api.controlPtz).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "up" }));
    expect(joystick.text()).not.toContain("Pan");
    expect(joystick.text()).not.toContain("Tilt");

    const pointerUp = new MouseEvent("pointerup", { bubbles: true, clientX: 88, clientY: 20 });
    Object.defineProperty(pointerUp, "pointerId", { value: 1 });
    joystick.element.dispatchEvent(pointerUp);
    await flushPromises();

    expect(api.controlPtz).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({ action: "stop" }));
    expect(api.controlPtz).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("读取云台默认速度并把第十档映射为协议速度 255", async () => {
    api.fetchPTZDefaultSpeedConfig.mockResolvedValueOnce({ code: 0, message: "", data: { level: 10 } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.get("input[type='range'][max='10']").element).toHaveProperty("value", "10");
    await wrapper.get("[aria-label='云台方向摇杆']").trigger("keydown", { key: "ArrowUp" });
    await flushPromises();

    expect(api.controlPtz).toHaveBeenCalledWith(
      channel.id,
      expect.objectContaining({ action: "up", speed: 255 })
    );
    wrapper.unmount();
  });

  it("云台默认速度加载失败时回退到第六档", async () => {
    api.fetchPTZDefaultSpeedConfig.mockRejectedValueOnce(new Error("network error"));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.get("input[type='range'][max='10']").element).toHaveProperty("value", "6");
    wrapper.unmount();
  });

  it("云台移动期间在视频画面显示对应方向的呼吸箭头，停止后隐藏", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find("[data-testid='ptz-direction-indicator']").exists()).toBe(false);

    const joystick = wrapper.get(".joystick-stage");
    vi.spyOn(joystick.element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, top: 0, left: 0, right: 176, bottom: 176, width: 176, height: 176, toJSON: () => ({}),
    } as DOMRect);
    const pointerDown = new MouseEvent("pointerdown", { bubbles: true, clientX: 156, clientY: 20 });
    Object.defineProperty(pointerDown, "pointerId", { value: 2 });
    joystick.element.dispatchEvent(pointerDown);
    await flushPromises();

    const indicator = wrapper.get("[data-testid='ptz-direction-indicator']");
    expect(indicator.attributes("data-direction")).toBe("右上");
    expect(indicator.attributes("aria-label")).toBe("云台正在向右上移动");
    expect(indicator.findAll(".ptz-direction-chevron")).toHaveLength(3);
    expect(indicator.findAll("[aria-hidden='true']")).toHaveLength(3);
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(source).toContain("background: rgb(96 165 250 / 82%);");
    expect(source).toContain("background: rgb(147 197 253 / 48%);");
    expect(source).toContain("background: rgb(191 219 254 / 22%);");
    expect(source).not.toContain("drop-shadow(0 0 9px rgb(255 255 255 / 28%))");

    const pointerUp = new MouseEvent("pointerup", { bubbles: true, clientX: 156, clientY: 20 });
    Object.defineProperty(pointerUp, "pointerId", { value: 2 });
    joystick.element.dispatchEvent(pointerUp);
    await flushPromises();

    expect(wrapper.find("[data-testid='ptz-direction-indicator']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("按返回地址动态展示协议并优先选择可播放的 WS-FLV", async () => {
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: "stream-mixed",
        ssrc: "0102030405",
        app: "rtp",
        urls: {
          wsFlv: "ws://zlm/rtp/mixed.live.flv",
          wssFlv: "wss://zlm/rtp/mixed.live.flv",
          httpFlv: "http://zlm/rtp/mixed.live.flv",
          httpsFlv: "https://zlm/rtp/mixed.live.flv",
          hls: "http://zlm/rtp/mixed/hls.m3u8",
          httpsHls: "https://zlm/rtp/mixed/hls.m3u8",
          rtsp: "rtsp://zlm:10554/rtp/mixed"
        },
        wsflvUrl: "ws://legacy/rtp/mixed.live.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe("ws://zlm/rtp/mixed.live.flv");
    expect(wrapper.findAll(".proto-btn").map((button) => button.text())).toEqual(["WS-FLV", "HTTP-FLV", "HLS", "WebRTC"]);
    expect(wrapper.findAll(".proto-btn").find((button) => button.text() === "WebRTC")?.attributes("disabled")).toBeDefined();

    await wrapper.get(".protocol-switcher select").trigger("click");
    await flushPromises();
    const dropdownText = wrapper.text();
    expect(dropdownText).toContain("wss://zlm/rtp/mixed.live.flv");
    expect(dropdownText).toContain("rtsp://zlm:10554/rtp/mixed");
    expect(dropdownText).not.toContain("http://legacy/rtp/mixed.live.flv");
    expect(dropdownText).not.toContain("可播放");
    expect(dropdownText).not.toContain("仅复制");
    expect(dropdownText).not.toContain("延迟最低,适合实时监控");
    expect(wrapper.find(".copy-url").exists()).toBe(false);
    const protocolRows = wrapper.findAll(".protocol-option");
    expect(protocolRows).toHaveLength(7);
    expect(protocolRows.every((row) => row.element.lastElementChild?.classList.contains("protocol-copy-btn"))).toBe(true);
    expect(wrapper.findAll(".protocol-option strong").map((label) => label.text())).toEqual([
      "WS-FLV:",
      "WSS-FLV:",
      "HTTP-FLV:",
      "HTTPS-FLV:",
      "HLS:",
      "HTTPS-HLS:",
      "RTSP:"
    ]);
    wrapper.unmount();
  });

  it("在播放弹窗中手动切换并包装 ZLM WebRTC 地址", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: "stream-webrtc",
        ssrc: "0102030405",
        app: "rtp",
        urls: {
          wsFlv: "ws://zlm/rtp/stream-webrtc.live.flv",
          webrtc: "http://zlm:18080/index/api/webrtc?app=rtp&stream=stream-webrtc&type=play"
        },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const player = wrapper.get("[data-testid='play-window']");
    expect(player.attributes("data-url")).toBe("ws://zlm/rtp/stream-webrtc.live.flv");
    expect(player.attributes("data-zlm-webrtc")).toBe("false");
    expect(wrapper.findAll(".protocol-option strong").map((label) => label.text())).toContain("WebRTC:");
    expect(wrapper.findAll(".proto-btn").map((button) => button.text())).toContain("WebRTC");

    const webRtcOption = wrapper.findAll(".protocol-option").find((option) => option.text().includes("WebRTC:"));
    await webRtcOption!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith(
      "http://zlm:18080/index/api/webrtc?app=rtp&stream=stream-webrtc&type=play"
    );
    expect(player.attributes("data-url")).toBe("ws://zlm/rtp/stream-webrtc.live.flv");

    const vm = wrapper.vm as unknown as { switchProtocol: (proto: "webrtc") => void };
    vm.switchProtocol("webrtc");
    await flushPromises();

    expect(player.attributes("data-url")).toBe(
      "webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-webrtc&type=play"
    );
    expect(player.attributes("data-zlm-webrtc")).toBe("true");
    wrapper.unmount();
  });

  it("新会话优先使用服务端协议快照并允许本次会话手动切换", async () => {
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: "stream-default-webrtc",
        ssrc: "0102030405",
        app: "rtp",
        defaultProtocol: "webrtc",
        protocol: "webrtc",
        url: "http://zlm:18080/index/api/webrtc?app=rtp&stream=stream-default-webrtc&type=play",
        zlmWebrtc: true,
        urls: {
          wsFlv: "ws://zlm/rtp/stream-default-webrtc.live.flv",
          webrtc: "http://zlm:18080/index/api/webrtc?app=rtp&stream=stream-default-webrtc&type=play"
        },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const player = wrapper.get("[data-testid='play-window']");
    expect(player.attributes("data-url")).toBe(
      "webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-default-webrtc&type=play"
    );
    expect(player.attributes("data-zlm-webrtc")).toBe("true");

    const vm = wrapper.vm as unknown as { switchProtocol: (proto: "ws-flv") => void };
    vm.switchProtocol("ws-flv");
    await flushPromises();
    expect(player.attributes("data-url")).toBe("ws://zlm/rtp/stream-default-webrtc.live.flv");
    expect(player.attributes("data-zlm-webrtc")).toBe("false");
    wrapper.unmount();
  });

  it("只有诊断协议时保持播放器地址为空", async () => {
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: "stream-diagnostic",
        ssrc: "0102030405",
        app: "rtp",
        urls: { rtsp: "rtsp://zlm:10554/rtp/diagnostic", rtmp: "rtmp://zlm:11935/rtp/diagnostic" },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe("");
    expect(wrapper.findAll(".proto-btn.active")).toHaveLength(0);
    wrapper.unmount();
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
    expect(api.getDeviceStatus).not.toHaveBeenCalled();
    expect(api.listCruiseTracks).toHaveBeenCalledWith(channel.id, false);
    expect(api.listCruiseTracks).not.toHaveBeenCalledWith(channel.id, true);
    // 流信息 tab 已并入探针 tab,原来的"累计 X 观看"现在在探针概览卡里
    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    expect(wrapper.get("[data-testid='stream-brief']").text()).toContain("累计 3");
    await wrapper.get("[data-testid='probe-start']").trigger("click");
    await flushPromises();
    expect(api.runStreamProbe).toHaveBeenCalledWith("stream-1");
    // 轨道明细里不再重复 codec(流信息块已有编码),改断言探针独有的采样帧与精确 FPS
    expect(wrapper.get("[data-testid='linked-detail-probe']").text()).toContain("76");
    expect(wrapper.get("[data-testid='linked-detail-probe']").text()).toContain("精确 FPS");
    expect(wrapper.get("[data-testid='linked-detail-probe']").text()).not.toContain("H264");

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
    // 流信息 tab 已删,监控快照映射改到探针 tab 的概览卡里断言
    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");

    const brief = wrapper.get("[data-testid='stream-brief']").text();
    // 视频信息
    expect(brief).toContain("1280×720");
    expect(brief).toContain("30");
    expect(brief).toContain("H264");
    // 音频信息(声道字段仍在探针概览卡)
    expect(brief).toContain("8000 Hz");
    expect(brief).toContain("PCMA");
    // 网络指标
    expect(brief).toContain("132.4 KB/s");
    expect(brief).toContain("累计 8.26 MB");
    // 「录制状态」和「输出码率」都已移除;后者跟"数据速率"同源
    expect(brief).not.toContain("录制状态");
    expect(brief).not.toContain("1084.3 kbps");
    // 累计观看数
    expect(brief).toContain("累计 1");
    wrapper.unmount();
  });

  it("侧栏与详情条按 tab 分工，云台/探针/高级各司其职", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel }
    });

    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    expect(wrapper.find(".hud").exists()).toBe(false);

    const ptzSide = wrapper.get("[data-testid='linked-side-ptz']");
    const ptzDetail = wrapper.get("[data-testid='linked-detail-ptz']");
    expect(ptzSide.text()).toContain("按住广播");
    expect(ptzSide.text()).not.toContain("预置位");
    expect(ptzSide.text()).not.toContain("巡航轨迹");
    expect(ptzDetail.text()).toContain("预置位");
    expect(ptzDetail.text()).toContain("巡航轨迹");
    expect(ptzDetail.text()).toContain("看守位");

    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    const probeSide = wrapper.get("[data-testid='linked-side-probe']");
    const probeDetail = wrapper.get("[data-testid='linked-detail-probe']");
    // 侧栏留"实时信息 + 触发/摘要",采样结果全部下移到详情条
    expect(probeSide.text()).toContain("开始 3 秒检测");
    expect(probeSide.text()).toContain("概览");
    expect(probeSide.text()).not.toContain("时间戳监控");
    expect(probeSide.text()).not.toContain("帧到达抖动");
    // 详情条三栏:轨道明细 / 时间戳监控 / 帧到达时间线
    expect(probeDetail.text()).toContain("轨道明细");
    expect(probeDetail.text()).toContain("时间戳监控");
    expect(probeDetail.text()).toContain("视频 DTS 间隔");
    expect(probeDetail.text()).toContain("帧到达时间线");
    expect(probeDetail.findAll(".linked-probe-layout > .linked-section")).toHaveLength(3);

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    const advancedSide = wrapper.get("[data-testid='linked-side-advanced']");
    const advancedDetail = wrapper.get("[data-testid='linked-detail-advanced']");
    expect(advancedSide.text()).toContain("设备控制");
    expect(advancedSide.text()).not.toContain("亮度");
    expect(advancedDetail.text()).not.toContain("亮度");
    expect(advancedDetail.text()).not.toContain("接口待接入");
    expect(advancedDetail.text()).toContain("标准控制字段");

    wrapper.unmount();
  });

  it("探针面板顶部的流信息按概览 + 音频左/视频右分栏，丢包各归各类", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    const brief = wrapper.get("[data-testid='stream-brief']");

    // 概览只放这两项持续变化的指标
    expect(brief.get(".stream-brief-overview").text()).toContain("当前观看");
    expect(brief.get(".stream-brief-overview").text()).toContain("数据速率");

    // 左视频、右音频,顺序由 DOM 决定
    const kinds = brief.findAll(".stream-brief-kind");
    expect(kinds).toHaveLength(2);
    expect(kinds[0].classes()).toContain("video");
    expect(kinds[1].classes()).toContain("audio");

    // 丢包跟着各自的媒体类型,不再单独占格
    expect(kinds[0].text()).toContain("分辨率");
    expect(kinds[0].text()).toContain("帧率");
    expect(kinds[0].text()).toContain("丢包");
    expect(kinds[1].text()).toContain("采样率");
    expect(kinds[1].text()).toContain("声道");
    expect(kinds[1].text()).toContain("丢包");

    // 未检测时不再显示"尚未执行深度检测"那块占位
    expect(wrapper.text()).not.toContain("尚未执行深度检测");

    wrapper.unmount();
  });

  it("联动详情横跨弹窗并统一使用紧凑高度", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");

    expect(source).toMatch(/\.linked-info-bar\s*\{[^}]*grid-column:\s*1\s*\/\s*-1/s);
    expect(source).toContain("--linked-detail-height: 148px");
    expect(source).toContain('width="min(1280px, calc(100vw - 32px))"');
    expect(source).toMatch(/\.console-body\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+336px/s);
    // 流信息 tab 已并入探针 tab,原来的 sidebar-stream / linked-detail-stream / linked-stream-metrics
    // 全都退出历史舞台
    expect(source).not.toContain("sidebar-stream");
    expect(source).not.toContain('data-testid="linked-detail-stream"');
    expect(source).not.toContain(".linked-stream-metrics");
    expect(source).not.toContain("phase === 'playing' && activeTab !== 'stream'");
    expect(source).toMatch(/\.linked-detail\s*\{[^}]*height:\s*var\(--linked-detail-height\)/s);
    expect(source).toMatch(/\.linked-card\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(source).toMatch(/\.preset-tile-more\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(source).toMatch(
      /@media \(max-width: 720px\)[\s\S]*?\.linked-detail\s*>\s*\.linked-ptz-layout\s*\{[^}]*flex:\s*0 0 auto;[^}]*grid-template-rows:\s*none;[^}]*height:\s*auto/s
    );
    expect(source).toContain("@container (max-width: 340px)");
    // 探针详情条三栏不等分:时间线是横向柱状图,等分会把 32 根柱子挤到每根不足 9px
    expect(source).toMatch(
      /\.linked-probe-layout\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+minmax\(0,\s*1fr\)\s+minmax\(0,\s*1\.4fr\)/s
    );
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

    // session-badge 在标题栏,不用切 tab 也能读到
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
    const joystick = wrapper.get("[aria-label='云台方向摇杆']");
    await joystick.trigger("keydown", { key: "ArrowUp" });
    window.dispatchEvent(new Event("blur"));
    await flushPromises();
    expect(api.controlPtz).toHaveBeenNthCalledWith(1, channel.id, expect.objectContaining({ action: "up" }));
    expect(api.controlPtz).toHaveBeenNthCalledWith(2, channel.id, expect.objectContaining({ action: "stop" }));
    wrapper.unmount();
  });

  it("麦克风授权期间松手不会创建后端会话", async () => {
    let resolveMedia!: (value: any) => void;
    const stop = vi.fn();
    const track = { enabled: true, stop };
    const getUserMedia = vi.fn().mockReturnValueOnce(new Promise(resolve => { resolveMedia = resolve; }));
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const talkButton = wrapper.get("[data-testid='talk-button']");
    talkButton.element.dispatchEvent(new Event("pointerdown"));
    await flushPromises();
    expect(getUserMedia).toHaveBeenCalledTimes(1);
    expect(api.createTalkSession).not.toHaveBeenCalled();
    await talkButton.trigger("pointerup");
    resolveMedia({ getTracks: () => [track], getAudioTracks: () => [track] });
    await flushPromises();

    expect(api.createTalkSession).not.toHaveBeenCalled();
    expect(stop).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("创建响应迟到时关闭弹窗仍会删除后端会话", async () => {
    let resolveCreate!: (value: any) => void;
    api.createTalkSession.mockReturnValueOnce(new Promise(resolve => { resolveCreate = resolve; }));
    const stop = vi.fn();
    const track = { enabled: true, stop };
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [track], getAudioTracks: () => [track] }) } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("pointerdown"));
    await flushPromises();
    await wrapper.setProps({ visible: false });
    resolveCreate({
      code: 0,
      message: "",
      data: { sessionId: "talk-after-close", mode: "broadcast", state: "reserved", nodeId: 1, nodeName: "ZLM", sourceStream: "talk", recvStream: "recv", ssrc: "1", publishUrl: "https://zlm/whip", publishToken: "secret", expiresAt: "" }
    });
    await flushPromises();

    expect(api.deleteTalkSession).toHaveBeenCalledWith(channel.id, "talk-after-close");
    wrapper.unmount();
  });

  it("Broadcast 的麦克风在后端 active 前保持静音并锁定 PCMA", async () => {
    let resolveStatus!: (value: any) => void;
    api.getTalkSession.mockReturnValueOnce(new Promise(resolve => { resolveStatus = resolve; }));
    const stop = vi.fn();
    const track = { enabled: true, stop };
    const stream = { getTracks: () => [track], getAudioTracks: () => [track] };
    const setCodecPreferences = vi.fn();
    const offerSdp = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 8\r\na=rtpmap:8 PCMA/8000\r\n";
    class FakePeerConnection {
      iceGatheringState = "complete";
      localDescription: RTCSessionDescriptionInit | null = null;
      addTransceiver = vi.fn(() => ({ setCodecPreferences }));
      createOffer = vi.fn().mockResolvedValue({ type: "offer", sdp: offerSdp });
      setLocalDescription = vi.fn(async (description: RTCSessionDescriptionInit) => { this.localDescription = description; });
      setRemoteDescription = vi.fn().mockResolvedValue(undefined);
      addEventListener = vi.fn();
      removeEventListener = vi.fn();
      close = vi.fn();
    }
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) } });
    vi.stubGlobal("RTCRtpSender", { getCapabilities: () => ({ codecs: [{ mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }] }) });
    vi.stubGlobal("RTCPeerConnection", FakePeerConnection);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 201, text: async () => offerSdp }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("pointerdown"));
    await flushPromises();

    expect(api.createTalkSession).toHaveBeenCalledWith(channel.id, "broadcast");
    expect(track.enabled).toBe(false);
    expect(setCodecPreferences).toHaveBeenCalledWith([{ mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }]);
    expect(wrapper.get("[data-testid='talk-button']").text()).toContain("正在建立广播");

    resolveStatus({ code: 0, message: "", data: { sessionId: "talk-1", mode: "broadcast", state: "active", expiresAt: "" } });
    await flushPromises();
    expect(track.enabled).toBe(true);
    expect(wrapper.get("[data-testid='talk-button']").text()).toContain("广播中");

    await wrapper.get("[data-testid='talk-button']").trigger("pointerup");
    await flushPromises();
    expect(track.enabled).toBe(false);
    expect(stop).toHaveBeenCalledTimes(1);
    expect(api.deleteTalkSession).toHaveBeenCalledWith(channel.id, "talk-1");
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

    const joystick = wrapper.get("[aria-label='云台方向摇杆']");
    expect(joystick.attributes("tabindex")).toBe("0");
    await joystick.trigger("keydown", { key: "ArrowUp" });
    await flushPromises();
    expect(api.controlPtz).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "up" }));

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    const record = wrapper.get("[data-testid='advanced-record']");
    expect(record.attributes("disabled")).toBeUndefined();
    expect(record.attributes("title")).toContain("设备上报不支持");
    expect(record.attributes("title")).toContain("仍可尝试");
    await record.trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "record_start" }));

    expect(wrapper.get("[data-testid='talk-button']").attributes("disabled")).toBeUndefined();
    const [broadcastMode, talkMode] = wrapper.findAll(".talk-mode-switch button");
    expect(broadcastMode.attributes("disabled")).toBeUndefined();
    expect(broadcastMode.attributes("title")).toContain("设备上报不支持");
    expect(talkMode.attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='talk-button']").attributes("title")).toContain("设备上报不支持");
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

  it("设备与通道编码变化时即使数据库 ID 相同也丢弃迟到的 DeviceStatus", async () => {
    let resolveOldStatus!: (value: any) => void;
    api.getDeviceStatus
      .mockReturnValueOnce(new Promise(resolve => { resolveOldStatus = resolve; }))
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { state: { recordState: "off", guardState: "armed", freshness: "fresh" }, freshness: "fresh" }
      });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);

    const nextChannel = {
      ...channel,
      deviceId: "34020000001320000002",
      channelId: "0411212999",
      name: "园区南门"
    };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();
    await requestDeviceStatus(wrapper);

    expect(api.startPlay).toHaveBeenLastCalledWith(nextChannel.deviceId, nextChannel.channelId);
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);

    resolveOldStatus({
      code: 0,
      message: "",
      data: { state: { recordState: "on", guardState: "disarmed", freshness: "fresh" }, freshness: "fresh" }
    });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    const facts = wrapper.get("[data-testid='advanced-fact-status']").text();
    expect(facts).toContain("设备未录制");
    expect(facts).toContain("已布防");
    expect(facts).not.toContain("设备录制中");
    wrapper.unmount();
  });

  it("切换设备后丢弃旧 DeviceStatus operation 的迟到轮询结果", async () => {
    vi.useFakeTimers();
    let resolveOldOperation!: (value: any) => void;
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown",
          refreshOperationId: "old-device-status-op"
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { state: { recordState: "off", guardState: "armed", freshness: "fresh" }, freshness: "fresh" }
      });
    api.getPtzOperation.mockReturnValueOnce(new Promise(resolve => { resolveOldOperation = resolve; }));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "old-device-status-op");

    const nextChannel = {
      ...channel,
      deviceId: "34020000001320000003",
      channelId: "0411212888",
      name: "园区西门"
    };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);

    resolveOldOperation(operationResponse("accepted", "old-device-status-op", null));
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    const facts = wrapper.get("[data-testid='advanced-fact-status']").text();
    expect(facts).toContain("设备未录制");
    expect(facts).toContain("已布防");
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
    expect(recordButton.text()).toContain("停止设备端录制");

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

  it("录像与布防的正反动作共享 pending 锁", async () => {
    let resolveControl!: (value: { code: number; message: string; data: Record<string, unknown> }) => void;
    api.controlDevice.mockReturnValueOnce(new Promise(resolve => { resolveControl = resolve; }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    await wrapper.get("[data-testid='advanced-record']").trigger("click");
    await wrapper.get("[data-testid='advanced-record-stop']").trigger("click");
    expect(api.controlDevice).toHaveBeenCalledTimes(1);
    expect(wrapper.get("[data-testid='advanced-record']").attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-testid='advanced-record-stop']").attributes("disabled")).toBeDefined();

    resolveControl({ code: 0, message: "", data: { operationId: "record-op", status: "queued", responseRequired: true } });
    await flushPromises();
    wrapper.unmount();
  });

  it("录像 operation 等待设备应答时不锁住其他高级控制", async () => {
    api.controlDevice
      .mockResolvedValueOnce({ code: 0, message: "", data: { operationId: "record-op", status: "queued", responseRequired: true } })
      .mockResolvedValueOnce({ code: 0, message: "", data: { operationId: "iframe-op", status: "sent", responseRequired: false } })
      .mockResolvedValueOnce({ code: 0, message: "", data: { operationId: "guard-op", status: "queued", responseRequired: true } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    await wrapper.get("[data-testid='advanced-record']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='advanced-record']").attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-testid='advanced-iframe']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='advanced-guard']").attributes("disabled")).toBeUndefined();

    await wrapper.get("[data-testid='advanced-iframe']").trigger("click");
    await wrapper.get("[data-testid='advanced-guard']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "iframe" }));
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "guard_set" }));
    wrapper.unmount();
  });

  it("切换设备后高级控制 operation 的迟到应答不能改写新设备事实", async () => {
    vi.useFakeTimers();
    let resolveOldOperation!: (value: any) => void;
    api.controlDevice.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { operationId: "old-record-op", status: "queued", responseRequired: true }
    });
    api.getPtzOperation.mockReturnValueOnce(new Promise(resolve => { resolveOldOperation = resolve; }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-record']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "old-record-op");

    await wrapper.setProps({
      channel: {
        ...channel,
        deviceId: "34020000001320000004",
        channelId: "0411212777",
        name: "园区东门"
      }
    });
    await flushPromises();

    resolveOldOperation(operationResponse("accepted", "old-record-op", null));
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("未知");
    expect(wrapper.get("[data-testid='advanced-record']").text()).toContain("开始设备端录制");
    wrapper.unmount();
  });

  it("单向高级命令 sent 后不轮询业务应答", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { operationId: "iframe-op", status: "sent", responseRequired: false }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-iframe']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(5000);
    expect(api.getPtzOperation).not.toHaveBeenCalled();
    expect(wrapper.get("[data-testid='advanced-iframe']").attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("高级 operation 缺少 deadline 时只补读一次并收敛为 unknown", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { operationId: "record-no-deadline", status: "queued", responseRequired: true, deadlineAt: null }
    });
    api.getPtzOperation.mockResolvedValue(operationResponse("sent", "record-no-deadline", null));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-record']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='advanced-record']").attributes("disabled")).toBeDefined();

    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    expect(wrapper.get("[data-testid='advanced-record']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='advanced-record']").text()).toContain("开始设备端录制");
    expect(wrapper.get("[data-testid='advanced-record']").text()).toContain("补读一次后结果未知");

    await vi.advanceTimersByTimeAsync(10000);
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("高级 operation 到 deadline 仍未终态时只做一次截止补读并收敛", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-25T03:00:00.000Z"));
    const deadline = "2026-07-25T03:00:01.500Z";
    api.controlDevice.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { operationId: "record-expired", status: "queued", responseRequired: true, deadlineAt: deadline }
    });
    api.getPtzOperation.mockResolvedValue(operationResponse("sent", "record-expired", deadline));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-record']").trigger("click");
    await flushPromises();

    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(2);
    expect(wrapper.get("[data-testid='advanced-record']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='advanced-record']").text()).toContain("操作超过服务端截止时间");

    await vi.advanceTimersByTimeAsync(10000);
    expect(api.getPtzOperation).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("2022 精准定位只发送 Pan Tilt Zoom", async () => {
    api.controlPtzPrecise.mockResolvedValueOnce({ code: 0, message: "", data: { operationId: "precise-op", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    await wrapper.get("[data-testid='ptz-precise-apply']").trigger("click");
    await flushPromises();
    expect(api.controlPtzPrecise).toHaveBeenCalledWith(channel.id, { pan: 180, tilt: 0, zoom: 1 });
    wrapper.unmount();
  });

  it("3D 定位使用画面拖框换算后的真实坐标", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-drag-zoom']").trigger("click");
    const layer = wrapper.get("[data-testid='drag-zoom-layer']");
    vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, left: 0, top: 0, right: 1000, bottom: 1000, width: 1000, height: 1000, toJSON: () => ({})
    } as DOMRect);
    vi.spyOn(wrapper.get("[data-testid='play-window']").element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, left: 0, top: 0, right: 800, bottom: 450, width: 800, height: 450, toJSON: () => ({})
    } as DOMRect);
    await layer.trigger("pointerdown", { clientX: 200, clientY: 100, pointerId: 1, button: 0 });
    await layer.trigger("pointermove", { clientX: 600, clientY: 300, pointerId: 1 });
    await layer.trigger("pointerup", { clientX: 600, clientY: 300, pointerId: 1 });
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({
      action: "drag_zoom_in",
      region: { length: 800, width: 450, midPointX: 400, midPointY: 200, lengthX: 400, lengthY: 200 }
    }));
    wrapper.unmount();
  });

  it("3D 缩小同样先拖框并携带实际画面坐标", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-drag-zoom-out']").trigger("click");
    const layer = wrapper.get("[data-testid='drag-zoom-layer']");
    vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, left: 0, top: 0, right: 800, bottom: 450, width: 800, height: 450, toJSON: () => ({})
    } as DOMRect);
    await layer.trigger("pointerdown", { clientX: 200, clientY: 100, pointerId: 2, button: 0 });
    await layer.trigger("pointermove", { clientX: 600, clientY: 300, pointerId: 2 });
    await layer.trigger("pointerup", { clientX: 600, clientY: 300, pointerId: 2 });
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(channel.id, expect.objectContaining({
      action: "drag_zoom_out",
      region: { length: 800, width: 450, midPointX: 400, midPointY: 200, lengthX: 400, lengthY: 200 }
    }));
    wrapper.unmount();
  });

  it("DeviceStatus 缺失时保持 unknown,且 pointercancel 不下发拖框命令", async () => {
    api.getDeviceStatus.mockResolvedValue({
      code: 0,
      message: "",
      data: { state: { freshness: "fresh" }, freshness: "fresh" }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("未知");
    expect(wrapper.get("[data-testid='advanced-record-stop']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='advanced-guard-reset']").attributes("disabled")).toBeUndefined();

    await wrapper.get("[data-testid='advanced-drag-zoom']").trigger("click");
    const layer = wrapper.get("[data-testid='drag-zoom-layer']");
    vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue({
      x: 0, y: 0, left: 0, top: 0, right: 800, bottom: 450, width: 800, height: 450, toJSON: () => ({})
    } as DOMRect);
    await layer.trigger("pointerdown", { clientX: 200, clientY: 100, pointerId: 4, button: 0 });
    await layer.trigger("pointercancel", { clientX: 600, clientY: 300, pointerId: 4 });
    await flushPromises();
    expect(api.controlDevice).not.toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "drag_zoom_in" }));
    wrapper.unmount();
  });

  it("录像与布防状态 unknown 时分别提供明确的正反动作", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    expect(wrapper.get("[data-testid='advanced-record']").text()).toContain("开始设备端录制");
    expect(wrapper.get("[data-testid='advanced-record-stop']").text()).toContain("请求停止设备录制");
    expect(wrapper.get("[data-testid='advanced-guard']").text()).toContain("布防");
    expect(wrapper.get("[data-testid='advanced-guard-reset']").text()).toContain("请求撤防");

    await wrapper.get("[data-testid='advanced-record-stop']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "record_stop" }));

    await wrapper.get("[data-testid='advanced-guard-reset']").trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "guard_reset" }));
    wrapper.unmount();
  });

  it("DeviceStatus 按 operation 等待慢应答后再读取设备事实", async () => {
    vi.useFakeTimers();
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown",
          refreshOperationId: "device-status-op"
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "on", guardState: "on", freshness: "fresh" },
          freshness: "fresh"
        }
      });
    api.getPtzOperation
      .mockResolvedValueOnce(operationResponse("sent", "device-status-op", null))
      .mockResolvedValueOnce(operationResponse("accepted", "device-status-op", null));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledTimes(2);
    expect(api.getPtzOperation).toHaveBeenLastCalledWith(channel.id, "device-status-op");
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(channel.id, false);
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("设备录制中");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("已布防");
    wrapper.unmount();
  });

  it("DeviceStatus 等待录像与报警 operation 全部终态后只读取一次合并事实", async () => {
    vi.useFakeTimers();
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown",
          refreshOperationId: "record-status-op",
          recordRefreshOperationId: "record-status-op",
          alarmRefreshOperationId: "alarm-status-op",
          refreshOperationIds: { record: "record-status-op", alarm: "alarm-status-op" }
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "on", guardState: "alarm", freshness: "fresh" },
          recordState: "on",
          guardState: "alarm",
          freshness: "fresh",
          alarmResolution: {
            status: "resolved",
            source: "direct_parent",
            targetCode: "A1",
            state: "alarm",
            freshness: "fresh",
            candidates: [{ code: "A1", name: "门磁" }]
          },
          alarmFacts: [{ targetCode: "A1", guardState: "alarm", freshness: "fresh" }]
        }
      });
    let alarmPolls = 0;
    api.getPtzOperation.mockImplementation((_channelId: number, operationId: string) => {
      if (operationId === "record-status-op") return Promise.resolve(operationResponse("accepted", operationId, null));
      alarmPolls += 1;
      return Promise.resolve(operationResponse(alarmPolls === 1 ? "sent" : "accepted", operationId, null));
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "record-status-op");
    expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "alarm-status-op");
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(2000);
    await flushPromises();
    expect(api.getPtzOperation.mock.calls.filter(([, id]) => id === "record-status-op")).toHaveLength(1);
    expect(api.getPtzOperation.mock.calls.filter(([, id]) => id === "alarm-status-op")).toHaveLength(2);
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(channel.id, false);

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    const factStatus = wrapper.get("[data-testid='advanced-fact-status']");
    expect(factStatus.text()).toContain("设备录制中");
    expect(factStatus.text()).toContain("ALARM 报警中");
    expect(factStatus.text()).toContain("A1");
    wrapper.unmount();
  });

  it("DeviceStatus 非终态 operation 到 deadline 后才读取合并事实", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-25T03:00:00.000Z"));
    const alarmDeadline = "2026-07-25T03:00:01.500Z";
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown",
          refreshOperationIds: { record: "record-deadline-op", alarm: "alarm-deadline-op" }
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "on", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown"
        }
      });
    api.getPtzOperation.mockImplementation((_channelId: number, operationId: string) => {
      if (operationId === "record-deadline-op") return Promise.resolve(operationResponse("accepted", operationId, null));
      return Promise.resolve(operationResponse("sent", operationId, alarmDeadline));
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(499);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(1);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(channel.id, false);
    wrapper.unmount();
  });

  it("DeviceStatus operation 查询卡住时在截止时间结束为未知并补读事实", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-25T03:00:00.000Z"));
    let resolveHungOperation!: (value: ReturnType<typeof operationResponse>) => void;
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown",
          refreshOperationId: "hung-device-status-op"
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown"
        }
      });
    api.getPtzOperation.mockImplementation(() => new Promise<ReturnType<typeof operationResponse>>((resolve) => {
      resolveHungOperation = resolve;
    }));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    await vi.advanceTimersByTimeAsync(1000);
    expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "hung-device-status-op");

    await vi.advanceTimersByTimeAsync(14000);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    expect(api.getDeviceStatus).toHaveBeenLastCalledWith(channel.id, false);
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("状态读取失败");
    resolveHungOperation(operationResponse("accepted", "hung-device-status-op", null));
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("DeviceStatus 最终补读卡住时不会在 operation 截止后一直保持查询中", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-25T03:00:00.000Z"));
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          state: { recordState: "unknown", guardState: "unknown", freshness: "unknown" },
          freshness: "unknown",
          refreshOperationId: "hung-final-read-op"
        }
      })
      .mockImplementationOnce(() => new Promise(() => {}));
    api.getPtzOperation.mockImplementation(() => new Promise(() => {}));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    await vi.advanceTimersByTimeAsync(15000);
    await flushPromises();
    expect(api.getDeviceStatus).toHaveBeenCalledTimes(2);

    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).not.toContain("正在查询设备状态");
    expect(wrapper.get(".advanced-status-refresh").attributes("disabled")).toBeUndefined();

    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("状态读取失败");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("最终补读超时");
    wrapper.unmount();
  });

  it("DeviceStatus 明示报警目标歧义与各报警事实且不封禁控制", async () => {
    api.getDeviceStatus.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        state: { recordState: "off", guardState: "unknown", freshness: "unknown" },
        recordState: "off",
        guardState: "unknown",
        freshness: "unknown",
        completeness: "partial",
        alarmResolution: {
          status: "ambiguous",
          source: "direct_parent",
          targetCode: "",
          state: "unknown",
          freshness: "unknown",
          candidates: [{ code: "A1", name: "门磁 1" }, { code: "A2", name: "门磁 2" }]
        },
        alarmFacts: [
          { targetCode: "A1", guardState: "on", freshness: "fresh" },
          { targetCode: "A2", guardState: "alarm", freshness: "fresh" }
        ]
      }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);

    const warning = wrapper.get("[data-testid='alarm-resolution-warning']");
    expect(warning.text()).toContain("报警目标不明确");
    expect(warning.text()).toContain("A1");
    expect(warning.text()).toContain("A2");
    const facts = wrapper.get("[data-testid='alarm-facts']");
    expect(facts.text()).toContain("A1 已布防");
    expect(facts.text()).toContain("A2 ALARM 报警中");
    expect(wrapper.get("[data-testid='advanced-guard']").attributes("disabled")).toBeUndefined();
    expect(wrapper.get("[data-testid='advanced-guard-reset']").attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("DeviceStatus 明示没有可用的 134 报警输入", async () => {
    api.getDeviceStatus.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        state: { recordState: "off", guardState: "unknown", freshness: "unknown" },
        recordState: "off",
        guardState: "unknown",
        freshness: "unknown",
        alarmResolution: {
          status: "unavailable",
          source: "",
          targetCode: "",
          state: "unknown",
          freshness: "unknown",
          candidates: []
        },
        alarmFacts: []
      }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    expect(wrapper.get("[data-testid='alarm-resolution-warning']").text()).toContain("未找到可用的 134 报警输入");
    expect(wrapper.get("[data-testid='alarm-resolution-warning']").text()).toContain("按注册父设备编码发送");
    expect(wrapper.get("[data-testid='advanced-guard']").attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("DeviceStatus 合法响应缺字段时不保留旧事实", async () => {
    api.getDeviceStatus
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { state: { recordState: "on", guardState: "on", freshness: "fresh" }, freshness: "fresh" }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { state: { freshness: "fresh" }, freshness: "fresh" }
      });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("设备录制中");

    await wrapper.get(".advanced-status-refresh").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).not.toContain("设备录制中");
    expect(wrapper.get("[data-testid='advanced-fact-status']").text()).toContain("未知");
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
    expect(dialog.text()).toContain("单位为秒");
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
    expect(api.listCruiseTracks).toHaveBeenCalledWith(channel.id, false);

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

  describe("home position state", () => {
    it("能力尚未确认时只提供查询入口，不渲染误导性的开关和技术枚举", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: null,
        freshness: "unknown",
        controlSupport: { status: "unknown", reason: "能力尚未确认" },
        querySupport: { status: "unknown", reason: "能力尚未确认" }
      }));
      const emptyWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const card = emptyWrapper.get("[data-testid='home-card']");
      expect(emptyWrapper.get("[data-testid='home-phase']").text()).toContain("尚未确认设备能力");
      expect(card.text()).not.toContain("unknown");
      expect(card.text()).not.toContain("新鲜度");
      expect(emptyWrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(emptyWrapper.find("[data-testid='home-fields']").exists()).toBe(false);
      expect(emptyWrapper.get("[data-testid='home-refresh']").text()).toContain("查询设备");
      emptyWrapper.unmount();
    });

    it("设备明确不支持控制和查询时收敛为单一不支持状态", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: null,
        freshness: "unknown",
        controlSupport: { status: "unsupported", reason: "设备未上报控制能力" },
        querySupport: { status: "unsupported", reason: "设备未上报查询能力" }
      }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const card = wrapper.get("[data-testid='home-card']");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("设备不支持看守位");
      expect(wrapper.get("[data-testid='home-state-icon']").attributes("data-icon")).toBe("unsupported");
      expect(card.text()).not.toContain("unsupported");
      expect(card.text()).not.toContain("设备未上报控制能力");
      expect(wrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(wrapper.find("[data-testid='home-fields']").exists()).toBe(false);
      expect(wrapper.find("[data-testid='home-save']").exists()).toBe(false);
      wrapper.unmount();
    });

    it("已启用时只展示产品状态和紧凑配置摘要", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: {
          enabled: true,
          resetTime: 300,
          presetId: 3,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const card = wrapper.get("[data-testid='home-card']");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("回位 #3 · 空闲 300 秒");
      expect(card.text()).not.toContain("新鲜度");
      expect(card.text()).not.toContain("查询已验证");
      expect(card.text()).not.toContain("Operation");
      expect(wrapper.find("[data-testid='home-toggle']").exists()).toBe(true);
      expect(wrapper.find("[data-testid='home-fields']").exists()).toBe(true);
      wrapper.unmount();
    });

    it("首次查询失败时显示可重试的用户态，不泄露底层错误", async () => {

      api.getHomePosition.mockRejectedValueOnce(new Error("network unavailable"));
      const failedWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(failedWrapper.get("[data-testid='home-phase']").text()).toContain("暂时无法确认设备状态");
      expect(failedWrapper.get("[data-testid='home-refresh']").text()).toContain("重试");
      expect(failedWrapper.get("[data-testid='home-card']").text()).not.toContain("network unavailable");
      expect(failedWrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
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
      expect((enabledWrapper.get("[data-testid='home-toggle']").element as HTMLInputElement).checked).toBe(true);
      await enabledWrapper.get("[data-testid='home-toggle']").setValue(false);
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
      vi.useFakeTimers();
      vi.setSystemTime("2026-07-22T10:00:00.000Z");
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
      expect(pendingWrapper.get("[data-testid='home-phase']").text()).toContain("等待设备确认");
      expect(pendingWrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("control-pending");
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
      expect(unknownWrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(unknownWrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(unknownWrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("control-unknown");
      expect(unknownWrapper.get("[data-testid='home-save']").attributes("disabled")).toBeUndefined();
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      unknownWrapper.unmount();
    });

    it("恢复查询 pending 且不重新发 refresh", async () => {
      vi.useFakeTimers();
      vi.setSystemTime("2026-07-22T10:00:00.000Z");
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

      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("正在查询设备");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("reconcile-pending");
      expect(api.getHomePosition).toHaveBeenCalledWith(channel.id);
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      wrapper.unmount();
    });

    it("混合能力收进诊断提示，离线时保留最后配置并禁用操作", async () => {
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        controlSupport: { status: "unsupported", reason: "厂商 profile 未声明控制" },
        querySupport: { status: "unknown", reason: "尚未收到合法查询应答" }
      }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("厂商 profile 未声明控制");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("尚未收到合法查询应答");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("厂商 profile 未声明控制");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("尚未收到合法查询应答");
      expect(wrapper.get("[data-testid='home-save']").attributes("disabled")).toBeUndefined();
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();

      await wrapper.setProps({ channel: { ...channel, status: 0 } });
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("设备离线");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("上次确认：已启用");
      expect(wrapper.get("[data-testid='home-save']").attributes("disabled")).toBeDefined();
      expect(wrapper.find("[data-testid='home-refresh']").exists()).toBe(false);
      wrapper.unmount();
    });
  });

  describe("home position operations", () => {
    const nowIso = "2026-07-22T10:00:00.000Z";
    const deadline = (seconds: number) => new Date(Date.parse(nowIso) + seconds * 1000).toISOString();

    it("启用保留 #0 边界，非法启用零请求，关闭只发送 enabled=false", async () => {
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
      const enableWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await enableWrapper.get("[data-testid='home-toggle']").setValue(true);
      await enableWrapper.get("[data-testid='home-preset']").setValue("0");
      await enableWrapper.get("[data-testid='home-reset-time']").setValue("10");
      await enableWrapper.get("[data-testid='home-save']").trigger("click");
      await flushPromises();

      expect(api.updateHomePosition).toHaveBeenCalledWith(
        channel.id,
        { enabled: true, resetTime: 10, presetId: 0 },
        expect.stringMatching(/^home-control-/)
      );
      enableWrapper.unmount();

      api.updateHomePosition.mockClear();
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        homePosition: {
          enabled: true,
          resetTime: 9,
          presetId: 0,
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      }));
      const closeWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(closeWrapper.get("[data-testid='home-save']").attributes("disabled")).toBeDefined();
      await closeWrapper.get("[data-testid='home-save']").trigger("click");
      expect(api.updateHomePosition).not.toHaveBeenCalled();

      await closeWrapper.get("[data-testid='home-toggle']").setValue(false);
      await closeWrapper.get("[data-testid='home-save']").trigger("click");
      await flushPromises();
      expect(api.updateHomePosition).toHaveBeenCalledWith(
        channel.id,
        { enabled: false },
        expect.stringMatching(/^home-control-/)
      );
      closeWrapper.unmount();
    });

    it("deferred PATCH 与 refresh 均防双击", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      let resolvePatch!: (value: any) => void;
      api.updateHomePosition.mockReturnValueOnce(new Promise(resolve => { resolvePatch = resolve; }));
      const patchWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await patchWrapper.get("[data-testid='home-save']").trigger("click");
      await patchWrapper.get("[data-testid='home-save']").trigger("click");
      expect(api.updateHomePosition).toHaveBeenCalledTimes(1);
      expect(patchWrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("true");
      resolvePatch({
        code: 0,
        message: "",
        data: { operationId: "patch-once", sn: 1, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      await flushPromises();
      expect(patchWrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("patch-once");
      patchWrapper.unmount();

      let resolveRefresh!: (value: any) => void;
      api.getHomePosition
        .mockResolvedValueOnce(homeResponse())
        .mockReturnValueOnce(new Promise(resolve => { resolveRefresh = resolve; }));
      const refreshWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();
      api.getHomePosition.mockClear();

      await refreshWrapper.get("[data-testid='home-refresh']").trigger("click");
      await refreshWrapper.get("[data-testid='home-refresh']").trigger("click");
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      resolveRefresh(homeResponse({
        refresh: { status: "pending", operationId: "refresh-once", errorCode: null, deadlineAt: deadline(10) }
      }));
      await flushPromises();
      expect(refreshWrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("refresh-once");
      refreshWrapper.unmount();
    });

    it("PATCH queued 无 deadline 时首轮 hung operation 由临时保险截止收敛且迟到结果不复活", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "queued-without-deadline", sn: 1, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      let resolveOperation!: (value: any) => void;
      api.getPtzOperation.mockReturnValueOnce(new Promise(resolve => { resolveOperation = resolve; }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-save']").trigger("click");
      await flushPromises();
      await vi.advanceTimersByTimeAsync(1000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(6000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("queued-without-deadline");

      resolveOperation(operationResponse("accepted", "queued-without-deadline", null));
      await flushPromises();
      await vi.advanceTimersByTimeAsync(5000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      wrapper.unmount();
    });

    it("PATCH 临时保险截止会被 operation 最新 deadline 覆盖且合法响应清除网络错误", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "queued-then-sent", sn: 1, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      api.getHomePosition.mockResolvedValueOnce(homeResponse()).mockResolvedValueOnce(homeResponse());
      api.getPtzOperation
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockResolvedValueOnce(operationResponse("sent", "queued-then-sent", deadline(10)))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockResolvedValueOnce(operationResponse("accepted", "queued-then-sent", null));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-save']").trigger("click");
      await flushPromises();
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("temporary network error");

      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(wrapper.find("[data-testid='home-notice']").exists()).toBe(false);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("等待设备确认");

      for (let second = 3; second <= 8; second += 1) {
        await vi.advanceTimersByTimeAsync(1000);
        await flushPromises();
      }
      expect(api.getPtzOperation).toHaveBeenCalledTimes(8);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      wrapper.unmount();
    });

    it.each(["accepted", "unknown", "rejected", "timeout", "cancelled"] as const)(
      "PATCH 同步返回 %s 时不显示等待设备确认提示",
      async status => {
        const info = vi.spyOn(Message, "info");
        api.updateHomePosition.mockResolvedValueOnce({
          code: 0,
          message: "",
          data: { operationId: `sync-${status}`, sn: 1, channelId: channel.channelId, action: "home_position", status }
        });
        const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
        await flushPromises();
        info.mockClear();

        await wrapper.get("[data-testid='home-save']").trigger("click");
        await flushPromises();
        expect(info).not.toHaveBeenCalled();

        wrapper.unmount();
        info.mockRestore();
      }
    );

    it("显式刷新只发一次 refresh=true，之后精确轮询 operation", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition
        .mockResolvedValueOnce(homeResponse())
        .mockResolvedValueOnce(homeResponse({
          refresh: { status: "pending", operationId: "refresh-op", errorCode: null, deadlineAt: deadline(10) }
        }))
        .mockResolvedValueOnce(homeResponse({
          freshness: "stale",
          refresh: { status: "succeeded_no_data", operationId: "refresh-op", errorCode: null, deadlineAt: null }
        }));
      api.getPtzOperation.mockResolvedValueOnce(operationResponse("accepted", "refresh-op", null));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-refresh']").trigger("click");
      await flushPromises();
      expect(api.getHomePosition).toHaveBeenNthCalledWith(2, channel.id, true, expect.stringMatching(/^home-refresh-/));

      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "refresh-op");
      expect(api.getHomePosition).toHaveBeenNthCalledWith(3, channel.id);
      expect(api.getHomePosition.mock.calls.filter((call: any[]) => call[1] === true)).toHaveLength(1);
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("缓存已过期");
      wrapper.unmount();
    });

    it("页面重载恢复精确 operation，并按 queued -> sent 的最新 deadline 延长保险截止", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition
        .mockResolvedValueOnce(homeResponse({
          control: {
            status: "pending",
            operationId: "reload-control",
            action: "home_position",
            errorCode: null,
            deadlineAt: deadline(2)
          }
        }))
        .mockResolvedValueOnce(homeResponse());
      api.getPtzOperation
        .mockResolvedValueOnce(operationResponse("sent", "reload-control", deadline(10)))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockRejectedValueOnce(new Error("temporary network error"))
        .mockResolvedValueOnce(operationResponse("accepted", "reload-control", null));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(api.updateHomePosition).not.toHaveBeenCalled();
      for (let second = 1; second <= 5; second += 1) {
        await vi.advanceTimersByTimeAsync(1000);
        await flushPromises();
      }

      expect(api.getPtzOperation).toHaveBeenCalledTimes(5);
      expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "reload-control");
      expect(api.getHomePosition.mock.calls.filter((call: any[]) => call[1] === true)).toHaveLength(0);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      wrapper.unmount();
    });

    it("持续网络失败只重试到服务端 deadline+2s，随后停止为 unknown", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        control: {
          status: "pending",
          operationId: "network-timeout",
          action: "home_position",
          errorCode: null,
          deadlineAt: deadline(2)
        }
      }));
      api.getPtzOperation.mockRejectedValue(new Error("network unavailable"));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(4000);
      await flushPromises();
      const callsAtCutoff = api.getPtzOperation.mock.calls.length;
      expect(callsAtCutoff).toBeGreaterThan(0);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("network-timeout");

      await vi.advanceTimersByTimeAsync(5000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(callsAtCutoff);
      wrapper.unmount();
    });

    it("单个 operation 请求卡住时由 deadline watchdog 收敛且迟到结果不复活", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        control: {
          status: "pending",
          operationId: "hung-operation",
          action: "home_position",
          errorCode: null,
          deadlineAt: deadline(2)
        }
      }));
      let resolveOperation!: (value: any) => void;
      api.getPtzOperation.mockReturnValueOnce(new Promise(resolve => { resolveOperation = resolve; }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(1000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(3000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");

      resolveOperation(operationResponse("accepted", "hung-operation", null));
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      wrapper.unmount();
    });

    it.each([
      ["缺失", null],
      ["已过期", "2026-07-22T09:59:59.000Z"]
    ])("pending operation 的%s deadline 重读一次后仍非法即停止", async (_label, invalidDeadline) => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      const invalid = homeResponse({
        control: {
          status: "pending",
          operationId: "invalid-deadline",
          action: "home_position",
          errorCode: null,
          deadlineAt: invalidDeadline
        }
      });
      api.getHomePosition.mockResolvedValueOnce(invalid).mockResolvedValueOnce(invalid);
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(api.getHomePosition).toHaveBeenCalledTimes(2);
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("截止时间");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      wrapper.unmount();
    });

    it.each([
      ["rejected", "DEVICE_REJECTED"],
      ["timeout", "APPLICATION_TIMEOUT"]
    ] as const)("控制 %s 回滚草稿到最后确认值并保留 operation ID", async (status, errorCode) => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: `control-${status}`, sn: 2, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      api.getPtzOperation.mockResolvedValueOnce(
        operationResponse(status, `control-${status}`, null, errorCode)
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-toggle']").setValue(false);
      await wrapper.get("[data-testid='home-save']").trigger("click");
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();

      expect((wrapper.get("[data-testid='home-toggle']").element as HTMLInputElement).checked).toBe(true);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain(errorCode);
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain(`control-${status}`);
      wrapper.unmount();
    });

    it("控制 accepted 后确认状态读取失败时保留提交草稿和 accepted operation", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition
        .mockResolvedValueOnce(homeResponse({
          homePosition: {
            enabled: false,
            resetTime: null,
            presetId: null,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        }))
        .mockRejectedValueOnce(new Error("read model unavailable"));
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "accepted-read-failed", sn: 3, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      api.getPtzOperation.mockResolvedValueOnce(operationResponse("accepted", "accepted-read-failed", null));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-toggle']").setValue(true);
      await wrapper.get("[data-testid='home-preset']").setValue("0");
      await wrapper.get("[data-testid='home-reset-time']").setValue("30");
      await wrapper.get("[data-testid='home-save']").trigger("click");
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已关闭");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("确认状态读取失败");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("accepted-read-failed");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();
      expect((wrapper.get("[data-testid='home-toggle']").element as HTMLInputElement).checked).toBe(true);
      expect((wrapper.get("[data-testid='home-preset']").element as HTMLSelectElement).value).toBe("0");
      expect((wrapper.get("[data-testid='home-reset-time']").element as HTMLInputElement).value).toBe("30");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已关闭");
      wrapper.unmount();
    });

    it("控制 accepted 后观察后端 reconcile，并用设备实际值覆盖草稿及展示 mismatch", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      const disabled = {
        enabled: false,
        resetTime: null,
        presetId: null,
        confirmedAt: "2026-07-22T10:00:00Z",
        source: "device_query",
        verification: "verified"
      };
      api.getHomePosition
        .mockResolvedValueOnce(homeResponse({ homePosition: disabled }))
        .mockResolvedValueOnce(homeResponse({
          homePosition: {
            enabled: true,
            resetTime: 30,
            presetId: 0,
            confirmedAt: "2026-07-22T10:00:01Z",
            source: "control_ack",
            verification: "unverified"
          },
          control: {
            status: "accepted",
            operationId: "control-accepted",
            action: "home_position",
            errorCode: null,
            deadlineAt: null
          },
          refresh: {
            status: "pending",
            operationId: "reconcile-op",
            errorCode: null,
            deadlineAt: deadline(12)
          }
        }))
        .mockResolvedValueOnce(homeResponse({
          homePosition: {
            enabled: true,
            resetTime: 45,
            presetId: 2,
            confirmedAt: "2026-07-22T10:00:02Z",
            source: "device_query",
            verification: "verified"
          },
          control: {
            status: "accepted",
            operationId: "control-accepted",
            action: "home_position",
            errorCode: null,
            deadlineAt: null
          },
          refresh: {
            status: "failed",
            operationId: "reconcile-op",
            errorCode: "HOME_POSITION_RECONCILE_MISMATCH",
            deadlineAt: null
          }
        }));
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "control-accepted", sn: 3, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      api.getPtzOperation
        .mockResolvedValueOnce(operationResponse("accepted", "control-accepted", null))
        .mockResolvedValueOnce(operationResponse("accepted", "reconcile-op", null));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-toggle']").setValue(true);
      await wrapper.get("[data-testid='home-preset']").setValue("0");
      await wrapper.get("[data-testid='home-reset-time']").setValue("30");
      await wrapper.get("[data-testid='home-save']").trigger("click");
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("reconcile-op");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("查询未验证");

      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(api.getPtzOperation).toHaveBeenNthCalledWith(1, channel.id, "control-accepted");
      expect(api.getPtzOperation).toHaveBeenNthCalledWith(2, channel.id, "reconcile-op");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("#2");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("45 秒");
      expect((wrapper.get("[data-testid='home-preset']").element as HTMLSelectElement).value).toBe("2");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备未按请求应用配置");
      wrapper.unmount();
    });

    it.each([
      ["timeout", "timeout"],
      ["no-data", "accepted"]
    ] as const)("control_ack/unverified 后 reconcile %s 保留配置并标记 stale", async (outcome, operationStatus) => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      const disabled = {
        enabled: false,
        resetTime: null,
        presetId: null,
        confirmedAt: "2026-07-22T10:00:00Z",
        source: "device_query",
        verification: "verified"
      };
      const unverified = {
        enabled: true,
        resetTime: 30,
        presetId: 0,
        confirmedAt: "2026-07-22T10:00:01Z",
        source: "control_ack",
        verification: "unverified"
      };
      api.getHomePosition
        .mockResolvedValueOnce(homeResponse({ homePosition: disabled }))
        .mockResolvedValueOnce(homeResponse({
          homePosition: unverified,
          control: {
            status: "accepted",
            operationId: "control-unverified",
            action: "home_position",
            errorCode: null,
            deadlineAt: null
          },
          refresh: {
            status: "pending",
            operationId: `reconcile-${outcome}`,
            errorCode: null,
            deadlineAt: deadline(10)
          }
        }));
      if (outcome === "no-data") {
        api.getHomePosition.mockResolvedValueOnce(homeResponse({
          homePosition: unverified,
          freshness: "stale",
          control: {
            status: "accepted",
            operationId: "control-unverified",
            action: "home_position",
            errorCode: null,
            deadlineAt: null
          },
          refresh: {
            status: "succeeded_no_data",
            operationId: "reconcile-no-data",
            errorCode: null,
            deadlineAt: null
          }
        }));
      }
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "control-unverified", sn: 3, channelId: channel.channelId, action: "home_position", status: "queued" }
      });
      api.getPtzOperation
        .mockResolvedValueOnce(operationResponse("accepted", "control-unverified", null))
        .mockResolvedValueOnce(operationResponse(
          operationStatus,
          `reconcile-${outcome}`,
          null,
          outcome === "timeout" ? "APPLICATION_TIMEOUT" : null
        ));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-toggle']").setValue(true);
      await wrapper.get("[data-testid='home-preset']").setValue("0");
      await wrapper.get("[data-testid='home-reset-time']").setValue("30");
      await wrapper.get("[data-testid='home-save']").trigger("click");
      await vi.advanceTimersByTimeAsync(2000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("#0");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("30 秒");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("查询未验证");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("缓存已过期");
      if (outcome === "timeout") {
        expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
        expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("APPLICATION_TIMEOUT");
      } else {
        expect(wrapper.find("[data-testid='home-notice']").exists()).toBe(false);
      }
      wrapper.unmount();
    });

    it.each([
      ["有缓存", true],
      ["无缓存", false]
    ])("refresh timeout 时%s都结束 pending 并保留重试入口", async (_label, hasCache) => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      const initial = homeResponse({ homePosition: hasCache ? homeResponse().data.homePosition : null, freshness: hasCache ? "fresh" : "unknown" });
      const pending = homeResponse({
        homePosition: hasCache ? homeResponse().data.homePosition : null,
        refresh: { status: "pending", operationId: "refresh-timeout", errorCode: null, deadlineAt: deadline(10) }
      });
      api.getHomePosition.mockResolvedValueOnce(initial).mockResolvedValueOnce(pending);
      api.getPtzOperation.mockResolvedValueOnce(
        operationResponse("timeout", "refresh-timeout", null, "APPLICATION_TIMEOUT")
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-refresh']").trigger("click");
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();
      if (hasCache) {
        expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
        expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("缓存已过期");
        expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      } else {
        expect(wrapper.get("[data-testid='home-phase']").text()).toContain("暂时无法确认设备状态");
        expect(wrapper.get("[data-testid='home-refresh']").text()).toContain("重试");
      }
      wrapper.unmount();
    });

    it("operation unknown 立即停止且不自动重发控制", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition.mockResolvedValueOnce(homeResponse({
        control: {
          status: "pending",
          operationId: "explicit-unknown",
          action: "home_position",
          errorCode: null,
          deadlineAt: deadline(10)
        }
      }));
      api.getPtzOperation.mockResolvedValueOnce(
        operationResponse("unknown", "explicit-unknown", null, "TRANSPORT_UNKNOWN")
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("explicit-unknown");
      expect(api.updateHomePosition).not.toHaveBeenCalled();

      await vi.advanceTimersByTimeAsync(5000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      wrapper.unmount();
    });

    it.each(["切换通道", "关闭弹窗", "卸载"])("operation 请求重叠时%s会隔离迟到 promise 和 timer", async action => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      const oldPending = homeResponse({
        control: {
          status: "pending",
          operationId: "late-operation",
          action: "home_position",
          errorCode: null,
          deadlineAt: deadline(20)
        }
      });
      const nextChannel = { ...channel, id: 2, channelId: "0411212756", name: "园区南门" };
      api.getHomePosition.mockImplementation((channelId: number) =>
        Promise.resolve(channelId === channel.id
          ? oldPending
          : homeResponse({
              homePosition: {
                enabled: false,
                resetTime: null,
                presetId: null,
                confirmedAt: "2026-07-22T10:00:00Z",
                source: "device_query",
                verification: "verified"
              }
            }))
      );
      let resolveOperation!: (value: any) => void;
      api.getPtzOperation.mockReturnValueOnce(new Promise(resolve => { resolveOperation = resolve; }));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      await vi.advanceTimersByTimeAsync(3000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);

      if (action === "切换通道") {
        await wrapper.setProps({ channel: nextChannel });
        await flushPromises();
      } else if (action === "关闭弹窗") {
        await wrapper.setProps({ visible: false });
      } else {
        wrapper.unmount();
      }

      resolveOperation(operationResponse("accepted", "late-operation", null));
      await flushPromises();
      await vi.advanceTimersByTimeAsync(5000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      if (action === "切换通道") {
        expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已关闭");
        expect(wrapper.text()).not.toContain("late-operation");
      }
      if (action !== "卸载") wrapper.unmount();
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

    expect(api.updateHomePosition).toHaveBeenCalledWith(
      channel.id,
      { enabled: false },
      expect.stringMatching(/^home-control-/)
    );
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

    expect(api.updateHomePosition).toHaveBeenCalledWith(
      channel.id,
      { enabled: true, resetTime: 10, presetId: 1 },
      expect.stringMatching(/^home-control-/)
    );
    wrapper.unmount();
  });

});
