<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { CirclePower, Pencil, PowerOff, Settings, Trash2 } from "lucide-vue-next";
import { useRouter } from "vue-router";

import {
  disableZLMNode,
  enableZLMNode,
  listZLMNodes,
  type ZLMNode
} from "@/api/gb28181-zlm";
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import NodeForm from "../../NodeForm.vue";
import ZLMNodeActionDialog from "../../ZLMNodeActionDialog.vue";
import LifecycleDot from "../../components/LifecycleDot.vue";
import HealthBadge from "../../components/HealthBadge.vue";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import type { NodeDangerAction } from "../../nodeActionState";
import {
  filterNodeRecords,
  nodeHealth,
  nodeHealthReason,
  nodeOnlineState,
  type NodeHealth
} from "./nodeManagementState";

const props = withDefaults(defineProps<{
  nodes?: readonly MediaNodeCatalogNode[];
  scope?: MediaScope;
  loading?: boolean;
  error?: unknown;
  active?: boolean;
  autoRefresh?: boolean;
  canonical?: boolean;
}>(), {
  nodes: undefined,
  scope: "all",
  loading: false,
  active: true,
  autoRefresh: true,
  canonical: true
});

const emit = defineEmits<{
  refresh: [];
}>();

const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const legacyNodes = ref<ZLMNode[]>([]);
const legacyLoading = ref(false);
const legacyError = ref<unknown>(null);
const search = ref("");
const filterState = ref<string>();
const filterEnabled = ref<boolean>();
const filterHealth = ref<NodeHealth>();
const appliedSearch = ref("");
const appliedFilterState = ref<string>();
const appliedFilterEnabled = ref<boolean>();
const appliedFilterHealth = ref<NodeHealth>();
const formVisible = ref(false);
const formNode = ref<ZLMNode | null>(null);
const actionVisible = ref(false);
const actionNode = ref<ZLMNode | null>(null);
const action = ref<NodeDangerAction | null>(null);
const opLoading = ref<Record<number, string | null>>({});
const AUTO_REFRESH_SECONDS = 10;
const refreshCountdown = ref(AUTO_REFRESH_SECONDS);
let requestGeneration = 0;
let refreshTimer: ReturnType<typeof setInterval> | null = null;

const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:node:manage"));
const sourceNodes = computed<readonly ZLMNode[]>(() => props.nodes === undefined ? legacyNodes.value : props.nodes as readonly ZLMNode[]);
const scopedNodes = computed(() => props.scope === "all"
  ? sourceNodes.value
  : sourceNodes.value.filter(node => node.id === props.scope));
const filteredNodes = computed(() => filterNodeRecords(scopedNodes.value, {
  keyword: appliedSearch.value,
  state: appliedFilterState.value as ZLMNode["state"] | undefined,
  enabled: appliedFilterEnabled.value,
  health: appliedFilterHealth.value
}));
const loading = computed(() => props.nodes === undefined ? legacyLoading.value : props.loading);
const loadError = computed(() => props.nodes === undefined ? legacyError.value : props.error);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const refreshButtonLabel = computed(() => props.active && props.autoRefresh
  ? `刷新（${refreshCountdown.value}s）`
  : "刷新");

function clearRefreshTimer() {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = null;
}

async function loadLegacyNodes() {
  if (props.nodes !== undefined || !props.active) return;
  const generation = ++requestGeneration;
  legacyLoading.value = true;
  try {
    const response = await listZLMNodes();
    if (generation !== requestGeneration) return;
    if (response.code !== 0) throw new Error(response.message || "节点列表加载失败");
    legacyNodes.value = response.data?.list ?? [];
    legacyError.value = null;
    reconcileContext(legacyNodes.value);
  } catch (error) {
    if (generation === requestGeneration) legacyError.value = error;
  } finally {
    if (generation === requestGeneration) legacyLoading.value = false;
  }
}

function reconcileContext(nodes: readonly Pick<ZLMNode, "id" | "name" | "state">[]) {
  const visible = nodes.map(node => ({ id: node.id, name: node.name, state: node.state }));
  if (!context.initialized) context.initialize(visible);
  else context.reconcileVisibleNodes(visible);
}

