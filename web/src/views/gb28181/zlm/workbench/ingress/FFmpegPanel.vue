<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Plus, RefreshCw, ShieldAlert } from "lucide-vue-next";

import {
  createZLMFFmpegSource,
  deleteZLMFFmpegSource,
  listZLMFFmpegSources,
  preflightDeleteZLMFFmpegSource,
  type ZLMCapabilityState,
  type ZLMFFmpegSource,
  type ZLMFFmpegSourceCreateRequest,
  type ZLMFFmpegSourcePage
} from "@/api/gb28181-zlm-ingress";
import type { ZLMOwnershipPreflight } from "@/api/gb28181-zlm-runtime";
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";

import FFmpegSourceForm from "../../FFmpegSourceForm.vue";
import ZLMDangerActionDialog from "../../components/ZLMDangerActionDialog.vue";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import { ffmpegCreateDecision, ffmpegURLText } from "../../ffmpegSourcesState";
import { ingressCapabilityFromError } from "../../proxyManagementState";
import { boundedPageRows } from "../boundedData";

const props = withDefaults(defineProps<{
  active?: boolean;
  scope?: MediaScope;
  nodes?: readonly MediaNodeCatalogNode[];
  refreshKey?: number;
}>(), {
  active: true,
  scope: "all",
  nodes: () => [],
  refreshKey: 0
});

const userStore = useUserStoreHook();
const nodeId = computed<number | null>(() => props.scope === "all" ? null : props.scope);
const canPoll = computed(() => props.active && props.scope !== "all");
const scopeBlocked = computed(() => props.scope === "all");
const selectedNodeName = computed(() => props.nodes.find(node => node.id === nodeId.value)?.name ?? `节点 #${nodeId.value ?? "—"}`);
const pageData = ref<ZLMFFmpegSourcePage | null>(null);
const observedCapability = ref<ZLMCapabilityState>("unknown");
const page = ref(1);
const pageSize = ref(20);
const loading = ref(false);
const loadError = ref<unknown>(null);
const formVisible = ref(false);
const saving = ref(false);
const deleteVisible = ref(false);
const deleteTarget = ref<ZLMFFmpegSource | null>(null);
const deletePreview = ref<ZLMOwnershipPreflight | null>(null);
const deleteLoading = ref(false);
const deleteError = ref<unknown>(null);
const deleting = ref(false);

const rows = computed(() => pageData.value?.list ?? []);
const tablePagination = computed(() => pageData.value ? ({
  current: pageData.value.page,
  pageSize: pageData.value.pageSize,
  total: pageData.value.total,
  showTotal: true,
  showJumper: true,
  showPageSize: true,
  pageSizeOptions: [10, 20, 50, 100]
}) : false);
const capability = computed<ZLMCapabilityState>(() => pageData.value?.capability ?? observedCapability.value);
const templates = computed(() => pageData.value?.templates ?? []);
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:ffmpeg:manage"));
const createDecision = computed(() => ffmpegCreateDecision(capability.value, templates.value, canManage.value && !scopeBlocked.value));
const paused = computed(() => formVisible.value || deleteVisible.value);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const deleteAllowed = computed(() => {
  const snapshot = deletePreview.value?.snapshot;
  return Boolean(nodeId.value !== null && deleteTarget.value?.nodeId === nodeId.value && snapshot?.present && snapshot.presenceKnown && snapshot.status === "managed");
});
const deleteReason = computed(() => {
  if (deleteError.value) return zlmErrorPresentation(deleteError.value).label;
  const snapshot = deletePreview.value?.snapshot;
  if (!snapshot) return "正在等待后端归属预检。";
  if (!snapshot.present && snapshot.presenceKnown) return "后端确认该源已经不存在。";
  if (snapshot.status !== "managed") return "该源受业务持有或归属未知，普通删除受保护。";
  return "后端确认该源由管理台创建，可执行普通删除。";
});
const deleteImpacts = computed(() => deletePreview.value?.snapshot.impacts?.map(impact =>
  impact.reason || impact.owner || [impact.resourceType, impact.resourceKey].filter(Boolean).join(" / ")
).filter(Boolean) as string[] ?? []);

