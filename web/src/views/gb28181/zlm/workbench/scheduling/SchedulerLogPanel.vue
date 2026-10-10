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
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import { useDictLabel } from "@/hooks/useDictOptions";
import { DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK } from "@/views/gb28181/mediaNodeState";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import MediaVChart from "../components/MediaVChart.vue";
import { buildSchedulerChartState, createSchedulerNodeSpec, createSchedulerResultSpec } from "../chart/schedulerChart";
import {
  buildSchedulerChartWindowFilter,
  buildSchedulerLogFilter,
  SCHEDULER_CHART_PERIODS,
  type SchedulerChartPeriod,
  type SchedulerLogFilterState
} from "../../schedulerLogState";
import { safeSchedulerError, schedulerAlgorithmLabel } from "./schedulerLogPresentation";

const props = withDefaults(
  defineProps<{
    active?: boolean;
    autoRefresh?: boolean;
    nodes?: readonly MediaNodeCatalogNode[];
    scope?: MediaScope;
    initialResult?: SchedulerLogFilterState["result"];
  }>(),
  {
    active: true,
    autoRefresh: true,
    scope: "all"
  }
);

const emit = defineEmits<{
  "update:scope": [scope: MediaScope];
}>();

type SchedulerLogDraft = Omit<SchedulerLogFilterState, "timeRange"> & {
  timeRange: Array<string | number | Date>;
};

const logs = ref<SchedulerLogEntry[]>([]);
const discoveredNodes = ref<MediaNodeCatalogNode[]>([]);
const loading = ref(false);
const loadError = ref<unknown>(null);
const draft = ref<SchedulerLogDraft>({
  timeRange: [],
  nodeId: typeof props.scope === "number" ? props.scope : undefined,
  algorithm: undefined,
  result: props.initialResult,
  streamId: ""
});
const applied = ref<SchedulerLogDraft>({ ...draft.value, timeRange: [] });
const sampleAvailable = ref(false);
const chartPeriod = ref<SchedulerChartPeriod>("24h");
const chartLogs = ref<Record<SchedulerChartPeriod, SchedulerLogEntry[] | undefined>>({ "24h": undefined, "7d": undefined });
const chartFilters = ref<Record<SchedulerChartPeriod, SchedulerLogFilter>>({ "24h": {}, "7d": {} });
const chartErrors = ref<Record<SchedulerChartPeriod, unknown>>({ "24h": null, "7d": null });
const page = ref(1);
const pageSize = ref(10);
let generation = 0;
let timer: ReturnType<typeof setTimeout> | null = null;
const chartPageLimit = 1000;

/** 节点状态文案（`media_node_state` 字典）。 */
const mediaNodeStateLabel = useDictLabel(DICT_CODE_MEDIA_NODE_STATE, MEDIA_NODE_STATE_LABEL_FALLBACK);

const nodes = computed(() => (props.nodes === undefined ? discoveredNodes.value : props.nodes));
const nodeOptions = computed(() =>
  nodes.value.map(node => ({ label: `${node.name} · ${mediaNodeStateLabel(node.state)}`, value: node.id }))
);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const chartState = computed(() =>
  buildSchedulerChartState(chartLogs.value[chartPeriod.value], chartFilters.value[chartPeriod.value], {
    period: chartPeriod.value,
    unavailable: !!chartErrors.value[chartPeriod.value] && chartLogs.value[chartPeriod.value] === undefined
  })
);
const resultSpec = computed(() => createSchedulerResultSpec(chartState.value));
const nodeSpec = computed(() => createSchedulerNodeSpec(chartState.value));
const visibleLogs = computed(() => logs.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value));
const tableScroll = computed(() => ({ x: 1148, ...(visibleLogs.value.length ? { y: "100%" } : {}) }));
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
  sampleAvailable.value = true;
  page.value = 1;
  loadError.value = null;
  return true;
}

