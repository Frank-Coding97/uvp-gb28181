<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { ArrowDownToLine, ArrowUpFromLine, Plus, ShieldAlert } from "lucide-vue-next";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
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
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import ProxyForm from "./ProxyForm.vue";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import { formatZLMByteRate, formatZLMDuration, zlmErrorPresentation } from "./components/zlmFormatters";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import {
  proxyAddressText,
  proxyCapabilityPresentation,
  proxyDeleteDecision,
  ingressCapabilityFromError,
  type ProxyTab
} from "./proxyManagementState";

type PollPayload = { kind: ProxyTab; data: ZLMProxyPage };

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref<unknown>(null);
const activeTab = ref<ProxyTab>("pull");
const pages = reactive({ pull: { page: 1, pageSize: 20 }, push: { page: 1, pageSize: 20 } });
const data = reactive<{ pull: ZLMProxyPage | null; push: ZLMProxyPage | null }>({ pull: null, push: null });
const observedCapability = reactive<Record<ProxyTab, ZLMCapabilityState>>({ pull: "unknown", push: "unknown" });
const loading = ref(true);
const loadError = ref<unknown>(null);
const formVisible = ref(false);
const saving = ref(false);
const deleteVisible = ref(false);
const deleteTarget = ref<ZLMProxy | null>(null);
const deletePreview = ref<ZLMProxyDeletePreflight | null>(null);
const deleteLoading = ref(false);
const deleteError = ref<unknown>(null);
const deleting = ref(false);

