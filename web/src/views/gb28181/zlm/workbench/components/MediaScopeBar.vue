<script setup lang="ts">
import { computed } from "vue";
import { RefreshCw } from "lucide-vue-next";

import type { MediaNodeCatalogNode, MediaScope } from "@/store/modules/media-workbench";

const props = withDefaults(defineProps<{
  modelValue: MediaScope;
  nodes: readonly MediaNodeCatalogNode[];
  allowAll?: boolean;
  requiresNode?: boolean;
  loading?: boolean;
  stale?: boolean;
  errorText?: string;
}>(), {
  allowAll: true,
  requiresNode: false,
  loading: false,
  stale: false,
  errorText: ""
});

const emit = defineEmits<{
  "update:modelValue": [scope: MediaScope];
  refresh: [];
}>();

const selectedValue = computed(() => String(props.modelValue));
const selectionRequired = computed(() => props.requiresNode && props.modelValue === "all");

function stateText(state: MediaNodeCatalogNode["state"]) {
  if (state === "active") return "在线";
  if (state === "maintenance") return "维护";
  return "离线";
}

function changeScope(event: Event) {
  const value = (event.target as HTMLSelectElement).value;
  if (value === "all" && props.allowAll && !props.requiresNode) {
    emit("update:modelValue", "all");
    return;
  }
  const nodeId = Number(value);
  if (Number.isSafeInteger(nodeId) && nodeId > 0 && props.nodes.some(node => node.id === nodeId)) {
    emit("update:modelValue", nodeId);
  }
}
</script>

<template>
  <section class="media-scope-bar" aria-label="媒体节点范围">
    <div class="media-scope-bar__field">
      <span>数据范围</span>
      <select aria-label="节点范围" :value="selectedValue" @change="changeScope">
        <option v-if="allowAll && !requiresNode" value="all">全部节点</option>
        <option v-if="requiresNode && modelValue === 'all'" value="all" disabled>请选择节点</option>
        <option v-for="node in nodes" :key="node.id" :value="String(node.id)">
          {{ node.name }} · {{ stateText(node.state) }}
        </option>
      </select>
    </div>
    <span v-if="selectionRequired" class="media-scope-bar__notice" role="status">请先明确选择节点，再执行节点级操作。</span>
    <span v-else-if="errorText" class="media-scope-bar__notice media-scope-bar__notice--error" role="alert">
      {{ errorText }}；仍显示上次成功目录。
    </span>
    <span v-else-if="stale" class="media-scope-bar__notice" role="status">节点目录可能已过期，可手动刷新。</span>
    <span v-else class="media-scope-bar__notice" role="status">
      {{ modelValue === 'all' ? `聚合 ${nodes.length} 个可见节点` : '仅查看所选节点' }}
    </span>
    <button type="button" aria-label="刷新节点目录" :disabled="loading" @click="emit('refresh')">
      <RefreshCw :size="14" :class="{ 'is-spinning': loading }" aria-hidden="true" />
      更新节点
    </button>
  </section>
</template>

<style scoped>
.media-scope-bar { display: flex; flex: none; min-width: 0; align-items: center; gap: 12px; padding: 10px 14px; color: var(--zlm-text-2); background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }
.media-scope-bar__field { display: flex; align-items: center; gap: 8px; flex: none; }
.media-scope-bar__field > span { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.media-scope-bar select { min-width: 190px; height: 32px; padding: 0 30px 0 10px; color: var(--zlm-text-1); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }
.media-scope-bar select:focus-visible, .media-scope-bar button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.media-scope-bar__notice { min-width: 0; flex: 1; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); overflow-wrap: anywhere; }
.media-scope-bar__notice--error { color: var(--zlm-warn-600); }
.media-scope-bar button { display: inline-flex; min-height: 30px; flex: none; align-items: center; gap: 6px; padding: 0 9px; color: var(--zlm-brand-600); background: transparent; border: 0; border-radius: var(--zlm-radius-md); cursor: pointer; }
.media-scope-bar button:hover { background: var(--zlm-brand-50); }
.media-scope-bar button:disabled { cursor: wait; opacity: .65; }
.is-spinning { animation: media-scope-spin .8s linear infinite; }
@keyframes media-scope-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .media-scope-bar * { animation-duration: .01ms !important; } }
@media (max-width: 768px) { .media-scope-bar { align-items: stretch; flex-direction: column; } .media-scope-bar__field { align-items: stretch; flex-direction: column; } .media-scope-bar select { width: 100%; } .media-scope-bar button { align-self: flex-start; } }
</style>
