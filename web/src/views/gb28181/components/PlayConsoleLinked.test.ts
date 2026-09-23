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
    reportPlaybackClientEvent: vi.fn().mockResolvedValue({ code: 0, message: "", data: { accepted: true } }),
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
    // 自动扫描(89H / 8AH)走的就是 /ptz/extended 这条通道,只是前端包了一层语义。
    controlPtzScan: vi.fn(),
    // 雨刷(A.3.7 表 A.11)有自己的一条路由:后端固定编号 1,前端只发 on/off。
    controlPtzWiper: vi.fn(),
    createPtzPreset: vi.fn(),
    callPtzPreset: vi.fn(),
    deletePtzPreset: vi.fn(),
    controlPtzCruise: vi.fn(),
    createCruiseTrack: vi.fn(),
    controlDevice: vi.fn(),
    createDeviceSnapshotSession: vi.fn(),
    getDeviceSnapshotSession: vi.fn(),
    // 目标跟踪(A.2.3.1.14)。默认回"平台还没下发过"(intent=null) —— 这是合法状态，
    // 不是错误；需要"下发过"的用例自己 mockResolvedValueOnce 覆盖。
    // ⛔ `deviceAcknowledged` / `responseRequired` 恒 false（无应答命令），
    //    mock 里也必须照这个给，否则用例会去验证一个真实环境里不存在的字段。
    getChannelTargetTrack: vi.fn().mockResolvedValue({
      code: 0,
      message: "",
      data: {
        intent: null,
        deviceAcknowledged: false,
        responseRequired: false,
        windowHint: "",
        capability: { state: "unknown", reason: "设备未上报该能力" },
        targetCode: "0411212755"
      }
    }),
    setChannelTargetTrack: vi.fn(),
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
    name: "PlayWindowStub",
    props: ["url", "zlmWebrtc", "hasAudio"],
    emits: ["error", "videosize", "first-frame", "player-error"],
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

/**
 * 拉框变焦的坐标基准桩。
 *
 * ⛔ `dragZoomPlaybackRect` 优先取**播放器那个 `.play-window`**、取不到才退回拖框层自己，
 *    所以两块都要给：只桩拖框层的话，命令里的 `length/width` 会来自另一套数字。
 */
function stubDragZoomRects(wrapper: VueWrapper, width = 800, height = 450) {
  const rect = { x: 0, y: 0, left: 0, top: 0, right: width, bottom: height, width, height, toJSON: () => ({}) } as DOMRect;
  const layer = wrapper.get("[data-testid='drag-zoom-layer']");
  vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue(rect);
  vi.spyOn(wrapper.get("[data-testid='play-window']").element, "getBoundingClientRect").mockReturnValue(rect);
  return layer;
}

/** 在**云台侧栏**点开拉框，并备好坐标基准。`action` 用 `in` / `out`。 */
async function enterDragZoomAtPtzTab(wrapper: VueWrapper, action: "in" | "out" = "in") {
  await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
  await wrapper.get("[data-testid='ptz-mode-speed']").trigger("click");
  await wrapper.get(`[data-testid='ptz-drag-zoom-${action}']`).trigger("click");
  return stubDragZoomRects(wrapper);
}

/**
 * 完成一次"按下 → 拖 → 松手"。
 *
 * `pointerId` 每刀都要换：同一个 id 再按下的语义是"同一次手势的第二个按下"，
 * 而被测代码按 `pointerId` 认人（`dragZoomPointerId`）。
 */
async function dragZoomOnce(wrapper: VueWrapper, from: [number, number], to: [number, number], pointerId: number) {
  const layer = wrapper.get("[data-testid='drag-zoom-layer']");
  await layer.trigger("pointerdown", { clientX: from[0], clientY: from[1], pointerId, button: 0 });
  await layer.trigger("pointermove", { clientX: to[0], clientY: to[1], pointerId });
  await layer.trigger("pointerup", { clientX: to[0], clientY: to[1], pointerId });
  await flushPromises();
}

/** 只数拉框变焦那两条命令：控制台挂载时可能还有别的 `controlDevice` 流量。 */
function dragZoomCalls(withAction: "drag_zoom_in" | "drag_zoom_out" = "drag_zoom_in") {
  // ⛔ 别把参数标注成元组(`[unknown, {...}]`)：`mock.calls` 是 `any[][]`，
  //    元组参数对不上 `filter` 的重载(TS2769)。按下标取最稳。
  return api.controlDevice.mock.calls.filter((call: any[]) => call[1]?.action === withAction);
}

/**
 * 目标跟踪读接口的应答（GB/T 28181-2022 A.2.3.1.14）。
 *
 * ⛔ `deviceAcknowledged` / `responseRequired` **恒 false**：它是无应答命令
 *    （9.3.1 d) + 表 1 序号 13）。桩里也必须照这个给 —— 给 true 的话用例会去验证
 *    一个真实环境里永远不存在的字段。
 */
function targetTrackResponse(intent: Record<string, unknown> | null = null) {
  return {
    code: 0,
    message: "",
    data: {
      intent,
      deviceAcknowledged: false,
      responseRequired: false,
      windowHint: "area 六项必须同一坐标系：length/width 是画面实际渲染的像素尺寸",
      capability: { state: "unknown", reason: "设备未上报该能力" },
      targetCode: channel.channelId
    }
  };
}

/** 一条"平台已下发手动跟踪"的意图（六项框选坐标都在）。 */
function targetTrackIntent(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    deviceId: 1,
    channelId: channel.id,
    targetCode: channel.channelId,
    mode: "Manual",
    deviceId2: "",
    areaLength: 800,
    areaWidth: 450,
    areaMidPointX: 400,
    areaMidPointY: 200,
    areaLengthX: 200,
    areaLengthY: 150,
    sourceOperationSeq: 3,
    sourceSn: 9,
    sourceOperationId: "op-tt-1",
    commandedBy: 1,
    commandedByDeptId: 1,
    commandedAt: "2026-09-21T02:00:00Z",
    createdAt: "2026-09-21T02:00:00Z",
    updatedAt: "2026-09-21T02:00:00Z",
    ...overrides
  };
}

/** 框选坐标基准桩（与拉框变焦同一套：优先取播放器那个 `.play-window`）。 */
function stubTargetTrackRects(wrapper: VueWrapper, width = 800, height = 450) {
  const rect = { x: 0, y: 0, left: 0, top: 0, right: width, bottom: height, width, height, toJSON: () => ({}) } as DOMRect;
  const layer = wrapper.get("[data-testid='target-track-layer']");
  vi.spyOn(layer.element, "getBoundingClientRect").mockReturnValue(rect);
  vi.spyOn(wrapper.get("[data-testid='play-window']").element, "getBoundingClientRect").mockReturnValue(rect);
  return layer;
}

/** 在**云台侧栏**点开「框选跟踪」，并备好坐标基准。 */
async function enterTargetTrackDraw(wrapper: VueWrapper) {
  await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
  await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
  await wrapper.get("[data-testid='ptz-target-track-manual']").trigger("click");
  return stubTargetTrackRects(wrapper);
}

/** 完成一次"按下 → 拖 → 松手"。`pointerId` 每刀都要换（被测代码按它认人）。 */
async function targetTrackDragOnce(wrapper: VueWrapper, from: [number, number], to: [number, number], pointerId: number) {
  const layer = wrapper.get("[data-testid='target-track-layer']");
  await layer.trigger("pointerdown", { clientX: from[0], clientY: from[1], pointerId, button: 0 });
  await layer.trigger("pointermove", { clientX: to[0], clientY: to[1], pointerId });
  await layer.trigger("pointerup", { clientX: to[0], clientY: to[1], pointerId });
  await flushPromises();
}

/**
 * 派发一次 Esc。
 *
 * ⛔ 必须从 `document.documentElement` 派发**并冒泡**：Arco 的 `esc-to-close` 就挂在
 *    `document.documentElement` 的 keydown 上，只往 `window` 派发等于跳过了它 ——
 *    而"按 Esc 把控制台整个关掉"恰恰是它干的（与我们的 `window` 监听是**并行**的两份，
 *    不是谁冒泡到谁）。这样派发一次，两条路径都能被覆盖到。
 */
function pressEscape() {
  document.documentElement.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
}

