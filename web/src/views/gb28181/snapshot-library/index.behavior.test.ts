import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const gbApi = vi.hoisted(() => ({ listSnapshotLibrary: vi.fn() }));
const accountState = vi.hoisted(() => ({ permissions: ["gb28181:device:snapshot"] as string[] }));
const account = reactive(accountState);
const messages = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn() }));
const authState = vi.hoisted(() => ({ accessToken: "tok-1" }));

const replace = vi.hoisted(() => vi.fn(() => Promise.resolve()));
const currentRoute = reactive({ query: {} as Record<string, unknown> });

vi.mock("@/api/gb28181", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/gb28181")>()),
  listSnapshotLibrary: gbApi.listSnapshotLibrary
}));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
vi.mock("@/utils/auth", () => ({ getAccessToken: () => authState }));
vi.mock("@/api/utils", () => ({ getBaseUrl: () => "" }));
vi.mock("@arco-design/web-vue", async importOriginal => ({
  ...(await importOriginal<typeof import("@arco-design/web-vue")>()),
  Message: messages
}));
vi.mock("vue-router", async importOriginal => ({
  ...(await importOriginal<typeof import("vue-router")>()),
  useRoute: () => currentRoute,
  useRouter: () => ({ replace })
}));

import SnapshotLibrary from "./index.vue";

function row(id: number, channelAlias = "东门出入口", channelName = "IPC-HFW2431S") {
  return {
    id,
    deviceId: 7,
    channelId: 9,
    channelCode: "37010301021320000312",
    channelName,
    channelAlias,
    deviceCode: "37010301021320000311",
    deviceName: "NVR-8CH",
    deviceAlias: "一号机房NVR",
    sessionId: "",
    fileName: `gate-${id}.jpg`,
    size: 136273,
    md5: "d41d8cd98f00b204e9800998ecf8427e",
    capturedAt: "2026-09-20T14:33:30+08:00",
    source: "device" as const,
    url: `/api/gb28181/device-mgmt/snapshots/${id}/content`,
    createdAt: "2026-09-20T14:34:00+08:00"
  };
}

function listResult(rows = [row(1), row(2), row(3)], total = rows.length) {
  return { code: 0, message: "ok", data: { list: rows, total, page: 1, pageSize: 10 } };
}

/**
 * 桩按**真 Arco** 建模，不按"我们以为的 prop"。
 *
 * ⛔ 本仓踩过的坑：Arco 各控件的可见性 prop 名不统一 —— `a-popover` 是
 * `popupVisible`（没有 `visible`），`a-modal`/`a-drawer` 才是 `visible`。
 * 桩要是按错的名字建模，绑错就变成**静默失败**（恒 false / 恒不渲染），
 * 而测试会照着错误的名字断言，全绿什么也没测到。
 */
