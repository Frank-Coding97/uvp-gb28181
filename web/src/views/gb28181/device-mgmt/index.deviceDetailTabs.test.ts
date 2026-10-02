import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup } from "@/test/source-assert";

/**
 * 设备详情抽屉的**只读契约**。
 *
 * ⛔ 为什么改：原七页签按**接口族**分类（设备信息 / 设备控制 / 基本参数 / 录像存储 /
 *    报警控制 / 设备状态 / 存储卡），三个配置页占了 4/7 的宽度，而它们是使用频率最低的一支。
 *    实测页签一行排满只余 22px（x=992 起、步长 88、最右 1562 vs 右界 1584），
 *    且多通道设备 DOM 里 `.fact-channel-picker` 有 **7 份**（每页一份同一状态）。
 *    设备详情只展示概览事实；通道级操作在独立的通道操作抽屉中完成。
 *
 * ⛔ 两块事实（DeviceStatus / 存储卡）的入口**只在**这里：播放控制台侧栏「高级」与详情区
 *    2026-09-20 都已摘除。别把它们加回控制台。
 * ⛔ 通道级内容（操作段四个面板 + 配置段）由宿主渲染**一个**通道选择器，四个面板一律
 *    `show-picker=false` —— 各自持一份就会切段串台，也是 P0-4 的成因。
 * ⛔ **契约钉不变量，不钉个数**：段内的面板会增减（2026-09-20 当天就加了
 *    「设备控制 / 快照配置」两个面板）。要钉的是"所有面板绑同一份通道状态"。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");
const consoleSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
const drawerSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");

/** 设备分支那一整块（从 drawer-body 到 `</a-drawer>`）—— 段结构断言都限定在这里判。 */
const branchStart = source.indexOf("drawerTarget?.type === 'device' && deviceDetail");
const branchEnd = source.indexOf("</a-drawer>", branchStart);
const deviceBranch = source.slice(branchStart, branchEnd);

/**
 * 去掉模板注释（`<!-- … -->`）。
 * ⛔ 「**不许出现** X」这类断言必须跑在去注释的文本上，否则会把注释里那句叮嘱本身当成违规
 *    —— 本仓已经踩过两次：这里写着"别再自己写 `<button>`"，被 `not.toContain("<button")` 顶红。
 *    正着断（"必须有 X"）用原文本即可，但用去注释版更保险（注释里提到某个标签不能算它真的在
 *    渲染）。所以本文件一律用去注释后的文本。
 */
const stripComments = (text: string) => text.replace(/<!--[\s\S]*?-->/g, "");
const deviceMarkup = stripComments(deviceBranch);

