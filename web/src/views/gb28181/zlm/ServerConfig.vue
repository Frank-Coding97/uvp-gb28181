<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message, Modal } from "@arco-design/web-vue";
import { RotateCw, ServerCog, ShieldAlert } from "lucide-vue-next";
import {
  getZLMNodeRestartStatus,
  listZLMNodes,
  restartZLMNode,
  type ZLMNode,
  type ZLMRestartOperation,
  type ZLMRestartStatus
} from "@/api/gb28181-zlm";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import NodeConfigPanel from "./components/NodeConfigPanel.vue";
import ZLMNodeContextBar from "./components/ZLMNodeContextBar.vue";
import { zlmErrorPresentation } from "./components/zlmFormatters";

const route = useRoute();
const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const nodesError = ref("");
const configDirty = ref(false);
const pollingNotice = ref("");
const configPanel = ref<{ refresh: (options?: { force?: boolean }) => Promise<boolean> } | null>(null);
const restartOperation = ref<ZLMRestartOperation | null>(null);
const restartPolling = ref(false);
const restartError = ref("");
let configTimer: ReturnType<typeof setInterval> | null = null;
let restartTimer: ReturnType<typeof setTimeout> | null = null;
let restartGeneration = 0;

const selectedNodeId = computed(() => context.selectedNodeId);
const selectedNodeName = computed(() => context.selectedNode?.name ?? `节点 #${selectedNodeId.value ?? "—"}`);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canUpdate = computed(() => hasPermission("gb28181:zlm:config:update"));
const canRestart = computed(() => hasPermission("gb28181:zlm:restart"));
const restartView = computed(() => restartStatusPresentation(restartOperation.value?.status ?? null));

function restartStatusPresentation(status: ZLMRestartStatus | null) {
  const presentations: Record<ZLMRestartStatus, { label: string; description: string; tone: string; terminal: boolean }> = {
    unknown: { label: "操作状态未知", description: "服务进程可能已重启或操作记录已丢失，不能据此判断节点已恢复。", tone: "danger", terminal: true },
    accepted: { label: "已受理", description: "后端已受理，不代表节点已经恢复。", tone: "warning", terminal: false },
    waiting_offline: { label: "等待节点离线", description: "等待心跳 watcher 确认旧进程离线。", tone: "warning", terminal: false },
    waiting_heartbeat: { label: "等待心跳恢复", description: "旧进程已离线，等待新进程心跳。", tone: "warning", terminal: false },
    converging: { label: "配置收敛中", description: "心跳已恢复，正在下发并回读平台管理配置。", tone: "warning", terminal: false },
    ready: { label: "已恢复", description: "节点心跳和配置收敛均已确认。", tone: "success", terminal: true },
    failed: { label: "恢复失败", description: "重启、心跳恢复或配置收敛失败，请查看错误并重试。", tone: "danger", terminal: true }
  };
  return status ? presentations[status] : { label: "未执行重启", description: "重启会中断该节点所有媒体会话。", tone: "neutral", terminal: true };
}

async function loadNodes() {
  nodesLoading.value = true;
  nodesError.value = "";
  try {
    const response = await listZLMNodes();
    if (response.code !== 0) throw new Error(response.message || "媒体节点加载失败");
    nodes.value = response.data?.list ?? [];
  } catch (error) {
    nodesError.value = zlmErrorPresentation(error).label;
  } finally {
    nodesLoading.value = false;
  }
}

function refreshConfig() {
  pollingNotice.value = "";
  if (configDirty.value) {
    pollingNotice.value = "存在未保存草稿时，自动轮询与节点切换都会暂停。";
    return;
  }
  if (restartPolling.value) {
    pollingNotice.value = "重启恢复阶段暂不刷新配置，待配置收敛确认后再回读。";
    return;
  }
  void configPanel.value?.refresh();
}

function startConfigPolling() {
  if (configTimer) clearInterval(configTimer);
  configTimer = setInterval(refreshConfig, 15_000);
}

function stopRestartPolling() {
  restartGeneration += 1;
  if (restartTimer) clearTimeout(restartTimer);
  restartTimer = null;
  restartPolling.value = false;
}

async function pollRestart(nodeId: number, operationId: string, generation: number) {
  try {
    const response = await getZLMNodeRestartStatus(nodeId, operationId);
    if (generation !== restartGeneration) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "重启状态读取失败");
    const operation = response.data;
    restartOperation.value = operation;
    if (operation.status === "ready") {
      restartPolling.value = false;
      Message.success("媒体节点已恢复，心跳与配置收敛均已确认");
      void configPanel.value?.refresh({ force: true });
      return;
    }
    if (operation.status === "failed" || operation.status === "unknown") {
      restartPolling.value = false;
      restartError.value = restartStatusPresentation(operation.status).description;
      Message.error(restartError.value);
      return;
    }
    restartTimer = setTimeout(() => void pollRestart(nodeId, operationId, generation), 2_000);
  } catch (error) {
    if (generation !== restartGeneration) return;
    restartPolling.value = false;
    restartError.value = zlmErrorPresentation(error).label;
  }
}

async function performRestart() {
  const nodeId = selectedNodeId.value;
  if (!nodeId || !canRestart.value || configDirty.value || restartPolling.value) return;
  stopRestartPolling();
  const generation = restartGeneration;
  restartError.value = "";
  try {
    const response = await restartZLMNode(nodeId);
    if (response.code !== 0 || !response.data?.accepted || !response.data.operationId) throw new Error(response.message || "重启未被后端受理");
    restartOperation.value = {
      operationId: response.data.operationId,
      nodeId,
      generation: 0,
      status: response.data.status,
      acceptedAt: new Date().toISOString(),
      updatedAt: new Date().toISOString()
    };
    restartPolling.value = true;
    Message.info("重启已受理，正在等待离线、心跳恢复和配置收敛");
    void pollRestart(nodeId, response.data.operationId, generation);
  } catch (error) {
    restartError.value = zlmErrorPresentation(error).label;
  }
}

