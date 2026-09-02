import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/home/home.vue"), "utf8");

describe("editable realtime dashboard layout", () => {
  it("uses authoritative realtime sources", () => {
    expect(source).toContain("fetchSipDashboardSnapshot");
    expect(source).toContain("getZLMOverview");
    expect(source).toContain("listDevices");
    expect(source).toContain("listChannels");
    expect(source).toContain("setInterval(refreshData, 5_000)");
  });

  it("implements the approved widgets and reference composition", () => {
    for (const title of ["实时 SIP RPM", "今日 SIP 处理数量", "点播成功率", "设备在线率", "通道在线率", "GB28181 SIP 协议监控", "媒体实时速率", "媒体节点健康", "活跃流排行"]) {
      expect(source).toContain(title);
    }
    expect(source).toContain("grid-stack-item");
    expect(source).toContain("<OnlineDonut");
    expect(source).not.toContain("class=\"runtime-title\"");
    expect(source).toContain("<CardTitle icon=\"server\" title=\"流媒体运行态\" />");
    expect(source).toContain("<CardTitle icon=\"platform\" title=\"平台信息\" />");
    expect(source).toContain("fetchSipPlatformInfo");
    expect(source).toContain("getHomeDashboardSummary");
    expect(source).toContain("平台版本");
    expect(source).toContain("平台运行时间");
    expect(source).not.toContain("网络线程负载");
    for (const color of ["--uvp-warning", "--uvp-brand-cyan", "--uvp-brand"]) {
      expect(source).toContain(`color=\"var(${color})\"`);
    }
    expect(source).toContain("var(--uvp-danger)");
  });

  it("supports editing, persistence, conflict messaging and reset", () => {
    expect(source).toContain("编辑仪表盘");
    expect(source).toContain("saveHomeDashboardLayout");
    expect(source).toContain("resetHomeDashboardLayout");
    expect(source).toContain("status === 409");
    expect(source).toContain("grid?.setEditing(true)");
  });

  it("keeps system theme tokens", () => {
    expect(source).not.toContain("background:var(--uvp-shell-muted)");
    expect(source).not.toContain("height:calc(100% - 2px)");
    expect(source).toContain("var(--uvp-panel-bg)");
    expect(source).toContain("var(--uvp-panel-border)");
    expect(source).toContain("var(--uvp-brand)");
  });

  it("compacts the embedded SIP monitor enough to avoid internal scrolling", () => {
    expect(source).toContain(".embedded{height:100%;gap:10px;");
    expect(source).toContain(".embedded :deep(.pulse){flex:none}");
    expect(source).toContain(".embedded :deep(.pulse__chart){flex:none;height:90px;min-height:90px}");
  });

  it("keeps cumulative traffic and realtime rate as different metrics", () => {
    expect(source).toContain("selectedTrafficValue");
    expect(source).toContain("trafficDirection");
    expect(source).toContain("今日累计{{ trafficDirectionLabel }}流量");
    expect(source).toContain("上行");
    expect(source).toContain("下行");
    expect(source).toContain("role=\"tablist\"");
    expect(source).toContain("aria-selected");
    expect(source).toContain("<MediaRateArea :values=\"mediaRateTrend\"");
    expect(source).not.toContain("{{ bytes(mediaRate) }}<small>/s</small>");
    expect(source).not.toContain("class=\"bars\"");
  });
});
