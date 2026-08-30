<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  listSchedulerLogs,
  listZLMNodes,
  type SchedulerAlgorithm,
  type SchedulerLogEntry,
  type SchedulerLogFilter
} from "@/api/gb28181-zlm";
import type { MediaNodeCatalogNode } from "@/store/modules/media-workbench";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import MediaVChart from "../components/MediaVChart.vue";
import { buildSchedulerChartState, createSchedulerNodeSpec, createSchedulerResultSpec } from "../chart/schedulerChart";
import { buildSchedulerLogFilter, type SchedulerLogFilterState } from "../../schedulerLogState";
import { safeSchedulerError, schedulerAlgorithmLabel } from "./schedulerLogPresentation";

const props = withDefaults(defineProps<{
  active?: boolean;
  autoRefresh?: boolean;
  nodes?: readonly MediaNodeCatalogNode[];
}>(), {
  active: true,
  autoRefresh: true
});

type SchedulerLogDraft = Omit<SchedulerLogFilterState, "timeRange"> & {
  timeRange: Array<string | number | Date>;
};

const logs = ref<SchedulerLogEntry[]>([]);
const discoveredNodes = ref<MediaNodeCatalogNode[]>([]);
const loading = ref(false);
const loadError = ref<unknown>(null);
const draft = ref<SchedulerLogDraft>({
  timeRange: [],
  nodeId: undefined,
  algorithm: undefined,
  result: undefined,
  streamId: "",
  limit: 100
});
const applied = ref<SchedulerLogDraft>({ ...draft.value, timeRange: [] });
const sampleFilter = ref<SchedulerLogFilter>({ limit: 100 });
const sampleAvailable = ref(false);
let generation = 0;
let timer: ReturnType<typeof setTimeout> | null = null;

const nodes = computed(() => props.nodes === undefined ? discoveredNodes.value : props.nodes);
const nodeOptions = computed(() => nodes.value.map(node => ({ label: `${node.name}（#${node.id}）`, value: node.id })));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const chartState = computed(() => buildSchedulerChartState(sampleAvailable.value ? logs.value : undefined, sampleFilter.value, { unavailable: !!loadError.value && !sampleAvailable.value }));
const resultSpec = computed(() => createSchedulerResultSpec(chartState.value));
const nodeSpec = computed(() => createSchedulerNodeSpec(chartState.value));
const limitOptions = [50, 100, 200, 500, 1000].map(value => ({ label: `${value} 条`, value }));
const algorithmOptions: Array<{ label: string; value: SchedulerAlgorithm }> = [
  { label: "轮询", value: "roundrobin" },
  { label: "加权轮询", value: "weighted" },
  { label: "最小负载", value: "leastload" }
];

function stopTimer() {
  if (timer !== null) clearTimeout(timer);
  timer = null;
}

function scheduleNext() {
  stopTimer();
  if (!props.active || !props.autoRefresh) return;
  timer = setTimeout(() => {
    timer = null;
    void refresh();
  }, 30_000);
}

async function loadNodes() {
  if (!props.active || props.nodes !== undefined) return;
  try {
    const response = await listZLMNodes();
    if (response.code === 0 && props.active) discoveredNodes.value = response.data?.list ?? [];
  } catch {
    // The log query remains usable without a node-name option list.
    discoveredNodes.value = [];
  }
}

async function fetchLogs(filter: SchedulerLogFilter, requestGeneration: number) {
  const response = await listSchedulerLogs(filter);
  if (requestGeneration !== generation || !props.active) return false;
  if (response.code !== 0) throw new Error(response.message || "调度日志加载失败");
  logs.value = response.data?.list ?? [];
  sampleFilter.value = filter;
  sampleAvailable.value = true;
  loadError.value = null;
  return true;
}

async function refresh() {
  if (!props.active) return false;
  const requestGeneration = ++generation;
  loading.value = true;
  try {
    const filter = buildSchedulerLogFilter(applied.value);
    return await fetchLogs(filter, requestGeneration);
  } catch (error) {
    if (requestGeneration === generation && props.active) loadError.value = error;
    return false;
  } finally {
    if (requestGeneration === generation) {
      loading.value = false;
      scheduleNext();
    }
  }
}