function refreshRows() {
  if (props.nodes === undefined) void loadLegacyNodes();
  else emit("refresh");
}

function queryRows() {
  appliedSearch.value = search.value.trim();
  appliedFilterState.value = filterState.value;
  appliedFilterEnabled.value = filterEnabled.value;
  appliedFilterHealth.value = filterHealth.value;
}

function resetFilters() {
  search.value = "";
  filterState.value = undefined;
  filterEnabled.value = undefined;
  filterHealth.value = undefined;
  queryRows();
}

function tickRefreshCountdown() {
  if (refreshCountdown.value > 1) {
    refreshCountdown.value -= 1;
    return;
  }
  refreshCountdown.value = AUTO_REFRESH_SECONDS;
  refreshRows();
}

function scheduleRefresh() {
  clearRefreshTimer();
  refreshCountdown.value = AUTO_REFRESH_SECONDS;
  if (!props.active || !props.autoRefresh) return;
  refreshTimer = setInterval(tickRefreshCountdown, 1_000);
}

function handleManualRefresh() {
  refreshRows();
  scheduleRefresh();
}

watch(() => props.nodes, value => {
  if (value !== undefined) reconcileContext(value);
}, { deep: true });
watch([() => props.active, () => props.autoRefresh, () => props.nodes], scheduleRefresh, { immediate: true });

onMounted(() => {
  if (props.nodes === undefined) void loadLegacyNodes();
});

onBeforeUnmount(() => {
  requestGeneration += 1;
  clearRefreshTimer();
});

function openServiceConfig(node: ZLMNode) {
  context.selectNode(node.id);
  const path = props.canonical ? `/media/nodes/${node.id}` : `/gb28181/zlm/nodes/${node.id}`;
  void router.push({ path, query: { nodeId: String(node.id) } });
}

function openCreate() {
  if (!canManage.value) return;
  formNode.value = null;
  formVisible.value = true;
}

function openEdit(node: ZLMNode) {
  if (!canManage.value) return;
  formNode.value = node;
  formVisible.value = true;
}

function openAction(node: ZLMNode, nextAction: "delete") {
  if (!canManage.value) {
    Message.warning("没有执行该节点操作的权限");
    return;
  }
  context.selectNode(node.id);
  actionNode.value = node;
  action.value = nextAction;
  actionVisible.value = true;
}

async function withOp(node: ZLMNode, name: string, run: () => Promise<void>) {
  opLoading.value[node.id] = name;
  try {
    await run();
  } finally {
    opLoading.value[node.id] = null;
  }
}

async function handleEnabled(node: ZLMNode) {
  if (!canManage.value) return;
  const nextEnabled = node.enabled === false;
  await withOp(node, nextEnabled ? "enable" : "disable", async () => {
    try {
      const response = nextEnabled ? await enableZLMNode(node.id) : await disableZLMNode(node.id);
      if (response.code !== 0) throw new Error(response.message || (nextEnabled ? "启用失败" : "停用失败"));
      if (nextEnabled) {
        Message.success(nodeOnlineState(node) === "active" ? "节点已启用，可参与新任务调度" : "节点已启用，在线后将自动参与调度");
      } else {
        Message.success("节点已停用，不再接收新任务；现有流不会中断");
      }
      refreshRows();
    } catch (error) {
      Message.error(zlmErrorPresentation(error).label);
    }
  });
}

function actionDone() {
  refreshRows();
}

function isZeroTime(value?: string) {
  return !value || value.startsWith("0001-01-01");
}

