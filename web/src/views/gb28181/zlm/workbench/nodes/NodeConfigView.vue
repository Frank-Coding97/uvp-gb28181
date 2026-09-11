<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, toRef, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { RotateCw, ServerCog, ShieldAlert } from "lucide-vue-next";

import {
  getZLMNodeRestartStatus,
  restartZLMNode,
  type ZLMRestartOperation,
  type ZLMRestartStatus
} from "@/api/gb28181-zlm";
import { useUserStoreHook } from "@/store/modules/user";
import NodeConfigPanel from "../../components/NodeConfigPanel.vue";
import { zlmErrorPresentation } from "../../components/zlmFormatters";

const props = withDefaults(defineProps<{
  nodeId: number;
  nodeName?: string;
  active?: boolean;
  editable?: boolean;
}>(), {
  nodeName: "媒体节点",
  active: true,
  editable: true
});

const emit = defineEmits<{
  dirtyChange: [dirty: boolean];
}>();

const userStore = useUserStoreHook();
const active = toRef(props, "active");
const configDirty = ref(false);
const pollingNotice = ref("");
const configPanel = ref<{ refresh: (options?: { force?: boolean }) => Promise<boolean> } | null>(null);
const restartOperation = ref<ZLMRestartOperation | null>(null);
const restartPolling = ref(false);
const restartError = ref("");
let configTimer: ReturnType<typeof setInterval> | null = null;
let restartTimer: ReturnType<typeof setTimeout> | null = null;
let restartGeneration = 0;

const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes(permission);
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

function refreshConfig() {
  if (!active.value) return;
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
  configTimer = null;
  if (!active.value) return;
  configTimer = setInterval(refreshConfig, 15_000);
}

function stopConfigPolling() {
  if (configTimer) clearInterval(configTimer);
  configTimer = null;
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
    if (generation !== restartGeneration || !active.value) return;
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
    if (generation !== restartGeneration || !active.value) return;
    restartPolling.value = false;
    restartError.value = zlmErrorPresentation(error).label;
  }
}

async function performRestart() {
  const nodeId = props.nodeId;
  if (!nodeId || !canRestart.value || configDirty.value || restartPolling.value || !active.value) return;
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
  if (!operation || !active.value || restartPolling.value || operation.status === "ready" || operation.status === "failed" || operation.status === "unknown") return;
  stopRestartPolling();
  const generation = restartGeneration;
  restartError.value = "";
  restartPolling.value = true;
  void pollRestart(operation.nodeId, operation.operationId, generation);
}

function requestRestart() {
  if (!props.nodeId || !canRestart.value || configDirty.value || restartPolling.value || !active.value) return;
  Modal.warning({
    title: "重启媒体节点",
    content: `将重启“${props.nodeName}”，该节点全部媒体会话会中断。后端受理后页面会持续等待离线、心跳恢复和配置收敛。`,
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
watch([() => props.nodeId, active], ([nodeId, isActive], previous) => {
  if (nodeId === previous?.[0] && isActive === previous?.[1]) return;
  stopRestartPolling();
  restartOperation.value = null;
  restartError.value = "";
  pollingNotice.value = "";
  if (isActive) {
    startConfigPolling();
    void configPanel.value?.refresh({ force: true });
  } else stopConfigPolling();
});

onMounted(() => startConfigPolling());
onBeforeUnmount(() => {
  stopConfigPolling();
  stopRestartPolling();
});
</script>

<template>
  <section class="node-config-view" aria-label="节点服务配置">
    <header class="config-heading"><div><h2>服务配置</h2><p>只允许热更新项进入提交；保存结果必须经过 ZLMediaKit 实际值回读。Secret 不会回显。</p></div><a-button v-if="canRestart" status="danger" :loading="restartPolling" :disabled="!active || configDirty || restartPolling" @click="requestRestart"><template #icon><RotateCw :size="15" /></template>重启当前节点</a-button></header>
    <a-alert v-if="pollingNotice" type="warning" class="config-page-alert">{{ pollingNotice }}</a-alert>
    <section v-if="restartOperation || restartError" :class="['restart-state', `restart-state--${restartView.tone}`]">
      <div class="restart-state__icon"><ServerCog :size="20" /></div>
      <div><strong>{{ restartView.label }}</strong><p>{{ restartError || restartView.description }}</p><code v-if="restartOperation">operation {{ restartOperation.operationId }} · {{ restartOperation.status }}</code></div>
      <a-spin v-if="restartPolling" /><a-button v-else-if="restartError && restartOperation && !restartView.terminal" @click="resumeRestartPolling">继续查询</a-button>
    </section>
    <a-alert v-if="!editable" type="info" class="config-page-alert"><ShieldAlert :size="15" />当前账号可查看配置，但没有热更新权限。</a-alert>
    <NodeConfigPanel ref="configPanel" :node-id="nodeId" :editable="editable" @dirty-change="value => { configDirty = value; emit('dirtyChange', value); }" @polling-skipped="pollingNotice = '存在未保存草稿时，自动轮询与节点切换都会暂停。'" />
  </section>
</template>

<style scoped>
.node-config-view { min-width: 0; color: var(--zlm-text-2); }.config-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 14px; }.config-heading h2 { margin: 0; color: var(--zlm-text-1); font-size: 17px; }.config-heading p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.config-page-alert { margin-bottom: 12px; }.restart-state { display: flex; align-items: center; gap: 12px; margin: 14px 0; padding: 13px 15px; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.restart-state__icon { display: grid; width: 38px; height: 38px; flex: none; color: var(--zlm-brand-600); background: var(--zlm-brand-50); border-radius: var(--zlm-radius-md); place-items: center; }.restart-state > div:nth-child(2) { min-width: 0; flex: 1; }.restart-state strong { color: var(--zlm-text-1); }.restart-state p { margin: 3px 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.restart-state code { color: var(--zlm-text-4); font-family: var(--zlm-font-mono); font-size: 11px; }.restart-state--warning { border-color: var(--zlm-warn-500); }.restart-state--success { border-color: var(--zlm-success-500); }.restart-state--danger { border-color: var(--zlm-danger-500); }
@media (max-width: 760px) { .config-heading { flex-direction: column; } }
</style>