const selectedNodeId = computed(() => context.selectedNodeId);
const selectedNodeName = computed(() => context.selectedNode?.name ?? `节点 #${selectedNodeId.value ?? "—"}`);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const currentData = computed(() => data[activeTab.value]);
const rows = computed(() => currentData.value?.list ?? []);
const capability = computed<ZLMCapabilityState>(() => currentData.value?.capability ?? observedCapability[activeTab.value]);
const capabilityView = computed(() => proxyCapabilityPresentation(capability.value));
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:proxy:manage"));
const paused = computed(() => formVisible.value || deleteVisible.value);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value ?? nodesError.value));
const deletionDecision = computed(() => deletePreview.value ? proxyDeleteDecision(deletePreview.value) : null);
const deleteImpacts = computed(() => deletePreview.value?.impacts?.map(impact =>
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

const { refresh } = useZLMRuntimePolling<PollPayload>({
  nodeId: selectedNodeId,
  paused,
  intervalMs: 8_000,
  async load(nodeId, signal) {
    loading.value = true;
    const kind = activeTab.value;
    const paging = pages[kind];
    const response = kind === "pull"
      ? await listZLMPullProxies(nodeId, paging, signal)
      : await listZLMPushProxies(nodeId, paging, signal);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "代理列表加载失败");
    return { kind, data: response.data };
  },
  publish(payload) {
    if (payload.kind === activeTab.value) data[payload.kind] = payload.data;
    observedCapability[payload.kind] = payload.data.capability;
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    observedCapability[activeTab.value] = ingressCapabilityFromError(error);
    loadError.value = error;
    loading.value = false;
  }
});

watch(activeTab, () => {
  closeDelete();
  loadError.value = null;
  refresh();
});

watch(selectedNodeId, (nodeId, previous) => {
  if (nodeId === previous) return;
  data.pull = null;
  data.push = null;
  observedCapability.pull = "unknown";
  observedCapability.push = "unknown";
  formVisible.value = false;
  closeDelete();
  loading.value = nodeId !== null;
  if (nodeId && previous !== null && route.query.nodeId !== String(nodeId)) {
    void router.replace({ query: { ...route.query, nodeId: String(nodeId) } });
  }
});

function openCreate() {
  if (!canManage.value || !capabilityView.value.actionable) return;
  formVisible.value = true;
}

async function createProxy(request: ZLMPullProxyCreateRequest | ZLMPushProxyCreateRequest) {
  const nodeId = selectedNodeId.value;
  if (!nodeId) return;
  saving.value = true;
  try {
    const response = activeTab.value === "pull"
      ? await createZLMPullProxy(nodeId, request as ZLMPullProxyCreateRequest)
      : await createZLMPushProxy(nodeId, request as ZLMPushProxyCreateRequest);
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
  if (!canManage.value || capability.value !== "supported") return;
  deleteTarget.value = { ...target, media: { ...target.media } };
  deletePreview.value = null;
  deleteError.value = null;
  deleteVisible.value = true;
  deleteLoading.value = true;
  try {
    const request = deleteRequest(target, target.provenanceFingerprint);
    const response = target.kind === "pull_proxy"
      ? await preflightDeleteZLMPullProxy(target.nodeId, target.key, request)
      : await preflightDeleteZLMPushProxy(target.nodeId, target.key, request);
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
  const target = deleteTarget.value;
  const preview = deletePreview.value;
  if (!target || !preview || !deletionDecision.value?.allowed || payload.fingerprint !== preview.fingerprint) return;
  deleting.value = true;
  try {
    const request = deleteRequest(target, preview.fingerprint);
    const response = target.kind === "pull_proxy"
      ? await deleteZLMPullProxy(target.nodeId, target.key, request)
      : await deleteZLMPushProxy(target.nodeId, target.key, request);
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
  pages[activeTab.value].page = page;
  refresh();
}

function changePageSize(pageSize: number) {
  pages[activeTab.value].pageSize = pageSize;
  pages[activeTab.value].page = 1;
  refresh();
}

onMounted(loadNodes);
</script>

<template>
  <div class="snow-fill"><div class="snow-fill-inner uvp-page-shell-flat ingress-page">
    <header class="page-header">
      <div><h1>拉流 / 推流代理</h1><p>管理请求全部经 UVP 后端；列表中的地址只保留协议、主机和端口摘要。</p></div>
      <a-button type="primary" :disabled="!canManage || !capabilityView.actionable" @click="openCreate"><template #icon><Plus :size="15" /></template>创建代理</a-button>
    </header>
    <ZLMNodeContextBar :nodes="contextNodes" :query-node-id="route.query.nodeId" :loading="nodesLoading || loading" :disabled="paused" @refresh="() => { loadNodes(); refresh(); }" />

    <div class="capability-banner" :data-tone="capabilityView.tone" role="status">
      <strong>{{ capabilityView.label }}</strong>
      <span v-if="capability === 'supported'">当前节点明确报告代理管理 API 可用。</span>
      <span v-else-if="capability === 'unsupported'">当前节点缺少该类代理完整 API，创建和删除均禁用。</span>
      <span v-else>能力探测未完成；不会用一次失败或空列表猜测节点支持。</span>
    </div>

    <a-tabs v-model:active-key="activeTab" class="proxy-tabs">
      <a-tab-pane key="pull" value="pull" title="拉流代理"><template #title><span class="tab-title"><ArrowDownToLine :size="15" />拉流代理</span></template></a-tab-pane>
      <a-tab-pane key="push" value="push" title="推流代理"><template #title><span class="tab-title"><ArrowUpFromLine :size="15" />推流代理</span></template></a-tab-pane>
    </a-tabs>

    <section class="data-panel">
      <div v-if="loadError && !currentData" class="state-box state-box--error" role="alert"><ShieldAlert :size="28" /><strong>{{ errorPresentation.label }}</strong><a-button @click="refresh">重新加载</a-button></div>
      <a-table v-else :data="rows" :loading="loading" :pagination="false" row-key="key" class="uvp-data-table" :scroll="{ x: 1180 }">
        <template #columns>
          <a-table-column title="媒体身份" :width="250"><template #cell="{ record }"><strong>{{ record.media.app }}/{{ record.media.stream }}</strong><div class="subtle">{{ record.media.schema }} · {{ record.media.vhost }}</div></template></a-table-column>
          <a-table-column title="地址摘要" :width="250"><template #cell="{ record }"><code>{{ proxyAddressText(record.source || record.target) }}</code><div class="subtle">{{ (record.source || record.target)?.hasUserInfo || (record.source || record.target)?.hasSensitiveQuery ? '认证信息已隐藏' : '不返回路径与查询参数' }}</div></template></a-table-column>
          <a-table-column title="真实状态" :width="150"><template #cell="{ record }"><a-tag :color="record.online ? 'green' : 'red'">{{ record.online ? '在线' : '失败/离线' }}</a-tag><div class="subtle">{{ record.statusText || `code ${record.status}` }}</div></template></a-table-column>
          <a-table-column title="运行信息" :width="170"><template #cell="{ record }"><div>{{ formatZLMDuration(record.liveSecs) }}</div><div class="subtle">{{ formatZLMByteRate(record.bytesSpeed) }} · {{ record.totalReaderCount }} 读者</div></template></a-table-column>
          <a-table-column title="重试" :width="130"><template #cell="{ record }">拉 {{ record.rePullCount }} / 推 {{ record.rePublishCount }}</template></a-table-column>
          <a-table-column title="来源" :width="120"><template #cell="{ record }"><a-tag :color="record.managed ? 'blue' : 'orange'">{{ record.managed ? '管理台' : '业务/未知' }}</a-tag></template></a-table-column>
          <a-table-column title="操作" fixed="right" :width="120"><template #cell="{ record }"><a-button size="small" status="danger" :disabled="!canManage || capability !== 'supported' || !record.managed" @click="openDelete(record)">删除</a-button></template></a-table-column>
        </template>
      </a-table>
      <div class="table-footer"><span v-if="currentData?.truncated">节点返回内容已截断，请缩小范围。</span><a-pagination v-if="currentData" :current="currentData.page" :page-size="currentData.pageSize" :total="currentData.total" show-page-size @change="changePage" @page-size-change="changePageSize" /></div>
    </section>

    <ProxyForm v-model:visible="formVisible" :kind="activeTab" :capability="capability" :permitted="canManage" :loading="saving" @submit="createProxy" />
    <ZLMDangerActionDialog
      v-if="deletePreview && deletionDecision?.allowed && deleteTarget"
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
  </div></div>
</template>

<style scoped>
.ingress-page { height: 100%; overflow: auto; box-sizing: border-box; padding: 4px 8px 24px; color: var(--zlm-text-2); }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }
h1 { margin: 0; color: var(--zlm-text-1); font-size: 20px; } p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.capability-banner { display: flex; gap: 10px; margin-top: 12px; padding: 10px 12px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
.capability-banner[data-tone="warning"] { color: var(--zlm-warn-600); border-color: var(--zlm-warn-500); background: var(--zlm-warn-50); }.capability-banner[data-tone="danger"] { color: var(--zlm-danger-600); border-color: var(--zlm-danger-500); background: var(--zlm-danger-50); }
.proxy-tabs { margin-top: 12px; }.tab-title { display: inline-flex; align-items: center; gap: 6px; }
.data-panel { padding: 14px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.subtle { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); } code { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); overflow-wrap: anywhere; }
.table-footer { display: flex; justify-content: space-between; align-items: center; gap: 12px; min-height: 38px; padding-top: 12px; color: var(--zlm-warn-600); font-size: var(--zlm-fs-caption); }
.state-box, .preflight-state { min-height: 180px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); text-align: center; }.state-box--error { color: var(--zlm-danger-600); }
@media (max-width: 760px) { .page-header { flex-direction: column; }.capability-banner, .table-footer { align-items: flex-start; flex-direction: column; } }
</style>
