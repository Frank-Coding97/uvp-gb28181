<template>
  <div class="cloud-recordings-page">
    <div class="cloud-recordings-shell">
      <a-alert v-if="!canView" type="warning" class="cloud-recordings-state">
        无权查看云端录像，请联系管理员分配录像查看权限。
      </a-alert>

      <template v-else>
        <div class="cloud-recordings-toolbar">
          <div v-if="mode !== 'files'" class="segmented recording-view-switch" role="group" aria-label="录像视图" data-testid="recording-view-switch">
            <button
              v-if="mode === 'all'"
              type="button"
              data-testid="files-tab"
              :class="{ active: activeView === 'files' }"
              :aria-pressed="activeView === 'files'"
              @click="selectView('files')"
            >
              <FileVideo2 data-testid="files-tab-icon" :size="14" aria-hidden="true" />
              录像文件 <span class="recording-view-switch__count">{{ pagination.total }}</span>
            </button>
            <button
              type="button"
              data-testid="active-tab"
              :class="{ active: activeView === 'active' }"
              :aria-pressed="activeView === 'active'"
              @click="selectView('active')"
            >
              <CircleDot data-testid="active-tab-icon" :size="14" aria-hidden="true" />
              正在录像 <span class="recording-view-switch__count">{{ visibleActiveRecordings.length }}</span>
            </button>
            <button
              type="button"
              data-testid="runtime-tab"
              :class="{ active: activeView === 'runtime' }"
              :aria-pressed="activeView === 'runtime'"
              @click="selectView('runtime')"
            >
              <SlidersHorizontal :size="14" aria-hidden="true" />
              运行控制
            </button>
          </div>
          <div class="cloud-recordings-header__actions">
            <span
              v-if="activeView === 'files' && reconciliationSummary"
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
              v-if="activeView === 'files' && canReconcile"
              class="recording-reconcile-button"
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

              <div v-if="canDelete && selectedRowKeys.length" class="recording-batch-bar">
                <span>已选 <strong>{{ selectedRowKeys.length }}</strong> 个录像文件</span>
                <div class="recording-batch-bar__actions">
                  <a-button data-testid="recording-batch-delete" status="danger" :loading="batchDeleting" :disabled="batchDeleting" @click="requestBatchDelete">
                    <template #icon><Trash2 :size="14" /></template>
                    批量删除
                  </a-button>
                  <a-button :disabled="batchDeleting" @click="selectedRowKeys = []">取消选择</a-button>
                </div>
              </div>

              <a-alert v-if="errorMessage && !loading" type="error" class="cloud-recordings-state">
                {{ errorMessage }}
              </a-alert>

              <div v-else class="cloud-recordings-table-wrap">
                <a-table
                  class="uvp-data-table"
                  data-testid="recording-table"
                  row-key="id"
                  :data="files"
                  v-model:selected-keys="selectedRowKeys"
                  :row-selection="rowSelection"
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
                      :width="284"
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
                              v-if="canDownload"
                              :data-testid="`download-${record.id}`"
                              class="uvp-table-action uvp-table-action--download"
                              @click="download(record)"
                            >
                              <template #icon><Download :data-testid="`download-icon-${record.id}`" :size="13" /></template>
                              <span>下载</span>
                            </a-link>
                          </template>
                          <a-link
                            v-if="canDelete"
                            :data-testid="`delete-${record.id}`"
                            class="uvp-table-action uvp-table-action--delete"
                            :loading="deletingIds.has(record.id)"
                            :disabled="deletingIds.has(record.id)"
                            @click="requestDelete(record)"
                          >
                            <template #icon><Trash2 :data-testid="`delete-icon-${record.id}`" :size="13" /></template>
                            <span>删除</span>
                          </a-link>
                        </div>
                      </template>
                    </a-table-column>
                  </template>
                  <template #empty><a-empty description="当前筛选条件下暂无云端录像" /></template>
                </a-table>
              </div>
            </div>
        </template>

        <template v-else-if="activeView === 'active'">
            <div v-if="activeView === 'active'" class="active-recordings-view">
              <a-alert v-if="activeError && !activeLoading" type="error" class="cloud-recordings-state">{{ activeError }}</a-alert>
              <div v-else class="cloud-recordings-table-wrap">
                <a-table
                  class="uvp-data-table"
                  row-key="id"
                  :data="visibleActiveRecordings"
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
                    <a-table-column v-if="canStop" title="操作" :width="140" align="center" :fixed="isMobile ? '' : 'right'">
                      <template #cell="{ record }">
                        <div class="uvp-table-actions cloud-recording-actions">
                          <a-link
                            :data-testid="`stop-recording-${record.id}`"
                            class="uvp-table-action uvp-table-action--stop"
                            :loading="stoppingIds.has(record.id)"
                            :disabled="stoppingIds.has(record.id)"
                            @click="requestStopRecording(record)"
                          >
                            <template #icon><CircleStop :size="13" /></template>
                            <span>停止录像</span>
                          </a-link>
                        </div>
                      </template>
                    </a-table-column>
                  </template>
                  <template #empty><a-empty description="当前没有正在录像的通道" /></template>
                </a-table>
              </div>
            </div>
        </template>

        <RecordingRuntimeControl v-else ref="runtimeControl" />
      </template>
    </div>
  </div>

  <RecordingDetailDrawer
    v-model:visible="detailVisible"
    :recording-id="detailId"
    :can-play="canView"
    :can-download="canDownload"
    @play="playFromDetail"
    @download="download"
  />
  <RecordingPlayerDialog v-model:visible="playerVisible" :recording="playingRecording" />
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, reactive, ref, watch } from "vue";
import { CircleCheck, CircleDot, CircleStop, Download, Eye, FileVideo2, LoaderCircle, Play, RefreshCw, RotateCcw, ScanSearch, Search, SlidersHorizontal, Trash2, TriangleAlert } from "@lucide/vue";
import { Modal } from "@arco-design/web-vue";
import { useDevicesSize } from "@/hooks/useDevicesSize";
import useGlobalProperties from "@/hooks/useGlobalProperties";
import { useUserStoreHook } from "@/store/modules/user";
import { boundedPageRows, boundedPageSize } from "@/views/gb28181/zlm/workbench/boundedData";
import RecordingDetailDrawer from "./components/RecordingDetailDrawer.vue";
import RecordingPlayerDialog from "./components/RecordingPlayerDialog.vue";
import RecordingRuntimeControl from "./components/RecordingRuntimeControl.vue";
import {
  batchDeleteRecordingFiles,
  deleteRecordingFile,
  listActiveRecordings,
  listReconciliations,
  listRecordingFiles,
  listRecordingOptions,
  stopActiveRecording,
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

type RecordingWorkspaceMode = "all" | "files" | "tasks";

const props = withDefaults(defineProps<{
  active?: boolean;
  mode?: RecordingWorkspaceMode;
  autoRefresh?: boolean;
  nodeId?: unknown;
  keyword?: unknown;
}>(), {
  active: false,
  mode: "all",
  autoRefresh: true
});

const emit = defineEmits<{
  stats: [value: { filesTotal?: number | null; activeTotal?: number | null }];
}>();

const userStore = useUserStoreHook();
const proxy = useGlobalProperties();
const { isMobile } = useDevicesSize();
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canView = computed(() => hasPermission("gb28181:recording:view"));
const canReconcile = computed(() => hasPermission("gb28181:recording:reconcile"));
const canDownload = computed(() => hasPermission("gb28181:recording:download"));
const canDelete = computed(() => hasPermission("gb28181:recording:delete"));
const canStop = computed(() => hasPermission("gb28181:recording:stop"));
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
const activeView = ref<"files" | "active" | "runtime">(props.mode === "tasks" ? "active" : "files");
const runtimeControl = ref<{ refresh: () => void } | null>(null);
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
const selectedRowKeys = ref<string[]>([]);
const deletingIds = ref(new Set<string>());
const batchDeleting = ref(false);
const stoppingIds = ref(new Set<string>());
const rowSelection = computed(() => canDelete.value ? { type: "checkbox" as const, showCheckedAll: true } : undefined);
const contextualNodeId = computed(() => {
  const scalar = Array.isArray(props.nodeId) ? props.nodeId[0] : props.nodeId;
  const value = typeof scalar === "number" || typeof scalar === "string" ? String(scalar).trim() : "";
  return /^\d+$/.test(value) && Number(value) > 0 ? value : "";
});
const contextualKeyword = computed(() => {
  const scalar = Array.isArray(props.keyword) ? props.keyword[0] : props.keyword;
  if (typeof scalar !== "string" && typeof scalar !== "number") return "";
  const value = String(scalar).trim().slice(0, 128);
  return /[:][/][/]|[\u0000-\u001f\u007f]/.test(value) ? "" : value;
});
const visibleActiveRecordings = computed(() => contextualNodeId.value
  ? activeRecordings.value.filter(item => String(item.node.id) === contextualNodeId.value)
  : activeRecordings.value);
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
const activeTableScroll = computed(() => ({ x: "100%", minWidth: canStop.value ? 1222 : 1082, ...(activeRecordings.value.length ? { y: "100%" } : {}) }));
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
let panelGeneration = 0;

function resetAutoRefreshCountdown() {
  autoRefreshCountdown.value = AUTO_REFRESH_INTERVAL_SECONDS;
}

function stopAutoRefresh() {
  if (autoRefreshTimer) clearInterval(autoRefreshTimer);
  autoRefreshTimer = null;
}

function startAutoRefresh() {
  stopAutoRefresh();
  if (!props.active || !props.autoRefresh) return;
  resetAutoRefreshCountdown();
  autoRefreshTimer = setInterval(() => {
    if (!props.active) {
      stopAutoRefresh();
      return;
    }
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
  if (!props.active || !canView.value) return;
  const request = requestCoordinator.next();
  loading.value = true;
  errorMessage.value = "";
  try {
    const response = await listRecordingFiles(currentQuery(), request.signal);
    if (!requestCoordinator.isCurrent(request.token)) return;
    const nextPageSize = boundedPageSize(response.data.pageSize, pagination.pageSize);
    files.value = boundedPageRows(response.data.list, nextPageSize);
    pagination.total = response.data.total ?? 0;
    pagination.current = response.data.page ?? pagination.current;
    pagination.pageSize = nextPageSize;
    emit("stats", { filesTotal: pagination.total });
  } catch (error) {
    if (!requestCoordinator.isCurrent(request.token)) return;
    errorMessage.value = recordingErrorPresentation(error);
  } finally {
    if (requestCoordinator.isCurrent(request.token)) loading.value = false;
  }
}

async function loadOptions() {
  if (!props.active || !canView.value) return;
  const requestGeneration = panelGeneration;
  try {
    const response = await listRecordingOptions(form.range.length === 2 ? { start: form.range[0], end: form.range[1] } : {});
    if (requestGeneration !== panelGeneration || !props.active) return;
    Object.assign(options, response.data);
  } catch {
    if (requestGeneration === panelGeneration && !options.nodes.length) Object.assign(options, { channels: [], devices: [], nodes: [] });
  }
}

async function loadActive() {
  if (!props.active || !canView.value) return;
  const requestGeneration = panelGeneration;
  activeLoading.value = true;
  activeError.value = "";
  try {
    const response = await listActiveRecordings();
    if (requestGeneration !== panelGeneration || !props.active) return;
    activeRecordings.value = response.data.list ?? [];
    emit("stats", { activeTotal: visibleActiveRecordings.value.length });
  } catch (error) {
    if (requestGeneration === panelGeneration) activeError.value = recordingErrorPresentation(error);
  } finally {
    if (requestGeneration === panelGeneration) activeLoading.value = false;
  }
}

async function loadReconciliationStates() {
  if (!props.active || !canReconcile.value) return;
  const requestGeneration = panelGeneration;
  try {
    const response = await listReconciliations();
    if (requestGeneration !== panelGeneration || !props.active) return;
    reconciliations.value = response.data.list ?? [];
  } catch {
    if (requestGeneration === panelGeneration && !reconciliations.value.length) reconciliations.value = [];
  }
}

function queryFiles() {
  resetAutoRefreshCountdown();
  pagination.current = 1;
  selectedRowKeys.value = [];
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
  selectedRowKeys.value = [];
  void loadFiles();
  void loadOptions();
}

function refreshCurrent() {
  if (!props.active || !canView.value) return;
  resetAutoRefreshCountdown();
  if (activeView.value === "active") void loadActive();
  else if (activeView.value === "runtime") runtimeControl.value?.refresh();
  else void loadFiles();
  if (activeView.value === "files") void loadReconciliationStates();
}

function selectView(view: "files" | "active" | "runtime") {
  if (props.mode === "files" && view !== "files") return;
  if (props.mode === "tasks" && view === "files") return;
  activeView.value = view;
}

function handlePageChange(page: number) {
  resetAutoRefreshCountdown();
  pagination.current = page;
  selectedRowKeys.value = [];
  void loadFiles();
}

function handlePageSizeChange(pageSize: number) {
  resetAutoRefreshCountdown();
  pagination.current = 1;
  pagination.pageSize = pageSize;
  selectedRowKeys.value = [];
  void loadFiles();
}

function openDetail(id: string) {
  detailId.value = id;
  detailVisible.value = true;
}

function play(recording: RecordingFile) {
  if (!canView.value || !availabilityPresentation(recording.availability).canAccess) return;
  playingRecording.value = recording;
  playerVisible.value = true;
}

function playFromDetail(recording: RecordingFile) {
  detailVisible.value = false;
  play(recording);
}

function download(recording: RecordingFile) {
  if (!canDownload.value || !availabilityPresentation(recording.availability).canAccess) return;
  void recordingDownloadCoordinator.enqueue({
    fileId: recording.id,
    fileName: recording.fileName || `recording-${recording.id}.mp4`
  });
}

function requestStopRecording(recording: ActiveRecording) {
  if (!canStop.value || stoppingIds.value.has(recording.id)) return;
  Modal.warning({
    title: "停止录像",
    content: `将停止“${recording.channelName || recording.channelCode}”在 ZLMediaKit 上的录像，并关闭该通道的云端录像开关。`,
    okText: "停止录像", cancelText: "取消", hideCancel: false, escToClose: true,
    okButtonProps: { status: "danger" }, onOk: () => performStopRecording(recording.id)
  });
}

async function performStopRecording(id: string) {
  if (!canStop.value || stoppingIds.value.has(id)) return;
  stoppingIds.value = new Set([...stoppingIds.value, id]);
  try {
    await stopActiveRecording(id);
    proxy.$message.success("录像已停止");
    await loadActive();
  } catch (error) {
    proxy.$message.error(recordingErrorPresentation(error));
  } finally {
    const next = new Set(stoppingIds.value);
    next.delete(id);
    stoppingIds.value = next;
  }
}

function requestDelete(recording: RecordingFile) {
  if (!canDelete.value || deletingIds.value.has(recording.id)) return;
  Modal.warning({
    title: "删除录像文件",
    content: `将从 ZLMediaKit 节点物理删除“${recording.fileName || recording.id}”，删除后不可恢复。`,
    okText: "删除", cancelText: "取消", hideCancel: false, escToClose: true,
    okButtonProps: { status: "danger" }, onOk: () => performDelete(recording.id)
  });
}

async function performDelete(id: string) {
  if (!canDelete.value || deletingIds.value.has(id)) return;
  deletingIds.value = new Set([...deletingIds.value, id]);
  try {
    await deleteRecordingFile(id);
    selectedRowKeys.value = selectedRowKeys.value.filter(item => item !== id);
    if (files.value.length === 1 && pagination.current > 1) pagination.current -= 1;
    proxy.$message.success("录像文件已删除");
    await loadFiles();
  } catch (error) {
    proxy.$message.error(recordingErrorPresentation(error));
  } finally {
    const next = new Set(deletingIds.value);
    next.delete(id);
    deletingIds.value = next;
  }
}

function requestBatchDelete() {
  if (!canDelete.value || !selectedRowKeys.value.length || batchDeleting.value) return;
  Modal.warning({
    title: "批量删除录像文件",
    content: `将从 ZLMediaKit 节点物理删除选中的 ${selectedRowKeys.value.length} 个录像文件，删除后不可恢复。`,
    okText: "删除", cancelText: "取消", hideCancel: false, escToClose: true,
    okButtonProps: { status: "danger" }, onOk: () => performBatchDelete()
  });
}

async function performBatchDelete() {
  if (!canDelete.value || batchDeleting.value || !selectedRowKeys.value.length) return;
  batchDeleting.value = true;
  const ids = [...selectedRowKeys.value];
  try {
    const response = await batchDeleteRecordingFiles(ids);
    const { deletedCount = 0, failedCount = 0, results = [] } = response.data;
    const failedIDs = new Set(results.filter(item => !item.deleted).map(item => item.id));
    selectedRowKeys.value = ids.filter(id => failedIDs.has(id));
    if (deletedCount >= files.value.length && pagination.current > 1) pagination.current -= 1;
    if (failedCount) proxy.$message.error(`已删除 ${deletedCount} 个，${failedCount} 个删除失败`);
    else proxy.$message.success(`已删除 ${deletedCount} 个录像文件`);
    await loadFiles();
  } catch (error) {
    proxy.$message.error(recordingErrorPresentation(error));
  } finally {
    batchDeleting.value = false;
  }
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
  if (!props.active || !canView.value) return;
  if (view === "active") void loadActive();
  else if (view === "runtime") void nextTick(() => runtimeControl.value?.refresh());
  else {
    void loadFiles();
    void loadOptions();
    void loadReconciliationStates();
  }
});

watch(
  [() => props.active, () => props.mode, () => props.autoRefresh, contextualNodeId, contextualKeyword, canView],
  ([active, mode, , nodeId, keyword, allowed]) => {
    panelGeneration += 1;
    requestCoordinator.dispose();
    stopAutoRefresh();
    if (!active || !allowed) {
      detailVisible.value = false;
      playerVisible.value = false;
      if (!allowed) emit("stats", { filesTotal: null, activeTotal: null });
      return;
    }
    if (mode !== "tasks") {
      form.nodeId = nodeId;
      if (keyword) form.keyword = keyword;
      pagination.current = 1;
    }
    if (mode === "files") activeView.value = "files";
    else if (mode === "tasks" && activeView.value === "files") activeView.value = "active";
    if (activeView.value === "files") {
      void loadFiles();
      void loadOptions();
      void loadReconciliationStates();
    } else if (activeView.value === "active") void loadActive();
    else void nextTick(() => runtimeControl.value?.refresh());
    startAutoRefresh();
  },
  { immediate: true }
);

watch(visibleActiveRecordings, value => {
  if (props.active && canView.value && props.mode === "tasks") emit("stats", { activeTotal: value.length });
});

onBeforeUnmount(() => {
  panelGeneration += 1;
  requestCoordinator.dispose();
  stopAutoRefresh();
});

defineExpose({ refresh: refreshCurrent });
</script>

<style scoped lang="scss">
.cloud-recordings-page { box-sizing: border-box; width: 100%; height: 100%; max-width: 100vw; min-width: 0; min-height: 0; overflow: hidden; contain: inline-size; color: var(--uvp-text-primary); }
.cloud-recordings-shell { box-sizing: border-box; display: flex; width: 100%; height: 100%; max-width: 100%; min-width: 0; min-height: 0; flex-direction: column; overflow: hidden; }
.cloud-recordings-page :deep(.uvp-search-panel .arco-input-wrapper),
.cloud-recordings-page :deep(.uvp-search-panel .arco-select-view),
.cloud-recordings-page :deep(.uvp-search-panel .arco-picker) {
  box-sizing: border-box;
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
  border-radius: 10px;
}
.cloud-recordings-page :deep(.uvp-data-table .arco-table-cell) { font-size: 14px; line-height: 22px; }
.cloud-recordings-toolbar { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 10px; }
.cloud-recordings-header__actions { display: flex; gap: 8px; align-items: center; }
.recording-reconcile-button {
  color: color-mix(in srgb, #7c3aed 82%, var(--uvp-text-primary));
  background: color-mix(in srgb, #7c3aed 9%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, #7c3aed 26%, var(--uvp-panel-border));
  box-shadow: 0 1px 2px rgb(124 58 237 / 8%);
}
.recording-reconcile-button:hover {
  color: color-mix(in srgb, #6d28d9 88%, var(--uvp-text-primary));
  background: color-mix(in srgb, #7c3aed 14%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, #7c3aed 38%, var(--uvp-panel-border));
}
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
.recording-batch-bar { display: flex; flex: 0 0 auto; align-items: center; justify-content: space-between; gap: 16px; margin: 10px 0 12px; padding: 10px 14px; color: var(--uvp-text-secondary); background: color-mix(in srgb, var(--uvp-danger) 6%, var(--uvp-panel-bg)); border: 1px solid color-mix(in srgb, var(--uvp-danger) 20%, var(--uvp-panel-border)); border-radius: 8px; }
.recording-batch-bar__actions { display: flex; align-items: center; gap: 8px; }
.recording-files-view,
.active-recordings-view { display: flex; flex: 1; min-height: 0; flex-direction: column; }
.recording-runtime-control { display: flex; flex: 1; min-height: 0; flex-direction: column; }
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
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--delete) { color: var(--uvp-danger); }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--delete:hover) { color: var(--uvp-danger); background: color-mix(in srgb, var(--uvp-danger) 8%, transparent); }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--stop) { color: #d97706; }
.cloud-recordings-page :deep(.cloud-recording-actions .uvp-table-action--stop:hover) { color: #b45309; background: rgb(217 119 6 / 8%); }
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
