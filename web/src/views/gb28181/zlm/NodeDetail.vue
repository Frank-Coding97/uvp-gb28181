<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import {
  activateZLMNode,
  getZLMNode,
  testZLMNodeConnection,
  type ZLMNode
} from "@/api/gb28181-zlm";
import { useZLMContextStore, type ZLMContextNode } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import StatCard from "./components/StatCard.vue";
import LifecycleDot from "./components/LifecycleDot.vue";
import HealthBadge from "./components/HealthBadge.vue";
import Sparkline from "./components/Sparkline.vue";
import NodeConfig from "./NodeConfig.vue";
import NodeForm from "./NodeForm.vue";
import ZLMNodeActionDialog from "./ZLMNodeActionDialog.vue";
import { formatZLMBytes, zlmErrorPresentation, zlmFreshnessPresentation } from "./components/zlmFormatters";
import { useZLMRuntimePolling } from "./composables/useZLMRuntimePolling";
import type { NodeDangerAction } from "./nodeActionState";

interface HistoryPoint {
  time: number;
  value: number;
}

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const node = ref<ZLMNode | null>(null);
const loading = ref(true);
const loadError = ref<unknown>(null);
const activeTab = ref<"overview" | "config">("overview");
const editVisible = ref(false);
const actionVisible = ref(false);
const action = ref<NodeDangerAction | null>(null);
const opLoading = ref<Record<string, boolean>>({});
const pollNodeId = ref<number | null>(null);
const streamHistory = ref<HistoryPoint[]>([]);
const sessionHistory = ref<HistoryPoint[]>([]);
const cpuHistory = ref<HistoryPoint[]>([]);
const HISTORY_MAX = 30;
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes(permission);
const canManage = computed(() => hasPermission("gb28181:zlm:node:manage"));
const canKick = computed(() => hasPermission("gb28181:zlm:node:kick"));
const canRestart = computed(() => hasPermission("gb28181:zlm:restart"));
const canShowMore = computed(() => Boolean(node.value)
  && (canManage.value || (node.value!.state !== "offline" && (canKick.value || canRestart.value))));

watch(
  () => route.params.id,
  value => {
    const parsed = Number(value);
    if (!Number.isSafeInteger(parsed) || parsed <= 0) {
      pollNodeId.value = null;
      void router.replace("/gb28181/zlm/nodes");
      return;
    }
    if (pollNodeId.value !== parsed) {
      streamHistory.value = [];
      sessionHistory.value = [];
      cpuHistory.value = [];
    }
    pollNodeId.value = parsed;
  },
  { immediate: true }
);

function appendHistory(history: HistoryPoint[], value: number) {
  return [...history, { time: Date.now(), value }].slice(-HISTORY_MAX);
}

const { refresh } = useZLMRuntimePolling<ZLMNode>({
  nodeId: pollNodeId,
  paused: editVisible,
  intervalMs: 30_000,
  async load(nodeId) {
    loading.value = true;
    const response = await getZLMNode(nodeId);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "节点详情加载失败");
    return response.data;
  },
  publish(value) {
    node.value = value;
    const visible = [
      ...context.visibleNodes.filter((item: ZLMContextNode) => item.id !== value.id),
      { id: value.id, name: value.name, state: value.state }
    ];
    if (!context.initialized) context.initialize(visible, value.id);
    else context.reconcileVisibleNodes(visible);
    context.selectNode(value.id);
    streamHistory.value = appendHistory(streamHistory.value, value.stats?.mediaSourceCount ?? 0);
    sessionHistory.value = appendHistory(sessionHistory.value, value.stats?.sessionCount ?? 0);
    cpuHistory.value = appendHistory(cpuHistory.value, cpuRatio(value));
    loadError.value = null;
    loading.value = false;
  },
  onError(error) {
    loadError.value = error;
    loading.value = false;
  }
});

const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const freshness = computed(() => zlmFreshnessPresentation(node.value?.stats?.lastHeartbeatAt));

function cpuRatio(value: ZLMNode | null) {
  if (!value?.stats) return 0;
  return (value.stats.netThreadLoadAvg ?? 0) * 0.6 + (value.stats.workThreadLoadAvg ?? 0) * 0.4;
}

