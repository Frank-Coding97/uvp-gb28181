import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { Message, Modal } from "@arco-design/web-vue";
import Cookies from "js-cookie";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineComponent, nextTick, reactive } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AccessTokenKey } from "@/utils/auth";

// VChart 会拉起 lottie-web 这类浏览器专属依赖,jsdom 下拿不到 canvas 上下文就会整包
// 加载失败。本文件只覆盖概览条与弹窗的开关行为,图表细节由 ProbeTimelineDialog 自己负责。
const vchartStub = vi.hoisted(() => ({ created: 0, rendered: 0, updated: 0, resized: 0, released: 0 }));

vi.mock("@visactor/vchart", () => ({
  default: class {
    constructor() {
      vchartStub.created += 1;
    }
    renderSync() {
      vchartStub.rendered += 1;
    }
    updateSpecSync() {
      vchartStub.updated += 1;
    }
    resize() {
      vchartStub.resized += 1;
    }
    release() {
      vchartStub.released += 1;
    }
    on() {
      return undefined;
    }
  }
}));

const api = vi.hoisted(() => {
  const presets = Array.from({ length: 20 }, (_, index) => ({
    presetId: index + 1,
    name: `预置位 ${index + 1}`,
    updatedAt: "2026-07-22T10:00:00Z"
  }));
  const cruises = Array.from({ length: 20 }, (_, index) => ({ trackId: index + 1, name: `巡航 ${index + 1}`, enabled: true }));
  return {
    startPlay: vi.fn().mockResolvedValue({
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
    authorizeFixedPlayback: vi.fn(),
    stopPlay: vi.fn().mockResolvedValue({ code: 0, message: "", data: null }),
    getStreamMonitor: vi.fn().mockResolvedValue({
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
    createStreamProbe: vi.fn(),
    getStreamProbeOperation: vi.fn(),
    getControlCapabilities: vi.fn().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        basicPtz: { state: "supported", reason: "" },
        iFrame: { state: "supported", reason: "" },
        record: { state: "supported", reason: "" },
        guard: { state: "supported", reason: "" },
        alarmReset: { state: "supported", reason: "" },
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
    // 存储卡状态(A.2.4.14/A.2.6.16)。默认回**空列表** —— 空列表是合法结果
    // （设备没装卡），不是错误；需要"有卡"的用例自己 mockResolvedValueOnce 覆盖。
    getChannelStorageCards: vi.fn().mockResolvedValue({
      code: 0,
      message: "",
      data: { list: [], freshness: "unknown", refreshOperationId: null, refreshError: null }
    }),
    // 视频参数属性(A.2.1.13 写 / A.2.3.2.5 读)。默认回**空列表 + never_read** ——
    // "设备还没被回读过视频参数"是合法状态(2016 设备本就无此配置类型),不是错误;
    // 需要"有值"的用例自己 mockResolvedValueOnce 覆盖。
    // ⛔ 应答里**没有**下发回显:Result=OK 不代表配置生效,结论只能来自 reconcile。
    getChannelVideoParams: vi.fn().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        list: [],
        freshness: "unknown",
        targetCode: "0411212755",
        streamNumberList: "",
        reconcile: { state: "never_read" },
        refreshOperationId: null,
        refreshError: null
      }
    }),
    applyChannelVideoParams: vi.fn().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        operationId: "video-param-op-1",
        channelId: "1",
        action: "refresh_video_params",
        sn: 1,
        status: "queued",
        streamCount: 1,
        reconcilePending: true
      }
    }),
    fetchPTZDefaultSpeedConfig: vi.fn().mockResolvedValue({ code: 0, message: "", data: { level: 6 } }),
    listPtzPresets: vi.fn().mockResolvedValue({ code: 0, message: "", data: { list: presets, freshness: "fresh" } }),
    listCruiseTracks: vi.fn().mockResolvedValue({ code: 0, message: "", data: { list: cruises, freshness: "fresh" } }),
    // 单条轨迹回读。「设备上这条轨迹走哪几个预置位」只能靠它拿到 —— 清单查询的
    // 应答里没有点位集合(标准 A.2.6.13 只有 <Number/> 和 <Name/>)。
    getCruiseTrack: vi.fn().mockResolvedValue({ code: 0, message: "", data: { track: {}, freshness: "fresh" } }),
    getHomePosition: vi.fn().mockResolvedValue({
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
    createDeviceSnapshotSession: vi.fn(),
    getDeviceSnapshotSession: vi.fn(),
    createTalkSession: vi.fn(),
    getTalkSession: vi.fn(),
    deleteTalkSession: vi.fn(),
    // 画面设置走通用配置通道：读取问的是全量 ConfigType 并集，
    // 底栏卡片的遮挡/镜像数据就来自这一次读取。
    getChannelDeviceConfigs: vi.fn(),
    applyChannelDeviceConfigs: vi.fn()
  };
});

const userState = vi.hoisted(() => ({ account: { permissions: ["*:*:*"] as string[] } }));

vi.mock("@/api/gb28181", () => api);
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => userState }));
vi.mock("./PlayWindow.vue", () => ({
  default: {
    props: ["url", "zlmWebrtc", "hasAudio"],
    emits: ["error", "videosize"],
    template:
      "<button class='play-window' data-testid='play-window' :data-url='url' :data-zlm-webrtc='String(Boolean(zlmWebrtc))' :data-has-audio='String(Boolean(hasAudio))' @click=\"$emit('error', '拉流超时')\" />",
    // 真实播放器每秒报一次画面解码尺寸；stub 也报一次，否则遮挡框选没有坐标基准。
    mounted(this: any) {
      this.$emit("videosize", { width: 1280, height: 720 });
    },
    methods: {
      refreshVideoSize(this: any) {
        this.$emit("videosize", { width: 1280, height: 720 });
      }
    },
    expose: ["refreshVideoSize"]
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

/**
 * 画面设置（镜像 + 隐私遮挡）的设备回读应答。
 *
 * ⛔ `payload` 是**那一块本身**，不是包在 `pictureMask` 键下的容器。
 * ⛔ 遮挡按 `Seq` 归位：设备只回 `Seq=1` 时，其余槽位是"空位"而不是"第 1 个"。
 */
/**
 * 画面组读应答。
 *
 * `mask` 默认是"启用 + 一个区域"的常规形态；传 `{ on: 0 }` 复现 2026-09-19 现场的
 * **停用但区域残留**（国标停用只关 `On`、不清 `RegionList`）。
 */
function pictureDeviceConfigResponse(
  mask: { on: number; regions: Array<Record<string, number>> } = {
    on: 1,
    regions: [{ seq: 1, left: 10, top: 20, right: 300, bottom: 400 }]
  },
  /**
   * 设备声明的图像坐标画布（`OSDConfig` 的 `Length/Width`）。
   *
   * ⛔ 不传 = 设备没回 `OSDConfig`（真机 2016 版设备、或该类型没读到）。
   *    此时平台退回画面解码尺寸，卡片必须把"基准未验证"标出来。
   */
  canvas?: { length: number; width: number }
) {
  return {
    code: 0,
    message: "",
    data: {
      list: [
        {
          configType: "FrameMirror",
          observedAt: "2026-09-19T02:10:00Z",
          sourceOperationId: "1402",
          payload: { value: 0 }
        },
        ...(canvas
          ? [
              {
                configType: "OSDConfig",
                observedAt: "2026-09-19T02:10:00Z",
                sourceOperationId: "1402",
                payload: { ...canvas, timeX: 0, timeY: 32, timeEnable: 1, timeType: 1, textEnable: 0, items: [] }
              }
            ]
          : []),
        {
          configType: "PictureMask",
          observedAt: "2026-09-19T02:10:00Z",
          sourceOperationId: "1402",
          payload: mask
        }
      ],
      absentTypes: [],
      registeredVersion: "2022",
      freshness: "fresh",
      observedAt: "2026-09-19T02:10:00Z",
      reconcile: {
        state: "read_ok",
        operationId: "1402",
        status: "accepted",
        responseHasData: true,
        derivedFromApply: false
      },
      refreshOperationId: null,
      refreshError: null
    }
  };
}

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
      completedAt: ["accepted", "rejected", "timeout", "unknown", "cancelled"].includes(status) ? "2026-07-22T10:00:05Z" : null,
      deadlineAt
    }
  };
}

async function requestDeviceStatus(wrapper: VueWrapper) {
  await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
  await wrapper.get(".advanced-status-refresh").trigger("click");
  await flushPromises();
}

/** 视频参数回读应答。`reconcile` 是唯一权威结论(下发应答没有回显)。 */
function videoParamsResponse(overrides: Record<string, unknown> = {}) {
  return {
    code: 0,
    message: "",
    data: {
      list: [],
      freshness: "unknown",
      targetCode: channel.channelId,
      streamNumberList: "",
      registeredVersion: "2022",
      reconcile: { state: "never_read" },
      refreshOperationId: null,
      refreshError: null,
      ...overrides
    }
  };
}

/** 一路码流的回读值（码值字符串，不是人读串）。 */
function videoParamRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    deviceId: 1,
    targetCode: channel.channelId,
    streamNumber: 0,
    videoFormat: "2",
    resolution: "6",
    frameRate: "25",
    bitRateType: "1",
    videoBitRate: "4096",
    observedAt: "2026-09-18T10:00:00Z",
    sourceSn: 7,
    ...overrides
  };
}

async function openHomeSettings(wrapper: VueWrapper) {
  await wrapper.get("[data-testid='home-configure']").trigger("click");
  await nextTick();
}

async function submitHomeSettings(wrapper: VueWrapper) {
  await wrapper.get("[data-testid='home-dialog-submit']").trigger("click");
  await flushPromises();
}

