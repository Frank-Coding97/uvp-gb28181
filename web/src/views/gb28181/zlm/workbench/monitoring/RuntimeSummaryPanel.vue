<script setup lang="ts">
import { computed, ref, toRef, watch } from "vue";
import { Activity, Clock3, Radio, Server, Users } from "lucide-vue-next";

import { getZLMNodeRuntime, getZLMOverview, type ZLMNodeRuntime } from "@/api/gb28181-zlm-runtime";
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
import { overviewAsRuntime, scopeRange } from "./monitoringState";

const props = withDefaults(defineProps<{
  active: boolean;
  scope: MediaScope;
  nodeId: number | null;
}>(), {
  active: true
});

const emit = defineEmits<{
  drilldown: [view: "streams" | "sessions"];
}>();

const runtime = ref<ZLMNodeRuntime | null>(null);
const history = ref<RuntimeTrendHistory>(createRuntimeTrendHistory({ nodeId: props.nodeId, range: scopeRange(props.scope, props.nodeId) }));
const loading = ref(false);
const loadError = ref<unknown>(null);

const requestNodeId = computed(() => props.scope === "all" ? 1 : props.nodeId);
const selectedNodeLabel = computed(() => props.scope === "all" ? "全部节点" : `节点 #${props.nodeId ?? "—"}`);
const summary = computed(() => runtime.value ? runtimeSnapshot(runtime.value) : null);
const chart = computed(() => buildRuntimeTrendChartState(history.value));
const mediaAvailable = computed(() => runtime.value?.mediaFreshness !== "unavailable" && runtime.value?.streams !== undefined);
const metricsAvailable = computed(() => runtime.value?.metricsComplete === true);
const freshness = computed(() => zlmFreshnessPresentation(runtime.value?.asOf));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const streamTrend = computed(() => history.value.samples.map(point => point.streamCount ?? undefined).filter((point): point is number => point !== undefined));
const viewerTrend = computed(() => history.value.samples.map(point => point.viewerCount ?? undefined).filter((point): point is number => point !== undefined));
const throughputTrend = computed(() => history.value.samples.map(point => point.throughput ?? undefined).filter((point): point is number => point !== undefined));
const sessionTrend = computed(() => history.value.samples.map(point => point.sessionCount ?? undefined).filter((point): point is number => point !== undefined));