const stubs = {
  "s-layout-search": {
    template: "<section><slot name='fields' /><slot name='actions' /></section>"
  },
  "a-input": {
    props: ["modelValue", "placeholder"],
    emits: ["update:modelValue", "pressEnter"],
    template:
      "<input :data-testid='$attrs[`data-testid`]' :data-placeholder='placeholder' :value='modelValue' @input='$emit(`update:modelValue`, $event.target.value)' @keyup.enter='$emit(`pressEnter`)' />"
  },
  "a-select": {
    props: ["modelValue", "placeholder"],
    emits: ["update:modelValue"],
    template:
      "<select :data-testid='$attrs[`data-testid`]' :data-placeholder='placeholder' :value='modelValue' @change='$emit(`update:modelValue`, $event.target.value || undefined)'><slot /></select>"
  },
  "a-option": { props: ["value"], template: "<option :value='value'><slot /></option>" },
  "a-range-picker": {
    // ⛔ `name` 必须显式声明：`findComponent({ name: "a-range-picker" })` 按组件名找，
    // 匿名桩找不到（会返回空 wrapper，报 "Cannot call vm on an empty VueWrapper"）。
    name: "a-range-picker",
    props: ["modelValue"],
    emits: ["update:modelValue"],
    // ⛔ 必须声明 `update:modelValue`：页面用 v-model 绑它，桩不声明这个事件时
    // Vue 会把回调落到 nativeOn 上，wrapper.vm.$emit 明明触发了却没人接。
    template: "<div data-testid='library-range' />"
  },
  "a-dropdown": {
    props: ["trigger"],
    emits: ["select"],
    // 真实 Arco 的 dropdown 菜单挂在浮层里；桩把 content slot 直接渲染出来，
    // 否则"点快捷时间"这类交互在测试里根本点不到。
    template: "<div><slot /><slot name='content' /></div>"
  },
  "a-button": {
    props: ["type", "size", "disabled", "loading", "long"],
    emits: ["click"],
    template:
      "<button :data-testid='$attrs[`data-testid`]' :disabled='disabled' @click='$emit(`click`)'><slot name='icon' /><slot name='suffix' /><slot /></button>"
  },
  // ⛔ `.stop` 必须原样透出事件对象：模板里写的是 `@click.stop`，
  // 桩若把 click 事件的参数吃掉，Vue 合成的处理器就会在读 `stopPropagation`
  // 时拿到 undefined 而抛错 —— 报错点在框架内部，与被测组件无关，最难定位。
  "a-link": {
    emits: ["click"],
    template: "<a :data-testid='$attrs[`data-testid`]' @click.prevent.stop='$emit(`click`, $event)'><slot /></a>"
  },
  "a-tag": { template: "<span><slot /></span>" },
  "a-empty": { props: ["description"], template: "<div data-testid='library-empty'>{{ description }}</div>" },
  "a-alert": { props: ["type"], template: "<div><slot /></div>" },
  // ⛔ 桩按真 Arco 建模：`modelValue` + `change(checked)`。
  // 真组件是受控的（选中态由 selectedIds 派生），所以勾选动作必须靠
  // **真实点击**触发 change —— 用 setValue 直接改 value 会绕过我们想验的路径。
  // ⛔ 桩必须把 data-testid 绑在 **<input>** 上，不能靠 $attrs 透传：
  // Vue 会把未声明的 attr 落在**根元素**（这里是 <label>），
  // 于是 `find('[data-testid="library-pick-1"]')` 找到的是 <label>，
  // 而 setChecked 作用在 <label> 上不产生 change —— 表现是"点了没反应 / 已选 0 张"，
  // 排查时会误以为是组件的选中逻辑坏了。
  "a-checkbox": {
    inheritAttrs: false,
    props: ["modelValue"],
    emits: ["change"],
    // ⛔ 默认 slot 也要渲染：真实 `a-checkbox` 支持 `<a-checkbox>文字</a-checkbox>`，
    // 桩吞掉 slot 的话"勾选框旁有说明文字"这条就测不出来。
    template:
      "<label><input type='checkbox' :data-testid='$attrs[`data-testid`]' :checked='modelValue' @change='$emit(`change`, $event.target.checked)' /><slot /></label>"
  },
  "a-pagination": {
    props: ["current", "pageSize", "total"],
    emits: ["change", "pageSizeChange"],
    template:
      "<div data-testid='library-pagination' :data-current='current' :data-total='total'><button data-testid='page-two' @click='$emit(`change`, 2)'>2</button></div>"
  },
  // ⛔ a-drawer 的可见性 prop 就是 `visible`（不是 popupVisible）。
  "a-drawer": {
    props: ["visible", "width", "footer", "escToClose"],
    emits: ["cancel"],
    template: "<aside v-if='visible' data-testid='library-detail-drawer'><slot name='title' /><slot /></aside>"
  }
};

function mountPage() {
  return mount(SnapshotLibrary, { global: { stubs } });
}