function applyFilters() {
  try {
    const filter = buildSchedulerLogFilter(draft.value);
    applied.value = { ...draft.value, timeRange: [...(draft.value.timeRange ?? [])] };
    void refresh();
    // Keep the request contract visible to assistive technology while the
    // response is pending; charts continue to describe the last completed sample.
    if (!filter) return;
  } catch (error) {
    Message.warning(error instanceof Error ? safeSchedulerError(error) : "筛选条件无效");
  }
}

function resetFilters() {
  draft.value = { timeRange: [], nodeId: undefined, algorithm: undefined, result: undefined, streamId: "", limit: 100 };
  applyFilters();
}

function formatTime(value: string) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function formatError(value: string) {
  return safeSchedulerError(value);
}

watch(() => [props.active, props.autoRefresh, props.nodes], ([active]) => {
  stopTimer();
  generation += 1;
  if (!active) {
    loading.value = false;
    return;
  }
  void loadNodes();
  void refresh();
}, { immediate: true });

onBeforeUnmount(() => {
  generation += 1;
  stopTimer();
});

defineExpose({ refresh });
</script>

<template>
  <section class="scheduler-log-panel" data-panel="scheduler-log" :aria-busy="loading ? 'true' : 'false'">
    <header class="panel-heading">
      <div>
        <span class="panel-eyebrow">DECISION AUDIT</span>
        <h2>调度日志</h2>
        <p>后端按筛选条件查询并限量返回；下方图表仅聚合本次筛选返回的样本。</p>
      </div>
      <div class="panel-actions">
        <span class="sample-count">当前样本 <strong>{{ chartState.sampleCount }}</strong> 条</span>
        <button type="button" class="refresh-button" :disabled="loading || !active" @click="refresh">{{ loading ? "查询中…" : "查询" }}</button>
      </div>
    </header>

    <div class="sample-boundary" role="status">{{ chartState.filterText }}<span v-if="chartState.warning"> · {{ chartState.warning }}</span></div>
    <div v-if="loadError && sampleAvailable" class="query-warning" role="status">本次查询失败：{{ errorPresentation.label }}；保留上一次已完成样本。</div>
    <div v-else-if="loadError" class="query-error" role="alert"><strong>{{ errorPresentation.label }}</strong><button v-if="errorPresentation.retryable" type="button" @click="refresh">重新查询</button></div>
    <div v-if="!active" class="inactive-state" role="status">当前视图未激活，未请求调度日志。</div>

    <div class="log-filters" role="search" aria-label="调度日志筛选">
      <label><span>时间</span><input v-model="draft.timeRange[0]" type="datetime-local" @change="applyFilters" /><input v-model="draft.timeRange[1]" type="datetime-local" @change="applyFilters" /></label>
      <label><span>节点</span><select v-model="draft.nodeId" @change="applyFilters"><option :value="undefined">全部节点</option><option v-for="node in nodeOptions" :key="node.value" :value="node.value">{{ node.label }}</option></select></label>
      <label><span>策略</span><select v-model="draft.algorithm" @change="applyFilters"><option :value="undefined">全部策略</option><option v-for="option in algorithmOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
      <label><span>结果</span><select v-model="draft.result" @change="applyFilters"><option :value="undefined">全部结果</option><option value="success">成功</option><option value="failure">失败</option></select></label>
      <label class="stream-filter"><span>业务流</span><input v-model="draft.streamId" type="search" placeholder="StreamID" @keyup.enter="applyFilters" /></label>
      <label><span>上限</span><select v-model.number="draft.limit" @change="applyFilters"><option v-for="option in limitOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
      <button type="button" class="reset-button" @click="resetFilters">重置</button>
    </div>

    <div class="chart-grid">
      <MediaVChart title="结果分布" :spec="resultSpec" :status="chartState.status" :status-text="chartState.summary" :summary="chartState.summary" :warning="chartState.warning" :as-of="chartState.asOf" :sampled-label="chartState.filterText" :active="active" />
      <MediaVChart title="命中节点" :spec="nodeSpec" :status="chartState.status" :status-text="chartState.summary" :summary="chartState.summary" :warning="chartState.warning" :as-of="chartState.asOf" :sampled-label="chartState.filterText" :active="active" />
    </div>

    <div v-if="active" class="log-table-wrap">
      <table class="log-table">
        <caption class="sr-only">调度日志，{{ chartState.filterText }}</caption>
        <thead><tr><th>时间</th><th>策略</th><th>命中节点</th><th>业务流</th><th>设备 / 通道</th><th>结果</th></tr></thead>
        <tbody>
          <tr v-for="entry in logs" :key="entry.id">
            <td>{{ formatTime(entry.happenedAt) }}</td>
            <td>{{ schedulerAlgorithmLabel(entry.algorithm) }}</td>
            <td>{{ entry.nodeName || `节点 #${entry.nodeID}` }}</td>
            <td class="mono">{{ entry.streamID || "—" }}</td>
            <td>{{ entry.deviceID || "—" }}<small v-if="entry.channelID"> / {{ entry.channelID }}</small></td>
            <td><span :class="entry.errorMessage ? 'result-failure' : 'result-success'">{{ entry.errorMessage ? formatError(entry.errorMessage) : "成功" }}</span></td>
          </tr>
        </tbody>
      </table>
      <div v-if="sampleAvailable && !logs.length" class="empty-state" role="status">当前筛选没有调度日志。</div>
    </div>
  </section>
