<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, toRef, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Eye, PowerOff, Radio, ShieldAlert, Users } from "lucide-vue-next";

import {
  getZLMStreamDetail,
  forceCloseZLMStream,
  listZLMStreams,
  listZLMStreamViewers,
  preflightCloseZLMStream,
  type ZLMOwnershipTarget,
  type ZLMStream,
  type ZLMStreamDetail,
  type ZLMStreamPage,
  type ZLMStreamViewerPage
} from "@/api/gb28181-zlm-runtime";
import type { MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";

import { formatZLMByteRate, formatZLMBytes, formatZLMDuration, zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import { streamCloseDecision, streamIdentityKey as mediaIdentityKey } from "../../streamManagementState";
import { buildStreamRequestQuery, createStreamFilters, type MonitoringStreamFilters } from "./monitoringState";
import { boundedPageRows } from "../boundedData";

const props = withDefaults(
  defineProps<{
    active: boolean;
    autoRefresh?: boolean;
    scope: MediaScope;
    nodeId: number | null;
    initialQuery?: Record<string, unknown>;
  }>(),
  {
    active: true,
    autoRefresh: true,
    initialQuery: undefined
  }
);

const userStore = useUserStoreHook();
const filters = reactive<MonitoringStreamFilters>(createStreamFilters(props.initialQuery));
const page = ref(1);
const pageSize = ref(10);
const pageData = ref<ZLMStreamPage | null>(null);
const loading = ref(false);
const loadError = ref<unknown>(null);
const detailVisible = ref(false);
const detailLoading = ref(false);
const detailError = ref<unknown>(null);
const detail = ref<ZLMStreamDetail | null>(null);
const detailViewers = ref<ZLMStreamViewerPage | null>(null);
const detailViewerPage = ref(1);
const detailTarget = ref<ZLMOwnershipTarget | null>(null);
const closeVisible = ref(false);
const closePreflighting = ref(false);
const AUTO_REFRESH_SECONDS = 10;
const refreshCountdown = ref(0);
let refreshCountdownTimer: ReturnType<typeof setInterval> | null = null;
let detailGeneration = 0;
let detailController: AbortController | null = null;
let disposed = false;
let closeModal: ReturnType<typeof Modal.confirm> | null = null;

const requestNodeId = computed(() => (props.scope === "all" ? 1 : props.nodeId));
const scopeLabel = computed(() => (props.scope === "all" ? "全部节点" : `节点 #${props.nodeId ?? "—"}`));
type StreamTableRow = ZLMStream & { rowKey: string };
const rows = computed<StreamTableRow[]>(() =>
  (pageData.value?.list ?? []).map(stream => ({
    ...stream,
    rowKey: streamIdentityKey(stream)
  }))
);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const detailErrorPresentation = computed(() => zlmErrorPresentation(detailError.value));
const hasPermission = (permission: string) =>
  userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canForceClose = computed(() => hasPermission("gb28181:zlm:stream:force-close"));
const readOnly = computed(() => !canForceClose.value);
const pollingPaused = computed(() => !props.autoRefresh || detailVisible.value || closeVisible.value);
const refreshButtonLabel = computed(() =>
  props.active && props.autoRefresh && refreshCountdown.value > 0 ? `刷新（${refreshCountdown.value}s）` : "刷新"
);

function streamIdentityKey(stream: Pick<ZLMStream, "nodeId" | "media">) {
  return mediaIdentityKey(stream.nodeId, stream.media);
}

const { refresh } = useZLMRuntimePolling<ZLMStreamPage>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  paused: pollingPaused,
  intervalMs: 10_000,
  async load(nodeId, signal) {
    loading.value = true;
    const query = buildStreamRequestQuery(
      filters,
      page.value,
      pageSize.value,
      props.scope === "all" ? undefined : (props.nodeId ?? nodeId)
    );
    const response = await listZLMStreams(query, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "媒体流列表加载失败");
    return response.data;
  },
  publish(value) {
    pageData.value = { ...value, list: boundedPageRows(value.list, pageSize.value) };
    loadError.value = null;
    loading.value = false;
    scheduleRefreshCountdown();
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
    scheduleRefreshCountdown();
  }
});

