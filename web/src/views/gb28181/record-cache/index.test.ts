import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { hasMarkup } from "@/test/source-assert";
import { recordCacheDownloadContentPath, type RecordCacheFile, type RecordCacheTask } from "@/api/recordCache";
import RecordCachePage from "./index.vue";

const api = vi.hoisted(() => ({
  listRecordCacheTasks: vi.fn(),
  getRecordCacheTask: vi.fn(),
  cancelRecordCacheTask: vi.fn(),
  deleteRecordCacheTask: vi.fn(),
  createRecordCacheDownload: vi.fn(),
  setRecordCacheFavorite: vi.fn()
}));

const account = vi.hoisted(() => ({
  permissions: [] as string[]
}));

const messages = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }));
const modalWarning = vi.hoisted(() => vi.fn());

// 只保留接口的 mock，格式化函数（formatByteSize / formatDuration）用原实现 ——
// 它们是纯函数，钉住真实输出才能让「15% / 4× / 2.0 MB/s」这类断言有意义。
vi.mock("@/api/recordCache", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/recordCache")>()),
  ...api
}));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));

vi.mock("@arco-design/web-vue", () => ({
  Message: { success: messages.success, error: messages.error, warning: messages.warning },
  Modal: { warning: modalWarning }
}));

const cacheTable = {
  props: ["data", "loading"],
  provide() {
    return { cacheTable: this };
  },
  template:
    "<div data-testid='cache-table' :data-count='data.length'><slot name='columns' /><slot v-if='!data.length' name='empty' /></div>"
};

const stubs = {
  // 页面骨架用标准搜索面板（`src/components/s-layout-search`）。测试里必须给出这个桩 ——
  // vitest 没有开组件自动注册，真组件会以"未解析组件"的形式渲染，具名插槽内容会整个丢掉。
  "s-layout-search": {
    template: "<div class='cache-search-panel'><slot name='fields' /><slot name='actions' /></div>"
  },
  "a-table": cacheTable,
  "a-table-column": {
    props: ["title"],
    inject: ["cacheTable"],
    template:
      "<span :data-title='title'><template v-for='record in cacheTable.data'><slot name='cell' :record='record' /></template></span>"
  },
  "a-button": {
    props: ["loading", "disabled", "type", "size", "status"],
    emits: ["click"],
    template: "<button :disabled='disabled' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
  },
  // 操作列走 `a-link`（与同仓 34 个列表页一致）。这里也渲染成 <button>，
  // 于是 `buttonByText` 这类按文案定位的用例对两种标签都成立。
  // ⚠️ Arco Link 是带 `loading` / `disabled` 的（link.d.ts），别把它 stub 成无状态标签。
  "a-link": {
    props: ["loading", "disabled", "status"],
    emits: ["click"],
    template: "<button :disabled='disabled' @click='$emit(`click`)'><slot name='icon' /><slot /></button>"
  },
  "a-select": {
    props: ["modelValue", "options"],
    emits: ["update:modelValue", "change"],
    template: "<select :value='modelValue' @change=\"$emit('update:modelValue', $event.target.value)\"><slot /></select>"
  },
  "a-option": { props: ["value"], template: "<option :value='value'><slot /></option>" },
  "a-input": {
    props: ["modelValue"],
    emits: ["update:modelValue", "press-enter"],
    template: "<input class='search-input' />"
  },
  "a-empty": { props: ["description"], template: "<span>{{ description }}</span>" },
  "a-tag": { template: "<span class='cache-tag'><slot /></span>" },
  "a-progress": {
    props: ["percent", "status", "showText", "size"],
    template: "<div class='cache-progress' :data-percent='percent' />"
  },
  "a-dropdown": { template: "<span><slot /><slot name='content' /></span>" },
  "a-doption": {
    props: ["disabled"],
    emits: ["click"],
    template: "<button class='cache-doption' :disabled='disabled' @click='$emit(`click`)'><slot /></button>"
  },
  "a-pagination": {
    props: ["current", "pageSize", "total", "pageSizeOptions"],
    emits: ["change", "page-size-change"],
    template: "<div data-testid='cache-pager' />"
  }
};

function file(overrides: Partial<RecordCacheFile> = {}): RecordCacheFile {
  return {
    index: 0,
    name: "缓存-第1段.mp4",
    size: 10485760,
    startTime: "2026-08-02T08:10:00+08:00",
    endTime: "2026-08-02T08:30:00+08:00",
    downloadable: true,
    ...overrides
  };
}

