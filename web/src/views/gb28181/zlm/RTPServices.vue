<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Plus, RadioTower, ShieldAlert } from "lucide-vue-next";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import {
  closeZLMRTPServer,
  createZLMRTPServer,
  forceCloseZLMRTPServer,
  listZLMRTPServers,
  preflightCloseZLMRTPServer,
  type ZLMCapabilityState,
  type ZLMRTPServer,
  type ZLMRTPServerCloseRequest,
  type ZLMRTPServerCreateRequest,
  type ZLMRTPServerPage
} from "@/api/gb28181-zlm-ingress";
import type { ZLMOwnershipPreflight } from "@/api/gb28181-zlm-runtime";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import RTPServerForm from "./RTPServerForm.vue";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import { zlmErrorPresentation } from "./components/zlmFormatters";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import { rtpCloseDecision } from "./rtpServicesState";
import { ingressCapabilityFromError } from "./proxyManagementState";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref<unknown>(null);
const pageData = ref<ZLMRTPServerPage | null>(null);
const observedCapability = ref<ZLMCapabilityState>("unknown");
const page = ref(1);
const pageSize = ref(20);
const loading = ref(true);
const loadError = ref<unknown>(null);
const formVisible = ref(false);
const saving = ref(false);
const closeVisible = ref(false);
const closeTarget = ref<ZLMRTPServer | null>(null);
const closePreview = ref<ZLMOwnershipPreflight | null>(null);
const closeForce = ref(false);
const closeLoading = ref(false);
const closeError = ref<unknown>(null);
const closing = ref(false);

const selectedNodeId = computed(() => context.selectedNodeId);
const selectedNodeName = computed(() => context.selectedNode?.name ?? `节点 #${selectedNodeId.value ?? "—"}`);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const rows = computed(() => pageData.value?.list ?? []);
const capability = computed<ZLMCapabilityState>(() => pageData.value?.capability ?? observedCapability.value);
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:rtp:manage"));
const canForce = computed(() => hasPermission("gb28181:zlm:rtp:force-close"));
const paused = computed(() => formVisible.value || closeVisible.value);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value ?? nodesError.value));
const initialCloseDecision = computed(() => closeTarget.value
  ? rtpCloseDecision(closeTarget.value, capability.value, closeForce.value ? canForce.value : canManage.value, closeForce.value)
  : null);
