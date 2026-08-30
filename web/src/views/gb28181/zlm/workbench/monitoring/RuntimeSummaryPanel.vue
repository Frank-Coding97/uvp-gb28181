<script setup lang="ts">
import { computed, ref, toRef, watch } from "vue";
import { Server } from "lucide-vue-next";

import {
  getZLMNodeRuntime,
  getZLMOverview,
  type ZLMNodeRuntime,
  type ZLMObjectStatistics,
  type ZLMOverview
} from "@/api/gb28181-zlm-runtime";
import type { MediaScope } from "@/store/modules/media-workbench";

import StatCard from "../../components/StatCard.vue";
import Sparkline from "../../components/Sparkline.vue";
import MediaVChart from "../components/MediaVChart.vue";
import { formatZLMByteRate, zlmErrorPresentation, zlmFreshnessPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import {
  appendRuntimeSnapshot,
  buildRuntimeTrendChartState,
  createRuntimeTrendHistory,
  runtimeSnapshot,
  type RuntimeTrendHistory
} from "../chart/runtimeChart";
import { orderClusterNodesByRisk, overviewAsRuntime, scopeRange } from "./monitoringState";

const props = withDefaults(defineProps<{
  active: boolean;
  scope: MediaScope;
  nodeId: number | null;
}>(), {
  active: true
});

const emit = defineEmits<{
  drilldown: [view: "streams" | "sessions"];
  selectNode: [nodeId: number];
}>();

const runtime = ref<ZLMNodeRuntime | null>(null);
const clusterOverview = ref<ZLMOverview | null>(null);
const history = ref<RuntimeTrendHistory>(createRuntimeTrendHistory({ nodeId: props.nodeId, range: scopeRange(props.scope, props.nodeId) }));
const loading = ref(false);
const loadError = ref<unknown>(null);

const requestNodeId = computed(() => props.scope === "all" ? 1 : props.nodeId);
const selectedNodeLabel = computed(() => props.scope === "all" ? "全部节点" : `节点 #${props.nodeId ?? "—"}`);
const summary = computed(() => runtime.value ? runtimeSnapshot(runtime.value) : null);
const chart = computed(() => buildRuntimeTrendChartState(history.value));
const metricsAvailable = computed(() => runtime.value?.metricsComplete === true);
const freshness = computed(() => zlmFreshnessPresentation(runtime.value?.asOf));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const streamTrend = computed(() => history.value.samples.map(point => point.streamCount ?? undefined).filter((point): point is number => point !== undefined));
const viewerTrend = computed(() => history.value.samples.map(point => point.viewerCount ?? undefined).filter((point): point is number => point !== undefined));
const throughputTrend = computed(() => history.value.samples.map(point => point.throughput ?? undefined).filter((point): point is number => point !== undefined));
const sessionTrend = computed(() => history.value.samples.map(point => point.sessionCount ?? undefined).filter((point): point is number => point !== undefined));
const eventThreadLoads = computed(() => runtime.value?.metrics.eventThreadLoads ?? []);
const clusterNodes = computed(() => orderClusterNodesByRisk(clusterOverview.value?.nodes ?? []));
const activeNodeCount = computed(() => clusterOverview.value?.nodes.filter(node => node.state === "active" && node.status !== "unavailable").length ?? 0);
const abnormalNodeCount = computed(() => clusterOverview.value?.nodes.filter(node => node.state !== "active" || node.status !== "fresh").length ?? 0);

interface RuntimePayload {
  runtime: ZLMNodeRuntime;
  overview: ZLMOverview | null;
}

const objectStatisticMeta: Array<{ key: keyof ZLMObjectStatistics; label: string }> = [
  { key: "mediaSource", label: "MediaSource" },
  { key: "multiMediaSourceMuxer", label: "MultiMediaSourceMuxer" },
  { key: "tcpServer", label: "TcpServer" },
  { key: "tcpSession", label: "TcpSession" },
  { key: "udpServer", label: "UdpServer" },
  { key: "udpSession", label: "UdpSession" },
  { key: "tcpClient", label: "TcpClient" },
  { key: "socket", label: "Socket" },
  { key: "frameImp", label: "FrameImp" },
  { key: "frame", label: "Frame" },
  { key: "buffer", label: "Buffer" },
  { key: "bufferRaw", label: "BufferRaw" },
  { key: "bufferLikeString", label: "BufferLikeString" },
  { key: "bufferList", label: "BufferList" },
  { key: "rtpPacket", label: "RtpPacket" },
  { key: "rtmpPacket", label: "RtmpPacket" }
];

const objectStatisticItems = computed(() => {
  const statistics = runtime.value?.metrics.objectStatistics;
  if (!statistics) return [];
  return objectStatisticMeta.map(item => ({ ...item, value: statistics[item.key] }));
});

function threadLoadWidth(load: number) {
  return `${Math.min(100, Math.max(0, load))}%`;
}

function threadLoadTone(load: number) {
  if (load > 80) return "danger";
  if (load > 50) return "warning";
  return "normal";
}

function nodeStateText(node: ZLMNodeRuntime) {
  if (node.state === "maintenance") return "维护";
  if (node.state === "offline") return "离线";
  if (node.status === "unavailable") return "采集失败";
  if (node.status === "partial") return "部分数据";
  return "在线";
}

function nodeStateTone(node: ZLMNodeRuntime) {
  if (node.state === "offline" || node.status === "unavailable") return "danger";
  if (node.state === "maintenance" || node.status === "partial") return "warning";
  return "success";
}

const { refresh } = useZLMRuntimePolling<RuntimePayload>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  intervalMs: 5_000,
  async load(nodeId, signal) {
    loading.value = true;
    if (props.scope === "all") {
      const response = await getZLMOverview(signal);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "媒体总览加载失败");
      return { runtime: overviewAsRuntime(response.data), overview: response.data };
    }
    if (props.nodeId === null || nodeId !== props.nodeId) throw new Error("尚未选择媒体节点");
    const response = await getZLMNodeRuntime(props.nodeId, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "节点运行态加载失败");
    if (response.data.nodeId !== props.nodeId) throw new Error("后端返回了错误的节点运行态");
    return { runtime: response.data, overview: null };
  },
  publish(value) {
    runtime.value = value.runtime;
    clusterOverview.value = value.overview;
    history.value = appendRuntimeSnapshot(history.value, value.runtime, {
      nodeId: props.scope === "all" ? null : props.nodeId,
      range: scopeRange(props.scope, props.nodeId)
    });
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
  }
});

