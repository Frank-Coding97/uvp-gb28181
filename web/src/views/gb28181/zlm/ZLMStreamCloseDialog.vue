<script setup lang="ts">
import { computed, ref, watch } from "vue";
import {
  closeZLMStream,
  closeZLMStreams,
  forceCloseZLMStream,
  preflightCloseZLMStream,
  preflightCloseZLMStreams,
  type ZLMOwnershipBatchPreflight,
  type ZLMOwnershipSnapshot,
  type ZLMOwnershipTarget,
  type ZLMStreamClosePreflight,
  type ZLMStreamOwnership
} from "@/api/gb28181-zlm-runtime";
import ZLMDangerActionDialog from "./components/ZLMDangerActionDialog.vue";
import { zlmErrorPresentation } from "./components/zlmFormatters";
import { streamCloseDecision, streamIdentityKey, type StreamCloseDecision } from "./streamManagementState";

const props = withDefaults(defineProps<{
  visible: boolean;
  nodeName: string;
  targets: ZLMOwnershipTarget[];
  force?: boolean;
  canForce?: boolean;
}>(), {
  force: false,
  canForce: false
});

const emit = defineEmits<{
  "update:visible": [visible: boolean];
  done: [result: { closed: number; alreadyAbsent: number; partial: boolean; uncertain: boolean }];
}>();

const loading = ref(false);
const busy = ref(false);
const loadError = ref<unknown>(null);
const singlePreflight = ref<ZLMStreamClosePreflight | null>(null);
const batchPreflight = ref<ZLMOwnershipBatchPreflight | null>(null);
const decision = ref<StreamCloseDecision | null>(null);
let generation = 0;

const firstTarget = computed(() => props.targets[0] ?? null);
const fingerprint = computed(() => singlePreflight.value?.fingerprint ?? batchPreflight.value?.fingerprint ?? "");
const targetKey = computed(() => props.targets.map(target => streamIdentityKey(target.nodeId, target.media)).join("\u001e"));
const targetLabel = computed(() => {
  const first = firstTarget.value;
  if (!first) return "未选择媒体流";
  if (props.targets.length > 1) return `${props.targets.length} 路媒体流`;
  return `${first.media.schema}://${first.media.vhost}/${first.media.app}/${first.media.stream}`;
});
const confirmPhrase = computed(() => props.force
  ? `强制关闭 ${firstTarget.value?.media.stream ?? "媒体流"}`
  : props.targets.length > 1
    ? `关闭 ${props.targets.length} 路流`
    : `关闭 ${firstTarget.value?.media.stream ?? "媒体流"}`);
const errorPresentation = computed(() => zlmErrorPresentation(loadError.value));
const canConfirm = computed(() => Boolean(firstTarget.value && fingerprint.value && decision.value?.allowed && !loadError.value));

function isOwnershipSnapshot(snapshot: ZLMStreamOwnership | ZLMOwnershipSnapshot): snapshot is ZLMOwnershipSnapshot {
  return "target" in snapshot && "fingerprint" in snapshot;
}

function ownershipLines(snapshot: ZLMStreamOwnership | ZLMOwnershipSnapshot, prefix = "") {
  const lines = [`${prefix}后端归属：${snapshot.status}`];
  if (isOwnershipSnapshot(snapshot)) {
    for (const owner of snapshot.owners ?? []) lines.push(`${prefix}${owner.type}${owner.owner ? ` · ${owner.owner}` : ""}（${owner.confidence}）`);
    for (const impact of snapshot.impacts ?? []) lines.push(`${prefix}${impact.resourceType || "业务资源"}${impact.resourceKey ? ` · ${impact.resourceKey}` : ""}`);
  } else {
    for (const source of snapshot.sources ?? []) lines.push(`${prefix}${source.type}（${source.confidence}）`);
    for (const impact of snapshot.impacts ?? []) lines.push(`${prefix}${impact.type} × ${impact.count}`);
  }
  return lines;
}

const impactLines = computed(() => {
  if (singlePreflight.value) return ownershipLines(singlePreflight.value.snapshot);
  return (batchPreflight.value?.snapshots ?? []).flatMap((snapshot, index) => ownershipLines(snapshot, `第 ${index + 1} 路：`));
});

function batchDecision(preflight: ZLMOwnershipBatchPreflight): StreamCloseDecision {
  if (props.force) {
    return { allowed: false, mode: "blocked", reason: "后端没有批量强制关闭契约，请逐路预检并强制关闭。" };
  }
  const protectedCount = preflight.snapshots.filter(snapshot => snapshot.status !== "managed" && snapshot.status !== "absent").length;
  if (protectedCount > 0) {
    return {
      allowed: false,
      mode: "blocked",
      reason: `${protectedCount} 路被后端判定为业务持有、冲突或归属未知；普通批量关闭受保护。`
    };
  }
  return { allowed: true, mode: "normal", reason: "全部目标均由后端确认可执行普通批量关闭。" };
}

