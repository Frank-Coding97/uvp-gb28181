<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, toRef, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Camera, Copy, Eye, Radio, ShieldAlert, Users } from "lucide-vue-next";

import {
  getZLMStreamDetail,
  fetchZLMStreamSnapshot,
  issueZLMPreviewGrant,
  listZLMStreams,
  listZLMStreamViewers,
  type ZLMOwnershipTarget,
  type ZLMPreviewGrant,
  type ZLMStream,
  type ZLMStreamDetail,
  type ZLMStreamPage,
  type ZLMStreamViewerPage
} from "@/api/gb28181-zlm-runtime";
import type { MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";
import PlayWindow from "@/views/gb28181/components/PlayWindow.vue";

import ZLMStreamCloseDialog from "../../ZLMStreamCloseDialog.vue";
import { formatZLMByteRate, formatZLMBytes, formatZLMDuration, zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import { streamIdentityKey as mediaIdentityKey } from "../../streamManagementState";
import { buildStreamRequestQuery, createStreamFilters, sameNodeTargets, type MonitoringStreamFilters } from "./monitoringState";
import { boundedPageRows } from "../boundedData";

const props = withDefaults(defineProps<{
  active: boolean;
  scope: MediaScope;
  nodeId: number | null;
  initialQuery?: Record<string, unknown>;
}>(), {
  active: true,
  initialQuery: undefined
});

const userStore = useUserStoreHook();
const filters = reactive<MonitoringStreamFilters>(createStreamFilters(props.initialQuery));
const page = ref(1);
const pageSize = ref(20);
const pageData = ref<ZLMStreamPage | null>(null);
const loading = ref(false);
const loadError = ref<unknown>(null);
const selectedKeys = ref<string[]>([]);
const detailVisible = ref(false);
const detailLoading = ref(false);
const detailError = ref<unknown>(null);
const detail = ref<ZLMStreamDetail | null>(null);
const detailViewers = ref<ZLMStreamViewerPage | null>(null);
const detailViewerPage = ref(1);
const detailTarget = ref<ZLMOwnershipTarget | null>(null);
const previewVisible = ref(false);
const previewLoading = ref(false);
const previewError = ref<unknown>(null);
const previewTarget = ref<ZLMOwnershipTarget | null>(null);
const previewGrant = ref<ZLMPreviewGrant | null>(null);
const previewProtocol = ref("https-flv");
const snapshotVisible = ref(false);
const snapshotTarget = ref<ZLMOwnershipTarget | null>(null);
const snapshotLoading = ref(false);
const snapshotError = ref<unknown>(null);
const snapshotURL = ref("");
const closeVisible = ref(false);
const closeTargets = ref<ZLMOwnershipTarget[]>([]);
const closeForce = ref(false);
let detailGeneration = 0;
let previewGeneration = 0;
let detailController: AbortController | null = null;
let snapshotController: AbortController | null = null;
let snapshotGeneration = 0;

const requestNodeId = computed(() => props.scope === "all" ? 1 : props.nodeId);
const scopeLabel = computed(() => props.scope === "all" ? "全部节点" : `节点 #${props.nodeId ?? "—"}`);
type StreamTableRow = ZLMStream & { rowKey: string };
const rows = computed<StreamTableRow[]>(() => (pageData.value?.list ?? []).map(stream => ({
  ...stream,
  rowKey: streamIdentityKey(stream)
})));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const detailErrorPresentation = computed(() => zlmErrorPresentation(detailError.value));
const previewErrorPresentation = computed(() => zlmErrorPresentation(previewError.value));
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canPreview = computed(() => hasPermission("gb28181:zlm:stream:preview"));
const canClose = computed(() => hasPermission("gb28181:zlm:stream:close"));
const canForceClose = computed(() => hasPermission("gb28181:zlm:stream:force-close"));
const readOnly = computed(() => !canPreview.value && !canClose.value && !canForceClose.value);
const pollingPaused = computed(() => detailVisible.value || previewVisible.value || snapshotVisible.value || closeVisible.value);
const selectedTargets = computed(() => {
  const selected = new Set(selectedKeys.value);
  return rows.value
    .filter(stream => selected.has(streamIdentityKey(stream)))
    .map(stream => ({ nodeId: stream.nodeId, media: { ...stream.media } }));
});
const canBatchClose = computed(() => sameNodeTargets(selectedTargets.value));
const closeNodeName = computed(() => {
  const nodeId = closeTargets.value[0]?.nodeId;
  return nodeId ? `节点 #${nodeId}` : scopeLabel.value;
});

function streamIdentityKey(stream: Pick<ZLMStream, "nodeId" | "media">) {
  return mediaIdentityKey(stream.nodeId, stream.media);
}

const { refresh } = useZLMRuntimePolling<ZLMStreamPage>({
  nodeId: requestNodeId,
  active: toRef(props, "active"),
  paused: pollingPaused,
  intervalMs: 8_000,
  async load(nodeId, signal) {
    loading.value = true;
    const query = buildStreamRequestQuery(filters, page.value, pageSize.value, props.scope === "all" ? undefined : props.nodeId ?? nodeId);
    const response = await listZLMStreams(query, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "媒体流列表加载失败");
    return response.data;
  },
  publish(value) {
    pageData.value = { ...value, list: boundedPageRows(value.list, pageSize.value) };
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
  }
});

function cancelDetail() {
  detailGeneration += 1;
  detailController?.abort();
  detailController = null;
}

function clearSnapshot() {
  snapshotGeneration += 1;
  snapshotController?.abort();
  snapshotController = null;
  if (snapshotURL.value) URL.revokeObjectURL(snapshotURL.value);
  snapshotURL.value = "";
  snapshotError.value = null;
  snapshotLoading.value = false;
}

function closeInteractions() {
  cancelDetail();
  previewGeneration += 1;
  detailVisible.value = false;
  previewVisible.value = false;
  snapshotVisible.value = false;
  clearSnapshot();
  closeVisible.value = false;
  detail.value = null;
  detailViewers.value = null;
  detailTarget.value = null;
  previewTarget.value = null;
  previewGrant.value = null;
}

watch([() => props.scope, () => props.nodeId], () => {
  closeInteractions();
  selectedKeys.value = [];
  pageData.value = null;
  loadError.value = null;
  loading.value = props.scope === "all" || props.nodeId !== null;
  if (props.active) refresh();
});

function applyFilters() {
  page.value = 1;
  selectedKeys.value = [];
  refresh();
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
  selectedKeys.value = [];
  refresh();
}

function changePageSize(nextPageSize: number) {
  pageSize.value = nextPageSize;
  page.value = 1;
  selectedKeys.value = [];
  refresh();
}

function ownershipText(stream: Pick<ZLMStream, "ownership">) {
  switch (stream.ownership.status) {
    case "managed": return "管理资源";
    case "owned": return "业务持有";
    case "conflicted": return "持有冲突";
    case "unknown": return "归属未知";
    default: return "未登记";
  }
}

async function loadDetailViewers(viewerPage = detailViewerPage.value) {
  const target = detailTarget.value;
  if (!target) return;
  const currentGeneration = detailGeneration;
  const response = await listZLMStreamViewers(target.nodeId, target.media, { page: viewerPage, pageSize: 20 }, detailController?.signal);
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

async function loadPreviewGrant() {
  const target = previewTarget.value;
  if (!target) return;
  const currentGeneration = ++previewGeneration;
  previewLoading.value = true;
  previewError.value = null;
  previewGrant.value = null;
  try {
    const response = await issueZLMPreviewGrant(target.nodeId, target.media, previewProtocol.value);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "预览授权失败");
    if (currentGeneration === previewGeneration && previewVisible.value) previewGrant.value = response.data;
  } catch (error) {
    if (currentGeneration === previewGeneration) previewError.value = error;
  } finally {
    if (currentGeneration === previewGeneration) previewLoading.value = false;
  }
}

function openPreview(stream: ZLMStream) {
  if (!canPreview.value) return;
  previewTarget.value = { nodeId: stream.nodeId, media: { ...stream.media } };
  previewVisible.value = true;
  void loadPreviewGrant();
}

async function copyPreviewURL() {
  if (!previewGrant.value?.url) return;
  try {
    await navigator.clipboard.writeText(previewGrant.value.url);
    Message.success("授权播放地址已复制；请注意其到期时间");
  } catch {
    Message.error("复制失败，请检查浏览器剪贴板权限");
  }
}

async function openSnapshot(stream: ZLMStream) {
  if (!canPreview.value) return;
  clearSnapshot();
  snapshotTarget.value = { nodeId: stream.nodeId, media: { ...stream.media } };
  snapshotVisible.value = true;
  snapshotLoading.value = true;
  snapshotController = new AbortController();
  const currentGeneration = snapshotGeneration;
  try {
    const blob = await fetchZLMStreamSnapshot(stream.nodeId, stream.media, snapshotController.signal);
    if (currentGeneration !== snapshotGeneration || !snapshotVisible.value) return;
    if (!(blob instanceof Blob) || blob.type !== "image/jpeg") throw new Error("后端没有返回有效 JPEG 截图");
    snapshotURL.value = URL.createObjectURL(blob);
  } catch (error) {
    if (currentGeneration === snapshotGeneration && (error as { name?: string })?.name !== "AbortError") snapshotError.value = error;
  } finally {
    if (currentGeneration === snapshotGeneration) snapshotLoading.value = false;
  }
}

function openClose(stream: ZLMStream, force: boolean) {
  if (force ? !canForceClose.value : !canClose.value) return;
  closeTargets.value = [{ nodeId: stream.nodeId, media: { ...stream.media } }];
  closeForce.value = force;
  closeVisible.value = true;
}

function openBatchClose() {
  if (!canClose.value || !canBatchClose.value) return;
  closeTargets.value = selectedTargets.value.map(target => ({ nodeId: target.nodeId, media: { ...target.media } }));
  closeForce.value = false;
  closeVisible.value = true;
}

function closeDone(result: { closed: number; alreadyAbsent: number; partial: boolean; uncertain: boolean }) {
  selectedKeys.value = [];
  if (result.uncertain || result.partial) Message.warning(`关闭已返回：成功 ${result.closed}，已不存在 ${result.alreadyAbsent}；部分结果需重新回读。`);
  else Message.success(`关闭完成：成功 ${result.closed}，已不存在 ${result.alreadyAbsent}。`);
  refresh();
}

onBeforeUnmount(() => {
  cancelDetail();
  previewGeneration += 1;
  clearSnapshot();
});

defineExpose({ refresh });
</script>

<template>
  <div class="monitoring-panel stream-panel">
    <div v-if="readOnly" class="monitoring-banner" role="status">当前账号仅可查看流运行态；预览、普通关闭和强制关闭按钮按独立权限隐藏。</div>
    <div v-if="pageData?.partial" class="monitoring-banner monitoring-banner--warning" role="status">部分节点采集失败，仍展示已返回的 {{ pageData.list.length }} 条数据，不清空整页。</div>
    <div v-else-if="loadError && pageData" class="monitoring-banner monitoring-banner--warning" role="status">本次刷新失败：{{ errorPresentation.label }}；保留上一次分页与筛选结果。</div>

    <s-layout-search class="stream-search">
      <template #fields>
        <a-input v-model="filters.schema" allow-clear placeholder="Schema" class="filter-short" @press-enter="applyFilters" />
        <a-input v-model="filters.vhost" allow-clear placeholder="VHost" class="filter-vhost" @press-enter="applyFilters" />
        <a-input v-model="filters.app" allow-clear placeholder="App" class="filter-app" @press-enter="applyFilters" />
        <a-input-search v-model="filters.stream" allow-clear placeholder="Stream ID" class="filter-stream" @search="applyFilters" />
        <a-select v-model="filters.recording" allow-clear placeholder="录制状态" class="filter-recording" style="width: 132px; min-width: 132px; max-width: 132px; flex: 0 0 132px"><a-option value="mp4">MP4 录制</a-option><a-option value="hls">HLS 录制</a-option></a-select>
      </template>
      <template #actions><a-button type="primary" @click="applyFilters">查询</a-button><a-button @click="clearFilters">重置</a-button><a-button class="uvp-page-action-btn uvp-refresh-btn" :loading="loading" aria-label="刷新媒体流" @click="refresh"><template #icon><icon-refresh /></template>刷新</a-button></template>
      <template #extra><span class="selection-meta">已选 {{ selectedTargets.length }} 路</span><a-button v-if="canClose" status="danger" :disabled="!canBatchClose" @click="openBatchClose">批量普通关闭</a-button><span v-if="selectedTargets.length > 0 && !canBatchClose" class="selection-warning">跨节点选择不可批量关闭</span></template>
    </s-layout-search>

    <div v-if="loading && !pageData" class="monitoring-state" role="status"><a-spin />正在读取{{ scopeLabel }}媒体流…</div>
    <div v-else-if="loadError && !pageData" class="monitoring-state monitoring-state--error" role="alert"><ShieldAlert :size="34" /><strong>{{ errorPresentation.label }}</strong><a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button></div>
    <section v-else class="stream-table-panel">
      <a-table v-model:selected-keys="selectedKeys" :data="rows" :loading="loading" row-key="rowKey" :row-selection="canClose ? { type: 'checkbox', showCheckedAll: true } : undefined" :pagination="false" class="uvp-data-table">
        <template #columns>
          <a-table-column title="媒体身份" :width="310"><template #cell="{ record }"><button type="button" class="identity-link" :aria-label="`查看流 ${record.media.app}/${record.media.stream} 详情`" @click="openDetail(record)"><Radio :size="14" /><span><strong>{{ record.media.app }}/{{ record.media.stream }}</strong><small>{{ record.media.schema }} · {{ record.media.vhost }}</small></span></button></template></a-table-column>
          <a-table-column title="来源" :width="130"><template #cell="{ record }"><span>{{ record.originTypeName || `类型 ${record.originType}` }}</span><small class="subline">#{{ record.nodeId }}</small></template></a-table-column>
          <a-table-column title="观看 / 累计" :width="120"><template #cell="{ record }"><span class="numeric"><Users :size="13" /> {{ record.readerCount }} / {{ record.totalReaderCount }}</span></template></a-table-column>
          <a-table-column title="吞吐" :width="120"><template #cell="{ record }"><span class="numeric">{{ formatZLMByteRate(record.bytesSpeed) }}</span></template></a-table-column>
          <a-table-column title="在线时长" :width="130"><template #cell="{ record }">{{ formatZLMDuration(record.aliveSecond) }}</template></a-table-column>
          <a-table-column title="录制" :width="105"><template #cell="{ record }"><span v-if="record.recordingMp4 || record.recordingHls" class="recording">{{ [record.recordingMp4 && 'MP4', record.recordingHls && 'HLS'].filter(Boolean).join(' + ') }}</span><span v-else class="muted">未录制</span></template></a-table-column>
          <a-table-column title="归属" :width="110"><template #cell="{ record }"><span :class="`ownership ownership--${record.ownership.status}`">{{ ownershipText(record) }}</span></template></a-table-column>
          <a-table-column title="操作" :width="330" fixed="right"><template #cell="{ record }"><div class="row-actions"><a-button size="small" @click="openDetail(record)">详情</a-button><a-button v-if="canPreview" size="small" @click="openPreview(record)"><Eye :size="13" />预览</a-button><a-button v-if="canPreview" size="small" @click="openSnapshot(record)"><Camera :size="13" />截图</a-button><a-button v-if="canClose" size="small" status="danger" @click="openClose(record, false)">关闭</a-button><a-button v-if="canForceClose" size="small" status="danger" type="primary" @click="openClose(record, true)">强制</a-button></div></template></a-table-column>
        </template>
        <template #empty><div class="stream-empty" role="status"><Radio :size="38" /><strong>没有符合当前筛选的媒体流</strong><span>筛选条件与分页由后端执行，不会加载全量流到浏览器。</span></div></template>
      </a-table>
      <div class="stream-pagination"><span>共 {{ pageData?.total ?? 0 }} 路<span v-if="pageData?.truncated">（结果已由后端截断）</span></span><a-pagination :current="page" :page-size="pageSize" :total="pageData?.total ?? 0" show-total show-page-size :page-size-options="[20, 50, 100]" @change="changePage" @page-size-change="changePageSize" /></div>
    </section>

    <a-drawer v-model:visible="detailVisible" :width="760" :footer="false" unmount-on-close @cancel="cancelDetail">
      <template #title>媒体流详情与观看者</template>
      <div v-if="detailLoading && !detail" class="drawer-state"><a-spin />正在读取详情和观看者…</div>
      <div v-else-if="detailError && !detail" class="drawer-state drawer-state--error" role="alert"><strong>{{ detailErrorPresentation.label }}</strong></div>
      <div v-else-if="detail" class="detail-body">
        <dl class="detail-grid"><div><dt>节点</dt><dd>#{{ detail.nodeId }} · {{ detail.nodeUuid }}</dd></div><div><dt>完整身份</dt><dd>{{ detail.media.schema }}://{{ detail.media.vhost }}/{{ detail.media.app }}/{{ detail.media.stream }}</dd></div><div><dt>来源</dt><dd>{{ detail.originTypeName || detail.originType }}</dd></div><div><dt>在线 / 当前吞吐</dt><dd>{{ formatZLMDuration(detail.aliveSecond) }} · {{ formatZLMByteRate(detail.bytesSpeed) }}</dd></div><div><dt>累计流量</dt><dd>{{ formatZLMBytes(detail.totalBytes) }}</dd></div><div><dt>归属</dt><dd>{{ ownershipText(detail) }}</dd></div><div><dt>Tracks</dt><dd>{{ detail.trackCount }}</dd></div></dl>
        <section><h3>业务持有来源</h3><div v-if="detail.ownership.sources?.length" class="ownership-list"><span v-for="source in detail.ownership.sources" :key="`${source.type}-${source.confidence}`">{{ source.type }} · {{ source.confidence }}</span></div><p v-else>后端未返回可证明的持有来源。</p></section>
        <section><h3>媒体轨道</h3><a-table :data="detail.tracks || []" :pagination="false" size="small"><template #columns><a-table-column title="编码" data-index="codecIdName" /><a-table-column title="类型" data-index="codecType" /><a-table-column title="就绪"><template #cell="{ record }">{{ record.ready ? '是' : '否' }}</template></a-table-column><a-table-column title="分辨率"><template #cell="{ record }">{{ record.width && record.height ? `${record.width}×${record.height}` : '—' }}</template></a-table-column><a-table-column title="FPS" data-index="fps" /></template></a-table></section>
        <section><h3>观看者（{{ detailViewers?.total ?? 0 }}）</h3><a-table :data="detailViewers?.list || []" :pagination="false" size="small"><template #columns><a-table-column title="标识" data-index="identifier" /><a-table-column title="远端"><template #cell="{ record }">{{ record.peerIp }}:{{ record.peerPort }}</template></a-table-column><a-table-column title="本地"><template #cell="{ record }">{{ record.localIp }}:{{ record.localPort }}</template></a-table-column><a-table-column title="类型" data-index="typeId" /></template></a-table><a-pagination v-if="(detailViewers?.total || 0) > 20" :current="detailViewerPage" :page-size="20" :total="detailViewers?.total || 0" simple @change="changeViewerPage" /></section>
      </div>
    </a-drawer>

    <a-drawer v-model:visible="previewVisible" :width="760" :footer="false" unmount-on-close @cancel="previewGeneration += 1; previewGrant = null">
      <template #title>受控媒体预览</template>
      <div class="preview-toolbar"><a-select v-model="previewProtocol" :style="{ width: '150px' }" @change="loadPreviewGrant"><a-option value="https-flv">HTTPS-FLV</a-option><a-option value="wss-flv">WSS-FLV</a-option><a-option value="https-hls">HTTPS-HLS</a-option><a-option value="webrtcs">WebRTC Secure</a-option></a-select><a-button :loading="previewLoading" @click="loadPreviewGrant">刷新授权</a-button><a-button :disabled="!previewGrant" @click="copyPreviewURL"><Copy :size="14" />复制授权地址</a-button></div>
      <div v-if="previewError" class="preview-error" role="alert">{{ previewErrorPresentation.label }}</div><div v-if="previewGrant" class="preview-expiry">授权到期：{{ previewGrant.expiresAt }}；地址仅来自后端授权响应，页面不持有节点 API Secret。</div><PlayWindow :url="previewGrant?.url || ''" :zlm-webrtc="previewGrant?.protocol === 'webrtcs'" @error="Message.error($event)" />
    </a-drawer>

    <a-modal v-model:visible="snapshotVisible" modal-class="uvp-system-dialog" :width="860" :footer="false" unmount-on-close @cancel="clearSnapshot"><template #title>媒体截图</template><div class="snapshot-body"><p>截图由共享鉴权客户端从 UVP 后端读取为 JPEG Blob，不在浏览器拼接 ZLM Host、端口、Secret 或 token 查询串。</p><div v-if="snapshotLoading" class="drawer-state"><a-spin />正在读取截图…</div><div v-else-if="snapshotError" class="preview-error" role="alert">{{ zlmErrorPresentation(snapshotError).label }}</div><img v-else-if="snapshotURL" :src="snapshotURL" alt="媒体流实时截图" /></div></a-modal>

    <ZLMStreamCloseDialog v-model:visible="closeVisible" :node-name="closeNodeName" :targets="closeTargets" :force="closeForce" :can-force="canForceClose" @done="closeDone" />
  </div>
</template>

<style scoped>
.monitoring-panel { box-sizing: border-box; min-width: 0; color: var(--zlm-text-2); }
.monitoring-banner { margin: 10px 0; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.monitoring-banner--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.monitoring-state { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; text-align: center; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); }.monitoring-state strong { color: var(--zlm-text-1); }.monitoring-state--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.stream-search { margin: 0 0 12px; }.filter-short { width: 100px; }.filter-vhost { width: 170px; }.filter-app { width: 130px; }.filter-stream { width: 190px; }.filter-recording { width: 132px; flex: 0 0 132px; }.selection-meta, .selection-warning { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.selection-warning { color: var(--zlm-warn-600); }
.stream-search :deep(.arco-select-view) { box-sizing: border-box; background: var(--uvp-search-control-bg) !important; border: 1px solid var(--uvp-search-secondary-btn-border) !important; border-radius: 10px !important; box-shadow: var(--uvp-search-control-shadow) !important; }
.stream-search :deep(.arco-select-view-focus) { border-color: var(--uvp-brand) !important; box-shadow: var(--uvp-search-control-focus-shadow) !important; }
.stream-table-panel { overflow: hidden; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.identity-link { display: inline-flex; align-items: center; gap: 8px; max-width: 100%; padding: 0; color: var(--zlm-brand-600); text-align: left; background: none; border: 0; cursor: pointer; }.identity-link > span { min-width: 0; }.identity-link strong, .identity-link small, .subline { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.identity-link small, .subline { margin-top: 2px; color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 11px; }.identity-link:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 3px; }.numeric { display: inline-flex; align-items: center; gap: 4px; font-family: var(--zlm-font-mono); }.recording { color: var(--zlm-danger-600); }.muted { color: var(--zlm-text-4); }.ownership { font-size: var(--zlm-fs-caption); }.ownership--managed { color: var(--zlm-success-600); }.ownership--owned, .ownership--conflicted { color: var(--zlm-warn-600); }.ownership--unknown { color: var(--zlm-danger-600); }.row-actions { display: flex; align-items: center; gap: 5px; }.row-actions svg { vertical-align: -2px; }.stream-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 48px 16px; color: var(--zlm-text-3); }.stream-empty strong { color: var(--zlm-text-1); }.stream-pagination { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 12px 16px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); border-top: 1px solid var(--zlm-border); }
.drawer-state { display: flex; min-height: 260px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; }.drawer-state--error { color: var(--zlm-danger-600); }.detail-body { display: flex; flex-direction: column; gap: 18px; color: var(--zlm-text-2); }.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 16px; margin: 0; }.detail-grid div { min-width: 0; }.detail-grid dt { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.detail-grid dd { margin: 4px 0 0; overflow-wrap: anywhere; color: var(--zlm-text-1); font-family: var(--zlm-font-mono); }.detail-body h3 { margin: 0 0 8px; color: var(--zlm-text-1); font-size: 14px; }.detail-body p { color: var(--zlm-text-3); }.ownership-list { display: flex; flex-wrap: wrap; gap: 8px; }.ownership-list span { padding: 4px 8px; background: var(--zlm-fill-2); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-sm); font-size: var(--zlm-fs-caption); }.preview-toolbar { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 12px; }.preview-expiry, .preview-error { margin-bottom: 12px; padding: 9px 12px; color: var(--zlm-text-2); background: var(--zlm-info-50); border: 1px solid var(--zlm-info-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.preview-error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }.snapshot-body p { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.snapshot-body img { display: block; max-width: 100%; max-height: 70vh; margin: 0 auto; background: #020617; border-radius: var(--zlm-radius-md); }
@media (max-width: 900px) { .filter-short, .filter-vhost, .filter-app, .filter-stream { width: 100%; }.detail-grid { grid-template-columns: 1fr; } }.stream-pagination { flex-wrap: wrap; }
</style>