function cancelDetail() {
  detailGeneration += 1;
  detailController?.abort();
  detailController = null;
}

function closeInteractions() {
  cancelDetail();
  detailVisible.value = false;
  closeVisible.value = false;
  closeModal?.close();
  closeModal = null;
  detail.value = null;
  detailViewers.value = null;
  detailTarget.value = null;
}

watch([() => props.scope, () => props.nodeId], () => {
  closeInteractions();
  pageData.value = null;
  loadError.value = null;
  loading.value = props.scope === "all" || props.nodeId !== null;
  if (props.active) requestRefresh();
});

function applyFilters() {
  page.value = 1;
  requestRefresh();
}

function clearFilters() {
  filters.schema = "";
  filters.vhost = "";
  filters.app = "";
  filters.stream = "";
  filters.recording = "";
  applyFilters();
}

function changePage(nextPage: number) {
  page.value = nextPage;
  requestRefresh();
}

function changePageSize(nextPageSize: number) {
  pageSize.value = nextPageSize;
  page.value = 1;
  requestRefresh();
}

function ownershipText(stream: Pick<ZLMStream, "ownership">) {
  switch (stream.ownership.status) {
    case "managed":
      return "管理资源";
    case "owned":
      return "业务持有";
    case "conflicted":
      return "持有冲突";
    case "unknown":
      return "归属未知";
    default:
      return "未登记";
  }
}

async function loadDetailViewers(viewerPage = detailViewerPage.value) {
  const target = detailTarget.value;
  if (!target) return;
  const currentGeneration = detailGeneration;
  const response = await listZLMStreamViewers(
    target.nodeId,
    target.media,
    { page: viewerPage, pageSize: 10 },
    detailController?.signal
  );
  if (response.code !== 0 || !response.data) throw new Error(response.message || "观看者加载失败");
  if (currentGeneration === detailGeneration && detailVisible.value) detailViewers.value = response.data;
}

async function openDetail(stream: ZLMStream) {
  cancelDetail();
  const currentGeneration = detailGeneration;
  detailController = new AbortController();
  detailTarget.value = { nodeId: stream.nodeId, media: { ...stream.media } };
  detailViewerPage.value = 1;
  detailVisible.value = true;
  detailLoading.value = true;
  detailError.value = null;
  detail.value = null;
  detailViewers.value = null;
  try {
    const [detailResponse] = await Promise.all([
      getZLMStreamDetail(stream.nodeId, stream.media, detailController.signal),
      loadDetailViewers(1)
    ]);
    if (detailResponse.code !== 0 || !detailResponse.data) throw new Error(detailResponse.message || "流详情加载失败");
    if (currentGeneration === detailGeneration && detailVisible.value) detail.value = detailResponse.data;
  } catch (error) {
    if (currentGeneration === detailGeneration) detailError.value = error;
  } finally {
    if (currentGeneration === detailGeneration) detailLoading.value = false;
  }
}

async function changeViewerPage(nextPage: number) {
  detailViewerPage.value = nextPage;
  detailLoading.value = true;
  try {
    await loadDetailViewers(nextPage);
  } catch (error) {
    detailError.value = error;
  } finally {
    detailLoading.value = false;
  }
}

