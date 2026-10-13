import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it, vi } from "vitest";
import { hasRuleBlock } from "@/test/source-assert";
import RecordCachePage from "./index.vue";

/**
 * 录像缓存页的**真实渲染**契约（挂真 Arco 组件，不 stub）。
 *
 * 为什么单开一个文件：`index.test.ts` 为了好定位交互，把 `a-progress` / `a-button` /
 * `a-link` 全换成了最简桩；桩能证明"我们传了什么"，证明不了"渲染出来是什么"。
 * 而本页 2026-10-04 的两次翻车恰恰都是**渲染层**的：
 *   1. 进度条永远顶满 —— `a-progress` 的 `percent` 要 0~1 比值，传 0~100 会渲染成
 *      `width: 1500%`（被容器裁掉后视觉满格），桩只记录 `data-percent` 属性，看不见宽度。
 *   2. 操作列观感与全仓不一致 —— 用了 `a-button type="text"`（Arco 按钮自带 padding /
 *      圆角 / hover 底色），而全仓 34 个列表页都用 `a-link` + `uvp-table-action--*`。
 *      这里直接断言落进 DOM 的是链接而不是按钮。
 */

const api = vi.hoisted(() => ({
  listRecordCacheTasks: vi.fn(),
  getRecordCacheTask: vi.fn()
}));

vi.mock("@/api/recordCache", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/recordCache")>()),
  listRecordCacheTasks: api.listRecordCacheTasks,
  getRecordCacheTask: api.getRecordCacheTask
}));

vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account: { permissions: ["*:*:*"] } })
}));

const searchPanel = { template: "<div><slot name='fields' /><slot name='actions' /></div>" };

function mountPage() {
  return mount(RecordCachePage, {
    global: {
      plugins: [ArcoVue],
      stubs: { "s-layout-search": searchPanel }
    }
  });
}

