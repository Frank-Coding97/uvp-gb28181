<template>
  <div class="snow-fill playback-log-page">
    <div class="snow-fill-inner uvp-page-shell-flat playback-log-shell">
      <s-layout-search class="playback-log-search">
        <template #fields>
          <a-input v-model="filters.deviceCode" allow-clear placeholder="设备编码" style="width: 190px" @press-enter="query" />
          <a-input v-model="filters.channelCode" allow-clear placeholder="通道编码" style="width: 190px" @press-enter="query" />
          <a-input v-model="filters.streamId" allow-clear placeholder="Stream ID" style="width: 180px" @press-enter="query" />
          <a-select v-model="filters.mediaState" allow-clear placeholder="媒体状态" style="width: 138px">
            <a-option value="ready">媒体已就绪</a-option>
            <a-option value="failed">媒体失败</a-option>
            <a-option value="stopped">媒体已停止</a-option>
            <a-option value="unknown">媒体未知</a-option>
          </a-select>
          <a-select v-model="filters.clientState" allow-clear placeholder="客户端状态" style="width: 138px">
            <a-option value="first_frame">已见首帧</a-option>
            <a-option value="failed">客户端失败</a-option>
            <a-option value="unknown">客户端未知</a-option>
          </a-select>
          <a-range-picker
            v-model="filters.range"
            show-time
            value-format="YYYY-MM-DDTHH:mm:ssZ"
            allow-clear
            style="width: 310px"
          />
        </template>
        <template #actions>
          <a-button type="primary" :loading="loading" @click="query"
            ><template #icon><Search :size="15" /></template>查询</a-button
          >
          <a-button :disabled="loading" @click="reset"
            ><template #icon><RotateCcw :size="15" /></template>重置</a-button
          >
          <a-button :disabled="loading" title="刷新" @click="loadRows"
            ><template #icon><RefreshCw :size="15" /></template
          ></a-button>
        </template>
      </s-layout-search>

      <a-alert v-if="errorMessage" class="playback-log-error" type="error" closable @close="errorMessage = ''">
        {{ errorMessage }}
      </a-alert>

      <section class="playback-log-table-panel">
        <a-table
          class="playback-log-table"
          row-key="lifecycleId"
          :columns="columns"
          :data="rows"
          :loading="loading"
          :pagination="false"
          :bordered="false"
          :scroll="{ x: 1320, y: '100%' }"
        >
          <template #startedAt="{ record }"
            ><span class="mono">{{ formatTime(record.startedAt) }}</span></template
          >
          <template #identity="{ record }">
            <div class="identity-cell">
              <strong>{{ record.deviceCode }}</strong
              ><span>{{ record.channelCode }}</span>
            </div>
          </template>
          <template #stream="{ record }">
            <div class="identity-cell">
              <strong class="mono">{{ record.streamId || "-" }}</strong
              ><span>节点 {{ record.nodeId || "-" }}</span>
            </div>
          </template>
          <template #mediaState="{ record }"
            ><a-tag :color="stateColor(record.mediaState)">{{ mediaStateLabel(record.mediaState) }}</a-tag></template
          >
          <template #clientState="{ record }"
            ><a-tag :color="stateColor(record.clientState)">{{ clientStateLabel(record.clientState) }}</a-tag></template
          >
          <template #reason="{ record }"
            ><span :title="record.reasonMessage">{{ record.reasonCode || "-" }}</span></template
          >
          <template #actions="{ record }">
            <a-button type="text" size="small" title="查看详情" @click="openDetail(record)"
              ><template #icon><Eye :size="16" /></template
            ></a-button>
          </template>
          <template #empty><a-empty description="暂无播放日志" /></template>
        </a-table>
        <footer class="playback-log-pagination">
          <span>共 {{ pagination.total }} 条</span>
          <a-pagination
            :current="pagination.page"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            :page-size-options="PLAYBACK_LOG_PAGE_SIZE_OPTIONS"
            show-total
            show-page-size
            show-jumper
            @change="changePage"
            @page-size-change="changePageSize"
          />
        </footer>
      </section>
    </div>

    <a-drawer v-model:visible="detailVisible" :width="560" title="播放生命周期" unmount-on-close>
      <a-spin :loading="detailLoading" class="detail-spin">
        <template v-if="detail">
          <div class="detail-summary">
            <div>
              <span>Lifecycle</span><strong class="mono">{{ detail.lifecycle.lifecycleId }}</strong>
            </div>
            <div>
              <span>设备 / 通道</span><strong>{{ detail.lifecycle.deviceCode }} / {{ detail.lifecycle.channelCode }}</strong>
            </div>
            <div>
              <span>媒体状态</span
              ><a-tag :color="stateColor(detail.lifecycle.mediaState)">{{ mediaStateLabel(detail.lifecycle.mediaState) }}</a-tag>
            </div>
            <div>
              <span>客户端状态</span
              ><a-tag :color="stateColor(detail.lifecycle.clientState)">{{
                clientStateLabel(detail.lifecycle.clientState)
              }}</a-tag>
            </div>
          </div>
          <a-timeline class="lifecycle-timeline">
            <a-timeline-item v-for="event in detail.events" :key="event.sequence" :dot-color="stateColor(event.factState)">
              <div class="timeline-head">
                <strong>{{ event.eventName }}</strong
                ><a-tag size="small" :color="stateColor(event.factState)">{{ factStateLabel(event.factState) }}</a-tag>
              </div>
              <div class="timeline-meta">
                <span>{{ event.stage }}</span
                ><span>{{ event.source }}</span
                ><span>{{ formatTime(event.eventAt) }}</span>
              </div>
              <div v-if="event.reasonCode" class="timeline-reason">
                {{ event.reasonCode }}<span v-if="event.reasonMessage"> · {{ event.reasonMessage }}</span>
              </div>
            </a-timeline-item>
          </a-timeline>
        </template>
      </a-spin>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { Eye, RefreshCw, RotateCcw, Search } from "@lucide/vue";