async function openClose(stream: ZLMStream) {
  if (!canForceClose.value || closePreflighting.value || closeVisible.value) return;
  closePreflighting.value = true;
  const scope = props.scope;
  const target = { nodeId: stream.nodeId, media: { ...stream.media } };
  try {
    const response = await preflightCloseZLMStream(target.nodeId, target.media);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "流关闭预检失败");
    if (disposed || !props.active || props.scope !== scope || !canForceClose.value) return;
    const preflight = response.data;
    const decision = streamCloseDecision(preflight, true, canForceClose.value);
    if (!decision.allowed) {
      Message.warning(decision.reason);
      return;
    }
    closeVisible.value = true;
    closeModal = Modal.confirm({
      title: "确认强关媒体流？",
      content: `节点 #${target.nodeId} · ${target.media.schema}://${target.media.vhost}/${target.media.app}/${target.media.stream}。此操作会中断当前观看连接。`,
      okText: "确认强关",
      cancelText: "取消",
      okButtonProps: { status: "danger" },
      onCancel: () => {
        closeVisible.value = false;
      },
      onClose: () => {
        closeVisible.value = false;
        closeModal = null;
      },
      onBeforeOk: async () => {
        if (disposed || !props.active || props.scope !== scope || !canForceClose.value) {
          closeVisible.value = false;
          return true;
        }
        try {
          const result = await forceCloseZLMStream(target.nodeId, target.media, preflight.fingerprint, "用户确认强关");
          if (result.code !== 0 || !result.data) throw new Error(result.message || "强关失败");
          closeDone({
            closed: result.data.closed ? 1 : 0,
            alreadyAbsent: result.data.alreadyAbsent ? 1 : 0,
            partial: false,
            uncertain: result.data.uncertain
          });
        } catch (error) {
          Message.error(zlmErrorPresentation(error).label);
        }
        closeVisible.value = false;
        return true;
      }
    });
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    closePreflighting.value = false;
  }
}

function closeDone(result: { closed: number; alreadyAbsent: number; partial: boolean; uncertain: boolean }) {
  if (result.uncertain || result.partial)
    Message.warning(`关闭已返回：成功 ${result.closed}，已不存在 ${result.alreadyAbsent}；部分结果需重新回读。`);
  else Message.success(`关闭完成：成功 ${result.closed}，已不存在 ${result.alreadyAbsent}。`);
  requestRefresh();
}

function clearRefreshCountdown() {
  if (refreshCountdownTimer) clearInterval(refreshCountdownTimer);
  refreshCountdownTimer = null;
  refreshCountdown.value = 0;
}

function tickRefreshCountdown() {
  if (refreshCountdown.value > 1) {
    refreshCountdown.value -= 1;
    return;
  }
  refreshCountdown.value = 0;
  clearRefreshCountdown();
}

function scheduleRefreshCountdown() {
  clearRefreshCountdown();
  if (!props.active || !props.autoRefresh || pollingPaused.value) return;
  refreshCountdown.value = AUTO_REFRESH_SECONDS;
  refreshCountdownTimer = setInterval(tickRefreshCountdown, 1_000);
}

function handleManualRefresh() {
  requestRefresh();
}

function requestRefresh() {
  clearRefreshCountdown();
  refresh();
}

watch([() => props.active, () => props.autoRefresh, pollingPaused], () => {
  if (!props.active || !props.autoRefresh || pollingPaused.value) clearRefreshCountdown();
});

onBeforeUnmount(() => {
  disposed = true;
  closeModal?.close();
  closeModal = null;
  cancelDetail();
  clearRefreshCountdown();
});

defineExpose({ refresh: requestRefresh });
</script>

