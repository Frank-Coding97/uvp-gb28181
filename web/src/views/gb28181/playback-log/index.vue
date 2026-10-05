<template>
  <div class="snow-fill playback-log-page">
    <div class="snow-fill-inner uvp-page-shell-flat playback-log-shell">
      <s-layout-search class="playback-log-search">
        <template #fields>
          <a-input v-model="filters.deviceCode" allow-clear placeholder="设备编码" style="width: 190px" @press-enter="query" />
          <a-input v-model="filters.channelCode" allow-clear placeholder="通道编码" style="width: 190px" @press-enter="query" />
          <a-input v-model="filters.streamId" allow-clear placeholder="Stream ID" style="width: 180px" @press-enter="query" />
          <a-select v-model="filters.mediaState" allow-clear placeholder="媒体状态" style="width: 138px">
            <a-option v-for="option in mediaStateOptions" :key="option.value" :value="option.value">
              <span class="playback-state-option"><i :data-tone="playbackStateTone(option.value)" />{{ option.label }}</span>
            </a-option>
          </a-select>
          <a-select v-model="filters.clientState" allow-clear placeholder="客户端状态" style="width: 138px">
            <a-option v-for="option in clientStateOptions" :key="option.value" :value="option.value">
              <span class="playback-state-option"><i :data-tone="playbackStateTone(option.value)" />{{ option.label }}</span>
            </a-option>
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
          class="uvp-data-table playback-log-table"
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
            ><span class="playback-state-tag" :data-tone="playbackStateTone(record.mediaState)">{{
              playbackDictionaryLabel(mediaStateItems, record.mediaState)
            }}</span></template
          >
          <template #clientState="{ record }"
            ><span class="playback-state-tag" :data-tone="playbackStateTone(record.clientState)">{{
              playbackDictionaryLabel(clientStateItems, record.clientState)
            }}</span></template
          >
          <template #currentStage="{ record }"
            ><span>{{ dictionaryLabel(PLAYBACK_STAGE_DICT_CODE, record.currentStage) }}</span></template
          >
          <template #reason="{ record }"
            ><span :title="record.reasonMessage">{{ record.reasonCode || "-" }}</span></template
          >
          <template #actions="{ record }">
            <a-button
              type="text"
              size="small"
              class="uvp-table-action uvp-table-action--detail"
              title="查看详情"
              @click="openDetail(record)"
              ><template #icon><Eye :size="16" /></template
            ></a-button>
          </template>
          <template #empty><a-empty description="暂无播放日志" /></template>
        </a-table>
      </section>
      <footer class="playback-log-pagination uvp-pagination-bar">
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
    </div>

    <a-drawer
      body-class="uvp-system-dialog__body"
      class="uvp-system-drawer"
      v-model:visible="detailVisible"
      :width="560"
      title="播放生命周期"
      unmount-on-close
    >
      <a-spin :loading="detailLoading" class="detail-spin">
        <template v-if="detail">
          <div class="detail-summary">
            <div>
              <span>生命周期 ID</span><strong class="mono">{{ detail.lifecycle.lifecycleId }}</strong>
            </div>
            <div>
              <span>设备 / 通道</span><strong>{{ detail.lifecycle.deviceCode }} / {{ detail.lifecycle.channelCode }}</strong>
            </div>
            <div>
              <span>媒体状态</span
              ><span class="playback-state-tag" :data-tone="playbackStateTone(detail.lifecycle.mediaState)">{{
                dictionaryLabel(PLAYBACK_MEDIA_STATE_DICT_CODE, detail.lifecycle.mediaState)
              }}</span>
            </div>
            <div>
              <span>客户端状态</span
              ><span class="playback-state-tag" :data-tone="playbackStateTone(detail.lifecycle.clientState)">{{
                dictionaryLabel(PLAYBACK_CLIENT_STATE_DICT_CODE, detail.lifecycle.clientState)
              }}</span>
            </div>
            <div>
              <span>播放生命周期</span
              ><span class="playback-state-tag" :data-tone="playbackStateTone(detail.lifecycle.lifecycleState)">{{
                dictionaryLabel(PLAYBACK_LIFECYCLE_STATE_DICT_CODE, detail.lifecycle.lifecycleState)
              }}</span>
            </div>
            <div>
              <span>当前阶段</span><strong>{{ dictionaryLabel(PLAYBACK_STAGE_DICT_CODE, detail.lifecycle.currentStage) }}</strong>
            </div>
            <div v-if="detail.lifecycle.failureStage">
              <span>失败阶段</span><strong>{{ dictionaryLabel(PLAYBACK_STAGE_DICT_CODE, detail.lifecycle.failureStage) }}</strong>
            </div>
          </div>
          <a-timeline class="lifecycle-timeline">
            <a-timeline-item
              v-for="event in detail.events"
              :key="event.sequence"
              :dot-color="playbackStateColor(event.factState)"
            >
              <div class="timeline-head">
                <strong>{{ dictionaryLabel(PLAYBACK_EVENT_DICT_CODE, event.eventName) }}</strong
                ><span class="playback-state-tag playback-state-tag--small" :data-tone="playbackStateTone(event.factState)">{{
                  dictionaryLabel(PLAYBACK_FACT_STATE_DICT_CODE, event.factState)
                }}</span>
              </div>
              <div class="timeline-meta">
                <span>{{ dictionaryLabel(PLAYBACK_STAGE_DICT_CODE, event.stage) }}</span
                ><span>{{ dictionaryLabel(PLAYBACK_EVENT_SOURCE_DICT_CODE, event.source) }}</span
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
import { computed, onMounted, reactive, ref } from "vue";
import { Eye, RefreshCw, RotateCcw, Search } from "@lucide/vue";
import { getPlayLifecycle, listPlayLifecycles, type PlayLifecycleEvent, type PlayLifecycleSummary } from "@/api/gb28181";
import { useSystemStore } from "@/store/modules/system";
import {
  createPlaybackLogQuery,
  playbackDictionaryLabel,
  playbackDictionaryOptions,
  PLAYBACK_CLIENT_STATE_DICT_CODE,
  PLAYBACK_EVENT_DICT_CODE,
  PLAYBACK_EVENT_SOURCE_DICT_CODE,
  PLAYBACK_FACT_STATE_DICT_CODE,
  PLAYBACK_LIFECYCLE_STATE_DICT_CODE,
  PLAYBACK_LOG_PAGE_SIZE,
  PLAYBACK_LOG_PAGE_SIZE_OPTIONS,
  PLAYBACK_MEDIA_STATE_DICT_CODE,
  PLAYBACK_STAGE_DICT_CODE,
  playbackStateColor,
  playbackStateTone,
  type PlaybackLogFilters
} from "./playbackLogState";

