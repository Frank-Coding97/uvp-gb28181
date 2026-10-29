import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";

/**
 * 「通道配置」对话框的入口契约（GB/T 28181 配置族里不需要播放上下文的那四组）。
 *
 * ## 一、为什么必须钉「有入口」
 *
 * 2026-09-20 的一轮 UI 收敛里，「录像存储 / 报警控制 / 设备维护」三个整页签从播放控制台
 * 搬进了**设备详情抽屉**；同日稍后又把设备详情抽屉改成**纯只读概览**（`index.deviceDetailTabs.test.ts`
 * 明确断言 `not.toContain("openDeviceRecordPlan(deviceDetail)")`），播放控制台侧栏也被
 * `PlayConsoleLinked.test.ts` 钉死"不许出现录像计划"。
 *
 * ⇒ 结果是一整片功能**代码在、接口在、报文在，界面上一个入口都没有**：
 *   录像计划（A.2.1.15）/ 报警录像（A.2.1.16）/ 报警上报（A.2.1.18）/ 基本参数（A.2.1.19）。
 *
 * ## 二、为什么是对话框而不是抽屉
 *
 * ① `还原 / 读取 / 下发` 是**全局动作**（下发的是整组报文，不是某个字段），必须固定在底部；
 *    抽屉 `:footer="false"`，按钮只能塞在内容里，周计划一滚就没了。
 * ② 面板是「左导航 + 时间轴」，抽屉 720px 已顶格。
 * ③ 有「未下发修改」草稿态，对话框的关闭拦截是标准套路。
 *
 * ⛔ 这三条都**不会报错**、也不影响接口通不通 —— 抽屉照样能打开、照样能下发，
 *    只是"配到一半滚下去找不到按钮""点遮罩草稿没了"。所以只能源码级守。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

/** `<a-modal ...>` 的开标签（这个面板是全仓唯一带 `channel-config-dialog` 的那颗）。 */
const modalStart = source.indexOf("channel-config-dialog");
const modalTagStart = source.lastIndexOf("<a-modal", modalStart);
const modalTag = source.slice(modalTagStart, source.indexOf(">", modalStart));

/** 页签区（a-tabs 内部）。 */
const tabsStart = source.indexOf('class="channel-config-tabs"');
const tabsEnd = source.indexOf("</a-tabs>", tabsStart);
const tabsMarkup = source.slice(tabsStart, tabsEnd);

/** 「设备配置」页签挂载的那一段（到下一个面板为止）。 */
const panelStart = source.indexOf("<DeviceConfigDrawer", tabsEnd);
const panelEnd = source.indexOf("<SnapshotConfigPanel", panelStart);
const panelMarkup = source.slice(panelStart, panelEnd);

/** `CHANNEL_CONFIG_GROUPS` 的取值。 */
const groupsMatch = source.match(/const CHANNEL_CONFIG_GROUPS = \[([^\]]*)\]/);
const groups = ((groupsMatch?.[1] ?? "").match(/"([^"]+)"/g) ?? []).map(item => item.replace(/"/g, ""));

