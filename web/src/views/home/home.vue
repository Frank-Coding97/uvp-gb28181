<template>
  <div class="dashboard-shell">
    <header class="dashboard-header">
      <div class="dashboard-title"><span class="title-icon"><LayoutDashboard :size="20" /></span><div><small>GB28181 视频接入平台</small><h1>仪表盘</h1></div></div>
      <div class="actions">
        <span class="live"><i />实时数据 · {{ updatedText }}</span>
        <button class="secondary" :disabled="refreshing" @click="refreshData"><RefreshCw :size="15" :class="{ spin: refreshing }" />刷新</button>
        <button v-if="!editing && canEditLayout" class="btn-primary primary" @click="startEditing"><Pencil :size="15" />编辑仪表盘</button>
        <template v-if="editing">
          <button v-if="canResetLayout" class="secondary" @click="resetLayout"><RotateCcw :size="15" />恢复默认</button>
          <button class="secondary" @click="cancelEditing">取消</button>
          <button v-if="canSaveLayout" class="btn-primary primary" :disabled="saving" @click="saveLayout"><Save :size="15" />{{ saving ? "保存中" : "保存布局" }}</button>
        </template>
      </div>
    </header>

    <section v-if="editing" class="widget-picker">
      <strong>组件</strong>
      <label v-for="widget in layout.widgets" :key="widget.id"><input v-model="widget.visible" type="checkbox" @change="rebuildGrid" />{{ widgetTitle(widget.id) }}</label>
      <span class="desktop-edit-hint">拖动卡片调整位置，拖拽边缘调整大小</span><span class="compact-edit-hint">小屏可调整组件显示，拖拽布局请在桌面端操作</span>
    </section>

    <div v-if="layoutLoading" class="loading"><LoaderCircle class="spin" :size="24" />正在加载仪表盘</div>
    <main v-else ref="gridElement" class="grid-stack dashboard-grid" :class="{ editing }">
      <article v-for="widget in visibleWidgets" :key="widget.id" class="grid-stack-item" :class="`widget-${widget.id}`"
        :gs-id="widget.id" :gs-x="widget.x" :gs-y="widget.y" :gs-w="widget.w" :gs-h="widget.h"
        :gs-min-w="definition(widget.id).minW" :gs-max-w="definition(widget.id).maxW"
        :gs-min-h="definition(widget.id).minH" :gs-max-h="definition(widget.id).maxH">
        <div class="grid-stack-item-content card" :class="{ 'drilldown-card': isDashboardDrilldownWidget(widget.id) }"
          :role="!editing && isDashboardDrilldownWidget(widget.id) ? 'button' : undefined"
          :tabindex="!editing && isDashboardDrilldownWidget(widget.id) ? 0 : undefined"
          @click="openWidgetDrilldown(widget.id)" @keydown="onWidgetDrilldownKeydown($event, widget.id)">
          <span v-if="editing" class="drag"><GripHorizontal :size="16" /></span>
          <template v-if="widget.id === 'sip-rpm'">
            <CardTitle icon="activity" title="实时 SIP RPM" /><div class="kpi">{{ number(sipRpm) }}<small>/min</small></div><p>最近一分钟处理速率</p><MiniTrend :values="sipTrend" color="var(--uvp-warning)" />
          </template>
          <template v-else-if="widget.id === 'sip-today'">
            <CardTitle icon="radio" title="今日 SIP 处理数量" /><div class="kpi">{{ number(sipTodayTotal) }}</div><p>{{ dashboardSummary?.sip?.status === "empty" ? "今日暂无 SIP 事务样本" : sipTodayTotal == null ? "持久化统计暂不可用" : `失败 ${number(sipTodayFailures)} 条${sipCoveragePartial ? " · 部分数据" : ""}` }}</p><MiniTrend :values="sipTodayTrend" color="var(--uvp-brand-cyan)" />
          </template>
          <template v-else-if="widget.id === 'play-success-24h'">
            <CardTitle icon="play" title="点播成功率（24H）" /><div class="kpi">{{ percent(inviteSuccessRate) }}</div><p>{{ playSuccessDescription }}</p><MiniTrend :values="playSuccessTrend" color="var(--uvp-brand)" />
          </template>
          <template v-else-if="widget.id === 'media-traffic-today'">
            <CardTitle icon="traffic" title="今日媒体流量" /><div class="kpi">{{ selectedTrafficValue == null ? "--" : bytes(selectedTrafficValue) }}</div>
            <p v-if="selectedTrafficValue != null">{{ trafficDescription }}<span v-if="trafficCoveragePartial"> · 统计覆盖不完整</span></p><p v-else>{{ trafficDescription }}暂不可用</p>
            <div class="traffic-legend" role="tablist" aria-label="今日媒体流量方向"><button type="button" role="tab" :aria-selected="trafficDirection === 'upstream'" :class="{ active: trafficDirection === 'upstream' }" @click.stop="trafficDirection = 'upstream'"><i class="up" />上行</button><button type="button" role="tab" :aria-selected="trafficDirection === 'downstream'" :class="{ active: trafficDirection === 'downstream' }" @click.stop="trafficDirection = 'downstream'"><i class="down" />下行</button></div>
            <MiniTrend :values="selectedTrafficTrend" :color="trafficTrendColor" />
          </template>
          <template v-else-if="widget.id === 'media-runtime'">
            <CardTitle icon="server" title="流媒体运行态" /><span v-if="mediaRuntimeCoverageText" class="runtime-coverage" :class="mediaRuntimeStatus">{{ mediaRuntimeCoverageText }}</span>
            <div class="runtime"><button type="button" @click.stop="openMediaRuntimeLedger('streams')"><small>在线流</small><strong>{{ number(runtimeMetric(mediaRuntimeLedger.totals.streams)) }}</strong></button><button type="button" @click.stop="openMediaRuntimeLedger('viewers')"><small>观看者</small><strong>{{ number(runtimeMetric(mediaRuntimeLedger.totals.viewers)) }}</strong></button><button type="button" @click.stop="openMediaRuntimeLedger('sessions')"><small>网络会话</small><strong>{{ number(runtimeMetric(mediaRuntimeLedger.totals.sessions)) }}</strong></button><button type="button" @click.stop="openMediaRuntimeLedger('recordings')"><small>录制中</small><strong>{{ number(runtimeMetric(mediaRuntimeLedger.totals.recordings)) }}</strong></button></div>
          </template>
          <template v-else-if="widget.id === 'sip-monitor'"><SipDashboardCard class="embedded" /></template>
          <template v-else-if="widget.id === 'device-online-rate'"><CardTitle icon="device" title="设备在线率" /><OnlineDonut :online="devices.online" :total="devices.total" label="设备在线率" /></template>
          <template v-else-if="widget.id === 'channel-online-rate'"><CardTitle icon="channel" title="通道在线率" /><OnlineDonut :online="channels.online" :total="channels.total" label="通道在线率" /></template>
          <template v-else-if="widget.id === 'platform-info'">
            <CardTitle icon="platform" title="平台信息" />
            <div class="platform-info">
              <span><small>平台版本</small><strong>{{ APP_VERSION_TEXT }}</strong></span>
              <span><small>平台运行时间</small><strong>{{ platformUptime }}</strong></span>
              <span class="platform-clock"><small>当前时间</small><em>{{ platformClockDate }}</em><span class="clock-time-shell" role="timer" :aria-label="platformClockTime"><span class="clock-unit-shell"><Transition name="clock-tick"><strong :key="platformClockHour" class="clock-unit">{{ platformClockHour }}</strong></Transition></span><b>:</b><span class="clock-unit-shell"><Transition name="clock-tick"><strong :key="platformClockMinute" class="clock-unit">{{ platformClockMinute }}</strong></Transition></span><b>:</b><span class="clock-unit-shell"><Transition name="clock-tick"><strong :key="platformClockSecond" class="clock-unit">{{ platformClockSecond }}</strong></Transition></span></span></span>
            </div>
          </template>
          <template v-else-if="widget.id === 'media-rate'">
            <CardTitle icon="rate" title="媒体实时速率" />
            <div class="rate-head">
              <div class="rate-metrics" aria-label="当前媒体实时速率">
                <span class="rate-metric"><span class="rate-label"><i class="up" />实时上行</span><strong>{{ bytes(mediaRateSnapshot.upstream) }}<small>/s</small></strong></span>
                <span class="rate-metric"><span class="rate-label"><i class="down" />实时下行</span><strong>{{ bytes(mediaRateSnapshot.downstream) }}<small>/s</small></strong></span>
              </div>
              <span class="rate-sample-state">{{ mediaRateExact ? "全部媒体节点 · 5 秒采样" : "数据不完整 · 等待全部媒体节点" }}</span>
            </div>
            <MediaRateArea :samples="mediaRateSamples" />
          </template>
          <template v-else-if="widget.id === 'media-node-health'">
            <CardTitle icon="health" title="媒体节点健康" /><div class="health-content"><div class="health-total"><strong>{{ healthyNodes }}/{{ mediaOverview?.nodes.length ?? 0 }}</strong><span>节点正常</span></div>
              <div class="health-legend"><span><i class="ok" />正常 {{ healthyNodes }}</span><span><i class="warn" />维护 {{ maintenanceNodes }}</span><span><i class="bad" />离线 {{ offlineNodes }}</span></div></div>
          </template>
          <template v-else-if="widget.id === 'active-stream-ranking'">
            <CardTitle icon="ranking" title="活跃流排行" /><div class="ranking"><div v-for="(item, index) in streamRanking.slice(0, 8)" :key="item.key"><span class="rank">{{ index + 1 }}</span><span class="stream"><strong>{{ item.name }}</strong><small>{{ item.app }} · 节点 {{ item.nodeId }}</small></span><span class="viewer">{{ item.readers }} 观看</span><strong>{{ bytes(item.bytesSpeed) }}/s</strong></div><p v-if="!streamRanking.length" class="empty">暂无活跃媒体流</p></div>
          </template>
        </div>
      </article>
    </main>
    <DashboardDrilldownDialog
      :visible="drilldown.visible.value" :metric="drilldown.metric.value" :range="drilldown.range.value" :ranges="drilldown.ranges.value"
      :loading="drilldown.loading.value" :stale="drilldown.stale.value" :error="drilldown.error.value" :result="drilldown.result.value"
      @close="drilldown.close" @range="drilldown.setRange" @retry="drilldown.reload"
    />
    <MediaRuntimeLedgerDialog :visible="mediaLedgerVisible" :kind="mediaLedgerKind" :ledger="mediaRuntimeLedger" @close="mediaLedgerVisible = false" @kind="mediaLedgerKind = $event" @refresh="refreshData" />
  </div>
