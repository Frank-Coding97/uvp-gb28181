<template>
  <div class="dashboard-shell">
    <header class="dashboard-header">
      <div class="dashboard-title"><span class="title-icon"><LayoutDashboard :size="20" /></span><div><small>GB28181 视频接入平台</small><h1>仪表盘</h1></div></div>
      <div class="actions">
        <span class="live"><i />实时数据 · {{ updatedText }}</span>
        <button class="secondary" :disabled="refreshing" @click="refreshData"><RefreshCw :size="15" :class="{ spin: refreshing }" />刷新</button>
        <button v-if="!editing" class="primary" @click="startEditing"><Pencil :size="15" />编辑仪表盘</button>
        <template v-else>
          <button class="secondary" @click="resetLayout"><RotateCcw :size="15" />恢复默认</button>
          <button class="secondary" @click="cancelEditing">取消</button>
          <button class="primary" :disabled="saving" @click="saveLayout"><Save :size="15" />{{ saving ? "保存中" : "保存布局" }}</button>
        </template>
      </div>
    </header>

    <section v-if="editing" class="widget-picker">
      <strong>组件</strong>
      <label v-for="widget in layout.widgets" :key="widget.id"><input v-model="widget.visible" type="checkbox" @change="rebuildGrid" />{{ widgetTitle(widget.id) }}</label>
      <span>拖动卡片调整位置，拖拽边缘调整大小</span>
    </section>

    <div v-if="layoutLoading" class="loading"><LoaderCircle class="spin" :size="24" />正在加载仪表盘</div>
    <main v-else ref="gridElement" class="grid-stack dashboard-grid" :class="{ editing }">
      <article v-for="widget in visibleWidgets" :key="widget.id" class="grid-stack-item" :class="`widget-${widget.id}`"
        :gs-id="widget.id" :gs-x="widget.x" :gs-y="widget.y" :gs-w="widget.w" :gs-h="widget.h"
        :gs-min-w="definition(widget.id).minW" :gs-max-w="definition(widget.id).maxW"
        :gs-min-h="definition(widget.id).minH" :gs-max-h="definition(widget.id).maxH">
        <div class="grid-stack-item-content card">
          <span v-if="editing" class="drag"><GripHorizontal :size="16" /></span>
          <template v-if="widget.id === 'sip-rpm'">
            <CardTitle icon="activity" title="实时 SIP RPM" /><div class="kpi">{{ number(sipRpm) }}<small>/min</small></div><p>最近一分钟处理速率</p><MiniTrend :values="sipTrend" color="var(--uvp-warning)" />
          </template>
          <template v-else-if="widget.id === 'sip-today'">
            <CardTitle icon="radio" title="今日 SIP 处理数量" /><div class="kpi">{{ number(sipTodayTotal) }}</div><p>{{ sipTodayTotal == null ? "暂无连续采样" : `异常 ${number(sipSnapshot?.todayAbnormal)} 条` }}</p><MiniTrend :values="sipTodayTrend" color="var(--uvp-brand-cyan)" />
          </template>
          <template v-else-if="widget.id === 'play-success-24h'">
            <CardTitle icon="play" title="点播成功率（24H）" /><div class="kpi">{{ percent(inviteSuccessRate) }}</div><p>{{ inviteSuccessRate == null ? "暂无完整 24H 样本" : "近 24 小时媒体可播放成功率" }}</p><MiniTrend :values="playSuccessTrend" color="var(--uvp-brand)" />
          </template>
          <template v-else-if="widget.id === 'media-traffic-today'">
            <CardTitle icon="traffic" title="今日媒体流量" /><div class="kpi">{{ selectedTrafficValue == null ? "--" : bytes(selectedTrafficValue) }}</div>
            <p v-if="selectedTrafficValue != null">今日累计{{ trafficDirectionLabel }}流量<span v-if="trafficCoveragePartial"> · 统计覆盖不完整</span></p><p v-else>今日累计{{ trafficDirectionLabel }}流量暂不可用</p>
            <div class="traffic-legend" role="tablist" aria-label="今日媒体流量方向"><button type="button" role="tab" :aria-selected="trafficDirection === 'upstream'" :class="{ active: trafficDirection === 'upstream' }" @click="trafficDirection = 'upstream'"><i class="up" />上行</button><button type="button" role="tab" :aria-selected="trafficDirection === 'downstream'" :class="{ active: trafficDirection === 'downstream' }" @click="trafficDirection = 'downstream'"><i class="down" />下行</button></div>
            <MiniTrend :values="selectedTrafficTrend" :color="trafficTrendColor" />
          </template>
          <template v-else-if="widget.id === 'media-runtime'">
            <CardTitle icon="server" title="流媒体运行态" />
            <div class="runtime"><span><small>在线流</small><strong>{{ number(mediaOverview?.metrics.streamCount) }}</strong></span><span><small>观看者</small><strong>{{ number(viewers) }}</strong></span><span><small>网络会话</small><strong>{{ number(mediaOverview?.metrics.networkSessionCount) }}</strong></span><span><small>录制中</small><strong>{{ number(recordings) }}</strong></span></div>
          </template>
          <template v-else-if="widget.id === 'sip-monitor'"><SipDashboardCard class="embedded" /></template>
          <template v-else-if="widget.id === 'device-online-rate'"><CardTitle icon="device" title="设备在线率" /><OnlineDonut :online="devices.online" :total="devices.total" label="设备在线率" /></template>
          <template v-else-if="widget.id === 'channel-online-rate'"><CardTitle icon="channel" title="通道在线率" /><OnlineDonut :online="channels.online" :total="channels.total" label="通道在线率" /></template>
          <template v-else-if="widget.id === 'platform-info'">
            <CardTitle icon="platform" title="平台信息" />
            <div class="platform-info"><span><small>平台版本</small><strong>{{ platformInfo?.version ? `v${platformInfo.version}` : "--" }}</strong></span><span><small>平台运行时间</small><strong>{{ platformUptime }}</strong></span></div>
          </template>
          <template v-else-if="widget.id === 'media-rate'">
            <CardTitle icon="rate" title="媒体实时速率" /><div class="rate-head"><strong>{{ bytes(mediaRate) }}/s</strong><span>全部媒体节点 · 5 秒采样</span></div>
            <MediaRateArea :values="mediaRateTrend" color="var(--uvp-brand)" />
          </template>
          <template v-else-if="widget.id === 'media-node-health'">
            <CardTitle icon="health" title="媒体节点健康" /><div class="health-total"><strong>{{ healthyNodes }}/{{ mediaOverview?.nodes.length ?? 0 }}</strong><span>节点正常</span></div>
            <div class="health-legend"><span><i class="ok" />正常 {{ healthyNodes }}</span><span><i class="warn" />维护 {{ maintenanceNodes }}</span><span><i class="bad" />离线 {{ offlineNodes }}</span></div>
          </template>
          <template v-else-if="widget.id === 'active-stream-ranking'">
            <CardTitle icon="ranking" title="活跃流排行" /><div class="ranking"><div v-for="(item, index) in streamRanking.slice(0, 8)" :key="item.key"><span class="rank">{{ index + 1 }}</span><span class="stream"><strong>{{ item.name }}</strong><small>{{ item.app }} · 节点 {{ item.nodeId }}</small></span><span class="viewer">{{ item.readers }} 观看</span><strong>{{ bytes(item.bytesSpeed) }}/s</strong></div><p v-if="!streamRanking.length" class="empty">暂无活跃媒体流</p></div>
          </template>
        </div>
      </article>
    </main>
  </div>
