<template>
  <div class="snow-fill cloud-recordings-page">
    <div class="snow-fill-inner uvp-page-shell-flat cloud-recordings-shell">
      <a-alert v-if="!canView" type="warning" class="cloud-recordings-state">
        无权查看云端录像，请联系管理员分配录像查看权限。
      </a-alert>

      <template v-else>
        <div class="cloud-recordings-toolbar">
          <div class="segmented recording-view-switch" role="group" aria-label="录像视图" data-testid="recording-view-switch">
            <button
              type="button"
              data-testid="files-tab"
              :class="{ active: activeView === 'files' }"
              :aria-pressed="activeView === 'files'"
              @click="activeView = 'files'"
            >
              录像文件 <span class="recording-view-switch__count">{{ pagination.total }}</span>
            </button>
            <button
              type="button"
              data-testid="active-tab"
              :class="{ active: activeView === 'active' }"
              :aria-pressed="activeView === 'active'"
              @click="activeView = 'active'"
            >
              正在录像 <span class="recording-view-switch__count">{{ activeRecordings.length }}</span>
            </button>
          </div>
          <div class="cloud-recordings-header__actions">
            <span
              v-if="reconciliationSummary"
              :class="['reconciliation-summary', `is-${reconciliationSummary.tone}`]"
              data-testid="reconciliation-summary"
            >
              <LoaderCircle
                v-if="reconciliationSummary.tone === 'running'"
                class="reconciliation-summary__icon is-spinning"
                :size="14"
                aria-hidden="true"
              />
              <TriangleAlert
                v-else-if="reconciliationSummary.tone === 'warning'"
                class="reconciliation-summary__icon"
                :size="14"
                aria-hidden="true"
              />
              <CircleCheck
                v-else
                class="reconciliation-summary__icon"
                data-testid="reconciliation-success-icon"
                :size="14"
                aria-hidden="true"
              />
              {{ reconciliationSummary.text }}
            </span>
            <a-button
              v-if="canReconcile"
              type="primary"
              data-testid="recording-reconcile"
              :loading="reconciling"
              @click="reconcile"
            >
              <template #icon><ScanSearch :size="15" /></template>
              对账
            </a-button>
            <a-button
              class="uvp-refresh-btn"
              data-testid="recording-refresh"
              :loading="loading || activeLoading"
              :title="`自动刷新倒计时 ${autoRefreshCountdown} 秒`"
              @click="refreshCurrent"
            >
              <template #icon><RefreshCw :size="15" /></template>
              刷新 <span class="recording-refresh-countdown">{{ autoRefreshCountdown }}s</span>
            </a-button>
          </div>
        </div>

        <template v-if="activeView === 'files'">
            <div v-if="activeView === 'files'" class="recording-files-view">
              <s-layout-search>
                <template #fields>
                  <a-range-picker
                    class="recording-date-range"
                    v-model="form.range"
                    show-time
                    allow-clear
                    format="YYYY-MM-DD HH:mm"
                    value-format="YYYY-MM-DDTHH:mm:ssZ"
                  />
                  <a-select v-model="form.nodeId" placeholder="存储节点" allow-clear style="width: 160px">
                    <a-option v-for="node in options.nodes" :key="node.id" :value="node.id">
                      {{ node.name || `节点 ${node.id}` }}
                    </a-option>
                  </a-select>
                  <a-select v-model="form.availability" placeholder="可用状态" allow-clear style="width: 150px">
                    <a-option v-for="item in availabilityOptions" :key="item.value" :value="item.value">{{ item.label }}</a-option>
                  </a-select>
                  <a-select v-model="form.metadataState" placeholder="信息状态" allow-clear style="width: 130px">
                    <a-option value="complete">信息完整</a-option>
                    <a-option value="partial">待完善</a-option>
                  </a-select>
                  <a-input
                    v-model="form.keyword"
                    data-testid="recording-keyword"
                    placeholder="文件名 / 设备名称或编号 / 通道名称或编号"
                    allow-clear
                    style="width: 320px"
                    @press-enter="queryFiles"
                  />
                </template>
                <template #actions>
                  <a-button type="primary" data-testid="recording-query" @click="queryFiles">
                    <template #icon><Search :size="15" /></template>
                    查询
                  </a-button>
                  <a-button data-testid="recording-reset" @click="resetFilters">
                    <template #icon><RotateCcw :size="15" /></template>
                    重置
                  </a-button>
                </template>
              </s-layout-search>

              <a-alert v-if="errorMessage && !loading" type="error" class="cloud-recordings-state">
                {{ errorMessage }}
              </a-alert>

              <div v-else class="cloud-recordings-table-wrap">
                <a-table
                  class="uvp-data-table"
                  data-testid="recording-table"
                  row-key="id"
                  :data="files"
                  :bordered="false"
                  :loading="loading"
                  :pagination="pagination"
                  :scroll="fileTableScroll"
                  @page-change="handlePageChange"
                  @page-size-change="handlePageSizeChange"
                >
                  <template #columns>
                    <a-table-column title="开始时间" :width="176">
                      <template #cell="{ record }"><span class="mono">{{ formatDateTime(record.startTime) }}</span></template>
                    </a-table-column>
                    <a-table-column title="通道" :width="200">
                      <template #cell="{ record }">
                        <div class="recording-entity-cell"><span>{{ record.channelName || record.channelCode || "--" }}</span><small>{{ record.channelCode || "--" }}</small></div>
                      </template>
                    </a-table-column>
                    <a-table-column title="设备" :width="190">
                      <template #cell="{ record }">
                        <div class="recording-entity-cell"><span>{{ record.deviceName || record.deviceId || "--" }}</span><small>{{ record.deviceId || "--" }}</small></div>
                      </template>
                    </a-table-column>
                    <a-table-column title="文件" :width="220" :ellipsis="true" :tooltip="true">
                      <template #cell="{ record }">{{ record.fileName || "--" }}</template>
                    </a-table-column>
                    <a-table-column title="时长" :width="110">
                      <template #cell="{ record }">{{ formatDuration(record.timeLen) }}</template>
                    </a-table-column>
                    <a-table-column title="大小" :width="110">
                      <template #cell="{ record }">{{ formatFileSize(record.fileSize) }}</template>
                    </a-table-column>
                    <a-table-column title="节点" :width="150">
                      <template #cell="{ record }">{{ record.node.name || `节点 ${record.node.id}` }}</template>
                    </a-table-column>
                    <a-table-column title="信息" :width="106">
                      <template #cell="{ record }">
                        <a-tag :color="record.metadataState === 'complete' ? 'blue' : 'orange'">
                          {{ record.metadataState === "complete" ? "完整" : "待完善" }}
                        </a-tag>
                      </template>
                    </a-table-column>
                    <a-table-column title="状态" :width="118">
                      <template #cell="{ record }">
                        <a-tag :color="availabilityPresentation(record.availability).color">
                          {{ availabilityPresentation(record.availability).label }}
                        </a-tag>
                      </template>
                    </a-table-column>
                    <a-table-column
                      title="操作"
                      data-testid="recording-actions-column"
                      :width="220"
                      align="center"
                      :fixed="isMobile ? '' : 'right'"
                    >
                      <template #cell="{ record }">
                        <div class="uvp-table-actions cloud-recording-actions">
                          <a-link
                            :data-testid="`detail-${record.id}`"
                            class="uvp-table-action uvp-table-action--detail"
                            @click="openDetail(record.id)"
                          >
                            <template #icon><Eye :data-testid="`detail-icon-${record.id}`" :size="13" /></template>
                            <span>详情</span>
                          </a-link>
                          <template v-if="availabilityPresentation(record.availability).canAccess">
                            <a-link
                              :data-testid="`play-${record.id}`"
                              class="uvp-table-action uvp-table-action--preview"
                              @click="play(record)"
                            >
                              <template #icon><Play :data-testid="`play-icon-${record.id}`" :size="13" /></template>
                              <span>播放</span>
                            </a-link>
                            <a-link
                              :data-testid="`download-${record.id}`"
                              class="uvp-table-action uvp-table-action--download"
                              @click="download(record)"
                            >
                              <template #icon><Download :data-testid="`download-icon-${record.id}`" :size="13" /></template>
                              <span>下载</span>
                            </a-link>
                          </template>
                        </div>
                      </template>
                    </a-table-column>
                  </template>
                  <template #empty><a-empty description="当前筛选条件下暂无云端录像" /></template>
                </a-table>
              </div>
            </div>
        </template>

        <template v-else>
            <div v-if="activeView === 'active'" class="active-recordings-view">
              <a-alert v-if="activeError && !activeLoading" type="error" class="cloud-recordings-state">{{ activeError }}</a-alert>
              <div v-else class="cloud-recordings-table-wrap">
                <a-table
                  class="uvp-data-table"
                  row-key="id"
                  :data="activeRecordings"
                  :bordered="false"
                  :loading="activeLoading"
                  :pagination="false"
                  :scroll="activeTableScroll"
                >
                  <template #columns>
                    <a-table-column title="开始时间" :width="176"><template #cell="{ record }">{{ formatDateTime(record.startedAt) }}</template></a-table-column>
                    <a-table-column title="通道" :width="220">
                      <template #cell="{ record }"><div class="recording-entity-cell"><span>{{ record.channelName || record.channelCode }}</span><small>{{ record.channelCode }}</small></div></template>
                    </a-table-column>
                    <a-table-column title="设备编码" :width="210"><template #cell="{ record }"><code>{{ record.deviceId }}</code></template></a-table-column>
                    <a-table-column title="节点" :width="180"><template #cell="{ record }">{{ record.node.name || `节点 ${record.node.id}` }}</template></a-table-column>
                    <a-table-column title="状态" :width="120"><template #cell><a-tag color="green">正在录制</a-tag></template></a-table-column>
                    <a-table-column title="更新时间" :width="176"><template #cell="{ record }">{{ formatDateTime(record.updatedAt) }}</template></a-table-column>
                  </template>
                  <template #empty><a-empty description="当前没有正在录像的通道" /></template>
                </a-table>
              </div>
            </div>
        </template>
      </template>
    </div>
  </div>

  <RecordingDetailDrawer
    v-model:visible="detailVisible"
    :recording-id="detailId"
    @play="playFromDetail"
    @download="download"
  />
  <RecordingPlayerDialog v-model:visible="playerVisible" :recording="playingRecording" />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { CircleCheck, Download, Eye, LoaderCircle, Play, RefreshCw, RotateCcw, ScanSearch, Search, TriangleAlert } from "@lucide/vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import useGlobalProperties from "@/hooks/useGlobalProperties";