</template>

<script setup lang="ts">
import dayjs from "dayjs";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { GripHorizontal, LayoutDashboard, LoaderCircle, Pencil, RefreshCw, RotateCcw, Save } from "lucide-vue-next";
import "gridstack/dist/gridstack.min.css";
import { fetchSipDashboardSnapshot, fetchSipPlatformInfo, type DashboardSnapshot, type SipPlatformInfo } from "@/api/gb28181";
import { getZLMOverview, type ZLMMediaRateSample, type ZLMOverview } from "@/api/gb28181-zlm-runtime";
import { getHomeDashboardLayout, getHomeDashboardSummary, resetHomeDashboardLayout, saveHomeDashboardLayout, type HomeDashboardSummary } from "@/api/home-dashboard";
import { createDashboardGrid, type DashboardGridHandle } from "./dashboardGridAdapter";
import { APP_VERSION_TEXT } from "@/config/version";
import { DASHBOARD_WIDGET_REGISTRY, DEFAULT_DASHBOARD_LAYOUT, normalizeDashboardLayout, type DashboardLayout, type DashboardWidgetId } from "./dashboardRegistry";
import CardTitle from "./components/dashboard/CardTitle.vue";
import MiniTrend from "./components/dashboard/MiniTrend.vue";
import MediaRateArea from "./components/dashboard/MediaRateArea.vue";
import OnlineDonut from "./components/dashboard/OnlineDonut.vue";
import SipDashboardCard from "./components/sip-dashboard/index.vue";
import DashboardDrilldownDialog from "./components/drilldown/DashboardDrilldownDialog.vue";
import MediaRuntimeLedgerDialog from "./components/drilldown/MediaRuntimeLedgerDialog.vue";
import { handleDashboardCardDrilldownKeydown, isDashboardDrilldownWidget, openDashboardCardDrilldown } from "./dashboardCardDrilldown";
import { buildMediaRuntimeLedger, type MediaRuntimeLedgerKind } from "./dashboardDrilldownState";
import { useDashboardDrilldown } from "./useDashboardDrilldown";
import { useUserStoreHook } from "@/store/modules/user";