describe("PlayConsoleLinked 双区联动", () => {
  beforeEach(() => {
    userState.account = reactive({ permissions: ["*:*:*"] });
    api.authorizeFixedPlayback.mockReset();
    api.getHomePosition.mockReset();
    api.getPtzOperation.mockReset();
    api.getDeviceStatus.mockReset();
    api.updateHomePosition.mockReset();
    api.createDeviceSnapshotSession.mockReset();
    api.getDeviceSnapshotSession.mockReset();
    api.fetchPTZDefaultSpeedConfig.mockReset();
    api.fetchPTZDefaultSpeedConfig.mockResolvedValue({ code: 0, message: "", data: { level: 6 } });
    api.getCruiseTrack.mockReset();
    api.getCruiseTrack.mockResolvedValue({ code: 0, message: "", data: { track: {}, freshness: "fresh" } });
    api.getChannelDeviceConfigs.mockReset();
    api.getChannelDeviceConfigs.mockResolvedValue(pictureDeviceConfigResponse());
    api.applyChannelDeviceConfigs.mockReset();
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "",
      data: { action: "apply-device-config", reconcilePending: false }
    });
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
    });
    api.controlDevice.mockResolvedValue({
      code: 0,
      message: "",
      data: { operationId: "op-1", action: "accepted", status: "accepted" }
    });
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
    api.createDeviceSnapshotSession.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        sessionId: "snap-1",
        channelId: "1",
        channelCode: channel.channelId,
        deviceCode: channel.deviceId,
        snapNum: 2,
        interval: 3,
        state: "waiting",
        receivedCount: 0,
        notifiedCount: 0,
        files: []
      }
    });
    api.getDeviceSnapshotSession.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        sessionId: "snap-1",
        channelId: "1",
        channelCode: channel.channelId,
        deviceCode: channel.deviceId,
        snapNum: 2,
        interval: 3,
        state: "completed",
        receivedCount: 2,
        notifiedCount: 2,
        files: [
          {
            name: "shot-1.jpg",
            size: 1024,
            receivedAt: "2026-08-30T23:30:00+08:00",
            url: "/api/gb28181/device-snapshots/uploads/token/shot-1.jpg"
          }
        ]
      }
    });
    api.createTalkSession.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        sessionId: "talk-1",
        mode: "broadcast",
        state: "reserved",
        expiresAt: "",
        uplink: {
          protocol: "whip",
          url: "/api/gb28181/device-mgmt/talk-sessions/talk-1/uplink",
          contentType: "application/sdp",
          iceServers: [{ urls: ["stun:media.example.test:3478"] }]
        }
      }
    });
    api.getTalkSession.mockResolvedValue({
      code: 0,
      message: "",
      data: { sessionId: "talk-1", mode: "broadcast", state: "active", expiresAt: "" }
    });
    api.deleteTalkSession.mockResolvedValue({ code: 0, message: "", data: { sessionId: "talk-1", state: "ended" } });
    api.getChannelVideoParams.mockReset();
    api.getChannelVideoParams.mockResolvedValue(videoParamsResponse());
    api.applyChannelVideoParams.mockReset();
    api.applyChannelVideoParams.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        operationId: "video-param-op-1",
        channelId: "1",
        action: "apply_video_params",
        sn: 1,
        status: "queued",
        streamCount: 1,
        reconcilePending: true
      }
    });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it("最小化后保留同一个播放器和点播会话，并支持拖动与恢复", async () => {
    const ModalStub = defineComponent({
      name: "PlaybackModalStub",
      inheritAttrs: false,
      props: {
        visible: Boolean,
        modalStyle: Object,
        modalClass: [String, Array]
      },
      template: `
        <div v-if="visible" data-testid="playback-modal-stub" :class="modalClass" :style="modalStyle">
          <slot name="title" />
          <slot />
        </div>
      `
    });
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel, displayMode: "expanded" },
      global: { stubs: { "a-modal": ModalStub } }
    });
    await flushPromises();

    const originalPlayer = wrapper.get("[data-testid='play-window']").element;
    expect(api.startPlay).toHaveBeenCalledTimes(1);
    expect(wrapper.get("[data-testid='play-console-minimize']").text()).toBe("小窗");
    expect(wrapper.get("[data-testid='play-console-close']").text()).toBe("关闭");

    await wrapper.get("[data-testid='play-console-minimize']").trigger("click");
    expect(wrapper.emitted("update:displayMode")?.at(-1)).toEqual(["minimized"]);

    await wrapper.setProps({ displayMode: "minimized" });
    await nextTick();
    expect(wrapper.get("[data-testid='play-window']").element).toBe(originalPlayer);
    expect(wrapper.get("[data-testid='play-console-body']").classes()).toContain("is-minimized");
    expect(api.startPlay).toHaveBeenCalledTimes(1);

    const modal = wrapper.findAllComponents(ModalStub)[0];
    const before = modal.props("modalStyle") as Record<string, string>;
    const title = wrapper.get("[data-testid='play-console-drag-handle']");
    await title.trigger("pointerdown", { button: 0, clientX: 100, clientY: 100 });
    window.dispatchEvent(new MouseEvent("pointermove", { bubbles: true, clientX: 60, clientY: 75 }));
    await nextTick();
    const after = modal.props("modalStyle") as Record<string, string>;
    expect([after.left, after.top]).not.toEqual([before.left, before.top]);
    window.dispatchEvent(new MouseEvent("pointerup", { bubbles: true }));

    await wrapper.get("[data-testid='play-console-restore']").trigger("click");
    expect(wrapper.emitted("update:displayMode")?.at(-1)).toEqual(["expanded"]);
    expect(api.startPlay).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ displayMode: "expanded" });
    await nextTick();
    expect(wrapper.find(".arco-modal-close-btn").exists()).toBe(false);
    await wrapper.get("[data-testid='play-console-close']").trigger("click");
    expect(wrapper.emitted("update:visible")?.at(-1)).toEqual([false]);
    wrapper.unmount();
  });

  it("拖拽摇杆按八方向发送云台指令，松手停止且不展示绝对角度", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const joystick = wrapper.get(".joystick-stage");
    expect(joystick.findAll(".joystick-dot")).toHaveLength(8);
    expect(joystick.findAll(".joystick-label.diagonal")).toHaveLength(4);
    vi.spyOn(joystick.element, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 176,
      bottom: 176,
      width: 176,
      height: 176,
      toJSON: () => ({})
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

    expect(api.controlPtz).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "up", speed: 255 }));
    wrapper.unmount();
  });

  it("云台默认速度加载失败时回退到第六档", async () => {
    api.fetchPTZDefaultSpeedConfig.mockRejectedValueOnce(new Error("network error"));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.get("input[type='range'][max='10']").element).toHaveProperty("value", "6");
    wrapper.unmount();
  });

  it("在高级控制中下发 2022 图像抓拍配置并展示设备上传结果", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='snapshot-count']").setValue("2");
    await wrapper.get("[data-testid='snapshot-interval']").setValue("3");
    await wrapper.get("[data-testid='snapshot-submit']").trigger("click");
    await flushPromises();

    expect(api.createDeviceSnapshotSession).toHaveBeenCalledWith(channel.id, { snapNum: 2, interval: 3 });
    expect(api.getDeviceSnapshotSession).toHaveBeenCalledWith(channel.id, "snap-1");
    expect(wrapper.get(".snapshot-status").text()).toContain("已完成 2/2");
    expect(wrapper.get(".snapshot-results img").attributes("src")).toContain("shot-1.jpg");
    wrapper.unmount();
  });

  it("云台移动期间在视频画面显示对应方向的呼吸箭头，停止后隐藏", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find("[data-testid='ptz-direction-indicator']").exists()).toBe(false);

    const joystick = wrapper.get(".joystick-stage");
    vi.spyOn(joystick.element, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 176,
      bottom: 176,
      width: 176,
      height: 176,
      toJSON: () => ({})
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

  it("把通道音频开关透传给播放器", async () => {
    const playable = {
      code: 0,
      message: "",
      data: {
        streamId: "stream-audio",
        ssrc: "0102030406",
        app: "rtp",
        urls: { wsFlv: "ws://zlm/rtp/audio.live.flv" },
        wsflvUrl: "ws://legacy/rtp/audio.live.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    };

    api.startPlay.mockResolvedValueOnce(playable);
    const withAudio = mount(PlayConsoleLinked, {
      props: { visible: true, channel: { ...channel, audioEnabled: true } }
    });
    await flushPromises();
    expect(withAudio.get("[data-testid='play-window']").attributes("data-has-audio")).toBe("true");
    withAudio.unmount();

    api.startPlay.mockResolvedValueOnce(playable);
    const withoutAudio = mount(PlayConsoleLinked, {
      props: { visible: true, channel: { ...channel, audioEnabled: false } }
    });
    await flushPromises();
    expect(withoutAudio.get("[data-testid='play-window']").attributes("data-has-audio")).toBe("false");
    withoutAudio.unmount();
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
    expect(wrapper.findAll(".proto-btn").map(button => button.text())).toEqual(["WS-FLV", "HTTP-FLV", "HLS", "WebRTC"]);
    expect(
      wrapper
        .findAll(".proto-btn")
        .find(button => button.text() === "WebRTC")
        ?.attributes("disabled")
    ).toBeDefined();

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
    expect(protocolRows.every(row => row.element.lastElementChild?.classList.contains("protocol-copy-btn"))).toBe(true);
    expect(wrapper.findAll(".protocol-option strong").map(label => label.text())).toEqual([
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
    expect(wrapper.findAll(".protocol-option strong").map(label => label.text())).toContain("WebRTC:");
    expect(wrapper.findAll(".proto-btn").map(button => button.text())).toContain("WebRTC");

    const webRtcOption = wrapper.findAll(".protocol-option").find(option => option.text().includes("WebRTC:"));
    await webRtcOption!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith("http://zlm:18080/index/api/webrtc?app=rtp&stream=stream-webrtc&type=play");
    expect(player.attributes("data-url")).toBe("ws://zlm/rtp/stream-webrtc.live.flv");

    const vm = wrapper.vm as unknown as { switchProtocol: (proto: "webrtc") => void };
    vm.switchProtocol("webrtc");
    await flushPromises();

    expect(player.attributes("data-url")).toBe("webrtc://zlm:18080/index/api/webrtc?app=rtp&stream=stream-webrtc&type=play");
    expect(player.attributes("data-zlm-webrtc")).toBe("true");
    wrapper.unmount();
  });

  it("在 HTTP 页面回退复制外部客户端协议地址且长地址不侵占按钮", async () => {
    const originalClipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    const originalExecCommand = Object.getOwnPropertyDescriptor(document, "execCommand");
    const execCommand = vi.fn().mockReturnValue(true);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    Object.defineProperty(document, "execCommand", { configurable: true, value: execCommand });
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: "stream-copy-fallback",
        ssrc: "0102030405",
        app: "rtp",
        urls: {
          wsFlv: "ws://zlm/rtp/stream-copy-fallback.live.flv",
          wsTs: "ws://zlm/rtp/stream-copy-fallback-with-a-very-long-fixed-address.live.ts"
        },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    try {
      await flushPromises();
      const wsTsOption = wrapper.findAll(".protocol-option").find(option => option.text().includes("WS-TS:"));
      const copyButton = wsTsOption!.get(".protocol-copy-btn");

      expect(copyButton.attributes("aria-label")).toBe("复制 WS-TS 地址");
      await copyButton.trigger("click");
      await flushPromises();

      expect(execCommand).toHaveBeenCalledWith("copy");
      expect(document.querySelector("textarea")).toBeNull();
      expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe(
        "ws://zlm/rtp/stream-copy-fallback.live.flv"
      );

      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
      expect(source).toMatch(
        /\.protocol-url\s*\{[^}]*min-width:\s*0;[^}]*overflow:\s*hidden;[^}]*text-overflow:\s*ellipsis;[^}]*white-space:\s*nowrap;/s
      );
    } finally {
      wrapper.unmount();
      if (originalClipboard) Object.defineProperty(navigator, "clipboard", originalClipboard);
      else Reflect.deleteProperty(navigator, "clipboard");
      if (originalExecCommand) Object.defineProperty(document, "execCommand", originalExecCommand);
      else Reflect.deleteProperty(document, "execCommand");
    }
  });

  it("复制固定流的带 token 协议地址前预授权，并复制同协议的新地址", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const fixedStreamID = `${channel.deviceId}_${channel.channelId}`;
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: fixedStreamID,
        ssrc: "0102030405",
        app: "rtp",
        urls: { wsFlv: "ws://zlm/rtp/fixed.live.flv?play_token=expired" },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    api.authorizeFixedPlayback.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: fixedStreamID,
        ssrc: "0102030405",
        app: "rtp",
        urls: { wsFlv: "ws://zlm/rtp/fixed.live.flv?play_token=fresh" },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0,
        authorizationExpiresAt: 1786867200
      }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const option = wrapper.findAll(".protocol-option").find(item => item.text().includes("WS-FLV:"));
    await option!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();

    expect(api.authorizeFixedPlayback).toHaveBeenCalledWith(channel.deviceId, channel.channelId);
    expect(writeText).toHaveBeenCalledWith("ws://zlm/rtp/fixed.live.flv?play_token=fresh");
    wrapper.unmount();
  });

  it("固定流预授权业务失败时拒绝复制旧地址", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const error = vi.spyOn(Message, "error");
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const fixedStreamID = `${channel.deviceId}_${channel.channelId}`;
    const currentURL = "ws://zlm/rtp/fixed.live.flv?play_token=current";
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: fixedStreamID,
        ssrc: "0102030405",
        app: "rtp",
        urls: { wsFlv: currentURL },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    api.authorizeFixedPlayback.mockResolvedValueOnce({ code: 500, message: "authorization rejected", data: null });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const option = wrapper.findAll(".protocol-option").find(item => item.text().includes("WS-FLV:"));
    await option!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();

    expect(api.authorizeFixedPlayback).toHaveBeenCalledWith(channel.deviceId, channel.channelId);
    expect(writeText).not.toHaveBeenCalled();
    expect(error).toHaveBeenCalledWith("播放地址授权失败，请重试");
    wrapper.unmount();
    error.mockRestore();
  });

  it("固定流预授权请求失败时拒绝复制旧地址", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const error = vi.spyOn(Message, "error");
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const fixedStreamID = `${channel.deviceId}_${channel.channelId}`;
    const currentURL = "ws://zlm/rtp/fixed.live.flv?play_token=current";
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: fixedStreamID,
        ssrc: "0102030405",
        app: "rtp",
        urls: { wsFlv: currentURL },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    api.authorizeFixedPlayback.mockRejectedValueOnce(new Error("network unavailable"));

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const option = wrapper.findAll(".protocol-option").find(item => item.text().includes("WS-FLV:"));
    await option!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();

    expect(api.authorizeFixedPlayback).toHaveBeenCalledWith(channel.deviceId, channel.channelId);
    expect(writeText).not.toHaveBeenCalled();
    expect(error).toHaveBeenCalledWith("播放地址授权失败，请重试");
    wrapper.unmount();
    error.mockRestore();
  });

  it("固定流预授权未返回当前协议地址时拒绝复制旧地址", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const error = vi.spyOn(Message, "error");
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const fixedStreamID = `${channel.deviceId}_${channel.channelId}`;
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: fixedStreamID,
        ssrc: "0102030405",
        app: "rtp",
        urls: { wsFlv: "ws://zlm/rtp/fixed.live.flv?play_token=current" },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    api.authorizeFixedPlayback.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId: fixedStreamID,
        ssrc: "",
        app: "rtp",
        urls: { httpFlv: "http://zlm/rtp/fixed.live.flv?play_token=fresh" },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const option = wrapper.findAll(".protocol-option").find(item => item.text().includes("WS-FLV:"));
    await option!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();

    expect(writeText).not.toHaveBeenCalled();
    expect(error).toHaveBeenCalledWith("播放地址授权失败，请重试");
    wrapper.unmount();
    error.mockRestore();
  });

  it.each([
    ["动态流", "stream-dynamic", "ws://zlm/rtp/dynamic.live.flv?play_token=present"],
    ["固定流无 token", `${channel.deviceId}_${channel.channelId}`, "ws://zlm/rtp/fixed.live.flv"],
    ["固定流仅有同名参数", `${channel.deviceId}_${channel.channelId}`, "ws://zlm/rtp/fixed.live.flv?not_play_token=present"],
    ["非当前固定流", "other-device_other-channel", "ws://zlm/rtp/other.live.flv?play_token=present"]
  ])("%s 复制地址时不请求预授权", async (_label, streamId, url) => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        streamId,
        ssrc: "0102030405",
        app: "rtp",
        urls: { wsFlv: url },
        wsflvUrl: "",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });

    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const option = wrapper.findAll(".protocol-option").find(item => item.text().includes("WS-FLV:"));
    await option!.get(".protocol-copy-btn").trigger("click");
    await flushPromises();

    expect(api.authorizeFixedPlayback).not.toHaveBeenCalled();
    expect(writeText).toHaveBeenCalledWith(url);
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
    api.createStreamProbe.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        operationId: "probe-op-1",
        streamId: "stream-1",
        durationMs: 3000,
        status: "queued",
        createdAt: "2026-07-22T10:00:00Z",
        deadlineAt: "2026-07-22T10:00:13Z"
      }
    });
    api.getStreamProbeOperation.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        operationId: "probe-op-1",
        streamId: "stream-1",
        durationMs: 3000,
        status: "completed",
        createdAt: "2026-07-22T10:00:00Z",
        completedAt: "2026-07-22T10:00:03Z",
        deadlineAt: "2026-07-22T10:00:13Z",
        snapshot: {
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
    expect(api.createStreamProbe).toHaveBeenCalledWith("stream-1", 3000);
    expect(api.getStreamProbeOperation).toHaveBeenCalledWith("probe-op-1");
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

  it("探针完成后侧栏展示全量帧到达概览，点击打开逐帧详情弹窗", async () => {
    const ModalStub = defineComponent({
      name: "ProbeTimelineModalStub",
      inheritAttrs: false,
      props: { visible: Boolean },
      template: `<div v-if="visible"><slot name="title" /><slot /></div>`
    });
    const timeline = [
      { sequence: 0, trackType: "video", codec: "H264", keyFrame: true, configFrame: false, relativeTimeMs: 0, frameSize: 20480 },
      {
        sequence: 1,
        trackType: "video",
        codec: "H264",
        keyFrame: false,
        configFrame: false,
        relativeTimeMs: 40,
        frameSize: 3072
      },
      { sequence: 2, trackType: "audio", codec: "PCMA", keyFrame: false, configFrame: false, relativeTimeMs: 60, frameSize: 200 },
      // 40ms 与 1200ms 之间断了 1160ms,超过后端阈值 500ms,概览条必须能标出来。
      {
        sequence: 3,
        trackType: "video",
        codec: "H264",
        keyFrame: true,
        configFrame: false,
        relativeTimeMs: 1200,
        frameSize: 21504
      },
      {
        sequence: 4,
        trackType: "audio",
        codec: "PCMA",
        keyFrame: false,
        configFrame: false,
        relativeTimeMs: 1220,
        frameSize: 200
      }
    ];
    api.createStreamProbe.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { operationId: "probe-op-2", streamId: "stream-1", durationMs: 3000, status: "queued", createdAt: "", deadlineAt: "" }
    });
    api.getStreamProbeOperation.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        operationId: "probe-op-2",
        streamId: "stream-1",
        durationMs: 3000,
        status: "completed",
        createdAt: "",
        completedAt: "",
        deadlineAt: "",
        snapshot: {
          nodeId: 1,
          nodeName: "ZLM",
          completedAt: "",
          summary: { sampleDurationMs: 3000, frameCount: 246, totalBytes: 786432, averageBitrateKbps: 2097.1 },
          video: null,
          audio: null,
          timestamps: { videoDtsIntervalMeanMs: null, arrivalJitterMs: null, ptsDtsMaxMs: null, avArrivalSkewMaxMs: null },
          timeline,
          health: { status: "warning", issues: [], thresholds: { largeArrivalGapMs: 500, keyFrameWindowMs: 3000 } }
        }
      }
    });

    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel },
      global: { stubs: { "a-modal": ModalStub } }
    });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    await wrapper.get("[data-testid='probe-start']").trigger("click");
    await flushPromises();

    const overview = wrapper.get("[data-testid='probe-timeline-open']");
    // 概览按时间分桶,柱子数固定,不随采样帧数增长(旧版逐帧一根柱子会溢出侧栏)。
    expect(overview.findAll(".frame-overview-bar").length).toBeGreaterThan(0);
    expect(overview.text()).toContain("246 帧");
    // 断档区间要在概览条上标出来,而不是只报一个总数。
    expect(overview.findAll(".frame-overview-bar.stalled").length).toBeGreaterThan(0);
    expect(wrapper.get("[data-testid='linked-detail-probe']").text()).toContain("1 处断档");

    // 详情弹窗按需打开,而不是一直挂在 DOM 里。
    expect(wrapper.find("[data-testid='probe-timeline-dialog']").exists()).toBe(false);
    await overview.trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='probe-timeline-dialog']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='probe-timeline-summary']").text()).toContain("1 处到达断档");

    wrapper.unmount();
  });

  it("点播请求尚未返回时关闭弹窗也不补发停播请求", async () => {
    let resolveStart!: (value: any) => void;
    api.startPlay.mockReturnValueOnce(
      new Promise(resolve => {
        resolveStart = resolve;
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.setProps({ visible: false });
    resolveStart({
      code: 0,
      message: "",
      data: {
        streamId: "stream-late",
        ssrc: "late",
        app: "rtp",
        wsflvUrl: "ws://zlm/late.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
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
          {
            kind: "audio",
            codec: "PCMA",
            ready: true,
            frames: 3453,
            duration: 69020,
            loss: 0,
            width: 0,
            height: 0,
            fps: 0,
            keyFrames: 0,
            gopSize: 0,
            gopIntervalMs: 0,
            sampleRate: 8000,
            channels: 1,
            sampleBit: 16
          },
          {
            kind: "video",
            codec: "H264",
            ready: true,
            frames: 2070,
            duration: 69033,
            loss: 0,
            width: 1280,
            height: 720,
            fps: 30,
            keyFrames: 84,
            gopSize: 25,
            gopIntervalMs: 846,
            sampleRate: 0,
            channels: 0,
            sampleBit: 0
          }
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

  it("「视频参数」tab 直接嵌入设备配置工作区，并随通道切换上下文", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel }
    });

    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");
    const workspace = wrapper.get("[data-testid='linked-side-videoparam'] .dcg-window--embedded");
    expect(workspace.attributes("aria-label")).toContain(channel.name);
    expect(workspace.text()).toContain("视频参数属性");
    expect(workspace.find("[data-testid='dcg-standard-badge']").text()).toContain("GB/T 28181-2022");
    expect(wrapper.find("[data-testid='play-console-open-device-config']").exists()).toBe(false);

    await wrapper.setProps({ channel: { ...channel, id: 999, channelId: "34020000001320000099", name: "园区西门" } });
    await flushPromises();
    expect(wrapper.get("[data-testid='linked-side-videoparam'] .dcg-window--embedded").attributes("aria-label")).toContain(
      "园区西门"
    );
  });

  it("侧栏与详情条按 tab 分工，云台/探针/高级/视频参数各司其职", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel }
    });

    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    expect(wrapper.find(".hud").exists()).toBe(false);

    const ptzSide = wrapper.get("[data-testid='linked-side-ptz']");
    const ptzDetail = wrapper.get("[data-testid='linked-detail-ptz']");
    expect(ptzSide.text()).toContain("开始广播");
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
    expect(advancedSide.text()).toContain("图像抓拍配置");
    expect(advancedSide.text()).not.toContain("请求关键帧");
    expect(advancedSide.text()).not.toContain("亮度");
    expect(advancedDetail.text()).toContain("媒体控制");
    expect(advancedDetail.text()).toContain("安防控制");
    expect(advancedDetail.text()).toContain("画面控制");
    expect(advancedDetail.findAll(".linked-advanced-layout > .linked-card")).toHaveLength(3);
    expect(advancedDetail.text()).not.toContain("亮度");
    expect(advancedDetail.text()).not.toContain("接口待接入");
    expect(advancedDetail.text()).toContain("标准控制字段");
    // ⛔ 视频参数已从"高级"拆成独立 tab（2026-09-18）。别让它被顺手加回"高级" ——
    //    那样"高级"又变回四张卡的杂货铺，而且这块配置会失去独立入口。
    expect(advancedSide.text()).not.toContain("视频参数属性");

    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");
    const vpSide = wrapper.get("[data-testid='linked-side-videoparam']");
    const vpDetail = wrapper.get("[data-testid='linked-detail-videoparam']");
    // 侧栏留编辑表单与状态文案；三行对照下移到详情条（见"对照搬到详情条"用例）。
    expect(vpSide.text()).toContain("视频参数属性");
    expect(vpSide.text()).not.toContain("设备控制");
    expect(vpSide.text()).not.toContain("图像抓拍配置");
    // ⛔ 对照区必须**只在**详情条：留在编辑表单旁边会被误读成"我刚改的值"。
    expect(vpSide.find("[data-testid='video-param-compare']").exists()).toBe(false);
    expect(vpDetail.text()).toContain("参数对照");
    expect(vpDetail.findAll(".linked-videoparam-layout > .linked-section")).toHaveLength(1);

    wrapper.unmount();
  });

  it("视频参数：三行对照渲染在播放器下方详情条，侧栏只留编辑表单", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({
        list: [videoParamRow({ id: 1, streamNumber: 0, resolution: "6" })],
        freshness: "fresh",
        reconcile: { state: "read_ok", operationId: "vp-op-0", status: "accepted", responseHasData: true }
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");

    const detail = wrapper.get("[data-testid='linked-detail-videoparam']");
    const compare = detail.get("[data-testid='video-param-compare']");
    // 三行都在详情条里 —— 这是"改在哪、验在哪同屏"的落点。
    expect(compare.text()).toContain("下发");
    expect(compare.text()).toContain("回读");
    expect(compare.text()).toContain("实测");
    // ⛔ 「回读」行取**设备事实**（码值 6 → 1080P），不是草稿值。
    expect(detail.get("[data-testid='video-param-compare-read']").text()).toContain("1080P");

    // 侧栏留表单、不留对照；对照只在详情条。
    const side = wrapper.get("[data-testid='linked-side-videoparam']");
    expect(side.find("[data-testid='video-param-compare']").exists()).toBe(false);
    expect(side.find("[data-testid='dcg-stream-0']").exists()).toBe(true);
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

  it("采样中仅保留右上角状态，不显示重复的采集提示卡", async () => {
    api.createStreamProbe.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        operationId: "probe-op-pending",
        streamId: "stream-1",
        durationMs: 3000,
        status: "queued",
        createdAt: "2026-07-22T10:00:00Z",
        deadlineAt: "2026-07-22T10:00:13Z"
      }
    });
    api.getStreamProbeOperation.mockReturnValueOnce(new Promise(() => undefined));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    await wrapper.get("[data-testid='probe-start']").trigger("click");
    await nextTick();

    expect(wrapper.get("[data-testid='probe-check'] .probe-status").text()).toContain("采样中");
    expect(wrapper.find("[data-testid='probe-check'] .probe-verdict.pending").exists()).toBe(false);
    expect(wrapper.get("[data-testid='probe-check']").text()).not.toContain("正在采集音视频帧");

    wrapper.unmount();
  });

  it("支持选择采样时长并将选项传给探针接口", async () => {
    api.createStreamProbe.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        operationId: "probe-op-duration",
        streamId: "stream-1",
        durationMs: 10000,
        status: "queued",
        createdAt: "2026-07-22T10:00:00Z",
        deadlineAt: "2026-07-22T10:00:20Z"
      }
    });
    api.getStreamProbeOperation.mockReturnValueOnce(new Promise(() => undefined));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    const duration = wrapper.findComponent("[data-testid='probe-duration']") as unknown as VueWrapper;
    expect(duration.findAll("option").map(option => option.text())).toEqual(["3 秒", "10 秒", "60 秒"]);
    expect(duration.attributes("modelvalue")).toBe("3000");

    duration.vm.$emit("update:modelValue", 10000);
    await nextTick();
    expect(wrapper.get("[data-testid='probe-start']").text()).toContain("开始 10 秒检测");
    await wrapper.get("[data-testid='probe-start']").trigger("click");
    expect(api.createStreamProbe).toHaveBeenCalledWith("stream-1", 10000);
    expect(wrapper.get("[data-testid='probe-start']").attributes("disabled")).toBeDefined();
    expect(duration.attributes("disabled")).toBeDefined();

    wrapper.unmount();
  });

  it("左侧导航独立占列，详情跨视频与右侧面板", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");

    expect(source).toMatch(/\.linked-info-bar\s*\{[^}]*grid-column:\s*2\s*\/\s*-1/s);
    expect(source).toContain("--linked-detail-height: 148px");
    expect(source).toContain(':width="playbackModalWidth"');
    expect(source).toContain(': "min(1520px, calc(100vw - 32px))"');
    expect(source).toMatch(/\.console-body\s*\{[^}]*grid-template-columns:\s*136px\s+minmax\(0,\s*1fr\)\s+360px/s);
    // 流信息 tab 已并入探针 tab,原来的 sidebar-stream / linked-detail-stream / linked-stream-metrics
    // 全都退出历史舞台
    expect(source).not.toContain("sidebar-stream");
    expect(source).not.toContain('data-testid="linked-detail-stream"');
    expect(source).not.toContain(".linked-stream-metrics");
    expect(source).not.toContain("phase === 'playing' && activeTab !== 'stream'");
    expect(source).toMatch(/\.linked-detail\s*\{[^}]*height:\s*var\(--linked-detail-height\)/s);
    // 所有页签共用同一条底部工作区基线；参数对照/设备配置不能再单独把区域撑到 220px。
    expect(source).not.toMatch(/\.linked-detail-actions\s*\{[^}]*height:\s*220px/s);
    expect(source).toMatch(/\.linked-detail-actions\s*\{[^}]*height:\s*var\(--linked-detail-height\)/s);
    expect(source).toMatch(/\.linked-card\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(source).toMatch(/\.preset-tile-more\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(source).toMatch(
      /@media \(max-width: 720px\)[\s\S]*?\.linked-detail\s*>\s*\.linked-ptz-layout\s*\{[^}]*flex:\s*0 0 auto;[^}]*grid-template-rows:\s*none;[^}]*height:\s*auto/s
    );
    expect(source).toMatch(/\.home-card-actions\s*\{[^}]*display:\s*flex/s);
    expect(source).toMatch(/\.home-settings-form\s*\{[^}]*display:\s*grid/s);
    // 探针详情条三栏不等分:时间线是横向柱状图,等分会把 32 根柱子挤到每根不足 9px
    expect(source).toMatch(
      /\.linked-probe-layout\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+minmax\(0,\s*1fr\)\s+minmax\(0,\s*1\.4fr\)/s
    );
    expect(source).toMatch(/\.sidebar-advanced\s+\.panels\s*\{[^}]*background:\s*transparent/s);
    expect(source).toMatch(/\.linked-advanced-layout\s*\{[^}]*grid-template-columns:\s*repeat\(3,\s*minmax\(0,\s*1fr\)\)/s);
    expect(source).toMatch(
      /\.linked-advanced-layout\s+\.adv-btn\s*\{[^}]*min-height:\s*40px[^}]*background:\s*var\(--uvp-panel-bg\)[^}]*border-color:\s*var\(--uvp-panel-border\)/s
    );
    expect(source).not.toMatch(
      /\.linked-advanced-layout\s+\.adv-btn\s*\{[^}]*background:\s*transparent[^}]*border-color:\s*transparent/s
    );
  });

  it("检测按钮与时长选择器按 7:3 分配宽度", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");

    expect(source).toMatch(/\.probe-action\s*\{[^}]*flex:\s*7 1 0/s);
    // flex item 默认 min-width:auto 会被内容顶住,两侧都必须显式清零
    expect(source).toMatch(/\.probe-action\s*\{[^}]*min-width:\s*0/s);

    // a-select 的根节点由 Arco 内部渲染,拿不到本组件的 scoped 属性(实测 hasScopeAttr=false)。
    // 裸类名选择器编译得过却永远匹配不到它,下拉会退回 Arco 自带的 width:100%,
    // 把按钮挤成竖排窄条 —— 所以必须写成 :deep() 后代选择器。
    expect(source).toMatch(/:deep\(\.probe-duration\)\s*\{[^}]*flex:\s*3 1 0/s);
    expect(source).toMatch(/:deep\(\.probe-duration\)\s*\{[^}]*min-width:\s*0/s);
    expect(source).not.toMatch(/^\s*\.probe-duration\s*[\{,:]/m);
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

  it("镜头按钮按住下发 FI 指令，松手用 FI 族的停止码收尾", async () => {
    // GB/T 28181 表 A.6:FI 族(聚焦/光圈)跟方向族一样是"带速度的开始动作",但**停止码不同**
    // —— 方向族停 0x00、FI 族停 0x40。松手时对 FI 发 0x00 设备不会停,镜头会一直走下去。
    // 之前这两个按钮是单击式且不补停止,等于把"开始动作"当"走一步"发。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const irisOpen = wrapper.get("[title='开大(按住连续)']");
    await irisOpen.trigger("pointerdown");
    expect(api.controlPtz).toHaveBeenNthCalledWith(1, channel.id, expect.objectContaining({ action: "iris_open" }));
    await irisOpen.trigger("pointerup");
    expect(api.controlPtz).toHaveBeenNthCalledWith(2, channel.id, expect.objectContaining({ action: "lens_stop" }));

    const focusFar = wrapper.get("[title='远焦(按住连续)']");
    await focusFar.trigger("pointerdown");
    expect(api.controlPtz).toHaveBeenNthCalledWith(3, channel.id, expect.objectContaining({ action: "focus_far" }));
    await focusFar.trigger("pointerup");
    expect(api.controlPtz).toHaveBeenNthCalledWith(4, channel.id, expect.objectContaining({ action: "lens_stop" }));
    wrapper.unmount();
  });

  it("变倍仍然用方向族的停止码", async () => {
    // 反向守卫:变倍属于方向族(0x00),不能被上面那条改动一并带成 lens_stop。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const zoomIn = wrapper.get("[title='放大']");
    await zoomIn.trigger("pointerdown");
    await zoomIn.trigger("pointerup");

    expect(api.controlPtz).toHaveBeenNthCalledWith(2, channel.id, expect.objectContaining({ action: "stop" }));
    expect(api.controlPtz).not.toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "lens_stop" }));
    wrapper.unmount();
  });

  it("不保留自动聚焦与自动光圈开关", async () => {
    // 自动聚焦/自动光圈不是 GB/T 28181 的能力(表 A.6 只有四个镜头动作 + 本族停止),
    // 是厂商私有概念。留着就是一个点了不发任何标准指令的假开关。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find("[title='自动聚焦']").exists()).toBe(false);
    expect(wrapper.find("[title='自动光圈']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("麦克风授权期间再次点击取消不会创建后端会话", async () => {
    let resolveMedia!: (value: any) => void;
    const stop = vi.fn();
    const track = { enabled: true, stop };
    const getUserMedia = vi.fn().mockReturnValueOnce(
      new Promise(resolve => {
        resolveMedia = resolve;
      })
    );
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const talkButton = wrapper.get("[data-testid='talk-button']");
    await talkButton.trigger("click");
    await flushPromises();
    expect(getUserMedia).toHaveBeenCalledTimes(1);
    expect(api.createTalkSession).not.toHaveBeenCalled();
    expect(talkButton.text()).toContain("正在申请麦克风");

    // ⛔ 过渡态里的第二次点击是**取消**，不是空转：对讲已改成点击开关，
    //    授权弹窗久等不来时这是操作员唯一的退出口。
    await talkButton.trigger("click");
    await flushPromises();
    expect(talkButton.text()).toContain("开始广播");
    resolveMedia({ getTracks: () => [track], getAudioTracks: () => [track] });
    await flushPromises();

    expect(api.createTalkSession).not.toHaveBeenCalled();
    expect(stop).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("创建响应迟到时关闭弹窗仍会删除后端会话", async () => {
    let resolveCreate!: (value: any) => void;
    api.createTalkSession.mockReturnValueOnce(
      new Promise(resolve => {
        resolveCreate = resolve;
      })
    );
    const stop = vi.fn();
    const track = { enabled: true, stop };
    vi.stubGlobal("navigator", {
      ...navigator,
      mediaDevices: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [track], getAudioTracks: () => [track] }) }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("click"));
    await flushPromises();
    await wrapper.setProps({ visible: false });
    resolveCreate({
      code: 0,
      message: "",
      data: {
        sessionId: "talk-after-close",
        mode: "broadcast",
        state: "reserved",
        expiresAt: "",
        uplink: {
          protocol: "whip",
          url: "/api/gb28181/device-mgmt/talk-sessions/talk-after-close/uplink",
          contentType: "application/sdp"
        }
      }
    });
    await flushPromises();

    expect(api.deleteTalkSession).toHaveBeenCalledWith(channel.id, "talk-after-close");
    wrapper.unmount();
  });

  it("Broadcast 的麦克风在后端 active 前保持静音并锁定 PCMA", async () => {
    let resolveStatus!: (value: any) => void;
    api.getTalkSession.mockReturnValueOnce(
      new Promise(resolve => {
        resolveStatus = resolve;
      })
    );
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
      setLocalDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
        this.localDescription = description;
      });
      setRemoteDescription = vi.fn().mockResolvedValue(undefined);
      addEventListener = vi.fn();
      removeEventListener = vi.fn();
      close = vi.fn();
    }
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) } });
    vi.stubGlobal("RTCRtpSender", {
      getCapabilities: () => ({ codecs: [{ mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }] })
    });
    vi.stubGlobal("RTCPeerConnection", FakePeerConnection);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 201, text: async () => offerSdp }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("click"));
    await flushPromises();

    expect(api.createTalkSession).toHaveBeenCalledWith(channel.id, "broadcast");
    expect(track.enabled).toBe(false);
    expect(setCodecPreferences).toHaveBeenCalledWith([{ mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }]);
    expect(wrapper.get("[data-testid='talk-button']").text()).toContain("正在建立广播");

    resolveStatus({ code: 0, message: "", data: { sessionId: "talk-1", mode: "broadcast", state: "active", expiresAt: "" } });
    await flushPromises();
    expect(track.enabled).toBe(true);
    // 说话中按钮显示的是**动作**（再点一下就停），不是长按时代的「松开结束」
    expect(wrapper.get("[data-testid='talk-button']").text()).toContain("停止广播");

    await wrapper.get("[data-testid='talk-button']").trigger("click");
    await flushPromises();
    expect(track.enabled).toBe(false);
    expect(stop).toHaveBeenCalledTimes(1);
    expect(api.deleteTalkSession).toHaveBeenCalledWith(channel.id, "talk-1");
    wrapper.unmount();
  });

  it("上行发给平台自己，并按平台下发的 ICE 服务器建连接", async () => {
    // 两个契约点：
    // ① offer 发给平台的上行入口，媒体节点地址与自签证书不进浏览器；
    // ② ICE 服务器用平台下发的，缺了它只产 host candidate，跨网段必连不上。
    let resolveStatus!: (value: any) => void;
    api.getTalkSession.mockReturnValueOnce(
      new Promise(resolve => {
        resolveStatus = resolve;
      })
    );
    const stop = vi.fn();
    const track = { enabled: true, stop };
    const stream = { getTracks: () => [track], getAudioTracks: () => [track] };
    const offerSdp = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 8\r\na=rtpmap:8 PCMA/8000\r\n";
    const peerConfigs: any[] = [];
    class FakePeerConnection {
      iceGatheringState = "complete";
      localDescription: RTCSessionDescriptionInit | null = null;
      constructor(config?: any) {
        peerConfigs.push(config);
      }
      addTransceiver = vi.fn(() => ({ setCodecPreferences: vi.fn() }));
      createOffer = vi.fn().mockResolvedValue({ type: "offer", sdp: offerSdp });
      setLocalDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
        this.localDescription = description;
      });
      setRemoteDescription = vi.fn().mockResolvedValue(undefined);
      addEventListener = vi.fn();
      removeEventListener = vi.fn();
      close = vi.fn();
    }
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 201, text: async () => offerSdp });
    Cookies.set(AccessTokenKey, JSON.stringify({ accessToken: "tok-1", accessTokenExpires: Date.now() + 60000 }));
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) } });
    vi.stubGlobal("RTCRtpSender", {
      getCapabilities: () => ({ codecs: [{ mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }] })
    });
    vi.stubGlobal("RTCPeerConnection", FakePeerConnection);
    vi.stubGlobal("fetch", fetchMock);
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("click"));
    await flushPromises();

    expect(peerConfigs[0]?.iceServers).toEqual([{ urls: ["stun:media.example.test:3478"] }]);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/gb28181/device-mgmt/talk-sessions/talk-1/uplink");
    expect(url).not.toContain("zlm");
    expect(init.method).toBe("POST");
    expect(init.headers["Content-Type"]).toBe("application/sdp");
    expect(init.headers.Authorization).toBe("Bearer tok-1");

    resolveStatus({ code: 0, message: "", data: { sessionId: "talk-1", mode: "broadcast", state: "active", expiresAt: "" } });
    await flushPromises();
    Cookies.remove(AccessTokenKey);
    wrapper.unmount();
  });

  it("说话中显示采集波形，停止后收起", async () => {
    // 波形是「麦克风确实在采」的可见证据。jsdom 里没有 AudioContext，电平恒为 0，
    // 但波形仍须出现（降级成静态起伏）—— 不能因为拿不到电平就把这个反馈整个吞掉。
    let resolveStatus!: (value: any) => void;
    api.getTalkSession.mockReturnValueOnce(
      new Promise(resolve => {
        resolveStatus = resolve;
      })
    );
    const stop = vi.fn();
    const track = { enabled: true, stop };
    const stream = { getTracks: () => [track], getAudioTracks: () => [track] };
    const offerSdp = "v=0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 8\r\na=rtpmap:8 PCMA/8000\r\n";
    class FakePeerConnection {
      iceGatheringState = "complete";
      localDescription: RTCSessionDescriptionInit | null = null;
      addTransceiver = vi.fn(() => ({ setCodecPreferences: vi.fn() }));
      createOffer = vi.fn().mockResolvedValue({ type: "offer", sdp: offerSdp });
      setLocalDescription = vi.fn(async (description: RTCSessionDescriptionInit) => {
        this.localDescription = description;
      });
      setRemoteDescription = vi.fn().mockResolvedValue(undefined);
      addEventListener = vi.fn();
      removeEventListener = vi.fn();
      close = vi.fn();
    }
    vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn().mockResolvedValue(stream) } });
    vi.stubGlobal("RTCRtpSender", {
      getCapabilities: () => ({ codecs: [{ mimeType: "audio/PCMA", clockRate: 8000, channels: 1 }] })
    });
    vi.stubGlobal("RTCPeerConnection", FakePeerConnection);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, status: 201, text: async () => offerSdp }));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find("[data-testid='talk-wave']").exists()).toBe(false);
    wrapper.get("[data-testid='talk-button']").element.dispatchEvent(new Event("click"));
    await flushPromises();
    // 授权 / 建会话 / 等信令这些过渡态都还没在说话，不该有波形
    expect(wrapper.find("[data-testid='talk-wave']").exists()).toBe(false);

    resolveStatus({
      code: 0,
      message: "",
      data: { sessionId: "talk-wave-1", mode: "broadcast", state: "active", expiresAt: "" }
    });
    await flushPromises();
    const wave = wrapper.get("[data-testid='talk-wave']");
    expect(wave.findAll("i")).toHaveLength(4);
    expect(wave.attributes("aria-hidden")).toBe("true");

    await wrapper.get("[data-testid='talk-button']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='talk-wave']").exists()).toBe(false);
    expect(stop).toHaveBeenCalledTimes(1);
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
      .mockReturnValueOnce(
        new Promise(resolve => {
          resolveOld = resolve;
        })
      )
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          streamId: "stream-2",
          ssrc: "2",
          app: "rtp",
          wsflvUrl: "ws://zlm/stream-2.flv",
          httpFlvUrl: "",
          hlsUrl: "",
          expireAt: 0
        }
      });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.setProps({ channel: { ...channel, id: 2, channelId: "0411212756", name: "园区南门" } });
    await flushPromises();
    expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe("ws://zlm/stream-2.flv");

    resolveOld({
      code: 0,
      message: "",
      data: {
        streamId: "stream-old",
        ssrc: "1",
        app: "rtp",
        wsflvUrl: "ws://zlm/stream-old.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    await flushPromises();
    expect(api.stopPlay).toHaveBeenCalledWith("stream-old");
    expect(wrapper.get("[data-testid='play-window']").attributes("data-url")).toBe("ws://zlm/stream-2.flv");
    wrapper.unmount();
  });

  it("设备与通道编码变化时即使数据库 ID 相同也丢弃迟到的 DeviceStatus", async () => {
    let resolveOldStatus!: (value: any) => void;
    api.getDeviceStatus
      .mockReturnValueOnce(
        new Promise(resolve => {
          resolveOldStatus = resolve;
        })
      )
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

  // 设备在 DeviceStatus 应答里一直报着 Online / Status / Encode / DeviceTime，
  // 以前解析器把它们全丢了，界面自然也无从显示。
  it("渲染设备自报的在线、自检、编码与时间偏差", async () => {
    api.getDeviceStatus.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        state: { recordState: "on", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        deviceReport: {
          online: "online",
          selfTest: "ok",
          encode: "on",
          deviceTime: "2026-09-19T20:03:58",
          clockSkewSeconds: 1,
          alarmInputCount: 0,
          observedAt: "2026-09-19T20:03:59Z"
        }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);

    const facts = wrapper.get("[data-testid='advanced-fact-status']").text();
    expect(facts).toContain("在线");
    expect(facts).toContain("自检正常");
    expect(facts).toContain("编码中");
    expect(facts).toContain("与平台一致");
    wrapper.unmount();
  });

  // 设备回了 Alarmstatus Num="0"，它说的是"我没有报警输入" —— 这是已知事实，
  // 不是"未知"。下方那条提示也要跟着改口径。
  it("设备明确回了 0 个报警输入时显示「设备无报警输入」而不是「未知」", async () => {
    api.getDeviceStatus.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        state: { recordState: "on", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        alarmResolution: {
          status: "unavailable",
          source: "",
          targetCode: "",
          state: "unknown",
          freshness: "unknown",
          candidates: []
        },
        deviceReport: { alarmInputCount: 0 }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);

    const facts = wrapper.get("[data-testid='advanced-fact-status']").text();
    expect(facts).toContain("设备无报警输入");
    expect(wrapper.get("[data-testid='alarm-resolution-warning']").text()).toContain("设备自报没有报警输入通道");
    wrapper.unmount();
  });

  // 对照：设备这次没报的项要显示「未上报」，既不能兜底成"关闭"，也不能沿用上一台的值。
  it("设备没上报那些事实时显示「未上报」而不是「已停」", async () => {
    api.getDeviceStatus.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        state: { recordState: "on", guardState: "unknown", freshness: "fresh" },
        freshness: "fresh",
        deviceReport: {
          online: null,
          selfTest: null,
          encode: null,
          deviceTime: null,
          clockSkewSeconds: null,
          alarmInputCount: null
        }
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await requestDeviceStatus(wrapper);

    const facts = wrapper.get("[data-testid='advanced-fact-status']").text();
    expect(facts).toContain("未上报");
    expect(facts).not.toContain("编码已停");
    expect(facts).not.toContain("自检异常");
    expect(facts).not.toContain("设备无报警输入");
    wrapper.unmount();
  });

  it("渲染存储卡状态并可发起查询", async () => {
    api.getChannelStorageCards.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        list: [
          {
            id: 1,
            deviceId: 1,
            targetCode: "0411212755",
            cardId: 1,
            hddName: "SD Card 1",
            status: "ok",
            formatProgress: null,
            capacityMb: 32768,
            freeSpaceMb: 24576,
            observedAt: "2026-09-17T10:00:00Z"
          }
        ],
        freshness: "fresh",
        refreshOperationId: null,
        refreshError: null
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    const card = wrapper.get("[data-testid='storage-card-status']");
    expect(card.text()).toContain("存储卡状态");
    expect(card.text()).toContain("SD Card 1");
    expect(card.text()).toContain("正常");
    // 容量单位在展示层换算：32768 MB → 32.0 GB，24576 MB → 24.0 GB。
    expect(card.text()).toContain("24.0 GB 可用 / 32.0 GB");

    api.getChannelStorageCards.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { list: [], freshness: "unknown", refreshOperationId: "sc-op-1", refreshError: null }
    });
    await wrapper.get("[data-testid='storage-card-refresh']").trigger("click");
    await flushPromises();
    // refresh=true 只是"发起查询"，真正的应答要靠轮询 operation。
    expect(api.getChannelStorageCards).toHaveBeenLastCalledWith(channel.id, true);
    wrapper.unmount();
  });

  it("设备无存储卡时展示空态而不是错误", async () => {
    // 空列表是合法结果（标准 SumNum=0 且不带 SDCardStatusInfo），
    // 不能和"查询失败"用同一套措辞 —— 否则现场会把正常设备当成故障。
    api.getChannelStorageCards.mockResolvedValue({
      code: 0,
      message: "",
      data: { list: [], freshness: "fresh", refreshOperationId: null, refreshError: null }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");

    const card = wrapper.get("[data-testid='storage-card-status']");
    expect(card.text()).toContain("设备未安装存储卡");
    expect(wrapper.find("[data-testid='storage-card-error']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("嵌入式视频参数工作区渲染回读事实与目录码流声明", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({
        list: [
          videoParamRow({ id: 1, streamNumber: 0 }),
          videoParamRow({ id: 2, streamNumber: 1, resolution: "5", bitRateType: "2", videoBitRate: null })
        ],
        freshness: "fresh",
        streamNumberList: "0/1",
        reconcile: { state: "read_ok", operationId: "vp-op-0", status: "accepted", responseHasData: true }
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");

    // 打开面板只读平台缓存，**不发 SIP 报文**（refresh=false）。
    expect(api.getChannelVideoParams).toHaveBeenCalledWith(channel.id, false);
    expect(wrapper.get("[data-testid='dcg-reconcile']").text()).toContain("回读成功");

    // 「按几段码流渲染」的出处是目录 <Info> 的 StreamNumberList，不是"我们看到几行"。
    expect(wrapper.get(".dcg-statusbar").text()).toContain("码流声明 0 / 1");

    // ⛔ 控件绑定标准码值，人读串只存在于 option 文案。
    expect((wrapper.get("[data-testid='dcg-format-0']").element as HTMLSelectElement).value).toBe("2");
    expect((wrapper.get("[data-testid='dcg-resolution-0']").element as HTMLSelectElement).value).toBe("6");

    // 配置文件切到子码流后，VBR 码率明确标为“不发”。
    await wrapper.get("[aria-label='配置文件']").setValue("1");
    await nextTick();
    expect(wrapper.get("[data-testid='dcg-stream-1'] [data-source='不发']").text()).toBe("不发");

    // 对照区的「回读」行取设备事实：改草稿不该动它（下方 mismatch 用例另有锁定）。
    expect(wrapper.get("[data-testid='video-param-compare-read']").text()).toContain("1080P");
    wrapper.unmount();
  });

  it("视频参数面板切到 VBR 时禁用码率格", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({ list: [videoParamRow()], freshness: "fresh", reconcile: { state: "read_ok" } })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");

    const bitRate = wrapper.get("[data-testid='dcg-bit-rate-0'] input.cfg-slider-input").element as HTMLInputElement;
    expect(bitRate.disabled).toBe(false);

    // CBR → VBR：该元素在报文里根本不出现，所以这一格必须禁用，
    // 而不是"可以填但会被忽略"（填了忽略会让人以为填的值生效了）。
    await wrapper.get("[data-testid='dcg-bit-rate-type-0-2']").trigger("click");
    await nextTick();
    expect(bitRate.disabled).toBe(true);
    wrapper.unmount();
  });

  it("下发视频参数不把 200 当终态：轮询回读后展示设备结论", async () => {
    // 写入应答（A.2.6.8）没有任何回显 —— Result=OK 只说明"收到并接受"。
    // 所以下发之后必须等自动回读落地，界面结论只能来自 reconcile。
    vi.useFakeTimers();
    api.getChannelVideoParams
      .mockResolvedValueOnce(
        videoParamsResponse({ list: [videoParamRow()], freshness: "fresh", reconcile: { state: "read_ok" } })
      )
      .mockResolvedValueOnce(
        videoParamsResponse({ list: [videoParamRow()], freshness: "fresh", reconcile: { state: "read_ok" } })
      )
      .mockResolvedValue(
        videoParamsResponse({
          // 真实形态就是这样：设备从没给出过数据，所以一行都没落库。
          list: [],
          freshness: "fresh",
          reconcile: {
            state: "type_absent",
            operationId: "vp-op-1",
            status: "accepted",
            responseHasData: false,
            deviceError: "应答未携带 VideoParamAttribute 元素"
          }
        })
      );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");

    await wrapper.get("[data-testid='dcg-resolution-0']").setValue("5");
    await nextTick();
    expect(wrapper.get("[data-testid='dcg-stream-0']").text()).toContain("已改");

    await wrapper.get("[data-testid='dcg-apply']").trigger("click");
    await flushPromises();
    expect(api.applyChannelVideoParams).toHaveBeenCalledWith(
      channel.id,
      [
        expect.objectContaining({
          streamNumber: 0,
          videoFormat: "2",
          resolution: "5",
          frameRate: "25",
          bitRateType: "1",
          videoBitRate: "4096"
        })
      ],
      expect.stringContaining("device-config-")
    );

    // 下发后自动回读，把 reconcile 结论带回来。
    await vi.advanceTimersByTimeAsync(1200);
    await flushPromises();

    const reconcile = wrapper.get("[data-testid='dcg-reconcile']");
    expect(reconcile.text()).toContain("设备未返回该配置类型");
    // ⛔ type_absent 是"一种结论"而不是失败：设备回了 OK 却没带该元素
    // （2016 设备与未实现该类型的厂商都是这个形态）→ 黄色提示，不是红色报错。
    expect(reconcile.classes()).toContain("is-warn");
    expect(reconcile.text()).toContain("VideoParamAttribute");
    // 列表空着的时候也不能说成"尚未读取" —— 那会把能力问题说成操作问题。
    expect(wrapper.get("[data-testid='dcg-empty'] p").text()).toBe("设备未返回该配置类型的参数");
    wrapper.unmount();
  });

  it("生效版本 2016 只加版本提示，不动按钮也不改判据", async () => {
    // §十③：被误登记成 2016 的真 2022 设备，不试一次就永远用不了这功能。
    // 所以版本只影响措辞 —— 按钮照样能用，结论照样由回读给出。
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({
        registeredVersion: "2016",
        reconcile: {
          state: "type_absent",
          operationId: "vp-op-9",
          status: "accepted",
          responseHasData: false,
          deviceError: "应答未携带 VideoParamAttribute 元素"
        }
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");
    await flushPromises();

    expect(wrapper.get("[data-testid='dcg-version-notice']").text()).toContain("平台按 2016 版处理");
    // ⛔ "设备不支持"不是"用户不许试"的理由：读取按钮仍可用。
    expect((wrapper.get("[data-testid='dcg-read']").element as HTMLButtonElement).disabled).toBe(false);
    wrapper.unmount();
  });

  it("对账不一致按提示展示，且不因此禁用编辑与下发", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({
        list: [videoParamRow()],
        freshness: "fresh",
        reconcile: {
          state: "mismatch",
          operationId: "vp-op-2",
          status: "accepted",
          responseHasData: true,
          errorCode: "VIDEO_PARAM_RECONCILE_MISMATCH",
          errorMessage: '设备已接受命令，但回读值不一致 —— 码流 0 的 Resolution: 下发 "5" 回读 "6"'
        }
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");
    await flushPromises();

    const reconcile = wrapper.get("[data-testid='dcg-reconcile']");
    expect(reconcile.text()).toContain("设备已接受命令，但值未生效");
    // ⛔ mismatch 是能力边界（下发 1080P、设备只到 720P），设备没做错 → 黄不是红。
    expect(reconcile.classes()).toContain("is-warn");
    expect(reconcile.classes()).not.toContain("is-error");
    // 逐格差异必须露出来，否则用户只知道"没生效"、不知道差在哪一格。
    expect(reconcile.text()).toContain("Resolution");

    // ⛔ 设备给的结论不是"用户不许试"的理由：字段仍可改、改完仍可下发。
    const resolution = wrapper.get("[data-testid='dcg-resolution-0']").element as HTMLSelectElement;
    expect(resolution.disabled).toBe(false);
    await wrapper.get("[data-testid='dcg-resolution-0']").setValue("5");
    await nextTick();
    expect((wrapper.get("[data-testid='dcg-apply']").element as HTMLButtonElement).disabled).toBe(false);

    // 还原回设备事实，脏值计数归零。
    await wrapper.get("[data-testid='dcg-reset']").trigger("click");
    await nextTick();
    expect((wrapper.get("[data-testid='dcg-resolution-0']").element as HTMLSelectElement).value).toBe("6");
    expect(wrapper.get("[data-testid='dcg-reset']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("设备离线时视频参数面板禁用读写并说明原因", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({ list: [videoParamRow()], freshness: "fresh", reconcile: { state: "read_ok" } })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel: { ...channel, status: 0 } } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");

    expect((wrapper.get("[data-testid='dcg-read']").element as HTMLButtonElement).disabled).toBe(true);
    expect((wrapper.get("[data-testid='dcg-apply']").element as HTMLButtonElement).disabled).toBe(true);
    expect((wrapper.get("[data-testid='dcg-resolution-0']").element as HTMLSelectElement).disabled).toBe(true);
    wrapper.unmount();
  });

  it("切换通道后视频参数面板换成新通道的回读值，不残留上一台的草稿", async () => {
    api.getChannelVideoParams
      .mockResolvedValueOnce(
        videoParamsResponse({
          list: [videoParamRow()],
          freshness: "fresh",
          streamNumberList: "0/1",
          reconcile: { state: "read_ok" }
        })
      )
      .mockResolvedValue(
        videoParamsResponse({
          list: [videoParamRow({ id: 3, targetCode: "0411212888", resolution: "4", videoBitRate: "2048" })],
          freshness: "fresh",
          streamNumberList: "",
          reconcile: { state: "read_ok" }
        })
      );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");

    // 先制造一格"脏草稿"，再切通道 —— 新通道的值不能被旧草稿遮住。
    await wrapper.get("[data-testid='dcg-resolution-0']").setValue("5");
    await nextTick();
    expect(wrapper.get("[data-testid='dcg-stream-0']").text()).toContain("已改");

    const nextChannel = { ...channel, id: 2, channelId: "0411212888", deviceId: "34020000001320000003", name: "园区西门" };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();

    expect(api.getChannelVideoParams).toHaveBeenLastCalledWith(2, false);
    expect((wrapper.get("[data-testid='dcg-resolution-0']").element as HTMLSelectElement).value).toBe("4");
    expect(wrapper.get("[data-testid='dcg-reset']").attributes("disabled")).toBeDefined();
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
    api.getPtzOperation.mockReturnValueOnce(
      new Promise(resolve => {
        resolveOldOperation = resolve;
      })
    );

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
    expect(api.controlPtzCruise.mock.calls.flatMap(([, body]) => [body.action])).not.toEqual(
      expect.arrayContaining(["pause", "resume"])
    );
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
    api.controlDevice.mockReturnValueOnce(
      new Promise(resolve => {
        resolveControl = resolve;
      })
    );
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
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "record-op", status: "queued", responseRequired: true }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "iframe-op", status: "sent", responseRequired: false }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { operationId: "guard-op", status: "queued", responseRequired: true }
      });
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
    api.getPtzOperation.mockReturnValueOnce(
      new Promise(resolve => {
        resolveOldOperation = resolve;
      })
    );
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
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: 1000,
      bottom: 1000,
      width: 1000,
      height: 1000,
      toJSON: () => ({})
    } as DOMRect);
    vi.spyOn(wrapper.get("[data-testid='play-window']").element, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: 800,
      bottom: 450,
      width: 800,
      height: 450,
      toJSON: () => ({})
    } as DOMRect);
    await layer.trigger("pointerdown", { clientX: 200, clientY: 100, pointerId: 1, button: 0 });
    await layer.trigger("pointermove", { clientX: 600, clientY: 300, pointerId: 1 });
    await layer.trigger("pointerup", { clientX: 600, clientY: 300, pointerId: 1 });
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(
      channel.id,
      expect.objectContaining({
        action: "drag_zoom_in",
        region: { length: 800, width: 450, midPointX: 400, midPointY: 200, lengthX: 400, lengthY: 200 }
      })
    );
    wrapper.unmount();
  });

  it("3D 缩小同样先拖框并携带实际画面坐标", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-advanced']").trigger("click");
    await wrapper.get("[data-testid='advanced-drag-zoom-out']").trigger("click");
    const layer = wrapper.get("[data-testid='drag-zoom-layer']");
    vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: 800,
      bottom: 450,
      width: 800,
      height: 450,
      toJSON: () => ({})
    } as DOMRect);
    await layer.trigger("pointerdown", { clientX: 200, clientY: 100, pointerId: 2, button: 0 });
    await layer.trigger("pointermove", { clientX: 600, clientY: 300, pointerId: 2 });
    await layer.trigger("pointerup", { clientX: 600, clientY: 300, pointerId: 2 });
    await flushPromises();
    expect(api.controlDevice).toHaveBeenLastCalledWith(
      channel.id,
      expect.objectContaining({
        action: "drag_zoom_out",
        region: { length: 800, width: 450, midPointX: 400, midPointY: 200, lengthX: 400, lengthY: 200 }
      })
    );
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
      x: 0,
      y: 0,
      left: 0,
      top: 0,
      right: 800,
      bottom: 450,
      width: 800,
      height: 450,
      toJSON: () => ({})
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
    api.getPtzOperation.mockImplementation(
      () =>
        new Promise<ReturnType<typeof operationResponse>>(resolve => {
          resolveHungOperation = resolve;
        })
    );

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
          candidates: [
            { code: "A1", name: "门磁 1" },
            { code: "A2", name: "门磁 2" }
          ]
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

  it("预置位为空时看守位不可配置，并给出明确下一步但仍可查询", async () => {
    // 看守位的语义是「空闲 ResetTime 秒后回到 PresetIndex 指向的那个预置位」——
    // 设备上没有预置位就等于没有归位目标,下发一个谁都不存在的编号毫无意义。
    // 而平台里 0 号预置位**永远创建不出来**:创建接口强制 presetId>0
    // (controllers/device_ptz_resources.go),列预置位也 `.filter(id>0)`(loadPresets)。
    api.listPtzPresets.mockResolvedValueOnce({ code: 0, message: "", data: { list: [], freshness: "fresh" } });
    api.getHomePosition.mockResolvedValueOnce(
      homeResponse({
        homePosition: {
          enabled: false,
          resetTime: null,
          presetId: 0, // 设备未配置看守位时回的占位值,不是「0 号预置位」
          confirmedAt: "2026-07-22T10:00:00Z",
          source: "device_query",
          verification: "verified"
        }
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find("[data-testid='home-settings-dialog']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='home-preset-required']").text()).toContain("请先添加预置位");
    expect(wrapper.get("[data-testid='home-configure']").attributes("disabled")).toBeDefined();
    await wrapper.get("[data-testid='home-configure']").trigger("click");
    expect(api.updateHomePosition).not.toHaveBeenCalled();

    expect(wrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();
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
    // 默认 1 个巡航点(预置位 1)
    expect(dialog.findAll("[data-testid='cruise-stop-row']")).toHaveLength(1);
    expect(dialog.get("[data-testid='cruise-stop-add-btn']").text()).toContain("添加巡航点");
    expect(dialog.get("[data-testid='cruise-save-speed']").attributes("min")).toBe("1");
    expect(dialog.get("[data-testid='cruise-save-speed']").attributes("max")).toBe("4095");
    expect(dialog.get("[data-testid='cruise-save-dwell']").attributes("min")).toBe("1");
    expect(dialog.get("[data-testid='cruise-save-dwell']").attributes("max")).toBe("4095");
    // 速度没有统一物理单位,提示必须说出来,否则操作员会以为 128 是某种百分比
    expect(dialog.text()).toContain("没有统一物理单位");
    // 停留时间只有「秒」一种解释,提示要带单位
    expect(dialog.text()).toContain("单位是秒");
    // 速度/停留时间都是**组级**的(控制层 0x86/0x87 只带巡航组号,不带点编号),
    // 界面必须如实说,否则操作员会一直找"逐点设置"的入口
    expect(dialog.text()).toContain("不支持逐点设置");
    expect(dialog.text()).toContain("整条轨迹共用");
    expect(dialog.text()).not.toContain("0 表示");
    // 上界换算成人能感知的说法,免得操作员对着 4095 猜那是多久
    expect(dialog.text()).toContain("最长约 68 分钟");
    wrapper.unmount();
  });

  it("巡航点最多 32 个且列表内部滚动,添加入口保持醒目", async () => {
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
    expect(source).toMatch(
      /@media \(max-width:\s*560px\)\s*\{[^}]*\.cruise-save-form\s*\{[^}]*max-height:\s*calc\(100dvh - 210px\)/s
    );
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
    await new Promise<void>(resolve => {
      vm.handleSaveCruiseBeforeOk(ok => {
        expect(ok).toBe(true);
        resolve();
      });
    });
    await flushPromises();
    expect(api.createCruiseTrack).toHaveBeenCalledWith(
      channel.id,
      expect.objectContaining({
        trackId: 21,
        stops: [{ presetId: 1 }],
        speed: 128,
        dwellSec: 5,
        replaceExisting: false
      })
    );
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
    await new Promise<void>(resolve => {
      vm.handleSaveCruiseBeforeOk(ok => {
        expect(ok).toBe(true);
        resolve();
      });
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
          list: [
            {
              trackId: 1,
              name: "巡航 1",
              enabled: false,
              detail: JSON.stringify({ source: "reconcile-pending", stops: [{ presetId: 1 }] })
            }
          ],
          freshness: "stale"
        }
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          list: [
            {
              trackId: 1,
              name: "巡航 1",
              enabled: true,
              detail: JSON.stringify({ source: "device-query", stops: [{ presetId: 1 }] })
            }
          ],
          freshness: "fresh"
        }
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
    await new Promise<void>(resolve => {
      vm.handleSaveCruiseBeforeOk(ok => {
        expect(ok).toBe(true);
        resolve();
      });
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
    api.createCruiseTrack.mockReturnValueOnce(
      new Promise(resolve => {
        resolveCreate = resolve;
      })
    );
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
    await new Promise<void>(resolve => {
      vm.handleSaveCruiseBeforeOk(ok => {
        expect(ok).toBe(true);
        resolve();
      });
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
        list: [
          {
            trackId: 0,
            name: "已确认零号巡航",
            enabled: true,
            detail: JSON.stringify({ source: "reconcile-pending", stops: [{ presetId: 1 }] })
          }
        ],
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
    await new Promise<void>(resolve => {
      vm.handleSaveCruiseBeforeOk(ok => {
        expect(ok).toBe(true);
        resolve();
      });
    });
    expect(api.createCruiseTrack).toHaveBeenCalledWith(
      channel.id,
      expect.objectContaining({ trackId: 1, replaceExisting: true })
    );
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
    api.controlPtzCruise.mockReturnValueOnce(
      new Promise(resolve => {
        resolveCruise = resolve;
      })
    );
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
        list: [
          {
            trackId: 7,
            name: "标准巡航",
            enabled: false,
            detail: JSON.stringify({
              trackId: 7,
              sumNum: 1,
              source: "reconcile-pending",
              cruisePoints: [{ presetIndex: 3, stayTime: 5, speed: 8 }]
            })
          }
        ],
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
    expect(pendingTooltip.attributes("content")).toContain("配置已下发");
    // 悬浮提示要带点位链:卡片上的一格 tile 放不下第二行信息,悬浮提示是操作员
    // 唯一能核对「设备上这条轨迹走的是哪几个预置位、什么顺序」的地方。
    expect(pendingTooltip.attributes("content")).toContain("预置位 3");
    // 刻意不把标准附录号塞进用户文案 —— 操作员不读 A.2.6.13
    expect(pendingTooltip.attributes("content")).not.toContain("A.2.6");
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
    const assetText = wrapper.get("[data-testid='asset-manager']").text();
    expect(assetText).toContain("预置位 3");
    // 单位必须写出来:`0x86`/`0x87` 的参数是 12 位裸整数,单写一个 5 没人知道是秒还是档位
    expect(assetText).toContain("每点停留 5 秒");
    expect(assetText).toContain("速度 8");
    expect(assetText).toContain("未验证");
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

  // ⛔ 设备未上报 `enabled` 时(= null)轨迹必须**可点**。
  //
  // 后端曾经在创建巡航时就乐观写入 `enabled = false`("设备确认前不能标 enabled"),
  // 但 `<Enabled>` 在标准 A.2.6.13/A.2.6.14 的元素表里没有依据、真机与模拟器都不回,
  // 对账于是永远走"保留库里原值"分支 —— 库里那个"原值"恰恰就是它自己写的 false。
  // 结果:操作员建好的轨迹过一会儿自己变灰、点不动。这里把前端侧的契约钉死:
  // 只有**设备明确报 false** 才算停用,null / 缺字段都是"未上报",照样可点。
  it("设备未上报 enabled 时巡航轨迹不得显示为禁用", async () => {
    api.listCruiseTracks.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        list: [
          {
            trackId: 9,
            name: "车间巡检",
            enabled: null,
            detail: JSON.stringify({ trackId: 9, sumNum: 1, cruisePoints: [{ presetIndex: 2, stayTime: 5, speed: 128 }] })
          }
        ],
        freshness: "fresh"
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const tile = wrapper.get("[data-testid='cruise-tile-9']");
    expect(tile.classes()).not.toContain("disabled");
    expect(tile.get(".cruise-item").attributes("disabled")).toBeUndefined();
    // 悬浮提示要明说"点一下会发生什么" —— 灰掉时这里会写「设备报告这条轨迹已停用」
    expect(wrapper.get("[data-testid='cruise-tile-tooltip-9']").attributes("content")).toContain("点一下开始巡航");
    wrapper.unmount();
  });

  it("巡航详情按点位顺序显示预置位链,并接受 1-4095 的速度", async () => {
    // 两个回归点:
    //  1) **顺序**。巡航的语义就是「按顺序走一串预置位」,只报「N 个点位」等于把
    //     这条轨迹最可识别的信息丢了 —— 操作员没法据此判断设备上跑的是不是自己排的那条。
    //     顺序必须原样透传,不能按编号大小重排。
    //  2) 速度上限是 **4095 不是 15**。前端原来卡 1-15,设备如实回显 128 时会被判成
    //     非法值丢掉,界面上退化成「速度未上报」,看着像设备没回话。
    api.listCruiseTracks.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        list: [
          {
            trackId: 4,
            name: "车间巡检",
            enabled: true,
            detail: JSON.stringify({
              trackId: 4,
              sumNum: 3,
              cruisePoints: [
                { presetIndex: 3, stayTime: 30, speed: 128 },
                { presetIndex: 1, stayTime: 30, speed: 128 },
                { presetIndex: 5, stayTime: 30, speed: 128 }
              ]
            })
          }
        ],
        freshness: "fresh"
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const vm = wrapper.vm as unknown as { openAssetManager: (tab: "cruise") => void };
    vm.openAssetManager("cruise");
    await wrapper.vm.$nextTick();
    const text = wrapper.get("[data-testid='asset-manager']").text();
    expect(text).toContain("预置位 3→1→5");
    expect(text).toContain("每点停留 30 秒");
    expect(text).toContain("速度 128");
    expect(text).not.toContain("速度未上报");
    wrapper.unmount();
  });

  it("点位过多时截断预置位链,但交代真实数量", async () => {
    // 32 个点的全链会把整行占满,还把后面的「每点停留 / 速度」挤掉。
    // 截断可以,但必须补真实数量,否则看起来像这条轨迹只有 6 个点。
    const cruisePoints = Array.from({ length: 9 }, (_, index) => ({ presetIndex: index + 1, stayTime: 5, speed: 20 }));
    api.listCruiseTracks.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        list: [{ trackId: 2, name: "长链", enabled: true, detail: JSON.stringify({ trackId: 2, sumNum: 9, cruisePoints }) }],
        freshness: "fresh"
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const vm = wrapper.vm as unknown as { openAssetManager: (tab: "cruise") => void };
    vm.openAssetManager("cruise");
    await wrapper.vm.$nextTick();
    expect(wrapper.get("[data-testid='asset-manager']").text()).toContain("预置位 1→2→3→4→5→6 等 9 个");
    wrapper.unmount();
  });

  it("巡航速度只保留一套 1-4095 的取值域", () => {
    // 前端曾经在提示语里写死「查询端设备速度独立显示为 1-15」,与同一条链路的
    // 写侧(cruiseDraftError / a-input-number 的 :max,1-4095)和平台解析侧
    // (manscdp.ParseCruiseTrackResponse,1-4095)互相矛盾,并把合法回显丢掉。
    // 这条用例把口径钉住,防止哪天再被"补"回一个窄域。
    //
    // 断言**只切归一化函数体**:源码里到处是解释这段历史的注释,按整文件做
    // 否定断言会被自己的注释绊倒(2026-09-17 踩过)。
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const start = source.indexOf("const normalizeQuerySpeed");
    expect(start).toBeGreaterThan(-1);
    const normalizeBody = source.slice(start, source.indexOf("return { points, source", start));
    expect(normalizeBody).toContain("speed <= 4095");
    expect(normalizeBody).not.toContain("<= 15");
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
    it("卡片只展示设备状态，设置表单进入独立弹窗且不暴露协议占位 #0", async () => {
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: true,
            resetTime: 10,
            presetId: 0,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const card = wrapper.get("[data-testid='home-card']");
      expect(card.text()).toContain("已启用");
      expect(card.text()).toContain("归位位置未配置");
      expect(card.text()).not.toContain("#0");
      expect(card.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(card.find("[data-testid='home-fields']").exists()).toBe(false);
      expect(card.get("[data-testid='home-configure']").text()).toContain("修改设置");
      expect(card.get("[data-testid='home-close']").text()).toContain("关闭");

      await card.get("[data-testid='home-configure']").trigger("click");
      await nextTick();
      expect(wrapper.get("[data-testid='home-settings-dialog']").text()).toContain("连续无云台操作达到指定时间后");
      expect(wrapper.find("[data-testid='home-preset'] option[value='0']").exists()).toBe(false);
      wrapper.unmount();
    });

    it("能力尚未确认时只提供查询入口，不渲染误导性的开关和技术枚举", async () => {
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: null,
          freshness: "unknown",
          controlSupport: { status: "unknown", reason: "能力尚未确认" },
          querySupport: { status: "unknown", reason: "能力尚未确认" }
        })
      );
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
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: null,
          freshness: "unknown",
          controlSupport: { status: "unsupported", reason: "设备未上报控制能力" },
          querySupport: { status: "unsupported", reason: "设备未上报查询能力" }
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const card = wrapper.get("[data-testid='home-card']");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("设备不支持看守位");
      expect(wrapper.get("[data-testid='home-state-icon']").attributes("data-icon")).toBe("unsupported");
      expect(card.text()).not.toContain("unsupported");
      expect(card.text()).not.toContain("设备未上报控制能力");
      expect(wrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(wrapper.find("[data-testid='home-fields']").exists()).toBe(false);
      // 设备两侧能力都不支持 → **不渲染**配置按钮,而不是渲染一个禁灰的。
      // 理由由 home-phase 的「设备不支持看守位」承担,不必再给一个点不动的按钮。
      expect(wrapper.find("[data-testid='home-configure']").exists()).toBe(false);
      wrapper.unmount();
    });

    it("已启用时只展示产品状态和紧凑配置摘要", async () => {
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: true,
            resetTime: 300,
            presetId: 3,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const card = wrapper.get("[data-testid='home-card']");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("归位到 #3 · 无操作 300 秒后归位");
      expect(card.text()).not.toContain("新鲜度");
      expect(card.text()).not.toContain("查询已验证");
      expect(card.text()).not.toContain("Operation");
      expect(wrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(wrapper.find("[data-testid='home-fields']").exists()).toBe(false);
      expect(wrapper.get("[data-testid='home-configure']").text()).toContain("修改设置");
      wrapper.unmount();
    });

    it("首次查询失败时显示可重试的用户态，不泄露底层错误", async () => {
      api.getHomePosition.mockRejectedValueOnce(new Error("network unavailable"));
      const failedWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(failedWrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
      expect(failedWrapper.get("[data-testid='home-refresh']").text()).toContain("重试");
      expect(failedWrapper.get("[data-testid='home-card']").text()).not.toContain("network unavailable");
      expect(failedWrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      failedWrapper.unmount();
    });

    it("卡片不使用立即生效开关，明确启用和关闭状态分别提供命令按钮", async () => {
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: true,
            resetTime: 300,
            presetId: 0,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const enabledWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(enabledWrapper.get("[data-testid='home-phase']").text()).toContain("已启用");
      expect(enabledWrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(enabledWrapper.get("[data-testid='home-configure']").text()).toContain("修改设置");
      expect(enabledWrapper.get("[data-testid='home-close']").text()).toContain("关闭");
      enabledWrapper.unmount();

      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: false,
            resetTime: null,
            presetId: null,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const disabledWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(disabledWrapper.get("[data-testid='home-phase']").text()).toContain("已关闭");
      expect(disabledWrapper.find("[data-testid='home-toggle']").exists()).toBe(false);
      expect(disabledWrapper.get("[data-testid='home-configure']").text()).toContain("配置并启用");
      expect(disabledWrapper.find("[data-testid='home-close']").exists()).toBe(false);
      disabledWrapper.unmount();
    });

    it.each([
      { resetTime: 0, expected: "0 秒" },
      { resetTime: 9, expected: "9 秒" },
      { resetTime: 3601, expected: "3601 秒" },
      { resetTime: null, expected: "未返回" }
    ])("把协议占位 #0 翻译为未配置并提示异常等待时间 $resetTime", async ({ resetTime }) => {
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: true,
            resetTime,
            presetId: 0,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      const confirmed = wrapper.get("[data-testid='home-confirmed-values']").text();
      expect(confirmed).toContain("归位位置未配置");
      expect(confirmed).not.toContain("#0");
      expect(wrapper.get("[data-testid='home-range-warning']").text()).toContain("归位配置不完整");
      wrapper.unmount();
    });

    it("恢复控制 pending 与 unknown，但 T10 不启动 operation 轮询", async () => {
      vi.useFakeTimers();
      vi.setSystemTime("2026-07-22T10:00:00.000Z");
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          control: {
            status: "pending",
            operationId: "control-pending",
            action: "home_position",
            errorCode: null,
            deadlineAt: "2026-07-22T10:00:15Z"
          }
        })
      );
      const pendingWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(pendingWrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("true");
      expect(pendingWrapper.get("[data-testid='home-phase']").text()).toContain("等待设备确认");
      expect(pendingWrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("control-pending");
      expect(pendingWrapper.find("[data-testid='home-configure']").exists()).toBe(false);
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      pendingWrapper.unmount();

      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          control: {
            status: "unknown",
            operationId: "control-unknown",
            action: "home_position",
            errorCode: "TRANSPORT_UNKNOWN",
            deadlineAt: null
          }
        })
      );
      const unknownWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(unknownWrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(unknownWrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
      expect(unknownWrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(unknownWrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("control-unknown");
      expect(unknownWrapper.get("[data-testid='home-configure']").attributes("disabled")).toBeUndefined();
      expect(api.getPtzOperation).not.toHaveBeenCalled();
      unknownWrapper.unmount();
    });

    it("恢复查询 pending 且不重新发 refresh", async () => {
      vi.useFakeTimers();
      vi.setSystemTime("2026-07-22T10:00:00.000Z");
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
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
        })
      );
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
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          controlSupport: { status: "unsupported", reason: "厂商 profile 未声明控制" },
          querySupport: { status: "unknown", reason: "尚未收到合法查询应答" }
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("厂商 profile 未声明控制");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("尚未收到合法查询应答");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("厂商 profile 未声明控制");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("尚未收到合法查询应答");
      expect(wrapper.find("[data-testid='home-configure']").exists()).toBe(false);
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();

      await wrapper.setProps({ channel: { ...channel, status: 0 } });
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("设备离线");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("上次确认：已启用");
      expect(wrapper.find("[data-testid='home-configure']").exists()).toBe(false);
      expect(wrapper.find("[data-testid='home-refresh']").exists()).toBe(false);
      wrapper.unmount();
    });
  });

  describe("home position operations", () => {
    const nowIso = "2026-07-22T10:00:00.000Z";
    const deadline = (seconds: number) => new Date(Date.parse(nowIso) + seconds * 1000).toISOString();

    it("启用必须挑一个真实存在的预置位，非法启用零请求，关闭只发送 enabled=false", async () => {
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: false,
            resetTime: null,
            presetId: null,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const enableWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await openHomeSettings(enableWrapper);
      // 预置位列表里只有 1..20(mock),这里挑一个真实存在的。
      // ⛔ 别再写 "0":0 号在平台里创建不出来,下拉里已经没有这个选项了。
      await enableWrapper.get("[data-testid='home-preset']").setValue("1");
      await enableWrapper.get("[data-testid='home-reset-time']").setValue("10");
      await submitHomeSettings(enableWrapper);

      expect(api.updateHomePosition).toHaveBeenCalledWith(
        channel.id,
        { enabled: true, resetTime: 10, presetId: 1 },
        expect.stringMatching(/^home-control-/)
      );
      enableWrapper.unmount();

      api.updateHomePosition.mockClear();
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          homePosition: {
            enabled: true,
            resetTime: 9,
            presetId: 0,
            confirmedAt: "2026-07-22T10:00:00Z",
            source: "device_query",
            verification: "verified"
          }
        })
      );
      const closeWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      expect(closeWrapper.get("[data-testid='home-configure']").attributes("disabled")).toBeUndefined();
      await closeWrapper.get("[data-testid='home-close']").trigger("click");
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
      api.updateHomePosition.mockReturnValueOnce(
        new Promise(resolve => {
          resolvePatch = resolve;
        })
      );
      const patchWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await openHomeSettings(patchWrapper);
      await patchWrapper.get("[data-testid='home-dialog-submit']").trigger("click");
      await patchWrapper.get("[data-testid='home-dialog-submit']").trigger("click");
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
      api.getHomePosition.mockResolvedValueOnce(homeResponse()).mockReturnValueOnce(
        new Promise(resolve => {
          resolveRefresh = resolve;
        })
      );
      const refreshWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();
      api.getHomePosition.mockClear();

      await refreshWrapper.get("[data-testid='home-refresh']").trigger("click");
      await refreshWrapper.get("[data-testid='home-refresh']").trigger("click");
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      resolveRefresh(
        homeResponse({
          refresh: { status: "pending", operationId: "refresh-once", errorCode: null, deadlineAt: deadline(10) }
        })
      );
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
        data: {
          operationId: "queued-without-deadline",
          sn: 1,
          channelId: channel.channelId,
          action: "home_position",
          status: "queued"
        }
      });
      let resolveOperation!: (value: any) => void;
      api.getPtzOperation.mockReturnValueOnce(
        new Promise(resolve => {
          resolveOperation = resolve;
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await openHomeSettings(wrapper);
      await submitHomeSettings(wrapper);
      await vi.advanceTimersByTimeAsync(1000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(6000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("queued-without-deadline");

      resolveOperation(operationResponse("accepted", "queued-without-deadline", null));
      await flushPromises();
      await vi.advanceTimersByTimeAsync(5000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      expect(api.getHomePosition).toHaveBeenCalledTimes(1);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
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

      await openHomeSettings(wrapper);
      await submitHomeSettings(wrapper);
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

        await openHomeSettings(wrapper);
        await submitHomeSettings(wrapper);
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
        .mockResolvedValueOnce(
          homeResponse({
            refresh: { status: "pending", operationId: "refresh-op", errorCode: null, deadlineAt: deadline(10) }
          })
        )
        .mockResolvedValueOnce(
          homeResponse({
            freshness: "stale",
            refresh: { status: "succeeded_no_data", operationId: "refresh-op", errorCode: null, deadlineAt: null }
          })
        );
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
        .mockResolvedValueOnce(
          homeResponse({
            control: {
              status: "pending",
              operationId: "reload-control",
              action: "home_position",
              errorCode: null,
              deadlineAt: deadline(2)
            }
          })
        )
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
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          control: {
            status: "pending",
            operationId: "network-timeout",
            action: "home_position",
            errorCode: null,
            deadlineAt: deadline(2)
          }
        })
      );
      api.getPtzOperation.mockRejectedValue(new Error("network unavailable"));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(4000);
      await flushPromises();
      const callsAtCutoff = api.getPtzOperation.mock.calls.length;
      expect(callsAtCutoff).toBeGreaterThan(0);
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
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
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          control: {
            status: "pending",
            operationId: "hung-operation",
            action: "home_position",
            errorCode: null,
            deadlineAt: deadline(2)
          }
        })
      );
      let resolveOperation!: (value: any) => void;
      api.getPtzOperation.mockReturnValueOnce(
        new Promise(resolve => {
          resolveOperation = resolve;
        })
      );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(1000);
      expect(api.getPtzOperation).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(3000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");

      resolveOperation(operationResponse("accepted", "hung-operation", null));
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
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
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
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
      api.getPtzOperation.mockResolvedValueOnce(operationResponse(status, `control-${status}`, null, errorCode));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-close']").trigger("click");
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("上次确认：已启用");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain(errorCode);
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain(`control-${status}`);
      wrapper.unmount();
    });

    it("控制 accepted 后确认状态读取失败时保留提交草稿和 accepted operation", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition
        .mockResolvedValueOnce(
          homeResponse({
            homePosition: {
              enabled: false,
              resetTime: null,
              presetId: null,
              confirmedAt: "2026-07-22T10:00:00Z",
              source: "device_query",
              verification: "verified"
            }
          })
        )
        .mockRejectedValueOnce(new Error("read model unavailable"));
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          operationId: "accepted-read-failed",
          sn: 3,
          channelId: channel.channelId,
          action: "home_position",
          status: "queued"
        }
      });
      api.getPtzOperation.mockResolvedValueOnce(operationResponse("accepted", "accepted-read-failed", null));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await openHomeSettings(wrapper);
      await wrapper.get("[data-testid='home-preset']").setValue("1");
      await wrapper.get("[data-testid='home-reset-time']").setValue("30");
      await submitHomeSettings(wrapper);
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
      expect(wrapper.get("[data-testid='home-notice']").text()).toContain("设备状态可能已变化");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("确认状态读取失败");
      expect(wrapper.get("[data-testid='home-diagnostics']").attributes("title")).toContain("accepted-read-failed");
      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("上次确认：已关闭");
      expect(api.updateHomePosition).toHaveBeenCalledWith(
        channel.id,
        { enabled: true, resetTime: 30, presetId: 1 },
        expect.stringMatching(/^home-control-/)
      );
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
        .mockResolvedValueOnce(
          homeResponse({
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
          })
        )
        .mockResolvedValueOnce(
          homeResponse({
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
          })
        );
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

      await openHomeSettings(wrapper);
      await wrapper.get("[data-testid='home-preset']").setValue("1");
      await wrapper.get("[data-testid='home-reset-time']").setValue("30");
      await submitHomeSettings(wrapper);
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
      api.getHomePosition.mockResolvedValueOnce(homeResponse({ homePosition: disabled })).mockResolvedValueOnce(
        homeResponse({
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
        })
      );
      if (outcome === "no-data") {
        api.getHomePosition.mockResolvedValueOnce(
          homeResponse({
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
          })
        );
      }
      api.updateHomePosition.mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          operationId: "control-unverified",
          sn: 3,
          channelId: channel.channelId,
          action: "home_position",
          status: "queued"
        }
      });
      api.getPtzOperation
        .mockResolvedValueOnce(operationResponse("accepted", "control-unverified", null))
        .mockResolvedValueOnce(
          operationResponse(operationStatus, `reconcile-${outcome}`, null, outcome === "timeout" ? "APPLICATION_TIMEOUT" : null)
        );
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await openHomeSettings(wrapper);
      await wrapper.get("[data-testid='home-preset']").setValue("1");
      await wrapper.get("[data-testid='home-reset-time']").setValue("30");
      await submitHomeSettings(wrapper);
      await vi.advanceTimersByTimeAsync(2000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain(outcome === "timeout" ? "当前状态未确认" : "已启用");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("归位位置未配置");
      expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("30 秒");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("查询未验证");
      expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("缓存已过期");
      if (outcome === "timeout") {
        expect(wrapper.get("[data-testid='home-notice']").text()).toContain("查询设备超时");
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
      const initial = homeResponse({
        homePosition: hasCache ? homeResponse().data.homePosition : null,
        freshness: hasCache ? "fresh" : "unknown"
      });
      const pending = homeResponse({
        homePosition: hasCache ? homeResponse().data.homePosition : null,
        refresh: { status: "pending", operationId: "refresh-timeout", errorCode: null, deadlineAt: deadline(10) }
      });
      api.getHomePosition.mockResolvedValueOnce(initial).mockResolvedValueOnce(pending);
      api.getPtzOperation.mockResolvedValueOnce(operationResponse("timeout", "refresh-timeout", null, "APPLICATION_TIMEOUT"));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await wrapper.get("[data-testid='home-refresh']").trigger("click");
      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();

      expect(wrapper.get("[data-testid='home-status']").attributes("aria-busy")).toBe("false");
      expect(wrapper.get("[data-testid='home-refresh']").attributes("disabled")).toBeUndefined();
      if (hasCache) {
        expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
        expect(wrapper.get("[data-testid='home-confirmed-values']").text()).toContain("上次确认：已启用");
        expect(wrapper.get("[data-testid='home-card']").text()).not.toContain("缓存已过期");
        expect(wrapper.get("[data-testid='home-notice']").text()).toContain("查询设备超时");
      } else {
        expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
        expect(wrapper.get("[data-testid='home-refresh']").text()).toContain("重试");
      }
      wrapper.unmount();
    });

    it("operation unknown 立即停止且不自动重发控制", async () => {
      vi.useFakeTimers();
      vi.setSystemTime(nowIso);
      api.getHomePosition.mockResolvedValueOnce(
        homeResponse({
          control: {
            status: "pending",
            operationId: "explicit-unknown",
            action: "home_position",
            errorCode: null,
            deadlineAt: deadline(10)
          }
        })
      );
      api.getPtzOperation.mockResolvedValueOnce(operationResponse("unknown", "explicit-unknown", null, "TRANSPORT_UNKNOWN"));
      const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
      await flushPromises();

      await vi.advanceTimersByTimeAsync(1000);
      await flushPromises();
      expect(wrapper.get("[data-testid='home-phase']").text()).toContain("当前状态未确认");
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
        Promise.resolve(
          channelId === channel.id
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
              })
        )
      );
      let resolveOperation!: (value: any) => void;
      api.getPtzOperation.mockReturnValueOnce(
        new Promise(resolve => {
          resolveOperation = resolve;
        })
      );
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

    const closeButton = wrapper.get("[data-testid='home-close']");
    expect(closeButton.attributes("disabled")).toBeUndefined();
    await closeButton.trigger("click");
    await flushPromises();

    expect(api.updateHomePosition).toHaveBeenCalledWith(channel.id, { enabled: false }, expect.stringMatching(/^home-control-/));
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

    await openHomeSettings(wrapper);
    const saveButton = wrapper.get("[data-testid='home-dialog-submit']");
    expect(saveButton.attributes("disabled")).toBeDefined();

    await wrapper.get("[data-testid='home-preset']").setValue("1");
    await wrapper.get("[data-testid='home-reset-time']").setValue(9);
    expect(saveButton.attributes("disabled")).toBeDefined();

    await wrapper.get("[data-testid='home-reset-time']").setValue(10);
    expect(saveButton.attributes("disabled")).toBeUndefined();
    await submitHomeSettings(wrapper);

    expect(api.updateHomePosition).toHaveBeenCalledWith(
      channel.id,
      { enabled: true, resetTime: 10, presetId: 1 },
      expect.stringMatching(/^home-control-/)
    );
    wrapper.unmount();
  });

  it("游客只显示实时播放和流监控，不挂载控制或分享入口", async () => {
    userState.account.permissions = ["gb28181:play:start", "gb28181:play:monitor"];
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(api.startPlay).toHaveBeenCalledTimes(1);
    expect(api.getStreamMonitor).toHaveBeenCalled();
    expect(wrapper.find("[data-testid='linked-tab-probe']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-ptz']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-tab-advanced']").exists()).toBe(false);
    // 「视频参数」tab 用 canViewPtz 门禁 —— 游客没有 ptz:view，整栏都不该挂载。
    expect(wrapper.find("[data-testid='linked-tab-videoparam']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-side-ptz']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-side-advanced']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-side-videoparam']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-detail-ptz']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-detail-advanced']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-detail-videoparam']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='probe-start']").exists()).toBe(false);
    expect(wrapper.find(".protocol-copy-btn").exists()).toBe(false);
    expect(api.getControlCapabilities).not.toHaveBeenCalled();
    expect(api.getHomePosition).not.toHaveBeenCalled();
    expect(api.getChannelVideoParams).not.toHaveBeenCalled();
    expect(api.controlPtz).not.toHaveBeenCalled();
    expect(api.createTalkSession).not.toHaveBeenCalled();
    expect(api.createStreamProbe).not.toHaveBeenCalled();
    expect(api.createDeviceSnapshotSession).not.toHaveBeenCalled();

    wrapper.unmount();
  });

  it("游客没有共享停播权限时切换和迟到点播响应都不调用 stopPlay", async () => {
    userState.account.permissions = ["gb28181:play:start", "gb28181:play:monitor"];
    let resolveInitial!: (value: any) => void;
    api.startPlay.mockReturnValueOnce(
      new Promise(resolve => {
        resolveInitial = resolve;
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.setProps({ visible: false });
    resolveInitial({
      code: 0,
      message: "",
      data: {
        streamId: "late-stream",
        ssrc: "late-ssrc",
        app: "rtp",
        wsflvUrl: "ws://zlm/late.live.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    await flushPromises();
    expect(api.stopPlay).not.toHaveBeenCalled();
    wrapper.unmount();

    api.stopPlay.mockClear();
    api.startPlay.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        streamId: "stream-current",
        ssrc: "ssrc",
        app: "rtp",
        wsflvUrl: "ws://zlm/current.live.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    const nextWrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await nextWrapper.setProps({ channel: { ...channel, id: 2, channelId: "0411212756" } });
    await flushPromises();
    expect(api.stopPlay).not.toHaveBeenCalled();
    nextWrapper.unmount();
  });

  it("确认删除回调执行时重新校验预置位和巡航权限", async () => {
    let warningConfig: any;
    const warning = vi.spyOn(Modal, "warning").mockImplementation((config: any) => {
      warningConfig = config;
      return {} as any;
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get(".preset-tile-del").trigger("click");
    userState.account.permissions = ["gb28181:ptz:view"];
    await nextTick();
    await warningConfig.onOk?.();
    expect(api.deletePtzPreset).not.toHaveBeenCalled();

    userState.account.permissions = ["*:*:*"];
    await nextTick();
    await wrapper.get(".cruise-tile .preset-tile-del").trigger("click");
    userState.account.permissions = ["gb28181:ptz:view"];
    await nextTick();
    await warningConfig.onOk?.();
    expect(api.controlPtzCruise).not.toHaveBeenCalled();

    warning.mockRestore();
    wrapper.unmount();
  });

  it("权限撤销后停止对讲轮询和等待循环的后续请求", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const vm = wrapper.vm as unknown as {
      beginTalkPoll: (channelId: number, sessionId: string) => void;
      waitTalkActive: (channelId: number, sessionId: string, token: number) => Promise<boolean>;
      talkToken: number;
    };

    api.getTalkSession.mockClear();
    vm.beginTalkPoll(channel.id, "talk-poll");
    userState.account.permissions = [];
    await vi.advanceTimersByTimeAsync(10000);
    await flushPromises();
    expect(api.getTalkSession).not.toHaveBeenCalled();

    let resolveStatus!: (value: any) => void;
    api.getTalkSession.mockReturnValueOnce(
      new Promise(resolve => {
        resolveStatus = resolve;
      })
    );
    userState.account.permissions = ["gb28181:talk:control"];
    const waiting = vm.waitTalkActive(channel.id, "talk-wait", vm.talkToken);
    userState.account.permissions = [];
    resolveStatus({ code: 0, message: "", data: { sessionId: "talk-wait", state: "pending" } });
    await flushPromises();
    await expect(waiting).resolves.toBe(false);
    expect(api.getTalkSession).toHaveBeenCalledTimes(1);

    wrapper.unmount();
  });

  /* ────────────────── 设备资源同步(「从设备同步」) ──────────────────
   *
   * 这几条锁的是**协作时序**,不是渲染结果。
   *
   * 预置位和巡航都住在设备上,平台库里只是一份镜像。在加这个按钮之前,前端**从来
   * 没有**发起过回读:`listPtzPresets(channelId)` 与 `loadCruises(..., false)` 的
   * refresh 参数一律是 false,于是设备上早就配好的预置位和巡航在界面上永远不出现,
   * 而巡航卡片头顶那行小字还在一直说「缓存数据已过期」—— 提示了问题,却不给入口。
   *
   * 更要紧的是**时序**:`?refresh=true` 的语义是"把查询发给设备",它的 HTTP 返回里
   * 带的是查询**之前**的缓存 + 一个 operationId。设备应答是异步的,所以必须轮询到
   * 终止态再重读。只发不等的话,设备稍慢一点界面就什么都不变,操作员会以为按钮坏了。
   */

  // 回读应答用**局部**夹具:模块级的 presets/cruises 定义在 `vi.hoisted` 工厂里,
  // 测试体访问不到(工厂要在 import 之前跑,这是刻意的),拿名字会踩 TDZ。
  const presetRows = [{ presetId: 1, name: "预置位 1", updatedAt: "2026-07-22T10:00:00Z" }];

  it("预置位同步:先下发查询,等操作落地后才重读列表", async () => {
    vi.useFakeTimers();
    let syncStatus: "queued" | "accepted" = "queued";
    api.getPtzOperation.mockImplementation((_channelId: number, operationId: string) =>
      Promise.resolve(
        operationId === "preset-sync-op"
          ? operationResponse(syncStatus, operationId, syncStatus === "queued" ? "2026-07-22T10:00:13Z" : null)
          : operationResponse("accepted", operationId, null)
      )
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    api.listPtzPresets.mockClear();
    api.listPtzPresets
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: { list: presetRows, freshness: "stale", refreshOperationId: "preset-sync-op" }
      })
      .mockResolvedValueOnce({ code: 0, message: "", data: { list: presetRows, freshness: "fresh" } });

    await wrapper.get("[data-testid='preset-sync-btn']").trigger("click");
    await flushPromises();

    // 第 1 次必须带 refresh=true —— 查询真的下发到设备,而不是重读本地缓存
    expect(api.listPtzPresets).toHaveBeenNthCalledWith(1, channel.id, true);
    // 设备还没答(queued 不是终止态)之前**不能**重读:读出来还是查询前的旧数据,
    // 而清单查询会把设备没报的编号标记为已删除,读早了界面会先闪一下空
    expect(api.listPtzPresets).toHaveBeenCalledTimes(1);
    expect(api.getPtzOperation).toHaveBeenCalledWith(channel.id, "preset-sync-op");
    const button = wrapper.get("[data-testid='preset-sync-btn']");
    expect(button.text()).toContain("同步中");
    expect(button.attributes("disabled")).toBeDefined();

    syncStatus = "accepted";
    await vi.advanceTimersByTimeAsync(300);
    await flushPromises();
    expect(api.listPtzPresets).toHaveBeenNthCalledWith(2, channel.id, false);
    expect(wrapper.get("[data-testid='preset-sync-btn']").text()).toContain("已同步");
    wrapper.unmount();
  });

  it("巡航同步:重读列表后,给点位未知的轨迹逐条回读点位链", async () => {
    api.getPtzOperation.mockResolvedValue(operationResponse("accepted", "any-op", null));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    api.listCruiseTracks.mockClear();
    api.getCruiseTrack.mockClear();
    api.getCruiseTrack.mockImplementation((_channelId: number, trackId: number) =>
      Promise.resolve({
        code: 0,
        message: "",
        data: { track: {}, freshness: "fresh", refreshOperationId: `cruise-detail-${trackId}` }
      })
    );
    api.listCruiseTracks
      // ① 带 refresh 的清单查询
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          list: [{ trackId: 1, name: "车间巡检", enabled: true }],
          freshness: "stale",
          refreshOperationId: "cruise-sync-op"
        }
      })
      // ② 重读:设备侧新发现的 #7 这一步才进库,但它**没有点位**
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          list: [
            {
              trackId: 1,
              name: "车间巡检",
              enabled: true,
              detail: { trackId: 1, cruisePoints: [{ presetIndex: 3, stayTime: 30, speed: 128 }] }
            },
            { trackId: 7, name: "球机默认轨迹", enabled: true, detail: { trackId: 7, name: "球机默认轨迹" } }
          ],
          freshness: "fresh"
        }
      })
      // ③ 补完点位后再重读一次
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          list: [
            {
              trackId: 1,
              name: "车间巡检",
              enabled: true,
              detail: { trackId: 1, cruisePoints: [{ presetIndex: 3, stayTime: 30, speed: 128 }] }
            },
            {
              trackId: 7,
              name: "球机默认轨迹",
              enabled: true,
              detail: { trackId: 7, cruisePoints: [{ presetIndex: 2, stayTime: 30, speed: 128 }] }
            }
          ],
          freshness: "fresh"
        }
      });

    await wrapper.get("[data-testid='cruise-sync-btn']").trigger("click");
    await flushPromises();
    await flushPromises();

    expect(api.listCruiseTracks).toHaveBeenNthCalledWith(1, channel.id, true);
    expect(api.listCruiseTracks).toHaveBeenNthCalledWith(2, channel.id, false);
    // ⛔ 止步于"重读列表"是不够的。标准 A.2.6.13 的清单应答里只有 <Number/> 和
    //    <Name/>,**没有点位集合** —— 点位链只能靠 `CruiseTrackQuery` 逐条问。
    //    少了这一步,设备侧发现的轨迹会永远停在「点位待查询」,操作员看得到轨迹名
    //    却看不到它串了哪几个预置位,而"串了哪几个预置位"正是他点同步最想确认的事。
    expect(api.getCruiseTrack).toHaveBeenCalledWith(channel.id, 7, true);
    // #1 的点位库里已经有,不重复问设备
    expect(api.getCruiseTrack).not.toHaveBeenCalledWith(channel.id, 1, true);
    expect(wrapper.get("[data-testid='cruise-tile-tooltip-7']").attributes("content")).toContain("预置位 2");
    expect(api.listCruiseTracks).toHaveBeenNthCalledWith(3, channel.id, false);
    expect(wrapper.get("[data-testid='cruise-sync-btn']").text()).toContain("已同步");
    wrapper.unmount();
  });

  it("设备不答时收敛为「同步失败」,并把原因写进悬浮提示", async () => {
    vi.useFakeTimers();
    // 一直停在 queued:设备收到了但没回,或压根没走到它
    api.getPtzOperation.mockResolvedValue(operationResponse("queued", "preset-sync-op", "2026-07-22T10:00:13Z"));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    api.listPtzPresets.mockClear();
    api.listPtzPresets.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { list: presetRows, freshness: "stale", refreshOperationId: "preset-sync-op" }
    });

    await wrapper.get("[data-testid='preset-sync-btn']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='preset-sync-btn']").text()).toContain("同步中");

    // 到总上限还没终止态就放弃等待 —— 不能无限转圈
    await vi.advanceTimersByTimeAsync(16000);
    await flushPromises();
    const button = wrapper.get("[data-testid='preset-sync-btn']");
    expect(button.text()).toContain("同步失败");
    // 「设备未应答」这类具体原因塞不进药丸(卡片只有详情条三分之一宽),
    // 但不说出来操作员只会反复点同一个按钮
    expect(button.attributes("title")).toContain("设备未应答");
    expect(button.attributes("disabled")).toBeUndefined();
    wrapper.unmount();
  });

  it("同步进行中重复点击不会重复下发查询", async () => {
    vi.useFakeTimers();
    api.getPtzOperation.mockResolvedValue(operationResponse("queued", "preset-sync-op", "2026-07-22T10:00:13Z"));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    api.listPtzPresets.mockClear();
    api.listPtzPresets.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { list: presetRows, freshness: "stale", refreshOperationId: "preset-sync-op" }
    });

    await wrapper.get("[data-testid='preset-sync-btn']").trigger("click");
    await flushPromises();
    expect(api.listPtzPresets).toHaveBeenCalledTimes(1);
    await wrapper.get("[data-testid='preset-sync-btn']").trigger("click");
    await flushPromises();
    expect(api.listPtzPresets).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("装载时不回读设备 —— 回读只由「同步」按钮触发", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    // 打开播放面板就往设备上抛一串查询会烧重试预算、也会在弱设备上互相挤,
    // 所以装载一律读本地缓存,只有操作员点了「同步」才真的下发
    expect(api.listPtzPresets).toHaveBeenCalledWith(channel.id, false);
    expect(api.listPtzPresets).not.toHaveBeenCalledWith(channel.id, true);
    expect(api.listCruiseTracks).toHaveBeenCalledWith(channel.id, false);
    expect(api.listCruiseTracks).not.toHaveBeenCalledWith(channel.id, true);
    expect(api.getCruiseTrack).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("左侧工作区保留三个既有模块，并按任务隔离配置分组", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    expect(wrapper.find("[data-testid='linked-tab-ptz']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-probe']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-advanced']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-videoparam']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-deviceconfig']").exists()).toBe(true);
    expect(wrapper.get("nav[aria-label='播放工作区']").findAll("button")).toHaveLength(8);
    expect(wrapper.get("[data-testid='linked-tab-videoparam']").text()).toContain("视频编码");

    await wrapper.get("[data-testid='linked-tab-videoparam']").trigger("click");
    expect(wrapper.get("[data-testid='linked-detail-videoparam']").classes()).toContain("linked-detail-actions");
    expect(wrapper.find("[data-testid='video-param-bottom-read']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='video-param-bottom-apply']").exists()).toBe(true);

    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    const sidePanel = wrapper.get("[data-testid='linked-side-deviceconfig']");
    expect(sidePanel.text()).toContain("图像叠加 OSD");
    // 画面设置不再有二级 tab：镜像 + 隐私遮挡下沉到底栏卡片后，侧栏只剩「图像叠加」一组，
    // `dcg-nav` 的 `configGroups.length > 1` 就此为假 —— 导航整体消失，而不是留一个只有一项的导航。
    expect(sidePanel.find(".dcg-nav").exists()).toBe(false);
    expect(sidePanel.find("[data-testid='dcg-nav-osd']").exists()).toBe(false);
    expect(sidePanel.find("[data-testid='dcg-nav-picture']").exists()).toBe(false);
    // 原「画面处理」的全部内容现在由底栏卡片承载
    const pictureBar = wrapper.get("[data-testid='linked-detail-picture']");
    expect(pictureBar.find("[data-testid='picture-mask-card']").exists()).toBe(true);
    expect(pictureBar.find("[data-testid='picture-mirror-card']").exists()).toBe(true);
    // 2026-09-19 流程重做：下发入口从底栏第三张卡搬进画布浮条。反向钉住"提交卡不再回来" ——
    // 这个 testid 一复现，就说明有人把卡片又加回来了，浮条与卡片会变成两个入口。
    expect(pictureBar.find("[data-testid='picture-apply-card']").exists()).toBe(false);
    // 视频参数属性走另一条通道，不混进本 tab 的组清单
    expect(sidePanel.find("[data-testid='dcg-nav-video-param']").exists()).toBe(false);
    expect(sidePanel.text()).not.toContain("SVAC");
    expect(sidePanel.find("[data-testid='dcg-nav-basic']").exists()).toBe(false);
    for (const [tab, group] of [
      ["record", "record-plan"],
      ["alarm", "alarm-report"],
      ["device", "basic"]
    ]) {
      await wrapper.get(`[data-testid='linked-tab-${tab}']`).trigger("click");
      await flushPromises();
      expect(wrapper.get("[data-testid='linked-side-deviceconfig'] [data-testid='dcg-group-title']").text()).toBe(
        { "record-plan": "录像计划", "alarm-report": "报警上报", basic: "基本参数" }[group]
      );
      expect(wrapper.find("[data-testid='linked-side-deviceconfig'] [data-testid='dcg-nav-video-param']").exists()).toBe(false);
    }
    wrapper.unmount();
  });
});

