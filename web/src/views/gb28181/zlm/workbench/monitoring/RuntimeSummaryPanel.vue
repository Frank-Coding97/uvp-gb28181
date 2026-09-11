<script setup lang="ts">
import { computed, ref, toRef, watch } from "vue";
import { Server } from "lucide-vue-next";

import { getZLMNodeRuntime, type ZLMNodeRuntime, type ZLMObjectStatistics } from "@/api/gb28181-zlm-runtime";

import StatCard from "../../components/StatCard.vue";
import Sparkline from "../../components/Sparkline.vue";
import MediaVChart from "../components/MediaVChart.vue";
import { formatZLMByteRate, zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import {
  appendRuntimeSnapshot,
  buildRuntimeTrendChartState,
  createRuntimeTrendHistory,
  runtimeSnapshot,
  type RuntimeTrendHistory
} from "../chart/runtimeChart";
import {
  appendObjectStatisticSample,
  objectStatisticTrend,
  type ObjectStatisticSample
} from "./objectStatisticState";
import {
  busiestEventThreads,
  eventThreadHeatmapColumns,
  eventThreadIndex,
  eventThreadLoadCSSPercent,
  eventThreadLoadCSSValue,
  eventThreadLoadDistribution,
  eventThreadTone,
  summarizeEventThreadLoads
} from "./threadLoadState";

const props = withDefaults(defineProps<{
  active: boolean;
  nodeId: number | null;
}>(), {
  active: true
});

const emit = defineEmits<{
  drilldown: [view: "streams" | "sessions"];
}>();

const runtime = ref<ZLMNodeRuntime | null>(null);
const history = ref<RuntimeTrendHistory>(createRuntimeTrendHistory({ nodeId: props.nodeId, range: `node:${props.nodeId ?? "none"}` }));
const objectStatisticHistory = ref<ObjectStatisticSample[]>([]);
const loading = ref(false);
const loadError = ref<unknown>(null);
const objectDetailsVisible = ref(false);

const requestNodeId = computed(() => props.nodeId);
const summary = computed(() => runtime.value ? runtimeSnapshot(runtime.value) : null);
const chart = computed(() => buildRuntimeTrendChartState(history.value));
const metricsAvailable = computed(() => runtime.value?.metricsComplete === true);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const streamTrend = computed(() => history.value.samples.map(point => point.streamCount ?? undefined).filter((point): point is number => point !== undefined));
const viewerTrend = computed(() => history.value.samples.map(point => point.viewerCount ?? undefined).filter((point): point is number => point !== undefined));
const throughputTrend = computed(() => history.value.samples.map(point => point.throughput ?? undefined).filter((point): point is number => point !== undefined));
const sessionTrend = computed(() => history.value.samples.map(point => point.sessionCount ?? undefined).filter((point): point is number => point !== undefined));
const eventThreadLoads = computed(() => runtime.value?.metrics.eventThreadLoads ?? []);
const eventThreadSummary = computed(() => summarizeEventThreadLoads(eventThreadLoads.value));
const eventThreadDistribution = computed(() => eventThreadLoadDistribution(eventThreadLoads.value));
const busiestThreads = computed(() => busiestEventThreads(eventThreadLoads.value));
const eventThreadColumns = computed(() => eventThreadHeatmapColumns(eventThreadLoads.value.length));

const objectStatisticMeta: Array<{
  key: keyof ZLMObjectStatistics;
  label: string;
  technicalLabel: string;
  primary?: boolean;
}> = [
  { key: "mediaSource", label: "媒体源", technicalLabel: "MediaSource", primary: true },
  { key: "multiMediaSourceMuxer", label: "多协议媒体复用器", technicalLabel: "MultiMediaSourceMuxer" },
  { key: "tcpServer", label: "TCP 服务监听", technicalLabel: "TcpServer" },
  { key: "tcpSession", label: "TCP 会话", technicalLabel: "TcpSession", primary: true },
  { key: "udpServer", label: "UDP 服务监听", technicalLabel: "UdpServer" },
  { key: "udpSession", label: "UDP 会话", technicalLabel: "UdpSession", primary: true },
  { key: "tcpClient", label: "TCP 客户端", technicalLabel: "TcpClient" },
  { key: "socket", label: "网络套接字", technicalLabel: "Socket", primary: true },
  { key: "frameImp", label: "视频帧实现", technicalLabel: "FrameImp" },
  { key: "frame", label: "视频帧", technicalLabel: "Frame" },
  { key: "buffer", label: "缓冲对象", technicalLabel: "Buffer", primary: true },
  { key: "bufferRaw", label: "原始缓冲", technicalLabel: "BufferRaw", primary: true },
  { key: "bufferLikeString", label: "字符串缓冲", technicalLabel: "BufferLikeString" },
  { key: "bufferList", label: "缓冲列表", technicalLabel: "BufferList" },
  { key: "rtpPacket", label: "RTP 数据包", technicalLabel: "RtpPacket" },
  { key: "rtmpPacket", label: "RTMP 数据包", technicalLabel: "RtmpPacket" }
];

const objectStatisticItems = computed(() => {
  const statistics = runtime.value?.metrics.objectStatistics;
  if (!statistics) return [];
  return objectStatisticMeta.map(item => ({
    ...item,
    value: statistics[item.key],
    trend: objectStatisticTrend(objectStatisticHistory.value, item.key)
  }));
});

const primaryObjectStatisticItems = computed(() => objectStatisticItems.value.filter(item => item.primary));

const { refresh } = useZLMRuntimePolling<ZLMNodeRuntime>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  intervalMs: 5_000,
  async load(nodeId, signal) {
    loading.value = true;
    if (props.nodeId === null || nodeId !== props.nodeId) throw new Error("尚未选择媒体节点");
    const response = await getZLMNodeRuntime(props.nodeId, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "节点运行态加载失败");
    if (response.data.nodeId !== props.nodeId) throw new Error("后端返回了错误的节点运行态");
    return response.data;
  },
  publish(value) {
    runtime.value = value;
    objectStatisticHistory.value = appendObjectStatisticSample(objectStatisticHistory.value, value);
    history.value = appendRuntimeSnapshot(history.value, value, {
      nodeId: props.nodeId,
      range: `node:${props.nodeId ?? "none"}`
    });
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
  }
});