describe("device detail drawer segments", () => {
  it("设备相关抽屉统一接入主题容器，暗色模式使用项目抽屉色板", () => {
    expect(source).toContain('v-model:visible="drawerVisible"');
    expect(source).toContain('v-model:visible="channelOperationVisible"');
    expect(source.match(/class="uvp-system-drawer"/g)?.length).toBe(2);
    expect(source.match(/body-class="uvp-system-dialog__body"/g)?.length).toBe(2);
  });

  it("设备详情使用响应式宽度，通道详情保留更窄的共享抽屉宽度", () => {
    expect(source).toContain("const drawerWidth = computed(() =>");
    expect(source).toContain('drawerTarget.value?.type === "device" ? "min(760px, 92vw)" : "min(640px, 92vw)"');
    expect(source).toContain(':width="drawerWidth"');
  });

  it("窄屏时档案条身份信息保持双列布局", () => {
    const idbarMedia = source.match(
      /@media \(width <= 720px\) \{[\s\S]{0,900}?\.device-idbar \{[\s\S]{0,220}?grid-template-columns: 36px minmax\(0, 1fr\);/
    );
    expect(idbarMedia).not.toBeNull();
    expect(source).not.toContain(".device-idbar .idbar-acts");
  });

  it("只读详情的文字和在线率使用清晰的系统节奏", () => {
    expect(deviceMarkup).toContain('class="idbar-status"');
    expect(deviceMarkup).toContain('status="success"');
    expect(source).toContain(".device-readonly {");
    expect(source).toContain("gap: 18px;");
    expect(source).toContain("font-size: 17px;");
    expect(source).toContain("width: 160px;");
    expect(source).toContain("height: 6px;");
    expect(source).toContain("background: var(--uvp-success);");
  });

  it("设备详情只保留纯只读概览", () => {
    expect(deviceBranch.length).toBeGreaterThan(0);
    expect(deviceBranch).not.toContain('data-testid="device-detail-segs"');
    expect(deviceBranch).toContain('data-seg-panel="overview"');
    expect(deviceBranch).not.toContain('data-seg-panel="operate"');
    expect(deviceBranch).not.toContain('data-seg-panel="config"');
    expect(deviceBranch).not.toContain("device-entrypoints");
    expect(deviceBranch).not.toContain("showDeviceChannels(deviceDetail)");
    expect(deviceBranch).not.toContain("openDeviceRecordPlan(deviceDetail)");
    expect(deviceBranch).not.toContain("刷新目录");
    expect(deviceBranch).not.toContain("管理订阅");
    expect(deviceBranch).toContain("设备地址");
    expect(deviceBranch).toContain("归属组织");
    expect(deviceBranch).toContain("节点策略");
    // ⛔ 身份信息不进段，常驻在段导航之上（档案条）
    expect(deviceBranch.indexOf('class="device-idbar"')).toBeLessThan(deviceBranch.indexOf('data-seg-panel="overview"'));
  });

  it("设备详情不提供任何操作入口", () => {
    // ⛔ 原页脚 6 个按钮在 640 宽抽屉里需要约 700px（可用 608px），实测默认落在视口外
    //    （内容 948 > 可视 850）—— 这正是"用户不知道有这些功能"的直接成因。
    //    「通道详情」那一支还留着页脚，所以文件里 `drawer-foot` 仍在，但必须在设备分支之前。
    const foot = source.lastIndexOf('class="drawer-foot"');
    expect(foot).toBeGreaterThan(-1);
    expect(foot).toBeLessThan(branchStart);
    expect(deviceMarkup).not.toContain('class="drawer-foot"');
    expect(deviceMarkup).not.toContain("openDeviceReboot(deviceDetail)");
    expect(deviceMarkup).not.toContain("openDeviceUpgrade(deviceDetail)");
    expect(deviceMarkup).not.toContain("openMaintenanceRecords(deviceDetail)");
    expect(deviceMarkup).not.toContain("handleRefreshDeviceCatalog(deviceDetail)");
    expect(deviceMarkup).not.toContain("openSubscriptionManager(deviceDetail)");
    expect(deviceMarkup).not.toContain("复制编码");
  });

  it("设备分支一律用框架控件，不许自造原生控件", () => {
    // ⛔ 口径：Arco 有这个组件就用它。本仓「分段切换 / 进度条 / 在线状态点」的既有写法分别是
    //    `a-radio-group type="button"`（`ShareDrawer.vue:234`、`cascade/index.vue:195`）、
    //    `a-progress`（`RecordingDownloadCenter.vue:36`）、`a-badge :status`（`RecordingPlansPanel.vue:65`）。
    //    自己拿原生 `<button>` / `div` 画一遍，丢掉的是 focus 环、键盘可达性、禁用态，
    //    以及"它跟别处的同类控件长得一样"这条一致性。
    expect(deviceMarkup).not.toMatch(/<button[\s>]/);
    expect(deviceMarkup).not.toContain('role="tab"');
    // 状态点 = `a-badge`（点色与文案都由框架渲染，离线态不用自己维护一个颜色分支）
    expect(hasMarkup(deviceMarkup, `<a-badge class="seg-diag" :status="deviceDetail.online ? 'success' : 'normal'"`)).toBe(true);
    // 进度条 = `a-progress`。⛔ Arco 的 `percent` 是 0~1，不是百分数
    expect(deviceMarkup).toContain("<a-progress");
    expect(deviceMarkup).toContain(':percent="onlineRatePercent(deviceDetail.onlineRate) / 100"');
    expect(deviceMarkup).toContain('status="success"');
    expect(deviceMarkup).not.toContain("<a-button");
    // ⛔ 自造的绘制痕迹一条都不许留：留一个就是"下次照着它再加原生控件"的样板
    expect(source).not.toContain(".seg-bar {");
    expect(source).not.toContain(".device-segs button {");
  });

  it("嵌入形态的分组导航横排、按组数等分（不写死列数，也不许退回竖排）", () => {
    // 宿主传几组是它自己的事（控制台 1 组 / 这里 4 组），写死列数会让 2 组的导航
    // 只占半排、看上去像坏了。
    expect(drawerSource).not.toMatch(/\.dcg-window--embedded \.dcg-nav \{[\s\S]{0,600}?grid-template-columns: repeat\(4/);
    expect(drawerSource).toContain(".dcg-window--embedded .dcg-nav > .dcg-nav-item {");
    // ⛔ `flex-direction: row` 必须显式写：基类 `.dcg-nav` 是 `flex-direction: column`
    //   （全窗口形态的竖排导航），`display: flex` 会把它继承下来 ⇒ 导航变成竖排
    //   （实测 2 个组各 592px 宽、上下摞着）。`display:grid` 时代它被隐式盖掉，
    //   换成 flex 之后不显式写就会静默退化。
    expect(drawerSource).toMatch(/\.dcg-window--embedded \.dcg-nav \{[\s\S]{0,200}?flex-direction: row;/);
  });

  it("通道操作抽屉按当前通道复用控制、抓拍、状态和存储面板", () => {
    expect(source).toContain('import DeviceControlPanel from "./DeviceControlPanel.vue"');
    expect(source).toContain('import SnapshotConfigPanel from "./SnapshotConfigPanel.vue"');
    expect(source).toContain('import DeviceStatusFactsPanel from "./DeviceStatusFactsPanel.vue"');
    expect(source).toContain('import StorageCardStatusPanel from "./StorageCardStatusPanel.vue"');
    expect(source).toContain('v-model:channel-id="channelOperationTarget.id"');
    expect(source).toContain(':channel-options="channelOperationOptions"');
    expect(source).toContain('class="channel-operation-tabs"');
    expect(source).toContain('value="control"');
    expect(source).toContain('value="snapshot"');
    expect(source).toContain('value="status"');
    expect(source).toContain('value="storage"');
    expect(source).toContain("const channelOperationOptions = computed");
    expect(source).toContain("openChannelOperations(record)");
    // 面板组件本身要认这个开关（否则传了不生效，选择器照旧各渲染一份）
    for (const file of [
      "DeviceControlPanel.vue",
      "SnapshotConfigPanel.vue",
      "DeviceStatusFactsPanel.vue",
      "StorageCardStatusPanel.vue"
    ]) {
      const panel = readFileSync(resolve(process.cwd(), `src/views/gb28181/device-mgmt/${file}`), "utf8");
      expect(panel, file).toContain("showPicker?: boolean;");
      expect(panel, file).toContain('v-if="props.showPicker !== false"');
    }
    expect(source).toContain(':active="channelOperationVisible"');
  });

  it("通道操作由 device:control / device:snapshot 任一权限放行", () => {
    expect(source).toContain("const canUseDeviceControl = computed(() => canControlDevice.value || canSnapshotDevice.value);");
    expect(source).toContain(':can-control="canControlDevice"');
    expect(source).toContain(':can-snapshot="canSnapshotDevice"');
    expect(source).toContain('v-if="canUseDeviceControl || canSnapshotDevice || canViewDeviceFacts"');
  });

  it("事实面板按 ptz:view 放行，不按 device:view", () => {
    expect(source).toContain('const canViewDeviceFacts = computed(() => hasPermission("gb28181:ptz:view"));');
    // ⛔ 读与写不是同一个权限码：读 = ptz:view，写 = ptz:control（种子把 /device-configs
    //    的 POST 绑在它上）。旧位置（播放控制台）只判了"通道在线"，
    //    会把一个点不动的下发按钮摆给没有写权限的账号。
    expect(source).not.toContain("canApplyDeviceConfig");
    expect(source).not.toMatch(/hasPermission\("gb28181:device:view"\)\);\s*[\r\n]+const canViewDeviceFacts/);
  });

  it("播放控制台侧不再保留这两个入口（含详情区页签机制）", () => {
    expect(consoleSource).not.toContain("linked-detail-tabs");
    expect(consoleSource).not.toContain("detailTabsByTab");
    expect(consoleSource).not.toContain("advanced-fact-status");
    expect(consoleSource).not.toContain("storage-card-status");
    // 连组件与状态都不要反向 import 回来（"入口"最容易以这种方式复活）。
    expect(consoleSource).not.toContain("DeviceStatusFactsPanel");
    expect(consoleSource).not.toContain("StorageCardStatusPanel");
    expect(consoleSource).not.toContain("deviceFactChannelId");
  });

  it("「基本参数 / 录像存储 / 报警控制」在控制台侧连页签和组清单一起摘掉", () => {
    // ⛔ 只删页签、把组留在 configWorkspaceGroups 里，会得到一批"永远没有入口的配置组"：
    //    切不过去，但读取请求照发（读取问的是 CONFIG_GROUPS 的全量并集）。
    // ⛔ 组清单那几条**限定在 configWorkspaceGroups 那段对象字面量里**判：
    //    注释里会（也应该）出现 `device: ["basic"]` 这类"别再这样写"的叮嘱，
    //    按全文件 `not.toContain` 去钉，会把叮嘱本身当成违规（本次就被顶红过一次）。
    const groupsBlock = consoleSource.slice(
      consoleSource.indexOf("const configWorkspaceGroups"),
      consoleSource.indexOf("const activeConfigGroups")
    );
    expect(groupsBlock.length).toBeGreaterThan(0);
    expect(groupsBlock).not.toContain("record:");
    expect(groupsBlock).not.toContain("alarm:");
    expect(groupsBlock).not.toContain("device:");
    // 页签项也一并摘掉（这几条字面量不会出现在注释里，整文件判是安全的）。
    expect(consoleSource).not.toContain('{ key: "record"');
    expect(consoleSource).not.toContain('{ key: "alarm"');
    expect(consoleSource).not.toContain('{ key: "device"');
    // ⛔ 页签联合类型也钉死 —— 留一个孤立的 `"device"` 在 TabKey 里，
    //    下次有人照着它加回页签只差复制一行。
    expect(consoleSource).toContain('type TabKey = "ptz" | "probe" | "deviceconfig";');
    // 页签走了，给它们用的图标也不该以 import 形式留下（vue-tsc TS6133）。
    // ⛔ `Settings` 不在其列：云台侧栏的「高级设置」与看守位「修改设置」还在用它。
    expect(consoleSource).not.toContain("HardDrive");
  });
});