watch([() => props.scope, () => props.nodeId], () => {
  runtime.value = null;
  clusterOverview.value = null;
  loadError.value = null;
  loading.value = props.scope === "all" || props.nodeId !== null;
  history.value = createRuntimeTrendHistory({
    nodeId: props.scope === "all" ? null : props.nodeId,
    range: scopeRange(props.scope, props.nodeId)
  });
  if (props.active) refresh();
});

watch(() => props.active, active => {
  if (active) refresh();
});

defineExpose({ refresh });
</script>

<template>
  <div class="monitoring-panel runtime-summary-panel">
    <div v-if="loadError && runtime" class="monitoring-banner monitoring-banner--warning" role="status">
      本次采样失败：{{ errorPresentation.label }}；页面保留 {{ runtime.asOf }} 的上一次成功结果。
    </div>
    <div v-else-if="runtime?.status === 'partial'" class="monitoring-banner monitoring-banner--warning" role="status">
      当前为部分采样，缺失的节点指标以破折号表示；趋势样本口径为 {{ selectedNodeLabel }}。
    </div>
    <div v-else-if="runtime?.status === 'unavailable'" class="monitoring-banner monitoring-banner--danger" role="alert">
      当前范围运行态不可用，请检查节点连接与生命周期。
    </div>

    <div v-if="loading && !runtime" class="monitoring-state" role="status" aria-label="正在加载运行摘要"><a-spin />正在采集运行态…</div>
    <div v-else-if="loadError && !runtime" class="monitoring-state monitoring-state--error" role="alert">
      <Server :size="34" /><strong>{{ errorPresentation.label }}</strong><a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button>
    </div>
    <div v-else-if="!runtime" class="monitoring-state" role="status"><Server :size="34" /><strong>等待运行态采样</strong><span>进入页面后才开始记录趋势。</span></div>

    <template v-if="runtime && summary">
      <section class="runtime-summary-kpis" aria-label="运行关键指标">
        <template v-if="scope === 'all'">
          <StatCard title="在线节点" :value="activeNodeCount" unit="个" accent="brand" :trend="`${clusterOverview?.nodes.length ?? 0} 个可见节点`" />
          <StatCard title="异常节点" :value="abnormalNodeCount" unit="个" :accent="abnormalNodeCount ? 'danger' : 'default'" trend="离线、维护或采样异常" />
          <button type="button" class="runtime-kpi-button" @click="emit('drilldown', 'streams')">
            <StatCard title="在线媒体流" :value="summary.mediaKnown ? summary.streamCount ?? undefined : undefined" unit="路" accent="accent" trend="查看流媒体">
              <template #spark><Sparkline :data="streamTrend" color="brand" fill /></template>
            </StatCard>
          </button>
          <StatCard title="媒体观看者" :value="summary.mediaKnown ? summary.viewerCount ?? undefined : undefined" unit="个" trend="在线 readerCount 合计">
            <template #spark><Sparkline :data="viewerTrend" color="accent" fill /></template>
          </StatCard>
          <StatCard title="媒体吞吐" :value-text="summary.throughput === null ? '—' : formatZLMByteRate(summary.throughput)" trend="在线流 bytesSpeed 合计">
            <template #spark><Sparkline :data="throughputTrend" color="info" fill /></template>
          </StatCard>
          <button type="button" class="runtime-kpi-button" @click="emit('drilldown', 'sessions')">
            <StatCard title="网络会话" :value="metricsAvailable ? summary.sessionCount ?? undefined : undefined" unit="个" trend="查看网络会话">
              <template #spark><Sparkline :data="sessionTrend" color="warning" fill /></template>
            </StatCard>
          </button>
        </template>
        <template v-else>
          <button type="button" class="runtime-kpi-button" @click="emit('drilldown', 'streams')">
            <StatCard title="在线媒体流" :value="summary.mediaKnown ? summary.streamCount ?? undefined : undefined" unit="路" accent="brand" trend="查看流媒体">
              <template #spark><Sparkline :data="streamTrend" color="brand" fill /></template>
            </StatCard>
          </button>
          <StatCard title="媒体观看者" :value="summary.mediaKnown ? summary.viewerCount ?? undefined : undefined" unit="个" accent="accent" trend="在线 readerCount 合计">
            <template #spark><Sparkline :data="viewerTrend" color="accent" fill /></template>
          </StatCard>
          <StatCard title="媒体吞吐" :value-text="summary.throughput === null ? '—' : formatZLMByteRate(summary.throughput)" trend="在线流 bytesSpeed 合计">
            <template #spark><Sparkline :data="throughputTrend" color="info" fill /></template>
          </StatCard>
          <button type="button" class="runtime-kpi-button" @click="emit('drilldown', 'sessions')">
            <StatCard title="网络会话" :value="metricsAvailable ? summary.sessionCount ?? undefined : undefined" unit="个" trend="查看网络会话">
              <template #spark><Sparkline :data="sessionTrend" color="warning" fill /></template>
            </StatCard>
          </button>
          <StatCard title="NetThread 负载" :value="metricsAvailable ? summary.netThreadLoad ?? undefined : undefined" is-percent unit="%" trend="网络线程平均负载" :accent="(summary.netThreadLoad ?? 0) >= .8 ? 'danger' : 'default'" />
          <StatCard title="正在录制" :value="summary.mediaKnown ? summary.recordingCount ?? undefined : undefined" unit="路" trend="MP4 或 HLS 录制标记" />
        </template>
      </section>

      <section class="runtime-summary-grid">
        <MediaVChart
          aria-label="实时媒体速率"
          :title="chart.title"
          :spec="chart.spec"
          :status="chart.status"
          :summary="chart.summary"
          :warning="chart.warning"
          :as-of="chart.asOf"
          :sampled-label="chart.sampledLabel"
          :active="active"
          status-text="进入页面后等待运行态采样"
        />
        <section v-if="scope === 'all'" class="runtime-thread-panel" aria-labelledby="cluster-health-title">
          <header class="runtime-thread-panel__header">
            <div><h3 id="cluster-health-title">节点健康</h3><p>异常优先 · 点击节点查看运行细节</p></div>
            <span>{{ clusterNodes.length }} 个节点</span>
          </header>
          <div v-if="clusterNodes.length" class="cluster-health-list">
            <button
              v-for="node in clusterNodes"
              :key="node.nodeId"
              type="button"
              class="cluster-health-row"
              :data-tone="nodeStateTone(node)"
              :aria-label="`查看节点 ${node.name}，状态 ${nodeStateText(node)}`"
              @click="emit('selectNode', node.nodeId)"
            >
              <span class="cluster-health-row__identity"><i /><strong :title="node.name">{{ node.name }}</strong><small>#{{ node.nodeId }}</small></span>
              <span class="cluster-health-row__metrics">
                <b>{{ nodeStateText(node) }}</b>
                <small>流 {{ node.mediaFreshness === 'unavailable' ? '—' : node.streams?.length ?? 0 }}</small>
                <small>Net {{ node.metricsComplete ? `${Math.round(node.metrics.netThreadLoad * 100)}%` : '—' }}</small>
              </span>
            </button>
          </div>
          <div v-else class="runtime-empty" role="status">当前没有可见媒体节点。</div>
        </section>
        <section v-else class="runtime-thread-panel" aria-labelledby="runtime-thread-title">
          <header class="runtime-thread-panel__header">
            <div><h3 id="runtime-thread-title">事件线程负载</h3><p>getThreadsLoad · 单线程负载与 FD 数量</p></div>
            <span>{{ eventThreadLoads.length }} 条线程</span>
          </header>
          <div v-if="!metricsAvailable" class="runtime-empty" role="status">线程负载采样不可用。</div>
          <div v-else-if="eventThreadLoads.length" class="thread-load-list">
            <div v-for="(thread, index) in eventThreadLoads" :key="`${thread.nodeId ?? runtime.nodeId}/${thread.name}/${index}`" class="thread-load-row" :data-tone="threadLoadTone(thread.load)">
              <div class="thread-load-row__head"><strong :title="thread.name">{{ thread.name }}</strong><span>{{ thread.load }}% · FD {{ thread.fdCount }}</span></div>
              <div class="thread-load-track"><i :style="{ width: threadLoadWidth(thread.load) }" /></div>
            </div>
          </div>
          <div v-else class="runtime-empty" role="status">ZLM 当前没有返回事件线程明细。</div>
        </section>
      </section>

      <section class="runtime-object-panel" aria-labelledby="runtime-object-title">
        <header class="runtime-object-panel__header">
          <div><h3 id="runtime-object-title">对象统计</h3><p>getStatistic · ZLM 内存对象实例计数</p></div>
          <span>当前快照 · {{ objectStatisticItems.length }} 项</span>
        </header>
        <div v-if="metricsAvailable && objectStatisticItems.length" class="object-stat-grid">
          <div v-for="item in objectStatisticItems" :key="item.key" class="object-stat-chip">
            <span :title="item.label">{{ item.label }}</span><strong>{{ item.value.toLocaleString() }}</strong>
          </div>
        </div>
        <div v-else class="runtime-object-empty" role="status">对象统计暂不可用。</div>
      </section>

      <footer class="runtime-summary-footnote" :data-tone="freshness.tone">
        {{ freshness.label }} · {{ selectedNodeLabel }} · 已记录 {{ history.samples.length }} / 60 个趋势样本
      </footer>
    </template>
  </div>
