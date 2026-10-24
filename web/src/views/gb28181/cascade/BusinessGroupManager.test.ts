import { DOMWrapper, flushPromises, mount } from "@vue/test-utils";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineComponent, h } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Modal } from "@arco-design/web-vue";
import type { CascadeBusinessGroup } from "@/api/gb28181";

const api = vi.hoisted(() => ({ get: vi.fn(), getPlatform: vi.fn(), replace: vi.fn(), push: vi.fn() }));
vi.mock("@/api/gb28181", () => ({
  getCascadeBusinessGroup: api.get,
  getCascadePlatform: api.getPlatform,
  replaceCascadeBusinessGroup: api.replace,
  pushCascadeCatalog: api.push
}));

/** 「在线状态」列要按来源设备回查通道状态 —— 单测里必须一并 mock，否则会打真实 HTTP。 */
const deviceApi = vi.hoisted(() => ({ getDevice: vi.fn(), listChannels: vi.fn() }));
vi.mock("@/views/gb28181/device-mgmt/api", () => ({
  getDevice: deviceApi.getDevice,
  listChannels: deviceApi.listChannels
}));

import BusinessGroupManager from "./BusinessGroupManager.vue";

const root = {
  code: "34020000002150000001",
  name: "业务根",
  kind: "group" as const,
  parentCode: "",
  sort: 0
};
const baseData = (platformId = 7): CascadeBusinessGroup => ({
  platformId,
  revision: 4,
  nodes: [root],
  bindings: [],
  channels: [
    {
      sourceChannelId: 31,
      sourceDeviceId: 3,
      publishedChannelId: "34020000001320000031",
      name: "已共享通道",
      parentOverride: "",
      ptzAllowed: false,
      active: true
    }
  ]
});

/**
 * 取**弹窗里**的元素。
 *
 * ⛔ 弹窗内容 `wrapper.get()` 恒找不到，是两个原因叠在一起（2026-10-14 踩透）：
 *   ① `BusinessGroupManager.vue` 把 Arco 组件**显式 import 成别名**（`Modal as AModal` 等），
 *      SFC 里的 `<a-modal>` 解析到这个**本地绑定**、根本不走 `resolveComponent` 的全局回退
 *      ⇒ `config.global.stubs` / `mount({ global: { stubs } })` 里那个 `a-modal` 桩被**静默绕过**，
 *         跑的是真 Arco Modal（`a-tree` / `a-checkbox` 同理，本文件里它们也是真组件）；
 *   ② 真 Arco Modal 默认 `renderToBody`，内容被 `<Teleport>` 到 `document.body`。
 * ⇒ 弹窗内一律走本函数。找不到**直接抛**，避免拿到空断言（"元素不存在"和"断言恒真"要能分开）。
 */
function dialogEl(selector: string) {
  const element = document.body.querySelector(selector);
  if (!element) throw new Error(`弹窗里没有 ${selector}（它在 document.body 上，不在 wrapper 上）`);
  return new DOMWrapper(element);
}

/**
 * 二次确认一律走 Arco 的**函数式** `Modal.confirm`（2026-10-15 老板要求：
 * 「所有这种提示都改成 confirm 的组件」—— 页面内嵌的确认条全部拆掉）。
 *
 * ⛔ 函数式弹窗**没有可点的模板 DOM**：它自己往 `document.body` 挂容器、自己 render，
 *   `wrapper.get()` 永远拿不到；所以断言只能落在"传进去的那份 config"上，
 *   再手动调 `onOk` / `onCancel`（与 `firmware-repo/index.test.ts` 同一套路）。
 * ⛔ 只能 `spyOn`，**不能**像 firmware-repo 那样整体 `vi.mock("@arco-design/web-vue")`：
 *   本组件把 `Modal` **同时当组件用**（`import { Modal as AModal }` ⇒ 模板里的 `<a-modal>`），
 *   把 `Modal` 换成 `{ confirm: vi.fn() }` 会让业务分组编辑弹窗整个渲染不出来。
 */
let confirmConfigs: any[] = [];
let confirmSpy: ReturnType<typeof vi.spyOn>;

/** 最近一次 confirm 的配置。没弹过就直接抛 —— "没弹窗"和"断言恒真"必须分得开。 */
function lastConfirm(): any {
  if (!confirmConfigs.length) throw new Error("没有弹出任何 confirm 弹窗");
  return confirmConfigs[confirmConfigs.length - 1];
}

/** 点弹窗的「确定」。`onOk` 可能是 async（保存并离开），所以这里必须 await 它。 */
async function acceptConfirm() {
  const config = lastConfirm();
  await config.onOk?.();
  await flushPromises();
}

/** 点弹窗的「取消」/ 右上角 ×（Arco 里两者都走 `onCancel`）。 */
async function dismissConfirm() {
  const config = lastConfirm();
  await config.onCancel?.();
  await flushPromises();
}

/**
 * `a-tree-select` 的桩（弹窗里「上级组织」用的就是它，2026-10-15 从扁平 `a-select` 换过来）。
 *
 * ⛔ 本文件不注册 Arco 全局组件 ⇒ 不桩的话 `<a-tree-select>` 在单测里是个解析不出来的未知标签，
 *   **选项一个都不会渲染**（"层级"这件事直接测不到，等于没测）。
 * ⛔ 桩必须把**树拍平成带层级的 option**（`data-depth` = 缩进层数），不能只列一层：
 *   真组件靠缩进 + 连接线表达父子，桩若拍平得没有层级，把 `data` 传成扁平数组也照样绿 ——
 *   那样这道门禁就是恒真的。同时把 `defaultExpandAll` / `allow-search` 落到属性上，
 *   它们决定"下级看不看得见、能不能搜索"，是最容易在后续重构里被删掉的两个 prop。
 */
const TreeSelectStub = defineComponent({
  name: "ATreeSelect",
  props: {
    modelValue: { type: [String, Number], default: "" },
    disabled: Boolean,
    data: { type: Array, default: () => [] },
    fieldNames: { type: Object, default: undefined },
    treeProps: { type: Object, default: undefined },
    allowSearch: Boolean
  },
  emits: ["update:modelValue"],
  setup(props, { attrs, emit }) {
    type RawNode = Record<string, unknown>;
    const rows = () => {
      const fields = (props.fieldNames as { key: string; title: string; children: string } | undefined) ?? {
        key: "key",
        title: "title",
        children: "children"
      };
      const result: Array<{ key: string; title: string; depth: number }> = [];
      const walk = (list: RawNode[], depth: number) => {
        list.forEach(node => {
          result.push({ key: String(node[fields.key]), title: String(node[fields.title] ?? ""), depth });
          walk((node[fields.children] as RawNode[] | undefined) ?? [], depth + 1);
        });
      };
      walk((props.data ?? []) as RawNode[], 0);
      return result;
    };
    return () =>
      h(
        "select",
        {
          ...attrs,
          "data-expand-all": String((props.treeProps as { defaultExpandAll?: boolean } | undefined)?.defaultExpandAll === true),
          "data-allow-search": String(props.allowSearch === true),
          value: props.modelValue ?? "",
          disabled: props.disabled,
          onChange: (event: Event) => emit("update:modelValue", (event.target as HTMLSelectElement).value)
        },
        rows().map(row =>
          h("option", { key: row.key, value: row.key, "data-depth": row.depth }, `${"　".repeat(row.depth)}${row.title}`)
        )
      );
  }
});

function mountManager(props: Record<string, unknown> = {}) {
  return mount(BusinessGroupManager, {
    props: {
      visible: true,
      platformId: 7,
      platformName: "上级 A",
      localDeviceId: "34020000002000000001",
      canWrite: true,
      canManage: true,
      ...props
    },
    global: {
      stubs: {
        BusinessChannelPicker: {
          name: "BusinessChannelPicker",
          props: ["targetPath", "targetCode", "channels", "bindings", "nodes", "platformName"],
          emits: ["apply", "cancel"],
          template: "<div class='picker-stub'></div>"
        },
        "a-modal": {
          // ⛔ 这个桩**对业务分组弹窗无效**：`BusinessGroupManager.vue` 里
          //    `import { Modal as AModal }`，SFC 的 `<a-modal>` 解析到该本地绑定，绕过全局桩；
          //    真正跑到的是真 Arco Modal，内容 Teleport 到 `document.body`。
          //    留在这里只给"没被显式 import 的 `<a-modal>`"兜底，弹窗断言一律用 `dialogEl()`。
          template: "<div><slot /><slot name='footer' /></div>"
        },
        // ⛔ 测试环境里 Arco 组件不一定能按需解析（未解析时 `<a-badge>` 不渲染文案），
        //    所以自己 stub 一个把 status/text 暴露出来，在线状态列的断言才可靠。
        "a-badge": {
          props: ["status", "text"],
          template: `<span class="badge-stub" :data-status="status">{{ text }}</span>`
        },
        "a-spin": { template: "<div><slot /></div>" },
        "a-alert": { template: "<div><slot /></div>" },
        "a-empty": { template: "<div><slot /></div>" },
        "a-dropdown": {
          props: ["popupVisible"],
          emits: ["popupVisibleChange"],
          template: `<div @contextmenu.prevent="$emit('popupVisibleChange', true)"><div @click="$emit('popupVisibleChange', !popupVisible)"><slot /></div><div v-if="popupVisible" role="menu"><slot name="content" /></div></div>`
        },
        "a-dsubmenu": {
          props: ["disabled"],
          template: `<div><slot name="title" /><slot /></div>`
        },
        "a-doption": {
          props: ["disabled"],
          template: `<button role="menuitem" :disabled="disabled"><slot /></button>`
        },
        "a-select": {
          props: ["modelValue", "disabled"],
          emits: ["update:modelValue"],
          template: `<select :value="modelValue" :disabled="disabled" :aria-label="$attrs['aria-label']" @change="$emit('update:modelValue', $event.target.value)"><slot /></select>`
        },
        "a-option": { props: ["value"], template: "<option :value='value'><slot /></option>" },
        "a-tree-select": TreeSelectStub
      }
    }
  });
}

/**
 * 接口 + confirm 的统一桩。
 *
 * ⛔ 本文件里除了主 describe，还有几个**顶层 describe**（confirm 门禁 / 树图标 / 批量布局…）。
 *   顶层 describe 继承不到主 describe 里的 `beforeEach` —— 不自己补 mock 的话
 *   `getCascadePlatform` 返回 undefined，工作区连 tab 都渲染不出来。
 *   ⇒ 抽成函数，各处自己调（主 describe 与 confirm 门禁都调它）。
 */
function stubApis() {
  api.get.mockReset();
  api.getPlatform.mockReset();
  api.replace.mockReset();
  api.push.mockReset();
  api.get.mockResolvedValue(baseData());
  api.getPlatform.mockImplementation(async (id: number) => ({
    id,
    configRevision: 2,
    publishGroup: false,
    enabled: true,
    effectiveVersion: "2016"
  }));
  api.replace.mockImplementation(async (_id: number, request: any) => ({
    ...baseData(),
    revision: request.expectedProjectionRevision + 1,
    nodes: request.nodes,
    bindings: request.bindings,
    publishGroup: request.publishGroup ?? request.resourceChanges?.publishGroup ?? false,
    configRevision: (request.expectedConfigRevision ?? 2) + 1
  }));
  deviceApi.getDevice.mockReset();
  deviceApi.listChannels.mockReset();
  deviceApi.getDevice.mockResolvedValue({ code: 0, message: "", data: { id: 3, deviceId: "34020000002000000003" } });
  deviceApi.listChannels.mockResolvedValue({ code: 0, message: "", data: { list: [], total: 0, page: 1, pageSize: 200 } });
  // 弹窗内容挂在 `document.body` 上（Teleport，见 `dialogEl`）⇒ 用例之间必须清干净，
  // 否则上一条用例残留的弹窗会被后一条的 `dialogEl` 查到（"元素明明是新的却断言通过"）。
  document.body.innerHTML = "";
  // function 式 confirm 被拦下来：既不真的渲染、又把 config 留下来给用例断言
  confirmConfigs = [];
  confirmSpy = vi.spyOn(Modal, "confirm").mockImplementation(((config: any) => {
    confirmConfigs.push(config);
    return { close: vi.fn(), update: vi.fn() };
  }) as any);
}