async function fetchChart(period: SchedulerChartPeriod, filter: SchedulerLogFilter, requestGeneration: number) {
  const fetchRange = async (from: string, to: string): Promise<SchedulerLogEntry[]> => {
    const response = await listSchedulerLogs({ ...filter, from, to, limit: chartPageLimit });
    if (requestGeneration !== generation || !props.active) return [];
    if (response.code !== 0) throw new Error(response.message || "调度图表加载失败");
    const rows = response.data?.list ?? [];
    if (rows.length < chartPageLimit) return rows;
    const startMs = Date.parse(from);
    const endMs = Date.parse(to);
    const midpoint = startMs + Math.floor((endMs - startMs) / 2);
    if (!Number.isFinite(startMs) || !Number.isFinite(endMs) || midpoint <= startMs || midpoint >= endMs) {
      throw new Error("调度图表时间片内日志超过查询上限");
    }
    const leftTo = new Date(midpoint).toISOString();
    const [left, right] = await Promise.all([fetchRange(from, leftTo), fetchRange(leftTo, to)]);
    return [...left, ...right];
  };
  try {
    if (!filter.from || !filter.to) throw new Error("调度图表时间范围缺失");
    const rows = await fetchRange(filter.from, filter.to);
    if (requestGeneration !== generation || !props.active) return false;
    const unique = new Map<number, SchedulerLogEntry>();
    for (const row of rows) unique.set(row.id, row);
    chartLogs.value[period] = [...unique.values()].sort((left, right) => right.happenedAt.localeCompare(left.happenedAt));
    chartFilters.value[period] = filter;
    chartErrors.value[period] = null;
    return true;
  } catch (error) {
    if (requestGeneration === generation && props.active) chartErrors.value[period] = error;
    return false;
  }
}