</template>

<script setup lang="ts">
import dayjs from "dayjs";
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { GripHorizontal, LayoutDashboard, LoaderCircle, Pencil, RefreshCw, RotateCcw, Save } from "lucide-vue-next";
import "gridstack/dist/gridstack.min.css";
import { fetchSipDashboardSnapshot, fetchSipPlatformInfo, HEALTH_EMPTY, type DashboardSnapshot, type SipPlatformInfo } from "@/api/gb28181";
import { getZLMOverview, type ZLMOverview } from "@/api/gb28181-zlm-runtime";
import { listChannels, listDevices } from "@/views/gb28181/device-mgmt/api";
import { getHomeDashboardLayout, getHomeDashboardSummary, resetHomeDashboardLayout, saveHomeDashboardLayout, type HomeDashboardSummary } from "@/api/home-dashboard";
import { createDashboardGrid, type DashboardGridHandle } from "./dashboardGridAdapter";
import { DASHBOARD_WIDGET_REGISTRY, DEFAULT_DASHBOARD_LAYOUT, normalizeDashboardLayout, type DashboardLayout, type DashboardWidgetId } from "./dashboardRegistry";
import CardTitle from "./components/dashboard/CardTitle.vue";
import MiniTrend from "./components/dashboard/MiniTrend.vue";
import MediaRateArea from "./components/dashboard/MediaRateArea.vue";
import OnlineDonut from "./components/dashboard/OnlineDonut.vue";
import SipDashboardCard from "./components/sip-dashboard/index.vue";