const task = {
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
  state: "running" as const,
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

describe("录像缓存页：真实 Arco 渲染契约", () => {
  it("进度条按 0~1 比值渲染成 15% 宽，且百分比数字在文字里", async () => {
    // ⛔ percent 若被喂成 15，这里读到的会是 `width: 1500%` —— 这就是"蓝条顶满"的现场。
    api.listRecordCacheTasks.mockResolvedValue({ code: 0, data: { list: [task], total: 1, page: 1, size: 20 } });
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task });

    const wrapper = mountPage();
    await flushPromises();

    const bar = wrapper.get(".arco-progress-line-bar");
    expect(bar.attributes("style")).toContain("width: 15%");
    expect(wrapper.get(".record-cache-progress__percent").text()).toBe("15%");
    // 百分比是独立文字节点，不依赖 a-progress 的内建文字（那个失败态会变成 ✗ 图标）。
    expect(bar.text()).toBe("");

    wrapper.unmount();
  });

  it("操作列渲染出来的是链接（a-link），不是 Arco 按钮", async () => {
    api.listRecordCacheTasks.mockResolvedValue({ code: 0, data: { list: [task], total: 1, page: 1, size: 20 } });
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task });

    const wrapper = mountPage();
    await flushPromises();

    const cancel = wrapper.get(".uvp-table-action--stop");
    // Arco Link 的根元素是 <a> / <span>；换成 a-button 的话这里是 <button>，
    // 并且会多出 `arco-btn-type-text` 那一整套按钮观感。
    expect(["A", "SPAN"]).toContain(cancel.element.tagName);
    expect(cancel.classes()).not.toContain("arco-btn");
    expect(cancel.text()).toBe("取消");
    // 色调类必须与容器类同时落在同一个元素上（`uvp-table-action--*` 的规则都挂在
    // `.uvp-data-table .uvp-table-action` 之下，只有同元素同时具备才能命中）。
    expect(cancel.classes()).toContain("uvp-table-action");
    expect(wrapper.find(".uvp-table-actions").exists()).toBe(true);

    wrapper.unmount();
  });

  it("收藏星标同样是链接（a-link），并且未收藏时是空心", async () => {
    api.listRecordCacheTasks.mockResolvedValue({ code: 0, data: { list: [task], total: 1, page: 1, size: 20 } });
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: { ...task, favorite: true } });

    const wrapper = mountPage();
    await flushPromises();

    const star = wrapper.get(".uvp-table-action--favorite");
    // 同操作列：换成 a-button 就会带上 `arco-btn-*` 一整套按钮观感（padding / 圆角 / hover 底色）。
    expect(["A", "SPAN"]).toContain(star.element.tagName);
    expect(star.classes()).not.toContain("arco-btn");
    expect(star.classes()).toContain("uvp-table-action");
    // 收藏态靠 `is-on` 上色 + 补 `fill`（见 uvp-ui-language.scss）——
    // 少了它，收藏前后只有颜色深浅之差，用户看不出收藏成功没。
    expect(star.classes()).toContain("is-on");
    // 图标是 lucide 的描边星，必须有 svg 才谈得上"实心"。
    expect(star.find("svg").exists()).toBe(true);

    wrapper.unmount();
  });

  it("收藏按钮带文字，且收藏态把文案换成「取消收藏」", async () => {
    // ⛔ 老板 2026-10-06 要求补文字：操作列里一排链接，纯星标认不出是什么。
    // 纯图标这一版得钉死，否则下次有人"优化"成只留图标不会有人拦。
    const mountWith = async (favorite: boolean) => {
      api.listRecordCacheTasks.mockResolvedValue({ code: 0, data: { list: [task], total: 1, page: 1, size: 20 } });
      api.getRecordCacheTask.mockResolvedValue({ code: 0, data: { ...task, favorite } });
      const wrapper = mountPage();
      await flushPromises();
      return wrapper;
    };

    const off = await mountWith(false);
    const offStar = off.get(".uvp-table-action--favorite");
    expect(offStar.text()).toContain("收藏");
    // 收藏态要**换文案**而不只是变色：只变色的话"点一下是取消"只能靠 tooltip 才知道。
    expect(offStar.text()).not.toContain("取消");
    // 悬停提示始终把语义说全（"不会被自动清理"是这个开关唯一的功能）。
    expect(offStar.attributes("title")).toBe("收藏后不会被自动清理");
    off.unmount();

    const on = await mountWith(true);
    const onStar = on.get(".uvp-table-action--favorite");
    expect(onStar.text()).toContain("取消收藏");
    expect(onStar.attributes("title")).toBe("已收藏，不会被自动清理");
    on.unmount();
  });

  it("进度条渲染成 6px 胶囊，且带得出业务状态（不交给 Arco 自己猜）", async () => {
    api.listRecordCacheTasks.mockResolvedValue({ code: 0, data: { list: [task], total: 1, page: 1, size: 20 } });
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: task });

    const wrapper = mountPage();
    await flushPromises();

    const bar = wrapper.get(".record-cache-progress__bar");
    // 状态要落在 DOM 上：颜色是 CSS 按 `data-state` 挑的，没有这个属性就退回默认蓝。
    expect(bar.attributes("data-state")).toBe("running");
    // ⛔ 高度是**行内 style**（Arco 把 strokeWidth 写成 `height: Npx`），
    //    CSS 的 height 压不过行内 ⇒ 只能断言这个行内值本身。
    // 3px 时100px 圆角被压成 1.5px，看着是根硬线；6px 才是胶囊。
    const line = wrapper.get(".arco-progress-line");
    expect(line.attributes("style")).toContain("height: 6px");

    wrapper.unmount();
  });

  it("失败态即使 percent 满格，颜色也归CSS 管（Arco 会自说自话）", async () => {
    /*
     *⛔ 实测（2026-10-06，本条断言的第一版就是错的）：
     *   Arco 的 `computedStatus = props.status || (props.percent >= 1 ? "success" : "normal")`
     *   （`es/progress/progress.js`）—— 所以**不传 `status` 并不能让它闭嘴**：
     *   `percent = 1` 时它照样给自己加 `arco-progress-status-success`，
     *   并用 `.arco-progress-status-success .arco-progress-line-bar { background-color:
     *   rgb(var(--success-6)) }` 上色（实测类名见下方）。
     *   ⇒"整理中 / 已取消但percent 已经是 1"的任务会被涂成成功绿，把终态说反。
     *   对策不是"不传 status"（传了也一样，它只是优先用你给的那个），
     *   而是**让本页 CSS 的特异度压过它**：每条状态色规则都带 `[data-state="..."]`，
     *   比Arco 的单层 status 类多一层，scoped 又再加一层 `[data-v-xxx]` ⇒ 稳赢。
     */
    api.listRecordCacheTasks.mockResolvedValue({ code: 0, data: { list: [task], total: 1, page: 1, size: 20 } });
    api.getRecordCacheTask.mockResolvedValue({ code: 0, data: { ...task, state: "failed", progress: 1 } });

    const wrapper = mountPage();
    await flushPromises();

    const bar = wrapper.get(".record-cache-progress__bar");
    expect(bar.attributes("data-state")).toBe("failed");
    // 前置事实（钉住 Arco 的真实行为）：它确实会自己加 success 类。
    // 哪天 Arco 改了这条，下面的源码断言就该重新评估优先级还够不够。
    expect(bar.classes()).toContain("arco-progress-status-success");

    wrapper.unmount();
  });
});

