import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DeviceRecordPlayback from "./index.vue";
import playbackPageSource from "./index.vue?raw";
import RecordTimeline from "./components/RecordTimeline.vue";
import timelineSource from "./components/RecordTimeline.vue?raw";
import { hasMarkup } from "@/test/source-assert";
import { channelReleaseWatchBudget } from "./playbackTeardown";

// ⛔ 别把 `{ prop: v; prop: v; }` 整串钉进断言：prettier 会把单行 CSS 拆成多行、
// stylelint 会把同名规则合并成并列选择器（`.a .x,\n.b .x {`），格式化一次就红一次。
// 这里按「某个选择器的**同一个规则块**里有没有这几条声明」来判。
function ruleBlocks(source: string, selector: string): string[] {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const pattern = new RegExp(escaped + "[^{]*\\{([^}]*)\\}", "g");
  const blocks: string[] = [];
  let matched: RegExpExecArray | null;
  while ((matched = pattern.exec(source)) !== null) {
    blocks.push(matched[1].replace(/\s+/g, " ").trim());
  }
  return blocks;
}

// 媒体查询会让同一个选择器有多个规则块（例如 `.playback-main` 宽屏是 grid、
// 窄屏是 flex 堆叠）。要求这几条声明**同处一块**，避免被另一个块的同名声明蒙混。
function hasRuleBlock(source: string, selector: string, ...declarations: string[]): boolean {
  return ruleBlocks(source, selector).some(block => declarations.every(item => block.includes(item)));
}

const api = vi.hoisted(() => ({
  getRecordQueryOptions: vi.fn(),
  queryDeviceRecords: vi.fn(),
  createPlaybackSession: vi.fn(),
  // 回放页的「下载」已经改成提交**服务端缓存任务**（见 @/api/recordCache）：
  // 受理成功后弹出的进度弹窗会轮询 getRecordCacheTask，两者都必须 mock，
  // 否则点击下载会打真实 http。
  createRecordCacheTask: vi.fn(),
  getRecordCacheTask: vi.fn(),
  cancelRecordCacheTask: vi.fn(),
  getPlaybackSession: vi.fn(),
  actionPlaybackSession: vi.fn(),
  deletePlaybackSession: vi.fn(),
  routerPush: vi.fn(),
  routerGetRoutes: vi.fn(() => [{ name: "device-mgmt-list", path: "/gb28181/device-mgmt/index" }])
}));

const account = vi.hoisted(() => ({
  permissions: ["gb28181:device-record:query", "gb28181:device-record:play", "gb28181:device-record:download"] as string[]
}));

const messageError = vi.hoisted(() => vi.fn());

vi.mock("@arco-design/web-vue", async importOriginal => ({
  ...(await importOriginal<typeof import("@arco-design/web-vue")>()),
  Message: { error: messageError }
}));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));

vi.mock("../device-mgmt/api", async importOriginal => ({
  ...(await importOriginal<typeof import("../device-mgmt/api")>()),
  ...api
}));

vi.mock("./api", async importOriginal => ({
  ...(await importOriginal<typeof import("./api")>()),
  createPlaybackSession: api.createPlaybackSession,
  getPlaybackSession: api.getPlaybackSession,
  actionPlaybackSession: api.actionPlaybackSession,
  deletePlaybackSession: api.deletePlaybackSession
}));

vi.mock("@/api/recordCache", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/recordCache")>()),
  createRecordCacheTask: api.createRecordCacheTask,
  getRecordCacheTask: api.getRecordCacheTask,
  cancelRecordCacheTask: api.cancelRecordCacheTask
}));

vi.mock("../components/PlayWindow.vue", () => ({
  default: {
    name: "PlayWindow",
    props: ["url", "playback", "hasAudio", "zlmWebrtc"],
    emits: ["timeupdate", "loading"],
    template: '<div data-testid="playback-player" :data-media-url="url" />'
  }
}));

vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => ({ params: { channelId: "31" }, query: { recordQueryMock: "complete", returnKey: "return-key" } }),
  useRouter: () => ({ push: api.routerPush, getRoutes: api.routerGetRoutes })
}));

// 这一页的错误文案并不来自模板渲染,而是 `utils/http` 对**每一个**失败请求自动弹的
// 全局 `Message.error(响应体 message)`。重试循环里的中间失败必须被抑制,只在真正
// 失败时弹一条 —— 所以下面的用例同时断言「弹了什么」和「没弹什么」。
const suppressed = { showErrorMessage: false };

// 后端 app.Response.Fail 的形状:业务错误码在 data.data 里,人话文案在 data.message。
function backendFailure(status: number, errorCode: string, errorStage: string, message: string) {
  return { message, response: { status, data: { message, data: { errorCode, errorStage } } } };
}

