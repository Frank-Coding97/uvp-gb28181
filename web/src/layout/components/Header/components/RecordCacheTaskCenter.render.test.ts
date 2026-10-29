import { readFileSync } from "node:fs";
import { flushPromises, mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { reactive, nextTick } from "vue";
import type { RecordCacheTask } from "@/api/recordCache";

/**
 * 顶栏「录像缓存任务」入口的**真实渲染**契约（挂真 Arco，不 stub）。
 *
 * 为什么单开一个文件：`RecordCacheTaskCenter.test.ts` 为了好定位交互，把
 * `a-popover` / `a-badge` / `a-progress` 全换成了最简桩；桩能证明"我们传了什么"，
 * 证明不了"用户点下去到底看得见什么"。这一页最容易静默失败的恰恰是渲染层：
 *   1. 触发按钮外面套着 `a-badge` 的 span，按钮自己的 `data-v-*` 与宿主不一致 ⇒
 *      尺寸/悬浮样式整套失效（表现是顶栏冒出一个没样式的裸图标）；
 *   2. `a-popover` 的内容是 teleport 到 body 的 ⇒ 面板里的样式如果靠"从宿主往下选"
 *      就会全部落空；
 *   3. `a-progress` 的 percent 要 0~1 比值，喂百分数会渲染成 `width: 1500%`。
 */

const api = vi.hoisted(() => ({ listRecordCacheTasks: vi.fn(), getRecordCacheTask: vi.fn() }));
const router = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => reactive({ account: { permissions: ["*:*:*"] } })
}));
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRouter: () => router
}));
vi.mock("@/api/recordCache", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/recordCache")>()),
  listRecordCacheTasks: api.listRecordCacheTasks,
  getRecordCacheTask: api.getRecordCacheTask
}));

import RecordCacheTaskCenter from "./RecordCacheTaskCenter.vue";

const task: RecordCacheTask = {
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
  expiresAt: "2026-10-10T16:00:00+08:00"
};

function mountCenter() {
  return mount(RecordCacheTaskCenter, {
    attachTo: document.body,
    global: { plugins: [ArcoVue] }
  });
}

describe("录像缓存任务入口：真实 Arco 渲染契约", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // 组件自己会挂 15 秒轮询。只假造 interval 那两个：真定时器漏到下一个用例会顶高请求计数，
    // 而 `setTimeout` 必须留真的（`flushPromises()` 和 Arco 的弹层动效都要用它）。
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    api.listRecordCacheTasks.mockResolvedValue({
      code: 0,
      data: { list: [task, { ...task, taskId: "t2", state: "merging" }], total: 2, page: 1, size: 50 }
    });
    // 活动态的行会逐条补问详情（见 mergeLiveProgress）。这里让探测"没拿到值"，
    // 面板就用列表给的数 —— 免得本文件里那条进度断言被详情接口的默认值搅乱。
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: null });
    document.body.innerHTML = "";
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("角标上真的印着进行中的任务数", async () => {
    const wrapper = mountCenter();
    await flushPromises();

    // 真 Arco 的角标数字落在 `.arco-badge-number` 里。
    expect(document.body.querySelector(".arco-badge-number")?.textContent?.trim()).toBe("2");
    // 触发按钮必须真的存在且可点（外面套着 badge 的 span 时最容易在这里出事）。
    const trigger = document.body.querySelector<HTMLButtonElement>("#system-tabs-record-cache");
    expect(trigger).not.toBeNull();
    expect(trigger!.tagName).toBe("BUTTON");

    wrapper.unmount();
  });

  it("点开后面板挂在 body 上，并且每条任务都带上真实进度", async () => {
    const wrapper = mountCenter();
    await flushPromises();

    // ⛔ 这里刻意用 `document.body` 而不是 `wrapper.find`：`a-popover` 的内容是
    //    teleport 出去的，从宿主往下找**永远找不到** —— 这正是"测试里好好的、
    //    真页面上什么都没有"的经典成因。
    await document.body.querySelector<HTMLButtonElement>("#system-tabs-record-cache")!.click();
    await nextTick();
    await flushPromises();

    const panel = document.body.querySelector(".record-cache-center");
    expect(panel).not.toBeNull();
    expect(panel!.textContent).toContain("东门出入口");
    expect(panel!.textContent).toContain("2 个正在缓存到服务器");

    const bar = document.body.querySelector<HTMLElement>(".arco-progress-line-bar");
    expect(bar).not.toBeNull();
    // ⛔ percent 若被喂成 15，这里读到的是 `width: 1500%` —— 被容器裁掉后就是"永远顶满"。
    expect(bar!.getAttribute("style")).toContain("width: 15%");

    wrapper.unmount();
  });

  it("点开面板后 `visible` 真的翻转，并把轮询切到 3 秒档", async () => {
    // ⛔⛔ 这条用**真 Arco** 才成立：`a-popover` 的可见性 prop 叫 `popupVisible`，
    //    **没有 `visible`**。写错是静默的 —— 面板照开、控制台无错，只是 `visible` 恒为 false，
    //    于是永远按"关着"的 15 秒档轮询（用户 2026-10-05 报的"开着时不会实时刷新，
    //    就第一次打开会刷新"；后端访问日志里那 8 分钟严格 15 秒一格就是它）。
    //    桩测抓不到：桩建模的是"我们以为的 prop"。这里拿真组件的 `aria-expanded`（直接绑
    //    `visible`）当探针 —— 它翻不翻转，就是绑定对不对的唯一事实。
    const wrapper = mountCenter();
    await flushPromises();
    const trigger = () => document.body.querySelector<HTMLButtonElement>("#system-tabs-record-cache")!;
    const calls = () => api.listRecordCacheTasks.mock.calls.length;
    const mounted = calls();

    expect(trigger().getAttribute("aria-expanded")).toBe("false");
    // 关着的时候推 3 秒不该有新请求（闲置档是 15 秒）。
    vi.advanceTimersByTime(3000);
    await flushPromises();
    expect(calls()).toBe(mounted);

    await trigger().click();
    await nextTick();
    await flushPromises();
    expect(trigger().getAttribute("aria-expanded")).toBe("true");

    const opened = calls();
    vi.advanceTimersByTime(3000);
    await flushPromises();
    expect(calls()).toBeGreaterThan(opened);

    wrapper.unmount();
  });

  it("角标与弹层都真的挂上了各自的样式锚点类", async () => {
    // ⛔ 这几个类名是样式的**唯一落点**，而且属于"静默失败"型：
    //    角标由 Arco 渲染、弹层 teleport 到 body —— 类名一旦没传对，样式整套不生效，
    //    页面照样能开，只是退回 Arco 默认皮肤（就是 2026-10-05 用户报的"配色不统一"）。
    //    桩测看不见这一层，只能在这里对着真 Arco 的 DOM 断言。
    const wrapper = mountCenter();
    await flushPromises();
    await document.body.querySelector<HTMLButtonElement>("#system-tabs-record-cache")!.click();
    await nextTick();
    await flushPromises();

    // 角标：业务类透传到 `a-badge` 的根 span 上，数字是它的子节点。
    expect(document.body.querySelector(".record-cache-center__badge .arco-badge-number")).not.toBeNull();
    // 弹层表面与箭头：`content-class` / `arrow-class` 分别落在
    // `.arco-popover-popup-content` 和 `.arco-popover-popup-arrow` 上。
    expect(document.body.querySelector(".arco-popover-popup-content.record-cache-center__popup")).not.toBeNull();
    expect(document.body.querySelector(".arco-popover-popup-arrow.record-cache-center__arrow")).not.toBeNull();

    wrapper.unmount();
  });
});

