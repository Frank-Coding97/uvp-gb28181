<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { RefreshCw } from "lucide-vue-next";
import { useZLMContextStore, type ZLMContextNode } from "@/store/modules/zlm-context";
import NodeStateBadge from "./NodeStateBadge.vue";

const props = withDefaults(defineProps<{
  nodes: ZLMContextNode[];
  queryNodeId?: unknown;
  loading?: boolean;
  disabled?: boolean;
  title?: string;
  allowAll?: boolean;
  defaultAll?: boolean;
  minimal?: boolean;
}>(), {
  queryNodeId: undefined,
  loading: false,
  disabled: false,
  title: "当前媒体节点",
  allowAll: false,
  defaultAll: false,
  minimal: false
});

const emit = defineEmits<{
  change: [nodeId: number | null];
  refresh: [];
}>();

const context = useZLMContextStore();
const defaultAllApplied = ref(false);

watch(
  () => [props.nodes, props.queryNodeId, props.loading] as const,
  ([nodes, queryNodeId, loading]) => {
    if (loading && nodes.length === 0) return;
    if (!context.initialized) context.initialize(nodes, queryNodeId);
    else context.reconcileVisibleNodes(nodes);
    if (props.allowAll && props.defaultAll && !defaultAllApplied.value) {
      const queryID = Number(Array.isArray(queryNodeId) ? queryNodeId[0] : queryNodeId);
      const hasValidQuery = Number.isSafeInteger(queryID) && queryID > 0 && nodes.some(node => node.id === queryID);
      if (hasValidQuery) {
        defaultAllApplied.value = true;
      } else {
        context.selectAll();
        if (nodes.length > 0) defaultAllApplied.value = true;
      }
    }
  },
  { immediate: true, deep: true }
);

const selectedID = computed(() => context.selectedNodeId ?? (props.allowAll ? "all" : undefined));
const selectedNode = computed(() => context.selectedNode);

const stateText = computed(() => {
  if (props.allowAll && context.selectedNodeId === null) return "聚合全部可见节点，可选择单个节点查看运行细节";
  switch (selectedNode.value?.state) {
    case "active":
      return "节点在线，运行态数据会自动刷新";
    case "maintenance":
      return "节点处于维护状态，不承接新的媒体会话";
    case "offline":
      return "节点已离线，保留当前选择以便排查";
    default:
      return "暂无可用的媒体节点";
  }
});

function select(value: string | number | undefined) {
  if (props.allowAll && value === "all") {
    context.selectAll();
    emit("change", null);
    return;
  }
  const nodeID = Number(value);
  if (!Number.isSafeInteger(nodeID) || nodeID <= 0 || !context.selectNode(nodeID)) return;
  emit("change", nodeID);
}
</script>

<template>
  <section class="zlm-node-context" aria-label="媒体节点上下文" :data-minimal="minimal">
    <div class="zlm-node-context__identity">
      <span v-if="!minimal" class="zlm-node-context__label">{{ title }}</span>
      <a-select
        :model-value="selectedID"
        :loading="loading"
        :disabled="disabled || context.visibleNodes.length === 0"
        placeholder="选择媒体节点"
        class="zlm-node-context__select"
        @change="select"
      >
        <a-option v-if="allowAll" value="all">
          <span class="zlm-node-context__option-name">全部节点</span>
        </a-option>
        <a-option v-for="node in context.visibleNodes" :key="node.id" :value="node.id">
          <span class="zlm-node-context__option-name">{{ node.name }}</span>
          <span class="zlm-node-context__option-id">#{{ node.id }}</span>
          <NodeStateBadge :state="node.state" />
        </a-option>
      </a-select>
      <NodeStateBadge v-if="!minimal && selectedNode" :state="selectedNode.state" />
    </div>

    <div v-if="!minimal" class="zlm-node-context__status" :data-node-state="selectedNode?.state || 'empty'">
      {{ stateText }}
    </div>

    <a-button
      class="zlm-node-context__refresh"
      :loading="loading"
      :disabled="disabled"
      aria-label="刷新媒体节点"
      @click="emit('refresh')"
    >
      <template #icon><RefreshCw :size="15" /></template>
      刷新
    </a-button>
  </section>
</template>

<style scoped>
.zlm-node-context {
  display: flex;
  align-items: center;
  gap: var(--zlm-space-3);
  min-height: 56px;
  padding: var(--zlm-space-3) var(--zlm-space-4);
  color: var(--zlm-text-2);
  background: var(--zlm-card);
  border: 1px solid var(--zlm-border);
  border-radius: var(--zlm-radius-lg);
}

.zlm-node-context[data-minimal="true"] {
  justify-content: flex-end;
}

.zlm-node-context__identity {
  display: flex;
  align-items: center;
  gap: var(--zlm-space-2);
  min-width: 0;
}

.zlm-node-context__label {
  flex: none;
  color: var(--zlm-text-3);
  font-size: var(--zlm-fs-caption);
  font-weight: var(--zlm-fw-medium);
}

.zlm-node-context__select {
  width: 260px;
}

.zlm-node-context__option-name {
  margin-right: 6px;
  color: var(--zlm-text-1);
  font-weight: var(--zlm-fw-medium);
}

.zlm-node-context__option-id {
  margin-right: 8px;
  color: var(--zlm-text-4);
  font-family: var(--zlm-font-mono);
  font-size: var(--zlm-fs-caption);
}

.zlm-node-context__status {
  min-width: 0;
  flex: 1;
  overflow: hidden;
  color: var(--zlm-text-3);
  font-size: var(--zlm-fs-caption);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.zlm-node-context__status[data-node-state="offline"] {
  color: var(--zlm-danger-600);
}

.zlm-node-context__status[data-node-state="maintenance"] {
  color: var(--zlm-warn-600);
}

.zlm-node-context__refresh {
  flex: none;
}

@media (max-width: 760px) {
  .zlm-node-context {
    align-items: stretch;
    flex-direction: column;
  }

  .zlm-node-context[data-minimal="true"] {
    align-items: center;
    flex-direction: row;
  }

  .zlm-node-context__select {
    width: min(100%, 320px);
  }
}
</style>