/**
 * 画面设置底栏卡片。
 *
 * 这里只测**控制台侧**的接线：卡片是否按设备回读值渲染、框选坐标是否换算到**设备声明的
 * 图像画布**（读不到时退回画面尺寸并明确标注）、下发是否带上同一组的两块。
 * DeviceConfigDrawer 自己的读写闸门由它的用例覆盖。
 */
describe("PlayConsoleLinked 画面设置底栏卡片", () => {
  beforeEach(() => {
    userState.account = reactive({ permissions: ["*:*:*"] });
    api.getChannelDeviceConfigs.mockReset();
    api.getChannelDeviceConfigs.mockResolvedValue(pictureDeviceConfigResponse());
    api.applyChannelDeviceConfigs.mockReset();
    api.applyChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "",
      data: { action: "apply-device-config", reconcilePending: false }
    });
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
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  /** 挂载控制台并切到「画面设置」页签（该 tab 现在是 OSD 侧栏 + 三张底栏卡片）。 */
  async function openPictureTab() {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    await flushPromises();
    return wrapper;
  }

  it("遮挡卡片按 Seq 列出设备回读区域，未用的槽位是空位", async () => {
    const wrapper = await openPictureTab();
    const card = wrapper.get("[data-testid='picture-mask-card']");
    expect(card.find("[data-testid='picture-mask-slot-1']").text()).toContain("10,20 → 300,400");
    expect(card.find("[data-testid='picture-mask-slot-2']").text()).toContain("空位");
    wrapper.unmount();
  });

  it("镜像卡片按设备回读值高亮", async () => {
    const wrapper = await openPictureTab();
    const card = wrapper.get("[data-testid='picture-mirror-card']");
    expect(card.get("[data-testid='picture-mirror-0']").classes()).toContain("active");
    expect(card.get("[data-testid='picture-mirror-1']").classes()).not.toContain("active");
    wrapper.unmount();
  });

  it("没读到设备声明的画布时退路仍通：比例 × 画面尺寸 1280×720 写进空槽位", async () => {
    // ⛔ 这是**退路**，不是基准定义：遮挡坐标的基准是设备声明的图像尺寸（见下一条用例）。
    //    退路也不能断 —— 自研模拟器把遮挡烧进 GL FBO、FBO 尺寸就是编码尺寸，
    //    它的合法基准确实等于画面尺寸。
    const wrapper = await openPictureTab();
    await wrapper.get("[data-testid='picture-mask-add-btn']").trigger("click");
    await flushPromises();

    const layer = wrapper.get("[data-testid='mask-draw-layer']");
    // ⛔ 必须给这层一个真实矩形：jsdom 的 getBoundingClientRect 恒为 0，
    //    不 mock 的话所有坐标都会被 clamp 到边界，断言就变成恒真。
    (layer.element as HTMLElement).getBoundingClientRect = () =>
      ({
        left: 0,
        top: 0,
        width: 200,
        height: 100,
        right: 200,
        bottom: 100,
        x: 0,
        y: 0,
        toJSON: () => ({})
      }) as DOMRect;

    await layer.trigger("pointerdown", { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    await layer.trigger("pointermove", { clientX: 110, clientY: 80, pointerId: 1 });
    await layer.trigger("pointerup", { clientX: 110, clientY: 80, pointerId: 1 });
    await flushPromises();

    // (10,20)-(110,80) ÷ 200×100 = (0.05,0.2)-(0.55,0.8) → ×1280×720
    expect(wrapper.get("[data-testid='picture-mask-slot-2']").text()).toContain("64,144 → 704,576");
    wrapper.unmount();
  });

  it("框选换算到**设备声明的画布**：比例 × 704×576 写进槽位，卡片标出基准", async () => {
    // ⭐ 2026-09-19 真机定因（海康 IPC，主码流 2560×1440）：遮挡 `Point` 的基准是设备在
    //    `OSDConfig` 里声明的 `Length/Width`（该机 704×576），**不是**画面解码尺寸。
    //    实测：发 `0,0,640,360` ⇒ 黑块落在 x 0~90.8% / y 0~62.6%（= 640/704、360/576）。
    // ⛔ 拿解码尺寸算坐标 ⇒ 遮挡块整体右移放大 ⇒"我画的框挡住了别的地方"。
    api.getChannelDeviceConfigs.mockResolvedValue(pictureDeviceConfigResponse(undefined, { length: 704, width: 576 }));
    const wrapper = await openPictureTab();

    // 基准常驻在卡片上：用户是在"发现落点不对"之后才会去找它，那就已经晚了。
    const base = wrapper.get("[data-testid='picture-mask-base']");
    expect(base.text()).toContain("704×576");
    expect(base.text()).toContain("设备声明");
    expect(base.classes()).not.toContain("is-unverified");

    await wrapper.get("[data-testid='picture-mask-add-btn']").trigger("click");
    await flushPromises();
    const layer = wrapper.get("[data-testid='mask-draw-layer']");
    (layer.element as HTMLElement).getBoundingClientRect = () =>
      ({
        left: 0,
        top: 0,
        width: 200,
        height: 100,
        right: 200,
        bottom: 100,
        x: 0,
        y: 0,
        toJSON: () => ({})
      }) as DOMRect;

    await layer.trigger("pointerdown", { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    await layer.trigger("pointermove", { clientX: 110, clientY: 80, pointerId: 1 });
    await layer.trigger("pointerup", { clientX: 110, clientY: 80, pointerId: 1 });
    await flushPromises();

    // (10,20)-(110,80) ÷ 200×100 = (0.05,0.2)-(0.55,0.8) → ×704×576
    expect(wrapper.get("[data-testid='picture-mask-slot-2']").text()).toContain("35,115 → 387,461");
    wrapper.unmount();
  });

  it("基准没拿到设备声明时标成「未验证」：退路不能伪装成设备值", async () => {
    const wrapper = await openPictureTab();
    const base = wrapper.get("[data-testid='picture-mask-base']");
    expect(base.text()).toContain("1280×720");
    expect(base.text()).toContain("未读到设备声明");
    expect(base.classes()).toContain("is-unverified");
    wrapper.unmount();
  });

  it("遮挡投影按设备画布归一化：704×576 的框画在画面对应的位置", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(
      pictureDeviceConfigResponse(
        { on: 1, regions: [{ seq: 1, left: 0, top: 0, right: 352, bottom: 288 }] },
        { length: 704, width: 576 }
      )
    );
    const wrapper = await openPictureTab();
    const box = wrapper.get("[data-testid='mask-overlay-layer'] .mask-overlay-box");
    // 352/704 = 50%、288/576 = 50% ⇒ 左上角四分之一。
    // ⛔ 用画面尺寸（1280×720）除会算成 27.5% / 40% —— 框就画歪了，
    //    与"落点错位"是同一个错的两半（一个在发出去的路上，一个在画回来的路上）。
    expect((box.element as HTMLElement).style.left).toBe("0%");
    expect((box.element as HTMLElement).style.width).toBe("50%");
    expect((box.element as HTMLElement).style.height).toBe("50%");
    wrapper.unmount();
  });

  it("零面积区域不摆成「已配置」：设备回读的删除痕迹既不出槽位也不投影", async () => {
    // 平台停用遮挡时就是把 `Seq 1..4` 铺成零面积让设备删；这台设备会把它按自己的
    // 画布回读回来（实测 Seq1 = `704,576,704,576`，`Num` 仍是 1）。
    api.getChannelDeviceConfigs.mockResolvedValue(
      pictureDeviceConfigResponse({
        on: 1,
        regions: [{ seq: 1, left: 704, top: 576, right: 704, bottom: 576 }]
      })
    );
    const wrapper = await openPictureTab();
    // 画面上没有任何遮挡 ⇒ 走空态，连槽位网格都不渲染（不是"槽位里写着一条"）。
    expect(wrapper.find("[data-testid='picture-mask-blank']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='picture-mask-slot-1']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='mask-overlay-layer']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("下发把镜像与遮挡作为同一组一起发出去", async () => {
    const wrapper = await openPictureTab();
    // 没改动时浮条**不存在**：它是零存在的，不占画面底部、也不制造"这里是不是该点一下"的干扰。
    expect(wrapper.find("[data-testid='picture-draft-bar']").exists()).toBe(false);
    await wrapper.get("[data-testid='picture-mirror-1']").trigger("click");
    await flushPromises();
    // 一改就出现，而且把"改的是什么"写出来 —— 只报 "1 项" 的话用户得回侧栏猜。
    const bar = wrapper.get("[data-testid='picture-draft-bar']");
    expect(bar.text()).toContain("镜像");
    await bar.get("[data-testid='picture-draft-apply']").trigger("click");
    await flushPromises();

    expect(api.applyChannelDeviceConfigs).toHaveBeenCalledTimes(1);
    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    expect(Object.keys(blocks).sort()).toEqual(["frameMirror", "pictureMask"]);
    expect((blocks.frameMirror as { value: number }).value).toBe(1);
    // 总闸与区域列表同属 PictureMask 一块：设备回读时 on 已开，没动它就原样发回去
    expect((blocks.pictureMask as { on: unknown }).on).toBeTruthy();
    // ⛔ seq 用的是**槽位编号**（mask3 → Seq=3）而不是数组下标：
    //    删掉区域 2 之后把 3/4 前移，等于给设备上的区域静默改名。
    expect((blocks.pictureMask as { regions: Array<{ seq: number }> }).regions.map(r => r.seq)).toEqual([1]);
    wrapper.unmount();
  });

  it("设备已停用但区域残留：画布不画框、卡片给说明态，不摆成「当前遮挡」", async () => {
    // ⛔ 2026-09-19 现场：设备 `On=0`（遮挡确实已停用，肉眼可见画面无遮挡），但国标停用
    //    只关开关、**不清区域** —— 设备保留了 RegionList，回读落库
    //    `{"on":0,"regions":[{"seq":1,"left":144,…}]}`。
    //    旧渲染把这个残留区域照旧画成画布上的框、还在卡片里列成一条，用户于是以为"没清掉"。
    api.getChannelDeviceConfigs.mockResolvedValue(
      pictureDeviceConfigResponse({ on: 0, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
    );
    const wrapper = await openPictureTab();

    // 画布上不该有投影框：此刻画面上本来就没有遮挡。
    expect(wrapper.find("[data-testid='mask-overlay-layer'] [data-seq='1']").exists()).toBe(false);
    // 卡片给说明态，而不是槽位网格。
    expect(wrapper.find("[data-testid='picture-mask-retained']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='picture-mask-slot-1']").exists()).toBe(false);
    // ⛔ 头部计数徽章必须一起消失：否则"徽章写着 1、正文说已停用"自相矛盾。
    expect(wrapper.find("[data-testid='picture-mask-card'] .preset-count").exists()).toBe(false);
    wrapper.unmount();
  });

  it("设备停用但区域残留时手动开总闸：那块框按「即将生效」画出来", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(
      pictureDeviceConfigResponse({ on: 0, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
    );
    const wrapper = await openPictureTab();
    expect(wrapper.find("[data-testid='mask-overlay-layer'] [data-seq='1']").exists()).toBe(false);

    await wrapper.get("[data-testid='picture-mask-switch']").trigger("click");
    await flushPromises();

    // 开闸 ⇒ 它马上要生效，必须看得见（否则用户无从确认"启用后会挡住哪"）。
    expect(wrapper.find("[data-testid='mask-overlay-layer'] [data-seq='1']").exists()).toBe(true);
    // 卡片也让出槽位网格。
    expect(wrapper.find("[data-testid='picture-mask-retained']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='picture-mask-slot-1']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("点开关即下发：**只发 PictureMask 这一块**，发完就地成事实（不停在「将启用」）", async () => {
    // 2026-09-19 产品决定。用户原话：「点这个启用，能不能直接调用专门对应的启用信令？
    // 还是说，必须得新建一条它才启用？」——真机实测给出了答案：
    //   ① 协议里**没有**"独立的启用信令"，`<On>` 只是 `PictureMask` 里的一个字段，
    //      和区域列表挤在同一条 DeviceConfig 报文里；
    //   ② 但设备**接受**单独发 `<On>1</On><SumNum>0</SumNum>`（不带 `RegionList`），
    //      回读 `On=1` 且不会凭空创建区域 ⇒ "只启用"完全可执行，没有理由逼用户先画区域；
    //   ③ 所以开关改成"点即发" —— 攒草稿等浮条「下发」会让用户以为点了没生效。
    api.getChannelDeviceConfigs.mockResolvedValue(
      pictureDeviceConfigResponse({ on: 0, regions: [{ seq: 1, left: 144, top: 295, right: 704, bottom: 576 }] })
    );
    const wrapper = await openPictureTab();
    const toggle = () => wrapper.get("[data-testid='picture-mask-switch']");

    // 没动过 ⇒ 草稿与设备一致，这时才可以如实说「已停用」。
    expect(toggle().text()).toContain("已停用");
    expect(toggle().text()).not.toContain("将");

    await toggle().trigger("click");
    await flushPromises();

    // ⛔ 核心：点一下就发出去了，不用再去点浮条的「下发」。
    expect(api.applyChannelDeviceConfigs).toHaveBeenCalledTimes(1);
    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    // ⛔ 只发遮挡这一块：用户点的是遮挡开关，不该顺手把还没下发的镜像草稿一起提交出去。
    expect(Object.keys(blocks)).toEqual(["pictureMask"]);
    expect((blocks.pictureMask as { on: number }).on).toBe(1);
    // 发完就地推基准（草稿成事实）⇒ 按钮如实说「已启用」，不再吊在"将启用"上、
    // 浮条也不会挂着"1 项画面改动未下发"。
    expect(toggle().text()).toContain("已启用");
    expect(toggle().classes()).not.toContain("pending");
    expect(wrapper.find("[data-testid='picture-draft-bar']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("删掉最后一个遮挡区后总闸自动关闭；再点开只发 `On=1`（无区域）并提示看不到遮挡", async () => {
    const wrapper = await openPictureTab();

    // ① 删掉唯一的区域。与「画了区域顺手开总闸」对称：最后一个区域没了就顺手关掉 ——
    //    否则会拼出 `{on:1,regions:[]}`，设备保留原有区域、遮挡照旧生效（2026-09-19 现场）。
    await wrapper.get("[data-testid='picture-mask-del-1']").trigger("click");
    await flushPromises();
    // ⛔ 只能显示「将停用」，**不能**显示「已停用」：这一步只改了草稿，设备那边
    //    `on` 还是 1（回读值没变、一个字节都没发出去）。
    //    （区域编辑仍是草稿制 —— 删错了还能「还原」；只有总闸开关是点即发。）
    expect(wrapper.get("[data-testid='picture-mask-switch']").text()).toContain("将停用");
    expect(wrapper.get("[data-testid='picture-mask-switch']").attributes("title")).toContain("下发");

    // ② 用户又点开总闸，但一块区域都没画 ⇒ 立刻下发 `On=1`（设备接受），
    //    并就地说明"看不到遮挡 + 设备里那点残留区域会一起活过来"。
    //    ⛔ 放行不等于沉默：这句提示是"启用了却没有遮挡"唯一的事后解释。
    await wrapper.get("[data-testid='picture-mask-switch']").trigger("click");
    await flushPromises();

    expect(api.applyChannelDeviceConfigs).toHaveBeenCalledTimes(1);
    const blocks = api.applyChannelDeviceConfigs.mock.calls[0]![1] as Record<string, unknown>;
    expect((blocks.pictureMask as { on: number }).on).toBe(1);
    expect((blocks.pictureMask as { regions: unknown[] }).regions).toEqual([]);
    const notice = wrapper.get("[data-testid='picture-mask-notice']").text();
    expect(notice).toContain("没有携带任何遮挡区域");
    // 设备事实是 `on=1`（遮挡本来就在生效）⇒ 残留数为 0 ⇒ 补的是"画面不会变"那句。
    // "设备里还留着旧区域"那支要设备 `On=0` 且草稿区域被清空，形态在 Drawer 层构造。
    expect(notice).toContain("画面上不会有任何变化");
    wrapper.unmount();
  });

  /**
   * jsdom 的 `getBoundingClientRect` 恒为 0，框选层必须被喂一个真实矩形，
   * 否则所有坐标都会被 clamp 到边界，断言就变成恒真。
   */
  function stubMaskDrawRect(element: Element, width = 200, height = 100) {
    (element as HTMLElement).getBoundingClientRect = () =>
      ({ left: 0, top: 0, width, height, right: width, bottom: height, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
  }

  it("画布上只有草稿框换样式：设备回读的那块仍是 data-draft=0", async () => {
    const wrapper = await openPictureTab();
    const boxOf = (seq: number) => wrapper.get(`[data-testid='mask-overlay-layer'] [data-seq='${seq}']`);
    // 设备回读来的 #1 是「事实」，不是草稿 —— 进页签就带着 draft=0。
    expect(boxOf(1).attributes("data-draft")).toBe("0");
    expect(boxOf(1).text()).toContain("#1");
    expect(boxOf(1).text()).not.toContain("待下发");

    await wrapper.get("[data-testid='picture-mask-add-btn']").trigger("click");
    await flushPromises();
    const layer = wrapper.get("[data-testid='mask-draw-layer']");
    stubMaskDrawRect(layer.element);
    await layer.trigger("pointerdown", { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    await layer.trigger("pointermove", { clientX: 110, clientY: 80, pointerId: 1 });
    await layer.trigger("pointerup", { clientX: 110, clientY: 80, pointerId: 1 });
    await flushPromises();

    // 新画的 #2 只活在草稿里，设备上还没有 —— 必须与 #1 看得出区别。
    expect(boxOf(2).attributes("data-draft")).toBe("1");
    expect(boxOf(2).text()).toContain("待下发");
    expect(boxOf(1).attributes("data-draft")).toBe("0");
    wrapper.unmount();
  });

  it("离开画面设置页：浮条收起，页签上留下未下发角标", async () => {
    const wrapper = await openPictureTab();
    await wrapper.get("[data-testid='picture-mirror-1']").trigger("click");
    await flushPromises();
    // 本页由浮条负责，角标不重复提醒。
    expect(wrapper.find("[data-testid='linked-tab-draft-dot']").exists()).toBe(false);

    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await flushPromises();
    // 草稿没丢（组件还在），但下发入口随浮条一起离开了视野 —— 角标把它钉回页签上。
    expect(wrapper.find("[data-testid='picture-draft-bar']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='linked-tab-draft-dot']").attributes("title")).toContain("镜像");
    wrapper.unmount();
  });

  it("有未下发草稿时关闭控制台要先确认，确认后才真关", async () => {
    let warningConfig: any;
    const warning = vi.spyOn(Modal, "warning").mockImplementation((config: any) => {
      warningConfig = config;
      return {} as any;
    });
    const wrapper = await openPictureTab();
    await wrapper.get("[data-testid='picture-mirror-1']").trigger("click");
    await flushPromises();

    await wrapper.get("[data-testid='play-console-close']").trigger("click");
    expect(warning).toHaveBeenCalledTimes(1);
    // 关掉是真丢（unmount-on-close），所以先拦住：此刻还没关。
    expect(wrapper.emitted("update:visible")).toBeUndefined();

    await warningConfig.onOk?.();
    expect(wrapper.emitted("update:visible")?.at(-1)).toEqual([false]);

    warning.mockRestore();
    wrapper.unmount();
  });

  it("没有草稿时关闭不问，直接关", async () => {
    const warning = vi.spyOn(Modal, "warning").mockImplementation(() => ({}) as any);
    const wrapper = await openPictureTab();
    await wrapper.get("[data-testid='play-console-close']").trigger("click");
    expect(warning).not.toHaveBeenCalled();
    expect(wrapper.emitted("update:visible")?.at(-1)).toEqual([false]);
    warning.mockRestore();
    wrapper.unmount();
  });

  it("切通道时如实告知上一个通道的草稿被放弃（入口在设备列表，这里拦不住）", async () => {
    const warning = vi.spyOn(Message, "warning").mockImplementation(() => ({}) as any);
    const wrapper = await openPictureTab();
    await wrapper.get("[data-testid='picture-mirror-1']").trigger("click");
    await flushPromises();

    await wrapper.setProps({ channel: { ...channel, id: 2, channelId: "0411212756" } });
    await flushPromises();
    expect(warning).toHaveBeenCalledTimes(1);
    expect(String(warning.mock.calls[0]![0])).toContain("已放弃");

    warning.mockRestore();
    wrapper.unmount();
  });
});