afterEach(() => {
  confirmSpy?.mockRestore();
});

describe("cascade business-group manager", () => {
  beforeEach(stubApis);

  it("separates channel sharing from directory sharing with accessible tabs", async () => {
    const wrapper = mountManager();
    await flushPromises();
    expect(wrapper.findComponent({ name: "Table" }).exists()).toBe(true);
    expect(wrapper.findComponent({ name: "Tree" }).exists()).toBe(true);
    expect(wrapper.find(".business-group-channel-header").exists()).toBe(false);
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("channel");
    expect(wrapper.get(".business-group-tree-panel").classes()).toContain("is-mode-hidden");
    expect(wrapper.find("[aria-label='批量移动目标']").exists()).toBe(false);
    expect(wrapper.find("[data-action='move-selected']").exists()).toBe(false);
    expect(wrapper.get("[data-action='add-channels']").isVisible()).toBe(true);
    expect(wrapper.findComponent({ name: "Tabs" }).classes()).toContain("uvp-system-tabs");
    await wrapper.get('[data-action="directory-tab"]').trigger("click");
    await flushPromises();
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("directory");
    expect(wrapper.get(".business-group-tree-panel").classes()).not.toContain("is-mode-hidden");
    // 节点操作 2026-10-14 改挂到**树节点自己身上**（图标按钮），不再有"虚拟组织"这类文字。
    expect(wrapper.get("[data-action='add-organization']").isVisible()).toBe(true);
    expect(wrapper.get("[data-action='add-organization']").attributes("aria-label")).toContain("新建下级组织");
    expect(api.replace).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps directory drafts when switching back to channel sharing and disables tabs during channel picking", async () => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    // 新建节点改走弹窗：填名称 → 确定
    await wrapper.get("[data-action='add-organization']").trigger("click");
    await dialogEl("[aria-label='分组名称']").setValue("测试目录");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    expect(wrapper.get("[data-testid='resource-table']").text()).toContain("已共享通道");
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    expect(wrapper.get(".business-group-tree").text()).toContain("测试目录");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    expect(wrapper.findAllComponents({ name: "TabPane" })).toHaveLength(2);
    expect(wrapper.findAllComponents({ name: "TabPane" }).every(pane => pane.props("disabled"))).toBe(true);
    await wrapper.get('[data-action="channel-tab"]').trigger("click");
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("directory");
    await wrapper.get(`[data-code='${root.code}']`).trigger("contextmenu");
    expect(wrapper.find("[role='menu']").exists()).toBe(false);
    expect(wrapper.get("[data-action='add-organization']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("allows changing a new root code before saving", async () => {
    api.get.mockResolvedValue({ ...baseData(), nodes: [] });
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.get("[data-action='create-root']").trigger("click");
    const input = dialogEl("[aria-label='国标 ID']");
    // 新建时 ID 是**可改**的：预填一个自动生成的合规编号，但允许按上级平台的号段约定改（2026-10-14 老板拍板）
    expect(input.attributes("readonly")).toBeUndefined();
    await input.setValue("34020000002150000009");
    await dialogEl("[aria-label='分组名称']").setValue("业务根 A");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-code='34020000002150000009']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("opens as an inline resource workspace and defaults to all shared channels", async () => {
    const data = baseData();
    data.nodes.push({ ...root, code: "34020000002160000002", name: "运营", kind: "organization", parentCode: root.code });
    data.bindings = [{ sourceChannelId: 31, nodeCode: data.nodes[1].code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    // ⛔ 别写 `wrapper.find("a-modal")` —— 真 Arco 渲染出的是 div，没有 `a-modal` 这个标签，
    //    那种断言**恒真**（永远测不到"弹窗意外打开了"）。弹窗内容一律从 `document.body` 上查。
    expect(document.body.querySelector("[data-testid='node-name-input']")).toBeNull();
    // 通道模式没有"选范围"这回事了（2026-10-14 删掉「全部共享 / 未分组」两个入口），
    // 它恒定展示全部已共享通道，所以判据改成「左树整块被隐藏」。
    expect(wrapper.get(".business-group-tree-panel").classes()).toContain("is-mode-hidden");
    expect(wrapper.get("[data-testid='resource-table']").text()).toContain("已共享通道");
    expect(wrapper.findAll(".channel-row-31")).toHaveLength(1);
    // 通道模式也有勾选列与批量移除（2026-10-14），且批量按钮**落在筛选区**里。
    // 2026-10-14 二次收紧：`.business-group-resource-toolbar` 这个类**已经从组件里删掉**了，
    // 再断它的存在性等于恒真 ⇒ 改成断**目录模式那几颗控件在通道模式下确实不在**。
    expect(wrapper.find("[data-action='move-out-selected']").exists()).toBe(false);
    // ⛔ 别写 `get(…).exists()` —— `get()` 拿不到元素就直接抛，`.exists()` 恒 true（既没信息量，
    //    类型上也过不去：`get()` 的返回类型故意 Omit 掉了 `exists`）。断它的模式文案更实在。
    expect(wrapper.get(".uvp-search-panel__extra [data-action='request-unshare-selected']").text()).toContain("批量移除");
    expect(wrapper.find(".business-group-panel-heading").exists()).toBe(false);
    expect(wrapper.find(".channel-row-31 input[type='checkbox']").exists()).toBe(true);
    expect(wrapper.get("[data-action='save']").text()).toContain("保存修改");
    wrapper.unmount();
  });

  it("selects rows in channel mode and batch-removes the selected shares", async () => {
    const data = baseData();
    data.channels.push(
      { ...data.channels[0], sourceChannelId: 32, publishedChannelId: "34020000001320000032", name: "第二路" },
      { ...data.channels[0], sourceChannelId: 33, publishedChannelId: "34020000001320000033", name: "第三路" }
    );
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    // 表头 = 本页全选（用内层原生 input 断言勾选态；半选态看 label 上的 class）
    const headerCheckbox = () => wrapper.get("[data-action='select-page'] input").element as HTMLInputElement;
    const headerLabel = () => wrapper.get("[data-action='select-page']");
    expect(headerCheckbox().checked).toBe(false);
    expect(headerLabel().classes()).not.toContain("arco-checkbox-indeterminate");
    await wrapper.get(".channel-row-31 input[type='checkbox']").setValue(true);
    await wrapper.get(".channel-row-32 input[type='checkbox']").setValue(true);
    // 部分选中 ⇒ 表头呈半选态（Arco 的 indeterminate 是 label 上的 class，不是 input 的 DOM 属性）
    expect(headerCheckbox().checked).toBe(false);
    expect(headerLabel().classes()).toContain("arco-checkbox-indeterminate");

    // 通道模式的批量动作叫「批量移除」，并带上已选数量
    const batch = wrapper.get("[data-action='request-unshare-selected']");
    expect(batch.text()).toContain("批量移除");
    expect(batch.text()).toContain("2");
    expect(batch.attributes("disabled")).toBeUndefined();
    await batch.trigger("click");
    // 走 confirm 弹窗二次确认，确认后才进草稿（2026-10-15 起页面里不再有内嵌确认条）
    expect(lastConfirm().content).toContain("停止向该上级共享 2 路");
    await acceptConfirm();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.find(".channel-row-31").exists()).toBe(false);
    expect(wrapper.find(".channel-row-32").exists()).toBe(false);
    expect(wrapper.find(".channel-row-33").exists()).toBe(true);
    wrapper.unmount();
  });

  it("lets the header checkbox select and clear the whole page in channel mode", async () => {
    const data = baseData();
    data.channels.push({ ...data.channels[0], sourceChannelId: 32, publishedChannelId: "34020000001320000032" });
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    const header = () => wrapper.get("[data-action='select-page'] input").element as HTMLInputElement;
    await wrapper.get("[data-action='select-page'] input").setValue(true);
    expect(header().checked).toBe(true);
    expect(wrapper.get("[data-action='select-page']").classes()).not.toContain("arco-checkbox-indeterminate");
    expect(wrapper.get("[data-action='request-unshare-selected']").text()).toContain("2");

    await wrapper.get("[data-action='select-page'] input").setValue(false);
    expect(header().checked).toBe(false);
    expect(wrapper.get("[data-action='request-unshare-selected']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("shows directory batch actions directly and only offers adding inside an organization", async () => {
    const data = baseData();
    data.nodes.push({ ...root, code: "34020000002160000009", name: "运营", kind: "organization", parentCode: root.code });
    data.bindings = [{ sourceChannelId: 31, nodeCode: root.code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.get(".uvp-search-panel__extra [data-action='request-unshare-selected']").text()).toContain("批量移除");
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    const search = wrapper.get(".uvp-search-panel");
    expect(search.findAll("button").map(button => button.text())).toEqual(["移动到组织", "移出组织"]);
    expect(search.find("[data-action='batch-actions']").exists()).toBe(false);
    for (const action of ["move-selected", "move-out-selected"]) {
      expect(search.get(`[data-action='${action}']`).attributes("disabled")).toBeDefined();
    }
    expect(search.find("[data-action='add-channels']").exists()).toBe(false);
    expect(search.find("[aria-label='状态筛选']").exists()).toBe(true);
    expect(search.find("[aria-label='批量移动目标']").exists()).toBe(false);
    await wrapper.get(".channel-row-31 input[type='checkbox']").setValue(true);
    expect(search.get("[data-action='move-selected']").attributes("disabled")).toBeUndefined();
    await search.get("[data-action='move-selected']").trigger("click");
    expect(dialogEl("[data-action='confirm-move-dialog']").attributes("disabled")).toBeDefined();
    await dialogEl("[aria-label='移动目标组织']").setValue(data.nodes[1].code);
    await dialogEl("[data-action='cancel-move-dialog']").trigger("click");
    expect(wrapper.find(".channel-row-31").exists()).toBe(true);
    expect(wrapper.get(".channel-row-31 input[type='checkbox']").element).toHaveProperty("checked", true);
    expect(search.get("[data-action='move-out-selected']").attributes("disabled")).toBeUndefined();
    expect(search.find("[data-action='request-unshare-selected']").exists()).toBe(false);

    await wrapper.get(`[data-code='${data.nodes[1].code}']`).trigger("click");
    expect(search.get("[data-action='add-channels']").text()).toBe("添加通道");
    expect(search.findComponent({ name: "SLayoutTools" }).exists()).toBe(false);
    wrapper.unmount();
  });

  it("allows sharing without a business root and submits resource changes with the directory", async () => {
    api.get.mockResolvedValue({ platformId: 7, revision: 4, nodes: [], bindings: [], channels: [] });
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.find("[data-action='create-root']").exists()).toBe(true);
    expect(wrapper.find("[data-action='add-channels']").exists()).toBe(true);
    await wrapper.get("[data-action='add-channels']").trigger("click");
    const picker = wrapper.findComponent({ name: "BusinessChannelPicker" });
    expect(picker.props("targetCode")).toBe("");
    // ⛔ 组件里现在有**两颗**真 Arco Modal（业务分组编辑弹窗 + 共享通道选择弹窗），
    //   `findComponent({ name: "Modal" })` 只会拿到第一颗（业务分组那颗，此刻是关的）
    //   ⇒ 得按 `modalClass` 认出选择器那颗，否则断言会莫名其妙地恒为 false。
    // ⛔⛔ 判据必须用**这颗自己的类** `business-share-channel-dialog`，不能用 `uvp-system-dialog`：
    //   2026-10-15 给业务分组弹窗补上房规类之后，两颗都含 `uvp-system-dialog`，
    //   那种写法会命中**第一颗**（关着的那颗）⇒ 断言恒为 false。
    const pickerModal = wrapper
      .findAllComponents({ name: "Modal" })
      .find(component => String(component.props("modalClass") ?? "").includes("business-share-channel-dialog"));
    expect(pickerModal?.props("visible")).toBe(true);
    expect(pickerModal?.props("modalClass")).toContain("uvp-system-dialog");
    const candidate = { ...baseData().channels[0], sourceChannelId: 90, sourceDeviceId: 0, name: "新通道" };
    picker.vm.$emit("apply", [candidate]);
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(api.replace).toHaveBeenCalledWith(
      7,
      expect.objectContaining({
        nodes: [],
        bindings: [],
        expectedProjectionRevision: 4,
        resourceChanges: {
          shareChannelIds: [90],
          unshareChannelIds: [],
          channelPermissions: []
        }
      })
    );
    expect(api.replace.mock.calls[0][1]).not.toHaveProperty("shareChannelIds");
    expect(wrapper.emitted("saved")).toHaveLength(1);
    wrapper.unmount();
  });

  it("requires a confirm dialog before drafting unshare", async () => {
    const data = baseData();
    data.channels[0].ptzAllowed = true;
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    const row = wrapper.get(".channel-row-31");
    await row.get("[data-action='request-unshare']").trigger("click");
    expect(lastConfirm().content).toContain("停止向该上级共享 1 路");
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    await acceptConfirm();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.find(".channel-row-31").exists()).toBe(false);
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(api.replace).toHaveBeenCalledWith(
      7,
      expect.objectContaining({
        resourceChanges: { shareChannelIds: [], unshareChannelIds: [31], channelPermissions: [] }
      })
    );
    wrapper.unmount();
  });

  /**
   * 云台控制 2026-10-14 移除：通道表不再有逐通道云台开关（改走统一控制入口）。
   * 反向门禁 —— 谁要是把它加回来，用例先炸。
   */
  it("no longer renders a per-channel PTZ switch and shows the online status column instead", async () => {
    deviceApi.getDevice.mockResolvedValue({ code: 0, message: "", data: { id: 3, deviceId: "34020000002000000003" } });
    deviceApi.listChannels.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        list: [{ id: 31, channelId: "34020000001320000031", deviceId: "34020000002000000003", status: 1 }],
        total: 1,
        page: 1,
        pageSize: 200
      }
    });
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.find("[data-action='channel-ptz']").exists()).toBe(false);
    expect(wrapper.get(".channel-row-31").text()).toContain("在线");
    expect(deviceApi.listChannels).toHaveBeenCalledWith({ deviceId: "34020000002000000003", page: 1, pageSize: 200 });
    wrapper.unmount();
  });

  /**
   * 「添加共享」选进来、**还没保存**的草稿行：`sourceDeviceId` 恒为 0，
   * 设备编号只在 `sourceDeviceCode` 上（见 BusinessChannelPicker.toProjection）。
   * ⛔⛔ 反向门禁 —— 以前的状态回查只认设备主键，这类行的在线状态会**永远显示「未知」、
   * 而且一个请求都不发**（2026-10-14 真机复现：3 行草稿全是「未知」、零请求）。
   */
  it("resolves online status for an unsaved draft through its device code", async () => {
    api.get.mockResolvedValue({
      ...baseData(),
      channels: [
        {
          sourceChannelId: 5233,
          sourceDeviceId: 0,
          sourceDeviceCode: "37010301022000000007",
          publishedChannelId: "34020000001320000020",
          name: "前置摄像头",
          parentOverride: "",
          ptzAllowed: false,
          active: true
        }
      ]
    });
    deviceApi.listChannels.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        list: [{ id: 5233, channelId: "34020000001320000020", deviceId: "37010301022000000007", status: 1 }],
        total: 1,
        page: 1,
        pageSize: 200
      }
    });
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.get(".channel-row-5233").text()).toContain("在线");
    expect(deviceApi.listChannels).toHaveBeenCalledWith({
      deviceId: "37010301022000000007",
      page: 1,
      pageSize: 200
    });
    // ⛔ 草稿行没有设备主键，不许再按主键回查（否则白打一次、还可能查错设备）
    expect(deviceApi.getDevice).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("filters shared channels by online, offline, and unknown status", async () => {
    const data = baseData();
    data.channels.push(
      { ...data.channels[0], sourceChannelId: 32, publishedChannelId: "34020000001320000032", name: "离线通道" },
      { ...data.channels[0], sourceChannelId: 33, publishedChannelId: "34020000001320000033", name: "未知通道" }
    );
    api.get.mockResolvedValue(data);
    deviceApi.listChannels.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        list: [
          { id: 31, channelId: "34020000001320000031", deviceId: "34020000002000000003", status: 1 },
          { id: 32, channelId: "34020000001320000032", deviceId: "34020000002000000003", status: 0 }
        ],
        total: 2,
        page: 1,
        pageSize: 200
      }
    });
    const wrapper = mountManager();
    await flushPromises();

    const statusFilter = wrapper.get("[aria-label='状态筛选']");
    expect(statusFilter.findAll("option").map(option => option.text())).toEqual(["全部状态", "在线", "离线", "未知"]);
    expect(wrapper.findAll(".business-group-channel-table tbody .arco-table-tr")).toHaveLength(3);

    await statusFilter.setValue("online");
    expect(wrapper.find(".channel-row-31").exists()).toBe(true);
    expect(wrapper.find(".channel-row-32").exists()).toBe(false);
    expect(wrapper.find(".channel-row-33").exists()).toBe(false);

    await statusFilter.setValue("offline");
    expect(wrapper.find(".channel-row-31").exists()).toBe(false);
    expect(wrapper.find(".channel-row-32").exists()).toBe(true);
    expect(wrapper.find(".channel-row-33").exists()).toBe(false);

    await statusFilter.setValue("unknown");
    expect(wrapper.find(".channel-row-31").exists()).toBe(false);
    expect(wrapper.find(".channel-row-32").exists()).toBe(false);
    expect(wrapper.find(".channel-row-33").exists()).toBe(true);
    wrapper.unmount();
  });

  it("adds an unshared channel as a draft and saves its ID with the tree in one request", async () => {
    const data = baseData();
    data.channels = [];
    data.nodes = [root, { ...root, code: "34020000002160000002", name: "运营", kind: "organization", parentCode: root.code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${data.nodes[1].code}']`).trigger("click");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    const picker = wrapper.findComponent({ name: "BusinessChannelPicker" });
    expect(picker.exists()).toBe(true);
    picker.vm.$emit("apply", [{ ...baseData().channels[0], sourceChannelId: 90, sourceDeviceId: 0, name: "新通道" }]);
    await flushPromises();
    expect(api.replace).not.toHaveBeenCalled();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.text()).toContain("待共享");
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenCalledTimes(1);
    expect(api.replace).toHaveBeenCalledWith(
      7,
      expect.objectContaining({
        expectedProjectionRevision: 4,
        resourceChanges: {
          shareChannelIds: [90],
          unshareChannelIds: [],
          channelPermissions: []
        },
        bindings: [{ sourceChannelId: 90, nodeCode: data.nodes[1].code }]
      })
    );
    expect(api.replace.mock.calls[0][1]).not.toHaveProperty("shareChannelIds");
    wrapper.unmount();
  });

  it("cancels pending sharing when a new draft channel is removed and retains it on save failure", async () => {
    const data = baseData();
    data.channels = [];
    data.nodes = [root, { ...root, code: "34020000002160000002", name: "运营", kind: "organization", parentCode: root.code }];
    api.get.mockResolvedValue(data);
    api.replace.mockRejectedValue(new Error("保存失败"));
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${data.nodes[1].code}']`).trigger("click");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    wrapper.findComponent({ name: "BusinessChannelPicker" }).vm.$emit("apply", [baseData().channels[0]]);
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("当前草稿已保留");
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    await wrapper.get("[data-action='remove-binding']").trigger("click");
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.findAll("[data-action='remove-binding']")).toHaveLength(0);
    wrapper.unmount();
  });

  it("uses the committed shared snapshot for subsequent moves without sharing again", async () => {
    const data = baseData();
    data.channels = [];
    const first = { ...root, code: "34020000002160000002", name: "一组", kind: "organization" as const, parentCode: root.code };
    const second = { ...first, code: "34020000002160000003", name: "二组" };
    data.nodes = [root, first, second];
    api.get.mockResolvedValue(data);
    const published = { ...baseData().channels[0], publishedChannelId: "34020000001320000999", ptzAllowed: true };
    api.replace.mockImplementation(async (_id, request) => ({
      ...data,
      ...request,
      revision: request.expectedProjectionRevision + 1,
      channels: [published]
    }));
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${first.code}']`).trigger("click");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    wrapper.findComponent({ name: "BusinessChannelPicker" }).vm.$emit("apply", [baseData().channels[0]]);
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.get(".channel-row-31").text()).not.toContain("待共享");
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${second.code}']`).trigger("click");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    const picker = wrapper.findComponent({ name: "BusinessChannelPicker" });
    expect(picker.props("channels")).toEqual([published]);
    picker.vm.$emit("apply", [published]);
    await flushPromises();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenLastCalledWith(
      7,
      expect.objectContaining({
        expectedProjectionRevision: 5,
        resourceChanges: { shareChannelIds: [], unshareChannelIds: [], channelPermissions: [] }
      })
    );
    wrapper.unmount();
  });

  it("keeps navigation free of duplicate actions and separates catalog pushing from saving", async () => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("click");
    // 节点操作按钮全部长在**树节点自己身上**（2026-10-14），左侧不再另起一条工具条；
    // 右侧详情区一颗都不许有（那是上一版的排布）。
    expect(wrapper.findAll("[data-action='add-organization']")).toHaveLength(1); // 只有业务根那一行
    expect(wrapper.find(".business-group-tree-panel [data-action='add-organization']").exists()).toBe(true);
    expect(wrapper.find(".business-group-detail [data-action='add-organization']").exists()).toBe(false);
    expect(wrapper.find(".business-group-tree-panel [data-action='edit-organization']").exists()).toBe(true);
    expect(wrapper.find(".business-group-tree-panel [data-action='delete-root']").exists()).toBe(true);
    expect(wrapper.find(".business-group-tree-actions").exists()).toBe(false);
    expect(wrapper.find(".business-group-footer").exists()).toBe(false);
    expect(wrapper.find(".business-group-overview").exists()).toBe(false);
    expect(wrapper.find("[data-action='close-manager']").exists()).toBe(true);
    expect(wrapper.find("[aria-label='共享方式操作'] [data-action='save']").exists()).toBe(true);
    expect(wrapper.find("[aria-label='共享方式操作'] [data-action='discard-changes']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.find("[aria-label='共享方式操作'] [data-action='push-catalog']").exists()).toBe(true);
    expect(wrapper.findAll("[data-action='manage-shares']")).toHaveLength(0);
    wrapper.unmount();
  });

  it("creates a child under the right-clicked organization and closes its menu", async () => {
    const organization = {
      ...root,
      code: "34020000002160000002",
      kind: "organization" as const,
      parentCode: root.code,
      name: "运营"
    };
    api.get.mockResolvedValue({ ...baseData(), nodes: [root, organization] });
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("click");
    await wrapper.get(`[data-code='${organization.code}']`).trigger("contextmenu");
    expect(wrapper.findComponent({ name: "Tree" }).props("selectedKeys")).toEqual([organization.code]);
    expect(wrapper.get("[data-action='context-add-organization']").text()).toContain("下级组织");
    await wrapper.get("[data-action='context-add-organization']").trigger("click");
    // 弹窗里的「上级组织」默认就是刚右键的那个节点
    expect((dialogEl("[aria-label='上级组织']").element as HTMLSelectElement).value).toBe(organization.code);
    expect(wrapper.find("[role='menu']").exists()).toBe(false);
    expect(api.replace).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("keeps context deletion disabled for populated nodes and confirms deletion of empty nodes", async () => {
    const organization = {
      ...root,
      code: "34020000002160000002",
      kind: "organization" as const,
      parentCode: root.code,
      name: "运营"
    };
    api.get.mockResolvedValue({ ...baseData(), nodes: [root, organization] });
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("contextmenu");
    expect(wrapper.get("[data-action='context-delete-node']").attributes("disabled")).toBeDefined();
    await wrapper.get(`[data-code='${organization.code}']`).trigger("keydown", { key: "F10", shiftKey: true });
    await wrapper.get("[data-action='context-delete-node']").trigger("click");
    expect(wrapper.find(`[data-code='${organization.code}']`).exists()).toBe(true);
    expect(lastConfirm().content).toContain("运营");
    expect(lastConfirm().okButtonProps).toEqual({ status: "danger" });
    await acceptConfirm();
    expect(wrapper.find(`[data-code='${organization.code}']`).exists()).toBe(false);
    wrapper.unmount();
  });

  it("creates a child from the in-tree button with the clicked node prefilled as its parent", async () => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("click");
    await wrapper.get("[data-action='add-organization']").trigger("click");
    expect((dialogEl("[aria-label='上级组织']").element as HTMLSelectElement).value).toBe(root.code);
    wrapper.unmount();
    // 同一个用例里 mount 两次 ⇒ 两套弹窗都会 Teleport 到 body，`dialogEl` 只会拿到第一份。
    // 卸载后主动清一次（Vue 正常会自己摘掉，这里是防"残留导致读到上一个实例的弹窗"）。
    document.body.innerHTML = "";

    const readonly = mountManager({ canWrite: false });
    await flushPromises();
    readonly.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await readonly.get(`[data-code='${root.code}']`).trigger("contextmenu");
    expect(readonly.get("[data-action='context-add-organization']").attributes("disabled")).toBeDefined();
    expect(readonly.get("[data-action='context-delete-node']").attributes("disabled")).toBeDefined();
    await readonly.get("[data-action='context-edit-organization']").trigger("click");
    expect(dialogEl("[aria-label='分组名称']").attributes("disabled")).toBeDefined();
    readonly.unmount();
  });

  it("shows an explicit empty state and generates a 20-digit root only after the user asks", async () => {
    api.get.mockResolvedValue({ platformId: 7, revision: 0, nodes: [], bindings: [], channels: [] });
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.find("[data-action='add-channels']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    // 空态：左侧给一句引导 + 一颗"新建业务分组"（不再有树上方那排小工具按钮）
    expect(wrapper.findAll("[data-action='create-root']")).toHaveLength(1);
    expect(wrapper.get(".business-group-tree-empty").text()).toContain("还没有业务分组");
    await wrapper.get("[data-action='create-root']").trigger("click");
    // 弹窗里 ID 已按国标预填（20 位、第 11–13 位 215），但**允许改**
    const code = (dialogEl("[aria-label='国标 ID']").element as HTMLInputElement).value;
    expect(code).toMatch(/^\d{20}$/);
    expect(code.slice(10, 13)).toBe("215");
    await dialogEl("[aria-label='分组名称']").setValue("业务根 A");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    expect(wrapper.find(".business-group-tree-empty").exists()).toBe(false);
    expect(wrapper.find(`[data-code='${code}']`).exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps the empty-state shortcut disabled for view-only accounts", async () => {
    api.get.mockResolvedValue({ platformId: 7, revision: 0, nodes: [], bindings: [], channels: [] });
    const wrapper = mountManager({ canWrite: false });
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    expect(wrapper.get(".business-group-tree-empty [data-action='create-root']").attributes("disabled")).toBeDefined();
    expect(wrapper.get(".business-group-tree-empty").text()).toContain("当前账号只有查看权限");
    await wrapper.get("[data-action='create-root']").trigger("click");
    // 没有写权限时点它**不该弹出任何东西**（守卫在 `openCreateRootDialog` 里，不只是把按钮置灰）
    expect(document.body.querySelector("[data-testid='node-name-input']")).toBeNull();
    wrapper.unmount();
  });

  it("validates the root dialog live and keeps submit disabled until the code is legal", async () => {
    api.get.mockResolvedValue({ platformId: 7, revision: 0, nodes: [], bindings: [], channels: [] });
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get("[data-action='create-root']").trigger("click");

    // 校验文案在弹窗里（Teleport 到 body，所以走 `dialogEl` / 直接查 body）
    const dialogError = () => document.body.querySelector(".business-group-node-form__error")?.textContent?.trim() ?? "";
    const submit = () => dialogEl("[data-action='confirm-node-dialog']");
    const codeInput = () => dialogEl("[aria-label='国标 ID']");
    const nameInput = () => dialogEl("[aria-label='分组名称']");

    // ① 名称不能空
    await nameInput().setValue("");
    expect(dialogError()).toContain("分组名称不能为空");
    expect(submit().attributes("disabled")).toBeDefined();

    // ② 编号必须 20 位
    await nameInput().setValue("业务根 A");
    await codeInput().setValue("3402000000215000000"); // 19 位
    expect(dialogError()).toContain("20 位数字");

    // ③ 业务根的类型码必须是 215（填 216 要拦下来）—— 这是最容易写漏、且漏了就会污染上级目录的一条
    await codeInput().setValue("34020000002160000001");
    expect(dialogError()).toContain("215");

    // ④ 改回合法编号 ⇒ 报错消失、提交可用
    await codeInput().setValue("34020000002150000009");
    expect(dialogError()).toBe("");
    expect(submit().attributes("disabled")).toBeUndefined();
    await submit().trigger("click");
    await flushPromises();
    expect(wrapper.find("[data-code='34020000002150000009']").exists()).toBe(true);
    wrapper.unmount();
  });

  it("refuses to reuse an existing node code inside the dialog", async () => {
    const organization = {
      code: "34020000002160000001",
      name: "运营",
      kind: "organization" as const,
      parentCode: root.code,
      sort: 0
    };
    api.get.mockResolvedValue({ ...baseData(), nodes: [root, organization] });
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    // 在业务根下新建虚拟组织，把编号改成「运营」已经在用的那个
    await wrapper.get(`[data-code='${root.code}'] [data-action='add-organization']`).trigger("click");
    expect((dialogEl("[aria-label='上级组织']").element as HTMLSelectElement).value).toBe(root.code);
    await dialogEl("[aria-label='国标 ID']").setValue(organization.code);
    expect(document.body.querySelector(".business-group-node-form__error")?.textContent).toContain("已被其它节点占用");
    expect(dialogEl("[data-action='confirm-node-dialog']").attributes("disabled")).toBeDefined();
    // 提交按钮被禁 = 点不动；再确认一次确实没有把那颗节点顶掉
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    expect(wrapper.findAll(`[data-code='${organization.code}']`)).toHaveLength(1);
    wrapper.unmount();
  });

  it("blocks writes in view-only mode while still showing the current platform tree", async () => {
    const wrapper = mountManager({ canWrite: false });
    await flushPromises();
    expect(wrapper.get("[data-testid='resource-table']").text()).toContain("已共享通道");
    expect(wrapper.get("[data-action='add-channels']").attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-action='save']").attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("moves a shared channel to one node and keeps the shared resource after removing the binding", async () => {
    const data = baseData();
    data.nodes = [
      root,
      { ...root, code: "34020000002160000002", name: "运营一组", kind: "organization", parentCode: root.code, sort: 0 }
    ];
    data.bindings = [{ sourceChannelId: 31, nodeCode: data.nodes[1].code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${data.nodes[1].code}']`).trigger("click");
    expect(wrapper.text()).toContain("已共享通道");
    expect(wrapper.text()).toContain("添加通道");
    expect(wrapper.findAll("[data-action='add-channels']")).toHaveLength(1);
    await wrapper.get(".channel-row-31 input[type='checkbox']").setValue(true);
    await wrapper.get("[data-action='move-out-selected']").trigger("click");
    // ⛔ 「未分组」入口 2026-10-14 已删除 —— 改从草稿摘要确认这一路确实被移出了组织，
    //   而共享资源本身仍在（表格还是那份已共享通道清单）。
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    // 移出之后当前范围（「运营一组」）自然就空了 —— 要证「共享还在」得切回**通道模式**：
    // 通道模式恒定列出全部已共享通道（不看归属），31 还在那儿 ⇒ 只摘了业务归属、没取消共享。
    // （原文案靠已删除的「未分组」入口看游离通道，2026-10-14 该入口已去掉。）
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    expect(wrapper.get("[data-testid='resource-table']").text()).toContain("已共享通道");
    expect(wrapper.findAll(".channel-row-31")).toHaveLength(1);
    wrapper.unmount();
  });

  it("renders the parent picker as an expandable hierarchy and creates the node on the chosen level", async () => {
    const data = baseData();
    const first = {
      ...root,
      code: "34020000002160000002",
      name: "运营一组",
      kind: "organization" as const,
      parentCode: root.code,
      sort: 0
    };
    const second = { ...first, code: "34020000002160000003", name: "前端点位", parentCode: first.code, sort: 0 };
    data.nodes = [root, first, second];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${first.code}']`).trigger("click");
    await wrapper.get(`[data-code='${first.code}'] [data-action='add-organization']`).trigger("click");

    // ⛔ 行内按钮每行都有一颗 ⇒ 必须先锁到那一行（`[data-code=…]` 是行容器），
    //   否则 `get("[data-action='add-organization']")` 拿到的是业务根那一颗，预填的上级就不是我点的节点。
    const select = dialogEl("[aria-label='上级组织']");
    // ① 分层级（老板 2026-10-15 的要求）：业务根 → 一级组织 → 二级组织，靠缩进体现父子关系。
    //    ⛔ 断言里**必须**带层数（`data-depth`）—— 只断"三个选项都在"，扁平数组也能过，等于没测。
    expect(select.findAll("option").map(option => [option.element.value, option.attributes("data-depth")])).toEqual([
      [root.code, "0"],
      [first.code, "1"],
      [second.code, "2"]
    ]);
    // ② 默认全展开 + 可搜索：组织一多，靠点箭头一层层找是不现实的
    expect(select.attributes("data-expand-all")).toBe("true");
    expect(select.attributes("data-allow-search")).toBe("true");
    // ③ 选最深的那一支 ⇒ 新组织必须挂在它下面（不是悄悄落回根上）
    expect((select.element as HTMLSelectElement).value).toBe(first.code);
    await select.setValue(second.code);
    const createdCode = (dialogEl("[aria-label='国标 ID']").element as HTMLInputElement).value;
    await dialogEl("[aria-label='分组名称']").setValue("球机");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();

    type TreeNode = { key: string; children?: TreeNode[] };
    const treeData = wrapper.findComponent({ name: "Tree" }).props("data") as TreeNode[];
    const nested = treeData[0].children?.find(node => node.key === first.code)?.children?.find(node => node.key === second.code);
    expect(nested?.children?.map(node => node.key)).toContain(createdCode);
    expect(treeData[0].children?.map(node => node.key)).not.toContain(createdCode);
    wrapper.unmount();
  });

  it("rejects deleting a populated node and excludes descendants from parent choices", async () => {
    const data = baseData();
    const organization = {
      ...root,
      code: "34020000002160000002",
      name: "运营一组",
      kind: "organization" as const,
      parentCode: root.code,
      sort: 0
    };
    data.nodes = [
      root,
      organization,
      { ...organization, code: "34020000002160000003", name: "摄像机组", parentCode: organization.code }
    ];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${organization.code}']`).trigger("click");
    // ⛔ 行内按钮每行都有 ⇒ 选择器必须**先锁定到那一行**（`[data-code=…]` 是行容器），
    //   否则 `get("[data-action='delete-node']")` 会拿到别的节点那一颗。
    expect(wrapper.get(`[data-code='${organization.code}'] [data-action='delete-node']`).attributes("disabled")).toBeDefined();
    await wrapper.get(`[data-code='${organization.code}'] [data-action='edit-organization']`).trigger("click");
    const parentOptions = dialogEl("[aria-label='上级组织']")
      .findAll("option")
      .map(option => option.element.value);
    expect(parentOptions).not.toContain(organization.code);
    expect(parentOptions).not.toContain("34020000002160000003");
    wrapper.unmount();
  });

  it("keeps edits on revision conflict and requires confirmation before reloading", async () => {
    api.get.mockResolvedValue({ ...baseData(), channels: [] });
    api.replace.mockRejectedValue(new Error("资源状态冲突"));
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("click");
    await wrapper.get(`[data-code='${root.code}'] [data-action='edit-organization']`).trigger("click");
    await dialogEl("[aria-label='分组名称']").setValue("本地草稿");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("数据已被其他操作修改");
    // 编辑改走弹窗后，草稿的落点是**树节点名**（右侧那排输入框已经没了）
    expect(wrapper.get(".business-group-tree").text()).toContain("本地草稿");
    expect(wrapper.get("[data-action='save']").attributes("disabled")).toBeDefined();
    await wrapper.get("[data-action='reload-latest']").trigger("click");
    expect(lastConfirm().content).toContain("重新读取将放弃当前草稿");
    await dismissConfirm();
    expect(wrapper.get(".business-group-tree").text()).toContain("本地草稿");
    await wrapper.get("[data-action='reload-latest']").trigger("click");
    await acceptConfirm();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    expect(wrapper.get(".business-group-tree").text()).toContain("业务根");
    wrapper.unmount();
  });

  it("does not let a late response for one platform replace the currently selected platform", async () => {
    let resolveA!: (value: unknown) => void;
    api.get.mockImplementation((id: number) =>
      id === 7
        ? new Promise(resolve => (resolveA = resolve))
        : Promise.resolve({ ...baseData(8), nodes: [{ ...root, code: "34020000002150000008", name: "平台 B 根" }] })
    );
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.setProps({ platformId: 8, platformName: "上级 B", localDeviceId: "34020000002000000008" });
    await flushPromises();
    resolveA(baseData(7));
    await flushPromises();

    expect(wrapper.text()).toContain("平台 B 根");
    await wrapper.get("[data-code='34020000002150000008']").trigger("click");
    await wrapper.get("[data-code='34020000002150000008'] [data-action='edit-organization']").trigger("click");
    // 编辑已有节点时 ID **只读**（可能已经推给上级平台了），但值要显示出来
    expect((dialogEl("[aria-label='国标 ID']").element as HTMLInputElement).value).toBe("34020000002150000008");
    expect(dialogEl("[aria-label='国标 ID']").attributes("readonly")).toBeDefined();
    wrapper.unmount();
  });

  it("reports load failures with a retry action", async () => {
    api.get.mockRejectedValueOnce(new Error("offline"));
    const wrapper = mountManager();
    await flushPromises();
    expect(wrapper.text()).toContain("业务分组读取失败");
    await wrapper.get("[data-action='retry-load']").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("业务根");
    expect(api.get).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("shows the selected organization path and uses it as the batch-add target", async () => {
    const data = baseData();
    const first = {
      ...root,
      code: "34020000002160000002",
      name: "运营",
      kind: "organization" as const,
      parentCode: root.code,
      sort: 0
    };
    const second = { ...first, code: "34020000002160000003", name: "一组", parentCode: first.code, sort: 0 };
    data.nodes = [root, first, second];
    data.channels = [
      ...data.channels,
      { ...data.channels[0], sourceChannelId: 32, publishedChannelId: "34020000001320000032", name: "未分组通道" },
      { ...data.channels[0], sourceChannelId: 33, publishedChannelId: "34020000001320000033", name: "其他组通道" }
    ];
    data.bindings = [{ sourceChannelId: 33, nodeCode: first.code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${second.code}']`).trigger("click");
    expect(wrapper.get("[data-testid='current-node-path']").text()).toBe("业务根 / 运营 / 一组 · 直属通道");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    const picker = wrapper.findComponent({ name: "BusinessChannelPicker" });
    expect(picker.props("targetPath")).toBe("业务根 / 运营 / 一组");
    expect(picker.props("targetCode")).toBe(second.code);
    expect(wrapper.findAll("[aria-label='批量归属目标']")).toHaveLength(0);
    wrapper.unmount();
  });

  it("distinguishes same-name sibling organizations when moving channels between them", async () => {
    const data = baseData();
    const first = {
      ...root,
      code: "34020000002160000002",
      name: "运营",
      kind: "organization" as const,
      parentCode: root.code,
      sort: 0
    };
    const second = { ...first, code: "34020000002160000003", sort: 1 };
    data.nodes = [root, first, second];
    data.channels = [
      { ...data.channels[0], sourceChannelId: 32, publishedChannelId: "34020000001320000032", name: "同名下通道" }
    ];
    data.bindings = [{ sourceChannelId: 32, nodeCode: second.code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${first.code}']`).trigger("click");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    const picker = wrapper.findComponent({ name: "BusinessChannelPicker" });
    expect(picker.props("targetPath")).toBe(`业务根 / 运营 · ${first.code}`);
    expect(picker.props("targetCode")).toBe(first.code);
    wrapper.unmount();
  });

  /**
   * 「离开时还有未保存修改」= confirm 弹窗（2026-10-15 老板要求，截图里那条页面内黄条已经拆掉）。
   *
   * 取消（含右上角 ×，同样走 onCancel）**必须**只等于"继续编辑"，绝不能顺手把草稿丢掉。
   */
  it("protects a dirty draft on route exit and lets the user continue editing", async () => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("click");
    // 改名改走弹窗（2026-10-14）：点行内编辑 → 在弹窗里改 → 确定落成草稿
    await wrapper.get(`[data-code='${root.code}'] [data-action='edit-organization']`).trigger("click");
    await dialogEl("[aria-label='分组名称']").setValue("本地草稿");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);

    const exit = (wrapper.vm as any).requestExit();
    await flushPromises();
    expect(wrapper.emitted("update:visible")).toBeUndefined();
    const config = lastConfirm();
    expect(config.title).toBe("有未保存修改");
    expect(config.okText).toBe("保存并离开");
    expect(config.cancelText).toBe("继续编辑");
    await dismissConfirm();
    // 继续编辑 ⇒ 留在页面上，草稿原封不动
    expect(await exit).toBe(false);
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);

    wrapper.unmount();
  });

  /**
   * 「添加通道」＝ 弹窗（2026-10-14 老板拍板）：右侧面板**永远**显示该节点下已添加的通道，
   * 选择器只活在弹窗里 —— 原来目录模式是把选择器内嵌在右侧面板里、把通道列表整个顶掉。
   */
  it("opens the add-channel dialog while keeping the node channel table on screen", async () => {
    const data = baseData();
    data.nodes = [
      root,
      {
        ...root,
        code: "34020000002160000002",
        name: "运营",
        kind: "organization",
        parentCode: root.code,
        sort: 0
      }
    ];
    data.channels = Array.from({ length: 25 }, (_, index) => ({
      ...baseData().channels[0],
      sourceChannelId: 100 + index,
      publishedChannelId: `3402000000132000${String(index + 1).padStart(4, "0")}`,
      name: `通道 ${index + 1}`
    }));
    // 给该节点挂一路已保存的归属 ⇒ 右侧表格里必须有它的行（"节点下已添加的通道"）
    data.bindings = [{ sourceChannelId: 100, nodeCode: data.nodes[1].code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${data.nodes[1].code}']`).trigger("click");
    await wrapper.get("[data-action='add-channels']").trigger("click");
    await flushPromises();

    const picker = wrapper.findComponent({ name: "BusinessChannelPicker" });
    expect(picker.props("targetCode")).toBe(data.nodes[1].code);
    // 目标路径必须带出去：弹窗标题放不下完整路径，路径由选择器自己那行「目标组织」显示。
    expect(picker.props("targetPath")).toBe("业务根 / 运营");
    expect(picker.props("channels")).toHaveLength(25);
    // 选择器在弹窗里（真 Arco Modal，挂在 `document.body` 上）——按 `modalClass` 认出它那一颗：
    // ⛔ `findComponent({name:'Modal'})` 只会拿到第一颗（业务分组编辑弹窗），必须用 findAllComponents。
    // ⛔⛔ 判据只认这颗自己的类 `business-share-channel-dialog`：2026-10-15 两颗都补上了
    //   `uvp-system-dialog`，拿它当判据会命中关着的第一颗（断言恒 false，且看起来"功能坏了"）。
    const pickerModal = wrapper
      .findAllComponents({ name: "Modal" })
      .find(component => String(component.props("modalClass") ?? "").includes("business-share-channel-dialog"));
    expect(pickerModal?.props("visible")).toBe(true);
    expect(pickerModal?.props("title")).toBe("添加通道");
    // 关键判据：弹窗开着的同时，右侧通道列表**照旧在页面上**（不再被选择器顶掉）
    // ⛔ 别写 `get(…).exists()`：`get()` 拿不到就直接抛（`.exists()` 恒真），且 vue-tsc 会报类型错。
    expect(wrapper.get("[data-testid='resource-table']").text()).toContain("通道 1");
    // 业务分组编辑弹窗此刻必须是关着的（别把两颗弹窗混成一颗）
    expect(document.body.querySelector("[data-testid='node-name-input']")).toBeNull();
    wrapper.unmount();
  });

  it("lets share-only users view the active mode but prevents saving a mode change", async () => {
    const wrapper = mountManager({ canManage: false });
    await flushPromises();
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("channel");
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("directory");
    expect(wrapper.get("[data-action='save']").attributes("disabled")).toBeDefined();
    expect(wrapper.get("[data-action='save']").attributes("disabled")).toBeDefined();
    expect(api.replace).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("accepts persisted resources after runtime sync failure and retries against the saved revision", async () => {
    api.getPlatform.mockResolvedValue({ id: 7, configRevision: 2, publishGroup: false, enabled: true, ptzEnabled: true });
    api.replace.mockImplementationOnce(async (_id: number, request: any) => ({
      ...baseData(),
      revision: 5,
      channels: [{ ...baseData().channels[0], ptzAllowed: true }],
      nodes: request.nodes,
      bindings: request.bindings,
      runtimeSynced: false,
      warning: "运行时暂不可用"
    }));
    const wrapper = mountManager();
    await flushPromises();
    // 造一份"有改动"的草稿：云台开关已移除，改用「取消共享该通道」把草稿置脏。
    await wrapper.get(".channel-row-31 [data-action='request-unshare']").trigger("click");
    await acceptConfirm();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("共享与目录已保存，但运行时同步失败");
    expect(wrapper.text()).not.toContain("当前草稿已保留");
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.emitted("saved")).toHaveLength(1);

    await wrapper.get("[data-action='retry-resource-sync']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenLastCalledWith(
      7,
      expect.objectContaining({
        expectedProjectionRevision: 5,
        resourceChanges: { shareChannelIds: [], unshareChannelIds: [], channelPermissions: [] }
      })
    );
    expect(wrapper.text()).not.toContain("运行时同步失败");
    wrapper.unmount();
  });

  it("stays on the workspace when save-before-exit persists but runtime sync fails", async () => {
    api.replace.mockImplementationOnce(async (_id: number, request: any) => ({
      ...baseData(),
      revision: 5,
      nodes: request.nodes,
      bindings: request.bindings,
      runtimeSynced: false
    }));
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}']`).trigger("click");
    await wrapper.get(`[data-code='${root.code}'] [data-action='edit-organization']`).trigger("click");
    await dialogEl("[aria-label='分组名称']").setValue("修改名称");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    const exit = (wrapper.vm as any).requestExit();
    await flushPromises();
    expect(lastConfirm().okText).toBe("保存并离开");
    await acceptConfirm();
    // 数据存下了但运行时没同步上 ⇒ 视为"没能干净地离开"：守卫退回 false、人留在页面上
    expect(await exit).toBe(false);
    expect(wrapper.text()).toContain("共享与目录已保存，但运行时同步失败");
    wrapper.unmount();
  });

  it.each([1, 10])("saves directory mode after moving %i shared channels out while retaining their shares", async count => {
    const data = baseData();
    const organization = {
      ...root,
      code: "34020000002160000002",
      name: "运营",
      kind: "organization" as const,
      parentCode: root.code
    };
    data.nodes.push(organization);
    data.channels = Array.from({ length: count + 1 }, (_, index) => ({
      ...data.channels[0],
      sourceChannelId: 31 + index,
      publishedChannelId: `340200000013200000${31 + index}`,
      name: `已共享通道 ${31 + index}`
    }));
    data.bindings = data.channels.map(channel => ({ sourceChannelId: channel.sourceChannelId, nodeCode: organization.code }));
    api.get.mockResolvedValue(data);
    api.getPlatform.mockResolvedValue({ id: 7, publishGroup: true, configRevision: 2, enabled: true });
    api.replace.mockImplementation(async (_id: number, request: any) => ({
      ...data,
      nodes: request.nodes,
      bindings: request.bindings,
      publishGroup: request.publishGroup,
      configRevision: 2,
      revision: 5
    }));
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.get(`[data-code='${organization.code}']`).trigger("click");
    for (const channel of data.channels.slice(0, count)) {
      await wrapper.get(`.channel-row-${channel.sourceChannelId} input[type='checkbox']`).setValue(true);
    }
    await wrapper.get("[data-action='move-out-selected']").trigger("click");
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenCalledWith(
      7,
      expect.objectContaining({
        bindings: [data.bindings[count]],
        publishGroup: true,
        resourceChanges: expect.objectContaining({ shareChannelIds: [], unshareChannelIds: [] })
      })
    );
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("directory");
    for (const channel of data.channels.slice(0, count)) {
      expect(wrapper.find(`.channel-row-${channel.sourceChannelId}`).exists()).toBe(false);
    }
    expect(wrapper.get("[data-testid='resource-table']").text()).toContain(data.channels[count].name);
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    for (const channel of data.channels) {
      expect(wrapper.get(`[data-testid='resource-table'] .channel-row-${channel.sourceChannelId}`).text()).toContain(
        channel.name
      );
    }
    wrapper.unmount();
  });

  it("saves directory mode with ungrouped shares but still rejects a missing business root", async () => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenCalledWith(7, expect.objectContaining({ bindings: [], publishGroup: true }));
    wrapper.unmount();

    api.get.mockResolvedValue({ ...baseData(), nodes: [] });
    api.replace.mockClear();
    const empty = mountManager();
    await flushPromises();
    empty.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await empty.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(api.replace).not.toHaveBeenCalled();
    expect(empty.text()).toContain("请先新建业务根");
    empty.unmount();
  });

  it("saves mode and assignments atomically and retains the tree when switching back", async () => {
    const data = baseData();
    data.nodes.push({ ...root, code: "34020000002160000002", name: "运营", kind: "organization", parentCode: root.code });
    // 通道 31 先挂在业务根下 —— 这样切到目录模式（范围＝业务根）就能看见它，
    // 再把它挪到子级「运营」上去。原先靠「未分组」入口找游离通道，那个入口 2026-10-14 已删除。
    data.bindings = [{ sourceChannelId: 31, nodeCode: root.code }];
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(".channel-row-31 input[type='checkbox']").setValue(true);
    await wrapper.get("[data-action='move-selected']").trigger("click");
    await dialogEl("[aria-label='移动目标组织']").setValue(data.nodes[1].code);
    await dialogEl("[data-action='confirm-move-dialog']").trigger("click");
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    const bindings = [{ sourceChannelId: 31, nodeCode: data.nodes[1].code }];
    expect(api.replace).toHaveBeenCalledTimes(1);
    expect(api.replace).toHaveBeenLastCalledWith(7, {
      nodes: data.nodes,
      bindings,
      expectedProjectionRevision: 4,
      expectedConfigRevision: 2,
      publishGroup: true,
      resourceChanges: { shareChannelIds: [], unshareChannelIds: [], channelPermissions: [] }
    });
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("directory");
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.emitted("publishGroupUpdated")?.at(-1)?.[0]).toMatchObject({ publishGroup: true, configRevision: 3 });
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenLastCalledWith(7, {
      nodes: data.nodes,
      bindings,
      expectedProjectionRevision: 5,
      expectedConfigRevision: 3,
      publishGroup: false,
      resourceChanges: { shareChannelIds: [], unshareChannelIds: [], channelPermissions: [] }
    });
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("channel");
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    expect(wrapper.get(".business-group-tree").text()).toContain("运营");
    // 通道 31 已经落到子级「运营」下，从业务根的概览范围里就能看见它 ——
    // 这条替代了原先「未分组」入口那个「0 路游离通道」的判据（那个入口已删除）。
    expect(wrapper.findAll(".channel-row-31")).toHaveLength(1);
    wrapper.unmount();
  });

  it("requires selecting a virtual organization before adding in directory mode", async () => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    expect(wrapper.find("[data-action='add-channels']").exists()).toBe(false);
    expect(wrapper.findComponent({ name: "BusinessChannelPicker" }).exists()).toBe(false);
    await wrapper.get("[data-action='add-organization']").trigger("click");
    // 新建改走弹窗：编号已按 216 预填，直接确定即为新建并选中
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-action='add-channels']").trigger("click");
    expect(wrapper.findComponent({ name: "BusinessChannelPicker" }).props("targetCode")).toBe("34020000002160000001");
    wrapper.unmount();
  });

  it("accepts a committed directory mode after runtime failure and retries with its revisions", async () => {
    const data = { ...baseData(), channels: [] };
    api.get.mockResolvedValue(data);
    api.replace.mockImplementationOnce(async (_id, request) => ({
      ...data,
      ...request,
      revision: 5,
      configRevision: 3,
      runtimeSynced: false,
      warning: "运行时暂不可用"
    }));
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("directory");
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);
    expect(wrapper.text()).toContain("运行时同步失败");
    await wrapper.get("[data-action='retry-resource-sync']").trigger("click");
    await flushPromises();
    expect(api.replace).toHaveBeenLastCalledWith(
      7,
      expect.objectContaining({
        expectedProjectionRevision: 5,
        expectedConfigRevision: 3,
        publishGroup: true
      })
    );
    expect(wrapper.text()).not.toContain("运行时同步失败");
    wrapper.unmount();
  });

  it("prefers the backend message over the generic HTTP error when loading fails", async () => {
    api.get.mockRejectedValueOnce(
      Object.assign(new Error("Request failed with status code 403"), {
        response: { status: 403, data: { message: "无权查看该上级平台的业务分组" } }
      })
    );
    const wrapper = mountManager();
    await flushPromises();

    expect(wrapper.text()).toContain("业务分组读取失败：无权查看该上级平台的业务分组");
    expect(wrapper.text()).not.toContain("Request failed with status code 403");
    wrapper.unmount();
  });

  it("prefers the backend message over the generic HTTP error when saving fails", async () => {
    api.replace.mockRejectedValueOnce(
      Object.assign(new Error("Request failed with status code 400"), {
        response: { status: 400, data: { message: "业务组织父节点必须是业务根或虚拟组织" } }
      })
    );
    const wrapper = mountManager();
    await flushPromises();
    await wrapper.get("[data-action='save']").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("业务组织父节点必须是业务根或虚拟组织");
    expect(wrapper.text()).not.toContain("Request failed with status code 400");
    wrapper.unmount();
  });
});

/**
 * 共享通道表的「竖向滚动条落在表格自己身上」是一组**只能靠几何实测发现**的约束：
 * 少写一条 CSS 不会报错、不会被任何行为用例抓住，页面照常渲染，只是滚动条悄悄跑回外层、
 * 表头跟着一起滚走（2026-10-14 实测：表格根节点 1291px、表体 scrollHeight==clientHeight 完全滚不动）。
 * 所以这里对**源码文本**做反向断言，任何一条被删掉就直接红。
 */
describe("shared channel table height chain", () => {
  const read = (file: string) => readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade", file), "utf8");
  const managerSource = read("BusinessGroupManager.vue");
  const pageSource = read("resources.vue");
  const cssBlock = (source: string, selector: string) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
  };

  it("pins the scrollbar to the table instead of the outer container", () => {
    // 表格声明 y:'100%'（字符串才是给根节点 height:100%，数字变成表体 max-height）
    expect(managerSource).toContain(":scroll=\"{ x: 1000, y: '100%' }\"");
    // 根节点定高并裁掉溢出，交给内层容器吃满
    const table = cssBlock(managerSource, ".business-group-channel-table");
    expect(table).toContain("flex: 1");
    expect(table).toContain("overflow: hidden");
    expect(managerSource).toContain(".business-group-channel-table :deep(.arco-table-container) {");
    // 外层容器不能再自己出滚动条，否则滚动条贴在面板边上
    expect(cssBlock(pageSource, ".cascade-resources-page")).not.toContain("overflow: auto");
  });

  it("keeps a definite height all the way up the chain", () => {
    // ⛔ 最关键的一行：flex 子项默认 min-height:auto ⇒ 整页被内容顶高、height:100% 退化成 auto。
    expect(cssBlock(pageSource, ".cascade-resources-page")).toContain("min-height: 0");
    expect(cssBlock(pageSource, ".cascade-resources-body")).toContain("min-height: 0");
    // 网格行默认是隐式 auto ⇒ 明细面板按内容高算，同样把定高链顶断。
    expect(cssBlock(managerSource, ".business-group-workspace")).toContain("grid-template-rows: minmax(0, 1fr)");
    expect(cssBlock(managerSource, ".business-group-detail")).toContain("min-height: 0");
  });
});

/** 工具行复用系统配置 Tabs 的外观，操作按钮保留统一的大尺寸。 */
describe("business group toolbar sizing", () => {
  const read = (file: string) => readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade", file), "utf8");
  const managerSource = read("BusinessGroupManager.vue");
  const cssBlock = (source: string, selector: string) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
  };
  const toolbarTemplate = managerSource.slice(
    managerSource.indexOf('<div class="business-group-toolbar"'),
    managerSource.indexOf('id="sharing-mode-panel"')
  );

  it("lets nothing inside the toolbar row override its own size", () => {
    // 切片必须真的切到工具行（indexOf 返回 -1 时 slice 会给出无关内容，断言就成空转）
    expect(toolbarTemplate).toContain('data-action="save"');
    expect(toolbarTemplate).not.toMatch(/<a-(?:tabs|button)\b[^>]*\bsize=/);
  });

  it("uses shared system tabs styling and keeps action buttons enlarged", () => {
    expect(toolbarTemplate).toContain('class="uvp-system-tabs business-group-sharing-tabs"');
    expect(managerSource).not.toContain(".business-group-sharing-tabs .arco-radio-button");
    // 操作按钮统一加大，保持等高、等宽。
    expect(cssBlock(managerSource, ".business-group-platform-actions .arco-btn")).toContain("min-width: 88px");
    expect(cssBlock(managerSource, ".business-group-platform-actions .arco-btn")).toContain("height: 40px");
  });
});

/**
 * 树节点图标（2026-10-14 老板要求：「把这个设备树优化一下，就是在节点上加上对应的图标」）。
 *
 * ⭐ 判据是 **`node.kind`**（业务根 215 / 虚拟组织 216），不是 Arco `#icon` 插槽给的 `isLeaf`：
 *   两类节点都可能是"有子节点或者没子节点"，靠 isLeaf 会把它们显示成同一个图标
 *   ⇒ 图标只能写在 `#title` 插槽体内，这里既断 DOM（真 a-tree 渲染结果）也断源码（防止被搬回去）。
 */
describe("business group tree node icons", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/BusinessGroupManager.vue"), "utf8");

  /**
   * ⛔ 这个 describe 在**主 describe 外面**，继承不到它那份 `beforeEach`。
   *    不自己补接口 mock 的话，`getCascadePlatform` 返回 undefined ⇒ 工作区渲染出来但 tab 是空的
   *    ⇒ `findComponent({ name: "Tabs" }).vm` 直接抛 "Cannot call vm on an empty VueWrapper"。
   */
  beforeEach(() => {
    api.get.mockReset();
    api.getPlatform.mockReset();
    api.get.mockResolvedValue(baseData());
    api.getPlatform.mockResolvedValue({
      id: 7,
      configRevision: 2,
      publishGroup: false,
      enabled: true,
      effectiveVersion: "2016"
    });
  });

  const ORG_CODE = "34020000002160000002";
  const CHILD_CODE = "34020000002160000003";
  const iconOf = (wrapper: ReturnType<typeof mountManager>, code: string) =>
    wrapper.get(`[data-code='${code}'] [data-testid='tree-node-icon']`).attributes("data-icon");

  /** 造一棵**有下级**的树：业务根 → 运营（虚拟组织）→ 客服（下级虚拟组织）。 */
  const withOrganization = async () => {
    const data = baseData();
    data.nodes.push(
      { ...root, code: ORG_CODE, name: "运营", kind: "organization", parentCode: root.code },
      { ...root, code: CHILD_CODE, name: "客服", kind: "organization", parentCode: ORG_CODE }
    );
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    return wrapper;
  };

  it("gives the business root and virtual organizations different icons", async () => {
    const wrapper = await withOrganization();
    // 两类节点必须是两个不同的图标，否则"加上对应图标"这个需求等于没做
    expect(iconOf(wrapper, root.code)).toBe("folder-tree");
    // 加载完默认**全部展开**（`load()` 里 `expandedNodeCodes = nodes.map(node => node.code)`），
    // 所以组织节点初始是「打开的文件夹」；折叠以后才变 `folder`（下一条用例覆盖）。
    expect(iconOf(wrapper, ORG_CODE)).toBe("folder-open");
    // 真图标（lucide 渲染出来的 svg），不是个空 span
    expect(wrapper.get(`[data-code='${root.code}'] [data-testid='tree-node-icon']`).element.tagName.toLowerCase()).toBe("svg");
    // 纯装饰：名称就在后面，读屏不该再念一遍图标
    expect(wrapper.get(`[data-code='${root.code}'] [data-testid='tree-node-icon']`).attributes("aria-hidden")).toBe("true");
    wrapper.unmount();
  });

  it("switches a virtual organization icon between collapsed and expanded", async () => {
    const wrapper = await withOrganization();
    const tree = wrapper.findComponent({ name: "Tree" });
    // 折叠「运营」：只留业务根展开
    tree.vm.$emit("update:expandedKeys", [root.code]);
    await flushPromises();
    expect(iconOf(wrapper, ORG_CODE)).toBe("folder");
    // 顺带证明确实折叠生效了（否则上一条断言可能只是因为"图标恒为 folder"）
    expect(wrapper.find(`[data-code='${CHILD_CODE}']`).exists()).toBe(false);
    // 再展开
    tree.vm.$emit("update:expandedKeys", [root.code, ORG_CODE]);
    await flushPromises();
    expect(iconOf(wrapper, ORG_CODE)).toBe("folder-open");
    expect(wrapper.find(`[data-code='${CHILD_CODE}']`).exists()).toBe(true);
    // ⛔ 业务根不随展开变化：它本来就是整棵共享目录树的根，换成"打开的文件夹"反而丢了层次
    expect(iconOf(wrapper, root.code)).toBe("folder-tree");
    wrapper.unmount();
  });

  it("keeps the icons in the title slot instead of Arco's icon slot", () => {
    // ⛔ Arco 的 `#icon` 插槽只收到 `nodeStatus`（loading / checked / selected / expanded / isLeaf），
    //    **拿不到 kind / code** ⇒ 搬过去之后业务根和虚拟组织立刻变成同一个图标。
    const titleSlot = source.slice(source.indexOf('<template #title="node">'), source.indexOf("</a-tree>"));
    expect(titleSlot).toContain('data-testid="tree-node-icon"');
    expect(titleSlot).toContain("NODE_ICONS[nodeIconName(node)]");
    expect(source).not.toContain('<template #icon="{ isLeaf, expanded }">');
  });

  it("reuses the shared tree icon class and its hover / dark-mode behaviour", () => {
    // ⭐ 复用全局 `.uvp-tree-node-icon`（自带"悬停 / 选中变品牌色" + 暗色兜底），别自己再造一套。
    expect(source).toContain("uvp-tree-node-icon business-group-tree-icon");
    // ⛔ 颜色规则必须是**裸类名**（`<类> {`），不能写成 `:deep(<类>)`：
    //    `:deep()` 会把特异性抬到高于"悬停 / 选中走品牌色"那条 ⇒ 选中时图标不再高亮。
    expect(source).toContain(".business-group-tree-icon--group {");
    expect(source).toContain(".business-group-tree-icon--organization {");
    // 配色沿用部门树那套（橙 = 目录 / 青 = 组织），同级三棵树观感一致
    expect(source).toContain("#c47a18");
    expect(source).toContain("#0f8b83");
  });

  it("wraps the whole dark-mode selector inside :global()", () => {
    // ⛔⛔ 暗色两条必须在：全局暗色兜底 `body[arco-theme="dark"] .uvp-tree-node-icon` 会把两个修饰类一起盖成灰。
    // ⛔⛔ 而且 `:global(...)` 必须把**整段选择器**包进去、`)` 后面直接就是 `{` ——
    //    只让它包住 `body[arco-theme="dark"]`、后面接着写 `.foo :deep(.bar)`（本仓另外几个面板就是这么写的）
    //    会被 scoped 编译器**静默截断**成 `body[arco-theme="dark"] { … }`：
    //    后半截选择器整个丢掉、规则挂到 `<body>` 上、不报错、也不生效。
    //    （2026-10-14 真机 `document.styleSheets` 里抓到过这个坏形态，光看源码完全看不出来。）
    expect(source).toMatch(/:global\(body\[arco-theme="dark"\][^){]*business-group-tree-icon--group\)\s*\{/);
    expect(source).toMatch(/:global\(body\[arco-theme="dark"\][^){]*business-group-tree-icon--organization\)\s*\{/);
    // 反面：`:global(body[…])` 括号闭合之后再跟别的选择器 = 会被截断的那个写法
    expect(source).not.toMatch(/:global\(body\[arco-theme="dark"\]\)\s*[.:]/);
  });

  it("uses the same expand arrow as the other tree panels", () => {
    // 同级三棵树的展开箭头必须一样（Arco 默认那个小三角在这套 UI 里已经没人用了）
    expect(source).toContain("<template #switcher-icon>");
    expect(source).toContain('ChevronDown class="arco-tree-node-switcher-icon"');
  });
});

/**
 * 树节点行内操作按钮必须**常显**（2026-10-14 老板要求：「需要本身就显示不要悬浮出现」）。
 * 原先做成了 `opacity: 0` + `:hover` 显形 —— 藏起来的按钮等于告诉用户"这一行没有操作"，
 * 而右键菜单本来就不显眼，两个入口都藏着就等于没有入口。
 * ⛔ 这类"删掉一段 CSS 什么都不会红"的东西只能靠源码级断言守（同上一个 describe）。
 */
describe("business group tree node actions", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/BusinessGroupManager.vue"), "utf8");
  const cssBlock = (selector: string) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
  };

  it("keeps the in-row buttons visible instead of revealing them on hover", () => {
    const buttons = cssBlock(".business-group-tree-buttons");
    // 先确认切片真的切到了规则体：否则下面两条 `not.toContain` 都是空转通过。
    expect(buttons).toContain("display: inline-flex");
    expect(buttons).not.toContain("opacity");
    expect(source).not.toMatch(/\.business-group-tree-buttons\s*\{[^}]*opacity\s*:/);
    // 标题块撑满整行，三颗按钮才会靠 `margin-left: auto` 统一贴到行尾成一列
    expect(cssBlock(".business-group-tree-title")).toContain("width: 100%");
  });

  it("keeps the width chain open so a long node name cannot push the buttons out of view", () => {
    // ⛔ 这是"按钮常显"之后才暴露的问题：按钮藏起来时名字把整行撑出去看不出来，
    //    常显以后按钮直接被推到面板外（真机量到 rightGap = -237px，按钮完全看不见）。
    //    决定性的一条是给 Arco 的 `title-block` 补 `min-width: 0`（真机逐条试出来的：
    //    只给 wrapper 加 overflow / 只给它 flex 都没用，必须松掉 block 的自动最小尺寸）。
    // ⛔ 断言必须**只截这一条 `:deep` 规则自己的声明块** —— 之前用"从这条规则到大括号"的整段切片，
    //    被同一段里的 `.business-group-tree-title { min-width: 0 }` 蒙过去了（反证时 1 passed，等于没守住）。
    //    切片稍微松一点，门禁就会变成恒真。
    // ⛔ 也别想用"选择器后面跟 `{`"把两条规则区分开：`title-text` 那层出现**两次**
    //    （共享块里紧跟逗号那条 + 后面独立的 `flex: 1` 那条），而共享块的选择器后面照样紧跟着 `{`。
    //    干脆把两条规则的声明块都取出来一起看（行为上只要"有规则给它们写了这两条"就算守住）。
    const sharedChain = source.match(/:deep\(\.arco-tree-node-title-block\)[^{]*\{([^}]*)\}/)?.[1] ?? "";
    expect(sharedChain).toContain("min-width: 0");
    const textRuleBodies = [...source.matchAll(/:deep\(\.arco-tree-node-title-text\)[^{]*\{([^}]*)\}/g)]
      .map(match => match[1])
      .join("");
    expect(textRuleBodies).toContain("flex: 1");
    expect(textRuleBodies).toContain("min-width: 0");
    // 我们那层自己也得开着（`width: 100%` 靠它才能被压住而不是溢出）
    const title = cssBlock(".business-group-tree-title");
    expect(title).toContain("width: 100%");
    expect(title).toContain("min-width: 0");
    // 名字必须允许省略号，否则它只会溢出而不是被压缩
    const name = cssBlock(".business-group-tree-name");
    expect(name).toContain("text-overflow: ellipsis");
    expect(name).toContain("overflow: hidden");
  });
});

/**
 * 树的行背景左边缘必须与搜索框左边缘对齐（2026-10-15 老板要求：
 * 「参照设备列表那棵树调整一下，主要是左边可以和输入框的左边对齐」）。
 *
 * ⛔ 背景来源：Arco 的行是 `[indent][switcher][title]` 三个平级兄弟，`block-node` 让 title 撑满整行
 *   ⇒ 高亮背景只能从箭头右边开始，比搜索框左边缘右 23px（真机 1440 实测：搜索框 308 / 根节点 title 331）。
 *   设备权限工作台那棵树没开 `block-node` 所以看不出；我们这棵必须开（三颗行内按钮要钉行尾）。
 * ⛔ 这是**纯 CSS 的观感约束**：删掉这两条规则什么都不会红（`getBoundingClientRect` 在 jsdom 里恒 0），
 *   只能靠源码断言守。断言的是"两条声明成对且和为 2px"这个**不变量**：
 *   `margin-left` 左移多少，`padding-left` 就得补回多少（原态是 Arco 的 `-4px + 6px = 2px`），
 *   这样图标 / 名字 / 按钮的水平位置一个像素都不会跟着漂。
 */
describe("business group tree row alignment", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/BusinessGroupManager.vue"), "utf8");
  const ruleOf = (selector: string) => {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
  };

  it("pulls the row background out to the search box without moving the row content", () => {
    const title = ruleOf(".business-group-tree :deep(.arco-tree-node-title)");
    // 先确认切片真的切到了规则体，否则下面全是空转通过
    expect(title).toContain("margin-left");
    expect(title).toContain("padding-left");
    const margin = title.match(/margin-left:\s*(-?\d+(?:\.\d+)?)px/);
    const padding = title.match(/padding-left:\s*(-?\d+(?:\.\d+)?)px/);
    expect(margin, "没找到 margin-left 的 px 值").not.toBeNull();
    expect(padding, "没找到 padding-left 的 px 值").not.toBeNull();
    const marginPx = Number(margin![1]);
    const paddingPx = Number(padding![1]);
    // 左移 23px 才能贴到搜索框边缘（这个值是 Arco 的 indent+switcher 占位算出来的，真机量过）
    expect(marginPx).toBe(-27);
    expect(paddingPx).toBe(29);
    // ⛔ 不变量：两条之和必须还是 Arco 原态的 2px（`-4px + 6px`）⇒ 内容水平位置不动。
    //    只改其中一条 = 整行内容跟着漂（试过：去掉 padding-left 后图标从 337 跑到 314）。
    expect(marginPx + paddingPx).toBe(2);
  });

  it("keeps the expand arrow clickable after the background covers it", () => {
    // ⛔ 这条不是观感修饰，是**功能**：背景块左移后正好压住箭头，而 title 在 DOM 里排在 switcher 后面
    //    ⇒ 不给 switcher 抬层，点在箭头上的事件会被 title 吃掉，展开 / 折叠直接点不动。
    const switcher = ruleOf(".business-group-tree :deep(.arco-tree-node-switcher)");
    expect(switcher).toContain("position: relative");
    expect(switcher).toMatch(/z-index:\s*[1-9]/);
  });

  it("gives the search box a magnifier prefix, matching the department tree", () => {
    // 参照对象的搜索框有放大镜前缀（`device-assignment` / `account` 的部门树都是这么写的）
    expect(source).toContain("<template #prefix><icon-search /></template>");
    expect(source).toMatch(/import \{[^}]*IconSearch[^}]*\} from "@arco-design\/web-vue\/es\/icon"/);
  });
});