async function loadPreflight() {
  const currentGeneration = ++generation;
  singlePreflight.value = null;
  batchPreflight.value = null;
  decision.value = null;
  loadError.value = null;
  if (!props.visible || !firstTarget.value) return;
  loading.value = true;
  try {
    if (props.targets.length === 1) {
      const target = firstTarget.value;
      const response = await preflightCloseZLMStream(target.nodeId, target.media);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "流关闭预检失败");
      if (currentGeneration !== generation || !props.visible) return;
      singlePreflight.value = response.data;
      decision.value = streamCloseDecision(response.data, props.force, props.canForce);
    } else {
      const response = await preflightCloseZLMStreams(props.targets);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "批量关闭预检失败");
      if (currentGeneration !== generation || !props.visible) return;
      batchPreflight.value = response.data;
      decision.value = batchDecision(response.data);
    }
  } catch (error) {
    if (currentGeneration === generation) loadError.value = error;
  } finally {
    if (currentGeneration === generation) loading.value = false;
  }
}

watch(
  () => [props.visible, props.force, props.canForce, targetKey.value] as const,
  ([visible]) => {
    if (visible) void loadPreflight();
    else generation += 1;
  },
  { immediate: true }
);

function close() {
  emit("update:visible", false);
}

async function confirm(payload: { nodeId: number; targetKey: string; fingerprint: string; reason: string }) {
  const target = firstTarget.value;
  if (!target || payload.nodeId !== target.nodeId || payload.targetKey !== targetKey.value || payload.fingerprint !== fingerprint.value) {
    loadError.value = new Error("确认快照已变化，请关闭后重新预检");
    return;
  }
  busy.value = true;
  try {
    if (props.targets.length > 1) {
      if (!batchPreflight.value) throw new Error("批量关闭预检缺失");
      const response = await closeZLMStreams(batchPreflight.value);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "批量关闭失败");
      emit("done", response.data);
    } else if (props.force) {
      const response = await forceCloseZLMStream(target.nodeId, target.media, payload.fingerprint, payload.reason);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "强制关闭失败");
      emit("done", {
        closed: response.data.closed ? 1 : 0,
        alreadyAbsent: response.data.alreadyAbsent ? 1 : 0,
        partial: false,
        uncertain: response.data.uncertain
      });
    } else {
      const response = await closeZLMStream(target.nodeId, target.media, payload.fingerprint);
      if (response.code !== 0 || !response.data) throw new Error(response.message || "关闭失败");
      emit("done", {
        closed: response.data.closed ? 1 : 0,
        alreadyAbsent: response.data.alreadyAbsent ? 1 : 0,
        partial: false,
        uncertain: response.data.uncertain
      });
    }
    close();
  } catch (error) {
    loadError.value = error;
    decision.value = { allowed: false, mode: "blocked", reason: "执行结果未确认；请关闭窗口并重新预检，页面不会沿用旧 fingerprint。" };
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <ZLMDangerActionDialog
    v-if="canConfirm"
    :visible="visible"
    :node-id="firstTarget!.nodeId"
    :node-name="nodeName"
    :target-key="targetKey"
    :target-label="targetLabel"
    :fingerprint="fingerprint"
    :impacts="impactLines"
    :confirm-phrase="confirmPhrase"
    :require-reason="force"
    :action-label="force ? '强制关闭' : '确认关闭'"
    :busy="busy"
    @confirm="confirm"
    @stale="close"
    @update:visible="emit('update:visible', $event)"
  />

  <a-modal
    v-else
    :visible="visible"
    modal-class="uvp-system-dialog zlm-stream-close-state"
    :width="560"
    :footer="false"
    :mask-closable="false"
    unmount-on-close
    @cancel="close"
  >
    <template #title>媒体流关闭预检</template>
    <div v-if="loading" class="preflight-state" role="status"><a-spin /><span>正在读取最新流状态和业务持有…</span></div>
    <div v-else class="preflight-body">
      <div :class="['preflight-message', { 'preflight-message--error': loadError }]" :role="loadError ? 'alert' : 'status'">
        {{ loadError ? errorPresentation.label : decision?.reason || "没有可执行的目标。" }}
      </div>
      <dl><div><dt>节点</dt><dd>{{ nodeName }}（#{{ firstTarget?.nodeId || '—' }}）</dd></div><div><dt>目标</dt><dd>{{ targetLabel }}</dd></div></dl>
      <ul v-if="impactLines.length"><li v-for="line in impactLines" :key="line">{{ line }}</li></ul>
      <div class="preflight-actions"><a-button @click="close">关闭</a-button><a-button v-if="loadError && errorPresentation.retryable" type="primary" @click="loadPreflight">重新预检</a-button></div>
    </div>
  </a-modal>
</template>

<style scoped>
.preflight-state { display: flex; min-height: 180px; flex-direction: column; align-items: center; justify-content: center; gap: 10px; color: var(--zlm-text-3); }
.preflight-body { display: flex; flex-direction: column; gap: 14px; color: var(--zlm-text-2); }
.preflight-message { padding: 10px 12px; color: var(--zlm-warn-600); background: var(--zlm-warn-50); border: 1px solid var(--zlm-warn-500); border-radius: var(--zlm-radius-md); line-height: 1.6; }
.preflight-message--error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border-color: var(--zlm-danger-500); }
dl { display: grid; gap: 8px; margin: 0; } dl div { display: grid; grid-template-columns: 56px minmax(0, 1fr); gap: 12px; } dt { color: var(--zlm-text-3); } dd { margin: 0; overflow-wrap: anywhere; color: var(--zlm-text-1); }
ul { max-height: 180px; margin: 0; padding-left: 20px; overflow: auto; line-height: 1.7; }
.preflight-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
