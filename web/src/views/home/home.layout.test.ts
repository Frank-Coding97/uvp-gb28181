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
    for (const title of [
      "实时 SIP RPM",
      "今日 SIP 处理数量",
      "点播成功率",
      "设备在线率",
      "通道在线率",
      "GB28181 SIP 协议监控",
      "媒体实时速率",
      "媒体节点健康",
      "活跃流排行"
    ]) {
      expect(source).toContain(title);
    }
    expect(source).toContain("grid-stack-item");
    expect(source).toContain("<OnlineDonut");
    expect(source).not.toContain('class="runtime-title"');
    expect(source).toContain('<CardTitle icon="server" title="流媒体运行态" />');
    expect(source).toContain('<CardTitle icon="platform" title="平台信息" />');
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
    expect(source).toContain(':key="platformClockHour"');
    expect(source).toContain(':key="platformClockMinute"');
    expect(source).toContain(':key="platformClockSecond"');
    expect(source).not.toContain(':key="platformClockTime"');
    expect(source).toMatch(
      /\.clock-time-shell\s*\{[^}]*width:\s*108px;[^}]*height:\s*24px;[^}]*font-variant-numeric:\s*tabular-nums;/s
    );
    expect(source).toMatch(/\.clock-unit-shell\s*\{[^}]*position:\s*relative;[^}]*width:\s*28px;/s);
    expect(source).toMatch(/setInterval\(\(\)\s*=>\s*\{\s*clockNow\.value\s*=\s*new Date\(\);\s*\},\s*1_000\)/s);
    expect(source).toContain('<Transition name="clock-tick"');
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
    // ⭐ 权限收敛（2026-10-11）：判定入口统一到 `@/hooks/usePermission`，
    //    home.vue 不再自己读 store —— 断言点从「读 store」改成「走统一入口」。
    expect(source).toContain('import { hasPermission } from "@/hooks/usePermission"');
    // ⭐ 权限改造（2026-10-11）：入口按钮**不再隐藏**，改为「可见但置灰」——
    //    隐藏会让用户不知道平台还有这些功能。函数体内的权限早退仍然保留（下方三条）。
    expect(source).toContain('v-if="!editing"');
    expect(source).toContain(':disabled="!canEditLayout"');
    expect(source).toContain('<template v-if="editing">');
    expect(source).toContain(':disabled="!canResetLayout"');
    expect(source).toContain(':disabled="saving || !canSaveLayout"');
    expect(source).toContain("if (!canEditLayout.value) return;");
    expect(source).toContain("if (!canSaveLayout.value) return;");
    expect(source).toContain("if (!canResetLayout.value) return;");
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

  it("reuses the shared button system for every dashboard header action", () => {
    // 契约不变（页头动作必须复用共享主按钮处理），只是真源从 home.vue 自己的
    // `.primary` 换成了全局语义类 `uvp-btn-primary`（web/src/styles/uvp-buttons.scss）。
    expect(source).toMatch(
      /<button\s+v-if="!editing"\s+class="uvp-btn-primary"\s+:disabled="!canEditLayout"[^>]*>[\s\S]*?编辑仪表盘[\s\S]*?<\/button>/s
    );
    expect(source).toMatch(
      /<button\s+class="uvp-btn-primary"\s+:disabled="saving \|\| !canSaveLayout"[^>]*>[\s\S]*?保存布局[\s\S]*?<\/button>/s
    );
    // ⛔ 2026-11-04 补：刷新是上一轮唯一"没上色"的按钮（原来挂本地 .secondary = 白底黑字），
    //    必须挂青绿语义类；取消走中性、恢复默认走次级，四层语义各就各位。
    expect(source).toMatch(/<button\s+class="uvp-refresh-btn"\s+:disabled="refreshing"[^>]*>[\s\S]*?刷新[\s\S]*?<\/button>/s);
    expect(source).toContain('class="uvp-btn-secondary" @click="resetLayout"');
    expect(source).toContain('class="uvp-btn-neutral" @click="cancelEditing"');
    // 页头不许再自持几何与配色，否则又会分叉出第二套观感。
    // （小屏媒体查询里把可点区抬到 min-height:40px 是触屏可达性，不算第二套，放行。）
    const actionsButtonBlocks = [...source.matchAll(/\.actions\s+button\s*\{([^}]*)\}/g)].map(m => m[1]);
    for (const block of actionsButtonBlocks) {
      expect(block).not.toMatch(/(^|[\s;])(height|padding|border-radius|background|color)\s*:/);
    }
    expect(source).not.toMatch(/^\s*\.secondary\s*\{/m);
    expect(source).not.toMatch(/^\s*\.primary\s*\{/m);
  });

  it("keeps system theme tokens", () => {
    expect(source).not.toContain("background:var(--uvp-shell-muted)");
    expect(source).not.toContain("height:calc(100% - 2px)");
    expect(source).toContain("var(--uvp-panel-bg)");
    expect(source).toContain("var(--uvp-panel-border)");
    expect(source).toContain("var(--uvp-brand)");
  });

  it("overrides GridStack content scrolling inside dashboard cards", () => {
    expect(source).toMatch(
      /\.dashboard-grid\s*>\s*\.grid-stack-item\s*>\s*\.grid-stack-item-content\.card\s*\{[^}]*overflow:\s*hidden;/s
    );
  });

  it("keeps the embedded SIP monitor compact and lets the pulse chart absorb spare height", () => {
    expect(source).toMatch(/\.embedded\s*\{[^}]*gap:\s*10px;[^}]*height:\s*100%;/s);
    // 卡片被拖小时只许长不许缩：缩掉图表会重现"画布高度 > 可见区、y=0 基线被裁"的老问题。
    expect(source).toMatch(/\.embedded\s+:deep\(\.pulse\)\s*\{[^}]*flex:\s*1 0 auto;/s);
    // 保底 90px 是原契约；2026-11-10 补维度：有富余高度时曲线区随容器长高，
    // 空白不再堆在事务网格下面，但仍不引入卡片内部滚动。
    expect(source).toMatch(
      /\.embedded\s+:deep\(\.pulse__chart\)\s*\{[^}]*flex:\s*1 1 auto;[^}]*height:\s*auto;[^}]*min-height:\s*90px;/s
    );
  });

  it("centers the media health and platform information contents", () => {
    expect(source).toContain('class="health-content"');
    expect(source).toMatch(
      /\.widget-media-node-health\s+\.card\s*,\s*\.widget-platform-info\s+\.card\s*\{[^}]*display:\s*flex;[^}]*flex-direction:\s*column;/s
    );
    expect(source).toMatch(
      /\.health-content\s*\{[^}]*display:\s*flex;[^}]*flex:\s*1;[^}]*flex-direction:\s*column;[^}]*justify-content:\s*center;/s
    );
    expect(source).toMatch(/\.platform-info\s*\{[^}]*display:\s*grid;[^}]*flex:\s*1;/s);
  });

  it("keeps cumulative traffic and realtime rate as different metrics", () => {
    expect(source).toContain("selectedTrafficValue");
    expect(source).toContain("trafficDirection");
    expect(source).toContain("今日累计上行流量");
    expect(source).toContain("今日已结算下行流量");
    expect(source).toContain("上行");
    expect(source).toContain("下行");
    expect(source).toContain('role="tablist"');
    expect(source).toContain("aria-selected");
    expect(source).toContain('<MediaRateArea :samples="mediaRateSamples"');
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
    expect(source).toMatch(/\.traffic-legend\s+\.up\s*\{[^}]*background:\s*var\(--uvp-brand\);/s);
    expect(source).toMatch(/\.traffic-legend\s+\.down\s*\{[^}]*background:\s*var\(--uvp-brand-cyan\);/s);
    expect(source).not.toContain("mediaRateDirection");
    expect(source).not.toContain("{{ bytes(mediaRate) }}<small>/s</small>");
    expect(source).not.toContain('class="bars"');
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
