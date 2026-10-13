import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { squash } from "@/test/source-assert";

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
    for (const control of ['class="cmdk"', 'class="view-switch"', "create-device-btn", "refresh-control", "status-select"]) {
      expect(source.indexOf(control)).toBeGreaterThan(actionsIndex);
    }
    expect(source).toContain(".workspace-toolbar :deep(.uvp-search-panel__fields)");
    expect(source).toContain("flex: 1 1 100%;");
  });

  it("orders toolbar controls by usage frequency", () => {
    const controls = ['class="view-switch"', 'class="cmdk"', "status-select", "refresh-control", "create-device-btn"];
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

  it("keeps device and channel lists available while exposing device context actions", () => {
    expect(source).toContain("<DirectoryPanel");
    expect(source).toContain('@device-select="onDirectoryDeviceSelect"');
    expect(source).not.toContain("<DeviceTreePanel");
    expect(source).not.toContain('class="navigation-tabs"');
    expect(source).not.toContain("DeviceBatchManager");
    expect(source).toContain('class="segmented asset-kind-switch"');
    expect(source).toContain('data-asset-kind="device"');
    expect(source).toContain('data-asset-kind="channel"');
    expect(source).toContain(':data="devices"');
    expect(source).toContain(':data="channels"');
    expect(source).toContain('title="所属设备"');
    for (const action of [
      "openDevice",
      "handleRefreshDeviceCatalog",
      "openStatusEvents",
      "openSubscriptionManager",
      "openEditDeviceModal",
      "handleDeleteDevice"
    ]) {
      expect(source).toContain(`${action}(selectedDevice)`);
    }
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

  it("keeps the double-click drilldown free of any floating hint", () => {
    // 2026-10-05 老板要求去掉设备列表上跟随光标的「双击进入通道」浮层。
    // 双击下钻本身保留(onDeviceDblclick 仍在),这里钉住"不要再挂回来"。
    expect(source).not.toContain("DrilldownHint");
    expect(source).not.toContain("drilldown-hint");
    expect(source).not.toContain("双击进入通道");
    expect(source).not.toContain("@mousemove=");
    expect(source).not.toContain("@mouseleave=");
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

  it("exposes separate device maintenance actions in the selected device context", () => {
    for (const component of ["DeviceRebootDialog", "DeviceFirmwareUpgradeDialog", "DeviceMaintenanceRecordsDialog"]) {
      expect(source).toContain(component);
    }
    expect(source).not.toContain("DeviceMaintenanceDialog");
    expect(source).not.toContain("DeviceFirmwareUpgradeDrawer");
    expect(source).toContain("DeviceMaintenanceRecordsDialog");
    expect(source).not.toContain("DeviceMaintenanceRecordsDrawer");
    for (const target of ["selectedDevice"]) {
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
