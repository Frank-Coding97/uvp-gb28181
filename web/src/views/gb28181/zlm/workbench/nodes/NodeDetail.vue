<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";

import { activateZLMNode, getZLMNode, testZLMNodeConnection, type ZLMNode } from "@/api/gb28181-zlm";
import { useZLMNodeCatalog } from "@/store/modules/media-workbench";
import { useRouteConfigStore } from "@/store/modules/route-config";
import { useZLMContextStore, type ZLMContextNode } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import HealthBadge from "../../components/HealthBadge.vue";
import LifecycleDot from "../../components/LifecycleDot.vue";
import StatCard from "../../components/StatCard.vue";
import { zlmErrorPresentation, zlmFreshnessPresentation } from "../../components/zlmFormatters";
import NodeForm from "../../NodeForm.vue";
import ZLMNodeActionDialog from "../../ZLMNodeActionDialog.vue";
import type { NodeDangerAction } from "../../nodeActionState";
import MediaWorkspaceShell from "../MediaWorkspaceShell.vue";
import NodeRuntimePanel from "./NodeRuntimePanel.vue";
import NodeConfigView from "./NodeConfigView.vue";
import { nodeHealth, nodeHealthReason, resolveNodeDetailView, type NodeDetailView } from "./nodeManagementState";

const props = withDefaults(defineProps<{ canonical?: boolean }>(), { canonical: true });

const route = useRoute();
const router = useRouter();
const routeStore = useRouteConfigStore();
const context = useZLMContextStore();
const catalog = useZLMNodeCatalog();
const userStore = useUserStoreHook();
const node = ref<ZLMNode | null>(null);
const loading = ref(true);
const loadError = ref<unknown>(null);
const editVisible = ref(false);
const actionVisible = ref(false);
const action = ref<NodeDangerAction | null>(null);
const operation = ref<"activate" | "reprobe" | null>(null);
const generation = ref(0);
const configDirty = ref(false);
const runtimeVisited = ref(false);
const configVisited = ref(false);

const nodeId = computed(() => {
  const parsed = Number(route.params.id);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
});
const legacyPaths = computed(() => routeStore.routeList.map((item: Menu.MenuOptions) => item.path));
const menuViews = computed(() => {
  const paths = new Set(legacyPaths.value);
  const wildcard = userStore.account.permissions.includes("*:*:*");
  return [
    wildcard || paths.has("/gb28181/zlm/nodes") || paths.has("/gb28181/zlm/nodes/:id") ? "overview" : "",
    wildcard || paths.has("/gb28181/zlm/runtime") ? "runtime" : "",
    wildcard || paths.has("/gb28181/zlm/config") ? "config" : ""
  ].filter(Boolean);
});
const allowedViews = computed<readonly NodeDetailView[]>(() => {
  // A route-config store is normally ready before this component mounts. During its
  // short bootstrap window retain the safe read-only overview instead of expanding rights.
  if (!routeStore.routeList.length) return ["overview"];
  return ["overview", "runtime", "config"].filter(view => menuViews.value.includes(view)) as NodeDetailView[];
});
const currentView = computed<NodeDetailView>(() => {
  const requested = resolveNodeDetailView(route.query.view);
  return allowedViews.value.includes(requested) ? requested : allowedViews.value[0] ?? "overview";
});
const detailViewLabels: Record<NodeDetailView, string> = {
  overview: "概览",
  runtime: "运行监控",
  config: "服务配置"
};
const detailViews = computed(() => allowedViews.value.map(view => ({ key: view, label: detailViewLabels[view] })));
const scopeNodes = computed(() => catalog.nodes.value.length ? catalog.nodes.value : context.visibleNodes);
watch(currentView, view => {
  if (view === "runtime") runtimeVisited.value = true;
  if (view === "config") configVisited.value = true;
}, { immediate: true });
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:node:manage"));
const canKick = computed(() => hasPermission("gb28181:zlm:node:kick"));
const canRestart = computed(() => hasPermission("gb28181:zlm:restart"));
const canConfigUpdate = computed(() => hasPermission("gb28181:zlm:config:update"));
const canShowMore = computed(() => Boolean(node.value)
  && (canManage.value || (node.value!.state !== "offline" && (canKick.value || canRestart.value))));
const freshness = computed(() => zlmFreshnessPresentation(node.value?.stats?.lastHeartbeatAt));
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));

async function loadNode(id: number) {
  const requestGeneration = ++generation.value;
  loading.value = true;
  loadError.value = null;
  try {
    const response = await getZLMNode(id);
    if (requestGeneration !== generation.value) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "节点详情加载失败");
    node.value = response.data;
    const visible = [
      ...context.visibleNodes.filter((item: ZLMContextNode) => item.id !== response.data!.id),
      { id: response.data.id, name: response.data.name, state: response.data.state }
    ];
    if (!context.initialized) context.initialize(visible, response.data.id);
    else context.reconcileVisibleNodes(visible);
    context.selectNode(response.data.id);
  } catch (error) {
    if (requestGeneration === generation.value) {
      node.value = null;
      loadError.value = error;
    }
  } finally {
    if (requestGeneration === generation.value) loading.value = false;
  }
}