const teardownStall = () => backendFailure(504, "playback_unavailable", "teardown", "停止回放会话超时：设备未确认拆除");
const channelBusy = () => backendFailure(429, "playback_busy", "busy", "当前通道已有回放会话");
const sessionGone = () => backendFailure(404, "playback_not_found", "not_found", "回放会话不存在");

// 通道被上一路的占位占着:会话还查得到,状态停在 stopping。
function heldSession() {
  return {
    code: 0,
    data: {
      sessionId: "session-1",
      state: "stopping",
      channelId: "31",
      recordKey: "opaque-record-key",
      segmentStart: "2026-08-02T08:10:00+08:00",
      segmentEnd: "2026-08-02T08:42:16+08:00",
      positionSeconds: 0,
      scale: 1,
      hasAudio: false,
      media: { urls: {} },
      expiresAt: "2026-08-02T09:10:00+08:00",
      errorStage: "",
      errorCode: ""
    }
  };
}

function twoRecordings() {
  return {
    code: 0,
    data: {
      status: "complete",
      declaredTotal: 2,
      receivedCount: 2,
      incomplete: false,
      timezone: "Asia/Shanghai",
      elapsedMs: 151,
      list: [
        {
          recordKey: "recording-one",
          name: "录像一",
          startTime: "2026-08-02T08:00:00+08:00",
          endTime: "2026-08-02T09:00:00+08:00"
        },
        {
          recordKey: "recording-two",
          name: "录像二",
          startTime: "2026-08-02T09:00:00+08:00",
          endTime: "2026-08-02T10:00:00+08:00"
        }
      ]
    }
  };
}

// ⛔⛔ 每个用例结束必须卸载组件。不卸载的话，组件内部的轮询定时器(800ms)与
// 「等通道释放」的探测循环会留在**同一个 fake-timer 队列**里，被下一条用例的
// `advanceTimersByTimeAsync` 一起推进 ⇒ `getPlaybackSession` 的调用次数随机膨胀
// （实测 18 / 42 / 85 / 111，全量跑必红、单文件跑偶然全绿）。判据：只加卸载、
// 不改任何断言，全量跑即转绿。
enableAutoUnmount(afterEach);