<template>
  <div class="monitoring-panel stream-panel">
    <div v-if="readOnly" class="monitoring-banner" role="status">当前账号仅可查看流运行态；强关按钮按独立权限隐藏。</div>
    <div v-if="pageData?.partial" class="monitoring-banner monitoring-banner--warning" role="status">
      部分节点采集失败，仍展示已返回的 {{ pageData.list.length }} 条数据，不清空整页。
    </div>
    <div v-else-if="loadError && pageData" class="monitoring-banner monitoring-banner--warning" role="status">
      本次刷新失败：{{ errorPresentation.label }}；保留上一次分页与筛选结果。
    </div>

    <s-layout-search class="stream-search">
      <template #fields>
        <slot name="scope" />
        <a-input v-model="filters.schema" allow-clear placeholder="Schema" class="filter-short" @press-enter="applyFilters" />
        <a-input v-model="filters.vhost" allow-clear placeholder="VHost" class="filter-vhost" @press-enter="applyFilters" />
        <a-input v-model="filters.app" allow-clear placeholder="App" class="filter-app" @press-enter="applyFilters" />
        <a-input-search
          v-model="filters.stream"
          allow-clear
          placeholder="Stream ID"
          class="filter-stream"
          @search="applyFilters"
        />
        <a-select
          v-model="filters.recording"
          allow-clear
          placeholder="录制状态"
          class="filter-recording"
          style="flex: 0 0 132px; width: 132px; min-width: 132px; max-width: 132px"
          ><a-option value="mp4">MP4 录制</a-option><a-option value="hls">HLS 录制</a-option></a-select
        >
      </template>
      <template #actions
        ><a-button type="primary" @click="applyFilters">查询</a-button><a-button @click="clearFilters">重置</a-button
        ><a-button
          class="uvp-page-action-btn uvp-refresh-btn stream-refresh-btn"
          aria-label="刷新媒体流"
          @click="handleManualRefresh"
          ><template #icon><icon-refresh /></template>{{ refreshButtonLabel }}</a-button
        ></template
      >
    </s-layout-search>

    <div v-if="loading && !pageData" class="monitoring-state" role="status"><a-spin />正在读取{{ scopeLabel }}媒体流…</div>
    <div v-else-if="loadError && !pageData" class="monitoring-state monitoring-state--error" role="alert">
      <ShieldAlert :size="34" /><strong>{{ errorPresentation.label }}</strong
      ><a-button v-if="errorPresentation.retryable" @click="requestRefresh">重新加载</a-button>
    </div>
    <template v-else>
      <section class="stream-table-panel">
        <a-table :data="rows" row-key="rowKey" :pagination="false" class="uvp-data-table">
          <template #columns>
            <a-table-column title="媒体身份" :width="310"
              ><template #cell="{ record }"
                ><button
                  type="button"
                  class="identity-link"
                  :aria-label="`查看流 ${record.media.app}/${record.media.stream} 详情`"
                  @click="openDetail(record)"
                >
                  <Radio :size="14" /><span
                    ><strong>{{ record.media.app }}/{{ record.media.stream }}</strong
                    ><small>{{ record.media.schema }} · {{ record.media.vhost }}</small></span
                  >
                </button></template
              ></a-table-column
            >
            <a-table-column title="来源" :width="130"
              ><template #cell="{ record }"
                ><span>{{ record.nodeName || `节点 #${record.nodeId}` }}</span
                ><small class="subline">{{ record.originTypeName || `类型 ${record.originType}` }}</small></template
              ></a-table-column
            >
            <a-table-column title="观看 / 累计" :width="120"
              ><template #cell="{ record }"
                ><span class="numeric"
                  ><Users :size="13" /> {{ record.readerCount }} / {{ record.totalReaderCount }}</span
                ></template
              ></a-table-column
            >
            <a-table-column title="吞吐" :width="120"
              ><template #cell="{ record }"
                ><span class="numeric">{{ formatZLMByteRate(record.bytesSpeed) }}</span></template
              ></a-table-column
            >
            <a-table-column title="在线时长" :width="130"
              ><template #cell="{ record }">{{ formatZLMDuration(record.aliveSecond) }}</template></a-table-column
            >
            <a-table-column title="录制" :width="105"
              ><template #cell="{ record }"
                ><span v-if="record.recordingMp4 || record.recordingHls" class="recording">{{
                  [record.recordingMp4 && "MP4", record.recordingHls && "HLS"].filter(Boolean).join(" + ")
                }}</span
                ><span v-else class="muted">未录制</span></template
              ></a-table-column
            >
            <a-table-column title="操作" :width="150" align="center" fixed="right"
              ><template #cell="{ record }"
                ><div class="uvp-table-actions stream-row-actions">
                  <a-link class="uvp-table-action uvp-table-action--detail" @click="openDetail(record)"
                    ><template #icon><Eye :size="13" /></template>详情</a-link
                  ><a-link v-if="canForceClose" class="uvp-table-action uvp-table-action--delete" @click="openClose(record)"
                    ><template #icon><PowerOff :size="13" /></template>强关</a-link
                  >
                </div></template
              ></a-table-column
            >
          </template>
          <template #empty
            ><div class="stream-empty" role="status">
              <Radio :size="38" /><strong>没有符合当前筛选的媒体流</strong
              ><span>筛选条件与分页由后端执行，不会加载全量流到浏览器。</span>
            </div></template
          >
        </a-table>
      </section>
      <div class="stream-pagination uvp-pagination-bar">
        <span>共 {{ pageData?.total ?? 0 }} 路<span v-if="pageData?.truncated">（结果已由后端截断）</span></span
        ><a-pagination
          :current="page"
          :page-size="pageSize"
          :total="pageData?.total ?? 0"
          show-page-size
          :page-size-options="[10, 20, 50, 100]"
          @change="changePage"
          @page-size-change="changePageSize"
        />
      </div>
    </template>

    <a-drawer
      v-model:visible="detailVisible"
      class="uvp-system-drawer"
      body-class="uvp-system-dialog__body"
      :width="760"
      :footer="false"
      unmount-on-close
      @cancel="cancelDetail"
    >
      <template #title>媒体流详情与观看者</template>
      <div v-if="detailLoading && !detail" class="drawer-state"><a-spin />正在读取详情和观看者…</div>
      <div v-else-if="detailError && !detail" class="drawer-state drawer-state--error" role="alert">
        <strong>{{ detailErrorPresentation.label }}</strong>
      </div>
      <div v-else-if="detail" class="detail-body">
        <dl class="detail-grid">
          <div>
            <dt>节点</dt>
            <dd>{{ detail.nodeName || `节点 #${detail.nodeId}` }} · {{ detail.nodeUuid }}</dd>
          </div>
          <div>
            <dt>完整身份</dt>
            <dd>{{ detail.media.schema }}://{{ detail.media.vhost }}/{{ detail.media.app }}/{{ detail.media.stream }}</dd>
          </div>
          <div>
            <dt>来源</dt>
            <dd>{{ detail.originTypeName || detail.originType }}</dd>
          </div>
          <div>
            <dt>在线 / 当前吞吐</dt>
            <dd>{{ formatZLMDuration(detail.aliveSecond) }} · {{ formatZLMByteRate(detail.bytesSpeed) }}</dd>
          </div>
          <div>
            <dt>累计流量</dt>
            <dd>{{ formatZLMBytes(detail.totalBytes) }}</dd>
          </div>
          <div>
            <dt>业务归属</dt>
            <dd>{{ ownershipText(detail) }}</dd>
          </div>
          <div>
            <dt>Tracks</dt>
            <dd>{{ detail.trackCount }}</dd>
          </div>
        </dl>
        <section>
          <h3>业务持有来源</h3>
          <div v-if="detail.ownership.sources?.length" class="ownership-list">
            <span v-for="source in detail.ownership.sources" :key="`${source.type}-${source.confidence}`"
              >{{ source.type }} · {{ source.confidence }}</span
            >
          </div>
          <p v-else>后端未返回可证明的持有来源。</p>
        </section>
        <section>
          <h3>媒体轨道</h3>
          <a-table class="uvp-data-table detail-table" :data="detail.tracks || []" :pagination="false" size="small"
            ><template #columns
              ><a-table-column title="编码" data-index="codecIdName" /><a-table-column
                title="类型"
                data-index="codecType" /><a-table-column title="就绪"
                ><template #cell="{ record }">{{ record.ready ? "是" : "否" }}</template></a-table-column
              ><a-table-column title="分辨率"
                ><template #cell="{ record }">{{
                  record.width && record.height ? `${record.width}×${record.height}` : "—"
                }}</template></a-table-column
              ><a-table-column title="FPS" data-index="fps" /></template
          ></a-table>
        </section>
        <section>
          <h3>观看者（{{ detailViewers?.total ?? 0 }}）</h3>
          <a-table class="uvp-data-table detail-table" :data="detailViewers?.list || []" :pagination="false" size="small"
            ><template #columns
              ><a-table-column title="标识" data-index="identifier" /><a-table-column title="远端"
                ><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column
              ><a-table-column title="本地"
                ><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column
              ><a-table-column title="类型" data-index="typeId" /></template></a-table
          ><a-pagination
            v-if="(detailViewers?.total || 0) > 10"
            :current="detailViewerPage"
            :page-size="10"
            :total="detailViewers?.total || 0"
            simple
            @change="changeViewerPage"
          />
        </section>
      </div>
    </a-drawer>
  </div>