</template>

<style scoped>
.monitoring-panel { box-sizing: border-box; min-width: 0; color: var(--zlm-text-2); }
.monitoring-banner { margin: 10px 0; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.monitoring-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.monitoring-banner--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.monitoring-state { display: flex; min-height: 270px; flex-direction: column; align-items: center; justify-content: center; gap: 9px; color: var(--zlm-text-3); text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }.monitoring-state strong { color: var(--zlm-text-1); }.monitoring-state--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.runtime-summary-kpis { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-top: 12px; }.runtime-kpi-button { min-width: 0; padding: 0; text-align: left; background: transparent; border: 0; border-radius: var(--zlm-radius-lg); cursor: pointer; }.runtime-kpi-button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }.runtime-kpi-button:hover :deep(.stat-card) { border-color: var(--zlm-brand-500); }
.runtime-summary-grid { display: grid; grid-template-columns: minmax(0, 1.08fr) minmax(0, .92fr); gap: 12px; margin-top: 12px; }.runtime-thread-panel, .runtime-object-panel { min-width: 0; padding: 13px 15px 12px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.runtime-thread-panel__header, .runtime-object-panel__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 10px; }.runtime-thread-panel h3, .runtime-object-panel h3 { margin: 0; color: var(--zlm-text-1); font-size: 14px; }.runtime-thread-panel p, .runtime-object-panel p { margin: 3px 0 0; }.runtime-thread-panel__header > span, .runtime-object-panel__header > span { flex: none; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 10px; }
.thread-load-list { display: grid; max-height: 192px; gap: 8px; overflow: auto; }.thread-load-row { min-width: 0; }.thread-load-row__head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 4px; font-size: 11px; }.thread-load-row__head strong { overflow: hidden; color: var(--zlm-text-2); font-weight: var(--zlm-fw-medium); text-overflow: ellipsis; white-space: nowrap; }.thread-load-row__head span { flex: none; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); }.thread-load-track { height: 5px; overflow: hidden; background: var(--zlm-fill-2); border-radius: 999px; }.thread-load-track i { display: block; height: 100%; background: var(--zlm-brand-500); border-radius: inherit; transition: width .2s ease; }.thread-load-row[data-tone="warning"] .thread-load-track i { background: var(--zlm-warn-500); }.thread-load-row[data-tone="danger"] .thread-load-track i { background: var(--zlm-danger-500); }.runtime-empty { display: grid; min-height: 160px; place-items: center; color: var(--zlm-text-3); text-align: center; font-size: var(--zlm-fs-caption); }
.cluster-health-list { display: grid; max-height: 192px; gap: 6px; overflow: auto; }.cluster-health-row { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 9px; color: inherit; text-align: left; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; }.cluster-health-row:hover { border-color: var(--zlm-brand-500); }.cluster-health-row:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 1px; }.cluster-health-row__identity, .cluster-health-row__metrics { display: flex; min-width: 0; align-items: center; gap: 6px; }.cluster-health-row__identity { flex: 1; }.cluster-health-row__identity i { width: 7px; height: 7px; flex: none; background: var(--zlm-success-500); border-radius: 50%; }.cluster-health-row__identity strong { overflow: hidden; color: var(--zlm-text-1); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.cluster-health-row small { color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 9px; }.cluster-health-row__metrics { flex: none; }.cluster-health-row__metrics b { color: var(--zlm-success-600); font-size: 10px; font-weight: var(--zlm-fw-medium); }.cluster-health-row[data-tone="warning"] .cluster-health-row__identity i { background: var(--zlm-warn-500); }.cluster-health-row[data-tone="warning"] .cluster-health-row__metrics b { color: var(--zlm-warn-600); }.cluster-health-row[data-tone="danger"] .cluster-health-row__identity i { background: var(--zlm-danger-500); }.cluster-health-row[data-tone="danger"] .cluster-health-row__metrics b { color: var(--zlm-danger-600); }
.runtime-object-panel { margin-top: 12px; }.object-stat-grid { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); gap: 7px; }.object-stat-chip { min-width: 0; padding: 8px 9px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.object-stat-chip span, .object-stat-chip strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.object-stat-chip span { color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 9px; }.object-stat-chip strong { margin-top: 3px; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-size: 15px; }.runtime-object-empty { padding: 18px; color: var(--zlm-text-3); text-align: center; font-size: var(--zlm-fs-caption); }
.runtime-summary-footnote { margin-top: 8px; color: var(--zlm-text-3); font-size: 11px; text-align: right; }.runtime-summary-footnote[data-tone="warning"] { color: var(--zlm-warn-600); }.runtime-summary-footnote[data-tone="danger"] { color: var(--zlm-danger-600); }
@media (max-width: 1400px) { .runtime-summary-kpis { grid-template-columns: repeat(3, minmax(0, 1fr)); }.object-stat-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); } }.media-vchart { min-height: 100%; }
@media (max-width: 820px) { .runtime-thread-panel__header, .runtime-object-panel__header { flex-direction: column; }.runtime-summary-grid { grid-template-columns: 1fr; }.runtime-summary-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }.object-stat-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }.runtime-summary-kpis :deep(.stat-card) { min-height: 88px; }
@media (max-width: 560px) { .runtime-summary-kpis { grid-template-columns: 1fr; } }
</style>