describe("device record playback workspace", () => {
  beforeEach(() => {
    vi.useRealTimers();
    account.permissions = ["gb28181:device-record:query", "gb28181:device-record:play", "gb28181:device-record:download"];
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        blob: vi.fn().mockResolvedValue(new Blob(["recording"]))
      })
    );
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: vi.fn(() => "blob:download") });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: vi.fn() });
    sessionStorage.clear();
    api.routerPush.mockReset();
    messageError.mockReset();
    api.getRecordQueryOptions.mockResolvedValue({
      code: 0,
      data: {
        device: { id: 7, code: "34020000002000000001", name: "园区 NVR-A", online: true },
        channel: { id: 31, code: "34020000001320000001", name: "东门出入口" },
        timezone: "Asia/Shanghai",
        serverNow: "2026-08-02T19:40:20+08:00",
        maxRangeHours: 24,
        timeoutSeconds: 15,
        supportedTypes: ["all", "manual", "alarm"]
      }
    });
    api.queryDeviceRecords.mockResolvedValue({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 1,
        receivedCount: 1,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 842,
        list: [
          {
            recordKey: "opaque-record-key",
            deviceId: "34020000001320000001",
            name: "上午巡检录像",
            filePath: "/record/001.dav",
            address: "园区东门",
            startTime: "2026-08-02T08:10:00+08:00",
            endTime: "2026-08-02T08:42:16+08:00",
            secrecy: 0,
            type: "time",
            recorderId: "NVR-A",
            fileSize: 248635904,
            recordLocation: "34020000002000000001",
            streamNumber: 0
          }
        ]
      }
    });
    // 缓存任务的受理结果与后续轮询都返回"正在跑"的同一份快照：
    // 这样进度弹窗能显示速率，且不会因为进入终态而提前停轮询（停轮询的路径
    // 由 record-cache 页自己的用例覆盖）。
    const cacheTask = {
      taskId: "cache-1",
      state: "running",
      downloadSpeed: 4,
      cachedBytes: 10485760,
      cachedSeconds: 300,
      totalSeconds: 1936,
      progress: 0.15,
      speedBytesPerSec: 2097152,
      files: [],
      lastError: ""
    };
    api.createRecordCacheTask.mockResolvedValue({ code: 0, data: cacheTask });
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: cacheTask });
    api.createPlaybackSession.mockResolvedValue({
      code: 0,
      data: {
        sessionId: "session-1",
        state: "playing",
        channelId: "31",
        recordKey: "opaque-record-key",
        segmentStart: "2026-08-02T08:10:00+08:00",
        segmentEnd: "2026-08-02T08:42:16+08:00",
        positionSeconds: 0,
        scale: 1,
        hasAudio: false,
        media: { urls: { wsFlv: "ws://zlm/playback/session-1.live.flv" } },
        expiresAt: "2026-08-02T09:10:00+08:00",
        errorStage: "",
        errorCode: ""
      }
    });
    api.getPlaybackSession.mockResolvedValue({
      code: 0,
      data: {
        sessionId: "session-1",
        state: "playing",
        channelId: "31",
        recordKey: "opaque-record-key",
        segmentStart: "2026-08-02T08:10:00+08:00",
        segmentEnd: "2026-08-02T08:42:16+08:00",
        positionSeconds: 0,
        scale: 1,
        hasAudio: false,
        media: { urls: { wsFlv: "ws://zlm/playback/session-1.live.flv" } },
        expiresAt: "2026-08-02T09:10:00+08:00",
        errorStage: "",
        errorCode: ""
      }
    });
    api.actionPlaybackSession.mockImplementation((_channelId, _sessionId, action) =>
      Promise.resolve({
        code: 0,
        data: {
          ...(api.createPlaybackSession.mock.results[0]?.value?.data || {}),
          sessionId: "session-1",
          state: action.action === "pause" ? "paused" : "playing",
          recordKey: "opaque-record-key",
          segmentStart: "2026-08-02T08:10:00+08:00",
          segmentEnd: "2026-08-02T08:42:16+08:00",
          positionSeconds: action.positionSeconds || 0,
          scale: action.scale || 1,
          hasAudio: false,
          media: { urls: { wsFlv: "ws://zlm/playback/session-1.live.flv" } }
        }
      })
    );
    api.deletePlaybackSession.mockResolvedValue({ code: 0, data: { state: "stopped" } });
  });

  it("fits the playback workspace into its layout host instead of the browser viewport", () => {
    expect(playbackPageSource).toContain('class="snow-fill-inner uvp-page-shell-flat playback-workspace"');
    expect(hasRuleBlock(playbackPageSource, ".record-playback-page", "height: 100%", "min-height: 0")).toBe(true);
    expect(
      hasRuleBlock(
        playbackPageSource,
        ".playback-workspace",
        "display: flex",
        "flex-direction: column",
        "height: 100%",
        "min-height: 0"
      )
    ).toBe(true);
    expect(hasRuleBlock(playbackPageSource, ".timeline-panel", "flex: none")).toBe(true);
    expect(playbackPageSource).not.toContain("min-height: 100vh");
  });

  it("keeps the query bar and transport controls on Arco components", () => {
    // 这一页原先整套用原生 input[type=datetime-local] / select / button 手描外观。
    // 换成 Arco 后要钉住,否则后续改动很容易静默退回原生控件。
    expect(playbackPageSource).toContain("<a-range-picker");
    expect(playbackPageSource).toContain('value-format="YYYY-MM-DDTHH:mm:ss"');
    expect(playbackPageSource).toContain("<a-select");
    expect(playbackPageSource).toContain("<a-button");
    expect(playbackPageSource).not.toContain('type="datetime-local"');
    expect(playbackPageSource).not.toContain("<select");
    expect(playbackPageSource).not.toContain("<option");
    expect(playbackPageSource).not.toContain("<input");
    // ⛔ 查询栏不许用 <label> 包 Arco 控件：label 会把点击转发给它的关联控件（a-select 里那个
    // 隐藏的 readonly input），一次点击变成两次开合 ⇒ 录像类型下拉弹出来立刻收回去。
    expect(playbackPageSource).not.toMatch(/<label[^>]*class="query-field/);
    // ⛔ 段卡片是刻意保留的原生 button:74px 高、内嵌三行内容的卡片式列表项,
    // 不是按钮控件。换成 a-button 得把它整套外观再覆盖回去,收益为零。
    expect(playbackPageSource).toContain("['segment-item'");
  });

  it("uses the device-list search panel language for the query area", () => {
    expect(playbackPageSource).toContain("background: var(--uvp-search-panel-bg);");
    expect(playbackPageSource).toContain("border: 1px solid var(--uvp-list-panel-border);");
    expect(playbackPageSource).toContain("border-radius: var(--uvp-panel-radius);");
    expect(playbackPageSource).toContain("box-shadow: var(--uvp-search-panel-shadow);");
    expect(
      hasRuleBlock(playbackPageSource, ".channel-context", "display: flex", "align-items: center", "gap: 9px", "min-width: 260px")
    ).toBe(true);
  });

  it("aligns and rounds the query, playback, and timeline regions", () => {
    expect(hasRuleBlock(playbackPageSource, ".query-bar", "min-height: 72px", "margin: 0 8px 10px")).toBe(true);
    expect(
      hasRuleBlock(
        playbackPageSource,
        ".playback-main",
        "display: grid",
        "grid-template-columns: minmax(0, 1fr) 312px",
        "flex: 1",
        "min-height: 0",
        "margin: 0 8px"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        playbackPageSource,
        ".playback-main",
        "background: var(--uvp-shell-muted)",
        "border: 1px solid var(--uvp-panel-border)",
        "border-radius: var(--uvp-panel-radius)",
        "overflow: hidden"
      )
    ).toBe(true);
    expect(
      hasRuleBlock(
        playbackPageSource,
        ".timeline-panel",
        "flex: none",
        "margin: 10px 8px 12px",
        "border-radius: var(--uvp-panel-radius)",
        "overflow: hidden"
      )
    ).toBe(true);
    // 窄屏改成纵向堆叠：同样要求这几条同处一块（宽屏那块是 grid，不会被误判）。
    expect(hasRuleBlock(playbackPageSource, ".playback-main", "display: flex", "flex: none", "flex-direction: column")).toBe(
      true
    );
  });

  it("uses a card selection state and a compact recording type dot", () => {
    expect(playbackPageSource).toContain(":aria-current=\"playback.recordKey === record.recordKey ? 'true' : undefined\"");
    expect(
      hasRuleBlock(
        playbackPageSource,
        ".segment-item.selected",
        "z-index: 1",
        "background: var(--uvp-table-row-checked-bg)",
        "border-color: var(--uvp-brand)"
      )
    ).toBe(true);
    expect(hasRuleBlock(playbackPageSource, ".segment-marker", "flex: none", "width: 7px", "height: 7px")).toBe(true);
    expect(playbackPageSource).not.toContain("box-shadow: inset 3px 0 0 var(--uvp-brand)");
  });

  it("returns to the dynamic device management route with its saved state key", async () => {
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await wrapper.get('[aria-label="返回设备管理"]').trigger("click");
    expect(api.routerPush).toHaveBeenCalledWith({
      name: "device-mgmt-list",
      query: { returnKey: "return-key" }
    });
  });

  it("cleans up with the channel id captured before leaving the route", () => {
    expect(playbackPageSource).toContain("const playbackChannelId = channelId.value;");
    // ⛔ 别把整行钉进断言:prettier 会把带第三个参数的调用拆成多行,格式化一次红一次。
    expect(hasMarkup(playbackPageSource, "getPlaybackSession(playbackChannelId, sessionId, { showErrorMessage: false })")).toBe(
      true
    );
    expect(hasMarkup(playbackPageSource, "deletePlaybackSession(playbackChannelId, sessionId,")).toBe(true);
    expect(playbackPageSource).not.toContain("deletePlaybackSession(channelId.value, sessionId).catch");
  });

  it("queries recordings automatically after loading the channel options", async () => {
    mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.getRecordQueryOptions).toHaveBeenCalledTimes(1);
    expect(api.queryDeviceRecords).toHaveBeenCalledTimes(1);
    expect(api.queryDeviceRecords).toHaveBeenCalledWith(31, expect.objectContaining({ type: "all" }), expect.any(AbortSignal));
  });

  it("writes a range picked on the Arco range picker back into the query payload", async () => {
    // 起止时间合成一个 a-range-picker 后，v-model 是二元组，而表单/序列化仍是 startTime、endTime
    // 两个字段（靠 timeRange 这个 computed 桥接）。这条用例钉住桥接的两个方向。
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    api.queryDeviceRecords.mockClear();

    const inputs = wrapper.findAll(".time-picker input");
    expect(inputs).toHaveLength(2);
    // get 方向：默认表单值要回显到控件上。
    expect((inputs[0].element as HTMLInputElement).value).toBe("2026-08-02T00:00:00");
    expect((inputs[1].element as HTMLInputElement).value).toBe("2026-08-02T19:40:20");

    // set 方向：改控件要写回表单，并原样进请求体（value-format 保证是裸字符串，不带时区后缀）。
    await inputs[0].setValue("2026-08-02T07:00:00");
    await inputs[1].setValue("2026-08-02T09:30:00");
    await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
    await flushPromises();

    expect(api.queryDeviceRecords).toHaveBeenCalledWith(
      31,
      expect.objectContaining({ startTime: "2026-08-02T07:00:00", endTime: "2026-08-02T09:30:00" }),
      expect.any(AbortSignal)
    );
  });

  it("does not query or render the playback workspace without query permission", async () => {
    account.permissions = ["gb28181:device-record:play"];
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.getRecordQueryOptions).not.toHaveBeenCalled();
    expect(api.queryDeviceRecords).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("无权查询设备录像");
  });

  it("allows query-only users to inspect results without opening a playback session", async () => {
    account.permissions = ["gb28181:device-record:query"];
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.queryDeviceRecords).toHaveBeenCalledTimes(1);
    expect(api.createPlaybackSession).not.toHaveBeenCalled();
    expect(wrapper.find('[data-testid="record-segment-0"]').attributes("aria-disabled")).toBe("true");
  });

  it("automatically plays the first playable recording instead of a one-second boundary fragment", async () => {
    api.queryDeviceRecords.mockResolvedValueOnce({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 2,
        receivedCount: 2,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 151,
        list: [
          {
            recordKey: "boundary-fragment",
            name: "边界片段",
            startTime: "2026-08-02T00:00:00+08:00",
            endTime: "2026-08-02T00:00:01+08:00"
          },
          {
            recordKey: "playable-recording",
            name: "有效录像",
            startTime: "2026-08-02T00:00:01+08:00",
            endTime: "2026-08-02T00:32:37+08:00"
          }
        ]
      }
    });

    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(wrapper.get('[data-testid="record-segment-1"]').classes()).toContain("selected");
    expect(wrapper.get('[data-testid="record-segment-0"]').classes()).not.toContain("selected");
    expect(api.createPlaybackSession).toHaveBeenCalledWith(
      31,
      { recordKey: "playable-recording", playFrom: "2026-08-02T00:00:01+08:00" },
      expect.any(String),
      suppressed
    );
  });

  it("keeps a stable idempotency key and restores the last recording after remount", async () => {
    const firstWrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    const firstRequest = api.createPlaybackSession.mock.calls[0];
    firstWrapper.unmount();

    api.createPlaybackSession.mockClear();
    const secondWrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    expect(api.createPlaybackSession.mock.calls[0][2]).toBe(firstRequest[2]);
    expect(secondWrapper.get('[data-testid="record-segment-0"]').classes()).toContain("selected");
  });

  it("stops the active session before automatically playing another recording", async () => {
    api.queryDeviceRecords.mockResolvedValueOnce({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 2,
        receivedCount: 2,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 151,
        list: [
          {
            recordKey: "recording-one",
            name: "录像一",
            startTime: "2026-08-02T00:00:01+08:00",
            endTime: "2026-08-02T00:32:37+08:00"
          },
          {
            recordKey: "recording-two",
            name: "录像二",
            startTime: "2026-08-02T00:32:37+08:00",
            endTime: "2026-08-02T01:19:20+08:00"
          }
        ]
      }
    });
    let finishStop!: () => void;
    api.deletePlaybackSession.mockReturnValueOnce(
      new Promise(resolve => {
        finishStop = () => resolve({ code: 0, data: { state: "stopped" } });
      })
    );
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    await wrapper.get('[data-testid="record-segment-1"]').trigger("click");
    await Promise.resolve();
    expect(api.deletePlaybackSession).toHaveBeenCalledWith(31, "session-1", suppressed);
    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    expect(wrapper.get('[data-testid="playback-loading"]').text()).toContain("正在停止");

    finishStop();
    await flushPromises();
    expect(api.createPlaybackSession).toHaveBeenCalledTimes(2);
    expect(api.createPlaybackSession).toHaveBeenLastCalledWith(
      31,
      { recordKey: "recording-two", playFrom: "2026-08-02T00:32:37+08:00" },
      expect.any(String),
      suppressed
    );
    expect(wrapper.find('[data-testid="playback-loading"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="playback-player"]').exists()).toBe(true);
  });

  it("does not start another recording when stopping the active session fails", async () => {
    api.queryDeviceRecords.mockResolvedValueOnce({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 2,
        receivedCount: 2,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 151,
        list: [
          {
            recordKey: "recording-one",
            name: "录像一",
            startTime: "2026-08-02T00:00:01+08:00",
            endTime: "2026-08-02T00:32:37+08:00"
          },
          {
            recordKey: "recording-two",
            name: "录像二",
            startTime: "2026-08-02T00:32:37+08:00",
            endTime: "2026-08-02T01:19:20+08:00"
          }
        ]
      }
    });
    api.deletePlaybackSession.mockRejectedValueOnce(new Error("stop failed"));
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    await wrapper.get('[data-testid="record-segment-1"]').trigger("click");
    await flushPromises();

    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    expect(wrapper.get('[data-testid="record-segment-0"]').classes()).toContain("selected");
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("回放失败");
    expect(messageError).toHaveBeenLastCalledWith("stop failed");
  });

  it("waits for the channel to be released after a teardown timeout instead of hanging", async () => {
    vi.useFakeTimers();
    api.deletePlaybackSession.mockRejectedValueOnce(teardownStall());
    // 第一轮探测:占位仍在,通道没释放;第二轮:会话已从平台消失 = 释放了。
    api.getPlaybackSession.mockResolvedValueOnce(heldSession()).mockRejectedValueOnce(sessionGone());
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    await wrapper.get('[aria-label="停止"]').trigger("click");
    await flushPromises();

    // ⛔ 不轮询 DELETE:绑定保留时它推不动任何状态(那条 TEARDOWN 不会重发),
    // 只会再白等一个 SIP 超时。观察释放只能靠「会话还在不在」。
    expect(api.deletePlaybackSession).toHaveBeenCalledTimes(1);
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在等待通道释放");
    expect(messageError).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(500);
    await flushPromises();
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在等待通道释放");

    await vi.advanceTimersByTimeAsync(800);
    await flushPromises();
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("已停止");
    expect(messageError).not.toHaveBeenCalled();
  });

  it("reports the teardown failure once when the channel never frees up", async () => {
    vi.useFakeTimers();
    api.deletePlaybackSession.mockRejectedValueOnce(teardownStall());
    api.getPlaybackSession.mockResolvedValue(heldSession());
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    await wrapper.get('[aria-label="停止"]').trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(20_000);
    await flushPromises();

    expect(api.deletePlaybackSession).toHaveBeenCalledTimes(1);
    expect(api.getPlaybackSession).toHaveBeenCalledTimes(channelReleaseWatchBudget);
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("回放失败");
    // 中间失败全被抑制,只留一条如实文案。
    expect(messageError).toHaveBeenCalledTimes(1);
    expect(messageError).toHaveBeenLastCalledWith("设备未确认拆除，通道仍在后台释放中，请稍后重试");
  });

  it("waits out a busy channel and retries session creation with the same idempotency key", async () => {
    vi.useFakeTimers();
    // 上一路刚结束、后台清扫器还没释放:平台固定回 429。
    api.createPlaybackSession.mockRejectedValueOnce(channelBusy());
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在等待通道释放");
    expect(messageError).not.toHaveBeenCalled();

    await vi.advanceTimersByTimeAsync(600);
    await flushPromises();

    expect(api.createPlaybackSession).toHaveBeenCalledTimes(2);
    // 同一幂等键重试 ⇒ 平台去重,不会因为重试生成第二路会话。
    expect(api.createPlaybackSession.mock.calls[0][2]).toBe(api.createPlaybackSession.mock.calls[1][2]);
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在回放");
    // ⛔ wrapper.get(...).exists() 是 TS2339(返回类型被 Omit 掉 exists),只能用 find。
    expect(wrapper.find('[data-testid="playback-player"]').exists()).toBe(true);
    expect(messageError).not.toHaveBeenCalled();
  });

  it("retries the teardown before switching recordings after a failed stop", async () => {
    vi.useFakeTimers();
    api.queryDeviceRecords.mockResolvedValueOnce(twoRecordings());
    // 第一次停止:设备没确认拆除,通道一直没释放 → 如实失败,但会话 id 保留。
    api.deletePlaybackSession.mockRejectedValueOnce(teardownStall());
    api.getPlaybackSession.mockResolvedValue(heldSession());
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    await wrapper.get('[aria-label="停止"]').trigger("click");
    await flushPromises();
    await vi.advanceTimersByTimeAsync(20_000);
    await flushPromises();
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("回放失败");

    // 再拖动到另一段:这次后台已经把通道释放了。⛔ 判据不能只看 status ——
    // 停在 "failed" 但会话 id 还在,必须先重新拆干净,否则只会撞 429。
    api.createPlaybackSession.mockClear();
    wrapper.findComponent(RecordTimeline).vm.$emit("locate", {
      recordKey: "recording-two",
      time: "2026-08-02T09:12:34+08:00"
    });
    await flushPromises();
    await vi.advanceTimersByTimeAsync(100);
    await flushPromises();

    expect(api.deletePlaybackSession).toHaveBeenCalledTimes(2);
    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    expect(api.createPlaybackSession).toHaveBeenLastCalledWith(
      31,
      { recordKey: "recording-two", playFrom: "2026-08-02T09:12:34+08:00" },
      expect.any(String),
      suppressed
    );
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在回放");
  });

  it("replaces the idle cover with the player after automatic playback starts", async () => {
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    const viewport = wrapper.get('[data-testid="playback-viewport"]');
    expect(viewport.find('[data-testid="playback-idle-cover"]').exists()).toBe(false);
    expect(viewport.find('[data-testid="playback-player"]').exists()).toBe(true);
    expect(viewport.find(".camera-scene").exists()).toBe(false);
    expect(viewport.text()).not.toContain("设备录像 MOCK");
  });

  it("keeps only the channel label over the video surface", () => {
    expect(playbackPageSource).toContain('class="video-overlay top-overlay"');
    expect(playbackPageSource).toContain("CH-01 {{ options?.channel.name");
    expect(playbackPageSource).not.toContain('class="video-overlay bottom-overlay"');
    expect(playbackPageSource).not.toContain('class="stream-badge"');
    expect(playbackPageSource).not.toContain("localDateTime(playback.currentTime");
  });

  it("keeps the timeline as the only dynamic current-time display", () => {
    expect(playbackPageSource).toContain('@timeupdate="handlePlayerTimeUpdate"');
    expect(playbackPageSource).toContain('@loading="handlePlayerLoading"');
    expect(playbackPageSource).not.toContain('data-testid="playback-time"');
    expect(timelineSource).toContain('data-testid="timeline-range"');
    expect(timelineSource).toContain("当前录像 · {{ selectedRecordRangeText }}");
    expect(timelineSource).not.toContain("当前视窗 · {{ visibleRangeText }}");
  });

  it("renders the five-zone playback workspace and query result", async () => {
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="playback-query-bar"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="playback-viewport"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="record-segment-list"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="playback-controls"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="record-timeline"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("上午巡检录像");
    expect(wrapper.get('[data-testid="record-segment-size-0"]').text()).toBe("237.1 MB");
    expect(wrapper.find('[aria-label="静音"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="取消静音"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="音量"]').exists()).toBe(false);
  });

  it("does not render a file size placeholder when the device omits FileSize", async () => {
    api.queryDeviceRecords.mockResolvedValueOnce({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 1,
        receivedCount: 1,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 120,
        list: [
          {
            recordKey: "without-file-size",
            name: "未返回大小的录像",
            startTime: "2026-08-02T09:00:00+08:00",
            endTime: "2026-08-02T09:30:00+08:00",
            fileSize: null
          }
        ]
      }
    });

    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(wrapper.find('[data-testid="record-segment-size-0"]').exists()).toBe(false);
  });

  it("creates a real playback session and renders its media URL", async () => {
    vi.useFakeTimers();
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    expect(api.createPlaybackSession).toHaveBeenCalledWith(
      31,
      { recordKey: "opaque-record-key", playFrom: "2026-08-02T08:10:00+08:00" },
      expect.any(String),
      suppressed
    );
    expect(wrapper.get('[data-testid="playback-status"]').text()).toContain("正在回放");
    expect(wrapper.get('[data-testid="playback-player"]').attributes("data-media-url")).toBe(
      "ws://zlm/playback/session-1.live.flv"
    );
    const player = wrapper.findComponent({ name: "PlayWindow" });
    player.vm.$emit("timeupdate", 10_000);
    player.vm.$emit("timeupdate", 11_000);
    await flushPromises();
    expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("08:10:01");
  });

  it("uses the session protocol snapshot and does not reselect it during polling", async () => {
    vi.useFakeTimers();
    api.createPlaybackSession.mockResolvedValueOnce({
      code: 0,
      data: {
        sessionId: "session-webrtc",
        state: "playing",
        channelId: "31",
        recordKey: "opaque-record-key",
        segmentStart: "2026-08-02T08:10:00+08:00",
        segmentEnd: "2026-08-02T08:42:16+08:00",
        positionSeconds: 0,
        scale: 1,
        hasAudio: false,
        media: {
          defaultProtocol: "webrtc",
          protocol: "webrtc",
          url: "https://zlm/index/api/webrtc?stream=one",
          zlmWebrtc: true,
          urls: { wsFlv: "ws://zlm/old.live.flv" }
        },
        expiresAt: "2026-08-02T09:10:00+08:00",
        errorStage: "",
        errorCode: ""
      }
    });
    api.getPlaybackSession.mockResolvedValueOnce({
      code: 0,
      data: {
        sessionId: "session-webrtc",
        state: "playing",
        channelId: "31",
        recordKey: "opaque-record-key",
        segmentStart: "2026-08-02T08:10:00+08:00",
        segmentEnd: "2026-08-02T08:42:16+08:00",
        positionSeconds: 1,
        scale: 1,
        hasAudio: false,
        media: { urls: { wsFlv: "ws://zlm/changed.live.flv" } },
        expiresAt: "2026-08-02T09:10:00+08:00",
        errorStage: "",
        errorCode: ""
      }
    });
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    expect(wrapper.get('[data-testid="playback-player"]').attributes("data-media-url")).toBe(
      "webrtc://zlm/index/api/webrtc?stream=one"
    );
    expect(wrapper.findComponent({ name: "PlayWindow" }).props("zlmWebrtc")).toBe(true);

    await vi.advanceTimersByTimeAsync(800);
    await flushPromises();
    expect(wrapper.get('[data-testid="playback-player"]').attributes("data-media-url")).toBe(
      "webrtc://zlm/index/api/webrtc?stream=one"
    );
  });

  it("freezes the timeline while the media is buffering", async () => {
    vi.useFakeTimers();
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    const player = wrapper.findComponent({ name: "PlayWindow" });

    player.vm.$emit("timeupdate", 10_000);
    player.vm.$emit("loading", true);
    player.vm.$emit("timeupdate", 13_000);
    await vi.advanceTimersByTimeAsync(3000);
    expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("08:10:00");

    player.vm.$emit("loading", false);
    player.vm.$emit("timeupdate", 11_000);
    await flushPromises();
    expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("08:10:01");
  });

  it("sends playback controls and stops the active session", async () => {
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
    await flushPromises();

    await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
    await flushPromises();
    expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "pause" });

    await wrapper.get('[data-testid="playback-primary-action"]').trigger("click");
    await flushPromises();
    expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "resume" });

    await wrapper.get('[data-testid="playback-scale"]').setValue("2");
    await flushPromises();
    expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "scale", scale: 2 });

    wrapper.findComponent(RecordTimeline).vm.$emit("locate", {
      recordKey: "opaque-record-key",
      time: "2026-08-02T08:10:42+08:00"
    });
    await flushPromises();
    expect(api.actionPlaybackSession).toHaveBeenCalledWith(31, "session-1", { action: "seek", positionSeconds: 42 });

    await wrapper.get('[aria-label="停止"]').trigger("click");
    await flushPromises();
    expect(api.deletePlaybackSession).toHaveBeenCalledWith(31, "session-1", suppressed);
  });

  it("switches recordings and starts playback from the time released on the timeline", async () => {
    api.queryDeviceRecords.mockResolvedValueOnce({
      code: 0,
      data: {
        status: "complete",
        declaredTotal: 2,
        receivedCount: 2,
        incomplete: false,
        timezone: "Asia/Shanghai",
        elapsedMs: 151,
        list: [
          {
            recordKey: "recording-one",
            name: "录像一",
            startTime: "2026-08-02T08:00:00+08:00",
            endTime: "2026-08-02T09:00:00+08:00"
          },
          {
            recordKey: "recording-two",
            name: "录像二",
            startTime: "2026-08-02T09:00:00+08:00",
            endTime: "2026-08-02T10:00:00+08:00"
          }
        ]
      }
    });
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    wrapper.findComponent(RecordTimeline).vm.$emit("locate", {
      recordKey: "recording-two",
      time: "2026-08-02T09:12:34+08:00"
    });
    await flushPromises();

    expect(api.deletePlaybackSession).toHaveBeenCalledWith(31, "session-1", suppressed);
    expect(api.createPlaybackSession).toHaveBeenCalledTimes(2);
    expect(api.createPlaybackSession).toHaveBeenLastCalledWith(
      31,
      { recordKey: "recording-two", playFrom: "2026-08-02T09:12:34+08:00" },
      expect.any(String),
      suppressed
    );
    expect(wrapper.get('[data-testid="timeline-playhead"]').text()).toContain("09:12:34");
  });

  it("submits a server-side cache task for the selected recording", async () => {
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
    await flushPromises();
    const download = wrapper.get('[data-testid="playback-download"]');
    expect(download.attributes("disabled")).toBeUndefined();
    await download.trigger("click");
    await flushPromises();
    // 提交的是**缓存任务**：拉流与落盘都在服务端，浏览器不再 fetch 直播流存 blob。
    expect(api.createRecordCacheTask).toHaveBeenCalledWith({
      channelId: 31,
      recordKey: "opaque-record-key",
      playFrom: "2026-08-02T08:10:00+08:00"
    });
    // 受理成功后弹出实时进度弹窗（看得见速率，也能随时转后台）。
    await flushPromises();
    expect(wrapper.find('[data-testid="record-cache-progress"]').exists()).toBe(true);
  });

  it("keeps playback controls for the visitor but hides every device-record download entry", async () => {
    account.permissions = ["gb28181:device-record:query", "gb28181:device-record:play"];
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();

    expect(api.createPlaybackSession).toHaveBeenCalledTimes(1);
    expect(wrapper.find('[data-testid="playback-primary-action"]').exists()).toBe(true);
    expect(wrapper.find('[aria-label="停止"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="playback-scale"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="playback-download"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="record-segment-download-0"]').exists()).toBe(false);
  });

  it("offers an independent download entry on every record segment", async () => {
    const wrapper = mount(DeviceRecordPlayback, { global: { stubs: { teleport: true } } });
    await flushPromises();
    await wrapper.get('[data-testid="record-query-submit"]').trigger("click");
    await flushPromises();
    const download = wrapper.get('[data-testid="record-segment-download-0"]');
    expect(download.attributes("aria-label")).toBe("缓存 上午巡检录像 到服务器");
    await download.trigger("click");
    await flushPromises();
    expect(api.createRecordCacheTask).toHaveBeenCalledWith({
      channelId: 31,
      recordKey: "opaque-record-key",
      playFrom: "2026-08-02T08:10:00+08:00"
    });
  });
});