const cpuPercent = computed(() => Math.round(cpuRatio(node.value) * 100));
const rtpUsage = computed(() => {
  if (!node.value) return { used: 0, total: 0, percent: 0 };
  const total = Math.max(0, node.value.rtpPortEnd - node.value.rtpPortStart + 1);
  const used = node.value.stats?.mediaSourceCount ?? 0;
  return { used, total, percent: total ? Math.round(used / total * 100) : 0 };
});

function healthOf(value: ZLMNode | null): "healthy" | "warning" | "critical" | "unknown" {
  if (!value) return "unknown";
  if (value.recoveryRequired || value.state === "offline") return "critical";
  if (value.state === "maintenance") return "unknown";
  return value.nearCapacity || !value.autoOnDemandReady ? "warning" : "healthy";
}

function healthReason(value: ZLMNode | null) {
  if (!value) return "";
  if (value.recoveryRequired) return value.recoveryReason || "配置恢复未完成";
  if (value.state === "offline") return "节点离线";
  if (value.state === "maintenance") return "节点处于维护状态";
  if (!value.autoOnDemandReady) return "自动按需配置尚未收敛";
  if (value.nearCapacity) return "接近容量";
  return "";
}

async function withOp(op: string, run: () => Promise<void>) {
  opLoading.value[op] = true;
  try {
    await run();
  } finally {
    opLoading.value[op] = false;
  }
}

async function activate() {
  if (!node.value || !canManage.value) return;
  await withOp("activate", async () => {
    try {
      const response = await activateZLMNode(node.value!.id);
      if (response.code !== 0) throw new Error(response.message || "激活失败");
      Message.success("节点已激活");
      refresh();
    } catch (error) {
      Message.error(zlmErrorPresentation(error).label);
    }
  });
}

async function reprobe() {
  if (!node.value || !canManage.value) return;
  await withOp("reprobe", async () => {
    try {
      const response = await testZLMNodeConnection(node.value!.id);
      if (response.code !== 0 || !response.data?.online) throw new Error(response.data?.error || response.message || "节点仍不可达");
      if (node.value!.state === "offline") {
        const activated = await activateZLMNode(node.value!.id);
        if (activated.code !== 0) throw new Error(activated.message || "连接已恢复，但激活失败");
      }
      Message.success("连接探测成功，节点状态已回读");
      refresh();
    } catch (error) {
      Message.error(zlmErrorPresentation(error).label);
    }
  });
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
    void router.replace("/gb28181/zlm/nodes");
    return;
  }
  refresh();
}

