import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

describe("device management toolbar layout", () => {
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
        for (const control of ['class="cmdk"', 'class="view-switch"', "create-device-btn", "refresh-control", "status-select", 'class="segmented"']) {
            expect(source.indexOf(control)).toBeGreaterThan(actionsIndex);
        }
        expect(source).toContain(".workspace-toolbar :deep(.uvp-search-panel__fields)");
        expect(source).toContain("flex: 1 1 100%;");
    });

    it("orders toolbar controls by usage frequency", () => {
        const controls = [
            'class="segmented"',
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

    it("does not expose SIP trace launch actions from device management", () => {
        expect(source).not.toContain("启动 SIP 诊断窗口");
        expect(source).not.toContain("startDeviceTraceCapture");
        expect(source).not.toContain("traceCaptureStarting");
        expect(source).not.toContain("trace-capture-action");
        expect(source).not.toContain("icon-btn.trace-capture");
    });

    it("distinguishes subscription management from the primary channel action", () => {
        expect(source).toContain('class="icon-btn small framed primary" type="button" @click.stop="showDeviceChannels(item)"');
        expect(source).toContain('class="icon-btn small framed subscription" type="button" @click.stop="openSubscriptionManager(item)"');
        expect(source).toContain(".icon-btn.framed.subscription {");
    });

    it("distinguishes device drilldown from manual channel mode", () => {
        expect(source).toContain('channelEntrySource.value = "device-drilldown";');
        expect(source).toContain('channelEntrySource.value = "manual";');
        expect(source).toContain('assetKind.value = "device";');
        expect(source).toContain('class="filter-chip device-drilldown-chip"');
        expect(source).toContain('aria-label="返回设备列表"');
        expect(source).toContain('v-else-if="deviceIdFilter" class="filter-chip"');
        expect(source).toContain('function resetFilters()');
        expect(source).toContain('if (assetKind.value === "channel" && channelEntrySource.value === "device-drilldown")');
    });
});