function task(overrides: Partial<RecordCacheTask> = {}): RecordCacheTask {
  return {
    taskId: "t1",
    channelId: 31,
    deviceId: "34020000001320000001",
    // ⛔ 刻意让通道编码与设备编码**不同**：两个字段同值的话，
    //    「通道 ID 列渲染的是 channelCode 还是 deviceId」这类断言就测了个寂寞。
    channelCode: "34020000001320000002",
    channelName: "东门出入口",
    deviceName: "一号 NVR",
    startTime: "2026-08-02T08:10:00+08:00",
    endTime: "2026-08-02T08:42:16+08:00",
    recordType: "time",
    downloadSpeed: 4,
    state: "running",
    lastError: "",
    cachedBytes: 10485760,
    cachedSeconds: 300,
    totalSeconds: 1936,
    progress: 0.15,
    speedBytesPerSec: 2097152,
    estimatedBytes: 0,
    favorite: false,
    files: [],
    createdByName: "admin",
    createdAt: "2026-10-03T16:00:00+08:00",
    startedAt: "2026-10-03T16:00:05+08:00",
    finishedAt: "",
    expiresAt: "2026-10-10T16:00:00+08:00",
    ...overrides
  };
}

function page(list: RecordCacheTask[]) {
  return { code: 0, data: { list, total: list.length, page: 1, size: 20 } };
}

function buttonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const target = wrapper.findAll("button").find(candidate => candidate.text().trim() === text);
  if (!target) throw new Error(`没有找到文案为「${text}」的按钮`);
  return target;
}

/**
 * 后端「签发一次下载」的应答桩。
 *
 * ⛔ contentUrl 必须用**真实现**（`recordCacheDownloadContentPath`）算出来：
 * 页面会把它跟后端给的地址逐字比对，桩里手写一个"看起来像"的字符串
 * 会让用例要么假通过（页面把地址当成异常），要么测不到那条比对本身。
 */
function downloadCreation(index?: number) {
  return {
    code: 0,
    data: {
      task: { taskId: "dl-1", status: "ready", bytesSent: 0 },
      contentUrl: recordCacheDownloadContentPath("dl-1", index)
    }
  };
}

/**
 * 抓住被交给浏览器的那个 `<a>`：href 与 download 就是"浏览器原生下载"的全部输入。
 *
 * ⛔ 用 click 时的 `this` 而不是 `document.createElement` 的 spy：页面在 click 之后
 * 就把节点移出文档了，只有 click 那一刻的 `this` 一定还是刚配好的那一个。
 */
function captureDownloadAnchor() {
  const anchors: HTMLAnchorElement[] = [];
  vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
    anchors.push(this);
  });
  return anchors;
}

enableAutoUnmount(afterEach);