function formatTime(value?: string) {
  if (!value || value.startsWith("0001-01-01")) return "—";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toLocaleString();
}
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat zlm-detail-shell">
      <div class="node-detail">
        <header class="detail-toolbar">
          <div class="node-identity">
            <a-button class="back-btn" aria-label="返回节点列表" @click="router.push('/gb28181/zlm/nodes')"><template #icon><icon-left /></template></a-button>
            <div class="identity-main">
              <div class="identity-title-row">
                <strong>{{ node?.name || "节点详情" }}</strong>
                <LifecycleDot v-if="node" :state="node.state" />
                <HealthBadge v-if="node" :health="healthOf(node)" :reason="healthReason(node)" />
              </div>
              <div class="identity-subtitle">
                <span>{{ node ? `${node.host}:${node.apiPort}` : "正在加载连接信息" }}</span>
                <span v-if="node?.mediaServerUUID">UUID {{ node.mediaServerUUID }}</span>
              </div>
            </div>
          </div>
          <div class="detail-actions">
            <a-button v-if="canManage && node?.state === 'offline'" type="primary" :loading="opLoading.reprobe" @click="reprobe">重新探测</a-button>
            <a-button v-if="canManage && node?.state === 'active'" @click="openAction('maintenance')">切维护</a-button>
            <a-button v-if="canManage && node?.state === 'maintenance'" type="primary" :loading="opLoading.activate" @click="activate">激活</a-button>
            <a-button v-if="canManage && node" @click="editVisible = true">编辑连接</a-button>
            <a-dropdown v-if="node && canShowMore" trigger="click" position="br">
              <a-button>更多<template #icon><icon-down /></template></a-button>
              <template #content>
                <a-doption v-if="canKick && node.state !== 'offline'" @click="openAction('kick')">驱逐全部会话</a-doption>
                <a-doption v-if="canRestart && node.state !== 'offline'" @click="openAction('restart')">重启 ZLM 服务</a-doption>
                <a-doption v-if="canManage" class="danger" @click="openAction('delete')">删除节点</a-doption>
              </template>
            </a-dropdown>
            <a-button class="uvp-refresh-btn" :loading="loading" aria-label="刷新节点详情" @click="refresh"><template #icon><icon-refresh /></template></a-button>
          </div>
        </header>

        <div v-if="node?.recoveryRequired" class="detail-status detail-status--danger" role="alert">
          <strong>节点处于恢复隔离</strong>
          <span>{{ node.recoveryReason || "候选更新回滚未确认，节点暂不参与调度。" }}</span>
        </div>
        <div v-else-if="loadError && node" class="detail-status detail-status--warning" role="status">
          本次刷新失败：{{ errorPresentation.label }}。已保留上一次节点详情。
        </div>

        <div v-if="loading && !node" class="detail-state" role="status" aria-label="正在加载节点详情"><a-spin /><span>正在加载节点详情…</span></div>
        <div v-else-if="loadError && !node" class="detail-state detail-state--error" role="alert">
          <strong>{{ errorPresentation.label }}</strong>
          <span>{{ errorPresentation.retryable ? "可以刷新重试。" : "请确认访问权限。" }}</span>
          <a-button v-if="errorPresentation.retryable" @click="refresh">重新加载</a-button>
        </div>

        <a-tabs v-else-if="node" v-model:active-key="activeTab" class="detail-tabs">
          <a-tab-pane key="overview" title="概览">
            <div class="freshness-row">
              <span :class="`freshness freshness--${freshness.tone}`">{{ freshness.label }}</span>
              <span>{{ freshness.description }}</span>
              <a-link @click="router.push({ path: '/gb28181/zlm/streams', query: { nodeId: String(node.id) } })">查看此节点流</a-link>
            </div>

            <section class="kpi-grid" aria-label="节点关键指标">
              <StatCard title="活跃流" :value="node.stats?.mediaSourceCount ?? 0" trend="MediaSource" accent="brand">
                <template #spark><Sparkline :data="streamHistory" color="brand" :width="80" :height="24" fill /></template>
              </StatCard>
              <StatCard title="会话数" :value="node.stats?.sessionCount ?? 0" trend="节点登记会话" accent="accent">
                <template #spark><Sparkline :data="sessionHistory" color="accent" :width="80" :height="24" fill /></template>
              </StatCard>
              <StatCard title="综合线程负载" :value="cpuPercent" unit="%" trend="Net×0.6 + Work×0.4" :accent="cpuPercent >= 80 ? 'danger' : cpuPercent >= 60 ? 'warning' : 'default'" />
              <StatCard title="内存使用" :value-text="formatZLMBytes(node.stats?.memoryUsageBytes)" trend="节点心跳值" />
            </section>

            <section class="trend-grid">
              <div class="trend-card">
                <div><strong>流数趋势</strong><span>最近 {{ streamHistory.length }}/{{ HISTORY_MAX }} 个可见采样</span></div>
                <Sparkline v-if="streamHistory.length >= 2" :data="streamHistory" color="brand" :width="600" :height="110" fill />
                <p v-else>至少需要两个采样点；切换节点或编辑连接时旧趋势不会串入。</p>
              </div>
              <div class="trend-card">
                <div><strong>线程负载趋势</strong><span>节点切换会清空旧节点数据</span></div>
                <Sparkline v-if="cpuHistory.length >= 2" :data="cpuHistory" color="warning" :width="600" :height="110" fill />
                <p v-else>正在等待下一次运行态采样。</p>
              </div>
            </section>

            <section class="info-block" aria-labelledby="node-info-title">
              <h2 id="node-info-title">节点信息</h2>
              <dl class="info-grid">
                <div><dt>ID / revision</dt><dd>#{{ node.id }} / {{ node.revision }}</dd></div>
                <div><dt>UUID</dt><dd class="mono">{{ node.mediaServerUUID || "—" }}</dd></div>
                <div><dt>管理地址</dt><dd class="mono">{{ node.host }}:{{ node.apiPort }}</dd></div>
                <div><dt>设备收流地址</dt><dd class="mono">{{ node.receiveHost || node.host }}</dd></div>
                <div><dt>播放访问地址</dt><dd class="mono">{{ node.playbackHost || node.host }}</dd></div>
                <div><dt>RTP 端口</dt><dd>{{ node.rtpPortStart }}-{{ node.rtpPortEnd }}（{{ rtpUsage.used }}/{{ rtpUsage.total }}，{{ rtpUsage.percent }}%）</dd></div>
                <div><dt>调度状态</dt><dd>{{ node.autoOnDemandReady ? `已收敛，权重 ${node.weight}` : "等待配置收敛，不应承接新流" }}</dd></div>
                <div><dt>最后心跳</dt><dd>{{ formatTime(node.stats?.lastHeartbeatAt) }}</dd></div>
                <div><dt>创建时间</dt><dd>{{ formatTime(node.createdAt) }}</dd></div>
                <div><dt>更新时间</dt><dd>{{ formatTime(node.updatedAt) }}</dd></div>
              </dl>
            </section>
          </a-tab-pane>
          <a-tab-pane key="config" title="配置"><NodeConfig :node-id="node.id" /></a-tab-pane>
        </a-tabs>

        <NodeForm v-model:visible="editVisible" :node="node" @saved="refresh" />
        <ZLMNodeActionDialog v-model:visible="actionVisible" :node="node" :action="action" @done="actionDone" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.zlm-detail-shell { padding: 4px 8px; overflow: hidden; }