async function refresh() {
  if (!props.active) return false;
  const requestGeneration = ++generation;
  loading.value = true;
  try {
    const tableFilter = buildSchedulerLogFilter(applied.value);
    const chartFiltersForPeriod = SCHEDULER_CHART_PERIODS.map(period => ({
      period: period.key,
      filter: buildSchedulerChartWindowFilter(applied.value, period.key)
    }));
    const [tableLoaded, ...chartsLoaded] = await Promise.all([
      fetchLogs(tableFilter, requestGeneration),
      ...chartFiltersForPeriod.map(({ period, filter }) => fetchChart(period, filter, requestGeneration))
    ]);
    return tableLoaded && chartsLoaded.every(Boolean);
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

function queryFilters() {
  applyFilters();
}

function changeNode(value: string | number | Event) {
  const rawValue = value instanceof Event ? (value.target as HTMLSelectElement).value : value;
  if (rawValue === "all") {
    emit("update:scope", "all");
    return;
  }
  const nodeId = Number(rawValue);
  if (Number.isSafeInteger(nodeId) && nodeId > 0 && nodes.value.some(node => node.id === nodeId)) {
    emit("update:scope", nodeId);
  }
}

function changePage(nextPage: number) {
  page.value = nextPage;
}

function changePageSize(nextPageSize: number) {
  pageSize.value = nextPageSize;
  page.value = 1;
}

function resetFilters() {
  draft.value = {
    timeRange: [],
    nodeId: typeof props.scope === "number" ? props.scope : undefined,
    algorithm: undefined,
    result: undefined,
    streamId: ""
  };
  queryFilters();
}

function formatTime(value: string) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function formatError(value: string) {
  return safeSchedulerError(value);
}

function schedulerNodeLabel(record: SchedulerLogEntry) {
  if (record.nodeName) return record.nodeName;
  return record.nodeID > 0 ? `节点 #${record.nodeID}` : "—";
}

watch(
  () => [props.active, props.autoRefresh, props.nodes, props.scope],
  ([active]) => {
    stopTimer();
    generation += 1;
    if (!active) {
      loading.value = false;
      return;
    }
    const scopedNodeId = typeof props.scope === "number" ? props.scope : undefined;
    draft.value.nodeId = scopedNodeId;
    applied.value.nodeId = scopedNodeId;
    chartLogs.value = { "24h": undefined, "7d": undefined };
    chartErrors.value = { "24h": null, "7d": null };
    void loadNodes();
    void refresh();
  },
  { immediate: true }
);

watch(
  () => props.initialResult,
  result => {
    if (draft.value.result === result && applied.value.result === result) return;
    draft.value.result = result;
    applied.value.result = result;
    chartLogs.value = { "24h": undefined, "7d": undefined };
    chartErrors.value = { "24h": null, "7d": null };
    if (props.active) void refresh();
  }
);

onBeforeUnmount(() => {
  generation += 1;
  stopTimer();
});

defineExpose({ refresh });
</script>

<template>
  <section class="scheduler-log-panel" data-panel="scheduler-log" :aria-busy="loading ? 'true' : 'false'">
    <div v-if="loadError && sampleAvailable" class="query-warning" role="status">
      本次查询失败：{{ errorPresentation.label }}；保留上一次已完成样本。
    </div>
    <div v-else-if="loadError" class="query-error" role="alert">
      <strong>{{ errorPresentation.label }}</strong
      ><button v-if="errorPresentation.retryable" type="button" @click="refresh">重新查询</button>
    </div>
    <div v-if="!active" class="inactive-state" role="status">当前视图未激活，未请求调度日志。</div>

    <s-layout-search class="scheduler-log-search">
      <template #fields>
        <a-range-picker
          v-model="draft.timeRange"
          show-time
          allow-clear
          format="YYYY-MM-DD HH:mm"
          value-format="YYYY-MM-DDTHH:mm:ssZ"
          class="time-filter"
        />
        <a-select
          data-testid="node-filter"
          :model-value="scope"
          :options="[{ label: '全部节点', value: 'all' }, ...nodeOptions]"
          :disabled="!nodeOptions.length"
          placeholder="节点"
          class="node-filter"
          @change="changeNode"
        />
        <a-select
          v-model="draft.algorithm"
          :options="[{ label: '全部策略', value: undefined }, ...algorithmOptions]"
          placeholder="策略"
          class="algorithm-filter"
        />
        <a-select
          v-model="draft.result"
          :options="[
            { label: '全部调度状态', value: undefined },
            { label: '已命中', value: 'success' },
            { label: '未命中', value: 'failure' }
          ]"
          placeholder="调度状态"
          class="result-filter"
        />
        <a-input-search
          v-model="draft.streamId"
          allow-clear
          placeholder="业务流 StreamID"
          class="stream-filter"
          @search="queryFilters"
          @press-enter="queryFilters"
        />
      </template>
      <template #actions>
        <a-button type="primary" @click="queryFilters"
          ><template #icon><icon-search /></template>查询</a-button
        >
        <a-button @click="resetFilters"
          ><template #icon><icon-refresh /></template>重置</a-button
        >
      </template>
    </s-layout-search>

    <div class="chart-grid">
      <div
        v-for="chart in [
          { title: '调度命中分布', spec: resultSpec },
          { title: '命中节点', spec: nodeSpec }
        ]"
        :key="chart.title"
        class="chart-card"
      >
        <MediaVChart
          :title="chart.title"
          :spec="chart.spec"
          :status="chartState.status"
          :status-text="chartState.summary"
          summary=""
          :show-summary="false"
          :active="active"
        >
          <template #header-actions>
            <div class="chart-period-tabs" data-testid="scheduler-chart-periods" role="tablist" aria-label="图表统计周期">
              <button
                v-for="period in SCHEDULER_CHART_PERIODS"
                :key="period.key"
                type="button"
                role="tab"
                :aria-selected="chartPeriod === period.key"
                :class="{ 'is-active': chartPeriod === period.key }"
                @click="chartPeriod = period.key"
              >
                {{ period.label }}
              </button>
            </div>
          </template>
        </MediaVChart>
      </div>
    </div>

    <section v-if="active" class="log-table-panel" aria-label="调度日志列表">
      <a-table
        :data="visibleLogs"
        :loading="loading"
        row-key="id"
        :pagination="false"
        :scroll="tableScroll"
        class="uvp-data-table scheduler-log-table"
      >
        <template #columns>
          <a-table-column title="时间" :width="168"
            ><template #cell="{ record }">{{ formatTime(record.happenedAt) }}</template></a-table-column
          >
          <a-table-column title="策略" :width="100"
            ><template #cell="{ record }">{{ schedulerAlgorithmLabel(record.algorithm) }}</template></a-table-column
          >
          <a-table-column title="命中节点" :width="140"
            ><template #cell="{ record }">{{ schedulerNodeLabel(record) }}</template></a-table-column
          >
          <a-table-column title="业务流" :width="170"
            ><template #cell="{ record }"
              ><span class="mono">{{ record.streamID || "—" }}</span></template
            ></a-table-column
          >
          <a-table-column title="设备 / 通道" :width="250"
            ><template #cell="{ record }"
              >{{ record.deviceID || "—" }}<small v-if="record.channelID"> / {{ record.channelID }}</small></template
            ></a-table-column
          >
          <a-table-column title="调度状态" :width="100"
            ><template #cell="{ record }"
              ><span :class="record.errorMessage ? 'scheduler-miss' : 'scheduler-hit'">{{
                record.errorMessage ? "未命中" : "已命中"
              }}</span></template
            ></a-table-column
          >
          <a-table-column title="未命中原因" :width="220"
            ><template #cell="{ record }"
              ><span :class="record.errorMessage ? 'scheduler-reason' : 'scheduler-reason scheduler-reason--empty'">{{
                record.errorMessage ? formatError(record.errorMessage) : "—"
              }}</span></template
            ></a-table-column
          >
        </template>
        <template #empty
          ><div class="empty-state" role="status">
            <strong>当前筛选没有调度日志</strong><span>调整筛选条件后重新查询。</span>
          </div></template
        >
      </a-table>
    </section>
    <div v-if="active && sampleAvailable && logs.length" class="log-pagination uvp-pagination-bar">
      <span>共 {{ logs.length }} 条调度日志</span
      ><a-pagination
        :current="page"
        :page-size="pageSize"
        :total="logs.length"
        show-page-size
        :page-size-options="[10, 20, 50, 100]"
        @change="changePage"
        @page-size-change="changePageSize"
      />
    </div>
  </section>
