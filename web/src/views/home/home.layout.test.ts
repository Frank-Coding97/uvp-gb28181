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
    expect(source).toContain("APP_VERSION_TEXT");
    expect(source).toContain("{{ APP_VERSION_TEXT }}");
    expect(source).not.toContain("platformInfo?.version ?");
    expect(source).toContain("平台运行时间");
    expect(source).toContain("当前时间");
    expect(source).toContain("platformClockDate");
    expect(source).toContain("platformClockTime");
    expect(source).toContain(":key=\"platformClockHour\"");
    expect(source).toContain(":key=\"platformClockMinute\"");
    expect(source).toContain(":key=\"platformClockSecond\"");
    expect(source).not.toContain(":key=\"platformClockTime\"");
    expect(source).toContain("width:108px;height:24px;font-variant-numeric:tabular-nums");
    expect(source).toContain(".clock-unit-shell{position:relative;width:28px;");
    expect(source).toContain("setInterval(() => { clockNow.value = new Date(); }, 1_000)");
    expect(source).toContain("<Transition name=\"clock-tick\"");
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

  it("reuses the shared primary button treatment for dashboard editing actions", () => {
    expect(source).toMatch(/<button v-if="!editing" class="btn-primary primary"[^>]*>.*编辑仪表盘<\/button>/s);
    expect(source).toMatch(/<button class="btn-primary primary"[^>]*>.*保存布局.*<\/button>/s);
    expect(source).toContain(".primary{color:#fff;background:var(--uvp-brand);border:0}");
    expect(source).toContain(".actions .primary{font-weight:600}");
  });

  it("keeps system theme tokens", () => {
    expect(source).not.toContain("background:var(--uvp-shell-muted)");
    expect(source).not.toContain("height:calc(100% - 2px)");
    expect(source).toContain("var(--uvp-panel-bg)");
    expect(source).toContain("var(--uvp-panel-border)");
    expect(source).toContain("var(--uvp-brand)");
  });

  it("overrides GridStack content scrolling inside dashboard cards", () => {
    expect(source).toContain(".dashboard-grid>.grid-stack-item>.grid-stack-item-content.card{overflow:hidden}");
  });

  it("compacts the embedded SIP monitor enough to avoid internal scrolling", () => {
    expect(source).toContain(".embedded{height:100%;gap:10px;");
    expect(source).toContain(".embedded :deep(.pulse){flex:none}");
    expect(source).toContain(".embedded :deep(.pulse__chart){flex:none;height:90px;min-height:90px}");
  });

  it("centers the media health and platform information contents", () => {
    expect(source).toContain("class=\"health-content\"");
    expect(source).toContain(".widget-media-node-health .card,.widget-platform-info .card{display:flex;flex-direction:column}");
    expect(source).toContain(".health-content{display:flex;flex:1;flex-direction:column;justify-content:center}");
    expect(source).toContain(".platform-info{display:grid;flex:1;");
  });

  it("keeps cumulative traffic and realtime rate as different metrics", () => {
    expect(source).toContain("selectedTrafficValue");
    expect(source).toContain("trafficDirection");
    expect(source).toContain("今日累计{{ trafficDirectionLabel }}流量");
    expect(source).toContain("上行");
    expect(source).toContain("下行");
    expect(source).toContain("role=\"tablist\"");
    expect(source).toContain("aria-selected");
    expect(source).toContain("<MediaRateArea :samples=\"mediaRateTrend\"");
    expect(source).not.toContain("{{ bytes(mediaRate) }}<small>/s</small>");
    expect(source).not.toContain("class=\"bars\"");
  });

  it("renders the real rolling 24-hour play success summary", () => {
    expect(source).toContain("dashboardSummary.value?.play");
    expect(source).toContain("playSummary.value?.rate");
    expect(source).toContain("近 24 小时暂无点播样本");
    expect(source).toContain("次媒体流就绪");
    expect(source).not.toContain("computed<number | null>(() => null)");
  });
});