const mediaStateItems = computed(() => dictFilter(PLAYBACK_MEDIA_STATE_DICT_CODE));
const clientStateItems = computed(() => dictFilter(PLAYBACK_CLIENT_STATE_DICT_CODE));
const mediaStateOptions = computed(() => playbackDictionaryOptions(mediaStateItems.value));
const clientStateOptions = computed(() => playbackDictionaryOptions(clientStateItems.value));

function dictionaryLabel(code: string, value: string) {
  return playbackDictionaryLabel(dictFilter(code), value);
}

const columns = [
  { title: "开始时间", slotName: "startedAt", width: 174 },
  { title: "设备 / 通道", slotName: "identity", width: 230 },
  { title: "流 / 节点", slotName: "stream", width: 210 },
  { title: "媒体状态", slotName: "mediaState", width: 132 },
  { title: "客户端状态", slotName: "clientState", width: 132 },
  { title: "当前阶段", slotName: "currentStage", width: 120 },
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

onMounted(() => {
  useSystemStore()
    .setDictData()
    .catch((error: Error) => console.warn("播放日志字典加载失败:", error));
  void loadRows();
});
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
  border-radius: 0;
  box-shadow: none;
}
.playback-log-pagination {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  gap: 16px;
  align-items: center;
  justify-content: flex-end;
  margin-top: 12px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
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
  color: var(--zlm-text-4);
}
.playback-state-tag {
  display: inline-flex;
  align-items: center;
  width: max-content;
  padding: 2px 8px;
  font-size: var(--zlm-fs-caption, 12px);
  font-weight: var(--zlm-fw-medium, 500);
  line-height: 1.4;
  white-space: nowrap;
  border: 1px solid transparent;
  border-radius: var(--zlm-radius-full, 999px);
}
.playback-state-tag--small {
  padding: 1px 6px;
  font-size: 11px;
}
.playback-state-tag[data-tone="success"] {
  color: var(--zlm-success-600);
  background: var(--zlm-success-50);
  border-color: var(--zlm-success-500);
}
.playback-state-tag[data-tone="warning"] {
  color: var(--zlm-warn-600);
  background: var(--zlm-warn-50);
  border-color: var(--zlm-warn-500);
}
.playback-state-tag[data-tone="danger"] {
  color: var(--zlm-danger-600);
  background: var(--zlm-danger-50);
  border-color: var(--zlm-danger-500);
}
.playback-state-tag[data-tone="info"] {
  color: var(--zlm-brand-600);
  background: var(--zlm-brand-50);
  border-color: var(--zlm-brand-500);
}
.playback-state-tag[data-tone="neutral"] {
  color: var(--zlm-text-3);
  background: var(--zlm-card);
  border-color: var(--zlm-border);
}
.playback-state-option {
  display: inline-flex;
  gap: 7px;
  align-items: center;
}
.playback-state-option i {
  flex: none;
  width: 6px;
  height: 6px;
  background: var(--zlm-text-4);
  border-radius: 50%;
}
.playback-state-option i[data-tone="success"] {
  background: var(--zlm-success-500);
}
.playback-state-option i[data-tone="warning"] {
  background: var(--zlm-warn-500);
}
.playback-state-option i[data-tone="danger"] {
  background: var(--zlm-danger-500);
}
.playback-state-option i[data-tone="info"] {
  background: var(--zlm-brand-500);
}
.mono {
  font-family: var(--zlm-font-mono);
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
.detail-summary > div > span:first-child {
  font-size: 12px;
  color: var(--zlm-text-3);
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
  color: var(--zlm-text-3);
}
.timeline-reason {
  margin-top: 5px;
  font-size: 12px;
  color: var(--zlm-danger-600);
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