/**
 * 进度条**配色**的源码级契约。
 *
 * ⛔ 为什么只能钉源码：上面那些渲染单测能证明"高度是 6px""状态落在 data-state 上"，
 *    但证明不了"底轨是什么颜色" —— happy-dom 不解析真实 CSS 计算值，
 *    `background: var(--某个不存在的token)` 在测试里一切正常，在页面上却是透明。
 *    同款坑：StorageCardStatusPanel 的底轨曾写 `var(--uvp-border)`（全仓未定义）。
 */
describe("录像缓存进度条的配色契约（源码级）", () => {
  const PAGE = resolve(process.cwd(), "src/views/gb28181/record-cache/index.vue");
  const source = readFileSync(PAGE, "utf8");

  it("底轨用系统专用的--uvp-meter-track，而不是 Arco 自带的灰阶", () => {
    // 底轨是**大面积色块**："还剩多少没缓存"全靠它读出来。Arco 的 `--color-fill-3`
    // 是它自己的灰阶，跟本仓面板底不同源，暗色下会和卡片底糊在一起。
    expect(
      hasRuleBlock(source, ".record-cache-progress__bar :deep(.arco-progress-line)", "background: var(--uvp-meter-track)")
    ).toBe(true);
    expect(source).not.toMatch(/var\(\s*--color-fill-3\s*\)/);
  });

  it("底轨与填充都是胶囊（999px 圆角），不靠 Arco 内部的 100px 数值", () => {
    // 依赖 Arco 的 `border-radius: 100px` 等于把圆角绑死在它的高度换算上：
    // 哪天它改了 strokeWidth，胶囊就变方角。
    expect(hasRuleBlock(source, ".record-cache-progress__bar :deep(.arco-progress-line)", "border-radius: 999px")).toBe(true);
    expect(hasRuleBlock(source, ".record-cache-progress__bar :deep(.arco-progress-line-bar)", "border-radius: 999px")).toBe(true);
  });

  it("五种业务状态各有自己的颜色，且都取本仓 token", () => {
    for (const state of ["running", "merging", "succeeded", "failed"]) {
      expect(source).toContain(`[data-state="${state}"]`);
    }
    // 颜色一律来自 token，不写死十六进制 —— 换主题时才会跟着变。
    // ⛔ 进度条是**纯色块、没有白字压在上面**，所以用 --uvp-brand / --uvp-success 这类
    //    "标记色"是对的；不要误套"白字压实心底色 ⇒ --uvp-solid-*"那条规则。
    for (const token of ["var(--uvp-brand)", "var(--uvp-warning)", "var(--uvp-success)", "var(--uvp-danger)"]) {
      expect(source).toContain(token);
    }
  });

  it("每条状态色规则都带 [data-state] 限定 —— 否则压不住 Arco 的 status 类", () => {
    /*
     * ⛔ 这是本页进度条配色最关键的一条。Arco 自带
     *    `.arco-progress-status-success .arco-progress-line-bar { background-color: ... }`
     *    （arco.css 12001行），而它**总会**出现：`computedStatus = status ||
     *    (percent >= 1 ? "success" : "normal")`（progress.js），不传 status 也一样。
     *    那条规则的选择器是「一个 status 类 + 一个 bar 类」= 2 个类。
     *    本页的规则写成 `[data-state="x"] .bar` = 2 个类 + 1 个属性选择器
     *    （scoped 再自动加 `[data-v-xxx]`）⇒ 本页稳赢。
     *⛔ 一旦有人把某条降级成 `.bar__外层 :deep(.arco-progress-line-bar)`
     *    （裸类、不带 data-state），特异度就掉到 2 个类，**变成平手**、
     *    顺序一变就被 Arco 盖掉 ⇒ 失败/取消的任务全变成功绿，而且看不出来。
     *
     * 判据用「逐个状态点名的**完整选择器串**」，而不是"数一数带 data-state 的规则有几条"
     * —— ⛔ 计数式判据在"降级一条"时数量不变，是会漏网的（第一版就这么写，被变异自检抓到）。
     */
    for (const state of ["running", "merging", "succeeded", "failed", "cancelled", "expired"]) {
      expect(source).toContain(`.record-cache-progress__bar[data-state="${state}"] :deep(`);
    }
  });

  it("已取消 / 已过期压成灰色，别显示成「卡在某个百分比」", () => {
    for (const state of ["cancelled", "expired"]) {
      expect(source).toContain(`[data-state="${state}"]`);
    }
    expect(source).toContain("var(--uvp-text-tertiary)");
  });
});