.node-detail { width: 100%; height: 100%; overflow: auto; box-sizing: border-box; color: var(--zlm-text-2); }
.detail-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.node-identity, .identity-title-row, .identity-subtitle, .detail-actions, .freshness-row { display: flex; align-items: center; gap: 9px; }
.node-identity { min-width: 0; flex: 1; }
.back-btn { width: 38px; min-width: 38px; padding: 0; }
.identity-main { min-width: 0; }
.identity-title-row strong { overflow: hidden; color: var(--zlm-text-1); font-size: 17px; text-overflow: ellipsis; white-space: nowrap; }
.identity-subtitle { margin-top: 3px; color: var(--zlm-text-3); font-family: var(--zlm-font-mono); font-size: 11px; }
.detail-actions { flex-wrap: wrap; justify-content: flex-end; }
.detail-status { display: flex; gap: 10px; margin-bottom: 14px; padding: 10px 13px; border: 1px solid; border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }
.detail-status--warning { color: var(--zlm-warn-600); background: var(--zlm-warn-50); border-color: var(--zlm-warn-500); }
.detail-status--danger { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
.detail-state { display: flex; min-height: 280px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); }
.detail-state--error { color: var(--zlm-danger-600); }
.detail-tabs :deep(.arco-tabs-nav) { margin-bottom: 16px; border-bottom: 1px solid var(--uvp-panel-border); }
.freshness-row { margin-bottom: 14px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.freshness-row .arco-link { margin-left: auto; }
.freshness--success { color: var(--zlm-success-600); }
.freshness--warning { color: var(--zlm-warn-600); }
.freshness--danger { color: var(--zlm-danger-600); }
.kpi-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
.trend-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-top: 16px; }
.trend-card, .info-block { padding: 16px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); border-radius: var(--uvp-panel-radius); box-shadow: var(--uvp-panel-shadow); }
.trend-card > div { display: flex; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
.trend-card strong, .info-block h2 { color: var(--zlm-text-1); font-size: 14px; }
.trend-card span, .trend-card p { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.trend-card svg { width: 100%; }
.info-block { margin-top: 16px; }
.info-block h2 { margin: 0 0 10px; }
.info-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; column-gap: 24px; }
.info-grid div { display: grid; grid-template-columns: 120px minmax(0, 1fr); gap: 12px; padding: 10px 0; border-bottom: 1px solid var(--uvp-divider); }
.info-grid dt { color: var(--zlm-text-3); }
.info-grid dd { min-width: 0; margin: 0; color: var(--zlm-text-1); overflow-wrap: anywhere; }
.mono { font-family: var(--zlm-font-mono); }
:deep(.danger) { color: var(--zlm-danger-600); }
:deep(.arco-btn) { border-radius: 10px; }
@media (max-width: 1120px) { .kpi-grid, .trend-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 720px) { .detail-toolbar, .node-identity, .freshness-row { align-items: flex-start; flex-direction: column; } .detail-actions { justify-content: flex-start; } .kpi-grid, .trend-grid, .info-grid { grid-template-columns: 1fr; } .info-grid div { grid-template-columns: 1fr; gap: 3px; } .freshness-row .arco-link { margin-left: 0; } }
</style>
