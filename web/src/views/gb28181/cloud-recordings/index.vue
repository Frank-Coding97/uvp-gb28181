<template>
  <div class="snow-page cloud-recordings-page">
    <div class="snow-inner uvp-page-shell-flat">
      <a-alert v-if="!canView" type="warning" class="cloud-recordings-state">
        无权查看云端录像，请联系管理员分配录像查看权限。
      </a-alert>

      <template v-else>
        <header class="cloud-recordings-header">
          <div>
            <h2>云端录像</h2>
            <p>查看 ZLMediaKit 已完成的录像文件及当前正在录像的通道。</p>
          </div>
          <div class="cloud-recordings-header__actions">
            <span v-if="reconciliationSummary" class="reconciliation-summary">{{ reconciliationSummary }}</span>
            <a-button
              v-if="canReconcile"
              data-testid="recording-reconcile"
              :loading="reconciling"
              @click="reconcile"
            >
              <template #icon><ScanSearch :size="15" /></template>
              对账
            </a-button>
            <a-button data-testid="recording-refresh" :loading="loading || activeLoading" @click="refreshCurrent">
              <template #icon><RefreshCw :size="15" /></template>
              刷新
            </a-button>
          </div>
        </header>

        <a-tabs v-model:active-key="activeView" class="cloud-recordings-tabs">
          <a-tab-pane key="files" :title="`录像文件 ${pagination.total}`">
            <div v-if="activeView === 'files'" class="recording-files-view">
              <s-layout-search>
                <template #fields>
                  <a-range-picker
                    v-model="form.range"
                    show-time
                    allow-clear
                    value-format="YYYY-MM-DDTHH:mm:ssZ"
                    style="width: 330px"
                  />
                  <a-select v-model="form.deviceId" placeholder="设备" allow-clear allow-search style="width: 180px">
                    <a-option v-for="device in options.devices" :key="device.id" :value="device.id">
                      {{ device.name || device.id }}
                    </a-option>
                  </a-select>
                  <a-select v-model="form.channelId" placeholder="通道" allow-clear allow-search style="width: 190px">
                    <a-option v-for="channel in options.channels" :key="channel.id" :value="channel.id">
                      {{ channel.name || channel.code }}
                    </a-option>
                  </a-select>
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
                    placeholder="文件 / 设备 / 通道关键词"
                    allow-clear
                    style="width: 220px"
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
                    <a-table-column title="操作" :width="178" align="center" :fixed="isMobile ? '' : 'right'">
                      <template #cell="{ record }">
                        <div class="uvp-table-actions">
                          <a-link class="uvp-table-action uvp-table-action--detail" @click="openDetail(record.id)">详情</a-link>
                          <template v-if="availabilityPresentation(record.availability).canAccess">
                            <a-link
                              :data-testid="`play-${record.id}`"
                              class="uvp-table-action uvp-table-action--preview"
                              @click="play(record)"
                            >播放</a-link>
                            <a-link
                              :data-testid="`download-${record.id}`"
                              class="uvp-table-action uvp-table-action--download"
                              @click="download(record)"
                            >下载</a-link>
                          </template>
                        </div>
                      </template>
                    </a-table-column>
                  </template>
                  <template #empty><a-empty description="当前筛选条件下暂无云端录像" /></template>
                </a-table>
              </div>
            </div>
          </a-tab-pane>

          <a-tab-pane key="active" :title="`正在录像 ${activeRecordings.length}`">
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
          </a-tab-pane>
        </a-tabs>
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
import { RefreshCw, RotateCcw, ScanSearch, Search } from "@lucide/vue";
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
  createPollingController,
  defaultRecordingQuery,
  recordingErrorPresentation
} from "./recordingState";

const userStore = useUserStoreHook();
const proxy = useGlobalProperties();
const { isMobile } = useDevicesSize();
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canView = computed(() => hasPermission("gb28181:recording:view"));
const canReconcile = computed(() => hasPermission("gb28181:recording:reconcile"));

