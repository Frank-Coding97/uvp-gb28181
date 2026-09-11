import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/home/home.vue"), "utf8");

describe("editable realtime dashboard layout", () => {
  it("uses authoritative realtime sources", () => {
    expect(source).toContain("fetchSipDashboardSnapshot");
		expect(source).toContain('fetchSipDashboardSnapshot({ window: "60s", precision: "1s" })');
    expect(source).toContain("getZLMOverview");
    expect(source).toContain('getHomeDashboardSummary(["assets", "aggregate"])');
    expect(source).toContain("dashboardSummary.value.assets");
    expect(source).not.toContain("listDevices");
    expect(source).not.toContain("listChannels");
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

	it("guards dashboard layout entry points and write functions by permission", () => {
		expect(source).toContain("useUserStoreHook");
		expect(source).toContain('v-if="!editing && canEditLayout"');
		expect(source).toContain('<template v-if="editing">');
		expect(source).toContain('v-if="canResetLayout"');
		expect(source).toContain('v-if="canSaveLayout"');
		expect(source).toContain('if (!canEditLayout.value) return;');
		expect(source).toContain('if (!canSaveLayout.value) return;');
		expect(source).toContain('if (!canResetLayout.value) return;');
	});

	it("does not fetch or expose SIP platform details without explicit view permission", () => {
		expect(source).toContain('const canViewSipConfig = computed(() => hasPermission("gb28181:sip:config:view"));');
		expect(source).toContain("canViewSipConfig.value ? fetchSipPlatformInfo() : Promise.resolve(null)");
		expect(source).toContain('if (!canViewSipConfig.value) return "--";');
		expect(source).toContain("if (!canViewSipConfig.value) platformInfo.value = null;");
		expect(source).toContain("fetchSipDashboardSnapshot");
		expect(source).toContain("getZLMOverview");
		expect(source).toContain("getHomeDashboardSummary");
	});

  it("reuses the shared primary button treatment for dashboard editing actions", () => {
		expect(source).toMatch(/<button v-if="!editing && canEditLayout" class="btn-primary primary"[^>]*>.*编辑仪表盘<\/button>/s);
		expect(source).toMatch(/<button v-if="canSaveLayout" class="btn-primary primary"[^>]*>.*保存布局.*<\/button>/s);
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
    expect(source).toContain("今日累计上行流量");
    expect(source).toContain("今日已结算下行流量");
    expect(source).toContain("上行");
    expect(source).toContain("下行");
    expect(source).toContain("role=\"tablist\"");
    expect(source).toContain("aria-selected");
    expect(source).toContain("<MediaRateArea :samples=\"mediaRateSamples\"");
    expect(source).toContain("overview.value.data.mediaRateSamples");
    expect(source).not.toContain("[...mediaRateSamples.value");
    expect(source).toContain("bytes(mediaRateSnapshot.upstream)");
    expect(source).toContain("bytes(mediaRateSnapshot.downstream)");
    expect(source).toContain("upstreamBytesPerSecond");
    expect(source).toContain("downstreamBytesPerSecond");
    expect(source).toContain("实时上行");
    expect(source).toContain("实时下行");
    expect(source).toContain("数据不完整 · 等待全部媒体节点");
    expect(source).not.toContain("兼容模式 · 媒体源码率");
    expect(source).not.toContain("mediaOverview.value?.streams.reduce");
    expect(source).toContain('trafficDirection.value === "upstream" ? "var(--uvp-brand)" : "var(--uvp-brand-cyan)"');
    expect(source).toContain(".traffic-legend .up{background:var(--uvp-brand)}");
    expect(source).toContain(".traffic-legend .down{background:var(--uvp-brand-cyan)}");
    expect(source).not.toContain("mediaRateDirection");
    expect(source).not.toContain("{{ bytes(mediaRate) }}<small>/s</small>");
    expect(source).not.toContain("class=\"bars\"");
  });

  it("renders the real rolling 24-hour play success summary", () => {
    expect(source).toContain("dashboardSummary.value?.play");
    expect(source).toContain("playSummary.value?.rate");
    expect(source).toContain("近 24 小时暂无点播样本");
    expect(source).toContain("次媒体流就绪");
		expect(source).toContain("staleStarted");
    expect(source).not.toContain("computed<number | null>(() => null)");
  });

	it("uses a rolling 60-second SIP rate and durable daily transaction totals", () => {
		expect(source).toContain("dashboardSummary.value?.sip");
		expect(source).toContain("sipSummary.value?.transactions");
		expect(source).toContain("sipSummary.value?.failure");
		expect(source).toContain("reduce((sum, item) => sum + item.msgPerSec, 0)");
		expect(source).not.toContain("item.msgPerSec, 0) / values.length * 60");
		expect(source).not.toContain("sipSnapshot.value.todayTotal");
	});

	it("surfaces incomplete and unavailable media runtime snapshots", () => {
		expect(source).toContain("mediaRuntimeStatus");
		expect(source).toContain("部分数据");
		expect(source).toContain("数据不可用");
	});
});