/**
 * 目录模式的批量编排控件**必须在筛选区里**（2026-10-14 老板要求：「第二栏给放到上面那一栏去，然后把这一栏给干掉」）。
 * 上面那条 DOM 用例只能证明"控件此刻在 `#extra` 里"，证明不了"没人又加回一根独立工具条" ——
 * 而多出一根 `s-layout-tools` 正是老板两次否掉的「凭空多一个区域」。
 * ⛔ 这类删除类约束删了不会红（`s-layout-tools` 是个自带背景+边框的卡片组件，删掉只是少一块 DOM），
 *   所以用源码断言兜住：模板里不许再出现它的标签，且筛选区与表格之间不许夹任何动作控件。
 */
describe("business group batch controls layout", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/BusinessGroupManager.vue"), "utf8");

  it("keeps the batch controls in the filter panel and leaves no toolbar between it and the table", () => {
    // ① 组件里彻底不再引用 `s-layout-tools`（标签 + import 两处，缺一条都会让"第二栏"复活）
    expect(source).not.toContain("<s-layout-tools");
    expect(source).not.toContain("SLayoutTools");
    // ② 三颗控件写在 `#extra` 槽里（取 `s-layout-search` 标签到其闭合标签之间的模板切片）
    const searchStart = source.indexOf("<s-layout-search");
    const searchEnd = source.indexOf("</s-layout-search");
    // 先确认切片真的切到了筛选区，否则下面的 `toContain` 全是空转（indexOf 返回 -1 时 slice 给出无关内容）
    expect(searchStart).toBeGreaterThan(-1);
    expect(searchEnd).toBeGreaterThan(searchStart);
    const searchTemplate = source.slice(searchStart, searchEnd);
    expect(searchTemplate).toContain(`data-action="move-selected"`);
    expect(searchTemplate).toContain(`data-action="move-out-selected"`);
    expect(searchTemplate).toContain(`data-action="request-unshare-selected"`);
    // ⛔ 「全选筛选结果（跨页）」2026-10-15 老板要求删除，**不许再回来**：
    //   它和表头勾选框（本页全选）语义重叠，两颗并存让人分不清"这次到底选了多少路"。
    //   删除类改动的门禁不能只靠 DOM 用例（现在断"它不存在"是恒真的），所以在这里钉死。
    expect(searchTemplate).not.toContain("select-all-filtered");
    // ③ 筛选区之后、表格之前**不许**再夹任何动作控件（第二栏就是被这么删掉的）
    const tableStart = source.indexOf(`data-testid="resource-table"`);
    expect(tableStart).toBeGreaterThan(searchEnd);
    expect(source.slice(searchEnd, tableStart)).not.toContain("data-action=");
  });
});