</template>

<style scoped>
.scheduler-log-panel {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  width: 100%;
  min-width: 0;
  height: 100%;
  min-height: 0;
  padding: 0;
  overflow: hidden;
  color: var(--zlm-text-2);
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.query-warning,
.query-error {
  display: flex;
  flex: none;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  margin-top: 12px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-warn-600);
  background: var(--zlm-warn-50);
  border: 1px solid var(--zlm-warn-300);
  border-radius: var(--zlm-radius-md);
}
.query-error {
  color: var(--zlm-danger-600);
  background: var(--zlm-danger-50);
  border-color: var(--zlm-danger-200);
}
.scheduler-log-search {
  flex: none;
  margin-bottom: 14px;
}
.scheduler-log-search :deep(.arco-input-wrapper),
.scheduler-log-search :deep(.arco-select-view-single),
.scheduler-log-search :deep(.arco-picker) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}
.scheduler-log-search :deep(.arco-input-wrapper:hover),
.scheduler-log-search :deep(.arco-select-view-single:hover),
.scheduler-log-search :deep(.arco-picker:hover),
.scheduler-log-search :deep(.arco-input-wrapper.arco-input-focus),
.scheduler-log-search :deep(.arco-select-view-focus),
.scheduler-log-search :deep(.arco-picker-focused) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
.scheduler-log-search :deep(.time-filter) {
  width: 300px;
}
.scheduler-log-search :deep(.node-filter) {
  width: 150px;
}
.scheduler-log-search :deep(.algorithm-filter),
.scheduler-log-search :deep(.result-filter) {
  width: 130px;
}
.scheduler-log-search :deep(.stream-filter) {
  width: 190px;
}
.chart-grid {
  display: grid;
  flex: none;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin-top: 0;
}
.chart-card {
  min-width: 0;
}
.chart-period-tabs {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  background: var(--zlm-fill-2);
  border-radius: var(--zlm-radius-md);
}
.chart-period-tabs button {
  min-height: 28px;
  padding: 0 10px;
  color: var(--zlm-text-3);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: var(--zlm-radius-sm);
}
.chart-period-tabs button:hover,
.chart-period-tabs button.is-active {
  color: var(--zlm-brand-700);
  background: var(--zlm-card);
}
.log-table-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  margin-top: 14px;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}
.scheduler-log-table {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.scheduler-log-table :deep(.arco-table-container) {
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.scheduler-log-table :deep(.arco-table-cell) {
  font-size: 12px;
}
.scheduler-log-table :deep(.arco-table-td) {
  height: 48px;
}
.scheduler-log-table :deep(.arco-table-th) {
  height: 44px;
}
.scheduler-log-table small {
  color: var(--zlm-text-4);
}
.mono {
  font-family: var(--zlm-font-mono);
}
.scheduler-hit {
  color: var(--zlm-success-600);
}
.scheduler-miss,
.scheduler-reason {
  color: var(--zlm-danger-600);
}
.scheduler-reason {
  display: inline-block;
  max-width: 220px;
  overflow-wrap: anywhere;
}
.scheduler-reason--empty {
  color: var(--zlm-text-4);
}
.empty-state {
  display: flex;
  flex-direction: column;
  gap: 7px;
  align-items: center;
  justify-content: center;
  min-height: 180px;
  padding: 32px 16px;
  color: var(--zlm-text-3);
}
.empty-state strong {
  color: var(--zlm-text-1);
}
.log-pagination {
  display: flex;
  flex: none;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  margin-top: 12px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
button:focus-visible,
input:focus-visible,
select:focus-visible {
  outline: 2px solid var(--zlm-brand-500);
  outline-offset: 2px;
}

@media (width <= 1200px) {
  .scheduler-log-search :deep(.time-filter),
  .scheduler-log-search :deep(.stream-filter) {
    width: 230px;
  }
}

@media (width <= 900px) {
  .chart-grid {
    grid-template-columns: 1fr;
  }
}

@media (width <= 760px) {
  .scheduler-log-search :deep(.time-filter),
  .scheduler-log-search :deep(.node-filter),
  .scheduler-log-search :deep(.algorithm-filter),
  .scheduler-log-search :deep(.result-filter),
  .scheduler-log-search :deep(.stream-filter) {
    width: 100%;
  }
  .log-pagination {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
