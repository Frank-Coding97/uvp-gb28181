import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * 设备详情抽屉的页签契约（2026-09-20）。
 *
 * ⛔ 两块事实（DeviceStatus / 存储卡）的入口**只在**这里：播放控制台侧栏「高级」与详情区
 *    2026-09-20 都已摘除。别把它们加回控制台 —— 它们是通道级查询结果，属于"某个设备的详情"。
 * ⛔ 后两页是**通道级**接口（/channel/:id/device-status、/channel/:id/storage-cards），
 *    设备级入口必须带一个"查哪个通道"的选择器，并把两块绑到同一个通道上。
 * ⛔ 通道列表只在用户真的切到后两页时才拉，别在打开抽屉时无条件拉一遍。
 * ⛔ **契约钉不变量，不钉个数**：抽屉里的页签/面板数量会增长（2026-09-20 当天就加了
 *    「设备控制 / 快照配置」两个面板）。按数量写死的断言会被别人的新面板顶红，
 *    而它想表达的"共用同一份通道选择"其实没被破坏 —— 钉「所有绑定都指向同一个状态」即可。
 */
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");
const consoleSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
const drawerSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/DeviceConfigDrawer.vue"), "utf8");

describe("device detail drawer tabs", () => {
  it("把设备详情分成「设备信息 / 设备控制 / 基本参数 / 录像存储 / 报警控制 / 设备状态 / 存储卡」，顺序稳定", () => {
    expect(source).toContain('data-testid="device-detail-tabs"');
    const info = source.indexOf('key="info" title="设备信息"');
    // 2026-09-20 新增：设备录制 / 布撤防 / 报警复位 / 图像抓拍配置从播放控制台搬来这里。
    const control = source.indexOf('key="control" title="设备控制"');
    // 2026-09-20 同日再搬来一个**整页签**：基本参数（basic 组 = A.2.1.19 BasicParam），
    // 来源是播放控制台那页「设备维护」（控制台侧已删）。
    const basic = source.indexOf('key="basic" title="基本参数"');
    // 再搬来两个**整页签**：录像存储（record-plan + alarm-record）
    // 与报警控制（alarm-report），来源是播放控制台的同名页签。
    const record = source.indexOf('key="record" title="录像存储"');
    const alarm = source.indexOf('key="alarm" title="报警控制"');
    const status = source.indexOf('key="status" title="设备状态"');
    const storage = source.indexOf('key="storage" title="存储卡"');
    expect(info).toBeGreaterThan(-1);
    expect(control).toBeGreaterThan(-1);
    expect(basic).toBeGreaterThan(-1);
    expect(record).toBeGreaterThan(-1);
    expect(alarm).toBeGreaterThan(-1);
    expect(status).toBeGreaterThan(-1);
    expect(storage).toBeGreaterThan(-1);
    // 排序口径 = 身份 → 能做什么（控制）→ 能配什么（三页写配置，按"设备自身 → 业务"聚簇，
    // 基本参数是其中最底层的一项，所以排在三页配置之首）→ 报什么（两块只读事实）。
    expect(info).toBeLessThan(control);
    expect(control).toBeLessThan(basic);
    expect(basic).toBeLessThan(record);
    expect(record).toBeLessThan(alarm);
    expect(alarm).toBeLessThan(status);
    expect(status).toBeLessThan(storage);
  });

  it("「基本参数 / 录像存储 / 报警控制」复用配置族抽屉，读写门禁按各自动作的权限分开", () => {
    expect(source).toContain('import DeviceConfigDrawer from "./DeviceConfigDrawer.vue"');
    expect(source).toContain('import FactChannelPicker from "./FactChannelPicker.vue"');
    // ⛔ 内容不重写：仍是 DeviceConfigDrawer 的同一套通道，组清单原样搬来。
    //    在这儿另写一份第二实现，配置族的读写闸门 / 回读对账 / absentTypes 判定迟早与它分岔。
    // ⛔ 基本参数那一页**不传** `config-only`：那个开关的语义是"排除 video-param 组"，
    //    而 `group-keys` 已经把它限定成 basic 了；两个开关同时开着，将来谁往 group-keys 里
    //    加了 video-param 会静默不生效。
    expect(source).toContain(":group-keys=\"['basic']\"");
    expect(source).toContain(":group-keys=\"['record-plan', 'alarm-record']\"");
    expect(source).toContain(":group-keys=\"['alarm-report']\"");
    // ⛔ 读与写不是同一个权限码：读 = gb28181:ptz:view（同 canViewDeviceFacts），
    //    写 = gb28181:ptz:control（种子把 /device-configs 的 POST 绑在它上）。
    //    旧位置（播放控制台）只判了"通道在线"，会把一个点不动的下发按钮摆给没有写权限的账号。
    expect(source).toContain('const canApplyDeviceConfig = computed(() => hasPermission("gb28181:ptz:control"));');
    expect(source).toContain(':can-read="canViewDeviceFacts"');
    expect(source).toContain(':can-apply="canApplyDeviceConfig && deviceConfigChannelOnline"');
    // ⛔ 嵌入的配置抽屉是**定高三段式**（导航 / 参数头 / 参数体各自滚）。
    //    宿主不给确定高度，参数体那行 `minmax(0, 1fr)` 就没有可分配的高度，
    //    长内容（如「周计划」7 天编辑器）会被 .dcg-window 的 overflow:hidden 静默裁掉。
    const configTabRule = source.slice(source.indexOf(".device-config-tab {"));
    expect(configTabRule.slice(0, 400)).toMatch(/height:\s*\d+px;/);
  });

  it("嵌入形态的分组导航横排、按组数等分（不写死列数，也不许退回竖排）", () => {
    // 宿主传几组是它自己的事（控制台 1 组 / 这里 2~3 组），写死列数会让 2 组的导航
    // 只占半排、看上去像坏了。
    expect(drawerSource).not.toMatch(/\.dcg-window--embedded \.dcg-nav \{[\s\S]{0,600}?grid-template-columns: repeat\(4/);
    expect(drawerSource).toContain(".dcg-window--embedded .dcg-nav > .dcg-nav-item {");
    // ⛔ `flex-direction: row` 必须显式写：基类 `.dcg-nav` 是 `flex-direction: column`
    //   （全窗口形态的竖排导航），`display: flex` 会把它继承下来 ⇒ 导航变成竖排
    //   （实测 2 个组各 592px 宽、上下摞着）。`display:grid` 时代它被隐式盖掉，
    //   换成 flex 之后不显式写就会静默退化。
    expect(drawerSource).toMatch(/\.dcg-window--embedded \.dcg-nav \{[\s\S]{0,200}?flex-direction: row;/);
  });

  it("「设备控制」页由 device:control / device:snapshot 任一权限放行，并复用同一份通道选择", () => {
    expect(source).toContain("const canUseDeviceControl = computed(() => canControlDevice.value || canSnapshotDevice.value);");
    expect(source).toContain('<a-tab-pane v-if="canUseDeviceControl" key="control" title="设备控制">');
    expect(source).toContain('import DeviceControlPanel from "./DeviceControlPanel.vue"');
    expect(source).toContain('import SnapshotConfigPanel from "./SnapshotConfigPanel.vue"');
    // 两个面板共用抽屉那一份通道状态（见上一个用例的不变量），页面显示与否按各自权限分开控制。
    expect(source).toContain(':can-control="canControlDevice"');
    expect(source).toContain(':can-snapshot="canSnapshotDevice"');
    // 通道列表的门禁也要跟着放宽：否则"只有 device:control、没有 ptz:view"的账号
    // 会看到一个没有通道可选的空面板（按钮全灰，还不知道为什么）。
    expect(source).toContain("if (!canViewDeviceFacts.value && !canUseDeviceControl.value) return;");
  });

  it("页签落在设备详情抽屉里：表头之后、抽屉按钮之前", () => {
    const drawerStart = source.indexOf("drawerTarget?.type === 'device' && deviceDetail");
    const tabs = source.indexOf('data-testid="device-detail-tabs"');
    // 设备分支的抽屉底栏是文件里最后一个 drawer-foot。
    const footer = source.lastIndexOf('class="drawer-foot"');
    expect(drawerStart).toBeGreaterThan(-1);
    expect(tabs).toBeGreaterThan(drawerStart);
    // 设备名/在线状态这类身份信息不进页签，仍然常驻在页签之上。
    expect(source.indexOf('class="drawer-headline"', drawerStart)).toBeLessThan(tabs);
    expect(footer).toBeGreaterThan(tabs);
  });

  it("所有面板共用同一个通道选择器与同一份通道列表", () => {
    expect(source).toContain('import DeviceStatusFactsPanel from "./DeviceStatusFactsPanel.vue"');
    expect(source).toContain('import StorageCardStatusPanel from "./StorageCardStatusPanel.vue"');
    // ⛔ 别断言「恰好几个」：抽屉里的面板数量会增长（2026-09-20 当天就从 2 加到 4，
    //    新增「设备控制 / 快照配置」两个面板时把这条按数量写死的断言顶红了）。
    //    要钉的是**不变量**：所有面板绑同一份通道状态 —— 各自持一份就会切页签串台。
    const channelIds = [...source.matchAll(/v-model:channel-id="([^"]+)"/g)].map(m => m[1]);
    const channelOptions = [...source.matchAll(/:channel-options="([^"]+)"/g)].map(m => m[1]);
    expect(channelIds.length).toBeGreaterThanOrEqual(2);
    expect(channelIds.length).toBe(channelOptions.length);
    expect(new Set(channelIds)).toEqual(new Set(["deviceFactChannelId"]));
    expect(new Set(channelOptions)).toEqual(new Set(["deviceFactChannelOptions"]));
    // 「本页是否显示」由页签决定，面板据此决定读不读缓存。
    expect(source).toContain(":active=\"deviceDrawerTab === 'status'\"");
    expect(source).toContain(":active=\"deviceDrawerTab === 'storage'\"");
  });

  it("事实页签按 ptz:view 放行，不按 device:view", () => {
    expect(source).toContain('const canViewDeviceFacts = computed(() => hasPermission("gb28181:ptz:view"));');
    // 同上：钉不变量（受它门禁的页签至少两个），不钉具体个数。
    expect([...source.matchAll(/v-if="canViewDeviceFacts"/g)].length).toBeGreaterThanOrEqual(2);
    expect(source).not.toMatch(/hasPermission\("gb28181:device:view"\)\);\s*[\r\n]+const canViewDeviceFacts/);
  });

  it("通道列表只在切到后两页时才拉，且切设备后丢弃在途结果", () => {
    expect(source).toContain("async function ensureDeviceFactChannels()");
    expect(source).toContain("listChannels({ deviceId: deviceCode, page: 1, pageSize: 200 })");
    expect(source).toContain("watch(deviceDrawerTab, tab => {");
    expect(source).toContain("function deviceFactChannelsAbandoned(deviceCode: string)");
    // 打开抽屉只重置状态，不拉通道 —— 从不点这两页的人不该为此多一次请求。
    const openDevice = source.slice(source.indexOf("async function openDevice"), source.indexOf("function resetDeviceFactState"));
    expect(openDevice).toContain("resetDeviceFactState();");
    expect(openDevice).not.toContain("listChannels(");
    expect(source).toContain('deviceDrawerTab.value = "info";');
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
    // ⛔ 这里**不要**再钉 `data-testid="snapshot-config"` 之类控制台侧栏的元素：
    //    那块（快照配置）2026-09-20 当天也被搬进设备抽屉（`SnapshotConfigPanel.vue`），
    //    由别的改动负责，钉它只会让本文件被别人的搬迁顶红。本用例只管"事实入口别回来"。
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
    // 反向：这几页的组件在设备侧确实是同一个配置族抽屉。
    expect(source).toContain('data-testid="device-config-basic"');
    expect(source).toContain('data-testid="device-config-record"');
    expect(source).toContain('data-testid="device-config-alarm"');
  });
});