defineOptions({ name: "Home" });
const gridElement = ref<HTMLElement | null>(null);
const layout = ref<DashboardLayout>(clone(DEFAULT_DASHBOARD_LAYOUT));
const savedLayout = ref<DashboardLayout>(clone(DEFAULT_DASHBOARD_LAYOUT));
const revision = ref(0), editing = ref(false), saving = ref(false), refreshing = ref(false), layoutLoading = ref(true);
const updatedAt = ref<Date | null>(null), sipSnapshot = ref<DashboardSnapshot | null>(null), mediaOverview = ref<ZLMOverview | null>(null);
const mediaRuntimeStatus = ref<"loading" | "complete" | "partial" | "unavailable">("loading");
const clockNow = ref(new Date());
const platformInfo = ref<SipPlatformInfo | null>(null);
const dashboardSummary = ref<HomeDashboardSummary | null>(null);
const devices = ref({ total: 0, online: 0 }), channels = ref({ total: 0, online: 0 });
type TrafficDirection = "upstream" | "downstream";
const trafficDirection = ref<TrafficDirection>("upstream");
const drilldown = useDashboardDrilldown();
const mediaLedgerVisible = ref(false), mediaLedgerKind = ref<MediaRuntimeLedgerKind>("streams");
const playSuccessTrend = ref<number[]>([]), mediaRateSamples = ref<ZLMMediaRateSample[]>([]), upstreamTrafficTrend = ref<number[]>([]), downstreamTrafficTrend = ref<number[]>([]);
let grid: DashboardGridHandle | null = null, timer: ReturnType<typeof setInterval> | null = null, clockTimer: ReturnType<typeof setInterval> | null = null;

const permissions = computed(() => useUserStoreHook().account.permissions ?? []);
const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canEditLayout = computed(() => hasPermission("gb28181:home:layout:save") || hasPermission("gb28181:home:layout:reset"));
const canSaveLayout = computed(() => hasPermission("gb28181:home:layout:save"));
const canResetLayout = computed(() => hasPermission("gb28181:home:layout:reset"));
const canViewSipConfig = computed(() => hasPermission("gb28181:sip:config:view"));