/**
 * 样式规则本身的契约。
 *
 * ⛔ 上面那条只证明「类名挂对了元素」，证明不了「规则还在」—— 而这几条规则一旦被删/被改回
 *    Arco 默认值，页面**照常能开**、控制台**没有报错**，只是暗色下悄悄变回"两套皮肤"
 *    （正是用户 2026-10-05 报的那个）。CSS 没有类型、jsdom 也不计算样式，只能看源码。
 */
describe("顶栏面板的样式锚点（源码级）", () => {
  // ⛔ 用相对 cwd 的路径而不是 `new URL(..., import.meta.url)`：本仓的 fs 拦截层
  //    在这个环境里拿到的 `import.meta.url` 不是 file 协议，会直接抛 "URL must be of scheme file"。
  const source = readFileSync("src/layout/components/Header/components/RecordCacheTaskCenter.vue", "utf8");

  it("弹层表面与箭头都走本仓浮层 token，而不是 Arco 自带的灰", () => {
    // ⛔ `a-popover` 不在任何全局覆盖里（Arco 用的是 --color-bg-popup / --color-neutral-3）。
    //    箭头是独立元素，漏了它会在面板边上留一个浅色小三角。
    expect(source).toMatch(/\.record-cache-center__popup\s*\{[^}]*background-color:\s*var\(--uvp-popconfirm-bg\)/s);
    expect(source).toMatch(/\.record-cache-center__arrow\s*\{[^}]*background-color:\s*var\(--uvp-popconfirm-bg\)/s);
  });

  it("角标缩过尺寸、并改用跟着主题走的配色（不是 Arco 的 20px 红底）", () => {
    expect(source).toMatch(/\.record-cache-center__badge\s+\.arco-badge-number\s*\{[^}]*height:\s*16px/s);
    expect(source).toMatch(
      /\.record-cache-center__badge\s+\.arco-badge-number\s*\{[^}]*background-color:\s*var\(--uvp-brand-strong\)/s
    );
    // 描边跟着顶栏底色 —— Arco 默认的 `--color-bg-2` 在深色顶栏上是一圈亮环。
    expect(source).toMatch(/\.record-cache-center__badge\s+\.arco-badge-number\s*\{[^}]*box-shadow:[^;]*--uvp-navigation-bg/s);
  });

  it("面板可见性绑的是 Arco 真实的 prop（`popupVisible`），不是不存在的 `visible`", () => {
    // ⛔⛔ `a-popover` **没有** `visible` prop（只有 `popupVisible` / `update:popupVisible`）。
    //    绑错的后果是静默的：`visible` 恒为 false ⇒ 面板开着也按"关着"的 15 秒档轮询
    //    （2026-10-05 用户报的"开着时不会实时刷新"），「全部任务」也关不掉面板。
    //    上面那条真 Arco 用例能用 `aria-expanded` 抓到它，这里再加一条源码级断言，
    //    失败时能直接指出"是哪个绑定写错了"，而不是只看到 `aria-expanded` 不对。
    //    ⛔ 只看 `<a-popover …>` 这个开标签：脚本里的注释也解释着这个坑，全文匹配会误伤。
    const popoverTag = source.match(/<a-popover[\s\S]*?>/)?.[0] ?? "";
    expect(popoverTag).toContain('v-model:popup-visible="visible"');
    expect(popoverTag).not.toContain("v-model:visible");
  });
});