/**
 * 「页面内嵌确认条 → confirm 弹窗」的门禁（2026-10-15 老板要求：
 * 截图上那条「有未保存修改」以及同款的黄条全部拆掉，改成 confirm 组件）。
 *
 * ⛔ 这类改动**没有任何编译期信号**：某条内嵌黄条被偷偷加回来、或者某个动作漏接 `Modal.confirm`，
 *   代码照样编译、页面照样能跑，只有用户点到那一步才发现"确认框没弹 / 又横出来一条黄框"。
 * ⇒ ① 源码级：那几组内嵌确认条的类名与 `role="alertdialog"` 必须绝迹；
 *    ② 行为级：逐个入口点一遍，**必须先弹 confirm**；且**取消（含右上角 ×）绝不能动草稿**。
 */
describe("business group confirm dialogs", () => {
  const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cascade/BusinessGroupManager.vue"), "utf8");

  beforeEach(stubApis);

  it("keeps no in-page confirmation bar behind", () => {
    // 三组内嵌确认条（截图那条「有未保存修改」就是第一组）连同它们的样式一起删干净
    expect(source).not.toContain("business-group-exit-choice");
    expect(source).not.toContain("business-group-move-confirm");
    expect(source).not.toContain("business-group-reload-confirm");
    expect(source).not.toContain('role="alertdialog"');
    // 反面锚点：确认文件没被读错/读空（否则上面四条"绝迹"全是空转）
    expect(source).toContain('aria-label="共享与目录工作区"');
    expect(source).toContain("有未保存修改");
  });

  it("pops a confirm for unshare / discard / leaving and cancelling never touches the draft", async () => {
    const orgCode = "34020000002160000002";
    const data = baseData();
    data.nodes.push({ ...root, code: orgCode, name: "运营", kind: "organization", parentCode: root.code });
    api.get.mockResolvedValue(data);
    const wrapper = mountManager();
    await flushPromises();

    // ① 取消共享：确认前草稿必须一点没动
    await wrapper.get(".channel-row-31 [data-action='request-unshare']").trigger("click");
    expect(lastConfirm().title).toBe("取消共享");
    await dismissConfirm();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);

    // 造一份脏草稿，后面三个入口都靠它
    await wrapper.get(".channel-row-31 [data-action='request-unshare']").trigger("click");
    await acceptConfirm();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);

    // ② 返回（带脏草稿）⇒ 弹窗，取消 = 继续编辑，不能顺手关掉面板
    await wrapper.get("[data-action='close-manager']").trigger("click");
    expect(lastConfirm().title).toBe("有未保存修改");
    expect(lastConfirm().cancelText).toBe("继续编辑");
    await dismissConfirm();
    expect(wrapper.emitted("update:visible")).toBeUndefined();
    expect(wrapper.find("[data-testid='change-summary']").exists()).toBe(false);

    // 放弃草稿只在离开确认中提供，页面工具栏仍然不放丢弃按钮。
    expect(wrapper.find("[data-action='discard-changes']").exists()).toBe(false);
    wrapper.unmount();
  });

  it.each(["route", "close", "push"])("discards all draft changes without saving before %s", async action => {
    const wrapper = mountManager();
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();
    await wrapper.get(`[data-code='${root.code}'] [data-action='edit-organization']`).trigger("click");
    await dialogEl("[aria-label='分组名称']").setValue("未保存名称");
    await dialogEl("[data-action='confirm-node-dialog']").trigger("click");
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "channel");
    await flushPromises();
    await wrapper.get(".channel-row-31 [data-action='request-unshare']").trigger("click");
    await acceptConfirm();
    await wrapper.get("[data-action='add-channels']").trigger("click");
    wrapper
      .findComponent({ name: "BusinessChannelPicker" })
      .vm.$emit("apply", [{ ...baseData().channels[0], sourceChannelId: 32, name: "新增通道" }]);
    await flushPromises();
    wrapper.findComponent({ name: "Tabs" }).vm.$emit("change", "directory");
    await flushPromises();

    let exit: Promise<boolean> | undefined;
    if (action === "route") exit = (wrapper.vm as any).requestExit();
    else await wrapper.get(`[data-action='${action === "close" ? "close-manager" : "push-catalog"}']`).trigger("click");
    await flushPromises();
    const footer = mount(defineComponent({ render: () => lastConfirm().footer() }));
    expect(footer.findAll("button").map(button => button.text())).toEqual(["继续编辑", "放弃并离开", "保存并离开"]);
    await footer.get("[data-action='discard-and-leave']").trigger("click");
    await flushPromises();

    expect(api.replace).not.toHaveBeenCalled();
    expect(wrapper.findComponent({ name: "Tabs" }).props("activeKey")).toBe("channel");
    expect(wrapper.find(".channel-row-31").exists()).toBe(true);
    expect(wrapper.find(".channel-row-32").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("未保存名称");
    expect(wrapper.emitted("dirtyChange")?.at(-1)).toEqual([false]);
    if (exit) expect(await exit).toBe(true);
    else if (action === "close") expect(wrapper.emitted("update:visible")?.at(-1)).toEqual([false]);
    else {
      expect(lastConfirm().title).toBe("推送上级平台目录");
      expect(lastConfirm().content).toContain("业务分组发布已关闭");
      expect(api.push).not.toHaveBeenCalled();
    }
    footer.unmount();
    wrapper.unmount();
  });
});
