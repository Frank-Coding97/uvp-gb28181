import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup } from "@/test/source-assert";

/**
 * 设备详情抽屉的**段契约**（2026-09-24 由「七页签」改为「三段」）。
 *
 * ⛔ 为什么改：原七页签按**接口族**分类（设备信息 / 设备控制 / 基本参数 / 录像存储 /
 *    报警控制 / 设备状态 / 存储卡），三个配置页占了 4/7 的宽度，而它们是使用频率最低的一支。
 *    实测页签一行排满只余 22px（x=992 起、步长 88、最右 1562 vs 右界 1584），
 *    且多通道设备 DOM 里 `.fact-channel-picker` 有 **7 份**（每页一份同一状态）。
 *    新口径按**行为形状**分三段：概览=只读事实 / 操作=点一下发一条+判读依据 /
 *    配置=读·改·下发·对账。设计文档：`docs/device-detail-drawer-design.md`。
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
  it("设备详情使用响应式宽度，通道详情保留更窄的共享抽屉宽度", () => {
    expect(source).toContain("const drawerWidth = computed(() =>");
    expect(source).toContain('drawerTarget.value?.type === "device" ? "min(760px, 92vw)" : "min(640px, 92vw)"');
    expect(source).toContain(':width="drawerWidth"');
  });

  it("窄屏时档案条动作独占第二行，避免挤压设备身份", () => {
    const idbarMedia = source.match(
      /@media \(width <= 720px\) \{[\s\S]{0,900}?\.device-idbar \{[\s\S]{0,220}?grid-template-columns: 36px minmax\(0, 1fr\);/
    );
    expect(idbarMedia).not.toBeNull();
    expect(source).toContain(".device-idbar .idbar-acts");
    expect(source).toContain("grid-column: 2;");
    expect(source).toContain("justify-content: flex-start;");
  });

  it("段导航复用系统的圆角分段视觉，而不是默认灰色整条横栏", () => {
    const segRule = source.slice(source.indexOf(".device-segs {"), source.indexOf(".device-segs :deep(.arco-radio-button) {"));
    expect(segRule).toContain("gap: 4px;");
    expect(segRule).toContain("padding: 4px;");
    expect(segRule).toContain("width: fit-content;");
    expect(segRule).toContain("max-width: 100%;");
    expect(segRule).toContain("background: #f3f7fb;");
    expect(segRule).toContain("border-radius: 12px;");
    expect(source).toContain(".device-segs :deep(.arco-radio-button.arco-radio-checked)");
    expect(source).toContain("box-shadow: 0 6px 16px rgb(37 99 235 / 10%);");
    expect(source).toContain("min-height: 30px;");
    expect(source).toContain("min-width: 92px;");
    expect(source).toContain(".device-segs :deep(.arco-radio-button-content)");
  });

  it("档案条动作、文字和在线率使用清晰的系统节奏", () => {
    expect(deviceMarkup).toContain('class="idbar-action"');
    expect(deviceMarkup).toContain('type="outline"');
    expect(deviceMarkup).toContain('status="success"');
    expect(source).toContain(".idbar-action {");
    expect(source).toContain("height: 32px;");
    expect(source).toContain("width: 160px;");
    expect(source).toContain("height: 6px;");
    expect(source).toContain("background: var(--uvp-success);");
  });

  it("配置段高度随视口收缩，不在小屏制造抽屉外溢", () => {
    const configTabRule = source.slice(
      source.indexOf(".device-config-tab {"),
      source.indexOf(".device-config-tab :deep", source.indexOf(".device-config-tab {"))
    );
    expect(configTabRule).toContain("height: clamp(420px, calc(100vh - 250px), 560px);");
  });

  it("设备分支分成「概览 / 操作 / 配置」三段，顺序稳定", () => {
    expect(deviceBranch.length).toBeGreaterThan(0);
    expect(deviceBranch).toContain('data-testid="device-detail-segs"');
    // 段清单（顺序即口径：只读事实 → 能做什么 → 能配什么）
    const overview = source.indexOf('value: "overview"');
    const operate = source.indexOf('value: "operate"');
    const config = source.indexOf('value: "config"');
    expect(overview).toBeGreaterThan(-1);
    expect(operate).toBeGreaterThan(overview);
    expect(config).toBeGreaterThan(operate);
    // 三个段面板本身也在（v-show 切换），顺序同上
    const p1 = deviceBranch.indexOf('data-seg-panel="overview"');
    const p2 = deviceBranch.indexOf('data-seg-panel="operate"');
    const p3 = deviceBranch.indexOf('data-seg-panel="config"');
    expect(p1).toBeGreaterThan(-1);
    expect(p2).toBeGreaterThan(p1);
    expect(p3).toBeGreaterThan(p2);
    // ⛔ 身份信息不进段，常驻在段导航之上（档案条）
    expect(deviceBranch.indexOf('class="device-idbar"')).toBeLessThan(p1);
  });

  it("设备分支**没有页脚**：设备级动作在档案条上就近落位", () => {
    // ⛔ 原页脚 6 个按钮在 640 宽抽屉里需要约 700px（可用 608px），实测默认落在视口外
    //    （内容 948 > 可视 850）—— 这正是"用户不知道有这些功能"的直接成因。
    //    「通道详情」那一支还留着页脚，所以文件里 `drawer-foot` 仍在，但必须在设备分支之前。
    const foot = source.lastIndexOf('class="drawer-foot"');
    expect(foot).toBeGreaterThan(-1);
    expect(foot).toBeLessThan(branchStart);
    expect(deviceMarkup).not.toContain('class="drawer-foot"');
    // 档案条 = 身份 + 三个设备级动作（重启 / 升级 / 维护记录）
    expect(hasMarkup(deviceMarkup, '<div class="idbar-acts">')).toBe(true);
    expect(deviceMarkup).toContain("openDeviceReboot(deviceDetail)");
    expect(deviceMarkup).toContain("openDeviceUpgrade(deviceDetail)");
    expect(deviceMarkup).toContain("openMaintenanceRecords(deviceDetail)");
    // 「维护记录」在**视觉层级**上是文本链接（它只"看历史"，与"会改动设备"风险等级不同），
    // 但控件是框架的 `a-button type="text"` —— 不是自造的原生 `<button>`。
    expect(hasMarkup(deviceMarkup, '<a-button class="idbar-link" type="text" size="mini"')).toBe(true);
    // 另外两个动作落在各自区块的标题行上：刷新目录刷的是通道列表，管理订阅管的是订阅
    expect(deviceMarkup).toContain("handleRefreshDeviceCatalog(deviceDetail)");
    expect(deviceMarkup).toContain("openSubscriptionManager(deviceDetail)");
    // ⛔ 「复制编码」页脚按钮已删：档案条上的编码旁边本来就有复制按钮（同一功能，不重复）
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
    // 段导航 = 框架的分段单选组。
    // ⛔ 走 `:model-value` + `@change` 而不是 `v-model`：Arco RadioGroup 的值类型是
    //    `string | number | boolean`，直接 `v-model` 到 `Ref<DeviceSeg>` 过不了 strict 模板检查。
    expect(hasMarkup(deviceMarkup, '<a-radio-group :model-value="deviceDrawerSeg" class="device-segs" type="button"')).toBe(true);
    expect(hasMarkup(deviceMarkup, "deviceDrawerSeg = value as DeviceSeg")).toBe(true);
    expect(hasMarkup(deviceMarkup, '<a-radio v-for="seg in deviceSegOptions" :key="seg.value" :value="seg.value">')).toBe(true);
    // 状态点 = `a-badge`（点色与文案都由框架渲染，离线态不用自己维护一个颜色分支）
    expect(hasMarkup(deviceMarkup, `<a-badge class="seg-diag" :status="deviceDetail.online ? 'success' : 'normal'"`)).toBe(true);
    // 进度条 = `a-progress`。⛔ Arco 的 `percent` 是 0~1，不是百分数
    expect(deviceMarkup).toContain("<a-progress");
    expect(deviceMarkup).toContain(':percent="onlineRatePercent(deviceDetail.onlineRate) / 100"');
    expect(deviceMarkup).toContain('status="success"');
    // 复制按钮也是框架按钮（同一个抽屉「通道详情」那一支用的就是 `a-button`）
    expect(hasMarkup(deviceMarkup, '<a-button class="copy-btn" type="text" size="mini"')).toBe(true);
    // ⛔ 自造的绘制痕迹一条都不许留：留一个就是"下次照着它再加原生控件"的样板
    expect(source).not.toContain(".seg-bar {");
    expect(source).not.toContain(".device-segs button {");
  });

  it("配置段只剩**一个** DeviceConfigDrawer 实例，四组带齐且不传 config-only", () => {
    expect(deviceBranch.split("<DeviceConfigDrawer").length - 1).toBe(1);
    expect(hasMarkup(deviceBranch, ":group-keys=\"['basic', 'record-plan', 'alarm-record', 'alarm-report']\"")).toBe(true);
    // ⛔ `config-only` 的语义是"排除 video-param 组"，而 `group-keys` 已把四组全部限定；
    //    两个开关同时开着，将来谁往 group-keys 里加了 video-param 会**静默不生效**
    //    （`DeviceConfigDrawer.vue:210`）。判据只取这一个标签，避免被注释里的叮嘱顶红。
    const tagStart = deviceBranch.indexOf("<DeviceConfigDrawer");
    const tag = deviceBranch.slice(tagStart, deviceBranch.indexOf("/>", tagStart));
    expect(tag).not.toContain("config-only");
    // 组导航必须受控：宿主切段/换设备时要能把它重置回 basic
    expect(tag).toContain('v-model:active-group-key="deviceConfigGroupKey"');
    expect(source).toContain('const deviceConfigGroupKey = ref<string>("basic");');
    // ⛔ 嵌入的配置抽屉是**定高三段式**（导航 / 参数头 / 参数体各自滚）。
    //    宿主不给确定高度，参数体那行 `minmax(0, 1fr)` 就没有可分配的高度，
    //    长内容（如「周计划」7 天编辑器）会被 .dcg-window 的 overflow:hidden 静默裁掉。
    const configTabRule = source.slice(source.indexOf(".device-config-tab {"));
    expect(configTabRule.slice(0, 400)).toMatch(/height:\s*(?:\d+px|clamp\([^;]+\));/);
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

  it("「操作」段的四个面板共用宿主的**一个**通道选择器", () => {
    expect(source).toContain('import DeviceControlPanel from "./DeviceControlPanel.vue"');
    expect(source).toContain('import SnapshotConfigPanel from "./SnapshotConfigPanel.vue"');
    expect(source).toContain('import DeviceStatusFactsPanel from "./DeviceStatusFactsPanel.vue"');
    expect(source).toContain('import StorageCardStatusPanel from "./StorageCardStatusPanel.vue"');
    expect(source).toContain('import FactChannelPicker from "./FactChannelPicker.vue"');
    // ⛔ 别断言「恰好几个面板」：要钉的是**不变量** —— 所有面板绑同一份通道状态。
    const channelIds = [...source.matchAll(/v-model:channel-id="([^"]+)"/g)].map(m => m[1]);
    const channelOptions = [...source.matchAll(/:channel-options="([^"]+)"/g)].map(m => m[1]);
    expect(channelIds.length).toBeGreaterThanOrEqual(2);
    expect(channelIds.length).toBe(channelOptions.length);
    expect(new Set(channelIds)).toEqual(new Set(["deviceFactChannelId"]));
    expect(new Set(channelOptions)).toEqual(new Set(["deviceFactChannelOptions"]));
    // ⛔ 面板一律不自渲染选择器：宿主已渲染一个（`showPicker` 默认 true，宿主传 false）。
    //    2026-09-24 之前每页一份，多通道设备 DOM 里 `.fact-channel-picker` 实测 7 份。
    // ⛔ 判据是「**宿主恰好渲染一个** + 面板全在它之后」，不是「两者相隔多少字符」——
    //    宿主的选择器与第一个面板本来就是**同级相邻**，拿字符距离当"嵌套"的代理会
    //    把正常结构判成违规（这条断言曾写成 `<FactChannelPicker[\s\S]{0,400}?<DeviceControlPanel`
    //    的 not.toMatch，正则会命中相邻同级，是一条永远不会绿的断言）。
    const operateSlice = stripComments(
      deviceBranch.slice(deviceBranch.indexOf('data-seg-panel="operate"'), deviceBranch.indexOf('data-seg-panel="config"'))
    );
    expect([...operateSlice.matchAll(/<FactChannelPicker/g)].length).toBe(1);
    expect(operateSlice.indexOf("<FactChannelPicker")).toBeLessThan(operateSlice.indexOf("<DeviceControlPanel"));
    expect([...operateSlice.matchAll(/:show-picker="false"/g)].length).toBeGreaterThanOrEqual(3);
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
    // 「本段是否显示」由段决定，面板据此决定读不读缓存。
    expect(source).toContain(":active=\"deviceDrawerSeg === 'operate'\"");
  });

  it("「操作」段由 device:control / device:snapshot 任一权限放行", () => {
    expect(source).toContain("const canUseDeviceControl = computed(() => canControlDevice.value || canSnapshotDevice.value);");
    expect(source).toContain(':can-control="canControlDevice"');
    expect(source).toContain(':can-snapshot="canSnapshotDevice"');
    // 通道列表的门禁也要跟着放宽：否则"只有 device:control、没有 ptz:view"的账号
    // 会看到一个没有通道可选的空面板（按钮全灰，还不知道为什么）。
    expect(source).toContain("if (!canViewDeviceFacts.value && !canUseDeviceControl.value) return;");
  });

  it("事实面板按 ptz:view 放行，不按 device:view", () => {
    expect(source).toContain('const canViewDeviceFacts = computed(() => hasPermission("gb28181:ptz:view"));');
    // ⛔ 读与写不是同一个权限码：读 = ptz:view，写 = ptz:control（种子把 /device-configs
    //    的 POST 绑在它上）。旧位置（播放控制台）只判了"通道在线"，
    //    会把一个点不动的下发按钮摆给没有写权限的账号。
    expect(source).toContain('const canApplyDeviceConfig = computed(() => hasPermission("gb28181:ptz:control"));');
    expect(source).toContain(':can-apply="canApplyDeviceConfig && deviceConfigChannelOnline"');
    expect(source).not.toMatch(/hasPermission\("gb28181:device:view"\)\);\s*[\r\n]+const canViewDeviceFacts/);
  });

  it("通道列表只在切到「操作 / 配置」时才拉，且切设备后丢弃在途结果", () => {
    expect(source).toContain("async function ensureDeviceFactChannels()");
    expect(source).toContain("listChannels({ deviceId: deviceCode, page: 1, pageSize: 200 })");
    expect(source).toContain("watch(deviceDrawerSeg, seg => {");
    // 概览是"打开即答"的只读段，不需要通道列表
    expect(source).toContain('if (seg === "overview") return;');
    expect(source).toContain("function deviceFactChannelsAbandoned(deviceCode: string)");
    // 打开抽屉只重置状态，不拉通道 —— 从不点这两段的人不该为此多一次请求。
    const openDevice = source.slice(source.indexOf("async function openDevice"), source.indexOf("function resetDeviceFactState"));
    expect(openDevice).toContain("resetDeviceFactState();");
    expect(openDevice).not.toContain("listChannels(");
    expect(source).toContain('deviceDrawerSeg.value = "overview";');
    // 换设备时组导航也要跟着回到第一组，否则上一台停在「报警上报」会带过来
    expect(source).toContain('deviceConfigGroupKey.value = "basic";');
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
    // 反向：这几页的配置在设备侧确实是**同一个**配置族抽屉实例（四组一起给）。
    expect(deviceBranch.split("<DeviceConfigDrawer").length - 1).toBe(1);
  });
});