import { useUserStoreHook } from "@/store/modules/user";
import RecordingDetailDrawer from "./components/RecordingDetailDrawer.vue";
import RecordingPlayerDialog from "./components/RecordingPlayerDialog.vue";
import {
  listActiveRecordings,
  listReconciliations,
  listRecordingFiles,
  listRecordingOptions,
  triggerReconciliation,
  type ActiveRecording,
  type RecordingAvailability,
  type RecordingFile,
  type RecordingFileQuery,
  type RecordingOptions,
  type RecordingReconciliation
} from "./api";
import { recordingDownloadCoordinator } from "./recordingDownloadService";
import {
  availabilityPresentation,
  createLatestRequestCoordinator,
  defaultRecordingQuery,
  recordingErrorPresentation
} from "./recordingState";

const userStore = useUserStoreHook();
const proxy = useGlobalProperties();
const { isMobile } = useDevicesSize();
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canView = computed(() => hasPermission("gb28181:recording:view"));
const canReconcile = computed(() => hasPermission("gb28181:recording:reconcile"));
const AUTO_REFRESH_INTERVAL_SECONDS = 10;

const initialQuery = defaultRecordingQuery();
const form = reactive({
  range: [] as string[],
  nodeId: "",
  availability: "" as RecordingAvailability | "",
  metadataState: "" as "complete" | "partial" | "",
  keyword: ""
});
const files = ref<RecordingFile[]>([]);
const options = reactive<RecordingOptions>({ channels: [], devices: [], nodes: [] });
const activeRecordings = ref<ActiveRecording[]>([]);
const reconciliations = ref<RecordingReconciliation[]>([]);
const activeView = ref<"files" | "active">("files");
const loading = ref(false);
const activeLoading = ref(false);
const reconciling = ref(false);
const errorMessage = ref("");
const activeError = ref("");
const autoRefreshCountdown = ref(AUTO_REFRESH_INTERVAL_SECONDS);
const detailVisible = ref(false);
const detailId = ref<string | null>(null);
const playerVisible = ref(false);
const playingRecording = ref<RecordingFile | null>(null);
const pagination = reactive({
  current: initialQuery.page,
  pageSize: initialQuery.pageSize,
  total: 0,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
});
const fileTableScroll = computed(() => ({ x: "100%", minWidth: 1558, ...(files.value.length ? { y: "100%" } : {}) }));
const activeTableScroll = computed(() => ({ x: "100%", minWidth: 1082, ...(activeRecordings.value.length ? { y: "100%" } : {}) }));
const reconciliationSummary = computed(() => {
  const running = reconciliations.value.filter(item => item.status === "queued" || item.status === "running").length;
  if (running) return { text: `${running} 个节点正在对账`, tone: "running" as const };
  const failed = reconciliations.value.filter(item => item.status === "failed" || item.status === "partial").length;
  if (failed) return { text: `${failed} 个节点对账需关注`, tone: "warning" as const };
  return reconciliations.value.length ? { text: "节点目录已对账", tone: "success" as const } : null;
});
const availabilityOptions = [
  { value: "available", label: "可播放" },
  { value: "node_offline", label: "节点离线" },
  { value: "node_missing", label: "节点已移除" },
  { value: "file_missing", label: "文件已缺失" },
  { value: "access_unavailable", label: "暂不可访问" }
] as const;

