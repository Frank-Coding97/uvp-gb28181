<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  listSchedulerLogs,
  listZLMNodes,
  type SchedulerAlgorithm,
  type SchedulerLogEntry
} from "@/api/gb28181-zlm";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import { zlmErrorPresentation } from "./components/zlmFormatters";
import { buildSchedulerLogFilter, type SchedulerLogFilterState } from "./schedulerLogState";

const logs = ref<SchedulerLogEntry[]>([]);
const nodes = ref<Array<{ label: string; value: number }>>([]);
const loading = ref(true);
const loadError = ref<unknown>(null);
const autoRefresh = ref(true);
const singletonScope = ref<number | null>(1);
const pollingPaused = computed(() => !autoRefresh.value);

const draft = reactive<SchedulerLogFilterState>({
  timeRange: [],
  nodeId: undefined,
  algorithm: undefined,
  result: undefined,
  streamId: "",
  limit: 100
});
const applied = ref<SchedulerLogFilterState>({ ...draft, timeRange: [] });

const limitOptions = [
  { label: "50 条", value: 50 },
  { label: "100 条", value: 100 },
  { label: "200 条", value: 200 },
  { label: "500 条", value: 500 }
];
const algorithmOptions: Array<{ label: string; value: SchedulerAlgorithm }> = [
  { label: "轮询", value: "roundrobin" },
  { label: "加权轮询", value: "weighted" },
  { label: "最小负载", value: "leastload" }
];
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
let manualGeneration = 0;

async function fetchLogs(state: SchedulerLogFilterState) {
  const filter = buildSchedulerLogFilter(state);
  const response = await listSchedulerLogs(filter);
  if (response.code !== 0) throw new Error(response.message || "调度日志加载失败");
  return response.data?.list ?? [];
}

function publishLogs(value: SchedulerLogEntry[]) {
  logs.value = value;
  loadError.value = null;
  loading.value = false;
}

function publishError(error: unknown) {
  loadError.value = error;
  loading.value = false;
}

const { refresh } = useZLMRuntimePolling<SchedulerLogEntry[]>({
  nodeId: singletonScope,
  paused: pollingPaused,
  intervalMs: 30_000,
  async load() {
    loading.value = true;
    return fetchLogs(applied.value);
  },
  publish: publishLogs,
  onError: publishError
});

async function loadNodes() {
  try {
    const response = await listZLMNodes();
    if (response.code === 0) {
      nodes.value = (response.data?.list ?? []).map(node => ({ label: `${node.name}（#${node.id}）`, value: node.id }));
    }
  } catch {
    nodes.value = [];
  }
}

async function manualRefresh() {
  const currentGeneration = ++manualGeneration;
  loading.value = true;
  try {
    const value = await fetchLogs(applied.value);
    if (currentGeneration === manualGeneration && !autoRefresh.value) publishLogs(value);
  } catch (error) {
    if (currentGeneration === manualGeneration && !autoRefresh.value) publishError(error);
  }
}

function applyFilters() {
  try {
    buildSchedulerLogFilter(draft);
    applied.value = { ...draft, timeRange: [...(draft.timeRange ?? [])] };
    if (autoRefresh.value) refresh();
    else void manualRefresh();
  } catch (error) {
    Message.warning((error as Error)?.message || "筛选条件无效");
  }
}

function resetFilters() {
  Object.assign(draft, {
    timeRange: [],
    nodeId: undefined,
    algorithm: undefined,
    result: undefined,
    streamId: "",
    limit: 100
  });
  applyFilters();
}

function toggleAutoRefresh(value: boolean | string | number) {
  autoRefresh.value = Boolean(value);
  if (autoRefresh.value) manualGeneration += 1;
  else void manualRefresh();
}