function resumeRestartPolling() {
  const operation = restartOperation.value;
  if (!operation || restartPolling.value || operation.status === "ready" || operation.status === "failed" || operation.status === "unknown") return;
  stopRestartPolling();
  const generation = restartGeneration;
  restartError.value = "";
  restartPolling.value = true;
  void pollRestart(operation.nodeId, operation.operationId, generation);
}

function requestRestart() {
  if (!selectedNodeId.value || !canRestart.value || configDirty.value || restartPolling.value) return;
  Modal.warning({
    title: "重启媒体节点",
    content: `将重启“${selectedNodeName.value}”，该节点全部媒体会话会中断。后端受理后页面会持续等待离线、心跳恢复和配置收敛。`,
    okText: "确认重启",
    cancelText: "取消",
    hideCancel: false,
    okButtonProps: { status: "danger" },
    onOk: performRestart
  });
}

watch(configDirty, dirty => {
  pollingNotice.value = dirty ? "存在未保存草稿时，自动轮询与节点切换都会暂停。" : "";
});
watch(selectedNodeId, (nodeId, previous) => {
  if (nodeId === previous) return;
  stopRestartPolling();
  restartOperation.value = null;
  restartError.value = "";
  if (nodeId) void router.replace({ query: { ...route.query, nodeId: String(nodeId) } });
});

onMounted(() => {
  void loadNodes();
  startConfigPolling();
});
onBeforeUnmount(() => {
  if (configTimer) clearInterval(configTimer);
  stopRestartPolling();
});
</script>

<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat server-config-page">
      <header class="config-heading">
        <div><h1>服务器配置</h1><p>只允许热更新项进入提交；保存结果必须经过 ZLMediaKit 实际值回读。</p></div>
        <a-button v-if="canRestart" status="danger" :loading="restartPolling" :disabled="!selectedNodeId || configDirty || restartPolling" @click="requestRestart"><template #icon><RotateCw :size="15" /></template>重启当前节点</a-button>
      </header>

      <ZLMNodeContextBar
        :nodes="contextNodes"
        :query-node-id="route.query.nodeId"
        :loading="nodesLoading"
        :disabled="configDirty || restartPolling"
        title="配置所属节点"
        @refresh="loadNodes(); refreshConfig()"
      />

      <a-alert v-if="nodesError" type="error" class="config-page-alert"><ShieldAlert :size="15" />{{ nodesError }}</a-alert>
      <a-alert v-else-if="pollingNotice" type="warning" class="config-page-alert">{{ pollingNotice }}</a-alert>

      <section v-if="restartOperation || restartError" :class="['restart-state', `restart-state--${restartView.tone}`]">
        <div class="restart-state__icon"><ServerCog :size="20" /></div>
        <div><strong>{{ restartView.label }}</strong><p>{{ restartError || restartView.description }}</p><code v-if="restartOperation">operation {{ restartOperation.operationId }} · {{ restartOperation.status }}</code></div>
        <a-spin v-if="restartPolling" />
        <a-button v-else-if="restartError && restartOperation && !restartView.terminal" @click="resumeRestartPolling">继续查询</a-button>
      </section>

      <div v-if="nodesLoading && nodes.length === 0" class="config-empty"><a-spin />正在加载媒体节点…</div>
      <div v-else-if="nodes.length === 0" class="config-empty"><ServerCog :size="36" /><strong>没有可见媒体节点</strong></div>
      <div v-else-if="!selectedNodeId" class="config-empty"><ServerCog :size="36" /><strong>请选择媒体节点</strong></div>
      <NodeConfigPanel
        v-else
        ref="configPanel"
        :node-id="selectedNodeId"
        :editable="canUpdate"
        @dirty-change="configDirty = $event"
        @polling-skipped="pollingNotice = '存在未保存草稿时，自动轮询与节点切换都会暂停。'"
      />
    </div>
  </div>
</template>

<style scoped>
.server-config-page { box-sizing: border-box; height: 100%; padding: 4px 8px 24px; overflow: auto; color: var(--zlm-text-2); }.config-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 16px; }.config-heading h1 { margin: 0; color: var(--zlm-text-1); font-size: 20px; }.config-heading p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.config-page-alert { margin-top: 12px; }.restart-state { display: flex; align-items: center; gap: 12px; margin: 14px 0; padding: 13px 15px; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.restart-state__icon { display: grid; width: 38px; height: 38px; flex: none; color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-radius: var(--zlm-radius-md); place-items: center; }.restart-state > div:nth-child(2) { min-width: 0; flex: 1; }.restart-state strong { color: var(--zlm-text-1); }.restart-state p { margin: 3px 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.restart-state code { color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 11px; }.restart-state--warning { border-color: var(--zlm-warn-500); }.restart-state--success { border-color: var(--zlm-success-500); }.restart-state--danger { border-color: var(--zlm-danger-500); }.config-empty { display: flex; min-height: 320px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; margin-top: 14px; color: var(--zlm-text-3); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.server-config-page :deep(.node-config-panel) { margin-top: 14px; }
@media (max-width: 760px) { .config-heading { flex-direction: column; } }
</style>