const requestCoordinator = createLatestRequestCoordinator();
let autoRefreshTimer: ReturnType<typeof setInterval> | null = null;

function resetAutoRefreshCountdown() {
  autoRefreshCountdown.value = AUTO_REFRESH_INTERVAL_SECONDS;
}

function stopAutoRefresh() {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer);
  autoRefreshTimer = null;
}

function startAutoRefresh() {
  stopAutoRefresh();
  resetAutoRefreshCountdown();
  autoRefreshTimer = setInterval(() => {
    if (autoRefreshCountdown.value <= 1) {
      refreshCurrent();
    } else autoRefreshCountdown.value -= 1;
  }, 1000);
}

function currentQuery(): RecordingFileQuery {
  return {
    page: pagination.current,
    pageSize: pagination.pageSize,
    ...(form.range.length === 2 ? { start: form.range[0], end: form.range[1] } : {}),
    ...(form.nodeId ? { nodeId: form.nodeId } : {}),
    ...(form.availability ? { availability: form.availability } : {}),
    ...(form.metadataState ? { metadataState: form.metadataState } : {}),
    ...(form.keyword.trim() ? { keyword: form.keyword.trim() } : {})
  };
}

async function loadFiles() {
  const request = requestCoordinator.next();
  loading.value = true;
  errorMessage.value = "";
  try {
    const response = await listRecordingFiles(currentQuery(), request.signal);
    if (!requestCoordinator.isCurrent(request.token)) return;
    files.value = response.data.list ?? [];
    pagination.total = response.data.total ?? 0;
    pagination.current = response.data.page ?? pagination.current;
    pagination.pageSize = response.data.pageSize ?? pagination.pageSize;
  } catch (error) {
    if (!requestCoordinator.isCurrent(request.token)) return;
    files.value = [];
    pagination.total = 0;
    errorMessage.value = recordingErrorPresentation(error);
  } finally {
    if (requestCoordinator.isCurrent(request.token)) loading.value = false;
  }
}