watch(nodeId, id => {
  node.value = null;
  if (id) void loadNode(id);
  else {
    generation.value += 1;
    loading.value = false;
    loadError.value = new Error("节点地址无效");
  }
}, { immediate: true });

function refresh() {
  if (nodeId.value) void loadNode(nodeId.value);
}

async function refreshCatalog() {
  try {
    const nodes = await catalog.refresh();
    context.reconcileVisibleNodes(nodes);
  } catch {
    // The shared catalog keeps its last successful nodes and exposes the retryable error.
  }
}

function setView(view: string) {
  if (!allowedViews.value.includes(view as NodeDetailView)) return;
  const path = props.canonical ? `/media/nodes/${nodeId.value}` : `/gb28181/zlm/nodes/${nodeId.value}`;
  void router.replace({ path, query: { ...route.query, view } });
}

function switchNode(next: number | "all") {
  if (typeof next !== "number" || next === nodeId.value) return;
  context.selectNode(next);
  const path = props.canonical ? `/media/nodes/${next}` : `/gb28181/zlm/nodes/${next}`;
  void router.push({ path, query: { ...route.query } });
}

function goBack() {
  void router.push(props.canonical ? "/media/nodes" : "/gb28181/zlm/nodes");
}

async function activate() {
  if (!node.value || !canManage.value || operation.value) return;
  operation.value = "activate";
  try {
    const response = await activateZLMNode(node.value.id);
    if (response.code !== 0) throw new Error(response.message || "激活失败");
    Message.success("节点已激活并重新允许调度");
    refresh();
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    operation.value = null;
  }
}

async function reprobe() {
  if (!node.value || !canManage.value || operation.value) return;
  operation.value = "reprobe";
  try {
    const response = await testZLMNodeConnection(node.value.id);
    if (response.code !== 0 || !response.data?.online) throw new Error(response.data?.error || response.message || "节点仍不可达");
    if (node.value.state === "offline") {
      const activated = await activateZLMNode(node.value.id);
      if (activated.code !== 0) throw new Error(activated.message || "连接已恢复，但激活失败");
    }
    Message.success("候选连接探测成功，节点状态已回读");
    refresh();
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    operation.value = null;
  }
}

function openAction(nextAction: NodeDangerAction) {
  const allowed = nextAction === "kick"
    ? canKick.value
    : nextAction === "restart"
      ? canRestart.value
      : canManage.value;
  if (!allowed) {
    Message.warning("没有执行该节点操作的权限");
    return;
  }
  action.value = nextAction;
  actionVisible.value = true;
}

function actionDone(payload: { action: NodeDangerAction }) {
  if (payload.action === "delete") {
    goBack();
    return;
  }
  refresh();
}

function formatTime(value?: string) {
  if (!value || value.startsWith("0001-01-01")) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}

function confirmDiscardConfigDrafts() {
  if (!configDirty.value) return Promise.resolve(true);
  return new Promise<boolean>(resolve => {
    Modal.warning({
      title: "存在未保存配置",
      content: "离开当前节点会丢失尚未保存的服务配置草稿。是否继续离开？",
      okText: "继续离开",
      cancelText: "留在此页",
      hideCancel: false,
      okButtonProps: { status: "danger" },
      onOk: () => resolve(true),
      onCancel: () => resolve(false)
    });
  });
}

onBeforeRouteLeave(() => confirmDiscardConfigDrafts());
onBeforeRouteUpdate((to: { params: Record<string, unknown> }) => {
  if (String(to.params.id ?? "") === String(route.params.id ?? "")) return true;
  return confirmDiscardConfigDrafts();
});

onMounted(async () => {
  try {
    const nodes = await catalog.load();
    context.reconcileVisibleNodes(nodes);
  } catch {
    // Node detail remains usable with the currently loaded node when the catalog is unavailable.
  }
});

onBeforeUnmount(() => {
  generation.value += 1;
});
</script>