describe("snapshot library page", () => {
  beforeEach(() => {
    account.permissions = ["gb28181:device:snapshot"];
    gbApi.listSnapshotLibrary.mockReset();
    replace.mockClear();
    replace.mockResolvedValue(undefined);
    Object.keys(currentRoute.query).forEach(key => delete currentRoute.query[key]);
    // 记录锚点点击的下载，避免测试环境真的发起导航
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function mockClick(this: HTMLAnchorElement) {
      downloads.push({ href: this.getAttribute("href"), download: this.getAttribute("download") });
    });
    downloads = [];
  });

  let downloads: Array<{ href: string | null; download: string | null }> = [];

  it("leaves one search box and drops the quick-range and code-filter affordances", async () => {
    // ⛔ 老板 2026-10-06：去掉「快捷时间」与「按编码精确查询」。
    // keyword 走后端模糊匹配，一个框已经覆盖设备名/通道名/别名/编码片段。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult());
    const wrapper = mountPage();
    await flushPromises();

    const keyword = wrapper.find('[data-testid="library-keyword"]');
    expect(keyword.exists()).toBe(true);
    expect(keyword.attributes("data-placeholder")).toContain("设备名");
    // 快捷时间与两个编码框都该从界面上消失（不是"收起来"，是彻底没有）。
    expect(wrapper.find('[data-testid="library-device-code"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-channel-code"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-advanced-toggle"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-quick-range"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("labels the card with the alias and surfaces both platform ids", async () => {
    // ⛔ 现场人员认的是别名（"东门枪机"），而 channelName 是设备上报的厂家串。
    // 光有 20 位编码没法定位库行，报障时对方要的是"哪一条记录"。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(21, "东门枪机")], 1));
    const wrapper = mountPage();
    await flushPromises();

    const card = wrapper.find(".library-card");
    expect(card.text()).toContain("东门枪机");
    // 上报名与别名不同 ⇒ 并排显示，让人知道这台是设备自报的那个名字。
    expect(card.text()).toContain("IPC-HFW2431S");
    expect(card.text()).toContain("设备 ID");
    expect(card.text()).toContain("通道 ID");
    expect(card.text()).toContain("2");
    wrapper.unmount();
  });

  it("hands the keyword to the backend rather than filtering the page it already has", async () => {
    // ⛔ 页内过滤会让「在第 7 页」和「不存在」长得一模一样 —— 用户无从分辨，
    // 只会反复改条件，而问题在分页。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult());
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-keyword"]').setValue("东门");
    await wrapper.find('[data-testid="library-query"]').trigger("click");
    await flushPromises();

    expect(gbApi.listSnapshotLibrary).toHaveBeenLastCalledWith(expect.objectContaining({ keyword: "东门" }));
    wrapper.unmount();
  });

  it("keeps the explicit time window and sends it as RFC3339", async () => {
    // ⛔ 快捷时间按钮被去掉了，但「按时间段筛」这个能力必须还在（range-picker 保留）——
    // 直接删掉整条时间维度的筛选会让"找上周那批图"变得不可能。
    // ⛔ 格式必须能被后端 time.Parse(RFC3339) 认，否则整页报"参数不合法"。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult());
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="library-range"]').exists()).toBe(true);

    const range = wrapper.findComponent({ name: "a-range-picker" });
    range.vm.$emit("update:modelValue", ["2026-09-19T00:00:00Z", "2026-09-20T23:59:59Z"]);
    await wrapper.find('[data-testid="library-query"]').trigger("click");
    await flushPromises();

    const call = gbApi.listSnapshotLibrary.mock.calls.at(-1)![0];
    expect(Number.isNaN(new Date(call.from).getTime())).toBe(false);
    expect(Number.isNaN(new Date(call.to).getTime())).toBe(false);
    expect(new Date(call.to).getTime()).toBeGreaterThan(new Date(call.from).getTime());
    wrapper.unmount();
  });

  it("shows the download entry on every card and keeps the token on the url", async () => {
    // ⛔ `<a>` 和 `<img>` 一样带不了 Authorization 头：不补 token 的表现是
    // "下载下来一个几百字节的 401 页面"，不报错，只有打开文件才发现。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(11)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-download-11"]').trigger("click");
    expect(downloads).toHaveLength(1);
    expect(downloads[0].href).toContain("token=tok-1");
    expect(downloads[0].download).toBe("gate-11.jpg");
    wrapper.unmount();
  });

  it("downloads every picked image instead of silently skipping all but the first", async () => {
    // ⛔ 浏览器会把连续同步点击判成非用户触发而拦掉，表现为"选了3张只下来1张"。
    // 实现里逐张间隔 250ms，所以这里必须驱动定时器 —— flushPromises 只排空
    // 微任务队列，不会让真实 setTimeout 回调跑起来。
    vi.useFakeTimers();
    try {
      gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2), row(3)]));
      const wrapper = mountPage();
      await flushPromises();

      // ⛔ 勾选框只在批量模式渲染（老板 2026-10-06），必须先进去再勾。
      await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
      await flushPromises();
      await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
      await wrapper.find('[data-testid="library-pick-3"]').setValue(true);
      await flushPromises();

      const bar = wrapper.find('[data-testid="library-batch-bar"]');
      expect(bar.exists()).toBe(true);
      expect(bar.text()).toContain("已选 2 张");

      wrapper.find('[data-testid="library-batch-download"]').trigger("click");
      // 放掉逐张之间的间隔：最后一张后面本来就不排程。
      for (let i = 0; i < 6; i++) {
        await vi.advanceTimersByTimeAsync(250);
      }
      await flushPromises();

      expect(downloads.map(item => item.download)).toEqual(["gate-1.jpg", "gate-3.jpg"]);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("adds up only what it can actually fetch when bulk downloading", async () => {
    // ⛔ 前端只拿到当前页：勾一个自己都拉不到的集合等于骗人。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)], 998));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await wrapper.find('[data-testid="library-batch-download"]').trigger("click");
    await flushPromises();

    // 只下本页这张，total=998 不能变成"已选998张"的假象。
    expect(downloads).toHaveLength(1);
    expect(messages.success).toHaveBeenCalledWith(expect.stringContaining("1 张"));
    wrapper.unmount();
  });

  it("opens a detail drawer that exposes the name and md5 for copying", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find(".library-thumb").trigger("click");
    await flushPromises();

    const drawer = wrapper.find('[data-testid="library-detail-drawer"]');
    expect(drawer.exists()).toBe(true);
    expect(drawer.text()).toContain("gate-1.jpg");
    // md5 界面上是短显，但复制按钮在（比对文件时必须能拿到全量）。
    expect(drawer.text()).toContain("d41d8cd9…427e");
    expect(wrapper.find('[data-testid="library-copy-md5-1"]').exists()).toBe(true);
    wrapper.unmount();
  });

  it("tells an over-narrow filter apart from a library that has nothing yet", async () => {
    // ⛔ 不区分的话"没搜到"和"库里本来就没图"是同一句话，用户会去反复改条件，
    // 而真正的问题在部署侧（根本没抓拍过）。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([], 0));
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="library-empty"]').text()).toContain("图像库还是空的");

    await wrapper.find('[data-testid="library-keyword"]').setValue("不存在的名字");
    await wrapper.find('[data-testid="library-query"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="library-empty"]').text()).toContain("放宽时间范围");
    wrapper.unmount();
  });

  it("keeps every shareable filter in the URL so a refresh lands on the same list", async () => {
    // ⛔ 原来只有会话/编码进 URL，于是"把这批图发给同事"丢掉的正是时间窗/来源/关键词。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult());
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-keyword"]').setValue("东门");
    await wrapper.find('[data-testid="library-query"]').trigger("click");
    await flushPromises();

    expect(replace).toHaveBeenCalledWith({ query: expect.objectContaining({ keyword: "东门" }) });
    wrapper.unmount();
  });

  it("keeps the checkboxes hidden until the user asks for batch selection", async () => {
    // ⛔ 老板 2026-10-06：勾选框**不要默认存在**。
    // 浏览态是主路径，每张图挂一个复选框会让人以为"进来就得做选择"，
    // 而大多数人只是来找图看图的。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="library-batch-toggle"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="library-pick-1"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-batch-bar"]').exists()).toBe(false);

    // 点「批量选择」后：勾选框出现，按钮文案变成"退出批量选择"。
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="library-pick-1"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="library-pick-2"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="library-batch-toggle"]').text()).toContain("退出批量选择");
    // ⛔ 工具条是「进入批量模式」就出现，不是「有选中」才出现 ——
    // 否则会出现"没勾选就看不到已选 0 张"，用户以为自己没选上。
    expect(wrapper.find('[data-testid="library-batch-bar"]').text()).toContain("已选 0 张");
    wrapper.unmount();
  });

  it("puts the checkbox in the card's bottom row instead of on top of the image", async () => {
    // ⛔ 勾选框压在图片上会盖住画面左上角，而那正是判断"拍到没有"的关键区域。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();

    // 勾选框在 `.library-card-actions`（body 底部行）里，不在 `.library-thumb`（图片区）里。
    const inActions = wrapper.find(".library-card-actions [data-testid='library-pick-1']");
    expect(inActions.exists()).toBe(true);
    expect(wrapper.find(".library-thumb [data-testid='library-pick-1']").exists()).toBe(false);
    wrapper.unmount();
  });

  it("drops the selection when leaving batch mode so the next run starts clean", async () => {
    // ⛔ 退出不清已选 ⇒ 下次进入时「已选 N 张」凭空出现，
    // 而用户一张都没勾过 —— 批量下载会直接下错文件。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-bar"]').text()).toContain("已选 1 张");

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="library-pick-1"]').exists()).toBe(false);
    // 再次进入必须是 0 张，而不是 1 张。
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-bar"]').text()).toContain("已选 0 张");
    wrapper.unmount();
  });

  it("clears the url and the selection on reset", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-bar"]').exists()).toBe(true);

    await wrapper.find('[data-testid="library-reset"]').trigger("click");
    await flushPromises();

    expect(wrapper.find('[data-testid="library-batch-bar"]').exists()).toBe(false);
    expect(gbApi.listSnapshotLibrary).toHaveBeenLastCalledWith({ page: 1, pageSize: 10 });
    wrapper.unmount();
  });

  it("shows a permission notice instead of the grid for an account without the permission", async () => {
    // ⛔ 菜单可见性与接口授权同码，但这层门禁不能省：少了它越权用户手输 URL
    // 就能看到别人的图（后端也会拦，但页面会先报一堆 403）。
    account.permissions = [];
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult());
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="library-grid"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-keyword"]').exists()).toBe(false);
    expect(gbApi.listSnapshotLibrary).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});