function formatTime(value: string) {
  if (!value) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat scheduler-log-shell">
      <div class="scheduler-log">
        <div class="log-boundary" role="status">
          筛选条件由后端数据库查询执行；页面不会先拉取全量日志再做本地过滤。
        </div>

        <s-layout-search class="scheduler-log-search">
          <template #fields>
            <a-range-picker
              v-model="draft.timeRange"
              show-time
              allow-clear
              value-format="timestamp"
              class="time-filter"
              aria-label="时间范围"
              @change="applyFilters"
            />
            <a-select v-model="draft.nodeId" :options="nodes" allow-clear placeholder="节点" class="short-filter" @change="applyFilters" />
            <a-select v-model="draft.algorithm" :options="algorithmOptions" allow-clear placeholder="策略" class="short-filter" @change="applyFilters" />
            <a-select
              v-model="draft.result"
              allow-clear
              placeholder="结果"
              class="short-filter"
              :options="[
                { label: '成功', value: 'success' },
                { label: '失败', value: 'failure' }
              ]"
              @change="applyFilters"
            />
            <a-input-search
              v-model="draft.streamId"
              allow-clear
              placeholder="业务流 StreamID"
              class="stream-filter"
              @search="applyFilters"
              @press-enter="applyFilters"
            />
            <a-select v-model="draft.limit" :options="limitOptions" placeholder="日志条数" class="short-filter" @change="applyFilters" />
          </template>
          <template #actions>
            <span class="log-count">{{ logs.length }} 条日志</span>
            <a-switch
              :model-value="autoRefresh"
              checked-text="自动 30s"
              unchecked-text="手动"
              class="auto-refresh"
              @change="toggleAutoRefresh"
            />
            <a-button @click="resetFilters">重置</a-button>
            <a-button class="uvp-refresh-btn" :loading="loading" aria-label="刷新调度日志" @click="applyFilters">
              <template #icon><icon-refresh /></template>查询
            </a-button>
          </template>
        </s-layout-search>

        <div v-if="loadError && logs.length" class="log-state log-state--warning" role="status">
          本次查询失败：{{ errorPresentation.label }}。已保留上一次结果。
        </div>
        <div v-if="loading && !logs.length" class="log-state" role="status" aria-label="正在加载调度日志"><a-spin /><span>正在查询调度日志…</span></div>
        <div v-else-if="loadError && !logs.length" class="log-state log-state--error" role="alert">
          <strong>{{ errorPresentation.label }}</strong>
          <a-button v-if="errorPresentation.retryable" @click="applyFilters">重新查询</a-button>
        </div>

        <div v-else class="scheduler-log-table">
          <a-table :data="logs" :loading="loading" row-key="id" :pagination="false" size="small" class="uvp-data-table">
            <template #columns>
              <a-table-column title="时间" :width="180"><template #cell="{ record }">{{ formatTime(record.happenedAt) }}</template></a-table-column>
              <a-table-column title="策略" :width="120"><template #cell="{ record }"><a-tag v-if="record.algorithm" size="small" color="arcoblue">{{ record.algorithm }}</a-tag><span v-else>—</span></template></a-table-column>
              <a-table-column title="命中节点" :width="190"><template #cell="{ record }"><span v-if="record.nodeName">{{ record.nodeName }} <small>#{{ record.nodeID }}</small></span><span v-else>—</span></template></a-table-column>
              <a-table-column title="业务流" :width="210"><template #cell="{ record }"><span class="mono">{{ record.streamID || "—" }}</span></template></a-table-column>
              <a-table-column title="设备 / 通道" :width="220"><template #cell="{ record }">{{ record.deviceID || "—" }}<small v-if="record.channelID"> / {{ record.channelID }}</small></template></a-table-column>
              <a-table-column title="结果">
                <template #cell="{ record }">
                  <div v-if="record.errorMessage" class="result result--error"><strong>失败</strong><span>{{ record.errorMessage }}</span></div>
                  <span v-else class="result result--success">成功</span>
                </template>
              </a-table-column>
            </template>
            <template #empty>
              <div class="empty" role="status"><strong>没有符合条件的调度日志</strong><span>调整时间、结果、节点、策略或业务流条件后重试。</span></div>
            </template>
          </a-table>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.scheduler-log-shell { padding: 4px 8px; overflow: hidden; }
.scheduler-log { height: 100%; overflow: auto; }
.log-boundary { margin-bottom: 12px; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-brand-50); border: 1px solid var(--zlm-brand-200); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
.scheduler-log-search { margin-bottom: 16px; }
.time-filter { width: 330px; }
.short-filter { width: 125px; }
.stream-filter { width: 190px; }
.log-count { color: var(--uvp-text-tertiary); font-size: 12px; white-space: nowrap; }
.auto-refresh { min-width: 92px; }
.log-state { display: flex; min-height: 220px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }
.log-state--warning { min-height: auto; align-items: flex-start; margin-bottom: 12px; padding: 10px 12px; color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }
.log-state--error { color: var(--zlm-danger-600); }
.scheduler-log-table { overflow: hidden; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.scheduler-log-table small { color: var(--zlm-text-4); }
.mono { font-family: var(--zlm-font-mono); }
.result { display: inline-flex; align-items: flex-start; gap: 7px; }
.result--success { color: var(--zlm-success-600); font-weight: var(--zlm-fw-medium); }
.result--error { color: var(--zlm-danger-600); }
.result--error span { color: var(--zlm-text-2); overflow-wrap: anywhere; }
.empty { display: flex; flex-direction: column; align-items: center; gap: 7px; padding: 44px 16px; color: var(--zlm-text-3); }
.empty strong { color: var(--zlm-text-1); }
:deep(.arco-btn), :deep(.arco-input-wrapper), :deep(.arco-select-view), :deep(.arco-picker) { border-radius: 10px; }
@media (max-width: 900px) { .time-filter, .short-filter, .stream-filter { width: 100%; } }
</style>