describe("record cache workspace", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // 默认给全套权限：权限差异单独由用例覆盖。
    account.permissions = [
      "gb28181:record-cache:view",
      "gb28181:record-cache:create",
      "gb28181:record-cache:cancel",
      "gb28181:record-cache:download",
      "gb28181:record-cache:delete",
      "gb28181:record-cache:favorite"
    ];
    api.listRecordCacheTasks.mockResolvedValue(page([task()]));
    // 详情接口默认回与列表同值的任务：列表里活动态的行会补问一次详情（见 mergeLiveProgress）。
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task() });
    api.cancelRecordCacheTask.mockResolvedValue({ code: 0, data: {} });
    api.deleteRecordCacheTask.mockResolvedValue({ code: 0, data: { taskId: "t1" } });
    api.createRecordCacheDownload.mockImplementation((_taskId: string, index?: number) => downloadCreation(index));
    // 收藏接口默认回包就是「写进去的那个值」。
    api.setRecordCacheFavorite.mockImplementation((_taskId: string, favorite: boolean) => ({
      code: 0,
      data: task({ favorite })
    }));
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
  });

  it("挂载后按默认分页拉取任务并渲染进度与实时速率", async () => {
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    expect(api.listRecordCacheTasks).toHaveBeenCalledWith({
      page: 1,
      size: 20,
      state: undefined,
      keyword: undefined
    });
    expect(wrapper.get("[data-testid='cache-table']").attributes("data-count")).toBe("1");
    const markup = wrapper.text();
    expect(markup).toContain("东门出入口");
    expect(markup).toContain("缓存中");
    // 进度列三段信息：百分比（自己渲染的文字）+「已缓存时长 / 总时长」+ 实时速率。
    // ⛔ 百分比必须落在**文字**上：`a-progress` 只吃 0~1 比值（见下面的单位用例），
    //    文字则由 progressPercent() 从同一比值推出来。
    expect(markup).toContain("15%");
    expect(markup).toContain("5分0秒 / 32分16秒");
    expect(markup).toContain("10.0 MB");
    expect(markup).toContain("4×");
    expect(markup).toContain("2.0 MB/s");
  });

  it("进度百分比向下取整，99.9% 不会提前显示成 100%", async () => {
    // ⛔ 用四舍五入的话 0.999 ⇒ "100%"，用户看到满格却还在拉最后一片。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ progress: 0.999 })]));
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ progress: 0.999 }) });
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(wrapper.get(".record-cache-progress__percent").text()).toBe("99%");
  });

  it("进度条拿到的是 0~1 的比值，而不是百分数", async () => {
    // ⛔ 本仓曾把这条写反（"Arco 收 0~100"），2026-10-04 线上翻车：
    //    Arco 的 `barStyle.width` 是 `percent * 100%`，所以 percent 必须 0~1。
    //    传 0.15 → width:15%（对）；传 15 → width:1500%，被容器裁掉后就是
    //    "进度条永远顶满"，而别处显示的百分比文字却还是 7% —— 静默的视觉错。
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(wrapper.get(".cache-progress").attributes("data-percent")).toBe("0.15");
  });

  it("列表里正在跑的任务会补问一次详情，进度按轮询粒度刷新", async () => {
    // ⛔ 后端**有意**让列表接口不带实时进度（`TestListDoesNotQueryMediaServer`：
    //    N 条任务 = N 次媒体节点查询）。所以"列表里也要看到进度在动"只能前端补：
    //    对活动态的行逐条问详情。少了这一步，进度会在整片（最长 7.5 分钟墙钟）里纹丝不动。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ progress: 0.01, cachedSeconds: 0, speedBytesPerSec: 0 })]));
    api.getRecordCacheTask.mockResolvedValue({
      code: 0,
      data: task({ progress: 0.3, cachedSeconds: 540, cachedBytes: 300 * 1024 * 1024, speedBytesPerSec: 2 * 1024 * 1024 })
    });

    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    expect(api.getRecordCacheTask).toHaveBeenCalledWith("t1", { showErrorMessage: false });
    expect(wrapper.get(".cache-progress").attributes("data-percent")).toBe("0.3");
    expect(wrapper.get(".record-cache-progress__percent").text()).toBe("30%");
    expect(wrapper.text()).toContain("9分0秒");
    expect(wrapper.text()).toContain("2.0 MB/s");
  });

  it("终态任务一行都不问详情", async () => {
    api.listRecordCacheTasks.mockResolvedValue(
      page([task({ state: "succeeded", progress: 1 }), task({ taskId: "t2", state: "failed" })])
    );
    mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(api.getRecordCacheTask).not.toHaveBeenCalled();
  });

  it("迟到的详情回包不会把已经收尾的行改回进行中", async () => {
    const pending: Array<(value: unknown) => void> = [];
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "running", progress: 0.2 })]));
    api.getRecordCacheTask.mockImplementation(() => new Promise(resolve => pending.push(resolve)));

    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises(); // 第一轮：列表=running，详情还挂着
    expect(pending).toHaveLength(1);

    // 第二轮：列表这次拿到终态，且不再问详情。
    // ⛔ 用「查询」触发而不是「刷新」：第一轮还挂着的详情让 loading 停在 true 上，
    //    而刷新按钮是 `:disabled="loading"`（与其它列表页一致），此刻点不动。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1 })]));
    await buttonByText(wrapper, "查询").trigger("click");
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(2);

    // 现在把第一轮那条迟到的回包放出来 —— 它还在说 running
    pending.forEach(resolve => resolve({ code: 0, data: task({ state: "running", progress: 0.2 }) }));
    await flushPromises();

    expect(wrapper.findAll(".cache-tag").map(node => node.text())).toEqual(["已完成"]);
    expect(wrapper.get(".cache-progress").attributes("data-percent")).toBe("1");
    expect(wrapper.get(".record-cache-progress__percent").text()).toBe("100%");
  });

  it("筛选条件要点「查询」才生效", async () => {
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(1);

    await wrapper.get("select").setValue("failed");
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(1);

    await buttonByText(wrapper, "查询").trigger("click");
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenLastCalledWith({
      page: 1,
      size: 20,
      state: "failed",
      keyword: undefined
    });
  });

  it("表格里能看到设备 ID、通道 ID 和任务 ID", async () => {
    // 用户 2026-10-05 提的：只有通道名/设备名时对不上号，排查问题要能直接看到编码。
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    /** 取某一列渲染出来的全部文字（列桩会把它的单元格都塞在同一个 `[data-title]` 里）。 */
    const columnText = (title: string) => {
      const node = wrapper.findAll("[data-title]").find(candidate => candidate.attributes("data-title") === title);
      if (!node) throw new Error(`没有找到标题为「${title}」的列`);
      return node.text();
    };

    // ⛔ 逐列断言而不是 `wrapper.text()).toContain(...)`：后者在
    //    "通道 ID 列其实渲染成了 deviceId" 这种错法下**照样通过**（两个编码都在页面里）。
    expect(columnText("设备 ID")).toBe("34020000001320000001");
    // 通道 ID 取的是 channelCode（国标编码）—— 不是 `channelId`（库里的自增主键 31），
    // 那个号在设备侧根本不存在，显示出来只会让人对不上。
    expect(columnText("通道 ID")).toBe("34020000001320000002");
    expect(columnText("任务 ID")).toBe("t1");
  });

  it("点星标收藏：调接口、按回包回写、给出提示", async () => {
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    const star = wrapper.get(".uvp-table-action--favorite");
    expect(star.classes()).not.toContain("is-on");

    await star.trigger("click");
    await flushPromises();

    expect(api.setRecordCacheFavorite).toHaveBeenCalledWith("t1", true);
    expect(messages.success).toHaveBeenCalledWith("已收藏，该录像不会被自动清理");
    // 星标要变成实心（`is-on`）—— 只有这个类会让 lucide 的描边星补上 fill。
    expect(wrapper.get(".uvp-table-action--favorite").classes()).toContain("is-on");
  });

  it("星标状态以后端回包为准，不做本地乐观翻转", async () => {
    // ⛔ 这条是整个功能的命门：本地取反谁都会写，但写库失败时界面会显示"已取消收藏"、
    //    库里其实还是 1 —— 下次清理是"跳过"了，但用户以为自己已经把它放回清理范围，
    //    两边说法不一致却没有任何报错。所以状态必须以服务端回包为准。
    //    这里让后端明确回「还收藏着」（模拟这次取消没生效），界面就必须继续显示已收藏。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ favorite: true })]));
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ favorite: true }) });
    api.setRecordCacheFavorite.mockResolvedValue({ code: 0, data: task({ favorite: true }) });

    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(wrapper.get(".uvp-table-action--favorite").classes()).toContain("is-on");

    await wrapper.get(".uvp-table-action--favorite").trigger("click");
    await flushPromises();

    expect(api.setRecordCacheFavorite).toHaveBeenCalledWith("t1", false);
    // 回包说还收藏着 ⇒ 星标就得继续实心，不能因为"我点了取消"就翻成空心。
    expect(wrapper.get(".uvp-table-action--favorite").classes()).toContain("is-on");
    expect(messages.success).toHaveBeenCalledWith("已收藏，该录像不会被自动清理");
  });

  it("取消收藏成功（回包 favorite=false）后星标变回空心", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ favorite: true })]));
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ favorite: true }) });
    api.setRecordCacheFavorite.mockResolvedValue({ code: 0, data: task({ favorite: false }) });

    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await wrapper.get(".uvp-table-action--favorite").trigger("click");
    await flushPromises();

    expect(wrapper.get(".uvp-table-action--favorite").classes()).not.toContain("is-on");
    expect(messages.success).toHaveBeenCalledWith("已取消收藏");
  });

  it("收藏接口失败时提示失败，且不改动星标状态", async () => {
    api.setRecordCacheFavorite.mockRejectedValue(new Error("Network Error"));
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await wrapper.get(".uvp-table-action--favorite").trigger("click");
    await flushPromises();

    expect(messages.error).toHaveBeenCalledWith("操作失败，请重试");
    expect(wrapper.get(".uvp-table-action--favorite").classes()).not.toContain("is-on");
  });

  it("「只看收藏」筛选把 favorite=true 传下去，不选时压根不传这个字段", async () => {
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    // 默认不筛：`favorite` 必须是 undefined（传 false 会被后端当成"只看未收藏"）。
    expect(api.listRecordCacheTasks).toHaveBeenLastCalledWith({
      page: 1,
      size: 20,
      state: undefined,
      keyword: undefined
    });

    // 收藏筛选是第二个下拉（第一个是任务状态）。
    await wrapper.findAll("select")[1].setValue("true");
    await buttonByText(wrapper, "查询").trigger("click");
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenLastCalledWith({
      page: 1,
      size: 20,
      state: undefined,
      keyword: undefined,
      favorite: true
    });

    await wrapper.findAll("select")[1].setValue("false");
    await buttonByText(wrapper, "查询").trigger("click");
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenLastCalledWith({
      page: 1,
      size: 20,
      state: undefined,
      keyword: undefined,
      favorite: false
    });
  });

  it("「重置」要把收藏筛选一起清掉", async () => {
    // ⛔ 漏了它，"重置"之后列表还是被筛过的样子，用户会以为任务丢了。
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await wrapper.findAll("select")[1].setValue("true");
    await buttonByText(wrapper, "查询").trigger("click");
    await flushPromises();

    await buttonByText(wrapper, "重置").trigger("click");
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenLastCalledWith({
      page: 1,
      size: 20,
      state: undefined,
      keyword: undefined
    });
  });

  it("正在「只看收藏」时取消收藏，会重新拉一次列表（否则像是没生效）", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ favorite: true })]));
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ favorite: true }) });
    api.setRecordCacheFavorite.mockResolvedValue({ code: 0, data: task({ favorite: false }) });

    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await wrapper.findAll("select")[1].setValue("true");
    await buttonByText(wrapper, "查询").trigger("click");
    await flushPromises();
    const before = api.listRecordCacheTasks.mock.calls.length;

    await wrapper.get(".uvp-table-action--favorite").trigger("click");
    await flushPromises();

    expect(api.listRecordCacheTasks.mock.calls.length).toBe(before + 1);
  });

  it("没有收藏权限时星标不出现在表格里", async () => {
    account.permissions = ["gb28181:record-cache:view"];
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(wrapper.find(".uvp-table-action--favorite").exists()).toBe(false);
  });

  it("只有存在未完成任务时才开启轮询，卸载时停掉定时器", async () => {
    const setIntervalSpy = vi.spyOn(window, "setInterval");
    const clearIntervalSpy = vi.spyOn(window, "clearInterval");
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
    wrapper.unmount();
    expect(clearIntervalSpy).toHaveBeenCalled();
  });

  it("全部是终态任务时不挂轮询", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1 })]));
    const setIntervalSpy = vi.spyOn(window, "setInterval");
    mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(setIntervalSpy).not.toHaveBeenCalled();
  });

  it("取消任务会调用取消接口并重新拉取列表", async () => {
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await buttonByText(wrapper, "取消").trigger("click");
    await flushPromises();

    expect(api.cancelRecordCacheTask).toHaveBeenCalledWith("t1");
    expect(messages.success).toHaveBeenCalledWith("已取消缓存任务");
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(2);
  });

  it("删除要先经过确认弹窗，确认后才真正删除", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1 })]));
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await buttonByText(wrapper, "删除").trigger("click");
    expect(modalWarning).toHaveBeenCalledTimes(1);
    // 只点删除按钮不能落库：必须走到确认回调里。
    expect(api.deleteRecordCacheTask).not.toHaveBeenCalled();

    const options = modalWarning.mock.calls[0][0] as { onOk: () => Promise<void> };
    await options.onOk();
    await flushPromises();

    expect(api.deleteRecordCacheTask).toHaveBeenCalledWith("t1");
    expect(messages.success).toHaveBeenCalledWith("已删除");
  });

  it("点「下载」走浏览器原生下载：先签发票据，再把同源地址交给浏览器", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1, files: [file()] })]));
    const anchors = captureDownloadAnchor();
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await buttonByText(wrapper, "下载").trigger("click");
    await flushPromises();

    // ⛔ 不传 index = 整段录像（多分片时后端在收尾期已经拼好）。
    expect(api.createRecordCacheDownload).toHaveBeenCalledWith("t1", undefined);
    // ⛔ 必须是 anchor 导航到那个地址 —— 那才会进浏览器的下载栏、有进度、能续传。
    //    退回 XHR 取 Blob 就等于回到"整包进内存、没有下载栏条目、大文件拖崩标签页"。
    expect(anchors).toHaveLength(1);
    expect(anchors[0].getAttribute("href")).toBe("/api/gb28181/record-cache/downloads/dl-1/content");
    expect(anchors[0].download).toBe("东门出入口-20260802081000.mp4");
    expect(messages.success).toHaveBeenCalledWith("已开始下载");
  });

  it("多分片任务的「下载」主入口取整段，另给按段下载的入口", async () => {
    api.listRecordCacheTasks.mockResolvedValue(
      page([task({ state: "succeeded", progress: 1, files: [file(), file({ index: 1, name: "缓存-第2段.mp4" })] })])
    );
    const anchors = captureDownloadAnchor();
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    // 主入口不传 index：多分片在后端已无损合成一个文件，
    // ⛔ 逐个 index 去下只会拿到同一个完整文件的多个副本（每次都是几十上百 MB）。
    await buttonByText(wrapper, "下载").trigger("click");
    await flushPromises();
    expect(api.createRecordCacheDownload).toHaveBeenCalledWith("t1", undefined);

    // 按段入口：序号必须**真的**传下去，并且下载地址也跟着变成那一段 ——
    // ⛔ 序号只停在前端（或后端忽略它）就会出现"点第 2 段拿到整段录像"，界面在说谎。
    const options = wrapper.findAll(".cache-doption");
    expect(options).toHaveLength(2);
    await options[1].trigger("click");
    await flushPromises();
    expect(api.createRecordCacheDownload).toHaveBeenLastCalledWith("t1", 1);
    expect(anchors[anchors.length - 1].getAttribute("href")).toBe("/api/gb28181/record-cache/downloads/dl-1/segments/1/content");
    expect(anchors[anchors.length - 1].download).toBe("东门出入口-20260802081000-第2段.mp4");
  });

  it("后端给的下载地址不是那一条时拒绝开始下载（不能把凭据交给外部地址）", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1, files: [file()] })]));
    api.createRecordCacheDownload.mockResolvedValue({
      code: 0,
      data: { task: { taskId: "dl-1", status: "ready", bytesSent: 0 }, contentUrl: "https://evil.example.com/x.mp4" }
    });
    const anchors = captureDownloadAnchor();
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await buttonByText(wrapper, "下载").trigger("click");
    await flushPromises();

    expect(anchors).toHaveLength(0);
    expect(messages.error).toHaveBeenCalledWith("下载地址异常，请重试");
    expect(messages.success).not.toHaveBeenCalled();
  });

  it("整理中的行：不给下载 / 不给取消，但仍然能删除", async () => {
    // ⛔ 三条判断互不相同，别合成一个：
    //    ① 整理中**不能**下载（产物还没落地，后端也会回 409）；
    //    ② 整理中**不能**取消（录像已全部拉回来了，后端回 409「任务已结束」）；
    //    ③ 整理中**必须能删除** —— 合并协程万一卡死，取消点不动、删除再藏起来，这行就再也清不掉了。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "merging", progress: 1, files: [file()] })]));
    // ⛔ 详情必须一起改成 merging：活动态的行会被详情回包覆盖（见 mergeLiveProgress），
    //    只改列表会让这一行在断言前又变回 running，用例就测了另一件事。
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ state: "merging", progress: 1, files: [file()] }) });
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    const labels = wrapper.findAll("button").map(node => node.text().trim());
    expect(labels).not.toContain("下载");
    expect(labels).not.toContain("取消");
    expect(labels).toContain("删除");
    expect(api.createRecordCacheDownload).not.toHaveBeenCalled();
    expect(wrapper.findAll(".cache-tag").map(node => node.text())).toEqual(["整理中"]);
    // 进度列要说清它在干什么，而不是显示成"缓存中"，让人以为还在耗流量。
    expect(wrapper.text()).toContain("正在整理成单个文件");
  });

  it("整理中也继续轮询（否则「整理中」会永远停在那里，下载入口再也不出现）", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "merging", progress: 1 })]));
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ state: "merging", progress: 1 }) });
    const setIntervalSpy = vi.spyOn(window, "setInterval");
    mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
  });

  it("服务端已给出可操作原因时不再盖一句笼统的「下载失败」", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1, files: [file()] })]));
    // 后端合并不成功 → 409 + 「请按分段下载」；HTTP 层会把这句话弹给用户。
    api.createRecordCacheDownload.mockRejectedValue({
      response: { status: 409, data: { message: "该录像分段暂时无法合并成一个文件，请按分段下载" } }
    });
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await buttonByText(wrapper, "下载").trigger("click");
    await flushPromises();

    expect(messages.error).not.toHaveBeenCalledWith("下载失败");
  });

  it("网络层失败（没有响应）仍然兜底提示下载失败", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded", progress: 1, files: [file()] })]));
    api.createRecordCacheDownload.mockRejectedValue(new Error("Network Error"));
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    await buttonByText(wrapper, "下载").trigger("click");
    await flushPromises();

    expect(messages.error).toHaveBeenCalledWith("下载失败");
  });

  it("没有取消/删除权限时对应按钮不出现在操作列", async () => {
    account.permissions = ["gb28181:record-cache:view"];
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();

    expect(wrapper.findAll("button").some(candidate => candidate.text().trim() === "取消")).toBe(false);
    expect(wrapper.findAll("button").some(candidate => candidate.text().trim() === "删除")).toBe(false);
  });

  it("没有任务时给出引导文案", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([]));
    const wrapper = mount(RecordCachePage, { global: { stubs } });
    await flushPromises();
    expect(wrapper.text()).toContain("还没有缓存任务");
  });
});