async function loadOptions() {
  try {
    const response = await listRecordingOptions(form.range.length === 2 ? { start: form.range[0], end: form.range[1] } : {});
    Object.assign(options, response.data);
  } catch {
    Object.assign(options, { channels: [], devices: [], nodes: [] });
  }
}

async function loadActive() {
  activeLoading.value = true;
  activeError.value = "";
  try {
    const response = await listActiveRecordings();
    activeRecordings.value = response.data.list ?? [];
  } catch (error) {
    activeError.value = recordingErrorPresentation(error);
  } finally {
    activeLoading.value = false;
  }
}

async function loadReconciliationStates() {
  try {
    const response = await listReconciliations();
    reconciliations.value = response.data.list ?? [];
  } catch {
    reconciliations.value = [];
  }
}

function queryFiles() {
  resetAutoRefreshCountdown();
  pagination.current = 1;
  void loadFiles();
  void loadOptions();
}

function resetFilters() {
  resetAutoRefreshCountdown();
  Object.assign(form, {
    range: [],
    nodeId: "",
    availability: "",
    metadataState: "",
    keyword: ""
  });
  pagination.current = 1;
  void loadFiles();
  void loadOptions();
}

function refreshCurrent() {
  resetAutoRefreshCountdown();
  if (activeView.value === "active") void loadActive();
  else void loadFiles();
  void loadReconciliationStates();
}