const visibleWidgets = computed(() => layout.value.widgets.filter(item => item.visible));
const updatedText = computed(() => updatedAt.value ? dayjs(updatedAt.value).format("HH:mm:ss") : "等待首帧");
const sipTrend = computed(() => sipSnapshot.value?.pulse.samples.slice(-24).map(item => item.msgPerSec) ?? []);
const sipSummary = computed(() => {
  const sip = dashboardSummary.value?.sip;
  if (!sip || !["ok", "partial"].includes(sip.status)) return null;
  return sip.data;
});
const sipTodayTrend = computed(() => sipSummary.value?.series.slice(-24).map(item => item.requests) ?? []);
const sipRpm = computed(() => {
  const values = sipSnapshot.value?.pulse.samples ?? [];
  return values.length ? values.reduce((sum, item) => sum + item.msgPerSec, 0) : null;
});
const sipTodayTotal = computed(() => sipSummary.value?.transactions ?? null);
const sipTodayFailures = computed(() => sipSummary.value?.failure ?? null);
const sipCoveragePartial = computed(() => dashboardSummary.value?.sip?.status === "partial");
const playSummary = computed(() => {
  const play = dashboardSummary.value?.play;
  if (!play || !["ok", "partial"].includes(play.status)) return null;
  return play.data;
});
const inviteSuccessRate = computed<number | null>(() => {
  const rate = playSummary.value?.rate;
  return typeof rate === "number" && Number.isFinite(rate) ? Math.min(1, Math.max(0, rate)) : null;
});
const playSuccessDescription = computed(() => {
  const play = dashboardSummary.value?.play;
  if (play?.status === "empty") return "近 24 小时暂无点播样本";
  if (!playSummary.value || inviteSuccessRate.value == null) return play?.data?.staleStarted ? `暂无终态样本 · ${number(play.data.staleStarted)} 条超时未终态` : "近 24 小时点播统计暂不可用";
  const incomplete = playSummary.value.staleStarted ? ` · ${number(playSummary.value.staleStarted)} 条超时未终态` : "";
  return `近 24 小时 ${number(playSummary.value.success)}/${number(playSummary.value.attempts)} 次媒体流就绪${incomplete}`;
});
const mediaRateExact = computed(() => {
  const sampled = mediaOverview.value?.metrics.mediaTrafficSampledNodes ?? 0;
  return sampled > 0 && sampled === mediaOverview.value?.metrics.sampledNodeCount;
});
const mediaRateSnapshot = computed(() => mediaRateExact.value
  ? { upstream: mediaOverview.value?.metrics.upstreamBytesPerSecond ?? 0, downstream: mediaOverview.value?.metrics.downstreamBytesPerSecond ?? 0 }
  : { upstream: null, downstream: null });
const todayTraffic = computed(() => {
  const traffic = dashboardSummary.value?.traffic;
  if (!traffic || !["ok", "partial"].includes(traffic.status)) return null;
  return traffic.data;
});
const selectedTrafficValue = computed<number | null>(() => todayTraffic.value?.[trafficDirection.value === "upstream" ? "upstreamBytes" : "downstreamBytes"] ?? null);
const selectedTrafficTrend = computed(() => trafficDirection.value === "upstream" ? upstreamTrafficTrend.value : downstreamTrafficTrend.value);
const trafficDescription = computed(() => trafficDirection.value === "upstream" ? "今日累计上行流量" : "今日已结算下行流量");
const trafficTrendColor = computed(() => trafficDirection.value === "upstream" ? "var(--uvp-brand)" : "var(--uvp-brand-cyan)");
const trafficCoveragePartial = computed(() => dashboardSummary.value?.traffic?.status === "partial");
const mediaRuntimeLedger = computed(() => buildMediaRuntimeLedger(mediaOverview.value, dashboardSummary.value?.bindings?.data));
const mediaRuntimeCoverageText = computed(() => mediaRuntimeStatus.value === "unavailable" ? "数据不可用" : mediaRuntimeLedger.value.partial || mediaRuntimeStatus.value === "partial" ? "部分数据" : "");
const platformUptime = computed(() => {
  if (!canViewSipConfig.value) return "--";
  const startedAt = Date.parse(platformInfo.value?.runtime.startedAt ?? "");
  if (!Number.isFinite(startedAt)) return platformInfo.value?.runtime.state === "running" ? "等待启动时间" : "未运行";
  return duration(Math.max(0, (updatedAt.value?.getTime() ?? Date.now()) - startedAt));
});
const platformClockDate = computed(() => dayjs(clockNow.value).format("YYYY年MM月DD日"));
const platformClockTime = computed(() => dayjs(clockNow.value).format("HH:mm:ss"));
const platformClockHour = computed(() => dayjs(clockNow.value).format("HH"));
const platformClockMinute = computed(() => dayjs(clockNow.value).format("mm"));
const platformClockSecond = computed(() => dayjs(clockNow.value).format("ss"));
const healthyNodes = computed(() => mediaOverview.value?.nodes.filter(item => item.state === "active" && item.status === "fresh").length ?? 0);
const maintenanceNodes = computed(() => mediaOverview.value?.nodes.filter(item => item.state === "maintenance").length ?? 0);
const offlineNodes = computed(() => mediaOverview.value?.nodes.filter(item => item.state === "offline").length ?? 0);
const streamRanking = computed(() => {
  const grouped = new Map<string, { key: string; name: string; app: string; nodeId: number; readers: number; bytesSpeed: number }>();
  for (const item of mediaOverview.value?.streams ?? []) { const key = `${item.nodeId}/${item.media.vhost}/${item.media.app}/${item.media.stream}`; const value = grouped.get(key) ?? { key, name: item.media.stream, app: item.media.app, nodeId: item.nodeId, readers: 0, bytesSpeed: 0 }; value.readers += item.readerCount; value.bytesSpeed += item.bytesSpeed; grouped.set(key, value); }
  const values = [...grouped.values()].sort((a, b) => b.readers - a.readers || b.bytesSpeed - a.bytesSpeed); const max = Math.max(...values.map(item => item.bytesSpeed), 1);
  return values.map(item => ({ ...item, bar: Math.max(4, Math.round(item.bytesSpeed / max * 100)) }));
});

