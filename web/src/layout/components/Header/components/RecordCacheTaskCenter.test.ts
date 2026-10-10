import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { reactive } from "vue";
import type { RecordCacheTask } from "@/api/recordCache";

const api = vi.hoisted(() => ({ listRecordCacheTasks: vi.fn(), getRecordCacheTask: vi.fn() }));
const accountState = vi.hoisted(() => ({ permissions: ["gb28181:record-cache:view"] as string[] }));
const account = reactive(accountState);
const router = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
// ⛔ 必须是**部分** mock：`@/utils/http` 会连带把 `@/router` 拉进来，而它要用
// `createRouter`。整个模块换成 `{ useRouter }` 会让导入阶段就炸掉。
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => router
}));

// 只换掉接口，`isRecordCacheTaskActive` / `formatByteSize` / `formatDuration` 用真实现：
// 「哪些状态算还在跑」「15% 怎么显示」都是本组件的行为断言，桩掉了就等于没测。
vi.mock("@/api/recordCache", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/recordCache")>()),
  listRecordCacheTasks: api.listRecordCacheTasks,
  getRecordCacheTask: api.getRecordCacheTask
}));

import RecordCacheTaskCenter from "./RecordCacheTaskCenter.vue";

const stubs = {
  // ⛔⛔ 这个桩必须照着**真 Arco** 建模：`a-popover` 的可见性 prop 是 `popupVisible`
  //    （+ `update:popupVisible` / `popupVisibleChange`），**没有 `visible`**。
  //    历史教训（2026-10-05）：这里当初写的是 `props: ["visible"]` + `emits: ["update:visible"]`，
  //    于是桩测"通过"、真页面上 `visible` 恒为 false —— 面板开着也按关着的那档轮询，
  //    看起来就是"打开时不会实时刷新"。桩建模错了 prop，测的就不是真组件。
  //    真 Arco 的验证在 `RecordCacheTaskCenter.render.test.ts`（用真组件点开、断 aria-expanded）。
  "a-popover": {
    props: ["popupVisible", "trigger", "position", "contentStyle"],
    emits: ["update:popupVisible", "popupVisibleChange"],
    template:
      "<div class='center-popover'>" +
      "<slot />" +
      "<span class='center-popover__open' @click='$emit(`update:popupVisible`, true)'>open</span>" +
      "<span class='center-popover__close' @click='$emit(`update:popupVisible`, false)'>close</span>" +
      "<div class='center-popover__content'><slot name='content' /></div>" +
      "</div>"
  },
  // ⛔ 角标只捕获 `count`：这个数字就是"有几个任务在缓存"，是入口存在的理由。
  "a-badge": {
    props: ["count", "maxCount"],
    template: "<span class='center-badge' :data-count='count'><slot /></span>"
  },
  "a-empty": { props: ["description"], template: "<span class='center-empty'>{{ description }}</span>" },
  "a-link": {
    props: ["loading", "disabled"],
    emits: ["click"],
    template: "<button class='center-link' @click='$emit(`click`)'><slot /></button>"
  },
  // ⛔ 捕获 `percent`：Arco 的 `a-progress` 收的是 0~1 的比值（`width = percent * 100%`），
  //    传百分数会被裁成"永远顶满"。这个 stub 就是这条口径的守卫。
  "a-progress": { props: ["percent", "showText", "size"], template: "<span class='center-progress' :data-percent='percent' />" }
};