const preflightCloseAllowed = computed(() => {
  const snapshot = closePreview.value?.snapshot;
  if (!snapshot?.presenceKnown || !snapshot.present) return false;
  return closeForce.value ? canForce.value : snapshot.status === "managed" && canManage.value;
});
const closeReason = computed(() => {
  if (closeError.value) return zlmErrorPresentation(closeError.value).label;
  const snapshot = closePreview.value?.snapshot;
  if (!snapshot) return initialCloseDecision.value?.reason || "正在等待后端预检。";
  if (!snapshot.presenceKnown) return "后端无法确认资源是否存在，操作保持禁用。";
  if (!snapshot.present) return "后端回读确认 RTP 服务已释放。";
  if (closeForce.value) return canForce.value ? "强制关闭将中断所有持有者，必须填写理由。" : "缺少 RTP 强制关闭权限。";
  if (snapshot.status !== "managed") return "业务持有或归属未知，普通关闭已保护。";
  return "后端确认该 RTP 服务由管理台创建，可执行普通关闭。";
});
const closeImpacts = computed(() => closePreview.value?.snapshot.impacts?.map(impact =>
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

const { refresh } = useZLMRuntimePolling<ZLMRTPServerPage>({
  nodeId: selectedNodeId,
  paused,
  intervalMs: 8_000,
  async load(nodeId, signal) {
    loading.value = true;
    const response = await listZLMRTPServers(nodeId, { page: page.value, pageSize: pageSize.value }, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "RTP 服务列表加载失败");
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
  closeDialog();
  loading.value = nodeId !== null;
  if (nodeId && previous !== null && route.query.nodeId !== String(nodeId)) {
    void router.replace({ query: { ...route.query, nodeId: String(nodeId) } });
  }
});

async function createServer(request: ZLMRTPServerCreateRequest) {
  const nodeId = selectedNodeId.value;
  if (!nodeId || capability.value !== "supported" || !canManage.value) return;
  saving.value = true;
  try {
    const response = await createZLMRTPServer(nodeId, request);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "RTP 服务创建失败");
    formVisible.value = false;
    Message.success(`RTP 服务已创建，实际端口 ${response.data.port}`);
    refresh();
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    saving.value = false;
  }
}

function closeRequest(target: ZLMRTPServer): ZLMRTPServerCloseRequest {
  return { nodeId: target.nodeId, vhost: target.vhost, app: target.app, stream: target.stream };
}

async function openClose(target: ZLMRTPServer, force: boolean) {
  const decision = rtpCloseDecision(target, capability.value, force ? canForce.value : canManage.value, force);
  if (!decision.allowed) return;
  closeTarget.value = { ...target };
  closeForce.value = force;
  closePreview.value = null;
  closeError.value = null;
  closeVisible.value = true;
  closeLoading.value = true;
  try {
    const response = await preflightCloseZLMRTPServer(target.nodeId, closeRequest(target));
    if (response.code !== 0 || !response.data) throw new Error(response.message || "RTP 关闭预检失败");
    closePreview.value = response.data;
  } catch (error) {
    closeError.value = error;
  } finally {
    closeLoading.value = false;
  }
}

function closeDialog() {
  if (closing.value) return;
  closeVisible.value = false;
  closeTarget.value = null;
  closePreview.value = null;
  closeError.value = null;
  closeForce.value = false;
}

async function confirmClose(payload: { reason: string }) {
  const target = closeTarget.value;
  if (!target || !preflightCloseAllowed.value) return;
  closing.value = true;
  try {
    const request = closeRequest(target);
    const response = closeForce.value
      ? await forceCloseZLMRTPServer(target.nodeId, request, payload.reason)
      : await closeZLMRTPServer(target.nodeId, request);
    if (response.code !== 0 || !response.data || (!response.data.released && !response.data.alreadyReleased)) {
      throw new Error(response.message || "后端未确认 RTP 服务已释放");
    }
    closeVisible.value = false;
    Message.success(response.data.alreadyReleased ? "后端回读确认 RTP 服务已释放" : "RTP 服务已关闭");
    refresh();
  } catch (error) {
    closeError.value = error;
  } finally {
    closing.value = false;
  }
}

function tcpModeText(value: number) { return value === 1 ? "TCP 被动" : value === 2 ? "TCP 主动" : "UDP"; }
function trackText(value: number) { return value === 1 ? "仅音频" : value === 2 ? "仅视频" : "自动"; }
function changePage(next: number) { page.value = next; refresh(); }
function changePageSize(next: number) { pageSize.value = next; page.value = 1; refresh(); }

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill"><div class="snow-fill-inner uvp-page-shell-flat ingress-page">
    <header class="page-header">
      <div><h1>RTP 服务</h1><p>端口 0 由节点自动分配；业务持有与管理台资源使用不同关闭边界。</p></div>
      <a-button type="primary" :disabled="!canManage || capability !== 'supported'" @click="formVisible = true"><template #icon><Plus :size="15" /></template>创建 RTP 服务</a-button>
    </header>
    <ZLMNodeContextBar :nodes="contextNodes" :query-node-id="route.query.nodeId" :loading="nodesLoading || loading" :disabled="paused" @refresh="() => { loadNodes(); refresh(); }" />
    <div class="capability-banner" :data-capability="capability" role="status"><RadioTower :size="17" /><strong>{{ capability === 'supported' ? '节点支持' : capability === 'unsupported' ? '节点不支持' : '能力未探测' }}</strong><span>{{ capability === 'supported' ? 'RTP list/open/close 均由节点明确报告可用。' : capability === 'unsupported' ? '节点缺少完整 RTP 管理 API，操作保持禁用。' : '不会把空列表或探测失败当作支持。' }}</span></div>

    <section class="data-panel">
      <div v-if="loadError && !pageData" class="state-box state-box--error" role="alert"><ShieldAlert :size="28" /><strong>{{ errorPresentation.label }}</strong><a-button @click="refresh">重新加载</a-button></div>
      <a-table v-else :data="rows" :loading="loading" :pagination="false" row-key="key" class="uvp-data-table" :scroll="{ x: 1120 }">
        <template #columns>
          <a-table-column title="媒体身份" :width="250"><template #cell="{ record }"><strong>{{ record.app }}/{{ record.stream }}</strong><div class="subtle">{{ record.vhost }} · {{ record.key }}</div></template></a-table-column>
          <a-table-column title="实际端口" :width="130"><template #cell="{ record }"><strong class="port">{{ record.port }}</strong><div class="subtle">{{ record.released ? '已释放' : '监听中' }}</div></template></a-table-column>
          <a-table-column title="传输" :width="150"><template #cell="{ record }">{{ tcpModeText(record.tcpMode) }}<div class="subtle">{{ trackText(record.onlyTrack) }}</div></template></a-table-column>
          <a-table-column title="SSRC" :width="170"><template #cell="{ record }"><code>{{ record.ssrc || '—' }}</code></template></a-table-column>
          <a-table-column title="Ownership" :width="170"><template #cell="{ record }"><a-tag :color="record.managed ? 'blue' : 'orange'">{{ record.managed ? '管理台创建' : '业务/未知' }}</a-tag><div class="subtle">{{ record.managed ? `用户 #${record.createdBy || '—'}` : '普通关闭不可用' }}</div></template></a-table-column>
          <a-table-column title="操作" fixed="right" :width="190"><template #cell="{ record }"><a-space><a-button size="small" status="danger" :disabled="!canManage || capability !== 'supported' || !record.managed || record.released" @click="openClose(record, false)">关闭</a-button><a-button v-if="canForce" size="small" status="danger" type="outline" :disabled="capability !== 'supported' || record.released" @click="openClose(record, true)">强制</a-button></a-space></template></a-table-column>
        </template>
      </a-table>
      <div class="table-footer"><span v-if="pageData?.truncated">结果已被后端有界截断。</span><a-pagination v-if="pageData" :current="pageData.page" :page-size="pageData.pageSize" :total="pageData.total" show-page-size @change="changePage" @page-size-change="changePageSize" /></div>
    </section>

    <RTPServerForm v-model:visible="formVisible" :capability="capability" :permitted="canManage" :loading="saving" @submit="createServer" />
    <ZLMDangerActionDialog
      v-if="closeTarget && closePreview && preflightCloseAllowed"
      v-model:visible="closeVisible"
      :node-id="closeTarget.nodeId"
      :node-name="selectedNodeName"
      :target-key="closeTarget.key"
      :target-label="`${closeTarget.app}/${closeTarget.stream}:${closeTarget.port}`"
      :fingerprint="closePreview.fingerprint"
      :impacts="closeImpacts"
      :confirm-phrase="`${closeForce ? '强制关闭' : '关闭'} RTP ${closeTarget.stream}`"
      :require-reason="closeForce"
      :action-label="closeForce ? '强制关闭' : '确认关闭'"
      :busy="closing"
      @confirm="confirmClose"
      @stale="closeDialog"
    />
    <a-modal v-else :visible="closeVisible" :footer="false" :width="540" unmount-on-close @cancel="closeDialog">
      <template #title>RTP 关闭预检</template>
      <div class="state-box" :class="{ 'state-box--error': closeError }"><a-spin v-if="closeLoading" /><strong>{{ closeLoading ? '正在读取后端真实状态与业务持有…' : closeReason }}</strong><a-button v-if="!closeLoading" @click="closeDialog">关闭</a-button></div>
    </a-modal>
  </div></div>
</template>

<style scoped>
.ingress-page { height: 100%; overflow: auto; box-sizing: border-box; padding: 4px 8px 24px; color: var(--zlm-text-2); }.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
h1 { margin: 0; color: var(--zlm-text-1); font-size: 20px; } p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.capability-banner { display: flex; align-items: center; gap: 9px; margin: 12px 0; padding: 10px 12px; font-size: var(--zlm-fs-caption); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.capability-banner[data-capability="unknown"] { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.capability-banner[data-capability="unsupported"] { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.data-panel { padding: 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.subtle { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.port { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); font-size: 16px; } code { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); }
.table-footer { display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 38px; padding-top: 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); }.state-box { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; }.state-box--error { color: var(--zlm-danger-600); }
@media (max-width: 760px) { .page-header, .capability-banner, .table-footer { align-items: flex-start; flex-direction: column; } }
</style>