describe("PlayConsoleLinked 双区联动", () => {
  beforeEach(() => {
    userState.account = reactive({ permissions: ["*:*:*"] });
    api.authorizeFixedPlayback.mockReset();
    api.reportPlaybackClientEvent.mockReset();
    api.reportPlaybackClientEvent.mockResolvedValue({ code: 0, message: "", data: { accepted: true } });
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
    // 目标跟踪：默认"平台还没下发过"。⛔ 一定要在 beforeEach 里重置 ——
    // 它是面板打开时自动读的，残留上一条用例的下发结果会让断言看起来"通过了"却什么也没验证。
    api.getChannelTargetTrack.mockReset();
    api.getChannelTargetTrack.mockResolvedValue(targetTrackResponse());
    api.setChannelTargetTrack.mockReset();
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

  it("上报当前播放会话的客户端事实且失败不影响播放", async () => {
    api.startPlay.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        lifecycleId: "life-1",
        clientFeedbackToken: "feedback-1",
        clientFeedbackExpiresAt: 1800000600,
        streamId: "stream-1",
        ssrc: "0102030405",
        app: "rtp",
        wsflvUrl: "ws://zlm/rtp/stream-1.live.flv",
        httpFlvUrl: "",
        hlsUrl: "",
        expireAt: 0
      }
    });
    api.reportPlaybackClientEvent.mockRejectedValueOnce(new Error("network unavailable"));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    wrapper.findComponent({ name: "PlayWindowStub" }).vm.$emit("first-frame", {
      event: "first_frame",
      clientElapsedMs: 321
    });
    await flushPromises();

    expect(api.reportPlaybackClientEvent).toHaveBeenCalledWith("life-1", "feedback-1", {
      event: "first_frame",
      clientElapsedMs: 321
    });
    expect(wrapper.find(".placeholder.error").exists()).toBe(false);
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

      const source = readFileSync(
        resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleProtocolBar.vue"),
        "utf8"
      );
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

  it("「画面设置」侧栏直接嵌入设备配置工作区，并随通道切换上下文", async () => {
    vi.useFakeTimers();
    const wrapper = mount(PlayConsoleLinked, {
      props: { visible: true, channel }
    });

    await vi.advanceTimersByTimeAsync(1500);
    await flushPromises();

    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    const workspace = wrapper.get("[data-testid='linked-side-deviceconfig'] .dcg-window--embedded");
    expect(workspace.attributes("aria-label")).toContain(channel.name);
    // 侧栏只剩「视频编码」一组 ⇒ 分组导航整体不渲染（`configGroups.length > 1` 才出），
    // 组的标题那一行由 `dcg-params-head` 承担。图像叠加在底栏，见 `picture-osd-cell`。
    expect(workspace.find(".dcg-nav").exists()).toBe(false);
    expect(
      wrapper.get("[data-testid='linked-side-video-compare']").find("[data-testid='video-param-compare-card']").exists()
    ).toBe(true);
    expect(wrapper.get("[data-testid='linked-detail-picture']").find("[data-testid='video-param-compare-card']").exists()).toBe(
      false
    );
    expect(wrapper.get("[data-testid='picture-osd-cell']").find("[data-testid='osd-block-time']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='play-console-open-device-config']").exists()).toBe(false);

    await wrapper.setProps({ channel: { ...channel, id: 999, channelId: "34020000001320000099", name: "园区西门" } });
    await flushPromises();
    expect(wrapper.get("[data-testid='linked-side-deviceconfig'] .dcg-window--embedded").attributes("aria-label")).toContain(
      "园区西门"
    );
  });

  it("侧栏与详情条按 tab 分工，云台/探针/视频参数各司其职", async () => {
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

    // ⛔ 2026-09-20：「高级」页签**整体退役**，四拨内容各有归属，控制台里一处都不留。
    //    别把它加回来，也别把其中任何一块挪回侧栏/详情条：
    //      请求关键帧 → 云台控制侧栏（流侧、立即）  ·  设备录制 / 布撤防 / 报警复位 → 设备详情抽屉「设备控制」
    //      图像抓拍配置 → 设备详情抽屉  ·  视频参数 → 早已是独立 tab
    expect(wrapper.find("[data-testid='linked-tab-advanced']").exists()).toBe(false);
    // 三拨搬走的内容，一个都不许在控制台的**渲染结果**里留下痕迹 ——
    // 只删页签却把卡片留在别的页签下，是这个改动最容易出的错。
    expect(wrapper.text()).not.toContain("开始设备端录制");
    expect(wrapper.text()).not.toContain("报警复位");
    expect(wrapper.text()).not.toContain("图像抓拍配置");
    // 通道级事实（DeviceStatus / 存储卡）只在设备管理页的「设备详情」抽屉里，
    // 控制台侧栏与详情区都不保留入口。
    expect(wrapper.find("[data-testid='linked-detail-tabs']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='storage-card-status']").exists()).toBe(false);

    // 「画面控制」卡（3D 放大/缩小）与请求关键帧的新家都在**云台控制侧栏** ——
    // 前者是画面级手势，后者是流侧动作，都跟"设备侧录制/布防"不同族，
    // 也不该只在别的页签下才找得到（那样切页签就会失去取消入口）。
    await wrapper.get("[data-testid='ptz-mode-speed']").trigger("click");
    const ptzSideForDragZoom = wrapper.get("[data-testid='linked-side-ptz']");
    expect(ptzSideForDragZoom.text()).toContain("3D 拖拽");
    expect(ptzSideForDragZoom.find("[data-testid='ptz-drag-zoom-in']").exists()).toBe(true);
    expect(ptzSideForDragZoom.find("[data-testid='ptz-drag-zoom-out']").exists()).toBe(true);
    expect(ptzSideForDragZoom.find(".ptz-precise").attributes("style")).toContain("display: none");
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    expect(ptzSideForDragZoom.find("[data-testid='ptz-iframe-request']").isVisible()).toBe(true);
    expect(ptzSideForDragZoom.text()).toContain("关键帧");

    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    const vpSide = wrapper.get("[data-testid='linked-side-deviceconfig']");
    const vpDetail = wrapper.get("[data-testid='linked-detail-picture']");
    // 侧栏留编辑表单与状态文案（只有「视频编码」这一组），参数对照紧跟在其下方。
    expect(vpSide.text()).toContain("视频编码");
    expect(vpSide.text()).not.toContain("图像叠加");
    expect(vpSide.text()).not.toContain("设备控制");
    expect(vpSide.text()).not.toContain("图像抓拍配置");
    expect(vpSide.find("[data-testid='linked-side-video-compare']").exists()).toBe(true);
    expect(vpSide.find("[data-testid='video-param-compare-card']").exists()).toBe(true);
    expect(vpDetail.find("[data-testid='video-param-compare-card']").exists()).toBe(false);
    // 底栏由图像叠加、遮挡、镜像三个组件组成；图像叠加占两列，内部两块与另外两张卡等分。
    expect(vpDetail.findAll(".linked-picture-layout > .linked-section")).toHaveLength(3);
    expect(vpDetail.get("[data-testid='picture-osd-cell']").find("[data-testid='osd-block-time']").exists()).toBe(true);

    wrapper.unmount();
  });

  it("视频参数：两行对照紧跟视频编码渲染，底栏只保留画面卡片", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({
        list: [videoParamRow({ id: 1, streamNumber: 0, resolution: "6" })],
        freshness: "fresh",
        reconcile: { state: "read_ok", operationId: "vp-op-0", status: "accepted", responseHasData: true }
      })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");

    const side = wrapper.get("[data-testid='linked-side-deviceconfig']");
    const compare = side.get("[data-testid='video-param-compare']");
    // 两行都在视频编码下方 —— 这是"设备说的 vs 画面在播的"同屏落点。
    expect(compare.text()).toContain("设备回读");
    expect(compare.text()).toContain("画面实测");
    // ⛔ 「回读」行取**设备事实**（码值 6 → 1080P），不是草稿值。
    expect(side.get("[data-testid='video-param-compare-read']").text()).toContain("1080P");

    // 底栏不再重复参数对照，避免视频编码下方和底栏各出现一份。
    const detail = wrapper.get("[data-testid='linked-detail-picture']");
    expect(detail.find("[data-testid='video-param-compare-card']").exists()).toBe(false);
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

  it("一级页签由独立组件承载，详情坞横跨主体底部", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const probePanelSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleProbePanel.vue"),
      "utf8"
    );
    const tabsSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleTabs.vue"),
      "utf8"
    );
    const ptzPanelSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsolePtzPanel.vue"),
      "utf8"
    );
    const detailStyles = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"),
      "utf8"
    );
    const dialogsSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleDialogs.vue"),
      "utf8"
    );

    expect(source).toContain("<PlayConsoleTabs");
    expect(source).not.toContain('aria-label="播放工作区"');
    expect(tabsSource).toContain('aria-label="播放工作区"');
    expect(source).toContain(':width="playbackModalWidth"');
    expect(source).toContain(': "min(1520px, calc(100vw - 32px), calc((100dvh - 380px) * 16 / 9 + 410px))"');
    // 两列 = 画面 + 属性栏。2026-09-21 一级页签回到属性栏顶部后，
    // 原来给左侧竖排导航的 `136px` 那一列退役（宽度还给画面）。
    expect(source).toMatch(/\.console-body\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+360px/s);
    // ⭐ 页签归属用「顺序」判据：`aria-label="播放工作区"` 必须落在 `class="sidebar"` 之后。
    // ⛔ 别退化成"存在性"断言 —— 页签被搬回 `console-body` 当独立一列时它照样"存在"，
    //    而那正是列索引集体错位的形态（`.video-frame` / `.linked-info-bar` / `.sidebar` 全要 -1）。
    const sidebarAt = source.indexOf('class="sidebar"');
    const tabsAt = source.indexOf("<PlayConsoleTabs");
    expect(sidebarAt).toBeGreaterThan(-1);
    expect(tabsAt).toBeGreaterThan(sidebarAt);
    // ⛔ 左侧竖排那套样式（`.workbench-nav`）与「当前页名」标题（`.workspace-heading`）
    //    必须连**规则**一起消失：留着就是"页签在哪一列"的第二个答案。
    expect(source).not.toMatch(/\.workbench-nav\s*[,{]/);
    expect(source).not.toMatch(/\.workspace-heading\s*[,{]/);
    // 属性栏吃满第 2 列（不留一条空列）
    expect(source).toMatch(/\.sidebar\s*\{[^}]*grid-column:\s*2;/s);
    // 流信息 tab 已并入探针 tab,原来的 sidebar-stream / linked-detail-stream / linked-stream-metrics
    // 全都退出历史舞台
    expect(source).not.toContain("sidebar-stream");
    expect(source).not.toContain('data-testid="linked-detail-stream"');
    expect(source).not.toContain(".linked-stream-metrics");
    expect(source).not.toContain("phase === 'playing' && activeTab !== 'stream'");
    expect(detailStyles).toMatch(/\.linked-card\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(detailStyles).toMatch(/\.preset-tile-more\s*\{[^}]*box-sizing:\s*border-box/s);
    expect(ptzPanelSource).toMatch(
      // ⛔ 媒体查询两种写法都要接受：stylelint 的 media-feature-range-notation 会把
      //    `(max-width: 720px)` 自动 fix 成 `(width <= 720px)`（提交钩子会跑 --fix），
      //    只认一种写法的话，样式没改、只是被格式化过也会红。
      /@media \((?:max-width:\s*960px|width <= 960px)\)[\s\S]*?\.linked-ptz-layout\s*\{[^}]*grid-template-rows:\s*none;[^}]*grid-template-columns:\s*1fr;[^}]*height:\s*auto/s
    );
    expect(detailStyles).toMatch(/\.home-card-actions\s*\{[^}]*display:\s*flex/s);
    expect(dialogsSource).toMatch(/\.home-settings-form\s*\{[^}]*display:\s*grid/s);
    // 探针详情条三栏不等分:时间线是横向柱状图,等分会把 32 根柱子挤到每根不足 9px
    expect(probePanelSource).toMatch(
      /\.linked-probe-layout\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+minmax\(0,\s*1fr\)\s+minmax\(0,\s*1\.4fr\)/s
    );
    // ⛔ `[^{]*` 而不是 `\s*`：这条规则可能与 `.sidebar-deviceconfig .panels` **并列成一条**
    //   （`.a .panels,\n.b .panels {`），语义没变但选择器后面不再紧跟着 `{`。
    expect(source).toMatch(/\.sidebar-probe\s+\.panels[^{]*\{[^}]*background:\s*transparent/s);
    // ⛔ 2026-09-20：「高级」页签退役，它的视觉契约（sidebar-advanced / linked-advanced-layout /
    //    adv-btn 族）必须**整体消失**，不能只剩一堆没人用的死样式挂在文件末尾 ——
    //    死 CSS 会被后来的人当成"还有这个面板"的证据，也会在改配色时被"顺手同步"。
    expect(source).not.toContain("sidebar-advanced");
    expect(source).not.toContain("linked-advanced-layout");
    expect(source).not.toContain("linked-detail-advanced");
    expect(source).not.toContain("adv-btn");
    expect(source).not.toContain("adv-actions");
  });

  it("三个详情页共享固定工作区高度，窄屏只滚动内容不改变弹窗高度", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const workspaceSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleDetailWorkspace.vue"),
      "utf8"
    );

    expect(source).toContain("<PlayConsoleDetailWorkspace");
    expect(source).toContain("container-type: inline-size;");
    expect(source).toMatch(
      /\.console-body:not\(\.is-minimized\) \.sidebar\s*\{[^}]*height:\s*calc\(\(100cqw - 376px\) \* 9 \/ 16 \+ var\(--play-console-protocol-height\) \+ 2px\)/s
    );
    expect(source).toMatch(/\.stage\s*\{[^}]*display:\s*contents/s);
    expect(workspaceSource).toContain("--play-console-detail-height: 180px");
    expect(workspaceSource).toContain('class="linked-info-bar"');
    expect(workspaceSource).not.toContain('class="stream-info-bar linked-info-bar"');
    expect(workspaceSource).toMatch(
      /\.linked-info-bar\s*\{[^}]*box-sizing:\s*border-box;[^}]*display:\s*grid;[^}]*height:\s*var\(--play-console-detail-height\)/s
    );
    expect(workspaceSource).toMatch(/\.linked-info-bar\s*\{[^}]*grid-column:\s*1\s*\/\s*-1;/s);
    expect(workspaceSource).toMatch(/\.linked-detail\s*\{[^}]*height:\s*var\(--play-console-detail-height\)/s);
    expect(workspaceSource).toMatch(/\.linked-detail\s*\{[^}]*overflow:\s*auto/s);
    expect(workspaceSource).not.toMatch(/@media[\s\S]*?\.linked-detail(?:-[\w-]+)?\s*\{[^}]*height:\s*auto/s);
    expect(readFileSync(resolve(process.cwd(), "src/views/gb28181/components/play-console/detail-cards.scss"), "utf8")).toMatch(
      /@media \((?:max-width:\s*720px|width <= 720px)\)[\s\S]*?\.linked-card,\s*\.picture-osd-cell,\s*\.linked-probe-layout > \.linked-section\.probe-detail-card\s*\{[^}]*box-sizing:\s*border-box;[^}]*min-height:\s*180px/s
    );
    expect(source).not.toContain("--linked-detail-height:");
    expect(source).not.toMatch(/\.linked-detail(?:-[\w-]+)?\s*\{[^}]*height:\s*auto/s);
  });

  it("详情区按云台、画面、探针三个职责组件挂载", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");

    expect(source).toContain("<PlayConsolePtzPanel");
    expect(source).toContain("<PlayConsolePicturePanel");
    expect(source).toContain("<PlayConsoleProbePanel");
    expect(source).not.toContain('class="linked-section probe-detail-card"');
    expect(source).not.toContain('data-testid="probe-timeline-open"');
    expect(source).not.toMatch(/class=\"linked-detail linked-detail-ptz\"/);
    expect(source).not.toMatch(/class=\"linked-detail linked-detail-actions\"/);
    expect(source).not.toMatch(/class=\"linked-detail\"\s*data-testid=\"linked-detail-probe\"/);
  });

  it("预置位和巡航完整模板归属各自卡片，父组件只负责挂载与回调", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const presetCard = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PtzPresetCard.vue"),
      "utf8"
    );
    const cruiseCard = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PtzCruiseCard.vue"),
      "utf8"
    );

    expect(source).toContain("<PtzPresetCard");
    expect(source).toContain("<PtzCruiseCard");
    expect(source).not.toContain('data-testid="preset-popover"');
    expect(source).not.toContain('data-testid="cruise-popover"');
    expect(presetCard).toContain('data-testid="preset-popover"');
    expect(cruiseCard).toContain('data-testid="cruise-popover"');
  });

  it("拆分后的三个面板自带布局样式，不依赖父组件 scoped CSS", () => {
    const base = resolve(process.cwd(), "src/views/gb28181/components/play-console");
    const ptz = readFileSync(resolve(base, "PlayConsolePtzPanel.vue"), "utf8");
    const picture = readFileSync(resolve(base, "PlayConsolePicturePanel.vue"), "utf8");
    const probe = readFileSync(resolve(base, "PlayConsoleProbePanel.vue"), "utf8");

    expect(ptz).toMatch(/\.linked-detail-ptz\s*\{[^}]*height:\s*var\(--play-console-detail-height/s);
    expect(ptz).toContain("grid-template-columns: repeat(4, minmax(0, 1fr))");
    expect(picture).toMatch(/\.linked-picture-layout\s*\{[^}]*grid-template-columns:\s*repeat\(4, minmax\(0, 1fr\)\)/s);
    expect(picture).toMatch(/\.linked-picture-layout\s*>\s*\.picture-osd-cell\s*\{[^}]*grid-column:\s*span 2/s);
    expect(probe).toContain("grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.4fr)");
  });

  it("视频编码侧栏与参数对照上下分区，底部画面卡片不再预留对照列", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(source).toMatch(
      /\.sidebar-deviceconfig-panel\s*\{[^}]*display:\s*grid;[^}]*grid-template-rows:\s*auto auto;[^}]*align-content:\s*start/s
    );
    expect(source).toMatch(/\.sidebar-deviceconfig-panel :deep\(\.dcg-window--embedded\)\s*\{[^}]*height:\s*auto/s);
    expect(source).toMatch(
      /\.sidebar-video-compare-card :deep\(\.vpc-grid\)\s*\{[^}]*flex:\s*0 0 auto;[^}]*overflow:\s*visible/s
    );
    expect(source).toContain('data-testid="linked-side-video-compare"');
  });

  it("详情卡样式归属子模块，父组件不再持有已拆组件的专属样式", () => {
    const base = resolve(process.cwd(), "src/views/gb28181/components/play-console");
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const workspace = readFileSync(resolve(base, "PlayConsoleDetailWorkspace.vue"), "utf8");
    const detailStyles = readFileSync(resolve(base, "detail-cards.scss"), "utf8");

    expect(workspace).toContain('<style lang="scss" src="./detail-cards.scss"></style>');
    for (const selector of [
      ".linked-card",
      ".preset-popover",
      ".home-config",
      ".scan-panel",
      ".probe-detail-card",
      ".mask-slot-grid",
      ".mirror-choice-grid",
      ".vpc-grid"
    ]) {
      expect(detailStyles).toContain(`${selector} {`);
      expect(source).not.toMatch(new RegExp(`^\\s*\\${selector.replaceAll(".", "\\.")}\\s*\\{`, "m"));
    }
  });

  it("检测按钮与时长选择器按 7:3 分配宽度", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleProbeSidebar.vue"),
      "utf8"
    );

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

    // 请求关键帧 2026-09-20 起在云台控制侧栏（默认页签），它是控制台里仅剩的"设备控制"类入口。
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    const iFrame = wrapper.get("[data-testid='ptz-iframe-request']");
    expect(iFrame.attributes("disabled")).toBeUndefined();
    expect(iFrame.attributes("title")).toContain("设备上报不支持");
    expect(iFrame.attributes("title")).toContain("仍可尝试");
    await iFrame.trigger("click");
    await flushPromises();
    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "iframe" }));

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
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");

    // 打开面板只读平台缓存，**不发 SIP 报文**（refresh=false）。
    expect(api.getChannelVideoParams).toHaveBeenCalledWith(channel.id, false);
    expect(wrapper.get("[data-testid='dcg-reconcile']").text()).toContain("回读成功");

    // 「按几段码流渲染」的出处是目录 <Info> 的 StreamNumberList，不是"我们看到几行"。
    // ⛔ 但那行只写在**抽屉底部 statusbar** 里，而 statusbar 是 `v-if="!embedded"` ——
    //    控制台这里是**嵌入形态**，根本不渲染 statusbar。嵌入形态能验证的是参数区汇总条：
    //    本组项数 × 当前配置文件下可见的码流数。
    expect(wrapper.get("[data-testid='dcg-params-foot']").text()).toContain("本组 5 项 × 1 路码流");

    // ⛔ 控件绑定标准码值，人读串只存在于 option 文案。
    //    ⛔⛔ 取 `modelvalue` attribute 而不是 `element.value`：测试环境里 `a-select` 的 stub
    //       渲染成裸 `<select><slot /></select>`，**不透传 model-value**，鸭子的
    //       `option` 也不带 value ⇒ `.value` 永远落在第一项，会把"控件没绑到回读值"
    //       这种**假象**当成组件缺陷。attribute 才是组件真实接到的东西。
    expect(wrapper.get("[data-testid='dcg-format-0']").attributes("model-value")).toBe("2");
    expect(wrapper.get("[data-testid='dcg-resolution-0']").attributes("model-value")).toBe("6");
    // ⛔ 用 find 而不是 get：get() 找不到会直接抛，返回类型里根本没有 exists（恒真），
    //    想断言「这行在不在」必须走 find().exists()。
    expect(wrapper.find("[data-testid='dcg-row-encoding-format']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-row-encoding-resolution']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='dcg-stream-0']").findAll("[data-source='设备']")).toHaveLength(0);
    expect(wrapper.get("[data-testid='dcg-params-foot']").text()).not.toContain("设备");

    // 配置文件切到子码流后，VBR 码率明确标为“不发”。
    await wrapper.get("[aria-label='配置文件']").setValue("1");
    await nextTick();
    expect(wrapper.get("[data-testid='dcg-stream-1'] [data-source='不发']").text()).toBe("不发");

    // 对照区的「回读」行取**当前选中那一路码流**的设备事实：切到子码流后跟着变成它的 720P；
    // 关键是草稿没动过它（下方 mismatch 用例另有锁定）。
    expect(wrapper.get("[data-testid='video-param-compare-read']").text()).toContain("720P");
    wrapper.unmount();
  });

  it("视频参数面板切到 VBR 时禁用码率格", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({ list: [videoParamRow()], freshness: "fresh", reconcile: { state: "read_ok" } })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");

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
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");

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
    // ⛔ type_absent 在**嵌入形态**是短文案（侧栏一行放不下完整句，“可判为不支持
    //    VideoParamAttribute”那半句只在抽屉形态出现）⇒ 别把完整句钉在这里。
    expect(reconcile.text()).toContain("设备未返回该配置");
    // ⛔ type_absent 是"一种结论"而不是失败：设备回了 OK 却没带该元素
    // （2016 设备与未实现该类型的厂商都是这个形态）→ 黄色提示，不是红色报错。
    expect(reconcile.classes()).toContain("is-warn");
    expect(reconcile.classes()).not.toContain("is-error");
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
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    await flushPromises();

    // ⛔ 版本提示那条**只在抽屉形态渲染**（`v-if="versionNotice && !embedded"`）：
    //    控制台这里是嵌入形态，宿主没留这条额外提示的空间。所以这里能兜住的是契约的
    //    「不改可用性」那一半；「措辞」那一半由 DeviceConfigDrawer.test.ts 的非嵌入用例兜。
    expect(wrapper.find("[data-testid='dcg-version-notice']").exists()).toBe(false);
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
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
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
    expect(wrapper.get("[data-testid='dcg-resolution-0']").attributes("model-value")).toBe("6");
    expect(wrapper.get("[data-testid='dcg-reset']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("设备离线时视频参数面板禁用读写并说明原因", async () => {
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({ list: [videoParamRow()], freshness: "fresh", reconcile: { state: "read_ok" } })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel: { ...channel, status: 0 } } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");

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
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");

    // 先制造一格"脏草稿"，再切通道 —— 新通道的值不能被旧草稿遮住。
    await wrapper.get("[data-testid='dcg-resolution-0']").setValue("5");
    await nextTick();
    expect(wrapper.get("[data-testid='dcg-stream-0']").text()).toContain("已改");

    const nextChannel = { ...channel, id: 2, channelId: "0411212888", deviceId: "34020000001320000003", name: "园区西门" };
    await wrapper.setProps({ channel: nextChannel });
    await flushPromises();

    expect(api.getChannelVideoParams).toHaveBeenLastCalledWith(2, false);
    expect(wrapper.get("[data-testid='dcg-resolution-0']").attributes("model-value")).toBe("4");
    expect(wrapper.get("[data-testid='dcg-reset']").attributes("disabled")).toBeDefined();
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

  /**
   * ⛔ 2026-09-20：设备录制 / 布撤防 / 报警复位 / 图像抓拍配置 已搬到设备管理页的「设备详情」抽屉。
   *    它们的收敛规则（"200 不是成功"、deadline 缺失与超时、换通道作废在途应答）现在由
   *    `device-mgmt/DeviceControlPanel.test.ts` 与 `device-mgmt/SnapshotConfigPanel.test.ts` 各自钉住 ——
   *    别在这里补回来：控制台已经**没有**这些按钮了。
   *    控制台里只剩 `iframe`（请求关键帧）这一个 DeviceControl 动作，而它是"送达即止"的。
   */
  it("请求关键帧是单向命令：sent 即止，不轮询也不说已生效", async () => {
    vi.useFakeTimers();
    api.controlDevice.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: { operationId: "iframe-op", status: "sent", responseRequired: false, deadlineAt: null }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    await wrapper.get("[data-testid='ptz-iframe-request']").trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(30000);

    expect(api.controlDevice).toHaveBeenCalledWith(channel.id, expect.objectContaining({ action: "iframe" }));
    // 附录 A 里没有关键帧的回读手段 ⇒ 平台本来就不可能知道执行结果，
    // 所以既不轮询 operation，也不许编一个"已生效"的终态出来。
    expect(api.getPtzOperation).not.toHaveBeenCalled();
    expect(wrapper.get("[data-testid='ptz-iframe-request']").attributes("disabled")).toBeUndefined();
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
    // 2026-09-20 起入口在云台控制侧栏的连续控制模式
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-speed']").trigger("click");
    await wrapper.get("[data-testid='ptz-drag-zoom-in']").trigger("click");
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
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-speed']").trigger("click");
    await wrapper.get("[data-testid='ptz-drag-zoom-out']").trigger("click");
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

  it("一次框选下发完仍留在拉框态:可以连着拉第二刀", async () => {
    // ⛔ 旧行为(2026-09-20 前):`finishDragZoom` 末尾把 dragZoomMode 置回 false ——
    //    操作员每拉一刀都得回侧栏重点一次按钮,而"先放大看结果、再决定往哪补一刀"
    //    才是真实用法。现在退出是**显式**的(再点同向按钮 / Esc)。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);
    await dragZoomOnce(wrapper, [200, 100], [600, 300], 11);
    expect(dragZoomCalls()).toHaveLength(1);
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='ptz-drag-zoom-in']").text()).toContain("取消 3D 放大");

    // 第二刀:同一层上直接再来一次,坐标按**当前画面**重新换算
    // （from/to 与第一刀不同 ⇒ 命令里的 region 必须是新的那个框,不是复用上一次的）
    await dragZoomOnce(wrapper, [100, 50], [300, 200], 12);
    expect(dragZoomCalls()).toHaveLength(2);
    expect(api.controlDevice).toHaveBeenLastCalledWith(
      channel.id,
      expect.objectContaining({
        action: "drag_zoom_in",
        region: { length: 800, width: 450, midPointX: 200, midPointY: 125, lengthX: 200, lengthY: 150 }
      })
    );
    wrapper.unmount();
  });

  it("拉框态按 Esc 退出,再点按钮能干净地重新进入", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true);

    pressEscape();
    await nextTick();
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='ptz-drag-zoom-in']").text()).toContain("3D 放大");

    // ⛔ 重进不能留下上一次的拖拽残留(起点 / 指针 id):留着的话下一次 pointerup 会拿旧起点
    //    算出一个"凭空出现"的框。判据 = 拖框层在没按下时**不该有框**。
    await wrapper.get("[data-testid='ptz-drag-zoom-in']").trigger("click");
    expect(wrapper.get("[data-testid='drag-zoom-layer']").find(".drag-zoom-box").attributes("style")).toBeUndefined();
    wrapper.unmount();
  });

  it("拉框态按 Esc 只收拉框态,绝不把控制台整个关掉", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);

    pressEscape();
    await flushPromises();

    // ① 拉框态收掉了 —— 这是我们挂在 `window` 上的监听干的。
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='ptz-drag-zoom-in']").text()).toContain("3D 放大");
    // ② 控制台**不许**跟着关：Arco 另在 `document.documentElement` 上挂了一份全局监听
    //    （`esc-to-close`），它跟我们是**并行**的两份，不是谁冒泡到谁。模式期间那个值必须为假，
    //    否则用户按一下 Esc 就"弹窗直接没了"。
    // ⛔ 这一条在本仓单测里**证明力有限**：Arco 是桩 ⇒ 它那条全局 Esc 路径压根不存在
    //    （这也是它当初没能拦住真机缺陷的原因）。真正有判据的是紧接着那两条"事件流"用例。
    expect(wrapper.emitted("update:visible")).toBeUndefined();
    wrapper.unmount();
  });

  it("拉框态那一下 Esc 必须被图层吃掉,不能让 documentElement 上的监听再收到", async () => {
    // ⛔⛔ 这条锁的是一个**只有真实浏览器才暴露**的缺陷（2026-09-20 实机抓到）：
    //    上一版只在模板上把 `esc-to-close` 在模式期间置假，指望 Arco 那边"自觉不动"。
    //    实际相位（探针实测）：`win-capture` 时 `esc-to-close=false`、图层还在；
    //    我们退出模式后 `escToClose` 在**同一个事件派发内**就变回 `true`，
    //    Arco 挂在 `documentElement` 上的监听**随后**才跑、读到的已经是 `true`
    //    ⇒ 照关不误（调用栈 `requestClose → handleClose → update:visible → consoleStore.close()`），
    //    用户看到的就是"按一下 Esc，播放控制台整个弹窗没了"。
    //    ⇒ 判据只能落在**事件流**上：图层既然接管了，就必须把那一下从事件流里拿掉。
    //    下面这个 `standIn` 就是 Arco 那条监听的替身（同相位：documentElement + 冒泡）。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);

    const seenByStandIn: string[] = [];
    const standIn = (event: KeyboardEvent) => seenByStandIn.push(event.key);
    document.documentElement.addEventListener("keydown", standIn);
    try {
      pressEscape();
      await flushPromises();
    } finally {
      document.documentElement.removeEventListener("keydown", standIn);
    }

    // 拉框态退出了 —— 这是我们自己的监听干的
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(false);
    // ……而且这一下**没有**继续传到 documentElement（否则 Arco 会顺手把控制台关掉）
    expect(seenByStandIn).toEqual([]);
    wrapper.unmount();
  });

  it("弹窗开着时那一下 Esc 必须放行给弹窗,不能被图层私吞", async () => {
    // 反方向钉住"别把 stopPropagation 写成无条件的"：上面有弹窗时那一下归弹窗，
    // 图层若把它吃掉，用户的弹窗就**再也关不掉**了（Ctrl+W 之外没有别的出口）。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);

    await wrapper.get("[data-testid='home-configure']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='home-settings-dialog']").exists()).toBe(true);

    const seenByStandIn: string[] = [];
    const standIn = (event: KeyboardEvent) => seenByStandIn.push(event.key);
    document.documentElement.addEventListener("keydown", standIn);
    try {
      pressEscape();
      await flushPromises();
    } finally {
      document.documentElement.removeEventListener("keydown", standIn);
    }

    expect(seenByStandIn).toEqual(["Escape"]); // 放行了
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true); // 拉框态原地保留
    wrapper.unmount();
  });

  it("拉框态下弹窗开着时,那一下 Esc 归弹窗、不顺手收掉拉框态", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);

    await wrapper.get("[data-testid='home-configure']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='home-settings-dialog']").exists()).toBe(true);

    pressEscape();
    await flushPromises();

    // 拉框态**还在**：那一下 Esc 的第一语义是关掉最上面那个弹窗（它自己也有 `esc-to-close`），
    // 不该顺手把拉框态收掉 —— 否则用户关完弹窗回来，按钮已经变回「3D 放大」，摆好的下一刀白拖。
    // ⛔ 弹窗**关不关**不在这里断言：那是 Arco `isLastDialog()` 的事，而在 jsdom 里弹窗实例会跨用例
    //    残留（卸载时 `visible` 仍为真、z-index 记账不退），它压根不可靠；本用例只钉我们自己的规则。
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='ptz-drag-zoom-in']").text()).toContain("取消 3D 放大");
    wrapper.unmount();
  });

  it("Esc 的归属闸门跟着「画面上拖」的模式走,不然那一下 Esc 会把控制台整个关掉", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    // ⛔ `a-modal` 在测试里是**桩**（`src/test/setup.ts` 统一替换），拿不到组件 props，
    //    只能从属性上看这个开关 —— 同文件里 `mouse-enter-delay` / `content` 那几条也是这个看法。
    const escToClose = () => wrapper.attributes("esc-to-close");

    // 平时：窗口归 Arco，控制台照旧能按 Esc 关（别一竿子挡死）
    expect(escToClose()).toBe("true");
    // 拉框态：窗口归图层，`a-modal` 那一侧必须让位
    await enterDragZoomAtPtzTab(wrapper);
    expect(escToClose()).toBe("false");
    // 退出后交还
    pressEscape();
    await flushPromises();
    expect(escToClose()).toBe("true");
    wrapper.unmount();
  });

  it("上一次框选还在下发中时拒收新的按下,并如实说在等", async () => {
    // ⛔ `runAdvancedAction` 对同 action 的并发请求是**静默丢弃**的。拉框态现在跨多刀常驻,
    //    不在这里挡住的话,用户会画出一个跟着消失、命令却没有的框 —— 比"画不出来"糟得多。
    // ⛔ 初值给一个空函数而不是 `null`：赋值发生在 Promise 执行器里，TS 的流程分析
    //    看不见"它其实会被赋上"，会把 `release` 收窄成 `null` ⇒ `release?.()` 报 TS2349。
    let release: (value: unknown) => void = () => undefined;
    api.controlDevice.mockImplementationOnce(
      () =>
        new Promise(resolve => {
          release = resolve;
        })
    );
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    const layer = await enterDragZoomAtPtzTab(wrapper);
    await dragZoomOnce(wrapper, [200, 100], [600, 300], 21);
    expect(dragZoomCalls()).toHaveLength(1);
    // 提示条换成"正在下发",否则用户看不到任何"为什么拖不动"的解释
    expect(layer.text()).toContain("正在下发");
    expect(layer.classes()).toContain("is-busy");

    await layer.trigger("pointerdown", { clientX: 100, clientY: 50, pointerId: 22, button: 0 });
    await layer.trigger("pointermove", { clientX: 300, clientY: 200, pointerId: 22 });
    // 判据在画面上:按下之后**不该出现框**(出现即"画了框却不下发")
    expect(layer.find(".drag-zoom-box").attributes("style")).toBeUndefined();
    await layer.trigger("pointerup", { clientX: 300, clientY: 200, pointerId: 22 });
    await flushPromises();
    expect(dragZoomCalls()).toHaveLength(1);

    release({ code: 0, message: "", data: { operationId: "dz-1", status: "accepted" } });
    await flushPromises();
    // 下发完了就恢复成可拖(否则"等下"会变成"卡死")
    expect(layer.text()).toContain("可连续框选");
    wrapper.unmount();
  });

  it("控制权被收回时退出拉框态:不留一层吃事件的膜却没有出口", async () => {
    // ⛔ 出口按钮挂在 `canControlDevice` 上、侧栏整块也随权限消失。真机上等价于设备转离线、
    //    或换到一个没有 device:control 的通道 —— 那时画面被膜盖住而没有任何退出入口。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterDragZoomAtPtzTab(wrapper);
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true);

    userState.account.permissions = ["gb28181:ptz:view"];
    await nextTick();
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(false);
    wrapper.unmount();
  });

  it('拉框态的退出只有一处实现,且三个"画面上拖"的模式互为死锁', () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    // ⛔ 散落的「把 dragZoomMode 置回 false」早晚会漏掉拖拽残留(起点 / 指针 id),
    //    表现为"下一次进拉框态画出个凭空出现的框"。所以只许 `exitDragZoomMode` 里写一次。
    expect(source.match(/dragZoomMode\.value = false/g) ?? []).toHaveLength(1);
    expect(source).toMatch(/function exitDragZoomMode\(\) \{\s*dragZoomMode\.value = false;/);
    // 拉框 / 遮挡框选 / OSD 调位置 —— 同一个按下动作在两层里各有一套解释,三者必须互斥:
    // 进任一个都关掉另两个。
    // ⛔ 判据按**函数体**取,不按全文字符串计数:同一句话在别的流程里也会出现
    //    (切页签的 watch、`toggleOsdEditMode`、Esc 处理器),数总数会把它们算进来。
    const bodyOf = (name: string) => {
      const m = source.match(new RegExp(`function ${name}\\([^)]*\\)[^{]*\\{([\\s\\S]*?)\\n\\}`));
      if (!m) throw new Error(`没找到 ${name} 的函数体`);
      return m[1];
    };
    const dragZoomToggle = bodyOf("toggleDragZoomMode");
    expect(dragZoomToggle).toContain("if (maskDrawMode.value) cancelMaskDraw();");
    expect(dragZoomToggle).toContain("if (osdEditMode.value) exitOsdEditMode();");
    expect(bodyOf("enterOsdEditMode")).toContain("if (dragZoomMode.value) exitDragZoomMode();");
    expect(bodyOf("startMaskDraw")).toContain("if (dragZoomMode.value) exitDragZoomMode();");
  });

  it("3D 拖拽只在连续控制模式展示", async () => {
    // ⛔ 3D 放大/缩小是画面级连续操作，与摇杆、镜头控制同属连续控制。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-speed']").trigger("click");
    await wrapper.get("[data-testid='ptz-drag-zoom-in']").trigger("click");
    expect(wrapper.get("[data-testid='ptz-drag-zoom-in']").text()).toContain("取消 3D 放大");

    const dragZoomBlock = wrapper.get("[data-testid='ptz-drag-zoom']").element as HTMLElement;
    expect(dragZoomBlock.closest(".ptz-speed")).not.toBeNull();
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true);
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    expect(wrapper.find(".ptz-speed").attributes("style")).toContain("display: none");
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='ptz-iframe-request']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("只有云台读权限、没有设备控制权限时不出现 3D 拖拽与关键帧入口", async () => {
    // ⛔ 防回归:动作侧 toggleDragZoomMode / runAdvancedAction 第一句就是 `if (!canControlDevice) return`。
    //    云台面板的可见性门禁 canPtzPanel 只看 ptz:* 权限,不挡就会出现
    //    "看得见按钮却点不动"的死按钮 —— 比看不见更糟。
    userState.account.permissions = ["gb28181:ptz:view"];
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    // 云台侧栏本身在(有 ptz:view),但 3D 拖拽块与请求关键帧都必须缺席
    expect(wrapper.find("[data-testid='linked-side-ptz']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='ptz-drag-zoom']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='ptz-drag-zoom-in']").exists()).toBe(false);
    // 关键帧 2026-09-20 搬进这个侧栏，同样自带 canControlDevice 门禁
    expect(wrapper.find("[data-testid='ptz-iframe']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='ptz-iframe-request']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("控制台搬迁后的布局契约:高级页签不留死样式,云台面板两行分配模式内容", () => {
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsolePtzSidebar.vue"),
      "utf8"
    );
    // ⛔ 「高级」页签 2026-09-20 整体退役：连同它的布局契约（.linked-advanced-layout 的列数）
    //    一起删干净。半截死样式会冒充"这个面板还在"，也会在改配色时被顺手同步。
    expect(source).not.toContain("linked-advanced-layout");
    expect(source).not.toContain("sidebar-advanced");
    // 云台面板是"模式切换 + 当前模式内容"两行；3D 归入连续控制，
    // 关键帧和目标跟踪归入高级控制，不再与外层 grid 行争用空间。
    expect(source).toMatch(/\[data-testid="linked-side-ptz"\]\s*\{[^}]*grid-template-rows:\s*auto\s+minmax\(0,\s*1fr\)/s);
  });

  it("DeviceStatus 与存储卡事实在控制台里读不到:抽屉才是它们的家", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    // ⛔ 2026-09-20：通道级事实（DeviceStatus 的录制/布防状态、存储卡）连同它们的轮询一起
    //    搬去设备管理页的「设备详情」抽屉。控制台里一旦还留着读取入口，就会出现
    //    "同一份事实两处各拉一次、两处各说一套"的老问题。
    expect(source).not.toContain("getDeviceStatus");
    expect(source).not.toContain("getChannelStorageCards");
    expect(source).not.toContain("storage-card-status");
  });

  it("请求关键帧是立即动作:pointercancel 时不下发拖框命令", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    // 3D 拖拽的入口在云台控制侧栏的连续控制模式
    await wrapper.get("[data-testid='ptz-mode-speed']").trigger("click");
    await wrapper.get("[data-testid='ptz-drag-zoom-in']").trigger("click");
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

  it("自动扫描卡片:开始/停止走 89H,运行态只说「已下发」", async () => {
    api.controlPtzScan.mockReset();
    api.controlPtzScan.mockResolvedValue({ code: 0, message: "", data: { operationId: "scan-op-1", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    // 第 4 张卡在云台详情区,默认组号 1
    const card = wrapper.get("[data-testid='scan-card']");
    // 原生 input 的 v-model 落在 DOM property 上,不是 attribute。
    expect((card.get("[data-testid='scan-group-input']").element as HTMLInputElement).value).toBe("1");
    expect(card.get("[data-testid='scan-toggle']").text()).toContain("开始扫描");

    await card.get("[data-testid='scan-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).toHaveBeenCalledWith(channel.id, { action: "scan_start", id: 1 });

    // HTTP 成功 ≠ 设备正在扫描:chip 说的是「启动已下发」
    const chip = wrapper.get("[data-testid='scan-running-chip']");
    expect(chip.text()).toContain("启动已下发");
    expect(wrapper.get("[data-testid='scan-toggle']").text()).toContain("停止扫描");

    // 停止没有专用指令码,复用全零停止帧 —— 前端发的是 scan_stop
    await chip.trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).toHaveBeenLastCalledWith(channel.id, { action: "scan_stop", id: 1 });
    expect(wrapper.find("[data-testid='scan-running-chip']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("扫描边界是「把当前朝向写进去」,速度只在 set_speed 时带 value", async () => {
    api.controlPtzScan.mockReset();
    api.controlPtzScan.mockResolvedValue({ code: 0, message: "", data: { operationId: "scan-op-2", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='scan-group-input']").setValue("3");
    await wrapper.get("[data-testid='scan-set-left']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).toHaveBeenLastCalledWith(channel.id, { action: "scan_set_left", id: 3 });

    await wrapper.get("[data-testid='scan-set-right']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).toHaveBeenLastCalledWith(channel.id, { action: "scan_set_right", id: 3 });

    await wrapper.get("[data-testid='scan-speed-input']").setValue("120");
    await wrapper.get("[data-testid='scan-set-speed']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).toHaveBeenLastCalledWith(channel.id, { action: "scan_set_speed", id: 3, value: 120 });
    wrapper.unmount();
  });

  it("扫描组号/速度越界时不下发,并给出人能读懂的取值范围", async () => {
    api.controlPtzScan.mockReset();
    api.controlPtzScan.mockResolvedValue({ code: 0, message: "", data: { operationId: "scan-op-3", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    // 12 位参数域:与巡航速度同一个量纲(1-4095)
    const speedInput = wrapper.get("[data-testid='scan-speed-input']");
    expect(speedInput.attributes("min")).toBe("1");
    expect(speedInput.attributes("max")).toBe("4095");
    await speedInput.setValue("5000");
    await wrapper.get("[data-testid='scan-set-speed']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).not.toHaveBeenCalled();

    await wrapper.get("[data-testid='scan-group-input']").setValue("300");
    await wrapper.get("[data-testid='scan-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("没有云台控制权限时扫描卡片可见但按钮全禁用", async () => {
    api.controlPtzScan.mockReset();
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    // 保留 play:start(否则不点播、详情区整块不渲染) —— 只撤掉 ptz:control。
    userState.account.permissions = ["gb28181:play:start", "gb28181:ptz:view"];
    await nextTick();

    const card = wrapper.get("[data-testid='scan-card']");
    for (const id of ["scan-toggle", "scan-set-left", "scan-set-right", "scan-set-speed"]) {
      expect(card.get(`[data-testid='${id}']`).attributes("disabled")).toBeDefined();
    }
    await card.get("[data-testid='scan-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlPtzScan).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("雨刷卡片:只发 on/off(编号由后端钉成 1),运行态只说「已下发」", async () => {
    api.controlPtzWiper.mockReset();
    api.controlPtzWiper.mockResolvedValue({ code: 0, message: "", data: { operationId: "wiper-op-1", status: "sent" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const card = wrapper.get("[data-testid='wiper-card']");
    expect(card.element.closest(".ptz-speed")).not.toBeNull();
    const auxGrid = card.element.closest(".ptz-aux-grid");
    expect(auxGrid).not.toBeNull();
    expect(wrapper.get("[data-testid='ptz-drag-zoom']").element.closest(".ptz-aux-grid")).toBe(auxGrid);
    expect(card.get("[data-testid='wiper-toggle']").text()).toContain("开启雨刷");
    // ⛔ 卡片里**没有**编号输入框:编号 1 是标准唯一命名的编号(A.3.7 表 A.11 注),
    //    由后端固定,不放给调用方填 —— 否则等于邀请 2~5 那些标准未定义的私有语义。
    expect(card.find("input").exists()).toBe(false);
    // 口径必须写清"标准只命名了编号 1"+"没有回读命令",否则会被读成平台能查到开关状态
    expect(card.get("[data-testid='wiper-hint']").text()).toContain("编号 1");
    expect(card.get("[data-testid='wiper-hint']").text()).toContain("没有查询命令");

    await card.get("[data-testid='wiper-toggle']").trigger("click");
    await flushPromises();
    expect(api.controlPtzWiper).toHaveBeenCalledWith(channel.id, { action: "on" });

    // HTTP 成功 ≠ 雨刷在刮:chip 说的是「已下发」,且不许出现"正在刮"这类词
    const chip = wrapper.get("[data-testid='wiper-running-chip']");
    expect(chip.text()).toContain("已下发");
    expect(chip.text()).not.toContain("刮");
    expect(wrapper.get("[data-testid='wiper-toggle']").text()).toContain("关闭雨刷");

    await chip.trigger("click");
    await flushPromises();
    expect(api.controlPtzWiper).toHaveBeenLastCalledWith(channel.id, { action: "off" });
    expect(wrapper.find("[data-testid='wiper-running-chip']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='wiper-toggle']").text()).toContain("开启雨刷");
    wrapper.unmount();
  });

  it("雨刷:设备未接受时不置运行态,把原因留在卡片里", async () => {
    api.controlPtzWiper.mockReset();
    api.controlPtzWiper.mockResolvedValue({ code: 0, message: "", data: { operationId: "wiper-op-2", status: "rejected" } });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    await wrapper.get("[data-testid='wiper-toggle']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='wiper-running-chip']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='wiper-error']").text()).toBeTruthy();
    expect(wrapper.get("[data-testid='wiper-toggle']").text()).toContain("开启雨刷");
    wrapper.unmount();
  });

  it("没有云台控制权限时雨刷卡片可见但按钮禁用", async () => {
    api.controlPtzWiper.mockReset();
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    // 与扫描同一条门禁:保留 play:start,只撤掉 ptz:control。
    userState.account.permissions = ["gb28181:play:start", "gb28181:ptz:view"];
    await nextTick();

    const toggle = wrapper.get("[data-testid='wiper-toggle']");
    expect(toggle.attributes("disabled")).toBeDefined();
    await toggle.trigger("click");
    await flushPromises();
    expect(api.controlPtzWiper).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("云台详情区列数必须等于卡片数(现在是 4:预置位/巡航/看守位/扫描)", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    const layout = wrapper.get(".linked-ptz-layout");
    expect(layout.findAll(":scope > .linked-section.linked-card")).toHaveLength(4);
    // 卡片数是渲染事实,栅格列数是 CSS 事实 —— 两者不一致时(曾经 4 列 3 卡)
    // 会有一列空着或最后一张被挤到第二行,而详情条高度是硬预算 148px、卡片
    // overflow: hidden ⇒ 第二行被裁掉。jsdom 算不出栅格,只能扫源码钉住这个数字。
    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsolePtzPanel.vue"),
      "utf8"
    );
    const rule = source.slice(source.indexOf(".linked-ptz-layout {"));
    expect(rule.slice(0, rule.indexOf("}"))).toContain("grid-template-columns: repeat(4, minmax(0, 1fr));");
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

    const source = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PlayConsoleDialogs.vue"),
      "utf8"
    );
    expect(source).toMatch(/\.cruise-stops-list\s*\{[^}]*max-height:\s*clamp\(168px,\s*30vh,\s*260px\)[^}]*overflow-y:\s*auto/s);
    expect(source).toMatch(/\.cruise-stop-add\s*\{[^}]*width:\s*100%[^}]*min-height:\s*44px/s);
    expect(source).toMatch(
      // ⛔ 同上：两种媒体查询写法都接受（stylelint --fix 会把 max-width 改成 width <=）。
      /@media \((?:max-width:\s*560px|width <= 560px)\)\s*\{[^}]*\.cruise-save-form\s*\{[^}]*max-height:\s*calc\(100dvh - 210px\)/s
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

    // 单位必须写出来:`0x86`/`0x87` 的参数是 12 位裸整数,单写一个 5 没人知道是秒还是档位
    expect(pendingTooltip.attributes("content")).toContain("每点停留 5 秒");
    expect(pendingTooltip.attributes("content")).toContain("速度 8");
    expect(pendingTile.text()).toContain("未验证");

    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    const presetCardSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PtzPresetCard.vue"),
      "utf8"
    );
    const cruiseCardSource = readFileSync(
      resolve(process.cwd(), "src/views/gb28181/components/play-console/PtzCruiseCard.vue"),
      "utf8"
    );
    expect(source).toContain("const RESOURCE_TOOLTIP_ENTER_DELAY_MS = 80;");
    expect(presetCardSource.match(/:mouse-enter-delay="tooltipDelay"/g)).toHaveLength(2);
    expect(cruiseCardSource.match(/:mouse-enter-delay="tooltipDelay"/g)).toHaveLength(2);
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

    const tooltip = wrapper.get("[data-testid='cruise-tile-tooltip-4']");
    expect(tooltip.attributes("content")).toContain("预置位 3→1→5");
    expect(tooltip.attributes("content")).toContain("每点停留 30 秒");
    expect(tooltip.attributes("content")).toContain("速度 128");
    expect(tooltip.attributes("content")).not.toContain("速度未上报");
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
    expect(wrapper.get("[data-testid='cruise-tile-tooltip-2']").attributes("content")).toContain("预置位 1→2→3→4→5→6 等 9 个");
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
    // 「画面设置」tab 用 canViewPtz 门禁 —— 游客没有 ptz:view，整栏都不该挂载
    // （它现在同时挂着视频编码与图像叠加两组，所以两组的读写口都要一起消失）。
    expect(wrapper.find("[data-testid='linked-tab-deviceconfig']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-side-ptz']").exists()).toBe(false);
    // 「请求关键帧」2026-09-20 搬进云台侧栏，同样要 canControlDevice 门禁 ⇒ 游客也看不到
    expect(wrapper.find("[data-testid='ptz-iframe-request']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-side-deviceconfig']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-detail-ptz']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-detail-picture']").exists()).toBe(false);
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

  it("左侧工作区保留既有模块，并按任务隔离配置分组", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();

    // 2026-09-20 起是 **3 个页签** —— 「高级」退役，「录像存储 / 报警控制」两个整页签搬去
    // 设备管理页的「设备详情」抽屉，「视频编码」并回「画面设置」（5 → 4），
    // 最后「设备维护」也搬去那张抽屉（改叫「基本参数」，4 → 3）。
    // ⛔ 别让「视频编码」再变回一级页签：它和图像叠加改的是同一台设备的同一路画面，
    //    分成两个并列顶级入口只会让"把画面调一下"变成要先猜进哪个。
    expect(wrapper.find("[data-testid='linked-tab-ptz']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-probe']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-advanced']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-tab-deviceconfig']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='linked-tab-videoparam']").exists()).toBe(false);
    // ⛔ 这三个页签连着它们的组清单一起搬走了；只删页签却把组留在 configWorkspaceGroups 里，
    //    会留下一批"没有入口的配置组"（读取照样发出去，界面上永远看不到）。
    expect(wrapper.find("[data-testid='linked-tab-record']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='linked-tab-alarm']").exists()).toBe(false);
    // ⛔ 「设备维护」同理：它的内容（`basic` 组 = A.2.1.19 BasicParam）现在**只在**
    //    设备详情抽屉的「基本参数」页里，控制台侧不再保留第二个入口。
    expect(wrapper.find("[data-testid='linked-tab-device']").exists()).toBe(false);
    expect(wrapper.get("nav[aria-label='播放工作区']").findAll("button")).toHaveLength(3);
    expect(wrapper.get("[data-testid='linked-tab-deviceconfig']").text()).toContain("画面设置");

    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    const pictureBar = wrapper.get("[data-testid='linked-detail-picture']");
    expect(pictureBar.classes()).toContain("linked-detail-actions");
    // 底栏**四个视觉卡片**：时间戳 / 叠加文字 / 遮挡 / 镜像。
    // 图像叠加组件占两列，内部两块与另外两张卡共同瓜分整行空间。
    expect(pictureBar.find("[data-testid='picture-osd-cell']").exists()).toBe(true);
    expect(pictureBar.find("[data-testid='picture-mask-card']").exists()).toBe(true);
    expect(pictureBar.find("[data-testid='picture-mirror-card']").exists()).toBe(true);
    expect(pictureBar.find("[data-testid='video-param-compare-card']").exists()).toBe(false);
    expect(pictureBar.findAll(".linked-picture-layout > .linked-section")).toHaveLength(3);
    // ⛔ 参数对照卡上**不再有**读取 / 还原 / 下发三颗按钮：侧栏抽屉的参数头已经有同一排
    //    （`dcg-embedded-actions`），同一屏两套同名按钮是本仓点过名的坑。
    expect(wrapper.find("[data-testid='video-param-bottom-read']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='video-param-bottom-apply']").exists()).toBe(false);
    // 2026-09-19 流程重做：下发入口从底栏第三张卡搬进画布浮条。反向钉住"提交卡不再回来" ——
    // 这个 testid 一复现，就说明有人把卡片又加回来了，浮条与卡片会变成两个入口。
    expect(pictureBar.find("[data-testid='picture-apply-card']").exists()).toBe(false);

    // 侧栏 = 只挂「视频编码」一组 ⇒ `dcg-nav` 整体不渲染（一组没有可切的东西）。
    // ⛔ 图像叠加已整块搬到下面底栏，这里**不该**再有第二份：同一份 `familyValues.osd`
    //    两个编辑面，改哪边都只看得见一半。
    const sidePanel = wrapper.get("[data-testid='linked-side-deviceconfig']");
    expect(sidePanel.find(".dcg-nav").exists()).toBe(false);
    expect(sidePanel.find("[data-testid='dcg-nav-video-param']").exists()).toBe(false);
    expect(sidePanel.find("[data-testid='dcg-nav-osd']").exists()).toBe(false);
    expect(sidePanel.find("[data-testid='dcg-osd-blocks']").exists()).toBe(false);
    // 侧栏里留下的是视频编码本体（组标题 + 对账条，`dcg-reconcile` 只在 video-param 出）。
    expect(sidePanel.find("[data-testid='dcg-group-title']").text()).toBe("视频编码");
    expect(sidePanel.find("[data-testid='dcg-reconcile']").exists()).toBe(true);
    // ⛔ 「画面处理」（镜像 + 隐私遮挡）仍不建在侧栏：它只有底栏卡片这一个编辑入口，
    //    侧栏再挂一份就是同一份 `familyValues.picture` 的两个入口。
    expect(sidePanel.find("[data-testid='dcg-nav-picture']").exists()).toBe(false);
    // ⛔ record-plan / alarm-record / alarm-report 三组的入口**只在**设备详情抽屉里。
    //    页签删了但组还留在 configWorkspaceGroups 里的话，这两串组名会以"永远看不到的
    //    配置组"形式复活（读取照发、界面无入口），所以这里按渲染文本钉一次。
    expect(sidePanel.text()).not.toContain("录像计划");
    expect(sidePanel.text()).not.toContain("报警上报");
    expect(sidePanel.find("[data-testid='dcg-nav-basic']").exists()).toBe(false);

    // 换页签会退出 OSD 编辑模式（状态由画面侧持有）；切回也不该自己冒出来。
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='picture-osd-cell']").find("[data-testid='osd-block-time']").exists()).toBe(true);

    // ⛔ 控制台侧**没有第二个配置页**了：唯一还在的配置页就是「画面设置」，它只挂
    //    `video-param` 一组；`basic` / `record-plan` / `alarm-*` 三族全部只在设备详情抽屉里。
    //    这条按"渲染出来的组标题"钉死 —— 组留在 configWorkspaceGroups 里而页签没了的话，
    //    读取照发、界面上永远看不到，是本仓点过名的坑。
    await wrapper.get("[data-testid='linked-tab-probe']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='linked-side-deviceconfig'] [data-testid='dcg-group-title']").text()).toBe("视频编码");
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

  it("对照卡的码流与侧栏「配置文件」是同一路，不是两份状态", async () => {
    // ⛔ 两个下拉各持一份状态，就会出现"底栏对着子码流、侧栏在改主码流"，而两边都不报错 ——
    //    用户照着对照卡上的数字去改，改的却根本不是那条流。
    //    真源只有 `selectedVideoStream` 一个，侧栏按它只渲染对应那一行。
    api.getChannelVideoParams.mockResolvedValue(
      videoParamsResponse({
        list: [
          videoParamRow({ id: 1, streamNumber: 0 }),
          videoParamRow({ id: 2, streamNumber: 1, resolution: "4", videoBitRate: "2048" })
        ],
        freshness: "fresh",
        streamNumberList: "0/1",
        reconcile: { state: "read_ok" }
      })
    );
    const wrapper = await openPictureTab();

    // 默认主码流：侧栏只渲染主码流那一行。
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-stream-1']").exists()).toBe(false);

    // 底栏切到子码流 → 侧栏跟着切。
    await wrapper.get("[data-testid='video-param-bottom-stream']").setValue("1");
    await flushPromises();
    expect(wrapper.find("[data-testid='dcg-stream-1']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='dcg-stream-0']").exists()).toBe(false);
    expect((wrapper.get("[data-testid='video-param-bottom-stream']").element as HTMLSelectElement).value).toBe("1");
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

/**
 * 图像叠加（OSD）的画布锚点层。
 *
 * ⛔ 形态是**锚点 + 内容标签**，不是"像真字的预览"：标准 `OSDCfgType`（A.2.1.12）里没有
 *    字体、字号、颜色，画出来就是在承诺平台给不了的能力；而设备已烧进码流的时间戳就在
 *    这个画面里，再叠一个假字会出现**两个时间戳**。
 * ⛔ 坐标基准与遮挡**同一把尺**（`OSDConfig.Length/Width`），不是画面解码尺寸。
 */
describe("PlayConsoleLinked 图像叠加（OSD）画布锚点层", () => {
  beforeEach(() => {
    userState.account = reactive({ permissions: ["*:*:*"] });
    api.getChannelDeviceConfigs.mockReset();
    api.getChannelDeviceConfigs.mockResolvedValue(osdDeviceConfigResponse());
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

  /**
   * OSD 组的设备回读应答。
   *
   * ⛔ `payload` 是**那一块本身**（`OSDConfig` 这一行），不是包在 `osdConfig` 键下的容器。
   * 画布取真机实测过的那把尺：`Length=704 / Width=576`。
   */
  function osdDeviceConfigResponse(
    overrides: { items?: Array<Record<string, unknown>>; timeEnable?: number; textEnable?: number } = {}
  ) {
    return {
      code: 0,
      message: "",
      data: {
        list: [
          {
            configType: "OSDConfig",
            observedAt: "2026-09-20T04:00:00Z",
            sourceOperationId: "1500",
            payload: {
              length: 704,
              width: 576,
              timeX: 44,
              timeY: 58,
              timeEnable: overrides.timeEnable ?? 1,
              timeType: 1,
              textEnable: overrides.textEnable ?? 1,
              items: overrides.items ?? [
                { text: "北门", x: 176, y: 288 },
                { text: "3 号车间", x: 352, y: 432 }
              ]
            }
          },
          // 画面组也给上事实：浮条的「下发」是**画面 + OSD 一次发多块**，
          // 少了这两块就测不出"合并"这件事（没事实的组会被跳过，那是另一条正确的闸门）。
          {
            configType: "FrameMirror",
            observedAt: "2026-09-20T04:00:00Z",
            sourceOperationId: "1500",
            payload: { value: 0 }
          },
          {
            configType: "PictureMask",
            observedAt: "2026-09-20T04:00:00Z",
            sourceOperationId: "1500",
            payload: { on: 0, regions: [] }
          }
        ],
        absentTypes: [],
        registeredVersion: "2022",
        freshness: "fresh",
        observedAt: "2026-09-20T04:00:00Z",
        reconcile: {
          state: "read_ok",
          operationId: "1500",
          status: "accepted",
          responseHasData: true,
          derivedFromApply: false
        },
        refreshOperationId: null,
        refreshError: null
      }
    };
  }

  /**
   * 挂载控制台并切到「画面设置」页签 —— OSD 面板就在这一页的**底栏第一格**。
   *
   * ⛔ 2026-09-20 第二阶段（老板：「不想用切换的方式，要一页全展示」）之后，
   *    「图像叠加」不再挂在侧栏的分组导航后面：侧栏只剩「视频编码」一组，
   *    `dcg-nav` 整体不渲染。所以这里**只切页签**，不再点任何分组。
   */
  async function openOsdTab() {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    await flushPromises();
    return wrapper;
  }

  /**
   * 取锚点样式里的 `left` 百分比。
   *
   * ⛔ 不直接比字符串：`44 / 704 * 100` 算出来是 `6.25` 这类无限小数，
   *    写成字面量断言等于把浮点表示钉进用例，改一次就必须重新抄一遍。
   */
  function leftPercent(style: string | undefined): number {
    return Number(/left:\s*([\d.]+)%/.exec(style ?? "")?.[1] ?? Number.NaN);
  }

  /** 给锚点层一个**真实矩形**：jsdom 的 `getBoundingClientRect` 恒为 0，不 mock 就变成恒真断言。 */
  function stubLayerRect(el: Element) {
    (el as HTMLElement).getBoundingClientRect = () =>
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
  }

  /**
   * 进「调整位置」编辑模式 —— 按钮在**侧栏「时间戳」面板**里（2026-09-20 最终落点）。
   *
   * ⛔ 这个 `get` 本身就是一条断言：按钮不在画面上、也不在画面下方的工具条里，
   *    它跟它控制的那个坐标读数（`osd-time-pos`）在同一个卡片里。
   */
  async function enterOsdEdit(wrapper: ReturnType<typeof mount>) {
    await wrapper.get("[data-testid='osd-edit-toggle']").trigger("click");
    await flushPromises();
  }

  /**
   * 2026-09-20 第二阶段：OSD 面板搬到**底栏第一格**，侧栏只剩「视频编码」一组。
   *
   * ⛔ 这条用例替代了原来的「侧栏切到视频编码组会退出编辑模式」—— 分组切换本身没了，
   *    也就不存在"切组"这条退出路径；退出只剩 按钮 / Esc / 换页签 / 换通道。
   * ⛔ 关键是**只有一份**：侧栏再留一份就是同一份 `familyValues.osd` 的两个编辑面
   *    （本仓因为"两份状态"返工过三次）。
   */
  it("OSD 面板只在底栏有一份，侧栏不再有第二份；换页签会退出编辑模式", async () => {
    const wrapper = await openOsdTab();

    const side = wrapper.get("[data-testid='linked-side-deviceconfig']");
    expect(side.find(".dcg-nav").exists()).toBe(false);
    expect(side.find("[data-testid='dcg-osd-blocks']").exists()).toBe(false);

    expect(wrapper.findAll("[data-testid='osd-block-time']")).toHaveLength(1);
    expect(wrapper.get("[data-testid='picture-osd-cell']").find("[data-testid='osd-block-time']").exists()).toBe(true);

    await enterOsdEdit(wrapper);
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(true);

    // 换页签退出（四条退出路径之一：按钮 / Esc / 换页签 / 换通道）。
    // ⛔ 目标页签从 `device`（「设备维护」）改成 `ptz`：那一页 2026-09-20 已退役，
    //    整个控制台只剩 3 个页签。
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);

    // 切回来也不自己冒出来 —— 重新进编辑模式是用户的显式动作。
    await wrapper.get("[data-testid='linked-tab-deviceconfig']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='osd-edit-toggle']").text()).toContain("调整位置");
    wrapper.unmount();
  });

  it("默认画面上**一个标记都没有**；点「调整位置」才出现，位置按设备画布归一化（不是解码尺寸）", async () => {
    const wrapper = await openOsdTab();

    // ⛔ 老板 2026-09-20 第二次反馈："这个为啥还是默认显示呢，不是说的点击调整位置之后才出现吗"
    //    —— 上一版做的是"锚点常驻 + 只读态降噪"，结果就是四五个带引线的标签摊在视频上。
    //    现在只读态**整层不渲染**：位置信息由侧栏那行 `X 289 · Y 256` 承担。
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);
    expect(wrapper.find("[data-anchor='time']").exists()).toBe(false);

    const toggle = wrapper.get("[data-testid='osd-edit-toggle']");
    expect(toggle.text()).toContain("调整位置");
    expect(toggle.attributes("data-active")).toBe("0");
    // ⛔ 按钮**不许长在画面上**（老板 2026-09-20："会遮挡画面"），也不在画面下方的工具条里 ——
    //    它就在它控制的那个坐标读数旁边（「时间戳」卡片的「位置」行）。
    expect(wrapper.get("[data-testid='osd-block-time']").find("[data-testid='osd-edit-toggle']").exists()).toBe(true);
    expect(wrapper.find(".switcher-osd").exists()).toBe(false);

    await enterOsdEdit(wrapper);
    const layer = wrapper.get("[data-testid='osd-overlay-layer']");
    // 1 个时间戳 + 2 行文字
    expect(layer.findAll(".osd-anchor")).toHaveLength(3);

    // 44 / 704 = 6.25%；58 / 576 ≈ 10.07%
    expect(leftPercent(layer.get("[data-anchor='time']").attributes("style"))).toBeCloseTo(6.25, 2);
    // 176 / 704 = 25%；288 / 576 = 50%
    const first = layer.get("[data-anchor='item-0']");
    expect(leftPercent(first.attributes("style"))).toBeCloseTo(25, 2);
    // 编号与侧栏列表同源：锚点标签就是「1 北门」
    expect(first.get(".osd-anchor-tag").text()).toContain("1 北门");

    // ⛔ 拿解码尺寸（测试环境的 1280×720）当基准的话，这两个百分比会完全不一样 ——
    //    那正是遮挡侧踩过的"我画的框挡住了别的地方"。
    expect(layer.get("[data-anchor='time']").attributes("style")).not.toContain(`${(44 / 1280) * 100}`);

    // 这层只在编辑模式存在，所以那条说明也就是"编辑模式的说明"，且必须指向**侧栏**的按钮
    //（2026-09-20 之前它在画面下方工具条，照旧文案会让用户低头找）。
    expect(layer.get("[data-testid='osd-layer-hint']").text()).toContain("侧栏");
    wrapper.unmount();
  });

  it("开关关闭的锚点**淡显**而不是消失（配置还在，只是画面不显示）", async () => {
    api.getChannelDeviceConfigs.mockResolvedValue(osdDeviceConfigResponse({ textEnable: 0 }));
    const wrapper = await openOsdTab();
    await enterOsdEdit(wrapper);
    const item = wrapper.get("[data-anchor='item-0']");
    // ⛔ 关闭不等于"没画出来"：`get` 能取到就说明它还在（隐藏会让用户以为配置丢了，
    //    而它下次启用会一起活过来）。
    expect(item.attributes("data-off")).toBe("1");
    expect(item.classes()).toContain("is-off");
    // 时间戳那一枚不受文字开关影响
    expect(wrapper.get("[data-anchor='time']").attributes("data-off")).toBe("0");
    wrapper.unmount();
  });

  it("新增文字行进入「未定位」态，浮条**把下发拦下来**并说明去哪儿摆位置", async () => {
    const wrapper = await openOsdTab();
    await enterOsdEdit(wrapper);
    await wrapper.get("[data-testid='osd-add']").trigger("click");
    await flushPromises();

    const anchor = wrapper.get("[data-anchor='item-2']");
    expect(anchor.attributes("data-unplaced")).toBe("1");
    expect(anchor.get(".osd-anchor-tag").text()).toContain("未定位");

    // ⛔ 拦下发的判据是**草稿里的定位标记**，不是坐标值：`0,0` 是合法坐标
    //    （设备的左上角就是有人会用的位置），靠数值反推必然把"还没摆"当成"摆了左上角"，
    //    设备上就真的多出一行贴左上角的字，而界面看起来一切正常。
    const apply = wrapper.get("[data-testid='picture-draft-apply']");
    expect(apply.attributes("disabled")).toBeDefined();
    expect(apply.attributes("title")).toContain("还没在画面上定位");

    // 字符计数**就地**显示：原实现要等 `buildOSD` 拒发才说一句「第 N 条超过 32 个字符」，
    // 用户还得自己回去找是哪一条。
    const input = wrapper.get("[data-testid='osd-text-2']");
    (input.element as HTMLInputElement).value = "国".repeat(33);
    await input.trigger("change");
    await flushPromises();
    expect(wrapper.get("[data-testid='osd-charcount-2']").text()).toBe("33/32");
    wrapper.unmount();
  });

  it("拖拽锚点：拖动中不写草稿，松手才落到设备画布坐标并标成已定位", async () => {
    const wrapper = await openOsdTab();
    await enterOsdEdit(wrapper);
    const layer = wrapper.get("[data-testid='osd-overlay-layer']");
    stubLayerRect(layer.element);

    const anchor = wrapper.get("[data-anchor='item-0']");
    await anchor.trigger("pointerdown", { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    await anchor.trigger("pointermove", { clientX: 110, clientY: 80, pointerId: 1 });
    // ⛔ 拖动中**不写草稿**：`pointermove` 里落草稿会让脏值统计一路抖动。
    expect(wrapper.find("[data-testid='picture-draft-bar']").exists()).toBe(false);

    await anchor.trigger("pointerup", { clientX: 110, clientY: 80, pointerId: 1 });
    await flushPromises();

    // (110 / 200) × 704 = 387.2 → 387；(80 / 100) × 576 = 460.8 → 461
    expect(leftPercent(wrapper.get("[data-anchor='item-0']").attributes("style"))).toBeCloseTo((387 / 704) * 100, 1);
    expect(wrapper.get("[data-testid='picture-draft-summary']").text()).toContain("1 条文字");
    wrapper.unmount();
  });

  it("没进编辑模式时画面是干净的：没有层、没有标记、也不会有半截拖拽残留", async () => {
    const wrapper = await openOsdTab();

    // 画面里既没有锚点层，也没有任何 OSD 相关的说明文字 —— 屏幕上的每一寸都在放视频。
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='osd-layer-hint']").exists()).toBe(false);
    expect(wrapper.find(".osd-anchor").exists()).toBe(false);
    // 侧栏的坐标读数照旧给出位置信息 —— "画面上不画"不等于"位置不可知"。
    expect(wrapper.get("[data-testid='osd-time-pos']").text()).toBe("X 44 · Y 58");
    expect(wrapper.find("[data-testid='picture-draft-bar']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("「完成调整」与 Esc 都能退出编辑模式，退出后整层锚点消失", async () => {
    const wrapper = await openOsdTab();

    await enterOsdEdit(wrapper);
    expect(wrapper.get("[data-testid='osd-overlay-layer']").findAll(".osd-anchor")).toHaveLength(3);
    // 按钮在侧栏里，文案跟着模式走
    expect(wrapper.get("[data-testid='osd-edit-toggle']").text()).toContain("完成调整");

    // ① 再点一次退出 → 层整个没了（不是"留着但不吃指针"）
    await wrapper.get("[data-testid='osd-edit-toggle']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);
    expect(wrapper.get("[data-testid='osd-edit-toggle']").text()).toContain("调整位置");

    // ② 进编辑模式后按 Esc 退出（拖到一半的临时坐标也要收干净，见 `exitOsdEditMode`）
    await enterOsdEdit(wrapper);
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(true);
    pressEscape();
    await flushPromises();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("进编辑模式会**顺手退出遮挡框选**（两层都吃 pointerdown，同时开着说不清拖的是什么）", async () => {
    const wrapper = await openOsdTab();
    await wrapper.get("[data-testid='picture-mask-add-btn']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='mask-draw-layer']").exists()).toBe(true);

    // 按钮在侧栏、不在框选层下面，所以它是可点的 —— 点它就是"改做另一件事"，
    // 框选（还没落框的那半截）随之取消，这是两层互斥的另一半（反方向见 `startMaskDraw`）。
    await enterOsdEdit(wrapper);
    expect(wrapper.find("[data-testid='mask-draw-layer']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("侧栏每行的「定位」直接进编辑模式并把那枚锚点拉到眼前（用户已经点名要挪这一行）", async () => {
    const wrapper = await openOsdTab();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);

    // ⛔ 否则会出现"点了按钮、锚点闪了一下、却拖不动" —— 而用户刚刚才被告知"拖动即可改位置"。
    await wrapper.get("[data-testid='osd-locate-0']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(true);
    // 高亮落在那一行上，而不是时间戳
    expect(wrapper.get("[data-anchor='item-0']").classes()).toContain("is-focus");
    expect(wrapper.get("[data-anchor='time']").classes()).not.toContain("is-focus");
    wrapper.unmount();
  });

  it("浮条把画面组与 OSD 的草稿**合并成一句话**，一次报文发全部块", async () => {
    const wrapper = await openOsdTab();
    // 改一个 OSD 字段（时间格式：回读是 "1"，改成 "0" —— 下拉框不是三条单选）
    await wrapper.get("[data-testid='osd-fmt-select']").setValue("0");
    await flushPromises();

    const bar = wrapper.get("[data-testid='picture-draft-bar']");
    expect(bar.get("[data-testid='picture-draft-summary']").text()).toContain("时间格式");

    await bar.get("[data-testid='picture-draft-apply']").trigger("click");
    await flushPromises();

    // ⭐ 协议上 `OSDConfig` 与 `PictureMask` 本来就是 `DeviceConfig` 里的兄弟元素，
    //    合并成一条报文 —— 两条浮条会重演「同一屏两套同名按钮」。
    const [, blocks] = api.applyChannelDeviceConfigs.mock.calls.at(-1)!;
    const keys = Object.keys(blocks as object).sort();
    expect(keys).toContain("osdConfig");
    expect(keys).toContain("pictureMask");
    wrapper.unmount();
  });

  it("没读到 OSD 设备事实时**一个锚点都不画**（平台初值不许伪装成设备现状）", async () => {
    // 只回画面组，不回 OSDConfig ⇒ `familyValues.osd` 里躺的是平台空白模板（timeX=10 这类）。
    api.getChannelDeviceConfigs.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        list: [{ configType: "FrameMirror", observedAt: "2026-09-20T04:00:00Z", payload: { value: 0 } }],
        absentTypes: ["OSDConfig"],
        registeredVersion: "2022",
        freshness: "fresh",
        observedAt: "2026-09-20T04:00:00Z",
        reconcile: { state: "type_absent", operationId: "1501", status: "accepted", responseHasData: true },
        refreshOperationId: null,
        refreshError: null
      }
    });
    const wrapper = await openOsdTab();
    expect(wrapper.find("[data-testid='osd-overlay-layer']").exists()).toBe(false);
    // 侧栏的只读基准块如实说「未读到」，而不是把模板里的 1920×1080 摆出来
    expect(wrapper.get("[data-testid='osd-canvas-value']").text()).toBe("—");
    expect(wrapper.get("[data-testid='osd-canvas-tag']").text()).toBe("未读到");
    // ⛔ 「调整位置」此时**禁用**：`familyValues.osd` 里躺的是平台空白模板（timeX=10 这类），
    //    放进去拖一把就等于把平台初值当成设备现状了。按钮禁用 + `enterOsdEditMode` 里再拦一道。
    expect(wrapper.get("[data-testid='osd-edit-toggle']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });
});

describe("PlayConsoleLinked 目标跟踪（GB/T 28181-2022 A.2.3.1.14）", () => {
  /**
   * ⛔ 本 describe 是**独立**的（不是上面任一 describe 的子块），所以它必须自己把
   *    `getChannelTargetTrack` 重置回"平台还没下发过"。漏了的话，上一条用例里
   *    `mockResolvedValue` 设下的意图会漏进下一条 —— 用例照样绿，但它验证的是
   *    "界面能显示上一条用例的状态"，而不是它自己声称的东西。
   */
  beforeEach(() => {
    userState.account = reactive({ permissions: ["*:*:*"] });
    api.getChannelTargetTrack.mockReset();
    api.getChannelTargetTrack.mockResolvedValue(targetTrackResponse());
    api.setChannelTargetTrack.mockReset();
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

  it("状态行说的是「平台最近一次下发」，绝不是「设备正在跟踪」", async () => {
    // ⛔⛔ 本组最重要的一条口径。目标跟踪是**无应答命令**（9.3.1 d) + 表 1 序号 13 =「（无）」），
    //    而且 2022 全文没有"查设备在跟踪什么"的命令 ⇒ 平台**永远无法**知道设备的实际状态。
    //    界面上一旦出现"设备正在跟踪"，那句话既无法被证伪、也永远发现不了是错的。
    api.getChannelTargetTrack.mockResolvedValue(targetTrackResponse(targetTrackIntent()));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");

    const text = wrapper.get("[data-testid='target-track-intent']").text();
    expect(text).toContain("平台最近一次下发：手动跟踪");
    expect(text).toContain("框 200×150 @ 400,200");
    expect(text).not.toContain("设备正在");
    expect(text).not.toContain("正在跟踪");
    wrapper.unmount();
  });

  it("从没下发过时如实说「还没有下发过」，而不是显示一条编出来的默认状态", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");

    expect(wrapper.get("[data-testid='target-track-intent']").text()).toContain("还没有向这台设备下发过目标跟踪");
    wrapper.unmount();
  });

  it("自动跟踪一键下发：只带 mode=Auto，**不带 area**（不带框才是自动跟踪）", async () => {
    api.setChannelTargetTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        operationId: "op-tt-auto",
        action: "target_track",
        status: "sent",
        responseRequired: false,
        deduplicated: false,
        intent: targetTrackIntent({ mode: "Auto", areaLengthX: null, areaLengthY: null })
      }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    await wrapper.get("[data-testid='ptz-target-track-auto']").trigger("click");
    await flushPromises();

    expect(api.setChannelTargetTrack).toHaveBeenCalledTimes(1);
    const [channelId, payload] = api.setChannelTargetTrack.mock.calls[0];
    expect(channelId).toBe(channel.id);
    expect(payload.mode).toBe("Auto");
    // ⛔ 补一个空 area 会被服务端拒（"手动跟踪才需要框"，而 Auto 带了框含义就变了）。
    expect("area" in payload).toBe(false);

    // ⛔ 状态词只能是「已下发 + 设备未回执」：无应答命令没有"成功/生效"这个概念，
    //    而 sent 已经是它的**终态**（不去轮询 operation，也不说"已完成"）。
    const status = wrapper.get("[data-testid='target-track-status']").text();
    expect(status).toContain("已下发");
    expect(status).toContain("设备未回执");
    expect(status).not.toContain("已完成");
    expect(status).not.toContain("正在跟踪");
    wrapper.unmount();
  });

  it("停止跟踪走 mode=Stop，且绝不带 area（带框的停止是自相矛盾的指令）", async () => {
    api.setChannelTargetTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { operationId: "op-tt-stop", action: "target_track", status: "sent", responseRequired: false }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    await wrapper.get("[data-testid='ptz-target-track-stop']").trigger("click");
    await flushPromises();

    const [, payload] = api.setChannelTargetTrack.mock.calls[0];
    expect(payload.mode).toBe("Stop");
    expect("area" in payload).toBe(false);
    wrapper.unmount();
  });

  it("框选跟踪按**画面渲染尺寸**换算 TargetArea，下发完立刻退出框选态", async () => {
    api.setChannelTargetTrack.mockResolvedValue({
      code: 0,
      message: "",
      data: { operationId: "op-tt-manual", action: "target_track", status: "sent", responseRequired: false }
    });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterTargetTrackDraw(wrapper);
    await targetTrackDragOnce(wrapper, [200, 100], [600, 300], 21);

    expect(api.setChannelTargetTrack).toHaveBeenCalledWith(
      channel.id,
      expect.objectContaining({
        mode: "Manual",
        // ⛔ length/width 是"播放窗口像素值"（标准原文），= 画面渲染出来的 800×450，
        //    不是视频原始分辨率、也不是播放器元素外框。设备按它做比例换算。
        area: { length: 800, width: 450, midPointX: 400, midPointY: 200, lengthX: 400, lengthY: 200 }
      })
    );
    // ⭐ 与拉框变焦**刻意不同**：跟踪是"选定一个目标"，下完一次就退出。
    //    留着态会让用户以为还要再框第二刀，而第二条手动跟踪会直接覆盖第一条的框。
    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("框太小时不下发，并留在框选态让用户重画", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterTargetTrackDraw(wrapper);
    // 4px × 4px：一次误触（点一下没怎么拖）落在这一段。
    await targetTrackDragOnce(wrapper, [200, 100], [204, 104], 22);

    expect(api.setChannelTargetTrack).not.toHaveBeenCalled();
    // 留在框选态 —— 否则用户得回侧栏再点一次按钮才能重画。
    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("框选态与 3D 拖拽互斥：进一个就关掉另一个（两个方向都钉住）", async () => {
    // ⛔ 判据是"同一个按下动作只能有一种解释"。四个"画面上拖"的模式（拉框变焦 / 遮挡框选 /
    //    OSD 调位置 / 目标跟踪框选）任两个同时开着，画出来的东西就说不清是哪一个。
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterTargetTrackDraw(wrapper);
    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(true);

    await wrapper.get("[data-testid='ptz-drag-zoom-in']").trigger("click");
    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(true);

    await wrapper.get("[data-testid='ptz-target-track-manual']").trigger("click");
    expect(wrapper.find("[data-testid='drag-zoom-layer']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("框选态那一下 Esc 只收框选态，且被图层吃掉（不让控制台跟着关）", async () => {
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await enterTargetTrackDraw(wrapper);
    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(true);

    const seenByStandIn: string[] = [];
    const standIn = (event: KeyboardEvent) => seenByStandIn.push(event.key);
    document.documentElement.addEventListener("keydown", standIn);
    try {
      pressEscape();
      await flushPromises();
    } finally {
      document.documentElement.removeEventListener("keydown", standIn);
    }

    expect(wrapper.find("[data-testid='target-track-layer']").exists()).toBe(false);
    // Arco 挂在 documentElement 上的那份监听（`esc-to-close`）不许收到这一下。
    expect(seenByStandIn).toEqual([]);
    expect(wrapper.get("[data-testid='ptz-target-track-manual']").text()).toContain("框选跟踪");
    wrapper.unmount();
  });

  it("下发失败落在错误行上，且不留下「已下发」的假状态", async () => {
    api.setChannelTargetTrack.mockRejectedValue(new Error("Network Error"));
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");
    await wrapper.get("[data-testid='ptz-mode-precise']").trigger("click");
    await wrapper.get("[data-testid='ptz-target-track-auto']").trigger("click");
    await flushPromises();

    expect(wrapper.get("[data-testid='target-track-error']").text()).toContain("Network Error");
    // ⛔ 状态行必须缺席：失败之后还挂着一句"已下发"，用户会以为命令出去了。
    expect(wrapper.find("[data-testid='target-track-status']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("面板自己挡 canControlDevice：只有 ptz 权限的账号看不到目标跟踪入口", async () => {
    // ⛔ 动作侧第一句就是 `if (!canControlDevice.value) return`，不在这儿挡就是"死按钮"。
    userState.account = reactive({ permissions: ["gb28181:ptz:view", "gb28181:ptz:control"] });
    const wrapper = mount(PlayConsoleLinked, { props: { visible: true, channel } });
    await flushPromises();
    await wrapper.get("[data-testid='linked-tab-ptz']").trigger("click");

    expect(wrapper.find("[data-testid='ptz-target-track']").exists()).toBe(false);
    // 反向对照：云台侧的拉框变焦此时也在（它由 canControlDevice 挡），说明整块门禁一致。
    expect(wrapper.find("[data-testid='ptz-drag-zoom']").exists()).toBe(false);
    wrapper.unmount();
  });
});