</template>

<style scoped>
.scheduler-log-panel { box-sizing: border-box; min-width: 0; padding: 18px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-xl); box-shadow: var(--uvp-panel-shadow); }
.panel-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }.panel-eyebrow { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); font-size: 10px; letter-spacing: .12em; }.panel-heading h2 { margin: 4px 0 0; color: var(--zlm-text-1); font-size: 18px; }.panel-heading p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.panel-actions { display: flex; align-items: center; gap: 10px; }.sample-count { color: var(--zlm-text-3); font-size: 12px; white-space: nowrap; }.sample-count strong { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); }.panel-actions button, .query-error button, .reset-button { min-height: 32px; padding: 0 11px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; }.panel-actions button:hover, .query-error button:hover, .reset-button:hover { color: var(--zlm-brand-600); border-color: var(--zlm-brand-500); }.panel-actions button:disabled { cursor: wait; opacity: .6; }
.sample-boundary { margin-top: 16px; padding: 10px 12px; color: var(--zlm-text-2); font-size: var(--zlm-fs-caption); line-height: 1.6; background: var(--zlm-brand-50); border: 1px solid var(--zlm-brand-200); border-radius: var(--zlm-radius-md); overflow-wrap: anywhere; }.sample-boundary span { color: var(--zlm-warn-600); }.query-warning, .query-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); background: var(--zlm-warn-50); border: 1px solid var(--zlm-warn-300); border-radius: var(--zlm-radius-md); }.query-error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-200); }.log-filters { display: flex; align-items: flex-end; gap: 8px; margin-top: 16px; padding: 12px; overflow-x: auto; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.log-filters label { display: flex; min-width: 105px; flex-direction: column; gap: 4px; }.log-filters label > span { color: var(--zlm-text-3); font-size: 11px; }.log-filters input, .log-filters select { min-width: 0; height: 32px; padding: 0 8px; color: var(--zlm-text-1); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.log-filters input[type="datetime-local"] { width: 165px; }.log-filters .stream-filter input { width: 150px; }.log-filters button { flex: none; }.chart-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin-top: 16px; }.log-table-wrap { margin-top: 16px; overflow: auto; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.log-table { width: 100%; min-width: 760px; border-collapse: collapse; font-size: 12px; }.log-table th, .log-table td { padding: 10px 12px; text-align: left; border-bottom: 1px solid var(--zlm-border); }.log-table th { color: var(--zlm-text-3); font-weight: var(--zlm-fw-medium); background: var(--zlm-fill-1); }.log-table td { color: var(--zlm-text-2); }.log-table tr:last-child td { border-bottom: 0; }.log-table small { color: var(--zlm-text-4); }.mono { font-family: var(--zlm-font-mono); }.result-success { color: var(--zlm-success-600); }.result-failure { display: inline-block; max-width: 260px; color: var(--zlm-danger-600); overflow-wrap: anywhere; }.empty-state, .inactive-state { display: grid; min-height: 160px; padding: 20px; color: var(--zlm-text-3); place-items: center; }.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
button:focus-visible, input:focus-visible, select:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
@media (max-width: 900px) { .chart-grid { grid-template-columns: 1fr; } }
@media (max-width: 760px) { .panel-heading { flex-direction: column; }.panel-actions { width: 100%; justify-content: space-between; }.log-filters { align-items: stretch; flex-direction: column; overflow: visible; }.log-filters label, .log-filters input[type="datetime-local"], .log-filters .stream-filter input { width: 100%; }.reset-button { align-self: flex-start; } }
</style>