const { refresh } = useZLMRuntimePolling<ZLMNodeRuntime>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  intervalMs: 5_000,
  async load(nodeId, signal) {
    loading.value = true;
    if (props.scope === "all") {
      const response = await getZLMOverview(signal);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "媒体总览加载失败");
      return overviewAsRuntime(response.data);
    }
    if (props.nodeId === null || nodeId !== props.nodeId) throw new Error("尚未选择媒体节点");
    const response = await getZLMNodeRuntime(props.nodeId, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "节点运行态加载失败");
    if (response.data.nodeId !== props.nodeId) throw new Error("后端返回了错误的节点运行态");
    return response.data;
  },
  publish(value) {
    runtime.value = value;
    history.value = appendRuntimeSnapshot(history.value, value, {
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
    <header class="monitoring-panel__header">
      <div>
        <div class="monitoring-panel__eyebrow"><span class="status-dot" :data-active="runtime ? 'true' : 'false'" />运行态采样</div>
        <h2>运行摘要</h2>
        <p>聚合媒体流、观看者、网络会话与线程负载；未知指标保持未知，不将缺失误报为 0。</p>
      </div>
      <div class="monitoring-panel__meta">
        <span>{{ selectedNodeLabel }}</span>
        <span v-if="runtime"><Clock3 :size="13" />{{ runtime.asOf }}</span>
      </div>
    </header>

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
        <StatCard title="WorkThread 负载" :value="metricsAvailable ? summary.workThreadLoad ?? undefined : undefined" is-percent unit="%" trend="工作线程平均负载" :accent="(summary.workThreadLoad ?? 0) >= .8 ? 'danger' : 'default'" />
        <StatCard title="文件描述符 / Socket" :value="metricsAvailable ? summary.fdCount ?? undefined : undefined" unit="个" trend="socketCount 口径" />
        <StatCard title="正在录制" :value="summary.mediaKnown ? summary.recordingCount ?? undefined : undefined" unit="路" trend="MP4 或 HLS 录制标记" />
      </section>

      <section class="runtime-summary-grid">
        <MediaVChart
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
        <section class="runtime-stream-panel" aria-labelledby="runtime-sample-title">
          <header class="runtime-stream-panel__header">
            <div><h3 id="runtime-sample-title">当前媒体采样</h3><p>完整媒体身份和来源均由后端返回。</p></div>
            <a-button size="small" @click="emit('drilldown', 'streams')">查看全部流</a-button>
          </header>
          <div v-if="!mediaAvailable" class="runtime-empty" role="status">媒体采样不可用，不能判断在线流是否为 0。</div>
          <div v-else-if="runtime.streams?.length" class="runtime-streams">
            <div v-for="stream in runtime.streams.slice(0, 6)" :key="`${stream.nodeId}/${stream.media.schema}/${stream.media.vhost}/${stream.media.app}/${stream.media.stream}`" class="runtime-stream">
              <Radio :size="14" />
              <span><strong>{{ stream.media.app }}/{{ stream.media.stream }}</strong><small>节点 #{{ stream.nodeId }} · {{ stream.media.schema }} · {{ stream.media.vhost }}</small></span>
              <span class="runtime-stream__right"><Users :size="12" />{{ stream.readerCount }}<Activity :size="12" />{{ formatZLMByteRate(stream.bytesSpeed) }}</span>
            </div>
          </div>
          <div v-else class="runtime-empty" role="status">后端确认当前范围没有在线媒体流。</div>
        </section>
      </section>

      <footer class="runtime-summary-footnote" :data-tone="freshness.tone">
        {{ freshness.label }} · {{ selectedNodeLabel }} · 已记录 {{ history.samples.length }} / 60 个趋势样本
      </footer>
    </template>
  </div>
</template>

<style scoped>
.monitoring-panel { box-sizing: border-box; min-width: 0; color: var(--zlm-text-2); }
.monitoring-panel__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.monitoring-panel__eyebrow { display: inline-flex; align-items: center; gap: 7px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.status-dot { width: 7px; height: 7px; background: var(--zlm-text-4); border-radius: 50%; }.status-dot[data-active="true"] { background: var(--zlm-success-500); box-shadow: 0 0 0 4px var(--zlm-success-50); }
.monitoring-panel h2 { margin: 4px 0 0; color: var(--zlm-text-1); font-size: 19px; }.monitoring-panel p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.55; }
.monitoring-panel__meta { display: flex; flex-direction: column; align-items: flex-end; gap: 5px; color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: 11px; }.monitoring-panel__meta span { display: inline-flex; align-items: center; gap: 5px; }
.monitoring-banner { margin: 10px 0; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.monitoring-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.monitoring-banner--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.monitoring-state { display: flex; min-height: 270px; flex-direction: column; align-items: center; justify-content: center; gap: 9px; color: var(--zlm-text-3); text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }.monitoring-state strong { color: var(--zlm-text-1); }.monitoring-state--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.runtime-summary-kpis { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; margin-top: 12px; }.runtime-kpi-button { min-width: 0; padding: 0; text-align: left; background: transparent; border: 0; border-radius: var(--zlm-radius-lg); cursor: pointer; }.runtime-kpi-button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }.runtime-kpi-button:hover :deep(.stat-card) { border-color: var(--zlm-brand-500); }
.runtime-summary-grid { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(0, .9fr); gap: 12px; margin-top: 12px; }.runtime-stream-panel { min-width: 0; padding: 14px 16px 12px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.runtime-stream-panel__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 10px; }.runtime-stream-panel h3 { margin: 0; color: var(--zlm-text-1); font-size: 14px; }.runtime-stream-panel p { margin: 4px 0 0; }
.runtime-streams { display: flex; flex-direction: column; gap: 7px; }.runtime-stream { display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: 9px; min-width: 0; padding: 9px 10px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.runtime-stream > span { min-width: 0; }.runtime-stream strong, .runtime-stream small { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.runtime-stream strong { color: var(--zlm-text-1); }.runtime-stream small { margin-top: 2px; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 10px; }.runtime-stream__right { display: inline-flex; align-items: center; gap: 4px; color: var(--zlm-text-3); font-size: 11px; }.runtime-empty { display: grid; min-height: 180px; place-items: center; color: var(--zlm-text-3); text-align: center; font-size: var(--zlm-fs-caption); }
.runtime-summary-footnote { margin-top: 8px; color: var(--zlm-text-3); font-size: 11px; text-align: right; }.runtime-summary-footnote[data-tone="warning"] { color: var(--zlm-warn-600); }.runtime-summary-footnote[data-tone="danger"] { color: var(--zlm-danger-600); }
@media (max-width: 1180px) { .runtime-summary-kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); } }.media-vchart { min-height: 100%; }
@media (max-width: 820px) { .monitoring-panel__header, .runtime-stream-panel__header { flex-direction: column; }.monitoring-panel__meta { align-items: flex-start; }.runtime-summary-grid { grid-template-columns: 1fr; } }.runtime-summary-kpis :deep(.stat-card) { min-height: 88px; }
@media (max-width: 560px) { .runtime-summary-kpis { grid-template-columns: 1fr; } }
</style>
