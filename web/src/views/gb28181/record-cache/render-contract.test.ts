import { flushPromises, mount } from "@vue/test-utils";
import ArcoVue from "@arco-design/web-vue";
import { describe, expect, it, vi } from "vitest";
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
});