const initialQuery = defaultRecordingQuery();
const form = reactive({
  range: [initialQuery.start!, initialQuery.end!] as string[],
  deviceId: "",
  channelId: "",
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
const detailVisible = ref(false);
const detailId = ref<string | null>(null);
const playerVisible = ref(false);
const playingRecording = ref<RecordingFile | null>(null);
const pagination = reactive({
  current: 1,
  pageSize: 20,
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
  if (running) return `${running} 个节点正在对账`;
  const failed = reconciliations.value.filter(item => item.status === "failed" || item.status === "partial").length;
  return failed ? `${failed} 个节点对账需关注` : reconciliations.value.length ? "节点目录已对账" : "";
});
const availabilityOptions = [
  { value: "available", label: "可播放" },
  { value: "node_offline", label: "节点离线" },
  { value: "node_missing", label: "节点已移除" },
  { value: "file_missing", label: "文件已缺失" },
  { value: "access_unavailable", label: "暂不可访问" }
] as const;

const requestCoordinator = createLatestRequestCoordinator();
const activePoller = createPollingController(
  async () => {
    try {
      return { list: (await listActiveRecordings()).data.list ?? [], error: "" };
    } catch (error) {
      return { list: activeRecordings.value, error: recordingErrorPresentation(error) };
    }
  },
  result => {
    activeRecordings.value = result.list;
    activeError.value = result.error;
    activeLoading.value = false;
  },
  10000
);

function currentQuery(): RecordingFileQuery {
  return {
    page: pagination.current,
    pageSize: pagination.pageSize,
    ...(form.range.length === 2 ? { start: form.range[0], end: form.range[1] } : {}),
    ...(form.deviceId ? { deviceId: form.deviceId } : {}),
    ...(form.channelId ? { channelId: form.channelId } : {}),
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
  pagination.current = 1;
  void loadFiles();
  void loadOptions();
}

function resetFilters() {
  const defaults = defaultRecordingQuery();
  Object.assign(form, {
    range: [defaults.start!, defaults.end!],
    deviceId: "",
    channelId: "",
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
  if (activeView.value === "active") void loadActive();
  else void loadFiles();
  void loadReconciliationStates();
}

function handlePageChange(page: number) {
  pagination.current = page;
  void loadFiles();
}

function handlePageSizeChange(pageSize: number) {
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
  if (view === "active") {
    activeLoading.value = true;
    activePoller.start();
  } else activePoller.stop();
});

onMounted(() => {
  if (!canView.value) return;
  void loadFiles();
  void loadOptions();
  void loadActive();
  void loadReconciliationStates();
});

onBeforeUnmount(() => {
  requestCoordinator.dispose();
  activePoller.stop();
});
</script>

<style scoped lang="scss">
.cloud-recordings-page { box-sizing: border-box; width: 100%; max-width: 100vw; min-width: 0; overflow-x: hidden; contain: inline-size; color: var(--uvp-text-primary); }
.cloud-recordings-page > .snow-inner { box-sizing: border-box; width: 100%; max-width: 100%; min-width: 0; }
.cloud-recordings-header { display: flex; gap: 16px; align-items: flex-start; justify-content: space-between; margin-bottom: 6px; }
.cloud-recordings-header h2 { margin: 0; font-size: 18px; font-weight: 650; }
.cloud-recordings-header p { margin: 4px 0 0; color: var(--uvp-text-secondary); font-size: 13px; }
.cloud-recordings-header__actions { display: flex; gap: 8px; align-items: center; }
.reconciliation-summary { color: var(--uvp-text-tertiary); font-size: 12px; white-space: nowrap; }
.cloud-recordings-tabs { min-width: 0; }
.cloud-recordings-state { margin: 10px 0 12px; }
.cloud-recordings-table-wrap { max-width: 100%; min-width: 0; overflow-x: auto; contain: inline-size; border-radius: 6px; }
.recording-entity-cell { display: flex; flex-direction: column; min-width: 0; line-height: 1.35; }
.recording-entity-cell span,
.recording-entity-cell small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.recording-entity-cell small { margin-top: 2px; color: var(--uvp-text-tertiary); font-size: 11px; }
.mono,
.active-recordings-view code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; }
@media (max-width: 768px) {
  .cloud-recordings-header { align-items: center; }
  .cloud-recordings-header p,
  .reconciliation-summary { display: none; }
  .cloud-recordings-header__actions { flex-wrap: wrap; justify-content: flex-end; }
  .cloud-recordings-header__actions :deep(.arco-btn),
  :deep(.uvp-table-action) { min-height: 44px; }
  :deep(.uvp-table-actions) { gap: 8px; }
  :deep(.uvp-table-action) { display: inline-flex; align-items: center; padding: 0 7px; }
}
@media (max-width: 480px) {
  .cloud-recordings-header h2 { font-size: 16px; }
  .cloud-recordings-header__actions :deep(.arco-btn) { padding-inline: 8px; }
}
</style>