<template>
  <MediaWorkspaceShell
    title="节点详情"
    description="查看单个 ZLMediaKit 节点的运行状态与服务配置。"
    :views="detailViews"
    :active-view="currentView"
    :scope="nodeId ?? 'all'"
    :nodes="scopeNodes"
    :loading="loading"
    :auto-refresh="false"
    :show-auto-refresh="false"
    :allow-all="false"
    :requires-node="true"
    :scope-loading="catalog.loading.value"
    :scope-error="catalog.error.value ? '节点目录刷新失败' : ''"
    @update:active-view="setView"
    @update:scope="switchNode"
    @refresh="refresh"
    @refresh-scope="refreshCatalog"
  >
    <template #content>
      <section class="node-detail-view" aria-label="媒体节点详情">
        <header class="detail-toolbar">
          <div class="node-identity">
            <a-button class="back-btn" aria-label="返回节点列表" @click="goBack"><template #icon><icon-left /></template></a-button>
            <div class="identity-main"><div class="identity-title-row"><strong>{{ node?.name || (nodeId ? `节点 #${nodeId}` : "节点详情") }}</strong><LifecycleDot v-if="node" :state="node.state" /><HealthBadge v-if="node" :health="nodeHealth(node)" :reason="nodeHealthReason(node)" /></div><div class="identity-subtitle"><span>{{ node ? `${node.host}:${node.apiPort}` : "正在加载连接信息" }}</span><span v-if="node?.mediaServerUUID">UUID {{ node.mediaServerUUID }}</span></div></div>
          </div>
          <div class="detail-actions"><a-button v-if="canManage && node?.state === 'offline'" type="primary" :loading="operation === 'reprobe'" @click="reprobe">重新探测</a-button><a-button v-if="canManage && node?.state === 'active'" @click="openAction('maintenance')">切维护</a-button><a-button v-if="canManage && node?.state === 'maintenance'" type="primary" :loading="operation === 'activate'" @click="activate">激活</a-button><a-button v-if="canManage && node" @click="editVisible = true">编辑连接</a-button><a-dropdown v-if="node && canShowMore" trigger="click" position="br"><a-button>更多<template #icon><icon-down /></template></a-button><template #content><a-doption v-if="canKick && node.state !== 'offline'" @click="openAction('kick')">驱逐全部会话</a-doption><a-doption v-if="canRestart && node.state !== 'offline'" @click="openAction('restart')">重启 ZLM 服务</a-doption><a-doption v-if="canManage" class="danger" @click="openAction('delete')">删除节点</a-doption></template></a-dropdown></div>
        </header>

        <div v-if="!allowedViews.length" class="detail-state detail-state--error" role="alert"><strong>没有节点详情权限</strong><span>当前账号没有可查看的节点视图。</span><a-button @click="goBack">返回节点列表</a-button></div>
        <div v-else-if="loadError && !node" class="detail-state detail-state--error" role="alert"><strong>{{ errorPresentation.label }}</strong><span>请确认节点存在、账号权限和登录状态。</span><a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button><a-button @click="goBack">返回节点列表</a-button></div>
        <template v-else>
          <div v-if="loadError && node" class="detail-status detail-status--warning" role="status">本次刷新失败：{{ errorPresentation.label }}。已保留上一次节点详情。</div>
          <div v-if="node?.recoveryRequired" class="detail-status detail-status--danger" role="alert"><strong>节点处于恢复隔离</strong><span>{{ node.recoveryReason || "候选更新回滚未确认，节点暂不参与调度。" }}</span></div>
          <div v-if="loading && !node" class="detail-state" role="status" aria-label="正在加载节点详情"><a-spin /><span>正在加载节点详情…</span></div>
          <template v-if="node">
            <section v-show="currentView === 'overview'" class="overview-view">
              <div class="freshness-row"><span :class="`freshness freshness--${freshness.tone}`">{{ freshness.label }}</span><span>{{ freshness.description }}</span><a-link v-if="allowedViews.includes('runtime')" @click="setView('runtime')">查看运行监控</a-link></div>
              <section class="kpi-grid" aria-label="节点关键指标"><StatCard title="活跃流" :value="node.stats?.mediaSourceCount" trend="MediaSource" accent="brand" /><StatCard title="会话数" :value="node.stats?.sessionCount" trend="节点登记会话" accent="accent" /><StatCard title="综合线程负载" :value="(node.stats?.netThreadLoadAvg ?? 0) * .6 + (node.stats?.workThreadLoadAvg ?? 0) * .4" is-percent unit="%" trend="Net×0.6 + Work×0.4" :accent="((node.stats?.netThreadLoadAvg ?? 0) * .6 + (node.stats?.workThreadLoadAvg ?? 0) * .4) >= .8 ? 'danger' : 'default'" /><StatCard title="内存使用" :value-text="node.stats?.memoryUsageBytes === undefined ? '—' : `${Math.round(node.stats.memoryUsageBytes / 1024 / 1024)} MB`" trend="节点心跳值" /></section>
              <section class="info-block" aria-labelledby="node-info-title"><h2 id="node-info-title">节点信息</h2><dl class="info-grid"><div><dt>ID / revision</dt><dd>#{{ node.id }} / {{ node.revision }}</dd></div><div><dt>UUID</dt><dd class="mono">{{ node.mediaServerUUID || "—" }}</dd></div><div><dt>管理地址</dt><dd class="mono">{{ node.host }}:{{ node.apiPort }}</dd></div><div><dt>设备收流地址</dt><dd class="mono">{{ node.receiveHost || node.host }}</dd></div><div><dt>播放访问地址</dt><dd class="mono">{{ node.playbackHost || node.host }}</dd></div><div><dt>RTP 端口</dt><dd>{{ node.rtpPortStart }}-{{ node.rtpPortEnd }}</dd></div><div><dt>调度状态</dt><dd>{{ node.autoOnDemandReady ? `已收敛，权重 ${node.weight}` : "等待配置收敛，不应承接新流" }}</dd></div><div><dt>最后心跳</dt><dd>{{ formatTime(node.stats?.lastHeartbeatAt) }}</dd></div><div><dt>创建时间</dt><dd>{{ formatTime(node.createdAt) }}</dd></div><div><dt>更新时间</dt><dd>{{ formatTime(node.updatedAt) }}</dd></div></dl></section>
            </section>
            <NodeRuntimePanel v-if="runtimeVisited" v-show="currentView === 'runtime'" :node-id="node.id" :active="currentView === 'runtime'" />
            <NodeConfigView v-if="configVisited" v-show="currentView === 'config'" :node-id="node.id" :node-name="node.name" :active="currentView === 'config'" :editable="canConfigUpdate" @dirty-change="configDirty = $event" />
          </template>
        </template>

        <NodeForm v-model:visible="editVisible" :node="node" @saved="refresh" />
        <ZLMNodeActionDialog v-model:visible="actionVisible" :node="node" :action="action" @done="actionDone" />
      </section>
    </template>
  </MediaWorkspaceShell>