defineOptions({ name: "Home" });
const gridElement = ref<HTMLElement | null>(null);
const layout = ref<DashboardLayout>(clone(DEFAULT_DASHBOARD_LAYOUT));
const savedLayout = ref<DashboardLayout>(clone(DEFAULT_DASHBOARD_LAYOUT));
const revision = ref(0), editing = ref(false), saving = ref(false), refreshing = ref(false), layoutLoading = ref(true);
const updatedAt = ref<Date | null>(null), sipSnapshot = ref<DashboardSnapshot | null>(null), mediaOverview = ref<ZLMOverview | null>(null);
const platformInfo = ref<SipPlatformInfo | null>(null);
const dashboardSummary = ref<HomeDashboardSummary | null>(null);
const devices = ref({ total: 0, online: 0 }), channels = ref({ total: 0, online: 0 });
type TrafficDirection = "upstream" | "downstream";
const trafficDirection = ref<TrafficDirection>("upstream");
const playSuccessTrend = ref<number[]>([]), mediaRateTrend = ref<number[]>([]), upstreamTrafficTrend = ref<number[]>([]), downstreamTrafficTrend = ref<number[]>([]);
let grid: DashboardGridHandle | null = null, timer: ReturnType<typeof setInterval> | null = null;

const visibleWidgets = computed(() => layout.value.widgets.filter(item => item.visible));
const updatedText = computed(() => updatedAt.value ? dayjs(updatedAt.value).format("HH:mm:ss") : "等待首帧");
const sipTrend = computed(() => sipSnapshot.value?.pulse.samples.slice(-24).map(item => item.msgPerSec) ?? []);
const sipTodayTrend = computed(() => { let total = 0; return sipTrend.value.map(value => (total += value * 60)); });
const sipRpm = computed(() => { const values = sipSnapshot.value?.pulse.samples.slice(-6) ?? []; return values.length ? Math.round(values.reduce((sum, item) => sum + item.msgPerSec, 0) / values.length * 60) : null; });
const sipTodayTotal = computed(() => sipSnapshot.value && sipSnapshot.value.health !== HEALTH_EMPTY ? sipSnapshot.value.todayTotal : null);
const inviteSuccessRate = computed<number | null>(() => null);
const viewers = computed(() => mediaOverview.value?.streams.reduce((sum, item) => sum + item.readerCount, 0) ?? 0);
const recordings = computed(() => mediaOverview.value?.streams.filter(item => item.recordingMp4 || item.recordingHls).length ?? 0);
const mediaRate = computed(() => mediaOverview.value?.streams.reduce((sum, item) => sum + item.bytesSpeed, 0) ?? 0);
const todayTraffic = computed(() => {
  const traffic = dashboardSummary.value?.traffic;
  if (!traffic || !["ok", "partial"].includes(traffic.status)) return null;
  return traffic.data;
});
const selectedTrafficValue = computed<number | null>(() => todayTraffic.value?.[trafficDirection.value === "upstream" ? "upstreamBytes" : "downstreamBytes"] ?? null);
const selectedTrafficTrend = computed(() => trafficDirection.value === "upstream" ? upstreamTrafficTrend.value : downstreamTrafficTrend.value);
const trafficDirectionLabel = computed(() => trafficDirection.value === "upstream" ? "上行" : "下行");
const trafficTrendColor = computed(() => trafficDirection.value === "upstream" ? "var(--uvp-brand-cyan)" : "var(--uvp-danger)");
const trafficCoveragePartial = computed(() => dashboardSummary.value?.traffic?.status === "partial");
const platformUptime = computed(() => {
  const startedAt = Date.parse(platformInfo.value?.runtime.startedAt ?? "");
  if (!Number.isFinite(startedAt)) return platformInfo.value?.runtime.state === "running" ? "等待启动时间" : "未运行";
  return duration(Math.max(0, (updatedAt.value?.getTime() ?? Date.now()) - startedAt));
});
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

