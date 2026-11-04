import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { reactive } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

const gbApi = vi.hoisted(() => ({ listSnapshotLibrary: vi.fn(), deleteSnapshots: vi.fn() }));

// ⛔ 源码断言需要真实文件内容：勾选框"外层不能包 label"这类不变量
// 靠渲染结果测不出来（桩渲染不出浏览器对 label 的默认激活行为）。
const pageSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/snapshot-library/index.vue"), "utf8");
const accountState = vi.hoisted(() => ({
  permissions: ["gb28181:snapshot:view", "gb28181:snapshot:download", "gb28181:snapshot:delete"] as string[]
}));
const account = reactive(accountState);
const messages = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn() }));
const modal = vi.hoisted(() => ({ confirm: vi.fn() }));
const authState = vi.hoisted(() => ({ accessToken: "tok-1" }));

const replace = vi.hoisted(() => vi.fn(() => Promise.resolve()));
const currentRoute = reactive({ query: {} as Record<string, unknown> });

vi.mock("@/api/gb28181", async importOriginal => ({
  ...(await importOriginal<typeof import("@/api/gb28181")>()),
  listSnapshotLibrary: gbApi.listSnapshotLibrary,
  deleteSnapshots: gbApi.deleteSnapshots
}));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
vi.mock("@/utils/auth", () => ({ getAccessToken: () => authState }));
vi.mock("@/api/utils", () => ({ getBaseUrl: () => "" }));
vi.mock("@arco-design/web-vue", async importOriginal => ({
  ...(await importOriginal<typeof import("@arco-design/web-vue")>()),
  Message: messages,
  Modal: modal
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
  // ⛔ 2026-11-04 起批量删除改成 a-popconfirm 气泡：点按钮只打开气泡，
  //    必须再点气泡里的 ok 才真正删除。stub 把这两步拆开暴露给用例。
  "a-popconfirm": {
    emits: ["ok"],
    props: ["disabled", "okText", "cancelText", "okButtonProps"],
    template:
      "<span v-bind='$attrs'><span class='popconfirm-content'><slot name='content' /></span><slot /><button class='popconfirm-ok' :disabled='disabled' @click='$emit(`ok`)'>{{ okText }}</button></span>"
  },
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
    name: "a-button",
    props: ["type", "status", "size", "disabled", "loading", "long"],
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
    emits: ["change", "click"],
    // ⛔ 默认 slot 也要渲染：真实 `a-checkbox` 支持 `<a-checkbox>文字</a-checkbox>`，
    // 桩吞掉 slot 的话"勾选框旁有说明文字"这条就测不出来。
    template:
      "<label @click='$emit(`click`, $event)'><input type='checkbox' :data-testid='$attrs[`data-testid`]' :checked='modelValue' @change='$emit(`change`, $event.target.checked)' /><slot /></label>"
  },
  "a-pagination": {
    props: ["current", "pageSize", "total"],
    emits: ["change", "pageSizeChange"],
    template:
      "<div data-testid='library-pagination' :data-current='current' :data-total='total'><button data-testid='page-two' @click='$emit(`change`, 2)'>2</button></div>"
  },
  // ⛔ a-drawer 的可见性 prop 就是 `visible`（不是 popupVisible）。
  "a-image-preview": {
    props: ["visible", "src"],
    emits: ["update:visible"],
    template:
      "<div v-if='visible' data-testid='library-image-preview'><img :src='src' /><button data-testid='close-preview' @click='$emit(`update:visible`, false)'>关闭</button></div>"
  },
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
    account.permissions = ["gb28181:snapshot:view", "gb28181:snapshot:download", "gb28181:snapshot:delete"];
    gbApi.listSnapshotLibrary.mockReset();
    replace.mockClear();
    replace.mockResolvedValue(undefined);
    modal.confirm.mockReset();
    Object.keys(currentRoute.query).forEach(key => delete currentRoute.query[key]);
    // 记录锚点点击的下载，避免测试环境真的发起导航
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function mockClick(this: HTMLAnchorElement) {
      downloads.push({ href: this.getAttribute("href"), download: this.getAttribute("download") });
    });
    downloads = [];
    // 批量下载会先发一次 preflight（只回 JSON）再触发 `<a>`：
    // ⛔ 这一步必须桩掉，否则 happy-dom 会真的发网络请求，测试变成"看机器能不能上网"。
    fetchMock = vi.fn(async () => preflightResponse({ code: 0, message: "", data: { count: 0, skipped: 0, bytes: 0 } }));
    vi.stubGlobal("fetch", fetchMock);
  });

  let downloads: Array<{ href: string | null; download: string | null }> = [];
  let fetchMock: ReturnType<typeof vi.fn>;

  /** preflight 的响应桩：只实现页面真正读的那三样（ok / json / status）。 */
  function preflightResponse(payload: unknown, ok = true, status = 200) {
    return { ok, status, json: async () => payload } as unknown as Response;
  }

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

  it("labels the card with the alias and shows the 20-digit GB codes", async () => {
    // ⛔ 现场人员认的是别名（"东门枪机"），而 channelName 是设备上报的厂家串。
    // ⛔ 两行 ID 显示的是**20 位国标编码**，不是库主键（老板 2026-10-21 纠正）：
    // 主键 7/9 对现场毫无意义，编码才是设备贴纸、SIP 报文、上级目录里用的那个。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(21, "东门枪机")], 1));
    const wrapper = mountPage();
    await flushPromises();

    const card = wrapper.find(".library-card");
    expect(card.text()).toContain("东门枪机");
    // 上报名与别名不同 ⇒ 并排显示，让人知道这台是设备自报的那个名字。
    expect(card.text()).toContain("IPC-HFW2431S");
    expect(card.text()).toContain("设备编码");
    expect(card.text()).toContain("通道编码");
    expect(card.text()).toContain("37010301021320000311");
    expect(card.text()).toContain("37010301021320000312");
    // ⛔ 反证：不能回落到库主键（界面上出现 7 / 9 就是回退了）。
    expect(card.text()).not.toContain("设备 ID");
    expect(card.text()).not.toContain("通道 ID");
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

  it("packs every picked image into one zip request instead of N downloads", async () => {
    // 老板 2026-11-04 的口径：选 2 张下来**一个** zip，不是 2 个文件。
    // ⛔ 逐张触发 `<a download>` 的老做法会被浏览器判成弹窗批量拦截，
    // 表现为"选了 3 张只下来 1 张"，所以这里钉住"只触发一次锚点"。
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

    fetchMock.mockResolvedValueOnce(preflightResponse({ code: 0, message: "", data: { count: 2, skipped: 0, bytes: 2048 } }));
    await wrapper.find('[data-testid="library-batch-download"]').trigger("click");
    await flushPromises();

    // 第一次请求是 preflight：只问"这 2 张能打包吗"，不带 ids 之外的参数暴露给下载。
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const probeUrl = String(fetchMock.mock.calls[0][0]);
    expect(probeUrl).toContain("/api/gb28181/device-mgmt/snapshots/archive?");
    expect(probeUrl).toContain("ids=1%2C3");
    expect(probeUrl).toContain("preflight=1");
    // ⛔ 预检是普通 fetch ⇒ 令牌走请求头，不进 URL：否则令牌会落进 nginx 访问日志。
    expect(probeUrl).not.toContain("token=");
    const probeHeaders = (fetchMock.mock.calls[0][1] as { headers?: Record<string, string> } | undefined)?.headers ?? {};
    expect(probeHeaders.Authorization).toBe("Bearer tok-1");

    // 真正的下载只触发一次，且**不带** preflight（那一次只回 JSON）。
    // ⛔ 下载走 `<a download>`，浏览器原生发起加不了头 ⇒ 这一步只能用 ?token=。
    expect(downloads).toHaveLength(1);
    expect(downloads[0].href).toContain("ids=1%2C3");
    expect(downloads[0].href).toContain("token=tok-1");
    expect(downloads[0].href).not.toContain("preflight");
    expect(downloads[0].download).toMatch(/^抓拍图-\d{8}-\d{6}\.zip$/);
    expect(messages.success).toHaveBeenCalledWith(expect.stringContaining("2 张"));
    wrapper.unmount();
  });

  it("reports the images that could not be packed instead of pretending the pack is complete", async () => {
    // ⛔ 库行还在、磁盘文件没了是运营常态（手工清过目录）。这时照常出包，
    // 但**必须说出来**：用户对着列表数张数，包里少了会以为是系统丢图。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();

    fetchMock.mockResolvedValueOnce(preflightResponse({ code: 0, message: "", data: { count: 1, skipped: 2, bytes: 1024 } }));
    await wrapper.find('[data-testid="library-batch-download"]').trigger("click");
    await flushPromises();

    expect(downloads).toHaveLength(1);
    expect(messages.warning).toHaveBeenCalledWith(expect.stringContaining("2 张"));
    wrapper.unmount();
  });

  it("never triggers the download when the preflight refuses", async () => {
    // ⭐ 这就是 preflight 存在的理由：`<a download>` 拿不到失败状态 ——
    // 后端回 400 时浏览器会把那段 JSON 当文件存下来，用户以为下到了 zip 却打不开。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();

    fetchMock.mockResolvedValueOnce(
      preflightResponse({ code: 400, message: "选中的图片文件都不在服务器上", data: null }, false, 400)
    );
    await wrapper.find('[data-testid="library-batch-download"]').trigger("click");
    await flushPromises();

    expect(downloads, "预检不通过时不许触发下载（否则会下一个装着错误信息的文件）").toHaveLength(0);
    expect(messages.error).toHaveBeenCalledWith(expect.stringContaining("不在服务器上"));
    wrapper.unmount();
  });

  it("asks the backend only for the images the current page actually selected", async () => {
    // ⛔ 前端只拿到当前页：勾一个自己都拉不到的集合等于骗人。
    // total=998 只说明"匹配很多"，不能变成"已选998张"的假象，也不能进 ids。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)], 998));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();

    fetchMock.mockResolvedValueOnce(preflightResponse({ code: 0, message: "", data: { count: 1, skipped: 0, bytes: 1024 } }));
    await wrapper.find('[data-testid="library-batch-download"]').trigger("click");
    await flushPromises();

    expect(String(fetchMock.mock.calls[0][0])).toContain("ids=1");
    expect(downloads).toHaveLength(1);
    wrapper.unmount();
  });

  it("opens a detail drawer that exposes the name and md5 for copying", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-detail-1"]').trigger("click");
    await flushPromises();

    const drawer = wrapper.find('[data-testid="library-detail-drawer"]');
    expect(drawer.exists()).toBe(true);
    expect(drawer.text()).toContain("gate-1.jpg");
    // md5 界面上是短显，但复制按钮在（比对文件时必须能拿到全量）。
    expect(drawer.text()).toContain("d41d8cd9…427e");
    expect(wrapper.find('[data-testid="library-copy-md5-1"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="library-image-preview"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("previews the clicked image with authentication without opening details or downloading", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.findAll(".library-thumb")[1].trigger("click");
    const preview = wrapper.find('[data-testid="library-image-preview"]');
    expect(preview.exists()).toBe(true);
    expect(preview.find("img").attributes("src")).toBe(wrapper.findAll(".library-thumb img")[1].attributes("src"));
    expect(preview.find("img").attributes("src")).toContain("token=tok-1");
    expect(wrapper.find('[data-testid="library-detail-drawer"]').exists()).toBe(false);
    expect(downloads).toHaveLength(0);

    await wrapper.find('[data-testid="close-preview"]').trigger("click");
    expect(wrapper.find('[data-testid="library-image-preview"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-detail-drawer"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("provides separate accessible icon buttons for download and details", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    for (const [action, label] of [
      ["download", "下载"],
      ["detail", "详情"]
    ]) {
      const button = wrapper.find(`[data-testid="library-${action}-1"]`);
      expect(button.element.tagName).toBe("BUTTON");
      expect(button.attributes("aria-label")).toBe(label);
      expect(button.text()).toBe("");
      expect(button.find("svg").exists()).toBe(true);
    }
    await wrapper.find('[data-testid="library-download-1"]').trigger("click");
    expect(downloads).toHaveLength(1);
    expect(wrapper.find('[data-testid="library-detail-drawer"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-image-preview"]').exists()).toBe(false);
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

  it("uses semantic colors for batch actions", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();
    const button = (testId: string) =>
      wrapper.findAllComponents({ name: "a-button" }).find(button => button.attributes("data-testid") === testId);

    // 选择族 = 紫（uvp-select-btn）。⛔ 2026-11-04 老板：「每个按钮颜色都不一样」，
    // 原来闲置态借次级浅蓝、进入批量后借中性灰 ⇒ 跟「重置」「全选本页」撞成三颗一样。
    expect(button("library-batch-toggle")?.classes()).toContain("uvp-select-btn");
    expect(button("library-batch-toggle")?.classes()).not.toContain("uvp-btn-neutral");
    expect(button("library-batch-toggle")?.props("type")).not.toBe("primary");
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    // 进入批量模式 → 文案变「退出批量选择」，但**颜色不跟着切**：
    // 它是位置固定的模式开关，颜色随模式跳会显得闪。
    expect(button("library-batch-toggle")?.classes()).toContain("uvp-select-btn");
    expect(button("library-batch-toggle")?.props("type")).not.toBe("primary");
    // 全选本页 = 天蓝；清空选择 = 琥珀。两者原来是「次级浅蓝」「无底文字」，
    // 现在是按钮就得有按钮的形和各自的色相。
    expect(button("library-select-all")?.classes()).toContain("uvp-sky-btn");
    expect(button("library-clear-selection")?.classes()).toContain("uvp-clear-btn");
    expect(button("library-clear-selection")?.props("type")).not.toBe("text");
    // 下载/导出 = 只取数不改数 ⇒ 青绿（uvp-export-btn），与「刷新」同色相。
    expect(button("library-batch-download")?.classes()).toContain("uvp-export-btn");
    expect(button("library-batch-download")?.props("type")).not.toBe("primary");
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    // 工具栏删除用**软红**（只给 status="danger"）；实心红是确认弹窗专用。
    expect(button("library-batch-delete")?.props()).toMatchObject({ status: "danger" });
    expect(button("library-batch-delete")?.props("type")).not.toBe("primary");
    wrapper.unmount();
  });

  it("selects and deselects only the clicked card across its body and image in batch mode", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    const wrapper = mountPage();
    await flushPromises();
    const card = wrapper.find('[data-snapshot-id="1"]');
    await card.trigger("click");
    expect(card.classes()).not.toContain("library-card--selected");
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");

    await card.find(".library-title").trigger("click");
    expect(card.classes()).toContain("library-card--selected");
    expect(wrapper.find('[data-snapshot-id="2"]').classes()).not.toContain("library-card--selected");
    await card.find(".library-card-info").trigger("click");
    expect(card.classes()).not.toContain("library-card--selected");
    await card.find(".library-thumb").trigger("click");
    expect(card.classes()).toContain("library-card--selected");
    expect(wrapper.find('[data-testid="library-image-preview"]').exists()).toBe(false);
    await card.find(".library-thumb").trigger("click");
    expect(card.classes()).not.toContain("library-card--selected");
    wrapper.unmount();
  });

  it("keeps checkbox, download and detail clicks from toggling the card twice", async () => {
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    const wrapper = mount(SnapshotLibrary, { attachTo: document.body, global: { stubs } });
    await flushPromises();
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    const card = wrapper.find('[data-snapshot-id="1"]');
    const checkbox = wrapper.find('[data-testid="library-pick-1"]');

    await checkbox.trigger("click");
    expect(card.classes()).toContain("library-card--selected");
    await checkbox.trigger("click");
    expect(card.classes()).not.toContain("library-card--selected");
    await wrapper.find('[data-testid="library-download-1"]').trigger("click");
    expect(downloads).toHaveLength(1);
    expect(card.classes()).not.toContain("library-card--selected");
    await wrapper.find('[data-testid="library-detail-1"]').trigger("click");
    expect(wrapper.find('[data-testid="library-detail-drawer"]').exists()).toBe(true);
    expect(card.classes()).not.toContain("library-card--selected");
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

  it("deletes the picked images and leaves the list clean afterwards", async () => {
    // ⛔ 老板 2026-10-07 要求加「批量删除」。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    gbApi.deleteSnapshots.mockResolvedValue({ data: { deleted: 2, filesFailed: 0 } });
    const wrapper = mountPage();
    await flushPromises();

    // 没勾选时**不显示**删除入口：避免"删 0 张"这种无意义请求。
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-delete"]').exists()).toBe(false);

    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await wrapper.find('[data-testid="library-pick-2"]').setValue(true);
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-delete"]').exists()).toBe(true);

    await wrapper.find('[data-testid="library-batch-delete"]').trigger("click");
    // 点按钮只打开气泡，不落库；气泡文案要写明张数与「不可恢复」
    expect(gbApi.deleteSnapshots).not.toHaveBeenCalled();
    const bubbleCopy = wrapper.get(".popconfirm-content").text();
    expect(bubbleCopy).toContain("2 张");
    expect(bubbleCopy).toContain("不可恢复");
    await wrapper.get(".popconfirm-ok").trigger("click");
    await flushPromises();

    expect(gbApi.deleteSnapshots).toHaveBeenCalledWith([1, 2]);
    // ⛔ 删完必须退出批量模式：selectedIds 里留着已删 id 会让工具条一直显示
    // 「已选 2 张」而列表只剩 0 张，用户以为删失败又去点删除。
    expect(wrapper.find('[data-testid="library-pick-1"]').exists()).toBe(false);
    // ⛔ 重新拉列表而不是前端 filter —— 后端才是权威（可能有越权行被跳过）。
    expect(gbApi.listSnapshotLibrary).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("keeps the selection when the delete call fails so a retry needs no re-picking", async () => {
    // ⛔ 失败时清空选择 = 用户得重新勾一遍，体验上像"删掉了又没删掉"。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    gbApi.deleteSnapshots.mockRejectedValue(new Error("删除失败"));
    const wrapper = mountPage();
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();

    await wrapper.find('[data-testid="library-batch-delete"]').trigger("click");
    await wrapper.get(".popconfirm-ok").trigger("click");
    await flushPromises();

    // 还在批量模式、选择还在 ⇒ 用户可直接重试。
    expect(wrapper.find('[data-testid="library-pick-1"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="library-batch-bar"]').text()).toContain("已选 1 张");
    wrapper.unmount();
  });

  it("ticking one card never ticks another one", async () => {
    // ⛔⛔ 老板 2026-10-07 报「勾第一张第二张也跟着勾」。根因**不在前端**：
    // 后端 JOIN 一对多把同一张图渲染两次（同一 id），Vue `:key` 相同 ⇒ 复用同一 DOM。
    // 但前端仍要钉住这条：一旦数据层出问题（同一 id 出现两次），
    // 至少「勾选不会传染」是本组件必须保证的不变量。
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1), row(2)]));
    const wrapper = mountPage();
    await flushPromises();
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();

    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();
    // ⛔ 只勾了 1 张 ⇒ 工具条必须是「已选 1 张」。曾经因为两行 id 相同
    // 而显示「已选 2 张」—— 那是数据放大，不是勾选逻辑。
    expect(wrapper.find('[data-testid="library-batch-bar"]').text()).toContain("已选 1 张");
    // ⛔ 勾选框外**不能再包一层 <label>**：a-checkbox 内部的 input 没有 id、
    // 外层 label 也没有 for ⇒ 点「选这张」文字时浏览器找不到关联控件，
    // 会回退激活页面里**第一个**可聚焦元素（表现为"勾 A 勾上 B"）。
    const wrapped = pageSource.indexOf('<label v-if="batchMode" class="library-pick"');
    expect(wrapped).toBe(-1);
    expect(pageSource).not.toContain('<label v-if="batchMode" class="library-pick"');
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

  it("hides every destructive entry for a view-only account", async () => {
    // ⛔ 2026-11-04 拆分三档权限后的核心行为：只有查看权的账号（审核/演示）
    // 必须看不到下载和删除，连「批量选择」都不该露出来（勾了也没动作可做）。
    account.permissions = ["gb28181:snapshot:view"];
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="library-grid"]').exists()).toBe(true);
    expect(wrapper.find('[data-testid="library-batch-toggle"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-batch-download"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="library-download-1"]').exists()).toBe(false);

    // 详情抽屉里的下载按钮同样不能出现。
    await wrapper.find('[data-testid="library-detail-1"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="library-detail-download"]').exists()).toBe(false);
    wrapper.unmount();
  });

  it("keeps delete invisible when the account may download but not delete", async () => {
    // 「能下载 ≠ 能删除」：两个按钮各自绑自己的权限码，不能一处放行处处放行。
    account.permissions = ["gb28181:snapshot:view", "gb28181:snapshot:download"];
    gbApi.listSnapshotLibrary.mockResolvedValue(listResult([row(1)]));
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.find('[data-testid="library-batch-toggle"]').exists()).toBe(true);
    await wrapper.find('[data-testid="library-batch-toggle"]').trigger("click");
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-download"]').exists()).toBe(true);

    await wrapper.find('[data-testid="library-pick-1"]').setValue(true);
    await flushPromises();
    expect(wrapper.find('[data-testid="library-batch-delete"]').exists()).toBe(false);
    wrapper.unmount();
  });
});