function handlePageChange(page: number) {
  resetAutoRefreshCountdown();
  pagination.current = page;
  void loadFiles();
}

function handlePageSizeChange(pageSize: number) {
  resetAutoRefreshCountdown();
  pagination.current = 1;
  pagination.pageSize = pageSize;
  void loadFiles();
}

function openDetail(id: string) {
  detailId.value = id;
  detailVisible.value = true;
}

function play(recording: RecordingFile) {
  playingRecording.value = recording;
  playerVisible.value = true;
}

function playFromDetail(recording: RecordingFile) {
  detailVisible.value = false;
  play(recording);
}

function download(recording: RecordingFile) {
  void recordingDownloadCoordinator.enqueue({
    fileId: recording.id,
    fileName: recording.fileName || `recording-${recording.id}.mp4`
  });
}

async function reconcile() {
  if (!canReconcile.value || reconciling.value) return;
  reconciling.value = true;
  try {
    await triggerReconciliation(form.range.length === 2 ? { start: form.range[0], end: form.range[1] } : {});
    proxy.$message.success("录像目录对账已进入队列");
    await loadReconciliationStates();
  } catch (error) {
    proxy.$message.error(recordingErrorPresentation(error));
  } finally {
    reconciling.value = false;
  }
}

function formatDateTime(value: string | null | undefined) {
  if (!value) return "--";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN", { hour12: false });
}

