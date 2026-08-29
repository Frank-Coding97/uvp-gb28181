<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { AlertTriangle } from "lucide-vue-next";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import {
  createDangerActionSnapshot,
  dangerActionSnapshotMatches,
  type DangerActionSnapshot
} from "./dangerActionState";

const props = withDefaults(defineProps<{
  visible: boolean;
  nodeId: number;
  nodeName: string;
  targetKey: string;
  targetLabel: string;
  fingerprint?: string;
  impacts?: string[];
  confirmPhrase: string;
  requireReason?: boolean;
  actionLabel?: string;
  busy?: boolean;
}>(), {
  fingerprint: "",
  impacts: () => [],
  requireReason: true,
  actionLabel: "确认执行",
  busy: false
});

const emit = defineEmits<{
  "update:visible": [visible: boolean];
  confirm: [payload: { nodeId: number; targetKey: string; fingerprint: string; reason: string }];
  stale: [];
}>();

const context = useZLMContextStore();
const snapshot = ref<DangerActionSnapshot | null>(null);
const reason = ref("");
const typedPhrase = ref("");

function currentIdentity() {
  return {
    contextVersion: context.dialogRevision,
    nodeId: context.selectedNodeId ?? props.nodeId,
    targetKey: props.targetKey,
    fingerprint: props.fingerprint
  };
}

function capture() {
  snapshot.value = createDangerActionSnapshot({
    contextVersion: context.dialogRevision,
    nodeId: props.nodeId,
    nodeName: props.nodeName,
    targetKey: props.targetKey,
    targetLabel: props.targetLabel,
    fingerprint: props.fingerprint,
    impacts: props.impacts,
    confirmPhrase: props.confirmPhrase,
    requireReason: props.requireReason
  });
  reason.value = "";
  typedPhrase.value = "";
}

function isCurrent() {
  return snapshot.value !== null && dangerActionSnapshotMatches(snapshot.value, currentIdentity());
}

function expire() {
  if (!snapshot.value) return;
  snapshot.value = null;
  emit("stale");
  emit("update:visible", false);
}

watch(() => props.visible, visible => {
  if (visible) capture();
  else snapshot.value = null;
}, { immediate: true });

watch(
  () => [context.dialogRevision, context.selectedNodeId, props.nodeId, props.targetKey, props.fingerprint],
  () => {
    if (props.visible && snapshot.value && !isCurrent()) expire();
  }
);

const canConfirm = computed(() => {
  const opening = snapshot.value;
  if (!opening || props.busy || !isCurrent()) return false;
  if (typedPhrase.value !== opening.confirmPhrase) return false;
  return !opening.requireReason || reason.value.trim().length > 0;
});

function close() {
  emit("update:visible", false);
}

function confirm() {
  const opening = snapshot.value;
  if (!opening || !isCurrent()) {
    expire();
    return;
  }
  if (!canConfirm.value) return;
  emit("confirm", {
    nodeId: opening.nodeId,
    targetKey: opening.targetKey,
    fingerprint: opening.fingerprint ?? "",
    reason: reason.value.trim()
  });
}
</script>

<template>
  <a-modal
    :visible="visible"
    modal-class="uvp-system-dialog zlm-danger-dialog"
    :width="560"
    :footer="false"
    :mask-closable="false"
    :esc-to-close="!busy"
    :closable="!busy"
    unmount-on-close
    @cancel="close"
    @update:visible="emit('update:visible', $event)"
  >
    <template #title>
      <span class="zlm-danger-dialog__title"><AlertTriangle :size="18" />高风险操作确认</span>
    </template>

    <div v-if="snapshot" class="zlm-danger-dialog__body">
      <div class="zlm-danger-dialog__warning" role="alert">
        操作执行前会再次校验节点、目标和影响指纹；任一项变化都必须重新打开本窗口确认。
      </div>

      <dl class="zlm-danger-dialog__summary">
        <div><dt>节点</dt><dd>{{ snapshot.nodeName }}（#{{ snapshot.nodeId }}）</dd></div>
        <div><dt>目标</dt><dd>{{ snapshot.targetLabel }}</dd></div>
      </dl>

      <div class="zlm-danger-dialog__impact">
        <div class="zlm-danger-dialog__section-title">预计影响</div>
        <ul v-if="snapshot.impacts.length">
          <li v-for="impact in snapshot.impacts" :key="impact">{{ impact }}</li>
        </ul>
        <p v-else>当前未识别到业务持有，但仍将按高风险操作执行。</p>
      </div>

      <label v-if="snapshot.requireReason" class="zlm-danger-dialog__field">
        <span>操作理由</span>
        <a-textarea v-model="reason" :max-length="256" show-word-limit placeholder="请输入可审计的操作理由" :disabled="busy" />
      </label>

      <label class="zlm-danger-dialog__field">
        <span>请输入确认短语：<strong>{{ snapshot.confirmPhrase }}</strong></span>
        <a-input v-model="typedPhrase" autocomplete="off" placeholder="输入上方完整短语" :disabled="busy" />
      </label>

      <div class="zlm-danger-dialog__actions">
        <a-button :disabled="busy" @click="close">取消</a-button>
        <a-button status="danger" type="primary" :loading="busy" :disabled="!canConfirm" @click="confirm">
          {{ actionLabel }}
        </a-button>
      </div>
    </div>
  </a-modal>
</template>

<style scoped>
.zlm-danger-dialog__title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--zlm-danger-600);
}

.zlm-danger-dialog__body {
  display: flex;
  flex-direction: column;
  gap: var(--zlm-space-4);
}

.zlm-danger-dialog__warning {
  padding: 10px 12px;
  color: var(--zlm-danger-600);
  font-size: var(--zlm-fs-caption);
  line-height: 1.6;
  background: var(--zlm-danger-50);
  border: 1px solid var(--zlm-danger-500);
  border-radius: var(--zlm-radius-md);
}

.zlm-danger-dialog__summary {
  display: grid;
  gap: 8px;
  margin: 0;
}

.zlm-danger-dialog__summary > div {
  display: grid;
  grid-template-columns: 56px minmax(0, 1fr);
  gap: 12px;
}

.zlm-danger-dialog__summary dt {
  color: var(--zlm-text-3);
}

.zlm-danger-dialog__summary dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--zlm-text-1);
}

.zlm-danger-dialog__section-title,
.zlm-danger-dialog__field > span {
  color: var(--zlm-text-2);
  font-size: var(--zlm-fs-caption);
  font-weight: var(--zlm-fw-medium);
}

.zlm-danger-dialog__impact ul,
.zlm-danger-dialog__impact p {
  margin: 8px 0 0;
  padding-left: 20px;
  color: var(--zlm-text-2);
  line-height: 1.7;
}

.zlm-danger-dialog__impact p {
  padding-left: 0;
}

.zlm-danger-dialog__field {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.zlm-danger-dialog__field strong {
  color: var(--zlm-danger-600);
  font-family: var(--zlm-font-mono);
}

.zlm-danger-dialog__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--zlm-space-2);
  padding-top: var(--zlm-space-2);
}
</style>
