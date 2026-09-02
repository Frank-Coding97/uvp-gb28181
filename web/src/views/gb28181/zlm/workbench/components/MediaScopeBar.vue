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
  showRefresh?: boolean;
}>(), {
  allowAll: true,
  requiresNode: false,
  loading: false,
  stale: false,
  errorText: "",
  showRefresh: true
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

function changeScope(next: string | number | Event) {
  const value = next instanceof Event ? (next.target as HTMLSelectElement).value : next;
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
    <div class="media-scope-bar__field" title="切换媒体节点">
      <span class="media-scope-bar__label">当前节点</span>
      <a-select
        class="media-scope-bar__select"
        aria-label="节点范围"
        :model-value="selectedValue"
        :loading="loading"
        placeholder="选择媒体节点"
        @change="changeScope"
      >
        <a-option v-if="allowAll && !requiresNode" value="all">全部节点</a-option>
        <a-option v-if="requiresNode && modelValue === 'all'" value="all" disabled>请选择节点</a-option>
        <a-option v-for="node in nodes" :key="node.id" :value="String(node.id)">
          {{ node.name }} · {{ stateText(node.state) }}
        </a-option>
      </a-select>
    </div>
    <span v-if="selectionRequired" class="media-scope-bar__notice" role="status">请先明确选择节点，再执行节点级操作。</span>
    <span v-else-if="errorText" class="media-scope-bar__notice media-scope-bar__notice--error" role="alert">
      {{ errorText }}；仍显示上次成功目录。
    </span>
    <span v-else-if="stale" class="media-scope-bar__notice" role="status">节点目录可能已过期，可手动刷新。</span>
    <span v-else-if="modelValue === 'all'" class="media-scope-bar__notice" role="status">聚合 {{ nodes.length }} 个可见节点</span>
    <button v-if="showRefresh" type="button" aria-label="刷新节点目录" :disabled="loading" @click="emit('refresh')">
      <RefreshCw :size="13" :class="{ 'is-spinning': loading }" aria-hidden="true" />
      更新节点
    </button>
    <slot name="actions" />
  </section>
</template>

<style scoped>
.media-scope-bar { display: flex; min-width: 0; align-items: center; justify-content: flex-end; gap: 10px; padding: 4px 0 6px; color: var(--zlm-text-2); background: transparent; border: 0; border-radius: 0; }
.media-scope-bar__field { display: flex; width: 284px; min-width: 0; align-items: center; gap: 8px; padding: 3px; background: linear-gradient(135deg, var(--zlm-brand-50), var(--zlm-card)); border: 1px solid var(--zlm-brand-200); border-radius: 11px; box-shadow: 0 4px 14px rgb(22 93 255 / 10%); transition: border-color .18s ease, box-shadow .18s ease; }
.media-scope-bar__field:hover, .media-scope-bar__field:focus-within { border-color: var(--zlm-brand-500); box-shadow: 0 5px 18px rgb(22 93 255 / 16%); }
.media-scope-bar__label { flex: none; margin-left: 9px; padding-right: 8px; color: var(--zlm-brand-600); border-right: 1px solid var(--zlm-brand-200); font-size: 12px; font-weight: var(--zlm-fw-semibold); line-height: 20px; white-space: nowrap; }
.media-scope-bar__select { min-width: 0; flex: 1; }
.media-scope-bar__select :deep(.arco-select-view) { height: 36px; padding-left: 8px; color: var(--zlm-text-1); font-weight: var(--zlm-fw-semibold); background: var(--zlm-card); border-color: transparent; border-radius: 8px; }
.media-scope-bar__select :deep(.arco-select-view:hover), .media-scope-bar__select :deep(.arco-select-view-focus) { background: var(--zlm-card); border-color: transparent; }
.media-scope-bar button:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
.media-scope-bar__notice { min-width: 0; flex: none; color: var(--zlm-text-3); font-size: 11px; line-height: 1.45; overflow-wrap: anywhere; }
.media-scope-bar__notice--error { color: var(--zlm-warn-600); }
.media-scope-bar button { display: inline-flex; min-height: 26px; flex: none; align-items: center; gap: 5px; padding: 0 5px; color: var(--zlm-brand-600); background: transparent; border: 0; border-radius: var(--zlm-radius-sm); cursor: pointer; font-size: 11px; }
.media-scope-bar button:hover { background: var(--zlm-brand-50); }
.media-scope-bar button:disabled { cursor: wait; opacity: .65; }
.is-spinning { animation: media-scope-spin .8s linear infinite; }
@keyframes media-scope-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .media-scope-bar * { animation-duration: .01ms !important; } }
@media (max-width: 768px) { .media-scope-bar { align-items: stretch; flex-direction: column; } .media-scope-bar__field { width: 100%; } .media-scope-bar__notice { text-align: right; } }
</style>
