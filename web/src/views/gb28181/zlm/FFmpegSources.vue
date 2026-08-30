<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Plus, ShieldAlert, Workflow } from "lucide-vue-next";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
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
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import FFmpegSourceForm from "./FFmpegSourceForm.vue";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import { zlmErrorPresentation } from "./components/zlmFormatters";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import { ffmpegCreateDecision, ffmpegURLText } from "./ffmpegSourcesState";
import { ingressCapabilityFromError } from "./proxyManagementState";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref<unknown>(null);
const pageData = ref<ZLMFFmpegSourcePage | null>(null);
const observedCapability = ref<ZLMCapabilityState>("unknown");
const page = ref(1);
const pageSize = ref(20);
const loading = ref(true);
const loadError = ref<unknown>(null);
const formVisible = ref(false);
const saving = ref(false);
const deleteVisible = ref(false);
const deleteTarget = ref<ZLMFFmpegSource | null>(null);
const deletePreview = ref<ZLMOwnershipPreflight | null>(null);
const deleteLoading = ref(false);
const deleteError = ref<unknown>(null);
const deleting = ref(false);

const selectedNodeId = computed(() => context.selectedNodeId);
const selectedNodeName = computed(() => context.selectedNode?.name ?? `节点 #${selectedNodeId.value ?? "—"}`);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const rows = computed(() => pageData.value?.list ?? []);
const capability = computed<ZLMCapabilityState>(() => pageData.value?.capability ?? observedCapability.value);
const templates = computed(() => pageData.value?.templates ?? []);
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:ffmpeg:manage"));
const createDecision = computed(() => ffmpegCreateDecision(capability.value, templates.value, canManage.value));
const paused = computed(() => formVisible.value || deleteVisible.value);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value ?? nodesError.value));
const deleteAllowed = computed(() => {
  const snapshot = deletePreview.value?.snapshot;
  return Boolean(snapshot?.present && snapshot.presenceKnown && snapshot.status === "managed");
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

async function loadNodes() {
  nodesLoading.value = true;
  try {
    const response = await listZLMNodes();
    if (response.code !== 0) throw new Error(response.message || "节点列表加载失败");
    nodes.value = response.data?.list ?? [];
    nodesError.value = null;
  } catch (error) {
    nodesError.value = error;
  } finally {
    nodesLoading.value = false;
  }
}

const { refresh } = useZLMRuntimePolling<ZLMFFmpegSourcePage>({
  nodeId: selectedNodeId,
  paused,
  intervalMs: 8_000,
  async load(nodeId, signal) {
    loading.value = true;
    const response = await listZLMFFmpegSources(nodeId, { page: page.value, pageSize: pageSize.value }, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "FFmpeg 源列表加载失败");
    return response.data;
  },
  publish(value) {
    pageData.value = value;
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

watch(selectedNodeId, (nodeId, previous) => {
  if (nodeId === previous) return;
  pageData.value = null;
  observedCapability.value = "unknown";
  formVisible.value = false;
  closeDelete();
  loading.value = nodeId !== null;
  if (nodeId && previous !== null && route.query.nodeId !== String(nodeId)) {
    void router.replace({ query: { ...route.query, nodeId: String(nodeId) } });
  }
});

async function createSource(request: ZLMFFmpegSourceCreateRequest) {
  const nodeId = selectedNodeId.value;
  if (!nodeId || !createDecision.value.allowed) return;
  saving.value = true;
  try {
    const response = await createZLMFFmpegSource(nodeId, request);
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
  if (!target.managed || !canManage.value || capability.value !== "supported") return;
  deleteTarget.value = { ...target, srcUrl: { ...target.srcUrl }, dstUrl: { ...target.dstUrl } };
  deletePreview.value = null;
  deleteError.value = null;
  deleteVisible.value = true;
  deleteLoading.value = true;
  try {
    const response = await preflightDeleteZLMFFmpegSource(target.nodeId, target.key);
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

async function confirmDelete() {
  const target = deleteTarget.value;
  if (!target || !deleteAllowed.value) return;
  deleting.value = true;
  try {
    const response = await deleteZLMFFmpegSource(target.nodeId, target.key);
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

function changePage(next: number) { page.value = next; refresh(); }
function changePageSize(next: number) { pageSize.value = next; page.value = 1; refresh(); }

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill"><div class="snow-fill-inner uvp-page-shell-flat ingress-page">
    <header class="page-header">
      <div><h1>FFmpeg 源</h1><p>只接受后端登记的模板标识和类型化参数；浏览器不能提交任意可执行内容。</p></div>
      <a-button type="primary" :disabled="!createDecision.allowed" @click="formVisible = true"><template #icon><Plus :size="15" /></template>创建 FFmpeg 源</a-button>
    </header>
    <ZLMNodeContextBar :nodes="contextNodes" :query-node-id="route.query.nodeId" :loading="nodesLoading || loading" :disabled="paused" @refresh="() => { loadNodes(); refresh(); }" />

    <div class="capability-banner" :data-capability="capability" role="status">
      <Workflow :size="17" />
      <strong>{{ capability === 'supported' ? '节点支持' : capability === 'unsupported' ? '节点不支持' : '能力未探测' }}</strong>
      <span>{{ createDecision.reason }}</span>
      <span v-if="capability === 'supported'">已登记模板 {{ templates.length }} 个。</span>
    </div>

    <section class="data-panel">
      <div v-if="loadError && !pageData" class="state-box state-box--error" role="alert"><ShieldAlert :size="28" /><strong>{{ errorPresentation.label }}</strong><a-button @click="refresh">重新加载</a-button></div>
      <a-table v-else :data="rows" :loading="loading" :pagination="false" row-key="key" class="uvp-data-table" :scroll="{ x: 980 }">
        <template #columns>
          <a-table-column title="任务标识" :width="220"><template #cell="{ record }"><strong>{{ record.key }}</strong><div class="subtle">节点 #{{ record.nodeId }}</div></template></a-table-column>
          <a-table-column title="模板" :width="170"><template #cell="{ record }"><code>{{ record.templateKey || '未知模板' }}</code></template></a-table-column>
          <a-table-column title="源摘要" :width="220"><template #cell="{ record }"><code>{{ ffmpegURLText(record.srcUrl) }}</code><div class="subtle">路径、认证和查询参数均不返回</div></template></a-table-column>
          <a-table-column title="目标摘要" :width="220"><template #cell="{ record }"><code>{{ ffmpegURLText(record.dstUrl) }}</code><div class="subtle">fingerprint 仅用于后端识别</div></template></a-table-column>
          <a-table-column title="运行来源" :width="130"><template #cell="{ record }"><a-tag :color="record.managed ? 'blue' : 'orange'">{{ record.managed ? '管理台' : '业务/未知' }}</a-tag><div class="subtle">{{ record.managed ? `用户 #${record.createdBy || '—'}` : '普通删除受保护' }}</div></template></a-table-column>
          <a-table-column title="状态" :width="130"><template #cell><a-tag color="green">ZLM 已列出</a-tag><div class="subtle">底层接口不提供重试计数</div></template></a-table-column>
          <a-table-column title="操作" fixed="right" :width="110"><template #cell="{ record }"><a-button size="small" status="danger" :disabled="!canManage || capability !== 'supported' || !record.managed" @click="openDelete(record)">删除</a-button></template></a-table-column>
        </template>
      </a-table>
      <div class="table-footer"><span v-if="pageData?.truncated">结果已被后端有界截断。</span><a-pagination v-if="pageData" :current="pageData.page" :page-size="pageData.pageSize" :total="pageData.total" show-page-size @change="changePage" @page-size-change="changePageSize" /></div>
    </section>

    <FFmpegSourceForm v-model:visible="formVisible" :capability="capability" :templates="templates" :permitted="canManage" :loading="saving" @submit="createSource" />
    <ZLMDangerActionDialog
      v-if="deleteTarget && deletePreview && deleteAllowed"
      v-model:visible="deleteVisible"
      :node-id="deleteTarget.nodeId"
      :node-name="selectedNodeName"
      :target-key="deleteTarget.key"
      :target-label="`FFmpeg ${deleteTarget.key}`"
      :fingerprint="deletePreview.fingerprint"
      :impacts="deleteImpacts"
      :confirm-phrase="`删除 FFmpeg ${deleteTarget.key}`"
      :require-reason="false"
      action-label="确认删除"
      :busy="deleting"
      @confirm="confirmDelete"
      @stale="closeDelete"
    />
    <a-modal v-else :visible="deleteVisible" :footer="false" :width="540" unmount-on-close @cancel="closeDelete">
      <template #title>FFmpeg 删除预检</template>
      <div class="state-box" :class="{ 'state-box--error': deleteError }"><a-spin v-if="deleteLoading" /><strong>{{ deleteLoading ? '正在读取后端真实状态与归属…' : deleteReason }}</strong><a-button v-if="!deleteLoading" @click="closeDelete">关闭</a-button></div>
    </a-modal>
  </div></div>
</template>

<style scoped>
.ingress-page { height: 100%; overflow: auto; box-sizing: border-box; padding: 4px 8px 24px; color: var(--zlm-text-2); }.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
h1 { margin: 0; color: var(--zlm-text-1); font-size: 20px; } p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.capability-banner { display: flex; align-items: center; gap: 9px; margin: 12px 0; padding: 10px 12px; font-size: var(--zlm-fs-caption); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.capability-banner[data-capability="unknown"] { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.capability-banner[data-capability="unsupported"] { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.data-panel { padding: 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.subtle { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); } code { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); overflow-wrap: anywhere; }
.table-footer { display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 38px; padding-top: 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); }.state-box { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; }.state-box--error { color: var(--zlm-danger-600); }
@media (max-width: 760px) { .page-header, .capability-banner, .table-footer { align-items: flex-start; flex-direction: column; } }
</style>
