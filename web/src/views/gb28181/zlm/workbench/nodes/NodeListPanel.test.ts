import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/workbench/nodes/NodeListPanel.vue"), "utf8");

describe("NodeListPanel", () => {
  it("keeps only the routine node actions in the list", () => {
    expect(source).toContain("enableZLMNode");
    expect(source).toContain("disableZLMNode");
    expect(source).toContain("ZLMNodeActionDialog");
    expect(source).toContain("gb28181:zlm:node:manage");
    expect(source).toContain("recoveryRequired");
    expect(source).toContain("autoOnDemandReady");
    expect(source).not.toContain("testZLMNodeConnection");
    expect(source).not.toContain("activateZLMNode");
    expect(source).not.toContain("openAction(record, 'maintenance')");
    expect(source).not.toContain("openAction(record, 'kick')");
    expect(source).not.toContain("openAction(record, 'restart')");
  });

  it("accepts canonical scope data and does not use a guessed node for writes", () => {
    expect(source).toContain("defineProps");
    expect(source).toContain("scope");
    expect(source).toContain("context.selectNode(node.id)");
    expect(source).toMatch(/setScope|nodeId/);
    expect(source).not.toContain("nodes[0]");
  });

  it("refreshes from the parent scope and retains the last successful rows on errors", () => {
    expect(source).toContain("emit(\"refresh\")");
    expect(source).toContain("上一次节点列表");
    expect(source).toContain("impact");
    expect(source).toContain("fingerprint");
  });

  it("shows a ten-second countdown and refreshes the node list in a loop", () => {
    expect(source).toContain("const AUTO_REFRESH_SECONDS = 10");
    expect(source).toContain("const refreshCountdown = ref(AUTO_REFRESH_SECONDS)");
    expect(source).toContain("refreshTimer = setInterval(tickRefreshCountdown, 1_000)");
    expect(source).toContain("function handleManualRefresh()")
    expect(source).toContain("scheduleRefresh();")
    expect(source).toContain("const refreshButtonLabel = computed")
    expect(source).toContain("刷新（${refreshCountdown.value}s）")
    expect(source).toContain("{{ refreshButtonLabel }}")
  });

  it("does not repeat cluster summary cards above the node table", () => {
    expect(source).not.toContain("StatCard");
    expect(source).not.toContain('class="kpi-row"');
  });

  it("uses the system search panel controls and explicit query actions", () => {
    expect(source).toContain("<s-layout-search");
    expect(source).toContain("<a-input");
    expect(source).not.toContain("<a-input-search");
    expect(source).toContain('@press-enter="queryRows"');
    expect(source).toContain('type="primary" @click="queryRows"');
    expect(source).toContain('@click="resetFilters"');
    expect(source).not.toContain("flex-wrap: nowrap");
    expect(source).not.toContain(".search, .filter-select { width: 100%; }");
    expect(source).toContain("background: var(--uvp-search-control-bg) !important");
    expect(source).toContain("border: 1px solid var(--uvp-search-secondary-btn-border) !important");
    expect(source).not.toContain('class="filter-meta"');
  });

  it("uses the system table action language without the redundant scope hint", () => {
    expect(source).not.toContain("当前范围：");
    expect(source).not.toContain('class="scope-hint"');
    expect(source).toContain('class="uvp-table-actions node-row-actions"');
    expect(source).toContain('title="操作" :width="280"');
    expect(source).toContain("openServiceConfig(record)");
    expect(source).toContain("服务配置");
    expect(source).toContain('class="uvp-table-action uvp-table-action--edit"');
    expect(source).toContain("handleEnabled(record)");
    expect(source).toContain('class="uvp-table-action uvp-table-action--delete"');
    expect(source).toContain("openAction(record, 'delete')");
    expect(source).not.toContain("gotoDetail");
    expect(source).not.toContain("MoreHorizontal");
    expect(source).not.toContain("<a-dropdown");
    expect(source).not.toContain(">详情</a-link>");
    expect(source).not.toContain('class="cell-ops"');
  });

  it("separates operator admission from heartbeat health", () => {
    expect(source).toContain('title="管理状态"');
    expect(source).toContain('title="在线状态"');
    expect(source).toContain('placeholder="管理状态"');
    expect(source).toContain('placeholder="在线状态"');
  });
});