const { refresh } = useZLMRuntimePolling<ZLMFFmpegSourcePage>({
  nodeId,
  active: canPoll,
  paused,
  intervalMs: 8_000,
  async load(currentNodeId, signal) {
    if (props.scope === "all") throw new Error("全部节点范围不执行节点级 FFmpeg 查询");
    loading.value = true;
    const response = await listZLMFFmpegSources(currentNodeId, { page: page.value, pageSize: pageSize.value }, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "FFmpeg 源列表加载失败");
    return response.data;
  },
  publish(value) {
    pageData.value = { ...value, list: boundedPageRows(value.list, pageSize.value) };
    observedCapability.value = value.capability;
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    observedCapability.value = ingressCapabilityFromError(error);
    loadError.value = error;
    loading.value = false;
  }
});

watch([nodeId, () => props.active], ([nextNodeId, nextActive]) => {
  pageData.value = null;
  observedCapability.value = "unknown";
  formVisible.value = false;
  closeDelete();
  loading.value = Boolean(nextNodeId && nextActive);
});

watch(() => props.refreshKey, (next, previous) => {
  if (next !== previous && canPoll.value) refresh();
});

function warnNodeScope() {
  Message.warning("请先选择具体媒体节点；全部节点范围不支持节点级接入操作。");
}

function openCreate() {
  if (!props.active || scopeBlocked.value) return warnNodeScope();
  if (!createDecision.value.allowed) return;
  formVisible.value = true;
}

async function createSource(request: ZLMFFmpegSourceCreateRequest) {
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  const currentNodeId = nodeId.value;
  saving.value = true;
  try {
    const response = await createZLMFFmpegSource(currentNodeId, request);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "FFmpeg 源创建失败");
    formVisible.value = false;
    Message.success(`FFmpeg 源 ${response.data.key} 已由后端确认创建`);
    refresh();
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    saving.value = false;
  }
}

async function openDelete(target: ZLMFFmpegSource) {
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  if (target.nodeId !== nodeId.value || !target.managed || !canManage.value || capability.value !== "supported") return;
  deleteTarget.value = { ...target, srcUrl: { ...target.srcUrl }, dstUrl: { ...target.dstUrl } };
  deletePreview.value = null;
  deleteError.value = null;
  deleteVisible.value = true;
  deleteLoading.value = true;
  try {
    const response = await preflightDeleteZLMFFmpegSource(nodeId.value, target.key);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "删除预检失败");
    deletePreview.value = response.data;
  } catch (error) {
    deleteError.value = error;
  } finally {
    deleteLoading.value = false;
  }
}

function closeDelete() {
  if (deleting.value) return;
  deleteVisible.value = false;
  deleteTarget.value = null;
  deletePreview.value = null;
  deleteError.value = null;
}

async function confirmDelete(payload: { fingerprint: string }) {
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  const target = deleteTarget.value;
  if (!target || target.nodeId !== nodeId.value || !deleteAllowed.value || payload.fingerprint !== deletePreview.value?.fingerprint) return;
  deleting.value = true;
  try {
    const response = await deleteZLMFFmpegSource(nodeId.value, target.key);
    if (response.code !== 0 || !response.data || (!response.data.released && !response.data.alreadyReleased)) {
      throw new Error(response.message || "后端未确认 FFmpeg 源已释放");
    }
    deleteVisible.value = false;
    Message.success(response.data.alreadyReleased ? "后端回读确认该源已不存在" : "FFmpeg 源已释放");
    refresh();
  } catch (error) {
    deleteError.value = error;
  } finally {
    deleting.value = false;
  }
}

function changePage(next: number) { page.value = next; if (canPoll.value) refresh(); }
function changePageSize(next: number) { pageSize.value = next; page.value = 1; if (canPoll.value) refresh(); }
</script>

