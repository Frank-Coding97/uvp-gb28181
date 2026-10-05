import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { hasMarkup, squash } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

describe("device management toolbar layout", () => {
  it("opens the shared playback console instead of mounting a route-local player", () => {
    expect(source).toContain('import { usePlaybackConsoleStore } from "@/store/modules/playback-console"');
    expect(source).toContain("playbackConsole.open(record);");
    expect(source).not.toContain("<PlayConsoleLinked");
  });

  it("uses one workspace-wide toolbar before the catalog and content panes", () => {
    const toolbarIndex = source.indexOf('class="workspace-toolbar"');
    const catalogIndex = source.indexOf('class="catalog-pane"');
    const contentIndex = source.indexOf('class="content-pane"');

    expect(toolbarIndex).toBeGreaterThan(-1);
    expect(toolbarIndex).toBeLessThan(catalogIndex);
    expect(toolbarIndex).toBeLessThan(contentIndex);
    expect(source).not.toContain('class="device-stats-zone"');
    expect(source).not.toContain('class="snow-fill-inner device-mgmt-page"');
    expect(source).not.toContain('class="content-topbar"');
    expect(source).not.toContain('class="main-head"');
  });

  it("removes unused controls and names the view buttons", () => {
    expect(source).not.toContain("<SlidersHorizontal");
    expect(source).not.toContain("<Settings2");
    expect(source).not.toContain("<Download");
    expect(source).toContain(':aria-label="`${item.label}视图`"');
    expect(source).toContain(':aria-pressed="viewMode === item.value"');
  });

  it("keeps status beside the right-aligned action group", () => {
    const statusIndex = source.indexOf('class="device-stats"');
    const actionsIndex = source.indexOf('class="toolbar-actions"');

    expect(statusIndex).toBeGreaterThan(-1);
    expect(actionsIndex).toBeGreaterThan(-1);
    expect(statusIndex).toBeLessThan(actionsIndex);
    for (const control of [
      'class="cmdk"',
      'class="view-switch"',
      "create-device-btn",
      "refresh-control",
      "status-select",
      "asset-kind-switch"
    ]) {
      expect(source.indexOf(control)).toBeGreaterThan(actionsIndex);
    }
    expect(source).toContain(".workspace-toolbar :deep(.uvp-search-panel__fields)");
    expect(source).toContain("flex: 1 1 100%;");
  });

  it("orders toolbar controls by usage frequency", () => {
    const controls = [
      "asset-kind-switch",
      'class="view-switch"',
      'class="cmdk"',
      "status-select",
      "refresh-control",
      "create-device-btn"
    ];
    const positions = controls.map(control => source.indexOf(control));

    expect(positions.every(position => position > -1)).toBe(true);
    expect(positions).toEqual([...positions].sort((left, right) => left - right));
    expect(source).not.toContain("order: 10;");
  });

  it("uses the outer page shell as the only workspace surface", () => {
    const workspaceRule = source.match(/\.workspace\s*\{([^}]*)\}/)?.[1] || "";
    const toolbarSurfaceRule = source.match(/\.workspace-toolbar :deep\(\.uvp-search-panel__surface\)\s*\{([^}]*)\}/)?.[1] || "";
    const paneRule = source.match(/\.catalog-pane,\s*\.content-pane\s*\{([^}]*)\}/)?.[1] || "";
    const catalogRule = source.match(/\.catalog-pane\s*\{([^}]*)\}/)?.[1] || "";

    expect(workspaceRule).toContain("gap: 0;");
    expect(toolbarSurfaceRule).toContain("background: transparent;");
    expect(toolbarSurfaceRule).toContain("border-radius: 0;");
    expect(toolbarSurfaceRule).toContain("box-shadow: none;");
    expect(paneRule).not.toContain("background:");
    expect(paneRule).not.toContain("border-radius:");
    expect(paneRule).not.toContain("box-shadow:");
    expect(catalogRule).toContain("border-right: 1px solid var(--uvp-panel-border);");
  });

  it("keeps a compact right-aligned device card action strip", () => {
    const cardRule = source.match(/\.device-summary-card\s*\{([^}]*)\}/)?.[1] || "";
    const actionRule = source.match(/\.device-card-actions\s*\{([^}]*)\}/)?.[1] || "";
    const buttonRule = source.match(/\.device-card-actions \.icon-btn\.small\s*\{([^}]*)\}/)?.[1] || "";

    expect(cardRule).toContain("grid-template-rows: auto auto max-content;");
    expect(cardRule).toContain("padding: 12px 12px 6px;");
    expect(actionRule).toContain("justify-content: flex-end;");
    expect(actionRule).toContain("gap: 6px;");
    expect(actionRule).toContain("padding-top: 3px;");
    expect(buttonRule).toContain("width: 28px;");
    expect(buttonRule).toContain("height: 28px;");
  });

  it("shows the GB protocol version on both the device table and the device card", () => {
    const flat = squash(source);

    // 列表：新增「国标版本」列，夹在「型号 / 版本」与「注册时间」之间
    const columns = ['title="型号/版本"', 'title="国标版本"', 'title="注册时间"'].map(anchor => flat.indexOf(anchor));
    expect(columns.every(index => index > -1)).toBe(true);
    expect(columns).toEqual([...columns].sort((left, right) => left - right));

    // 卡片：同一字段夹在「型号」与「地址」之间
    const cardModel = flat.indexOf("<span>型号</span>");
    const cardVersion = flat.indexOf("<span>国标版本</span>");
    const cardAddress = flat.indexOf("<span>地址</span>", cardModel);
    expect(cardModel).toBeGreaterThan(-1);
    expect(cardVersion).toBeGreaterThan(cardModel);
    expect(cardAddress).toBeGreaterThan(cardVersion);

    // 两个视图都读设备生效版本(effectiveVersion),并共用同一个来源 tooltip
    expect(hasMarkup(source, "protocolVersionText(record.effectiveVersion)")).toBe(true);
    expect(hasMarkup(source, "protocolVersionText(item.effectiveVersion)")).toBe(true);
    expect(hasMarkup(source, "protocolVersionTooltip(record)")).toBe(true);
    expect(hasMarkup(source, "protocolVersionTooltip(item)")).toBe(true);
  });

  it("flattens device table detail/edit/delete instead of burying them in the more menu", () => {
    const flat = squash(source);
    const tableStart = flat.indexOf(':data="devices"');
    const tableEnd = flat.indexOf("</a-table>", tableStart);
    expect(tableStart).toBeGreaterThan(-1);
    expect(tableEnd).toBeGreaterThan(tableStart);

    const table = flat.slice(tableStart, tableEnd);
    const columnStart = table.indexOf('title="操作"');
    const column = table.slice(columnStart, table.indexOf("</a-table-column>", columnStart));
    expect(columnStart).toBeGreaterThan(-1);

    // 三个动作平铺在操作列里,各自直连 handler,不再需要展开下拉
    expect(hasMarkup(column, 'class="uvp-table-action uvp-table-action--detail" @click="openDevice(record)"')).toBe(true);
    expect(hasMarkup(column, 'class="uvp-table-action uvp-table-action--edit"')).toBe(true);
    expect(column).toContain('@click="openEditDeviceModal(record)"');
    expect(hasMarkup(column, 'class="uvp-table-action uvp-table-action--delete"')).toBe(true);
    expect(column).toContain('@click="handleDeleteDevice(record)"');

    // 顺序跟随卡片视图:详情 → 编辑 → 删除
    const order = ["uvp-table-action--detail", "uvp-table-action--edit", "uvp-table-action--delete"].map(token =>
      column.indexOf(token)
    );
    expect(order.every(index => index > -1)).toBe(true);
    expect(order).toEqual([...order].sort((left, right) => left - right));

    // 下拉里不再有 a-doption 形态的详情/编辑/删除,只剩设备维护,且排在操作列末尾
    expect(column).not.toContain("<a-doption");
    expect(column).toContain("<DeviceMaintenanceMenu");
    expect(column).toContain("<span>维护</span>");
    expect(column.indexOf("uvp-table-action--more")).toBeGreaterThan(order[2]);
  });

  it("sizes both action columns for every action they can render at once", () => {
    const flat = squash(source);

    // 设备表与通道表各有 7 个动作(通道「直播中」会多出一个「停止」);列宽不够时,
    // 那一行的按钮会溢出单元格、被表格右缘裁掉,并让该行看起来与表头「错位」。
    for (const dataSource of [':data="channels"', ':data="devices"']) {
      const tableStart = flat.indexOf(dataSource);
      const tableEnd = flat.indexOf("</a-table>", tableStart);
      expect(tableStart).toBeGreaterThan(-1);
      expect(tableEnd).toBeGreaterThan(tableStart);

      const table = flat.slice(tableStart, tableEnd);
      const columnStart = table.indexOf('title="操作"');
      expect(columnStart).toBeGreaterThan(-1);
      const column = table.slice(columnStart, table.indexOf("</a-table-column>", columnStart));

      const actionCount = (column.match(/uvp-table-action--/g) || []).length;
      const width = Number(column.match(/:width="(\d+)"/)?.[1]);
      // 单个动作 50px(内边距 4+4、图标 13、图标与文字间隙 3、两个汉字 26)+ 动作间 1px gap
      // + 单元格左右各 16px 内边距
      const required = actionCount * 50 + (actionCount - 1) + 32;

      expect(actionCount).toBeGreaterThan(0);
      expect(width).toBeGreaterThanOrEqual(required);
    }
  });

  it("hints the double-click drilldown on hover instead of leaving it invisible", () => {
    expect(source).toContain("function updateDrilldownHint");
    expect(source).toContain("function hideDrilldownHint");
    expect(source).toContain("双击进入通道");
    expect(hasMarkup(source, 'class="drilldown-hint"')).toBe(true);
    // 列表视图与卡片视图挂的是同一套悬停提示
    expect(source.match(/@mousemove="updateDrilldownHint"/g)).toHaveLength(2);
    expect(source.match(/@mouseleave="hideDrilldownHint"/g)).toHaveLength(2);
    // 通道侧的双击是播放而不是下钻,提示只在设备视图生效
    expect(source).toContain('assetKind.value !== "device"');
  });

  it("keeps offline channel snapshots at normal color and opacity", () => {
    expect(source).not.toContain(".channel-snapshot.offline { color: var(--uvp-text-tertiary); opacity: 0.72; }");
    expect(source).not.toContain("filter: grayscale(1);");
    expect(source).not.toContain(".channel-snapshot.offline .channel-snapshot-image");
  });

  it("refreshes overwritten snapshot files when snapshotAt changes", () => {
    expect(source).toContain("function snapshotImageUrl");
    expect(source).toContain("snapshotAt");
    expect(source).toContain(':src="snapshotImageUrl(record)"');
    expect(source).toContain(':src="snapshotImageUrl(item)"');
    expect(source).not.toContain(':src="record.snapshotUrl"');
    expect(source).not.toContain(':src="item.snapshotUrl"');
  });

  it("does not expose SIP trace launch actions from device management", () => {
    expect(source).not.toContain("启动 SIP 诊断窗口");
    expect(source).not.toContain("startDeviceTraceCapture");
    expect(source).not.toContain("traceCaptureStarting");
    expect(source).not.toContain("trace-capture-action");
    expect(source).not.toContain("icon-btn.trace-capture");
  });

  it("uses a control icon for the channel operation entry instead of the generic more icon", () => {
    const flat = squash(source);

    // 通道表格：截取「操作」这条动作本身的 <a-link> 片段(别拿整表,那会带上别人)
    const tableAnchor = flat.indexOf("openChannelOperations(record)");
    expect(tableAnchor).toBeGreaterThan(-1);
    const tableStart = flat.lastIndexOf("<a-link", tableAnchor);
    const tableAction = flat.slice(tableStart, flat.indexOf("</a-link>", tableStart));
    expect(tableAction).toContain("<Joystick");
    expect(tableAction).not.toContain("MoreHorizontal");
    expect(tableAction).toContain("<span>操作</span>");

    // 通道卡片：「通道操作」那颗图标按钮
    const cardAnchor = flat.indexOf('aria-label="通道操作"');
    expect(cardAnchor).toBeGreaterThan(-1);
    const cardStart = flat.lastIndexOf("<button", cardAnchor);
    const cardButton = flat.slice(cardStart, flat.indexOf("</button>", cardStart));
    expect(cardButton).toContain("<Joystick");
    expect(cardButton).not.toContain("MoreHorizontal");
  });

  it("distinguishes subscription management from the primary channel action", () => {
    expect(source).toContain('class="icon-btn small framed primary" type="button" @click.stop="showDeviceChannels(item)"');
    expect(source).toContain(
      'class="icon-btn small framed subscription" type="button" @click.stop="openSubscriptionManager(item)"'
    );
    expect(source).toContain(".icon-btn.framed.subscription {");
  });

  it("places the device maintenance menu at the far right of card actions", () => {
    const actionBar = source.slice(
      source.indexOf('<div class="card-actions device-card-actions">'),
      source.indexOf("</article>", source.indexOf('<div class="card-actions device-card-actions">'))
    );
    const moreActionsIndex = actionBar.indexOf('aria-label="更多设备操作"');
    const deleteIndex = actionBar.indexOf('class="icon-btn small framed danger"');

    expect(moreActionsIndex).toBeGreaterThan(-1);
    expect(deleteIndex).toBeGreaterThan(-1);
    expect(moreActionsIndex).toBeGreaterThan(deleteIndex);
  });

  it("keeps the card maintenance dropdown from scrolling when all actions fit", () => {
    const menuRule = source.match(/\.device-action-menu-item\)\s*\{([^}]*)\}/)?.[1] || "";
    const actionButtonIndex = source.indexOf('aria-label="更多设备操作"');
    const cardMenuStart = source.lastIndexOf("<a-dropdown", actionButtonIndex);
    const cardMenu = source.slice(cardMenuStart, source.indexOf("</a-dropdown>", cardMenuStart));

    expect(menuRule).toContain("gap: 10px;");
    expect(menuRule).toContain("line-height: 32px;");
    expect(cardMenu).toContain(':popup-max-height="false"');
    expect(cardMenu).toContain('content-class="device-card-maintenance-dropdown"');
    expect(source).toContain(":global(.device-card-maintenance-dropdown .arco-scrollbar-thumb-direction-vertical)");
  });

  it("distinguishes device drilldown from manual channel mode", () => {
    expect(source).toContain('channelEntrySource.value = "device-drilldown";');
    expect(source).toContain('channelEntrySource.value = "manual";');
    const drilldown = source.slice(
      source.indexOf("function showDeviceChannels"),
      source.indexOf("async function loadPtzTypeDict")
    );
    expect(drilldown).toContain("drawerVisible.value = false;");
    expect(source).toContain('assetKind.value = "device";');
    expect(source).toContain('class="filter-chip device-drilldown-chip"');
    expect(source).toContain('aria-label="返回设备列表"');
    expect(source).toContain('v-else-if="deviceIdFilter" class="filter-chip"');
    expect(source).toContain("function resetFilters()");
    expect(source).toContain('if (assetKind.value === "channel" && channelEntrySource.value === "device-drilldown")');
  });

  it("edits the device alias, media node and protocol without overriding reported hardware metadata", () => {
    const modalStart = source.indexOf("<!-- 编辑设备 Modal -->");
    const modalEnd = source.indexOf("<!-- 编辑通道 Modal -->", modalStart);
    const modal = source.slice(modalStart, modalEnd);

    expect(modal).toContain('field="zlmNodeId" label="ZLM 节点"');
    expect(modal).toContain(':loading="zlmNodesLoading"');
    expect(source).toContain('value: 0, label: "自动调度"');
    expect(modal).not.toContain('field="manufacturer"');
    expect(modal).not.toContain('field="model"');
    expect(modal).not.toContain('field="firmware"');
    expect(source).toContain("listZLMNodes");
    expect(source).toContain("zlmNodesError");
  });

  it("exposes separate device maintenance actions on device cards", () => {
    for (const component of [
      "DeviceRebootDialog",
      "DeviceFirmwareUpgradeDialog",
      "DeviceMaintenanceRecordsDialog",
      "DeviceMaintenanceMenu"
    ]) {
      expect(source).toContain(component);
    }
    expect(source).not.toContain("DeviceMaintenanceDialog");
    expect(source).not.toContain("DeviceFirmwareUpgradeDrawer");
    expect(source).toContain("DeviceMaintenanceRecordsDialog");
    expect(source).not.toContain("DeviceMaintenanceRecordsDrawer");
    for (const target of ["record", "item"]) {
      expect(source).toContain(`openDeviceUpgrade(${target})`);
      expect(source).toContain(`openDeviceReboot(${target})`);
      expect(source).toContain(`openMaintenanceRecords(${target})`);
    }
    const detailStart = source.indexOf("drawerTarget?.type === 'device' && deviceDetail");
    const detailEnd = source.indexOf("</a-drawer>", detailStart);
    const detail = source.slice(detailStart, detailEnd);
    expect(detail).not.toContain("openDeviceUpgrade");
    expect(detail).not.toContain("openDeviceReboot");
    expect(detail).not.toContain("openMaintenanceRecords");
    expect(source).toContain('v-model:visible="upgradeVisible"');
    expect(source).toContain("<DeviceFirmwareUpgradeDialog");
    expect(source).toContain('v-model:visible="rebootVisible"');
    expect(source).toContain('v-model:visible="recordsVisible"');
    expect(source).toContain("<DeviceMaintenanceRecordsDialog");
    expect(source).toContain("<a-modal");

    const playback = readFileSync(resolve(process.cwd(), "src/views/gb28181/components/PlayConsoleLinked.vue"), "utf8");
    expect(playback).not.toContain("远程重启父设备");
    expect(playback).not.toContain("runAdvancedAction('teleboot')");
  });
});
