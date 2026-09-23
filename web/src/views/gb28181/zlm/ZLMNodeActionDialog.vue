<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  deleteZLMNode,
  kickZLMNodeSessions,
  purgeUnreachableZLMNode,
  restartZLMNode,
  setZLMNodeMaintenance,
  type ZLMNode,
  type ZLMNodeImpactPreflight
} from "@/api/gb28181-zlm";
import { useZLMContextStore, type ZLMContextNode } from "@/store/modules/zlm-context";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import { zlmErrorPresentation } from "./components/zlmFormatters";
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
const purgeConfirming = ref(false);
const purging = ref(false);
let generation = 0;

// 只有「删除」且预检确实失败时才给强制移除的出口:它用于处理节点不可达时
// 无法读取影响、普通删除无法继续的死角。
const canPurge = computed(() => props.action === "delete" && !!props.node && !!loadError.value);

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
    loadError.value = zlmErrorPresentation(error).label;
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
      purgeConfirming.value = false;
      purging.value = false;
    }
  },
  { immediate: true }
);

function close() {
  if (executing.value || purging.value) return;
  emit("update:visible", false);
}

async function purgeUnreachable() {
  const node = props.node;
  if (!node) return;
  purging.value = true;
  try {
    const response = await purgeUnreachableZLMNode(node.id);
    if (response.code !== 0) throw new Error(response.message || "强制移除失败");
    const detached = response.data?.detachedRows ?? 0;
    Message.success(`已移除节点「${node.name}」，${detached} 个设备的首选节点已重置为自动调度`);
    emit("done", { action: "delete" });
    emit("update:visible", false);
  } catch (error) {
    // 409 = 此刻节点其实是可读的,后端拒绝了 force —— 这恰恰是我们要的保护。
    const message = zlmErrorPresentation(error).label;
    loadError.value = message;
    Message.error(message);
    purgeConfirming.value = false;
  } finally {
    purging.value = false;
  }
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
    const message = zlmErrorPresentation(error).label;
    if (status === 409) {
      if (action === "delete") {
        Message.warning(`${message}；节点已停用，等待活动流、录制和会话排空后重试删除`);
        emit("done", { action });
        emit("update:visible", false);
        return;
      }
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
      <div v-if="!purgeConfirming">
        <a-button @click="close">取消</a-button>
        <a-button type="primary" @click="loadPreflight">重新预检</a-button>
        <a-button
          v-if="canPurge"
          status="danger"
          data-test="purge-entry"
          @click="purgeConfirming = true"
        >强制移除</a-button>
      </div>
      <template v-if="canPurge">
        <p v-if="!purgeConfirming" class="purge-hint">
          节点不可达时后端读不到它上面的流和会话，普通删除无法完成影响确认。
        </p>
        <div v-else class="purge-confirm">
          <strong>确认强制移除该节点？</strong>
          <span>后端会先探一次：此刻如果读得通就会拒绝删除。</span>
          <span>由于读不到影响，它上面是否还有流、录制和级联会话是未知的；重新上线后需手动清理残留。</span>
          <span>指向它的设备「首选节点」会被重置为自动调度。</span>
          <div>
            <a-button :disabled="purging" @click="purgeConfirming = false">返回</a-button>
            <a-button status="danger" data-test="purge-confirm" :loading="purging" @click="purgeUnreachable">确认移除</a-button>
          </div>
        </div>
      </template>
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
    :require-confirm-phrase="action !== 'delete'"
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
.preflight-state--error div { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; margin-top: 8px; }
.purge-hint { max-width: 420px; margin: 4px 0 0; color: var(--zlm-text-3); font-size: 12px; line-height: 1.6; }
.purge-confirm { display: flex; flex-direction: column; align-items: center; gap: 6px; margin-top: 8px; max-width: 460px; }
.purge-confirm span { color: var(--zlm-text-3); font-size: 12px; line-height: 1.6; }
.purge-confirm strong { color: var(--zlm-danger-600); font-size: 13px; }
</style>