describe("通道配置对话框", () => {
  it("容器是 a-modal，且挂了房规类（否则面板底色/圆角/暗色控件色全部失效）", () => {
    expect(modalTagStart).toBeGreaterThan(-1);
    // 房规：全仓每一颗 `<a-modal>` 都必须带 `uvp-system-dialog`（见 style/model/system-dialog-unification.test.ts）。
    expect(modalTag).toContain("uvp-system-dialog");
    expect(modalTag).toContain("channel-config-dialog");
  });

  it("点遮罩不许关：草稿没有恢复路径，点一下就没了而且不报错", () => {
    expect(hasMarkup(modalTag, ':mask-closable="false"')).toBe(true);
    // 关掉要整块销毁，否则四组回读轮询在后台空转。
    expect(modalTag).toContain("unmount-on-close");
  });

  it("页签收敛为四个，云台那一页交还给播放控制台", () => {
    expect(tabsStart).toBeGreaterThan(-1);
    expect(tabsEnd).toBeGreaterThan(tabsStart);
    // ⛔ 不许再把「即时控制」挂回来：云台/变倍/聚焦要画面反馈，这个面板没有画面，
    //    挂在这里等于让人闭眼开云台 —— 转过去看不见，转坏了也发现不了。
    expect(tabsMarkup).not.toContain("即时控制");
    expect(tabsMarkup).not.toContain('key="control"');
    expect(hasMarkup(tabsMarkup, `<a-tab-pane key="config" title="设备配置"`)).toBe(true);
    expect(hasMarkup(tabsMarkup, `<a-tab-pane key="status" title="设备状态"`)).toBe(true);
    expect(hasMarkup(tabsMarkup, `<a-tab-pane key="snapshot" title="抓拍参数"`)).toBe(true);
    expect(hasMarkup(tabsMarkup, `<a-tab-pane key="storage" title="存储卡"`)).toBe(true);
  });

  it("页签的可见性只认这条接口自己的读权限，不借用相邻页签的控制权", () => {
    // ⛔ `/channel/:id/device-configs` 的读门禁是 `gb28181:ptz:view`（见 routes.go），
    //    写门禁是 `gb28181:ptz:control`。相邻能力用的是 `gb28181:device:control`
    //    （`/device-control` 那条链路）—— 套过来就是"看得见页签、点读取 403"。
    expect(tabsMarkup).not.toContain("canUseDeviceControl");
    expect(tabsMarkup).not.toContain("canControlDevice");
    expect(source).toContain('const canControlChannelConfig = computed(() => hasPermission("gb28181:ptz:control"));');
  });

  it("挂的是 DeviceConfigDrawer 的嵌入形态 + 侧栏导航，且只挂这四组", () => {
    expect(panelStart).toBeGreaterThan(-1);
    // `embedded` 必带：不带就是 position:fixed 的全屏遮罩，会把对话框整个盖住。
    // `nav-side` 必带：横排导航在 1000px 宽里会拉成一条没人会点第二遍的色带。
    expect(hasMarkup(panelMarkup, "embedded config-only nav-side")).toBe(true);
    expect(hasMarkup(panelMarkup, ':group-keys="CHANNEL_CONFIG_GROUPS"')).toBe(true);
    // 通道上下文必须一路传下去：设备编码用于报文 DeviceID，channelId 驱动按通道读快照。
    expect(hasMarkup(panelMarkup, ':device-code="channelOperationTarget.deviceId"')).toBe(true);
    expect(hasMarkup(panelMarkup, ':channel-id="channelOperationTarget.id"')).toBe(true);
    // 下发闸门：有写权限 **且** 通道在线。离线设备发出去只会等超时。
    expect(hasMarkup(panelMarkup, ':can-apply="canControlChannelConfig && channelOperationTarget.status === 1"')).toBe(true);

    expect(groups).toEqual(["record-plan", "alarm-record", "alarm-report", "basic"]);
  });

  it("不与另外两个编辑面重叠：视频参数归播放控制台，画面处理归底栏卡片", () => {
    // ⛔ 把 `video-param` 加进来，同一份视频参数就有了两个编辑面（一个按码流分行、
    //    一个走通用容器），对账粒度会互相污染；`picture`（镜像 + 遮挡）要画布框选，
    //    这里没有画面，加进来就是一个画不出框的形态。
    expect(groups).not.toContain("video-param");
    expect(groups).not.toContain("picture");
    expect(groups).not.toContain("osd");
  });

  it("切走要销毁实例（v-if 链），不是 v-show 挂后台", () => {
    // ⛔ `v-show` 会让四组的回读轮询在后台空转，并且下次打开还带着上一次通道的草稿。
    expect(hasMarkup(source, `v-if="channelOperationTab === 'config' && canViewDeviceFacts"`)).toBe(true);
    expect(panelMarkup).not.toContain("v-show");
  });

  it("标题栏能直接翻上下一个通道（配 12 路不用 12 次开关对话框）", () => {
    const titleStart = source.indexOf('class="channel-config-title"');
    const titleEnd = source.indexOf("</template>", titleStart);
    expect(titleStart).toBeGreaterThan(-1);
    expect(titleEnd).toBeGreaterThan(titleStart);
    const titleMarkup = source.slice(titleStart, titleEnd);
    expect(hasMarkup(titleMarkup, 'data-testid="channel-config-prev"')).toBe(true);
    expect(hasMarkup(titleMarkup, 'data-testid="channel-config-next"')).toBe(true);
    // ⛔ 顺序取当前列表渲染顺序：用户看到第几行就翻第几行，用后端全量顺序会"跳着翻"。
    expect(squash(source)).toContain("channels.value.findIndex(item=>item.id===current.id)");
  });
});