<template>
  <section class="ingress-panel" aria-label="FFmpeg 源管理">
    <header class="panel-toolbar">
      <div class="toolbar-actions">
        <a-button class="uvp-refresh-btn" :loading="loading" :disabled="!canPoll" @click="refresh"><template #icon><RefreshCw :size="15" /></template>刷新</a-button>
        <a-button type="primary" :disabled="!props.active || !createDecision.allowed || scopeBlocked" @click="openCreate"><template #icon><Plus :size="15" /></template>创建源</a-button>
      </div>
    </header>
    <div v-if="scopeBlocked" class="scope-warning" role="status"><strong>全部节点范围</strong><span>FFmpeg 源列表和写操作需要先选择一个具体节点。</span></div>
    <section class="data-panel">
      <div v-if="loadError && !pageData" class="state-box state-box--error" role="alert"><ShieldAlert :size="28" /><strong>{{ errorPresentation.label }}</strong><a-button @click="refresh">重新加载</a-button></div>
      <div v-else-if="scopeBlocked" class="state-box" role="status"><strong>请选择具体节点</strong><span>全部节点模式不会发起逐节点 FFmpeg 查询。</span></div>
      <template v-else>
      <div v-if="pageData?.truncated" class="result-notice">结果已被后端有界截断。</div>
      <a-table :data="rows" :loading="loading" :pagination="tablePagination" row-key="key" class="uvp-data-table" :scroll="{ x: 1060 }" @page-change="changePage" @page-size-change="changePageSize">
        <template #columns>
          <a-table-column title="源标识" :width="220"><template #cell="{ record }"><strong>{{ record.key }}</strong><div class="subtle">模板：{{ record.templateKey }}</div></template></a-table-column>
          <a-table-column title="源地址摘要" :width="260"><template #cell="{ record }"><code>{{ ffmpegURLText(record.srcUrl) }}</code><div class="subtle">指纹：{{ record.srcUrl.fingerprint }}</div></template></a-table-column>
          <a-table-column title="目标地址摘要" :width="260"><template #cell="{ record }"><code>{{ ffmpegURLText(record.dstUrl) }}</code><div class="subtle">指纹：{{ record.dstUrl.fingerprint }}</div></template></a-table-column>
          <a-table-column title="来源" :width="130"><template #cell="{ record }"><a-tag :color="record.managed ? 'blue' : 'orange'">{{ record.managed ? '管理台' : '业务/未知' }}</a-tag><div class="subtle">{{ record.createdBy ? `用户 #${record.createdBy}` : '无创建人' }}</div></template></a-table-column>
          <a-table-column title="操作" fixed="right" :width="120"><template #cell="{ record }"><a-button size="small" status="danger" :disabled="!props.active || !canManage || capability !== 'supported' || !record.managed || scopeBlocked" @click="openDelete(record)">释放</a-button></template></a-table-column>
        </template>
      </a-table>
      </template>
    </section>

    <FFmpegSourceForm v-model:visible="formVisible" :capability="capability" :templates="templates" :permitted="props.active && canManage && !scopeBlocked" :loading="saving" @submit="createSource" />
    <ZLMDangerActionDialog
      v-if="props.active && deletePreview && deleteAllowed && deleteTarget && !scopeBlocked"
      v-model:visible="deleteVisible"
      :node-id="deleteTarget.nodeId"
      :node-name="selectedNodeName"
      :target-key="deleteTarget.key"
      :target-label="deleteTarget.key"
      :fingerprint="deletePreview.fingerprint"
      :impacts="deleteImpacts"
      :confirm-phrase="`释放 FFmpeg ${deleteTarget.key}`"
      :require-reason="false"
      action-label="确认释放"
      :busy="deleting"
      @confirm="confirmDelete"
      @stale="closeDelete"
    />
    <a-modal v-else :visible="deleteVisible" :footer="false" :width="540" unmount-on-close @cancel="closeDelete">
      <template #title>FFmpeg 源释放预检</template>
      <div v-if="deleteLoading" class="preflight-state"><a-spin /><span>正在读取后端真实状态与归属…</span></div>
      <div v-else class="preflight-state" :class="{ 'state-box--error': deleteError }"><strong>{{ deleteError ? zlmErrorPresentation(deleteError).label : deleteReason }}</strong><a-button @click="closeDelete">关闭</a-button></div>
    </a-modal>
  </section>
</template>

<style scoped>
.ingress-panel { display: flex; min-width: 0; flex-direction: column; gap: 12px; padding-bottom: 12px; color: var(--zlm-text-2); }.panel-toolbar { display: flex; align-items: center; justify-content: flex-end; gap: 16px; }
.toolbar-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; }.toolbar-actions :deep(.arco-btn) { box-sizing: border-box; min-width: 88px; height: 40px; padding-inline: 14px; border-radius: 10px; font-weight: 600; }
.scope-warning { display: flex; align-items: center; gap: 9px; padding: 10px 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); background: var(--zlm-warn-50); border: 1px solid var(--zlm-warn-500); border-radius: var(--zlm-radius-md); }
.data-panel { padding: 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.subtle { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); } code { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); overflow-wrap: anywhere; }.result-notice { padding: 8px 10px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); background: var(--zlm-warn-50); border-radius: 8px; }.state-box, .preflight-state { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; }.state-box--error { color: var(--zlm-danger-600); }
@media (max-width: 760px) { .scope-warning { align-items: flex-start; flex-direction: column; } }
</style>