import { getPlayLifecycle, listPlayLifecycles, type PlayLifecycleEvent, type PlayLifecycleSummary } from "@/api/gb28181";
import {
  clientStateLabel,
  createPlaybackLogQuery,
  factStateLabel,
  mediaStateLabel,
  PLAYBACK_LOG_PAGE_SIZE,
  PLAYBACK_LOG_PAGE_SIZE_OPTIONS,
  stateColor,
  type PlaybackLogFilters
} from "./playbackLogState";

const columns = [
  { title: "开始时间", slotName: "startedAt", width: 174 },
  { title: "设备 / 通道", slotName: "identity", width: 230 },
  { title: "流 / 节点", slotName: "stream", width: 210 },
  { title: "媒体状态", slotName: "mediaState", width: 132 },
  { title: "客户端状态", slotName: "clientState", width: 132 },
  { title: "当前阶段", dataIndex: "currentStage", width: 120 },
  { title: "原因码", slotName: "reason", width: 180 },
  { title: "", slotName: "actions", width: 60, fixed: "right" }
];

const filters = reactive<PlaybackLogFilters>({});
const pagination = reactive({ page: 1, pageSize: PLAYBACK_LOG_PAGE_SIZE, total: 0 });
const rows = ref<PlayLifecycleSummary[]>([]);
const loading = ref(false);
const errorMessage = ref("");
const detailVisible = ref(false);
const detailLoading = ref(false);
const detail = ref<{ lifecycle: PlayLifecycleSummary; events: PlayLifecycleEvent[] } | null>(null);
let requestGeneration = 0;

async function loadRows() {
  const generation = ++requestGeneration;
  loading.value = true;
  errorMessage.value = "";
  try {
    const response = await listPlayLifecycles(createPlaybackLogQuery(filters, pagination.page, pagination.pageSize));
    if (generation !== requestGeneration) return;
    if (response.code !== 0) throw new Error(response.message || "查询失败");
    rows.value = response.data?.list ?? [];
    pagination.total = response.data?.total ?? 0;
  } catch (error) {
    if (generation !== requestGeneration) return;
    errorMessage.value = "播放日志暂时不可用";
    console.error(error);
  } finally {
    if (generation === requestGeneration) loading.value = false;
  }
}

function query() {
  pagination.page = 1;
  void loadRows();
}

function reset() {
  Object.assign(filters, {
    deviceCode: undefined,
    channelCode: undefined,
    streamId: undefined,
    mediaState: undefined,
    clientState: undefined,
    range: undefined
  });
  query();
}

function changePage(page: number) {
  pagination.page = page;
  void loadRows();
}

function changePageSize(pageSize: number) {
  pagination.page = 1;
  pagination.pageSize = pageSize;
  void loadRows();
}

async function openDetail(record: PlayLifecycleSummary) {
  detailVisible.value = true;
  detailLoading.value = true;
  detail.value = null;
  try {
    const response = await getPlayLifecycle(record.lifecycleId);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "查询失败");
    detail.value = response.data;
  } catch (error) {
    errorMessage.value = "播放日志详情暂时不可用";
    detailVisible.value = false;
    console.error(error);
  } finally {
    detailLoading.value = false;
  }
}

function formatTime(value?: string) {
  if (!value) return "-";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

onMounted(loadRows);
</script>

<style scoped>
.playback-log-page {
  height: 100%;
  min-height: 0;
  overflow: hidden;
}
.playback-log-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 0;
  overflow: hidden;
}
.playback-log-search {
  flex: none;
  margin-bottom: 14px;
}
.playback-log-error {
  flex: none;
  margin-bottom: 12px;
}
.playback-log-table-panel {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}
.playback-log-table {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.playback-log-table :deep(.arco-table-container) {
  height: 100%;
  border: 0;
}
.playback-log-table :deep(.arco-table-cell) {
  font-size: 12px;
}
.playback-log-pagination {
  display: flex;
  flex: none;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-top: 1px solid var(--uvp-panel-border);
}
.identity-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.identity-cell strong,
.identity-cell span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.identity-cell span {
  color: var(--color-text-3);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.detail-spin {
  width: 100%;
  min-height: 180px;
}
.detail-summary {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-bottom: 22px;
}
.detail-summary > div {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.detail-summary span {
  font-size: 12px;
  color: var(--color-text-3);
}
.detail-summary strong {
  overflow-wrap: anywhere;
}
.timeline-head {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}
.timeline-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 5px;
  font-size: 12px;
  color: var(--color-text-3);
}
.timeline-reason {
  margin-top: 5px;
  font-size: 12px;
  color: rgb(var(--red-6));
  overflow-wrap: anywhere;
}

@media (width <= 760px) {
  .detail-summary {
    grid-template-columns: 1fr;
  }
  .playback-log-pagination {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