function relativeTime(value?: string) {
  if (isZeroTime(value)) return "从未上报";
  const timestamp = new Date(value!).getTime();
  if (Number.isNaN(timestamp)) return "时间未知";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86400)} 天前`;
}
</script>

<template>
  <section class="node-list-panel" aria-label="媒体节点治理">
    <div v-if="loadError && scopedNodes.length" class="page-state page-state--warning" role="status">
      本次刷新失败：{{ errorPresentation.label }}。已保留上一次节点列表。
    </div>

    <s-layout-search class="node-search-panel">
      <template #fields>
        <a-input v-model="search" allow-clear placeholder="节点名称 / Host" style="width: 260px" @press-enter="queryRows" />
        <a-select v-model="filterEnabled" allow-clear placeholder="管理状态" style="width: 150px" :options="[
          { label: '启用', value: true },
          { label: '停用', value: false }
        ]" />
        <a-select v-model="filterState" allow-clear placeholder="在线状态" style="width: 150px" :options="[
          { label: '在线', value: 'active' },
          { label: '离线', value: 'offline' }
        ]" />
        <a-select v-model="filterHealth" allow-clear placeholder="健康度" style="width: 150px" :options="[
          { label: '健康', value: 'healthy' },
          { label: '告警', value: 'warning' },
          { label: '严重', value: 'critical' },
          { label: '未知', value: 'unknown' }
        ]" />
      </template>
      <template #actions>
        <a-button type="primary" @click="queryRows"><template #icon><icon-search /></template>查询</a-button>
        <a-button @click="resetFilters"><template #icon><icon-refresh /></template>重置</a-button>
      </template>
      <template #extra>
        <a-button class="node-refresh-button uvp-page-action-btn uvp-refresh-btn" :loading="loading" aria-label="刷新节点列表" @click="handleManualRefresh"><template #icon><icon-refresh /></template>{{ refreshButtonLabel }}</a-button>
        <a-button v-if="canManage" class="uvp-page-action-btn uvp-create-btn" type="primary" @click="openCreate"><template #icon><icon-plus /></template>添加节点</a-button>
      </template>
    </s-layout-search>

    <div v-if="loading && !scopedNodes.length" class="page-state" role="status" aria-label="正在加载节点列表"><a-spin /><span>正在加载媒体节点…</span></div>
    <div v-else-if="loadError && !scopedNodes.length" class="page-state page-state--error" role="alert">
      <strong>{{ errorPresentation.label }}</strong>
      <span>{{ errorPresentation.retryable ? "可以刷新重试。" : "请确认账号权限或登录状态。" }}</span>
      <a-button v-if="errorPresentation.retryable" @click="refreshRows">重新加载</a-button>
    </div>
    <section v-else class="node-table-wrap">
      <a-table :data="filteredNodes" :loading="loading" row-key="id" :pagination="false" class="node-table uvp-data-table">
        <template #columns>
          <a-table-column title="节点" :width="230"><template #cell="{ record }"><div class="cell-node"><span class="cell-node-name">{{ record.name }}</span><span class="cell-node-host">{{ record.host }}:{{ record.apiPort }}</span></div></template></a-table-column>
          <a-table-column title="管理状态" :width="110"><template #cell="{ record }"><span :class="['admin-state', record.enabled === false ? 'admin-state--disabled' : 'admin-state--enabled']">{{ record.enabled === false ? "停用" : "启用" }}</span></template></a-table-column>
          <a-table-column title="在线状态" :width="110"><template #cell="{ record }"><LifecycleDot :state="nodeOnlineState(record)" /></template></a-table-column>
          <a-table-column title="健康度" :width="170"><template #cell="{ record }"><HealthBadge :health="nodeHealth(record)" :reason="nodeHealthReason(record)" /><span v-if="record.recoveryRequired" class="recovery-mark">恢复隔离</span></template></a-table-column>
          <a-table-column title="流 / 会话" :width="120"><template #cell="{ record }"><span v-if="nodeOnlineState(record) === 'offline'">—</span><span v-else class="numeric">{{ record.stats?.mediaSourceCount ?? 0 }} / {{ record.stats?.sessionCount ?? 0 }}</span></template></a-table-column>
          <a-table-column title="调度" :width="130"><template #cell="{ record }"><span v-if="record.enabled === false || nodeOnlineState(record) !== 'active'" class="muted">不参与</span><span v-else-if="record.autoOnDemandReady" class="ready">可调度 · {{ record.weight }}</span><span v-else class="warning">等待收敛</span></template></a-table-column>
          <a-table-column title="最后心跳" :width="130"><template #cell="{ record }"><span :title="record.stats?.lastHeartbeatAt">{{ relativeTime(record.stats?.lastHeartbeatAt) }}</span></template></a-table-column>
          <a-table-column title="操作" :width="280" align="center" fixed="right"><template #cell="{ record }"><div class="uvp-table-actions node-row-actions">
            <a-link class="uvp-table-action uvp-table-action--detail" @click="openServiceConfig(record)"><template #icon><Settings :size="13" /></template>服务配置</a-link>
            <a-link v-if="canManage" class="uvp-table-action uvp-table-action--edit" @click="openEdit(record)"><template #icon><Pencil :size="13" /></template>编辑</a-link>
            <a-link v-if="canManage" :class="['uvp-table-action', record.enabled === false ? 'uvp-table-action--execute' : 'uvp-table-action--scope']" :loading="opLoading[record.id] === (record.enabled === false ? 'enable' : 'disable')" @click="handleEnabled(record)"><template #icon><CirclePower v-if="record.enabled === false" :size="13" /><PowerOff v-else :size="13" /></template>{{ record.enabled === false ? "启用" : "停用" }}</a-link>
            <a-link v-if="canManage" class="uvp-table-action uvp-table-action--delete" @click="openAction(record, 'delete')"><template #icon><Trash2 :size="13" /></template>删除</a-link>
          </div></template></a-table-column>
        </template>
        <template #empty><div class="empty" role="status"><icon-cloud class="empty-icon" /><strong>{{ scopedNodes.length ? "没有符合筛选条件的节点" : "还没有 ZLM 节点" }}</strong><span>{{ scopedNodes.length ? "清空筛选条件后重试。" : "添加第一个节点后，后端会先执行连接探测。" }}</span></div></template>
      </a-table>
    </section>

    <!-- ZLMNodeActionDialog performs backend impact preflight and exact fingerprint confirmation. -->
    <NodeForm v-model:visible="formVisible" :node="formNode" @saved="refreshRows" />
    <ZLMNodeActionDialog v-model:visible="actionVisible" :node="actionNode" :action="action" @done="actionDone" />
  </section>
</template>

<style scoped>
.node-list-panel { min-width: 0; color: var(--zlm-text-2); }
.node-search-panel { margin-bottom: 16px; }
.node-search-panel :deep(.arco-select-view) { box-sizing: border-box; background: var(--uvp-search-control-bg) !important; border: 1px solid var(--uvp-search-secondary-btn-border) !important; border-radius: 10px !important; box-shadow: var(--uvp-search-control-shadow) !important; }
.node-search-panel :deep(.arco-select-view:hover), .node-search-panel :deep(.arco-select-view-focus) { border-color: var(--uvp-brand) !important; box-shadow: var(--uvp-search-control-focus-shadow) !important; }
.node-refresh-button { min-width: 104px; justify-content: center; }
.admin-state { display: inline-flex; align-items: center; gap: 5px; color: var(--zlm-text-2); font-size: var(--zlm-fs-caption); font-weight: var(--zlm-fw-medium); }.admin-state::before { width: 7px; height: 7px; background: var(--zlm-success-500); border-radius: 50%; content: ""; }.admin-state--disabled { color: var(--zlm-text-3); }.admin-state--disabled::before { background: var(--zlm-text-4); }
.recovery-mark { display: inline-block; margin-left: 6px; color: var(--zlm-danger-600); font-size: 11px; }
.page-state { display: flex; min-height: 220px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin-bottom: 16px; padding: 16px; color: var(--zlm-text-3); text-align: center; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }
.page-state--warning { min-height: auto; align-items: flex-start; color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.page-state--error { color: var(--zlm-danger-600); }
.node-table-wrap { overflow: hidden; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--uvp-panel-shadow); }
.cell-node { display: flex; max-width: 100%; flex-direction: column; gap: 2px; text-align: left; }.cell-node-name { overflow: hidden; color: var(--zlm-text-1); font-weight: var(--zlm-fw-semibold); text-overflow: ellipsis; white-space: nowrap; }.cell-node-host { color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: var(--zlm-fs-caption); }
.node-row-actions .uvp-table-action { flex: none; white-space: nowrap; }
.numeric { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); }.muted { color: var(--zlm-text-4); }.ready { color: var(--zlm-success-600); }.warning { color: var(--zlm-warn-600); }.empty { display: flex; flex-direction: column; align-items: center; gap: 7px; padding: 44px 16px; color: var(--zlm-text-3); }.empty strong { color: var(--zlm-text-1); }.empty-icon { font-size: 42px; color: var(--zlm-text-4); }
</style>