/**
 * ⛔⛔ 列表页骨架护栏（2026-10-04）。
 *
 * 教训：本页最初是「自己搭一套」——手写 toolbar + 裸 `a-table` + 自造操作列容器。
 * 结果是同一个产品里长着两套观感：搜索区没有统一面板、表格没有统一数据表样式、
 * 操作列没有统一色调。本仓有 55 个页面用 `s-layout-search`、34 个页面用
 * `uvp-table-action--*`，**这类分页列表页必须照抄同仓基准，不要另起一套**。
 *
 * 基准 = `src/views/system/account/account.vue`（分页统一用例的基准页）与
 *        `src/views/gb28181/playback-log/index.vue`（同模块兄弟页）。
 * 相关统一规则见 `src/style/model/pagination-unification.test.ts` 与
 * `web/src/style/model/uvp-ui-language.scss` 的 `.uvp-data-table / .uvp-pagination-bar` 共享块。
 */
describe("录像缓存页骨架：必须与同仓其它分页列表页一致", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/record-cache/index.vue"), "utf8");

  it("搜索区用统一搜索面板，表格用统一数据表类", () => {
    expect(hasMarkup(source, '<s-layout-search class="record-cache-search">')).toBe(true);
    expect(hasMarkup(source, '<a-table class="uvp-data-table record-cache-table"')).toBe(true);
    // ⛔ 不要再回到 `:bordered="{ wrapper: true, cell: false }"` 那套自造的边框。
    expect(hasMarkup(source, ':bordered="false"')).toBe(true);
  });

  it("分页条挂 uvp-pagination-bar 且三开关全开", () => {
    expect(hasMarkup(source, 'class="record-cache-pagination uvp-pagination-bar"')).toBe(true);
    expect(hasMarkup(source, "show-total")).toBe(true);
    expect(hasMarkup(source, "show-page-size")).toBe(true);
    expect(hasMarkup(source, "show-jumper")).toBe(true);
  });

  it("操作列走统一容器与色调类，不自己发明按钮样式", () => {
    expect(hasMarkup(source, '<div class="uvp-table-actions">')).toBe(true);
    for (const tone of ["--download", "--delete", "--stop"]) {
      expect(hasMarkup(source, `class="uvp-table-action uvp-table-action${tone}"`), tone).toBe(true);
    }
    // ⛔ 别再退回 `a-button type="text"`：Arco 按钮自带 padding / 圆角 / hover 底色，
    //    在同一张表里就成了第二套观感。本仓 34 个页面的操作列都是 a-link，
    //    色调统一由 `uvp-table-action--*` 给（全局基准见 uvp-ui-language.scss）。
    //    「a-link 没有 loading 所以只能用 a-button」是**错的** —— Link 有 loading/disabled。
    expect(source).not.toMatch(/<a-button[^>]*uvp-table-action/);
  });
});