function clone(value: DashboardLayout): DashboardLayout { return { schemaVersion: value.schemaVersion, widgets: value.widgets.map(item => ({ ...item, settings: {} })) }; }
function definition(id: DashboardWidgetId) { return DASHBOARD_WIDGET_REGISTRY.find(item => item.layout.id === id)!; }
function widgetTitle(id: DashboardWidgetId): string { return ({ "sip-rpm": "实时 SIP RPM", "sip-today": "今日 SIP 处理数量", "play-success-24h": "点播成功率", "media-traffic-today": "今日媒体流量", "media-runtime": "流媒体运行态", "sip-monitor": "GB28181 SIP 协议监控", "device-online-rate": "设备在线率", "channel-online-rate": "通道在线率", "media-rate": "媒体实时速率", "media-node-health": "媒体节点健康", "active-stream-ranking": "活跃流排行", "platform-info": "平台信息" })[id]; }
function openMediaRuntimeLedger(kind: MediaRuntimeLedgerKind) { if (editing.value) return; mediaLedgerKind.value = kind; mediaLedgerVisible.value = true; }
function openWidgetDrilldown(id: DashboardWidgetId) { openDashboardCardDrilldown(id, editing.value, metric => { void drilldown.open(metric); }, () => openMediaRuntimeLedger("streams")); }
function onWidgetDrilldownKeydown(event: KeyboardEvent, id: DashboardWidgetId) { handleDashboardCardDrilldownKeydown(event, id, editing.value, metric => { void drilldown.open(metric); }, () => openMediaRuntimeLedger("streams")); }