function formatDuration(seconds: number | null) {
  if (seconds == null || seconds < 0) return "--";
  const total = Math.round(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const remain = total % 60;
  return hours ? `${hours}时${minutes}分` : minutes ? `${minutes}分${remain}秒` : `${remain}秒`;
}

function formatFileSize(bytes: number | null) {
  if (bytes == null || bytes < 0) return "--";
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}

watch(activeView, view => {
  resetAutoRefreshCountdown();
  if (view === "active") void loadActive();
  else void loadFiles();
});

onMounted(() => {
  if (!canView.value) return;
  void loadFiles();
  void loadOptions();
  void loadActive();
  void loadReconciliationStates();
  startAutoRefresh();
});

onBeforeUnmount(() => {
  requestCoordinator.dispose();
  stopAutoRefresh();
});
</script>

<style scoped lang="scss">
.cloud-recordings-page { box-sizing: border-box; width: 100%; height: 100%; max-width: 100vw; min-width: 0; min-height: 0; overflow: hidden; contain: inline-size; color: var(--uvp-text-primary); }
.cloud-recordings-shell { box-sizing: border-box; display: flex; width: 100%; height: 100%; max-width: 100%; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; }
.cloud-recordings-page :deep(.uvp-search-panel .arco-input-wrapper),
.cloud-recordings-page :deep(.uvp-search-panel .arco-select-view),
.cloud-recordings-page :deep(.uvp-search-panel .arco-picker) {
  box-sizing: border-box;
  height: 44px;
  min-height: 44px;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}
.cloud-recordings-page :deep(.uvp-search-panel .arco-input-wrapper:focus-within),
.cloud-recordings-page :deep(.uvp-search-panel .arco-select-view-focus),
.cloud-recordings-page :deep(.uvp-search-panel .arco-picker-focused) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
.cloud-recordings-page :deep(.uvp-search-panel .arco-input::placeholder),
.cloud-recordings-page :deep(.uvp-search-panel .arco-select-view-input::placeholder),
.cloud-recordings-page :deep(.uvp-search-panel .arco-picker input::placeholder) {
  color: var(--uvp-text-tertiary) !important;
  opacity: 1;
}
.cloud-recordings-page :deep(.uvp-search-panel .arco-btn),
.cloud-recordings-page :deep(.cloud-recordings-header__actions .arco-btn) {
  box-sizing: border-box;
  height: 44px;
  min-height: 44px;
  border-radius: 10px;
}
.cloud-recordings-page :deep(.uvp-data-table .arco-table-cell) { font-size: 14px; line-height: 22px; }
.cloud-recordings-toolbar { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 10px; }
.cloud-recordings-header__actions { display: flex; gap: 8px; align-items: center; }
.recording-date-range { width: 360px; max-width: 100%; }
.recording-view-switch button { display: inline-flex; align-items: center; gap: 6px; }
.recording-view-switch__count { color: var(--uvp-text-tertiary); font-variant-numeric: tabular-nums; }
.recording-view-switch button.active .recording-view-switch__count { color: currentColor; }
.recording-refresh-countdown { color: var(--uvp-text-tertiary); font-variant-numeric: tabular-nums; }
.reconciliation-summary { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; white-space: nowrap; }
.reconciliation-summary.is-success { color: rgb(var(--green-6)); }
.reconciliation-summary.is-running { color: var(--uvp-brand); }
.reconciliation-summary.is-warning { color: var(--uvp-warning); }
.reconciliation-summary__icon { flex: 0 0 auto; }
.reconciliation-summary__icon.is-spinning { animation: reconciliation-spin 900ms linear infinite; }
@keyframes reconciliation-spin { to { transform: rotate(360deg); } }
.cloud-recordings-state { margin: 10px 0 12px; }
.recording-files-view,
.active-recordings-view { display: flex; flex: 1; min-height: 0; flex-direction: column; }
.recording-files-view > :deep(.uvp-search-panel) { flex: 0 0 auto; }
.cloud-recordings-table-wrap { flex: 1; max-width: 100%; min-width: 0; min-height: 0; overflow: hidden; contain: inline-size; border-radius: 6px; }
.cloud-recordings-table-wrap :deep(.uvp-data-table) { height: 100%; min-height: 0; }
.recording-entity-cell { display: flex; flex-direction: column; min-width: 0; line-height: 1.35; }
.recording-entity-cell span,
.recording-entity-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.recording-entity-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 11px; }
.cloud-recording-actions { flex-wrap: nowrap; white-space: nowrap; }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action) { flex: 0 0 auto; }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--detail) { color: #0f7490; }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--detail:hover) { color: #0e647c; background: rgb(14 116 144 / 8%); }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--preview) { color: #2563eb; }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--preview:hover) { color: #1d4ed8; background: rgb(37 99 235 / 8%); }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--download) { color: #16845b; }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--download:hover) { color: #10704b; background: rgb(22 132 91 / 8%); }
.mono,
.active-recordings-view code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; }
@media (max-width: 768px) {
  .cloud-recordings-toolbar { align-items: stretch; flex-wrap: wrap; gap: 10px; }
  .recording-view-switch { flex: 1 0 auto; }
  .recording-view-switch button { flex: 1; justify-content: center; }
  .reconciliation-summary { display: none; }
  .cloud-recordings-header__actions { margin-left: auto; flex-wrap: wrap; justify-content: flex-end; }
  .cloud-recordings-header__actions :deep(.arco-btn),
  :deep(.uvp-table-action) { min-height: 44px; }
  :deep(.uvp-table-actions) { gap: 8px; }
  :deep(.uvp-table-action) { display: inline-flex; align-items: center; padding: 0 7px; }
}
@media (max-width: 480px) {
  .recording-date-range { width: 100%; }
  .cloud-recordings-header__actions :deep(.arco-btn) { padding-inline: 8px; }
}
@media (prefers-reduced-motion: reduce) {
  .reconciliation-summary__icon.is-spinning { animation: none; }
}
</style>
