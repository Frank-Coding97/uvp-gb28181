<script setup lang="ts">
import { computed, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { ArrowDownToLine, ArrowUpFromLine, Plus, RefreshCw, ShieldAlert } from "lucide-vue-next";

import {
  createZLMPullProxy,
  createZLMPushProxy,
  deleteZLMPullProxy,
  deleteZLMPushProxy,
  listZLMPullProxies,
  listZLMPushProxies,
  preflightDeleteZLMPullProxy,
  preflightDeleteZLMPushProxy,
  type ZLMCapabilityState,
  type ZLMPullProxyCreateRequest,
  type ZLMProxy,
  type ZLMProxyDeletePreflight,
  type ZLMProxyDeleteRequest,
  type ZLMProxyPage,
  type ZLMPushProxyCreateRequest
} from "@/api/gb28181-zlm-ingress";
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import { useUserStoreHook } from "@/store/modules/user";

import ProxyForm from "../../ProxyForm.vue";
import ZLMDangerActionDialog from "../../components/ZLMDangerActionDialog.vue";
import { formatZLMByteRate, formatZLMDuration, zlmErrorPresentation } from "../../components/zlmFormatters";
import { useZLMRuntimePolling } from "../../composables/useZLMRuntimePolling";
import {
  ingressCapabilityFromError,
  proxyAddressText,
  proxyCapabilityPresentation,
  proxyDeleteDecision,
  type ProxyTab
} from "../../proxyManagementState";
import { boundedPageRows } from "../boundedData";

type PollPayload = { kind: ProxyTab; data: ZLMProxyPage };

const props = withDefaults(defineProps<{
  active?: boolean;
  scope?: MediaScope;
  nodes?: readonly MediaNodeCatalogNode[];
  kind?: ProxyTab;
  showTabs?: boolean;
  refreshKey?: number;
}>(), {
  active: true,
  scope: "all",
  nodes: () => [],
  kind: "pull",
  showTabs: false,
  refreshKey: 0
});

const userStore = useUserStoreHook();
const activeTab = ref<ProxyTab>(props.kind);
const pages = reactive({ pull: { page: 1, pageSize: 20 }, push: { page: 1, pageSize: 20 } });
const data = reactive<{ pull: ZLMProxyPage | null; push: ZLMProxyPage | null }>({ pull: null, push: null });
const observedCapability = reactive<Record<ProxyTab, ZLMCapabilityState>>({ pull: "unknown", push: "unknown" });
const loading = ref(false);
const loadError = ref<unknown>(null);
const formVisible = ref(false);
const saving = ref(false);
const deleteVisible = ref(false);
const deleteTarget = ref<ZLMProxy | null>(null);
const deletePreview = ref<ZLMProxyDeletePreflight | null>(null);
const deleteLoading = ref(false);
const deleteError = ref<unknown>(null);
const deleting = ref(false);

const nodeId = computed<number | null>(() => props.scope === "all" ? null : props.scope);
const canPoll = computed(() => props.active && props.scope !== "all");
const currentKind = computed<ProxyTab>(() => props.showTabs ? activeTab.value : props.kind);
const currentData = computed(() => data[currentKind.value]);
const rows = computed(() => currentData.value?.list ?? []);
const capability = computed<ZLMCapabilityState>(() => currentData.value?.capability ?? observedCapability[currentKind.value]);
const capabilityView = computed(() => proxyCapabilityPresentation(capability.value));
const scopeBlocked = computed(() => props.scope === "all");
const selectedNodeName = computed(() => props.nodes.find(node => node.id === nodeId.value)?.name ?? `节点 #${nodeId.value ?? "—"}`);
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:proxy:manage"));
const paused = computed(() => formVisible.value || deleteVisible.value);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const deletionDecision = computed(() => deletePreview.value ? proxyDeleteDecision(deletePreview.value) : null);
const deleteImpacts = computed(() => deletePreview.value?.impacts?.map(impact =>
  impact.reason || impact.owner || [impact.resourceType, impact.resourceKey].filter(Boolean).join(" / ")
).filter(Boolean) as string[] ?? []);

const { refresh } = useZLMRuntimePolling<PollPayload>({
  nodeId,
  active: canPoll,
  paused,
  intervalMs: 8_000,
  async load(currentNodeId, signal) {
    if (props.scope === "all") throw new Error("全部节点范围不执行节点级代理查询");
    loading.value = true;
    const kind = currentKind.value;
    const paging = pages[kind];
    const response = kind === "pull"
      ? await listZLMPullProxies(currentNodeId, paging, signal)
      : await listZLMPushProxies(currentNodeId, paging, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "代理列表加载失败");
    return { kind, data: response.data };
  },
  publish(payload) {
    if (payload.kind === currentKind.value) {
      data[payload.kind] = { ...payload.data, list: boundedPageRows(payload.data.list, pages[payload.kind].pageSize) };
    }
    observedCapability[payload.kind] = payload.data.capability;
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    observedCapability[currentKind.value] = ingressCapabilityFromError(error);
    loadError.value = error;
    loading.value = false;
  }
});

watch(currentKind, () => {
  closeDelete();
  loadError.value = null;
  if (canPoll.value) refresh();
});

watch([nodeId, () => props.active], ([nextNodeId, nextActive]) => {
  data.pull = null;
  data.push = null;
  observedCapability.pull = "unknown";
  observedCapability.push = "unknown";
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
  if (!canManage.value || !capabilityView.value.actionable) return;
  formVisible.value = true;
}

async function createProxy(request: ZLMPullProxyCreateRequest | ZLMPushProxyCreateRequest) {
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  const currentNodeId = nodeId.value;
  saving.value = true;
  try {
    const response = currentKind.value === "pull"
      ? await createZLMPullProxy(currentNodeId, request as ZLMPullProxyCreateRequest)
      : await createZLMPushProxy(currentNodeId, request as ZLMPushProxyCreateRequest);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "代理创建失败");
    formVisible.value = false;
    Message.success(`代理 ${response.data.key} 已由后端确认创建`);
    refresh();
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    saving.value = false;
  }
}

function deleteRequest(target: ZLMProxy, fingerprint?: string): ZLMProxyDeleteRequest {
  return { nodeId: target.nodeId, key: target.key, media: { ...target.media }, ...(fingerprint ? { fingerprint } : {}) };
}

async function openDelete(target: ZLMProxy) {
  if (!props.active || props.scope === "all" || nodeId.value === null) return warnNodeScope();
  if (target.nodeId !== nodeId.value || !canManage.value || capability.value !== "supported") return;
  deleteTarget.value = { ...target, media: { ...target.media } };
  deletePreview.value = null;
  deleteError.value = null;
  deleteVisible.value = true;
  deleteLoading.value = true;
  try {
    const request = deleteRequest(target, target.provenanceFingerprint);
    const response = target.kind === "pull_proxy"
      ? await preflightDeleteZLMPullProxy(nodeId.value, target.key, request)
      : await preflightDeleteZLMPushProxy(nodeId.value, target.key, request);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "代理删除预检失败");
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
  const preview = deletePreview.value;
  if (!target || target.nodeId !== nodeId.value || !preview || !deletionDecision.value?.allowed || payload.fingerprint !== preview.fingerprint) return;
  deleting.value = true;
  try {
    const request = deleteRequest(target, preview.fingerprint);
    const response = target.kind === "pull_proxy"
      ? await deleteZLMPullProxy(nodeId.value, target.key, request)
      : await deleteZLMPushProxy(nodeId.value, target.key, request);
    if (response.code !== 0 || !response.data || (!response.data.removed && !response.data.alreadyAbsent)) {
      throw new Error(response.message || "后端未确认代理已删除");
    }
    deleteVisible.value = false;
    Message.success(response.data.alreadyAbsent ? "后端回读确认代理已不存在" : "代理已删除并完成账本回读");
    refresh();
  } catch (error) {
    deleteError.value = error;
  } finally {
    deleting.value = false;
  }
}

function changePage(page: number) {
  pages[currentKind.value].page = page;
  if (canPoll.value) refresh();
}

function changePageSize(pageSize: number) {
  pages[currentKind.value].pageSize = pageSize;
  pages[currentKind.value].page = 1;
  if (canPoll.value) refresh();
}
</script>

<template>
  <section class="ingress-panel" aria-label="代理接入管理">
    <header class="panel-toolbar">
      <div><h2>{{ showTabs ? "拉流 / 推流代理" : (kind === "pull" ? "拉流代理" : "推流代理") }}</h2><p>请求全部经 UVP 后端；地址只展示安全摘要，完整 URL 不回显。</p></div>
      <a-space>
        <a-button :loading="loading" :disabled="!canPoll" @click="refresh"><template #icon><RefreshCw :size="15" /></template>刷新</a-button>
        <a-button type="primary" :disabled="!props.active || !canManage || !capabilityView.actionable || scopeBlocked" @click="openCreate"><template #icon><Plus :size="15" /></template>创建代理</a-button>
      </a-space>
    </header>

    <div v-if="scopeBlocked" class="scope-warning" role="status">
      <strong>全部节点范围</strong><span>代理列表和所有节点级写操作都需要先选择一个具体节点。</span>
    </div>
    <div class="capability-banner" :data-tone="capabilityView.tone" role="status">
      <strong>{{ capabilityView.label }}</strong>
      <span v-if="capability === 'supported'">当前节点明确报告代理管理 API 可用。</span>
      <span v-else-if="capability === 'unsupported'">当前节点缺少该类代理完整 API，创建和删除均禁用。</span>
      <span v-else>能力探测未完成；不会用一次失败或空列表猜测节点支持。</span>
    </div>

    <a-tabs v-if="showTabs" v-model:active-key="activeTab" class="proxy-tabs">
      <a-tab-pane key="pull" title="拉流代理"><template #title><span class="tab-title"><ArrowDownToLine :size="15" />拉流代理</span></template></a-tab-pane>
      <a-tab-pane key="push" title="推流代理"><template #title><span class="tab-title"><ArrowUpFromLine :size="15" />推流代理</span></template></a-tab-pane>
    </a-tabs>

    <section class="data-panel">
      <div v-if="loadError && !currentData" class="state-box state-box--error" role="alert"><ShieldAlert :size="28" /><strong>{{ errorPresentation.label }}</strong><a-button @click="refresh">重新加载</a-button></div>
      <div v-else-if="scopeBlocked" class="state-box" role="status"><strong>请选择具体节点</strong><span>全部节点模式不会发起逐节点代理查询。</span></div>
      <a-table v-else :data="rows" :loading="loading" :pagination="false" row-key="key" class="uvp-data-table" :scroll="{ x: 1180 }">
        <template #columns>
          <a-table-column title="媒体身份" :width="250"><template #cell="{ record }"><strong>{{ record.media.app }}/{{ record.media.stream }}</strong><div class="subtle">{{ record.media.schema }} · {{ record.media.vhost }}</div></template></a-table-column>
          <a-table-column title="地址摘要" :width="250"><template #cell="{ record }"><code>{{ proxyAddressText(record.source || record.target) }}</code><div class="subtle">{{ (record.source || record.target)?.hasUserInfo || (record.source || record.target)?.hasSensitiveQuery ? '认证信息已隐藏' : '不返回路径与查询参数' }}</div></template></a-table-column>
          <a-table-column title="真实状态" :width="150"><template #cell="{ record }"><a-tag :color="record.online ? 'green' : 'red'">{{ record.online ? '在线' : '失败/离线' }}</a-tag><div class="subtle">{{ record.statusText || `code ${record.status}` }}</div></template></a-table-column>
          <a-table-column title="运行信息" :width="170"><template #cell="{ record }"><div>{{ formatZLMDuration(record.liveSecs) }}</div><div class="subtle">{{ formatZLMByteRate(record.bytesSpeed) }} · {{ record.totalReaderCount }} 读者</div></template></a-table-column>
          <a-table-column title="重试" :width="130"><template #cell="{ record }">拉 {{ record.rePullCount }} / 推 {{ record.rePublishCount }}</template></a-table-column>
          <a-table-column title="来源" :width="120"><template #cell="{ record }"><a-tag :color="record.managed ? 'blue' : 'orange'">{{ record.managed ? '管理台' : '业务/未知' }}</a-tag></template></a-table-column>
          <a-table-column title="操作" fixed="right" :width="120"><template #cell="{ record }"><a-button size="small" status="danger" :disabled="!props.active || !canManage || capability !== 'supported' || !record.managed || scopeBlocked" @click="openDelete(record)">删除</a-button></template></a-table-column>
        </template>
      </a-table>
      <div class="table-footer"><span v-if="currentData?.truncated">节点返回内容已截断，请缩小范围。</span><a-pagination v-if="currentData" :current="currentData.page" :page-size="currentData.pageSize" :total="currentData.total" show-page-size @change="changePage" @page-size-change="changePageSize" /></div>
    </section>

    <ProxyForm v-model:visible="formVisible" :kind="currentKind" :capability="capability" :permitted="props.active && canManage && !scopeBlocked" :loading="saving" @submit="createProxy" />
    <ZLMDangerActionDialog
      v-if="props.active && deletePreview && deletionDecision?.allowed && deleteTarget && !scopeBlocked"
      v-model:visible="deleteVisible"
      :node-id="deleteTarget.nodeId"
      :node-name="selectedNodeName"
      :target-key="deleteTarget.key"
      :target-label="`${deleteTarget.media.app}/${deleteTarget.media.stream}`"
      :fingerprint="deletePreview.fingerprint"
      :impacts="deleteImpacts"
      :confirm-phrase="`删除代理 ${deleteTarget.key}`"
      :require-reason="false"
      action-label="确认删除"
      :busy="deleting"
      @confirm="confirmDelete"
      @stale="closeDelete"
    />
    <a-modal v-else :visible="deleteVisible" :footer="false" :width="540" unmount-on-close @cancel="closeDelete">
      <template #title>代理删除预检</template>
      <div v-if="deleteLoading" class="preflight-state"><a-spin /><span>正在读取后端真实状态与归属…</span></div>
      <div v-else class="preflight-state" :class="{ 'state-box--error': deleteError }"><strong>{{ deleteError ? zlmErrorPresentation(deleteError).label : deletionDecision?.reason }}</strong><a-button @click="closeDelete">关闭</a-button></div>
    </a-modal>
  </section>
</template>

<style scoped>
.ingress-panel { display: flex; min-width: 0; flex-direction: column; gap: 12px; padding-bottom: 12px; color: var(--zlm-text-2); }
.panel-toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }.panel-toolbar h2 { margin: 0; color: var(--zlm-text-1); font-size: 18px; }.panel-toolbar p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.scope-warning, .capability-banner { display: flex; align-items: center; gap: 9px; padding: 10px 12px; font-size: var(--zlm-fs-caption); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.scope-warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.capability-banner[data-tone="warning"] { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.capability-banner[data-tone="danger"] { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.proxy-tabs { margin-top: -2px; }.tab-title { display: inline-flex; align-items: center; gap: 6px; }.data-panel { padding: 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }.subtle { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); } code { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); overflow-wrap: anywhere; }
.table-footer { display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 38px; padding-top: 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); }.state-box, .preflight-state { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; }.state-box--error { color: var(--zlm-danger-600); }
@media (max-width: 760px) { .panel-toolbar { align-items: flex-start; flex-direction: column; }.scope-warning, .capability-banner, .table-footer { align-items: flex-start; flex-direction: column; } }
</style>
