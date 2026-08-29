<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  deleteZLMNode,
  kickZLMNodeSessions,
  restartZLMNode,
  setZLMNodeMaintenance,
  type ZLMNode,
  type ZLMNodeImpactPreflight
} from "@/api/gb28181-zlm";
import { useZLMContextStore, type ZLMContextNode } from "@/store/modules/zlm-context";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import {
  isNodeImpactPreflight,
  nodeActionCopy,
  nodeImpactItems,
  nodeImpactItemsFromError,
  type NodeDangerAction,
  type NodeImpactAction
} from "./nodeActionState";

const props = defineProps<{
  visible: boolean;
  node: ZLMNode | null;
  action: NodeDangerAction | null;
}>();

const emit = defineEmits<{
  "update:visible": [visible: boolean];
  done: [payload: { action: NodeDangerAction; operationId?: string }];
}>();

const context = useZLMContextStore();
const preflight = ref<ZLMNodeImpactPreflight | null>(null);
const preflightLoading = ref(false);
const executing = ref(false);
const loadError = ref("");
let generation = 0;

const copy = computed(() => props.node && props.action ? nodeActionCopy(props.action, props.node.name) : null);
const dialogReady = computed(() => {
  if (!props.visible || !props.node || !props.action || loadError.value || preflightLoading.value) return false;
  return props.action === "restart" || preflight.value !== null;
});
const fingerprint = computed(() => preflight.value?.fingerprint ?? "");
const impacts = computed(() => {
  if (preflight.value) return nodeImpactItems(preflight.value);
  if (props.action === "restart" && props.node) {
    return [
      `当前登记流 ${props.node.stats?.mediaSourceCount ?? 0} 路`,
      `当前登记会话 ${props.node.stats?.sessionCount ?? 0} 个`,
      "重启仅表示后端已受理；完成状态以离线、心跳恢复和配置收敛为准"
    ];
  }
  return [];
});

function ensureContextNode(node: ZLMNode) {
  const candidates = [
    ...context.visibleNodes.filter((item: ZLMContextNode) => item.id !== node.id),
    { id: node.id, name: node.name, state: node.state }
  ];
  if (!context.initialized) context.initialize(candidates, node.id);
  else context.reconcileVisibleNodes(candidates);
  context.selectNode(node.id);
}

async function requestPreflight(action: NodeImpactAction, nodeId: number) {
  switch (action) {
    case "maintenance":
      return setZLMNodeMaintenance(nodeId);
    case "kick":
      return kickZLMNodeSessions(nodeId);
    case "delete":
      return deleteZLMNode(nodeId);
  }
}

async function loadPreflight() {
  const node = props.node;
  const action = props.action;
  if (!props.visible || !node || !action) return;
  const currentGeneration = ++generation;
  ensureContextNode(node);
  preflight.value = null;
  loadError.value = "";
  if (action === "restart") return;

  preflightLoading.value = true;
  try {
    const response = await requestPreflight(action, node.id);
    if (currentGeneration !== generation || !props.visible) return;
    if (response.code !== 0 || !isNodeImpactPreflight(response.data, node.id, action)) {
      throw new Error(response.message || "后端未返回可确认的影响指纹，已停止执行");
    }
    preflight.value = response.data;
  } catch (error) {
    if (currentGeneration !== generation || !props.visible) return;
    loadError.value = (error as { response?: { data?: { message?: string } }; message?: string })?.response?.data?.message
      || (error as Error)?.message
      || "影响预检失败";
  } finally {
    if (currentGeneration === generation) preflightLoading.value = false;
  }
}

watch(
  () => [props.visible, props.node?.id, props.action] as const,
  ([visible]) => {
    if (visible) void loadPreflight();
    else {
      generation += 1;
      preflight.value = null;
      loadError.value = "";
      preflightLoading.value = false;
      executing.value = false;
    }
  },
  { immediate: true }
);

function close() {
  if (executing.value) return;
  emit("update:visible", false);
}

async function confirm(payload: { fingerprint: string }) {
  const node = props.node;
  const action = props.action;
  if (!node || !action) return;
  executing.value = true;
  try {
    if (action === "restart") {
      const response = await restartZLMNode(node.id, 5000);
      if (response.code !== 0) throw new Error(response.message || "操作失败");
      const operationId = response.data?.operationId;
      Message.success(operationId ? `重启已受理，操作号 ${operationId}` : "重启已受理，等待节点状态推进");
      emit("done", { action, operationId });
      emit("update:visible", false);
      return;
    }

    let response;
    switch (action) {
      case "maintenance":
        response = await setZLMNodeMaintenance(node.id, payload.fingerprint);
        break;
      case "kick":
        response = await kickZLMNodeSessions(node.id, payload.fingerprint);
        break;
      case "delete":
        response = await deleteZLMNode(node.id, payload.fingerprint);
        break;
    }
    if (response.code !== 0) throw new Error(response.message || "操作失败");
    Message.success(copy.value?.actionLabel || "操作已执行");
    emit("done", { action });
    emit("update:visible", false);
  } catch (error) {
    const status = (error as { response?: { status?: number } })?.response?.status;
    const message = (error as { response?: { data?: { message?: string } }; message?: string })?.response?.data?.message
      || (error as Error)?.message
      || "操作失败";
    if (status === 409) {
      const latestImpacts = nodeImpactItemsFromError(error);
      const impactText = latestImpacts.length ? `；后端返回影响：${latestImpacts.join("，")}` : "";
      Message.error(`${message}${impactText}；影响已变化，请重新打开确认`);
      emit("update:visible", false);
    } else {
      Message.error(message);
    }
  } finally {
    executing.value = false;
  }
}
</script>

<template>
  <a-modal
    v-if="visible && !dialogReady"
    :visible="visible"
    :footer="false"
    :mask-closable="false"
    :esc-to-close="!preflightLoading"
    :closable="!preflightLoading"
    modal-class="uvp-system-dialog"
    @cancel="close"
  >
    <template #title>获取节点影响</template>
    <div v-if="preflightLoading" class="preflight-state" role="status">
      <a-spin />
      <span>正在从后端读取活动流、录制和会话影响…</span>
    </div>
    <div v-else class="preflight-state preflight-state--error" role="alert">
      <strong>未执行任何管理动作</strong>
      <span>{{ loadError }}</span>
      <div>
        <a-button @click="close">取消</a-button>
        <a-button type="primary" @click="loadPreflight">重新预检</a-button>
      </div>
    </div>
  </a-modal>

  <ZLMDangerActionDialog
    v-if="node && action && copy"
    :visible="dialogReady"
    :node-id="node.id"
    :node-name="node.name"
    :target-key="`node:${node.id}:${action}`"
    :target-label="copy.targetLabel"
    :fingerprint="fingerprint"
    :impacts="impacts"
    :confirm-phrase="copy.confirmPhrase"
    :require-reason="false"
    :action-label="copy.actionLabel"
    :busy="executing"
    @update:visible="value => !value && close()"
    @confirm="confirm"
    @stale="close"
  />
</template>

<style scoped>
.preflight-state { display: flex; min-height: 150px; flex-direction: column; align-items: center; justify-content: center; gap: 12px; color: var(--zlm-text-3); text-align: center; }
.preflight-state--error { color: var(--zlm-danger-600); }
.preflight-state--error div { display: flex; gap: 8px; margin-top: 8px; }
</style>