async function loadLayout() {
  try { const response = await getHomeDashboardLayout(); layout.value = normalizeDashboardLayout(response.data.layout); revision.value = response.data.revision; }
  catch { layout.value = clone(DEFAULT_DASHBOARD_LAYOUT); }
  savedLayout.value = clone(layout.value); layoutLoading.value = false; await nextTick(); initGrid();
}
async function refreshData() {
  if (refreshing.value) return; refreshing.value = true;
  const results = await Promise.allSettled([fetchSipDashboardSnapshot(), getZLMOverview(), listDevices({ page: 1, pageSize: 1 }), listChannels({ page: 1, pageSize: 1 }), listChannels({ status: "online", page: 1, pageSize: 1 }), fetchSipPlatformInfo(), getHomeDashboardSummary(["aggregate"])]);
  const [sip, overview, devicePage, channelPage, onlineChannels, platform, summary] = results;
  if (sip.status === "fulfilled") sipSnapshot.value = sip.value.data;
  if (overview.status === "fulfilled") { mediaOverview.value = overview.value.data; mediaRateTrend.value = [...mediaRateTrend.value.slice(-23), mediaRate.value]; }
  if (inviteSuccessRate.value != null) playSuccessTrend.value = [...playSuccessTrend.value.slice(-23), inviteSuccessRate.value];
  if (devicePage.status === "fulfilled") devices.value = { total: devicePage.value.data.total ?? 0, online: devicePage.value.data.onlineTotal ?? 0 };
  if (channelPage.status === "fulfilled" && onlineChannels.status === "fulfilled") channels.value = { total: channelPage.value.data.total ?? 0, online: onlineChannels.value.data.total ?? 0 };
  if (platform.status === "fulfilled") platformInfo.value = platform.value.data;
  if (summary.status === "fulfilled") {
    dashboardSummary.value = summary.value.data;
    if (todayTraffic.value) {
      upstreamTrafficTrend.value = [...upstreamTrafficTrend.value.slice(-23), todayTraffic.value.upstreamBytes];
      downstreamTrafficTrend.value = [...downstreamTrafficTrend.value.slice(-23), todayTraffic.value.downstreamBytes];
    }
  }
  updatedAt.value = new Date(); refreshing.value = false;
}
function initGrid() { grid?.destroy(); grid = gridElement.value ? createDashboardGrid(gridElement.value, undefined, { onChange(items) { for (const value of items) { const item = layout.value.widgets.find(widget => widget.id === value.id); if (item) Object.assign(item, value); } } }) : null; grid?.setEditing(editing.value); }
async function rebuildGrid() { await nextTick(); initGrid(); }
function startEditing() { savedLayout.value = clone(layout.value); editing.value = true; grid?.setEditing(true); }
function cancelEditing() { layout.value = clone(savedLayout.value); editing.value = false; rebuildGrid(); }
async function saveLayout() { saving.value = true; try { const response = await saveHomeDashboardLayout(revision.value, normalizeDashboardLayout(layout.value)); revision.value = response.data.revision; layout.value = normalizeDashboardLayout(response.data.layout); savedLayout.value = clone(layout.value); editing.value = false; Message.success("仪表盘布局已保存"); await rebuildGrid(); } catch (error: any) { error?.response?.status === 409 ? Message.warning("布局已在其他页面更新，请刷新后重试") : Message.error("仪表盘布局保存失败"); } finally { saving.value = false; } }
async function resetLayout() { try { const response = await resetHomeDashboardLayout(); revision.value = response.data.revision; layout.value = normalizeDashboardLayout(response.data.layout); savedLayout.value = clone(layout.value); Message.success("已恢复默认布局"); await rebuildGrid(); } catch { Message.error("恢复默认布局失败"); } }
function number(value: number | null | undefined) { return value == null || !Number.isFinite(value) ? "--" : value.toLocaleString("zh-CN"); }
function percent(value: number | null | undefined) { return value == null ? "--" : `${(value * 100).toFixed(1)}%`; }
function bytes(value: number) { if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`; if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB`; if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`; return `${value.toFixed(0)} B`; }
function duration(milliseconds: number) { const totalMinutes = Math.floor(milliseconds / 60_000); const days = Math.floor(totalMinutes / 1440), hours = Math.floor(totalMinutes % 1440 / 60), minutes = totalMinutes % 60; return days ? `${days} 天 ${hours} 小时` : hours ? `${hours} 小时 ${minutes} 分钟` : `${minutes} 分钟`; }
onMounted(async () => { await Promise.all([loadLayout(), refreshData()]); timer = setInterval(refreshData, 5_000); });
onBeforeUnmount(() => { if (timer) clearInterval(timer); grid?.destroy(); });
</script>

<style scoped lang="scss">
.dashboard-shell{min-height:100%;padding:18px;color:var(--uvp-text-primary)}.dashboard-header{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}.dashboard-title{display:flex;gap:12px;align-items:center}.dashboard-title h1{margin:1px 0 0;font-size:24px}.dashboard-title small,.card p{color:var(--uvp-text-tertiary)}.title-icon{display:grid;width:42px;height:42px;color:var(--uvp-brand);background:var(--uvp-brand-soft);border-radius:9px;place-items:center}.actions{display:flex;gap:8px;align-items:center}.actions button{display:inline-flex;gap:6px;align-items:center;height:36px;padding:0 13px;font:inherit;cursor:pointer;border-radius:8px}.secondary{color:var(--uvp-text-primary);background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border)}.primary{color:#fff;background:var(--uvp-brand);border:1px solid var(--uvp-brand)}.live{display:flex;gap:7px;align-items:center;margin-right:4px;font-size:12px;color:var(--uvp-text-secondary)}.live i{width:7px;height:7px;background:var(--uvp-brand-cyan);border-radius:50%;box-shadow:0 0 0 4px color-mix(in srgb,var(--uvp-brand-cyan) 12%,transparent)}
.widget-picker{display:flex;gap:14px;align-items:center;padding:10px 14px;margin-bottom:12px;overflow-x:auto;font-size:12px;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:9px}.widget-picker label{display:inline-flex;gap:5px;align-items:center;white-space:nowrap}.widget-picker>span{margin-left:auto;color:var(--uvp-text-tertiary);white-space:nowrap}.loading{display:grid;min-height:420px;color:var(--uvp-text-secondary);place-content:center;justify-items:center;gap:10px}.dashboard-grid{margin:-6px}.card{position:relative;box-sizing:border-box;padding:15px;overflow:hidden;background:var(--uvp-panel-bg);border:1px solid var(--uvp-panel-border);border-radius:var(--uvp-panel-radius);box-shadow:var(--uvp-panel-shadow)}.editing .card{border-color:color-mix(in srgb,var(--uvp-brand) 45%,var(--uvp-panel-border))}.drag{position:absolute;top:4px;right:7px;z-index:3;color:var(--uvp-text-tertiary);cursor:move}.kpi{margin-top:25px;font-size:29px;font-weight:700;line-height:1}.kpi small{margin-left:3px;font-size:12px;font-weight:500;color:var(--uvp-text-tertiary)}.card p{margin-top:9px;font-size:11px}
.traffic-legend{position:absolute;bottom:13px;left:15px;display:flex;gap:10px}.traffic-legend button{display:inline-flex;gap:4px;align-items:center;padding:0;font:inherit;font-size:10px;color:var(--uvp-text-tertiary);cursor:pointer;background:none;border:0}.traffic-legend button.active{color:var(--uvp-text-primary)}.traffic-legend i{width:7px;height:7px;border-radius:50%}.traffic-legend .up{background:var(--uvp-brand-cyan)}.traffic-legend .down{background:var(--uvp-danger)}
.runtime{display:grid;grid-template-columns:repeat(2,1fr);gap:6px 12px;margin-top:10px}.runtime span{display:flex;flex-direction:column;gap:2px}.runtime small{font-size:10px;line-height:1;color:var(--uvp-text-tertiary)}.runtime strong{font-size:20px;line-height:1.05}.platform-info{display:grid;grid-template-columns:repeat(2,1fr);gap:20px;margin-top:18px}.platform-info span{display:flex;flex-direction:column;gap:5px}.platform-info small{font-size:11px;color:var(--uvp-text-tertiary)}.platform-info strong{font-size:20px}.embedded{height:100%;gap:10px;padding:0;border:0;box-shadow:none}.embedded :deep(.pulse){flex:none}.embedded :deep(.pulse__chart){flex:none;height:90px;min-height:90px}.rate-head{display:flex;align-items:baseline;justify-content:space-between;margin:18px 0 10px}.rate-head strong{font-size:22px}.rate-head span{font-size:11px;color:var(--uvp-text-tertiary)}
.health-total{display:flex;align-items:baseline;justify-content:center;gap:7px;margin:28px 0 20px}.health-total strong{font-size:32px}.health-total span{font-size:11px;color:var(--uvp-text-tertiary)}.health-legend{display:flex;justify-content:space-around;font-size:11px;color:var(--uvp-text-secondary)}.health-legend i{display:inline-block;width:7px;height:7px;margin-right:4px;border-radius:50%}.health-legend .ok{background:var(--uvp-brand-cyan)}.health-legend .warn{background:var(--uvp-warning)}.health-legend .bad{background:var(--uvp-danger)}.ranking{margin-top:14px}.ranking>div{display:grid;grid-template-columns:26px 1fr 90px 110px;gap:10px;align-items:center;min-height:38px;font-size:11px;border-bottom:1px solid var(--uvp-panel-border)}.rank{color:var(--uvp-text-tertiary);text-align:center}.stream{display:flex;flex-direction:column;min-width:0}.stream strong,.stream small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.stream small{color:var(--uvp-text-tertiary)}.viewer{color:var(--uvp-brand-cyan)}.ranking>div>strong{text-align:right}.empty{display:grid;min-height:90px;color:var(--uvp-text-tertiary);place-items:center}.spin{animation:spin 1s linear infinite}@keyframes spin{to{transform:rotate(360deg)}}
@media(max-width:900px){.dashboard-header{align-items:flex-start}.actions{flex-wrap:wrap;justify-content:flex-end}.live{width:100%;justify-content:flex-end}}
</style>
