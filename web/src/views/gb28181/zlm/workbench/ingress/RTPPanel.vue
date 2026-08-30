<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Plus, RadioTower, RefreshCw, ShieldAlert } from "lucide-vue-next";

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
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";

import RTPServerForm from "../../RTPServerForm.vue";
import ZLMDangerActionDialog from "../../components/ZLMDangerActionDialog.vue";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import { ingressCapabilityFromError } from "../../proxyManagementState";
import { rtpCloseDecision } from "../../rtpServicesState";
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
const pageData = ref<ZLMRTPServerPage | null>(null);
const observedCapability = ref<ZLMCapabilityState>("unknown");
const page = ref(1);
const pageSize = ref(20);
const loading = ref(false);
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

const rows = computed(() => pageData.value?.list ?? []);
const capability = computed<ZLMCapabilityState>(() => pageData.value?.capability ?? observedCapability.value);
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:rtp:manage"));
const canForce = computed(() => hasPermission("gb28181:zlm:rtp:force-close"));
const paused = computed(() => formVisible.value || closeVisible.value);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const initialCloseDecision = computed(() => closeTarget.value
  ? rtpCloseDecision(closeTarget.value, capability.value, closeForce.value ? canForce.value : canManage.value, closeForce.value)
  : null);
const preflightCloseAllowed = computed(() => {
  const snapshot = closePreview.value?.snapshot;
  if (nodeId.value === null || closeTarget.value?.nodeId !== nodeId.value || !snapshot?.presenceKnown || !snapshot.present) return false;
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

const { refresh } = useZLMRuntimePolling<ZLMRTPServerPage>({
  nodeId,
  active: canPoll,
  paused,
  intervalMs: 8_000,
  async load(currentNodeId, signal) {
    if (props.scope === "all") throw new Error("全部节点范围不执行节点级 RTP 查询");
    loading.value = true;
    const response = await listZLMRTPServers(currentNodeId, { page: page.value, pageSize: pageSize.value }, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "RTP 服务列表加载失败");
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
  closeDialog();
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
  if (!canManage.value || capability.value !== "supported") return;
  formVisible.value = true;
}

async function createServer(request: ZLMRTPServerCreateRequest) {
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  const currentNodeId = nodeId.value;
  if (capability.value !== "supported" || !canManage.value) return;
  saving.value = true;
  try {
    const response = await createZLMRTPServer(currentNodeId, request);
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
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  if (target.nodeId !== nodeId.value) return;
  const decision = rtpCloseDecision(target, capability.value, force ? canForce.value : canManage.value, force);
  if (!decision.allowed) return;
  closeTarget.value = { ...target };
  closeForce.value = force;
  closePreview.value = null;
  closeError.value = null;
  closeVisible.value = true;
  closeLoading.value = true;
  try {
    const response = await preflightCloseZLMRTPServer(nodeId.value, closeRequest(target));
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
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  const target = closeTarget.value;
  if (!target || target.nodeId !== nodeId.value || !preflightCloseAllowed.value) return;
  closing.value = true;
  try {
    const request = closeRequest(target);
    const response = closeForce.value
      ? await forceCloseZLMRTPServer(nodeId.value, request, payload.reason)
      : await closeZLMRTPServer(nodeId.value, request);
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
function changePage(next: number) { page.value = next; if (canPoll.value) refresh(); }
function changePageSize(next: number) { pageSize.value = next; page.value = 1; if (canPoll.value) refresh(); }
</script>

<template>
  <section class="ingress-panel" aria-label="RTP 服务管理">
    <header class="panel-toolbar">
      <div><h2>RTP 服务</h2><p>端口 0 由节点自动分配；普通关闭与强制关闭沿用后端归属边界。</p></div>
      <a-space>
        <a-button :loading="loading" :disabled="!canPoll" @click="refresh"><template #icon><RefreshCw :size="15" /></template>刷新</a-button>
        <a-button type="primary" :disabled="!props.active || !canManage || capability !== 'supported' || scopeBlocked" @click="openCreate"><template #icon><Plus :size="15" /></template>创建服务</a-button>
      </a-space>
    </header>
    <div v-if="scopeBlocked" class="scope-warning" role="status"><strong>全部节点范围</strong><span>RTP 服务列表和写操作需要先选择一个具体节点。</span></div>
    <div class="capability-banner" :data-capability="capability" role="status"><RadioTower :size="17" /><strong>{{ capability === 'supported' ? '节点支持' : capability === 'unsupported' ? '节点不支持' : '能力未探测' }}</strong><span>{{ capability === 'supported' ? 'RTP list/open/close 均由节点明确报告可用。' : capability === 'unsupported' ? '节点缺少完整 RTP 管理 API，操作保持禁用。' : '不会把空列表或探测失败当作支持。' }}</span></div>

    <section class="data-panel">
      <div v-if="loadError && !pageData" class="state-box state-box--error" role="alert"><ShieldAlert :size="28" /><strong>{{ errorPresentation.label }}</strong><a-button @click="refresh">重新加载</a-button></div>
      <div v-else-if="scopeBlocked" class="state-box" role="status"><strong>请选择具体节点</strong><span>全部节点模式不会发起逐节点 RTP 查询。</span></div>
      <a-table v-else :data="rows" :loading="loading" :pagination="false" row-key="key" class="uvp-data-table" :scroll="{ x: 1120 }">
        <template #columns>
          <a-table-column title="媒体身份" :width="250"><template #cell="{ record }"><strong>{{ record.app }}/{{ record.stream }}</strong><div class="subtle">{{ record.vhost }} · {{ record.key }}</div></template></a-table-column>
          <a-table-column title="实际端口" :width="130"><template #cell="{ record }"><strong class="port">{{ record.port }}</strong><div class="subtle">{{ record.released ? '已释放' : '监听中' }}</div></template></a-table-column>
          <a-table-column title="传输" :width="150"><template #cell="{ record }">{{ tcpModeText(record.tcpMode) }}<div class="subtle">{{ trackText(record.onlyTrack) }}</div></template></a-table-column>
          <a-table-column title="SSRC" :width="170"><template #cell="{ record }"><code>{{ record.ssrc || '—' }}</code></template></a-table-column>
          <a-table-column title="Ownership" :width="170"><template #cell="{ record }"><a-tag :color="record.managed ? 'blue' : 'orange'">{{ record.managed ? '管理台创建' : '业务/未知' }}</a-tag><div class="subtle">{{ record.managed ? `用户 #${record.createdBy || '—'}` : '普通关闭不可用' }}</div></template></a-table-column>
          <a-table-column title="操作" fixed="right" :width="190"><template #cell="{ record }"><a-space><a-button size="small" status="danger" :disabled="!props.active || !canManage || capability !== 'supported' || !record.managed || record.released || scopeBlocked" @click="openClose(record, false)">关闭</a-button><a-button v-if="canForce" size="small" status="danger" type="outline" :disabled="!props.active || capability !== 'supported' || record.released || scopeBlocked" @click="openClose(record, true)">强制</a-button></a-space></template></a-table-column>
        </template>
      </a-table>
      <div class="table-footer"><span v-if="pageData?.truncated">结果已被后端有界截断。</span><a-pagination v-if="pageData" :current="pageData.page" :page-size="pageData.pageSize" :total="pageData.total" show-page-size @change="changePage" @page-size-change="changePageSize" /></div>
    </section>

    <RTPServerForm v-model:visible="formVisible" :capability="capability" :permitted="props.active && canManage && !scopeBlocked" :loading="saving" @submit="createServer" />
    <ZLMDangerActionDialog
      v-if="props.active && closeTarget && closePreview && preflightCloseAllowed"
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
  </section>
</template>

<style scoped>
.ingress-panel { display: flex; min-width: 0; flex-direction: column; gap: 12px; padding-bottom: 12px; color: var(--zlm-text-2); }.panel-toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }.panel-toolbar h2 { margin: 0; color: var(--zlm-text-1); font-size: 18px; }.panel-toolbar p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.scope-warning, .capability-banner { display: flex; align-items: center; gap: 9px; padding: 10px 12px; font-size: var(--zlm-fs-caption); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.scope-warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.capability-banner[data-capability="unknown"] { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.capability-banner[data-capability="unsupported"] { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.data-panel { padding: 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.subtle { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.port { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); font-size: 16px; } code { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); }.table-footer { display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 38px; padding-top: 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); }.state-box { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; }.state-box--error { color: var(--zlm-danger-600); }
@media (max-width: 760px) { .panel-toolbar { align-items: flex-start; flex-direction: column; }.scope-warning, .capability-banner, .table-footer { align-items: flex-start; flex-direction: column; } }
</style>