</template>

<style scoped>
.node-detail-view { box-sizing: border-box; width: 100%; min-height: 100%; color: var(--zlm-text-2); }.detail-toolbar, .identity-title-row, .identity-subtitle, .detail-actions, .freshness-row { display: flex; align-items: center; gap: 9px; }.detail-toolbar { justify-content: space-between; margin-bottom: 14px; }.node-identity { display: flex; min-width: 0; flex: 1; align-items: center; gap: 9px; }.back-btn { width: 38px; min-width: 38px; padding: 0; }.identity-main { min-width: 0; }.identity-title-row strong { overflow: hidden; color: var(--zlm-text-1); font-size: 17px; text-overflow: ellipsis; white-space: nowrap; }.identity-subtitle { margin-top: 3px; color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: 11px; }.detail-actions { flex-wrap: wrap; justify-content: flex-end; }.detail-status { display: flex; gap: 10px; margin-bottom: 14px; padding: 10px 13px; border: 1px solid; border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.detail-status--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }.detail-status--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }.detail-state { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; }.detail-state--error { color: var(--zlm-danger-600); }.freshness-row { margin-bottom: 14px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.freshness-row .arco-link { margin-left: auto; }.freshness--success { color: var(--zlm-success-600); }.freshness--warning { color: var(--zlm-warn-600); }.freshness--danger { color: var(--zlm-danger-600); }.kpi-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }.info-block { margin-top: 16px; padding: 16px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); box-shadow: var(--uvp-panel-shadow); }.info-block h2 { margin: 0 0 10px; color: var(--zlm-text-1); font-size: 14px; }.info-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; column-gap: 24px; }.info-grid div { display: grid; grid-template-columns: 120px minmax(0, 1fr); gap: 12px; padding: 10px 0; border-bottom: 1px solid var(--uvp-divider); }.info-grid dt { color: var(--zlm-text-3); }.info-grid dd { min-width: 0; margin: 0; color: var(--zlm-text-1); overflow-wrap: anywhere; }.mono { font-family: var(--zlm-font-mono); }.danger { color: var(--zlm-danger-600); }
@media (max-width: 1120px) { .kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@container media-workspace-content (max-width: 720px) { .detail-toolbar, .node-identity, .freshness-row { align-items: flex-start; flex-direction: column; }.identity-subtitle { flex-wrap: wrap; }.detail-actions { justify-content: flex-start; }.kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }.info-grid { grid-template-columns: 1fr; }.info-grid div { grid-template-columns: 1fr; gap: 3px; }.freshness-row .arco-link { margin-left: 0; } }
@media (max-width: 720px) { .detail-toolbar, .node-identity, .freshness-row { align-items: flex-start; flex-direction: column; }.detail-actions { justify-content: flex-start; }.kpi-grid, .info-grid { grid-template-columns: 1fr; }.info-grid div { grid-template-columns: 1fr; gap: 3px; }.freshness-row .arco-link { margin-left: 0; } }
</style>
