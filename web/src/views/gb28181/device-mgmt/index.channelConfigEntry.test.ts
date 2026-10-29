import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";

/**
 * 「设备配置」页签的入口契约（GB/T 28181 配置族里不需要播放上下文的那四组）。
 *
 * ## 为什么要专门钉这一条
 *
 * 2026-09-20 的一轮 UI 收敛里，「录像存储 / 报警控制 / 设备维护」三个整页签从播放控制台
 * 搬进了**设备详情抽屉**；同日稍后又把设备详情抽屉改成**纯只读概览**（`index.deviceDetailTabs.test.ts`
 * 明确断言 `not.toContain("openDeviceRecordPlan(deviceDetail)")`），播放控制台侧栏也被
 * `PlayConsoleLinked.test.ts` 钉死"不许出现录像计划"。
 *
 * ⇒ 结果是一整片功能**代码在、接口在、报文在，界面上一个入口都没有**：
 *   录像计划（A.2.1.15）/ 报警录像（A.2.1.16）/ 报警上报（A.2.1.18）/ 基本参数（A.2.1.19）。
 *   `DeviceConfigDrawer` 早就支持这四组（读写 + 回读对账），缺的只是一个挂载点。
 *
 * 本用例钉的就是这个挂载点：页签在、挂的是这四个组、权限用的是这条接口自己的门禁。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

/** 「通道操作」抽屉里的页签组（只看 radio-group 内部）。 */
const tabsStart = source.indexOf('class="channel-operation-tabs"');
const tabsEnd = source.indexOf("</a-radio-group>", tabsStart);
const tabsMarkup = source.slice(tabsStart, tabsEnd);

/** 「设备配置」页签那一段（到下一个面板为止）。 */
const panelStart = source.indexOf('class="channel-config-panel"');
const panelEnd = source.indexOf("<SnapshotConfigPanel", panelStart);
const panelMarkup = source.slice(panelStart, panelEnd);

/** `CHANNEL_CONFIG_GROUPS` 的取值。 */
const groupsMatch = source.match(/const CHANNEL_CONFIG_GROUPS = \[([^\]]*)\]/);
const groups = ((groupsMatch?.[1] ?? "").match(/"([^"]+)"/g) ?? []).map(item => item.replace(/"/g, ""));

describe("通道操作抽屉的「设备配置」页签", () => {
  it("页签在同一组单选里，且排在最常用的「即时控制」之后", () => {
    expect(tabsStart).toBeGreaterThan(-1);
    expect(tabsEnd).toBeGreaterThan(tabsStart);
    expect(hasMarkup(tabsMarkup, `<a-radio :disabled="!canViewDeviceFacts" value="config">设备配置</a-radio>`)).toBe(true);
    expect(tabsMarkup.indexOf('value="control"')).toBeLessThan(tabsMarkup.indexOf('value="config"'));
  });

  it("页签的可见性只认这条接口自己的读权限，不借用相邻页签的控制权", () => {
    // ⛔ `/channel/:id/device-configs` 的读门禁是 `gb28181:ptz:view`（见 routes.go），
    //    写门禁是 `gb28181:ptz:control`。相邻页签用的是 `gb28181:device:control`
    //    （`/device-control` 那条链路）—— 套过来就是"看得见页签、点读取 403"。
    const tab = tabsMarkup.slice(tabsMarkup.indexOf('value="config"') - 80, tabsMarkup.indexOf('value="config"'));
    expect(tab).not.toContain("canUseDeviceControl");
    expect(tab).not.toContain("canControlDevice");
    expect(source).toContain('const canControlChannelConfig = computed(() => hasPermission("gb28181:ptz:control"));');
  });

  it("挂的是 DeviceConfigDrawer 的嵌入形态，且只挂这四组", () => {
    expect(panelStart).toBeGreaterThan(-1);
    expect(panelMarkup).toContain("<DeviceConfigDrawer");
    // `embedded` 必带：不带就是 position:fixed 的全屏遮罩，会把抽屉整个盖住。
    expect(hasMarkup(panelMarkup, "embedded config-only")).toBe(true);
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
    const before = squash(source.slice(panelStart - 160, panelStart));
    expect(before).toContain("v-else-if=\"channelOperationTab==='config'&&canViewDeviceFacts\"");
  });
});
