<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useRouter } from "vue-router";

import {
  activateZLMNode,
  listZLMNodes,
  testZLMNodeConnection,
  type ZLMNode
} from "@/api/gb28181-zlm";
import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import NodeForm from "../../NodeForm.vue";
import ZLMNodeActionDialog from "../../ZLMNodeActionDialog.vue";
import StatCard from "../../components/StatCard.vue";
import LifecycleDot from "../../components/LifecycleDot.vue";
import HealthBadge from "../../components/HealthBadge.vue";
import { zlmErrorPresentation } from "../../components/zlmFormatters";
import type { NodeDangerAction } from "../../nodeActionState";
import {
  filterNodeRecords,
  nodeHealth,
  nodeHealthReason,
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
const filterHealth = ref<NodeHealth>();
const formVisible = ref(false);
const formNode = ref<ZLMNode | null>(null);
const actionVisible = ref(false);
const actionNode = ref<ZLMNode | null>(null);
const action = ref<NodeDangerAction | null>(null);
const opLoading = ref<Record<number, string | null>>({});
let requestGeneration = 0;
let refreshTimer: ReturnType<typeof setInterval> | null = null;

const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:node:manage"));
const canKick = computed(() => hasPermission("gb28181:zlm:node:kick"));
const canRestart = computed(() => hasPermission("gb28181:zlm:restart"));
const sourceNodes = computed<readonly ZLMNode[]>(() => props.nodes === undefined ? legacyNodes.value : props.nodes as readonly ZLMNode[]);
const scopedNodes = computed(() => props.scope === "all"
  ? sourceNodes.value
  : sourceNodes.value.filter(node => node.id === props.scope));
const filteredNodes = computed(() => filterNodeRecords(scopedNodes.value, {
  keyword: search.value,
  state: filterState.value as ZLMNode["state"] | undefined,
  health: filterHealth.value
}));
const loading = computed(() => props.nodes === undefined ? legacyLoading.value : props.loading);
const loadError = computed(() => props.nodes === undefined ? legacyError.value : props.error);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const totalNodes = computed(() => scopedNodes.value.length);
const activeCount = computed(() => scopedNodes.value.filter(node => node.state === "active").length);
const offlineCount = computed(() => scopedNodes.value.filter(node => node.state === "offline").length);
const maintenanceCount = computed(() => scopedNodes.value.filter(node => node.state === "maintenance").length);
const totalStreams = computed(() => scopedNodes.value.reduce((sum, node) => sum + (node.stats?.mediaSourceCount ?? 0), 0));
const totalSessions = computed(() => scopedNodes.value.reduce((sum, node) => sum + (node.stats?.sessionCount ?? 0), 0));
const healthyCount = computed(() => scopedNodes.value.filter(node => nodeHealth(node) === "healthy").length);

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

function scheduleRefresh() {
  clearRefreshTimer();
  if (!props.active || !props.autoRefresh) return;
  refreshTimer = setInterval(refreshRows, 30_000);
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

function gotoDetail(node: ZLMNode) {
  context.selectNode(node.id);
  const path = props.canonical ? `/media/nodes/${node.id}` : `/gb28181/zlm/nodes/${node.id}`;
  void router.push({ path, query: { view: "overview", nodeId: String(node.id) } });
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

function openAction(node: ZLMNode, nextAction: NodeDangerAction) {
  const allowed = nextAction === "kick"
    ? canKick.value
    : nextAction === "restart"
      ? canRestart.value
      : canManage.value;
  if (!allowed) {
    Message.warning("没有执行该节点操作的权限");
    return;
  }
  context.selectNode(node.id);
  actionNode.value = node;
  action.value = nextAction;
  actionVisible.value = true;
}

function canShowMore(node: ZLMNode) {
  return canManage.value || (node.state !== "offline" && (canKick.value || canRestart.value));
}

async function withOp(node: ZLMNode, name: string, run: () => Promise<void>) {
  opLoading.value[node.id] = name;
  try {
    await run();
  } finally {
    opLoading.value[node.id] = null;
  }
}

async function handleActivate(node: ZLMNode) {
  if (!canManage.value) return;
  await withOp(node, "activate", async () => {
    try {
      const response = await activateZLMNode(node.id);
      if (response.code !== 0) throw new Error(response.message || "激活失败");
      Message.success("节点已激活并重新允许调度");
      refreshRows();
    } catch (error) {
      Message.error(zlmErrorPresentation(error).label);
    }
  });
}

async function handleReprobe(node: ZLMNode) {
  if (!canManage.value) return;
  await withOp(node, "reprobe", async () => {
    try {
      const response = await testZLMNodeConnection(node.id);
      if (response.code !== 0 || !response.data?.online) {
        throw new Error(response.data?.error || response.message || "节点仍不可达");
      }
      if (node.state === "offline") {
        const activate = await activateZLMNode(node.id);
        if (activate.code !== 0) throw new Error(activate.message || "连接已恢复，但激活失败");
      }
      Message.success("候选连接探测成功，节点状态已回读");
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
    <section class="kpi-row" aria-label="节点集群指标">
      <StatCard title="节点总数" :value="totalNodes" :trend="`${activeCount} 活跃 · ${maintenanceCount} 维护 · ${offlineCount} 离线`" accent="brand" />
      <StatCard title="活跃流" :value="totalStreams" :trend="`${activeCount} 个调度节点`" accent="accent" />
      <StatCard title="网络会话" :value="totalSessions" trend="节点心跳登记值" />
      <StatCard title="健康节点" :value="healthyCount" :trend="`${totalNodes ? Math.round(healthyCount / totalNodes * 100) : 0}% 集群占比`" :accent="healthyCount === totalNodes ? 'accent' : 'warning'" />
    </section>

    <div v-if="loadError && scopedNodes.length" class="page-state page-state--warning" role="status">
      本次刷新失败：{{ errorPresentation.label }}。已保留上一次节点列表。
    </div>

    <s-layout-search class="node-search-panel">
      <template #fields>
        <a-input-search v-model="search" allow-clear placeholder="搜索节点名或 Host" class="search" />
        <a-select v-model="filterState" allow-clear placeholder="生命周期" class="filter-select" :options="[
          { label: '活跃', value: 'active' },
          { label: '维护', value: 'maintenance' },
          { label: '离线', value: 'offline' }
        ]" />
        <a-select v-model="filterHealth" allow-clear placeholder="健康度" class="filter-select" :options="[
          { label: '健康', value: 'healthy' },
          { label: '告警', value: 'warning' },
          { label: '严重', value: 'critical' },
          { label: '未知', value: 'unknown' }
        ]" />
      </template>
      <template #actions>
        <span class="filter-meta">{{ filteredNodes.length }} / {{ scopedNodes.length }} 节点</span>
        <a-button class="uvp-refresh-btn" :loading="loading" aria-label="刷新节点列表" @click="refreshRows"><template #icon><icon-refresh /></template>刷新</a-button>
      </template>
      <template #extra>
        <a-button v-if="canManage" type="primary" @click="openCreate"><template #icon><icon-plus /></template>添加节点</a-button>
      </template>
    </s-layout-search>
     <p class="scope-hint">当前范围：{{ props.scope === "all" ? "全部可见节点" : `节点 #${props.scope}` }}；新增、维护、删除和重启等写操作必须作用于明确选择节点。</p>

    <div v-if="loading && !scopedNodes.length" class="page-state" role="status" aria-label="正在加载节点列表"><a-spin /><span>正在加载媒体节点…</span></div>
    <div v-else-if="loadError && !scopedNodes.length" class="page-state page-state--error" role="alert">
      <strong>{{ errorPresentation.label }}</strong>
      <span>{{ errorPresentation.retryable ? "可以刷新重试。" : "请确认账号权限或登录状态。" }}</span>
      <a-button v-if="errorPresentation.retryable" @click="refreshRows">重新加载</a-button>
    </div>
    <section v-else class="node-table-wrap">
      <a-table :data="filteredNodes" :loading="loading" row-key="id" :pagination="false" class="node-table uvp-data-table">
        <template #columns>
          <a-table-column title="节点" :width="230"><template #cell="{ record }"><button type="button" class="cell-node" :aria-label="`查看节点 ${record.name}`" @click="gotoDetail(record)"><span class="cell-node-name">{{ record.name }}</span><span class="cell-node-host">{{ record.host }}:{{ record.apiPort }}</span></button></template></a-table-column>
          <a-table-column title="状态" :width="110"><template #cell="{ record }"><LifecycleDot :state="record.state" /></template></a-table-column>
          <a-table-column title="健康度" :width="170"><template #cell="{ record }"><HealthBadge :health="nodeHealth(record)" :reason="nodeHealthReason(record)" /><span v-if="record.recoveryRequired" class="recovery-mark">恢复隔离</span></template></a-table-column>
          <a-table-column title="流 / 会话" :width="120"><template #cell="{ record }"><span v-if="record.state === 'offline'">—</span><span v-else class="numeric">{{ record.stats?.mediaSourceCount ?? 0 }} / {{ record.stats?.sessionCount ?? 0 }}</span></template></a-table-column>
          <a-table-column title="调度" :width="130"><template #cell="{ record }"><span v-if="record.state !== 'active'" class="muted">不参与</span><span v-else-if="record.autoOnDemandReady" class="ready">可调度 · {{ record.weight }}</span><span v-else class="warning">等待收敛</span></template></a-table-column>
          <a-table-column title="最后心跳" :width="130"><template #cell="{ record }"><span :title="record.stats?.lastHeartbeatAt">{{ relativeTime(record.stats?.lastHeartbeatAt) }}</span></template></a-table-column>
          <a-table-column title="操作" :width="300" fixed="right"><template #cell="{ record }"><div class="cell-ops">
            <a-button size="small" @click="gotoDetail(record)">详情</a-button>
            <a-button v-if="canManage" size="small" @click="openEdit(record)">编辑</a-button>
            <a-button v-if="canManage && record.state === 'active'" size="small" @click="openAction(record, 'maintenance')">维护</a-button>
            <a-button v-else-if="canManage && record.state === 'maintenance'" size="small" type="primary" :loading="opLoading[record.id] === 'activate'" @click="handleActivate(record)">激活</a-button>
            <a-button v-else-if="canManage && record.state === 'offline'" size="small" type="primary" :loading="opLoading[record.id] === 'reprobe'" @click="handleReprobe(record)">探测</a-button>
            <a-dropdown v-if="canShowMore(record)" trigger="click" position="br"><a-button size="small">更多<template #icon><icon-down /></template></a-button><template #content>
              <a-doption v-if="canKick && record.state !== 'offline'" @click="openAction(record, 'kick')">驱逐全部会话</a-doption>
              <a-doption v-if="canRestart && record.state !== 'offline'" @click="openAction(record, 'restart')">重启 ZLM</a-doption>
              <a-doption v-if="canManage" class="danger" @click="openAction(record, 'delete')">删除节点</a-doption>
            </template></a-dropdown>
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
.kpi-row { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; margin-bottom: 16px; }
.node-search-panel { margin-bottom: 16px; }
.search { width: 220px; }.filter-select { width: 132px; }.filter-meta { display: inline-flex; align-items: center; min-height: 34px; color: var(--zlm-text-3); font-size: 12px; }
.scope-hint { margin: -8px 0 14px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.recovery-mark { display: inline-block; margin-left: 6px; color: var(--zlm-danger-600); font-size: 11px; }
.page-state { display: flex; min-height: 220px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin-bottom: 16px; padding: 16px; color: var(--zlm-text-3); text-align: center; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }
.page-state--warning { min-height: auto; align-items: flex-start; color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.page-state--error { color: var(--zlm-danger-600); }
.node-table-wrap { overflow: hidden; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--uvp-panel-shadow); }
.cell-node { display: flex; max-width: 100%; flex-direction: column; gap: 2px; padding: 0; text-align: left; background: transparent; border: 0; cursor: pointer; }.cell-node:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 3px; border-radius: 5px; }.cell-node-name { overflow: hidden; color: var(--zlm-text-1); font-weight: var(--zlm-fw-semibold); text-overflow: ellipsis; white-space: nowrap; }.cell-node:hover .cell-node-name { color: var(--zlm-brand-600); }.cell-node-host { color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: var(--zlm-fs-caption); }
.numeric { color: var(--zlm-text-1); font-family: var(--zlm-font-mono); }.muted { color: var(--zlm-text-4); }.ready { color: var(--zlm-success-600); }.warning { color: var(--zlm-warn-600); }.cell-ops { display: flex; align-items: center; gap: 6px; }.empty { display: flex; flex-direction: column; align-items: center; gap: 7px; padding: 44px 16px; color: var(--zlm-text-3); }.empty strong { color: var(--zlm-text-1); }.empty-icon { font-size: 42px; color: var(--zlm-text-4); }.danger { color: var(--zlm-danger-600); }
 @media (max-width: 1180px) { .kpi-row { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
 @media (max-width: 720px) { .kpi-row { grid-template-columns: 1fr; } .search, .filter-select { width: 100%; } .cell-ops { flex-wrap: wrap; } }
</style>