async function loadLayout() {
  try { const response = await getHomeDashboardLayout(); layout.value = normalizeDashboardLayout(response.data.layout); revision.value = response.data.revision; }
  catch { layout.value = clone(DEFAULT_DASHBOARD_LAYOUT); }
  savedLayout.value = clone(layout.value); layoutLoading.value = false; await nextTick(); initGrid();
}
async function refreshData() {
  if (refreshing.value) return; refreshing.value = true;
  const results = await Promise.allSettled([fetchSipDashboardSnapshot({ window: "60s", precision: "1s" }), getZLMOverview(), canViewSipConfig.value ? fetchSipPlatformInfo() : Promise.resolve(null), getHomeDashboardSummary(["assets", "aggregate"])]);
  const [sip, overview, platform, summary] = results;
  if (sip.status === "fulfilled") sipSnapshot.value = sip.value.data;
  if (overview.status === "fulfilled") {
    mediaOverview.value = overview.value.data;
    mediaRuntimeStatus.value = overview.value.data.partial ? "partial" : "complete";
    mediaRateSamples.value = overview.value.data.mediaRateSamples ?? [];
  } else mediaRuntimeStatus.value = "unavailable";
  if (!canViewSipConfig.value) platformInfo.value = null;
  else if (platform.status === "fulfilled" && platform.value) platformInfo.value = platform.value.data;
  if (summary.status === "fulfilled") {
    dashboardSummary.value = summary.value.data;
    const assets = dashboardSummary.value.assets;
    if (assets && ["ok", "partial", "empty"].includes(assets.status)) {
      devices.value = { total: assets.data.devices.total, online: assets.data.devices.online };
      channels.value = { total: assets.data.channels.total, online: assets.data.channels.online };
    }
    if (inviteSuccessRate.value != null) playSuccessTrend.value = [...playSuccessTrend.value.slice(-23), inviteSuccessRate.value];
    if (todayTraffic.value) {
      upstreamTrafficTrend.value = [...upstreamTrafficTrend.value.slice(-23), todayTraffic.value.upstreamBytes];
      downstreamTrafficTrend.value = [...downstreamTrafficTrend.value.slice(-23), todayTraffic.value.downstreamBytes];
    }
  } else dashboardSummary.value = null;
  updatedAt.value = new Date(); refreshing.value = false;
}
function initGrid() {
  grid?.destroy();
  // GridStack mutates DOM attributes; restore canonical geometry before rebuilding.
  for (const element of Array.from(gridElement.value?.children ?? [])) {
    const item = layout.value.widgets.find(widget => widget.id === element.getAttribute("gs-id"));
    if (!item) continue;
    const limits = definition(item.id);
    for (const [key, value] of Object.entries({ x: item.x, y: item.y, w: item.w, h: item.h, "min-w": limits.minW, "max-w": limits.maxW, "min-h": limits.minH, "max-h": limits.maxH })) element.setAttribute(`gs-${key}`, String(value));
  }
  grid = gridElement.value ? createDashboardGrid(gridElement.value, undefined, { onChange(items) {
    for (const value of items) {
      const item = layout.value.widgets.find(widget => widget.id === value.id);
      if (item) Object.assign(item, value);
    }
  } }) : null;
  grid?.setEditing(editing.value);
}
async function rebuildGrid() { await nextTick(); initGrid(); }
function startEditing() { if (!canEditLayout.value) return; savedLayout.value = clone(layout.value); editing.value = true; grid?.setEditing(true); }
function cancelEditing() { layout.value = clone(savedLayout.value); editing.value = false; rebuildGrid(); }
async function saveLayout() { if (!canSaveLayout.value) return; saving.value = true; try { const response = await saveHomeDashboardLayout(revision.value, normalizeDashboardLayout(layout.value)); revision.value = response.data.revision; layout.value = normalizeDashboardLayout(response.data.layout); savedLayout.value = clone(layout.value); editing.value = false; Message.success("仪表盘布局已保存"); await rebuildGrid(); } catch (error: any) { error?.response?.status === 409 ? Message.warning("布局已在其他页面更新，请刷新后重试") : Message.error("仪表盘布局保存失败"); } finally { saving.value = false; } }
async function resetLayout() { if (!canResetLayout.value) return; try { const response = await resetHomeDashboardLayout(); revision.value = response.data.revision; layout.value = normalizeDashboardLayout(response.data.layout); savedLayout.value = clone(layout.value); Message.success("已恢复默认布局"); await rebuildGrid(); } catch { Message.error("恢复默认布局失败"); } }
function number(value: number | null | undefined) { return value == null || !Number.isFinite(value) ? "--" : value.toLocaleString("zh-CN"); }
function runtimeMetric(value: number) { return mediaOverview.value ? value : null; }
function percent(value: number | null | undefined) { return value == null ? "--" : `${(value * 100).toFixed(1)}%`; }
function bytes(value: number | null | undefined) { if (value == null || !Number.isFinite(value)) return "--"; if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`; if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`; if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`; return `${value.toFixed(0)} B`; }
function duration(milliseconds: number) { const totalMinutes = Math.floor(milliseconds / 60_000); const days = Math.floor(totalMinutes / 1440), hours = Math.floor(totalMinutes % 1440 / 60), minutes = totalMinutes % 60; return days ? `${days} 天 ${hours} 小时` : hours ? `${hours} 小时 ${minutes} 分钟` : `${minutes} 分钟`; }
onMounted(async () => { await Promise.all([loadLayout(), refreshData()]); timer = setInterval(refreshData, 5_000); clockTimer = setInterval(() => { clockNow.value = new Date(); }, 1_000); });
onBeforeUnmount(() => { if (timer) clearInterval(timer); if (clockTimer) clearInterval(clockTimer); grid?.destroy(); });
</script>

<style scoped lang="scss">
.dashboard-shell{min-height:100%;padding:18px;color:var(--uvp-text-primary)}.dashboard-header{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}.dashboard-title{display:flex;gap:12px;align-items:center}.dashboard-title h1{margin:1px 0 0;font-size:24px}.dashboard-title small,.card p{color:var(--uvp-text-tertiary)}.title-icon{display:grid;width:42px;height:42px;color:var(--uvp-brand);background:var(--uvp-brand-soft);border-radius:9px;place-items:center}.actions{display:flex;gap:8px;align-items:center}.actions button{display:inline-flex;gap:6px;align-items:center;height:36px;padding:0 13px;font:inherit;cursor:pointer;border-radius:8px}.secondary{color:var(--uvp-text-primary);background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border)}.primary{color:#fff;background:var(--uvp-brand);border:0}.actions .primary{font-weight:600}.live{display:flex;gap:7px;align-items:center;margin-right:4px;font-size:12px;color:var(--uvp-text-secondary)}.live i{width:7px;height:7px;background:var(--uvp-brand-cyan);border-radius:50%;box-shadow:0 0 0 4px color-mix(in srgb,var(--uvp-brand-cyan) 12%,transparent)}
.widget-picker{display:flex;gap:14px;align-items:center;padding:10px 14px;margin-bottom:12px;overflow-x:auto;font-size:12px;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:9px}.widget-picker label{display:inline-flex;gap:5px;align-items:center;white-space:nowrap}.widget-picker>span{margin-left:auto;color:var(--uvp-text-tertiary);white-space:nowrap}.loading{display:grid;min-height:420px;color:var(--uvp-text-secondary);place-content:center;justify-items:center;gap:10px}.dashboard-grid{margin:-6px}.card{position:relative;box-sizing:border-box;padding:15px;overflow:hidden;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:var(--uvp-panel-radius);box-shadow:var(--uvp-panel-shadow)}.dashboard-grid>.grid-stack-item>.grid-stack-item-content.card{overflow:hidden}.drilldown-card[role="button"]{cursor:pointer;transition:border-color .16s ease,box-shadow .16s ease}.drilldown-card[role="button"]:hover{border-color:color-mix(in srgb,var(--uvp-brand) 45%,var(--uvp-panel-border))}.drilldown-card[role="button"]:focus-visible{outline:2px solid var(--uvp-brand);outline-offset:2px}.editing .card{border-color:color-mix(in srgb,var(--uvp-brand) 45%,var(--uvp-panel-border))}.editing .drilldown-card{cursor:move}.drag{position:absolute;top:4px;right:7px;z-index:3;color:var(--uvp-text-tertiary);cursor:move}.kpi{margin-top:25px;font-size:29px;font-weight:700;line-height:1}.kpi small{margin-left:3px;font-size:12px;font-weight:500;color:var(--uvp-text-tertiary)}.card p{margin-top:9px;font-size:11px}
.traffic-legend{position:absolute;bottom:13px;left:15px;display:flex;gap:10px}.traffic-legend button{display:inline-flex;gap:4px;align-items:center;padding:0;font:inherit;font-size:10px;color:var(--uvp-text-tertiary);cursor:pointer;background:none;border:0}.traffic-legend button.active{color:var(--uvp-text-primary)}.traffic-legend i{width:7px;height:7px;border-radius:50%}.traffic-legend .up{background:var(--uvp-brand)}.traffic-legend .down{background:var(--uvp-brand-cyan)}
.runtime-coverage{position:absolute;top:14px;right:14px;padding:2px 6px;font-size:10px;color:var(--uvp-warning);background:color-mix(in srgb,var(--uvp-warning) 10%,transparent);border-radius:999px}.runtime-coverage.unavailable{color:var(--uvp-danger);background:color-mix(in srgb,var(--uvp-danger) 10%,transparent)}.runtime{display:grid;grid-template-columns:repeat(2,1fr);gap:6px 12px;margin-top:10px}.runtime button{display:flex;flex-direction:column;gap:2px;padding:3px;color:inherit;text-align:left;cursor:pointer;background:transparent;border:0;border-radius:5px}.runtime button:hover,.runtime button:focus-visible{background:var(--uvp-brand-soft);outline:none}.runtime small{font-size:10px;line-height:1;color:var(--uvp-text-tertiary)}.runtime strong{font-size:20px;line-height:1.05}.widget-media-node-health .card,.widget-platform-info .card{display:flex;flex-direction:column}.platform-info{display:grid;flex:1;grid-template-columns:.8fr .9fr 1.3fr;gap:18px;align-items:center;margin-top:7px}.platform-info>span{display:flex;flex-direction:column;gap:5px;align-items:center;min-width:0;text-align:center}.platform-info small{font-size:11px;color:var(--uvp-text-tertiary)}.platform-info strong{font-size:20px;white-space:nowrap}.platform-clock em{font-size:10px;font-style:normal;color:var(--uvp-text-tertiary);white-space:nowrap}.clock-time-shell{display:flex;align-items:center;justify-content:center;width:108px;height:24px;font-variant-numeric:tabular-nums}.clock-time-shell>b{width:8px;font-size:20px;font-weight:600;line-height:24px}.clock-unit-shell{position:relative;width:28px;height:24px;overflow:hidden}.clock-unit{position:absolute;inset:0;display:block;font-size:20px;line-height:24px;letter-spacing:.04em}.clock-tick-enter-active,.clock-tick-leave-active{transition:opacity .18s ease,transform .18s ease,filter .18s ease}.clock-tick-enter-from{opacity:0;filter:blur(2px);transform:translateY(6px)}.clock-tick-leave-to{opacity:0;filter:blur(2px);transform:translateY(-6px)}.embedded{height:100%;gap:10px;padding:0;border:0;box-shadow:none}.embedded :deep(.pulse){flex:none}.embedded :deep(.pulse__chart){flex:none;height:90px;min-height:90px}.rate-head{display:flex;gap:12px;align-items:flex-end;justify-content:space-between;margin:14px 0 6px}.rate-metrics{display:flex;gap:22px;min-width:0}.rate-metric{display:flex;flex-direction:column;gap:3px;min-width:0}.rate-label{display:inline-flex;gap:5px;align-items:center;font-size:10px;color:var(--uvp-text-tertiary)}.rate-label i{width:7px;height:7px;flex:0 0 7px;border-radius:50%}.rate-label .up{background:var(--uvp-brand)}.rate-label .down{background:var(--uvp-brand-cyan)}.rate-metric strong{font-size:20px;line-height:1;white-space:nowrap}.rate-metric small{margin-left:2px;font-size:10px;font-weight:500;color:var(--uvp-text-tertiary)}.rate-sample-state{font-size:10px;color:var(--uvp-text-tertiary);white-space:nowrap}
.health-content{display:flex;flex:1;flex-direction:column;justify-content:center}.health-total{display:flex;align-items:baseline;justify-content:center;gap:7px;margin:0 0 24px}.health-total strong{font-size:32px}.health-total span{font-size:11px;color:var(--uvp-text-tertiary)}.health-legend{display:flex;justify-content:space-around;font-size:11px;color:var(--uvp-text-secondary)}.health-legend i{display:inline-block;width:7px;height:7px;margin-right:4px;border-radius:50%}.health-legend .ok{background:var(--uvp-brand-cyan)}.health-legend .warn{background:var(--uvp-warning)}.health-legend .bad{background:var(--uvp-danger)}.ranking{margin-top:14px}.ranking>div{display:grid;grid-template-columns:26px 1fr 90px 110px;gap:10px;align-items:center;min-height:38px;font-size:11px;border-bottom:1px solid var(--uvp-panel-border)}.rank{color:var(--uvp-text-tertiary);text-align:center}.stream{display:flex;flex-direction:column;min-width:0}.stream strong,.stream small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.stream small{color:var(--uvp-text-tertiary)}.viewer{color:var(--uvp-brand-cyan)}.ranking>div>strong{text-align:right}.empty{display:grid;min-height:90px;color:var(--uvp-text-tertiary);place-items:center}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
@media(max-width:900px){.dashboard-header{align-items:flex-start}.actions{flex-wrap:wrap;justify-content:flex-end}.live{width:100%;justify-content:flex-end}}

/* Container rules also cover user-resized desktop cards. */
.card{container-type:inline-size}
.compact-edit-hint{display:none}
.runtime{row-gap:4px;margin-top:6px}
.widget-media-rate .card{display:flex;flex-direction:column}
.widget-media-rate :deep(.media-rate-area){flex:1;height:auto;min-height:0}
.rate-head{flex-wrap:wrap}
.embedded :deep(.pulse__head){flex-wrap:wrap;gap:8px}
.embedded :deep(.pulse__legend){flex-wrap:wrap}
.embedded :deep(.pulse__legend>span){white-space:nowrap}
@container(max-width:260px){
  .mini-trend{width:clamp(24px,calc(100cqw - 128px),64px)}
}
@container(max-width:520px){
  .embedded :deep(.summary-bar){grid-template-columns:1fr;gap:10px}
  .embedded :deep(.summary-bar__divider){display:none}
  .embedded :deep(.summary-bar__stats){gap:28px}
  .embedded :deep(.pulse__stats){display:block;margin:4px 0 0}
  .embedded :deep(.tx-grid){grid-template-columns:repeat(2,minmax(0,1fr))}
  .embedded :deep(.tx-cell__en){display:block;margin-left:0}
}
@container(max-width:400px){
  .platform-info{grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
  .platform-info>.platform-clock{grid-column:1/-1}
  .platform-info>span>strong{font-size:17px;white-space:normal;overflow-wrap:anywhere}
  .ranking>div{grid-template-columns:20px minmax(0,1fr) auto;gap:6px}
  .ranking>div>strong{grid-column:2/-1;font-size:11px}
}
@media(max-width:1279px){
  .desktop-edit-hint,.drag{display:none}
  .widget-picker{flex-wrap:wrap;gap:12px;overflow:visible}
  .widget-picker>.compact-edit-hint{display:block;width:100%;margin-left:0}
}
@media(max-width:767px){
  .dashboard-shell{padding:12px}
  .dashboard-header{flex-direction:column;gap:12px}
  .dashboard-title{min-width:0}
  .actions{width:100%;justify-content:flex-start;flex-wrap:wrap}
  .live{justify-content:flex-start}
  .actions button{min-height:40px}
  .widget-picker label{min-height:32px}
  .dashboard-grid.gs-1{display:flex;flex-direction:column;gap:12px;height:auto!important;margin:0}
  .dashboard-grid.gs-1>.grid-stack-item{position:relative;inset:auto!important;width:100%!important;height:auto!important}
  .dashboard-grid.gs-1>.grid-stack-item>.card{position:relative;inset:auto;min-height:160px}
  .dashboard-grid.gs-1>.widget-media-rate>.card{height:340px}
  .dashboard-grid.gs-1>.widget-device-online-rate>.card,.dashboard-grid.gs-1>.widget-channel-online-rate>.card{height:300px}
  .dashboard-grid.gs-1>.widget-media-node-health>.card{height:240px}
  .dashboard-grid.gs-1>.widget-platform-info>.card{min-height:260px}
  .dashboard-grid.gs-1>.widget-sip-monitor .embedded{height:auto}
  .traffic-legend button{min-height:32px}
}
</style>