watch(() => props.nodeId, () => {
  runtime.value = null;
  loadError.value = null;
  loading.value = props.nodeId !== null;
  objectDetailsVisible.value = false;
  objectStatisticHistory.value = [];
  history.value = createRuntimeTrendHistory({
    nodeId: props.nodeId,
    range: `node:${props.nodeId ?? "none"}`
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
      当前节点仅返回部分运行指标，缺失值以破折号表示。
    </div>
    <div v-else-if="runtime?.status === 'unavailable'" class="monitoring-banner monitoring-banner--danger" role="alert">
      当前节点运行态不可用，请检查节点连接与生命周期。
    </div>

    <div v-if="loading && !runtime" class="monitoring-state" role="status" aria-label="正在加载运行摘要"><a-spin />正在采集运行态…</div>
    <div v-else-if="loadError && !runtime" class="monitoring-state monitoring-state--error" role="alert">
      <Server :size="34" /><strong>{{ errorPresentation.label }}</strong><a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button>
    </div>
    <div v-else-if="!runtime" class="monitoring-state" role="status"><Server :size="34" /><strong>等待运行态采样</strong><span>进入页面后才开始记录趋势。</span></div>

    <template v-if="runtime && summary">
      <section class="runtime-summary-kpis" aria-label="运行关键指标">
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
      </section>

      <section class="runtime-summary-grid">
        <MediaVChart
          aria-label="实时媒体速率"
          :title="chart.title"
          :spec="chart.spec"
          :status="chart.status"
          :summary="chart.summary"
          :warning="chart.warning"
          :sampled-label="chart.sampledLabel"
          legend-label="媒体速率（KB/s）"
          :show-summary="false"
          :active="active"
          status-text="进入页面后等待运行态采样"
        />
        <section class="runtime-object-panel runtime-object-panel--summary" aria-labelledby="runtime-object-title">
          <header class="runtime-object-panel__header">
            <div><h3 id="runtime-object-title">对象实例</h3><p>关注媒体、会话和缓冲对象的数量变化</p></div>
            <button
              v-if="objectStatisticItems.length"
              type="button"
              class="object-stat-toggle"
              @click="objectDetailsVisible = true"
            >查看全部对象</button>
          </header>
          <div v-if="metricsAvailable && primaryObjectStatisticItems.length" class="object-stat-grid">
            <div v-for="item in primaryObjectStatisticItems" :key="item.key" class="object-stat-chip">
              <div class="object-stat-chip__value">
                <span :title="item.technicalLabel">{{ item.label }}</span>
                <small>{{ item.technicalLabel }}</small>
                <strong>{{ item.value.toLocaleString() }}</strong>
              </div>
              <Sparkline
                class="object-stat-sparkline"
                :data="item.trend"
                :width="64"
                :height="24"
                color="brand"
                aria-hidden="true"
              />
            </div>
          </div>
          <div v-else class="runtime-object-empty" role="status">对象实例暂不可用。</div>
        </section>
      </section>

      <section class="runtime-thread-panel runtime-thread-panel--full" aria-labelledby="runtime-thread-title">
        <header class="runtime-thread-panel__header">
          <div><h3 id="runtime-thread-title">事件线程负载</h3><p>查看全部事件线程的负载分布和热点，悬停可查看线程与 FD</p></div>
        </header>
        <div v-if="!metricsAvailable" class="runtime-empty" role="status">线程负载采样不可用。</div>
        <div v-else-if="eventThreadLoads.length" class="thread-load-content">
          <div class="thread-load-summary" aria-label="事件线程负载摘要">
            <div><span>线程总数</span><strong>{{ eventThreadSummary.count }}</strong></div>
            <div><span>平均负载</span><strong>{{ eventThreadSummary.average }}%</strong></div>
            <div><span>最高负载</span><strong>{{ eventThreadSummary.peak }}%</strong></div>
            <div :data-alert="eventThreadSummary.highCount > 0"><span>高负载线程</span><strong>{{ eventThreadSummary.highCount }}</strong></div>
          </div>
          <div class="thread-analysis-grid">
            <section class="thread-heatmap-section" aria-labelledby="thread-heatmap-title">
              <div class="thread-section-title"><strong id="thread-heatmap-title">线程负载矩阵</strong><span>编号对应 ZLM event poller</span></div>
              <div
                class="thread-heatmap"
                role="list"
                :style="{ '--thread-columns': String(eventThreadColumns) }"
                :aria-label="`事件线程负载矩阵，共 ${eventThreadSummary.count} 条线程`"
              >
                <a-tooltip
                  v-for="(thread, index) in eventThreadLoads"
                  :key="`${thread.nodeId ?? runtime.nodeId}/${thread.name}/${index}`"
                  :content="`${thread.name} · 负载 ${thread.load}% · FD ${thread.fdCount}`"
                >
                  <span
                    class="thread-heatmap-cell"
                    role="listitem"
                    tabindex="0"
                    :data-tone="eventThreadTone(thread.load)"
                    :style="{ '--thread-load': eventThreadLoadCSSValue(thread.load) }"
                    :aria-label="`${thread.name}，负载 ${thread.load}%，FD ${thread.fdCount}`"
                  >{{ eventThreadIndex(thread, index) }}</span>
                </a-tooltip>
              </div>
            </section>
            <aside class="thread-insights" aria-label="线程负载分析">
              <section>
                <div class="thread-section-title"><strong>负载分布</strong><span>按风险区间统计</span></div>
                <div class="thread-load-distribution" aria-label="事件线程负载区间分布">
                  <span
                    v-for="bucket in eventThreadDistribution"
                    :key="bucket.key"
                    :data-tone="bucket.key"
                    :style="{ width: `${bucket.ratio}%` }"
                  />
                </div>
                <div class="thread-distribution-legend">
                  <div v-for="bucket in eventThreadDistribution" :key="bucket.key">
                    <i :data-tone="bucket.key" /><span>{{ bucket.label }} {{ bucket.count }}</span><small>{{ bucket.range }}</small>
                  </div>
                </div>
              </section>
              <section>
                <div class="thread-section-title"><strong>热点线程排行</strong><span>负载最高前 5 条</span></div>
                <ol class="thread-ranking">
                  <li v-for="(thread, index) in busiestThreads" :key="`${thread.name}/${index}`">
                    <div><span>{{ eventThreadIndex(thread, eventThreadLoads.indexOf(thread)) }} 号线程</span><strong>{{ thread.load }}%</strong></div>
                    <span class="thread-ranking-bar" :data-tone="eventThreadTone(thread.load)">
                      <i :style="{ width: eventThreadLoadCSSPercent(thread.load) }" />
                    </span>
                  </li>
                </ol>
              </section>
            </aside>
          </div>
        </div>
        <div v-else class="runtime-empty" role="status">ZLM 当前没有返回事件线程明细。</div>
      </section>

      <a-drawer v-model:visible="objectDetailsVisible" :width="720" :footer="false" unmount-on-close title="全部对象实例">
        <p class="object-detail-intro">中文名称用于日常判断，英文名称对应 ZLM getStatistic 返回的技术对象。</p>
        <div class="object-detail-grid">
          <div v-for="item in objectStatisticItems" :key="item.key" class="object-detail-card">
            <div><strong>{{ item.label }}</strong><small>{{ item.technicalLabel }}</small></div>
            <span>{{ item.value.toLocaleString() }}</span>
          </div>
        </div>
      </a-drawer>
    </template>
  </div>
</template>

<style scoped>
.monitoring-panel { box-sizing: border-box; min-width: 0; color: var(--zlm-text-2); }
.monitoring-banner { margin: 10px 0; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.monitoring-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.monitoring-banner--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.monitoring-state { display: flex; min-height: 270px; flex-direction: column; align-items: center; justify-content: center; gap: 9px; color: var(--zlm-text-3); text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }.monitoring-state strong { color: var(--zlm-text-1); }.monitoring-state--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.runtime-summary-kpis { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 10px; margin-top: 0; }.runtime-kpi-button { min-width: 0; padding: 0; text-align: left; background: transparent; border: 0; border-radius: var(--zlm-radius-lg); cursor: pointer; }.runtime-kpi-button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }.runtime-kpi-button:hover :deep(.stat-card) { border-color: var(--zlm-brand-500); }
.runtime-summary-grid { display: grid; grid-template-columns: minmax(0, 1.08fr) minmax(0, .92fr); align-items: stretch; gap: 12px; margin-top: 8px; }
.runtime-thread-panel, .runtime-object-panel { min-width: 0; padding: 13px 15px 12px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.runtime-thread-panel--full { margin-top: 8px; padding-bottom: 10px; }
.runtime-thread-panel__header, .runtime-object-panel__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.runtime-thread-panel h3, .runtime-object-panel h3 { margin: 0; color: var(--zlm-text-1); font-size: 14px; }
.runtime-thread-panel p, .runtime-object-panel p { margin: 2px 0 0; color: var(--zlm-text-3); font-size: 12px; line-height: 1.35; }
.thread-load-content { display: grid; gap: 10px; }
.thread-load-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); padding-bottom: 8px; border-bottom: 1px solid var(--zlm-border); }
.thread-load-summary > div { min-width: 0; padding: 0 14px; border-left: 1px solid var(--zlm-border); }
.thread-load-summary > div:first-child { padding-left: 0; border-left: 0; }
.thread-load-summary span, .thread-load-summary strong { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.thread-load-summary span { color: var(--zlm-text-3); font-size: 12px; line-height: 1.35; }
.thread-load-summary strong { margin-top: 2px; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-size: 14px; }
.thread-load-summary [data-alert="true"] strong { color: var(--zlm-danger-600); }
.thread-analysis-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(300px, .42fr); gap: 18px; align-items: start; }
.thread-heatmap-section, .thread-insights, .thread-insights > section { min-width: 0; }
.thread-section-title { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 6px; }
.thread-section-title strong { color: var(--zlm-text-2); font-size: 12px; line-height: 1.35; }
.thread-section-title span { overflow: hidden; color: var(--zlm-text-3); font-size: 12px; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.thread-heatmap { display: grid; grid-template-columns: repeat(var(--thread-columns), minmax(22px, 1fr)); gap: 6px; align-content: start; }
.thread-heatmap-cell { --thread-load: 0; position: relative; z-index: 0; display: grid; height: 26px; overflow: hidden; place-items: center; color: var(--zlm-text-2); background: var(--zlm-fill-2); border: 1px solid var(--zlm-border); border-radius: 5px; font-family: var(--zlm-font-mono); font-size: 12px; cursor: help; }
.thread-heatmap-cell::before { position: absolute; z-index: -1; inset: 0; background: var(--zlm-brand-500); content: ""; opacity: calc(.08 + var(--thread-load) * .82); transition: opacity .25s ease; }
.thread-heatmap-cell[data-tone="warning"]::before { background: var(--zlm-warn-500); }
.thread-heatmap-cell[data-tone="danger"]::before { background: var(--zlm-danger-500); }
.thread-heatmap-cell:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 1px; }
.thread-insights { display: grid; grid-template-columns: minmax(0, .9fr) minmax(0, 1.1fr); gap: 14px; padding-left: 14px; border-left: 1px solid var(--zlm-border); }
.thread-load-distribution { display: flex; height: 10px; overflow: hidden; background: var(--zlm-fill-2); border-radius: 999px; }
.thread-load-distribution span { min-width: 0; transition: width .25s ease; }
.thread-load-distribution [data-tone="normal"], .thread-distribution-legend i[data-tone="normal"] { background: var(--zlm-brand-500); }
.thread-load-distribution [data-tone="warning"], .thread-distribution-legend i[data-tone="warning"] { background: var(--zlm-warn-500); }
.thread-load-distribution [data-tone="danger"], .thread-distribution-legend i[data-tone="danger"] { background: var(--zlm-danger-500); }
.thread-distribution-legend { display: grid; gap: 3px; margin-top: 7px; }
.thread-distribution-legend > div { display: grid; grid-template-columns: 7px 1fr auto; align-items: center; gap: 5px; min-width: 0; font-size: 12px; line-height: 1.35; }
.thread-distribution-legend i { width: 7px; height: 7px; border-radius: 50%; }
.thread-distribution-legend span { overflow: hidden; color: var(--zlm-text-2); text-overflow: ellipsis; white-space: nowrap; }
.thread-distribution-legend small { color: var(--zlm-text-3); font-size: 12px; }
.thread-ranking { display: grid; gap: 4px; margin: 0; padding: 0; list-style: none; }
.thread-ranking li > div { display: flex; justify-content: space-between; gap: 8px; color: var(--zlm-text-3); font-size: 12px; line-height: 1.35; }
.thread-ranking li strong { color: var(--zlm-text-2); font-family: var(--zlm-font-mono); font-size: 12px; }
.thread-ranking-bar { position: relative; display: block; height: 3px; margin-top: 2px; overflow: hidden; background: var(--zlm-fill-2); border-radius: 999px; }
.thread-ranking-bar i { display: block; height: 100%; background: var(--zlm-brand-500); border-radius: inherit; transition: width .25s ease; }
.thread-ranking-bar[data-tone="warning"] i { background: var(--zlm-warn-500); }
.thread-ranking-bar[data-tone="danger"] i { background: var(--zlm-danger-500); }
.runtime-empty { display: grid; min-height: 160px; place-items: center; color: var(--zlm-text-3); text-align: center; font-size: var(--zlm-fs-caption); }
.runtime-object-panel--summary { padding-block: 12px; }
.object-stat-toggle { flex: none; padding: 2px 0; color: var(--zlm-brand-600); background: transparent; border: 0; font-size: 14px; line-height: 1.35; cursor: pointer; }
.object-stat-toggle:hover { color: var(--zlm-brand-500); }
.object-stat-toggle:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.object-stat-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; }
.object-stat-chip { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) 64px; align-items: center; gap: 7px; min-height: 56px; padding: 6px 9px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }
.object-stat-chip__value { display: grid; min-width: 0; grid-template-columns: 1fr auto; align-items: baseline; column-gap: 6px; }
.object-stat-chip span, .object-stat-chip small, .object-stat-chip strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.object-stat-chip span { color: var(--zlm-text-2); font-size: 12px; line-height: 1.35; }
.object-stat-chip small { grid-column: 1; color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: 12px; line-height: 1.35; }
.object-stat-chip strong { grid-row: 1 / span 2; grid-column: 2; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-size: 15px; }
.object-stat-sparkline { width: 64px; max-width: 100%; }
.runtime-object-empty { padding: 18px; color: var(--zlm-text-3); text-align: center; font-size: var(--zlm-fs-caption); }
.object-detail-intro { margin: 0 0 14px; color: var(--zlm-text-3); font-size: 12px; }
.object-detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.object-detail-card { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: 12px; padding: 12px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }
.object-detail-card div { min-width: 0; }
.object-detail-card strong, .object-detail-card small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.object-detail-card strong { color: var(--zlm-text-1); font-size: 12px; }
.object-detail-card small { margin-top: 2px; color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: 12px; line-height: 1.35; }
.object-detail-card > span { flex: none; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); font-size: 18px; }
@media (max-width: 1400px) { .runtime-summary-kpis { grid-template-columns: repeat(3, minmax(0, 1fr)); }.thread-analysis-grid { grid-template-columns: 1fr; }.thread-insights { grid-template-columns: repeat(2, minmax(0, 1fr)); padding: 12px 0 0; border-top: 1px solid var(--zlm-border); border-left: 0; } }
@media (max-width: 1100px) { .thread-analysis-grid { grid-template-columns: 1fr; }.thread-insights { padding: 12px 0 0; border-top: 1px solid var(--zlm-border); border-left: 0; } }
@media (max-width: 820px) { .runtime-thread-panel__header { flex-direction: column; }.runtime-summary-grid { grid-template-columns: 1fr; }.runtime-summary-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }.thread-heatmap { grid-template-columns: repeat(8, minmax(0, 1fr)); }.thread-insights, .object-detail-grid { grid-template-columns: 1fr; } }.runtime-summary-kpis :deep(.stat-card) { min-height: 88px; }
@media (max-width: 560px) { .runtime-summary-kpis { grid-template-columns: 1fr; } }
</style>