</template>

<style scoped>
.monitoring-panel {
  box-sizing: border-box;
  min-width: 0;
  color: var(--zlm-text-2);
}
.monitoring-banner {
  padding: 9px 12px;
  margin: 10px 0;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-2);
  background: var(--zlm-info-50);
  border: 1px solid var(--zlm-info-500);
  border-radius: var(--zlm-radius-md);
}
.monitoring-banner--warning {
  color: var(--zlm-warn-600);
  background: var(--zlm-warn-50);
  border-color: var(--zlm-warn-500);
}
.monitoring-state {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  justify-content: center;
  min-height: 280px;
  text-align: center;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
}
.monitoring-state strong {
  color: var(--zlm-text-1);
}
.monitoring-state--error {
  color: var(--zlm-danger-600);
  background: var(--zlm-danger-50);
  border-color: var(--zlm-danger-500);
}
.stream-search {
  margin: 0 0 12px;
}
.stream-refresh-btn {
  min-width: 128px;
}
.filter-short {
  width: 100px;
}
.filter-vhost {
  width: 170px;
}
.filter-app {
  width: 130px;
}
.filter-stream {
  width: 190px;
}
.filter-recording {
  flex: 0 0 132px;
  width: 132px;
}
.stream-search :deep(.arco-select-view) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border: 1px solid var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}
.stream-search :deep(.arco-select-view-focus) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}
.stream-table-panel {
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: var(--uvp-panel-radius);
  box-shadow: var(--uvp-panel-shadow);
}
.identity-link {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  max-width: 100%;
  padding: 0;
  color: var(--zlm-brand-600);
  text-align: left;
  cursor: pointer;
  background: none;
  border: 0;
}
.identity-link > span {
  min-width: 0;
}
.identity-link strong,
.identity-link small,
.subline {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.identity-link small,
.subline {
  margin-top: 2px;
  font-family: var(--zlm-font-mono);
  font-size: 11px;
  color: var(--zlm-text-4);
}
.identity-link:focus-visible {
  outline: 2px solid var(--zlm-brand-500);
  outline-offset: 3px;
}
.numeric {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-family: var(--zlm-font-mono);
}
.recording {
  color: var(--zlm-danger-600);
}
.muted {
  color: var(--zlm-text-4);
}
.ownership {
  font-size: var(--zlm-fs-caption);
}
.ownership--managed {
  color: var(--zlm-success-600);
}
.ownership--owned,
.ownership--conflicted {
  color: var(--zlm-warn-600);
}
.ownership--unknown {
  color: var(--zlm-danger-600);
}
.stream-row-actions {
  flex-wrap: wrap;
}
.stream-empty {
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  padding: 48px 16px;
  color: var(--zlm-text-3);
}
.stream-empty strong {
  color: var(--zlm-text-1);
}
.stream-pagination {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: center;
  justify-content: space-between;
  margin-top: 12px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.drawer-state {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  justify-content: center;
  min-height: 260px;
}
.drawer-state--error {
  color: var(--zlm-danger-600);
}
.detail-body {
  display: flex;
  flex-direction: column;
  gap: 18px;
  color: var(--zlm-text-2);
}
.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 16px;
  margin: 0;
}
.detail-grid div {
  min-width: 0;
}
.detail-grid dt {
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.detail-grid dd {
  margin: 4px 0 0;
  font-family: var(--zlm-font-mono);
  color: var(--zlm-text-1);
  overflow-wrap: anywhere;
}
.detail-body h3 {
  margin: 0 0 8px;
  font-size: 14px;
  color: var(--zlm-text-1);
}
.detail-body p {
  color: var(--zlm-text-3);
}
.detail-table {
  overflow: hidden;
  border-radius: 10px;
}
.ownership-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.ownership-list span {
  padding: 4px 8px;
  font-size: var(--zlm-fs-caption);
  background: var(--zlm-fill-2);
  border: 1px solid var(--zlm-border);
  border-radius: var(--zlm-radius-sm);
}

@media (width <= 900px) {
  .filter-short,
  .filter-vhost,
  .filter-app,
  .filter-stream {
    width: 100%;
  }
  .detail-grid {
    grid-template-columns: 1fr;
  }
}
</style>