function task(overrides: Partial<RecordCacheTask> = {}): RecordCacheTask {
  return {
    taskId: "t1",
    channelId: 31,
    deviceId: "34020000001320000001",
    channelCode: "34020000001320000001",
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
  return { code: 0, data: { list, total: list.length, page: 1, size: 50 } };
}

function mountCenter(variant?: "tabs" | "header") {
  return mount(RecordCacheTaskCenter, { props: variant ? { variant } : {}, global: { stubs } });
}

enableAutoUnmount(afterEach);

describe("RecordCacheTaskCenter", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    account.permissions = ["gb28181:record-cache:view"];
    api.listRecordCacheTasks.mockResolvedValue(page([task()]));
    // 详情接口默认"这一轮探测没拿到值"（活动态的行会逐条补问它，见 mergeLiveProgress）：
    // 于是面板沿用列表给的数字。要验证"进度确实被详情刷新"的用例自己覆盖它。
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: null });
    // ⛔ 只假造 interval 那两个：组件挂在 `setInterval` 上，一个真定时器漏到下一个用例
    //    就会把它的请求计数顶高（同款假红见前端 flaky 专题）。
    //    而 `setTimeout` 必须留真的 —— `flushPromises()` 自己就是靠 `setTimeout(0)` 让出
    //    事件循环的，一起假造会让每个用例都挂死到超时。
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
  });

  afterEach(() => {
    vi.useRealTimers();
    Object.defineProperty(document, "hidden", { configurable: true, value: false });
  });

  /** 手动推一轮轮询（定时器是假的，不会自己走）。 */
  async function tick() {
    vi.advanceTimersByTime(15000);
    await flushPromises();
  }

  it("没有录像缓存权限时入口不出现", async () => {
    account.permissions = [];
    const wrapper = mountCenter();
    await flushPromises();
    expect(wrapper.find(".record-cache-center__trigger").exists()).toBe(false);
    expect(wrapper.find(".center-badge").exists()).toBe(false);
  });

  it("管理员通配符权限也能看到入口", async () => {
    account.permissions = ["*:*:*"];
    const wrapper = mountCenter();
    await flushPromises();
    expect(wrapper.find(".record-cache-center__trigger").exists()).toBe(true);
  });

  it("角标只数「还在推进」的任务，终态不算", async () => {
    // ⛔ merging 必须算进去：录像已经拉完、后端正在拼文件的那几十秒里角标归零，
    //    用户会以为任务结束了，去下载却被告知"正在整理"。
    api.listRecordCacheTasks.mockResolvedValue(
      page([
        task({ taskId: "a", state: "queued" }),
        task({ taskId: "b", state: "running" }),
        task({ taskId: "c", state: "merging" }),
        task({ taskId: "d", state: "succeeded" }),
        task({ taskId: "e", state: "failed" }),
        task({ taskId: "f", state: "expired" })
      ])
    );

    const wrapper = mountCenter();
    await flushPromises();

    expect(wrapper.get(".center-badge").attributes("data-count")).toBe("3");
    expect(wrapper.get("[data-testid='record-cache-center-summary']").text()).toContain("3 个正在缓存到服务器");
    // 面板里只列活跃的那三条，不该把已完成/失败/过期的也塞进来。
    expect(wrapper.findAll(".record-cache-center__item")).toHaveLength(3);
  });

  it("没有活跃任务时角标为 0，面板给出空态", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded" })]));
    const wrapper = mountCenter();
    await flushPromises();

    expect(wrapper.get(".center-badge").attributes("data-count")).toBe("0");
    expect(wrapper.find(".record-cache-center__item").exists()).toBe(false);
    expect(wrapper.text()).toContain("暂无正在缓存的任务");
  });

  it("面板里每条给出通道名、状态、进度与实时速率", async () => {
    const wrapper = mountCenter();
    await flushPromises();

    expect(wrapper.text()).toContain("东门出入口");
    expect(wrapper.text()).toContain("缓存中");
    // ⛔ 喂给 `a-progress` 的必须是 0~1 的比值：传 15 会渲染成 width:1500%（被裁成满格）。
    expect(wrapper.get(".center-progress").attributes("data-percent")).toBe("0.15");
    const text = wrapper.text();
    expect(text).toContain("15%");
    expect(text).toContain("5分0秒 / 32分16秒");
    expect(text).toContain("10.0 MB");
    expect(text).toContain("2.0 MB/s");
  });

  it("整理中的那条明确说「整理中」，不说成「缓存中」", async () => {
    // 录像已经全部拉回来了，再说"缓存中"会让人以为还在耗流量、还占着设备通道。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "merging", progress: 1, speedBytesPerSec: 0 })]));
    const wrapper = mountCenter();
    await flushPromises();

    expect(wrapper.text()).toContain("整理中");
    expect(wrapper.text()).toContain("正在整理成单个文件");
    expect(wrapper.text()).not.toContain("缓存中");
  });

  it("活动任务的进度取自详情接口 —— 光看列表会永远停在 0%", async () => {
    // ⛔⛔ 这条是用户 2026-10-05 实测反馈的原话："这个进度它不会动呢"。
    //    成因是后端两条**有意**的设计叠在一起：
    //      1. 列表接口不查媒体节点（反向断言 `TestListDoesNotQueryMediaServer` 钉着）；
    //      2. 进度字段只在**整片收尾那一刻**才落库，而一片最长要拉 7 分半钟。
    //    ⇒ 一整片的时间里列表给的 progress/cachedSeconds/cachedBytes 全是 0。
    //    实时值只有详情接口有，所以面板必须对活动态的行逐条补问，并把它盖回行上。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ progress: 0, cachedSeconds: 0, cachedBytes: 0 })]));
    api.getRecordCacheTask.mockResolvedValue({
      code: 0,
      data: task({ progress: 0.42, cachedSeconds: 813, cachedBytes: 31457280 })
    });

    const wrapper = mountCenter();
    await flushPromises();

    expect(api.getRecordCacheTask).toHaveBeenCalledWith("t1", { showErrorMessage: false });
    expect(wrapper.get(".center-progress").attributes("data-percent")).toBe("0.42");
    expect(wrapper.text()).toContain("42%");
  });

  it("终态任务不去补问详情", async () => {
    // 实时值只有"还在跑"的行才需要。列表一页 50 条，给已经完结的任务再打一遍详情纯属白烧。
    api.listRecordCacheTasks.mockResolvedValue(page([task({ state: "succeeded" })]));
    mountCenter();
    await flushPromises();
    expect(api.getRecordCacheTask).not.toHaveBeenCalled();
  });

  it("详情回了面板里没有的任务时直接忽略，不凭空多出一行", async () => {
    api.listRecordCacheTasks.mockResolvedValue(page([task({ taskId: "t1" })]));
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task({ taskId: "t9", channelName: "不该出现的通道" }) });
    const wrapper = mountCenter();
    await flushPromises();

    expect(wrapper.text()).not.toContain("不该出现的通道");
    expect(wrapper.findAll(".record-cache-center__item")).toHaveLength(1);
  });

  it("拉列表时明确关掉错误提示并只取一页", async () => {
    // ⛔ 这是后台轮询：服务端抖一下就在用户脸上弹一次红条，比不刷新更烦。
    mountCenter();
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenCalledWith({ page: 1, size: 50 }, { showErrorMessage: false });
  });

  it("每 15 秒轮询一次；后台标签页不轮询、恢复后继续", async () => {
    const wrapper = mountCenter();
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(1);

    // 后台标签页：用户看不见角标，这些请求纯属白烧数据库。
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    await tick();
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(1);

    // 只推 14 秒还不够 —— 轮询间隔就是 15 秒，这是个会写错的边界。
    Object.defineProperty(document, "hidden", { configurable: true, value: false });
    vi.advanceTimersByTime(14000);
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(1000);
    await flushPromises();
    expect(api.listRecordCacheTasks).toHaveBeenCalledTimes(2);

    wrapper.unmount();
  });

  it("轮询失败时保留上一次的结果，不把角标清零", async () => {
    // 清空等于对用户说"任务没了" —— 比不刷新更糟。
    const wrapper = mountCenter();
    await flushPromises();
    expect(wrapper.get(".center-badge").attributes("data-count")).toBe("1");

    api.listRecordCacheTasks.mockRejectedValue(new Error("Network Error"));
    await tick();

    expect(wrapper.get(".center-badge").attributes("data-count")).toBe("1");
    expect(wrapper.findAll(".record-cache-center__item")).toHaveLength(1);
  });

  it("「全部任务」跳到录像缓存列表页并先关掉面板", async () => {
    const wrapper = mountCenter();
    await flushPromises();

    await wrapper.get(".center-link").trigger("click");
    await flushPromises();

    expect(router.push).toHaveBeenCalledWith("/gb28181/record-cache");
  });

  it("打开面板先补刷一次，并把轮询切到 3 秒档；关掉降回 15 秒档", async () => {
    // ⛔⛔ 这条对应的是用户 2026-10-05 的原话："下载任务面板在开着的时候还是不会实时刷新的，
    //    就第一次打开的时候会刷新"。
    //    根因不是请求发不出去 —— 是 `v-model:visible` 绑到了 `a-popover` **不存在**的 prop 上
    //    （真 prop 叫 `popupVisible`），于是 `visible` 恒为 false：打开那一下仍会走一次补刷
    //    （"第一次打开时会刷新"），但轮询永远停在"关着"的 15 秒档，永远不切"打开档"的 3 秒
    //    —— 后端访问日志里那 8 分钟严格 15 秒一格就是它。
    const wrapper = mountCenter();
    await flushPromises();
    const calls = () => api.listRecordCacheTasks.mock.calls.length;
    const mounted = calls();

    // 关着：推 3 秒还不够闲置档的一格，不该有新请求（否则就是把两档写成了同一个值）。
    vi.advanceTimersByTime(3000);
    await flushPromises();
    expect(calls()).toBe(mounted);

    await wrapper.get(".center-popover__open").trigger("click");
    await flushPromises();
    const opened = calls();
    // 打开那一下必须立刻补刷：面板里显示的要是此刻的状态，而不是上一轮轮询的旧值。
    expect(opened).toBe(mounted + 1);
    // `v-model:popup-visible` 生效的直接观感。
    expect(wrapper.get("button").attributes("aria-expanded")).toBe("true");

    // 打开档：再推 3 秒就该有一轮。
    vi.advanceTimersByTime(3000);
    await flushPromises();
    expect(calls()).toBe(opened + 1);

    // 关掉要降回 15 秒档 —— 不降的话，面板关了还在用高频打接口。
    await wrapper.get(".center-popover__close").trigger("click");
    await flushPromises();
    expect(wrapper.get("button").attributes("aria-expanded")).toBe("false");
    vi.advanceTimersByTime(3000);
    await flushPromises();
    expect(calls()).toBe(opened + 1);
    vi.advanceTimersByTime(12000);
    await flushPromises();
    expect(calls()).toBe(opened + 2);

    wrapper.unmount();
  });

  it("「全部任务」跳页前真的关得掉面板", async () => {
    // ⛔ 与上一条同源：`visible.value = false` 在绑定写错时是个空操作，
    //    跳页后那个面板会一直挂在顶栏下面。
    const wrapper = mountCenter();
    await flushPromises();
    await wrapper.get(".center-popover__open").trigger("click");
    await flushPromises();
    expect(wrapper.get("button").attributes("aria-expanded")).toBe("true");

    await wrapper.get(".center-link").trigger("click");
    await flushPromises();

    expect(router.push).toHaveBeenCalledWith("/gb28181/record-cache");
    expect(wrapper.get("button").attributes("aria-expanded")).toBe("false");
  });

  it("卸载时清掉轮询定时器", async () => {
    const wrapper = mountCenter();
    await flushPromises();
    expect(vi.getTimerCount()).toBe(1);
    wrapper.unmount();
    // ⛔ 留在队列里的定时器会打在已卸载组件的 ref 上，下一个用例的请求计数就被它顶高了。
    expect(vi.getTimerCount()).toBe(0);
  });

  it("两个形态用各自的按钮 id，尺寸类也跟着切换", async () => {
    // 桌面端挂在标签栏（跟全屏/主题切换同排），移动端挂在头部那一排。
    const tabs = mountCenter();
    await flushPromises();
    expect(tabs.get("button").attributes("id")).toBe("system-tabs-record-cache");
    expect(tabs.get("button").classes()).toContain("record-cache-center__trigger--tabs");

    const header = mountCenter("header");
    await flushPromises();
    expect(header.get("button").attributes("id")).toBe("system-header-record-cache");
    expect(header.get("button").classes()).toContain("record-cache-center__trigger--header");
  });
});
