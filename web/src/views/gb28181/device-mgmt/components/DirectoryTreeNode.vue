<script setup lang="ts">
/**
 * DirectoryTreeNode — 递归目录树节点(C2)
 *
 * 视觉对照 mockup list.html .tree-node:
 * - 缩进随 depth 增加(lv-1 ~ lv-5 padding-left 16/32/48/64/80)
 * - 节点类型彩色 chip(civil_code 紫 / biz_group 青 / virtual_org 橙 / device 蓝 / channel 绿)
 * - twist icon 90° 旋转表展开
 * - active 态:左侧 2px 主色描边 + 浅蓝底
 *
 * 行为:
 * - click(twist) → emit('toggle', id) 父级 store 处理 lazy 加载
 * - click(行)   → emit('select', id)  父级路由 push ?node=id
 */
import { computed } from "vue";
import { useCatalogStore } from "../stores/catalog";
import type { CatalogNode, NodeType } from "../api/catalog";

interface Props {
  node: CatalogNode;
  activeId: number | null;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  (e: "toggle", id: number): void;
  (e: "select", id: number): void;
}>();

const store = useCatalogStore();

const isExpanded = computed(() => store.isExpanded(props.node.id));
const isLoading = computed(() => store.isLoadingChildren(props.node.id));
const isActive = computed(() => props.activeId === props.node.id);
const children = computed(() => store.childrenOf(props.node.id));

const hasChildren = computed(() => {
  // 已加载过 & 有子节点 → 显示展开图标
  if (children.value.length > 0) return true;
  // 未加载过 & 不是 channel(channel 是叶) → 也允许点开(lazy)
  return props.node.nodeType !== "channel";
});

const typeChipClass = computed(() => {
  const map: Record<NodeType, string> = {
    civil_code: "t-civil",
    biz_group: "t-biz",
    virtual_org: "t-virtual",
    device: "t-device",
    channel: props.node.anomaly ? "t-virtual" : "t-channel",
  };
  return map[props.node.nodeType] || "t-biz";
});

const depthClass = computed(() => `lv-${Math.min((props.node.depth || 0) + 1, 5)}`);

function onTwistClick(e: MouseEvent) {
  e.stopPropagation();
  if (hasChildren.value) {
    emit("toggle", props.node.id);
  }
}

function onRowClick() {
  emit("select", props.node.id);
}
</script>

<template>
  <div
    :class="['tree-node', depthClass, { expanded: isExpanded, active: isActive }]"
    @click="onRowClick"
  >
    <span class="twist" @click="onTwistClick" :data-loading="isLoading">
      <svg v-if="hasChildren" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <polyline points="9 18 15 12 9 6" />
      </svg>
      <i v-else class="twist-spacer" />
    </span>
    <span :class="['node-chip', typeChipClass]" :title="node.nodeType">
      <svg
        v-if="node.nodeType === 'civil_code'"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z" />
        <circle cx="12" cy="10" r="3" />
      </svg>
      <svg
        v-else-if="node.nodeType === 'device'"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <rect x="2" y="6" width="20" height="12" rx="2" />
        <path d="m22 9-4 3 4 3z" />
      </svg>
      <svg
        v-else-if="node.nodeType === 'channel'"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <circle cx="12" cy="12" r="3" />
        <circle cx="12" cy="12" r="8" />
      </svg>
      <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" />
      </svg>
    </span>
    <span class="label">{{ node.name }}</span>
    <span v-if="typeof node.mountCount === 'number'" class="count">
      {{ node.mountCount }}
    </span>
    <span v-if="node.anomaly" class="anomaly-flag" title="编码不规范,已兜底">⚠</span>
  </div>

  <template v-if="isExpanded">
    <DirectoryTreeNode
      v-for="child in children"
      :key="child.id"
      :node="child"
      :active-id="activeId"
      @toggle="(id) => emit('toggle', id)"
      @select="(id) => emit('select', id)"
    />
  </template>
</template>

<script lang="ts">
export default { name: "DirectoryTreeNode" };
</script>

<style lang="scss" scoped>
.tree-node {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 28px;
  padding: 0 var(--space-4);
  color: var(--color-text-2);
  cursor: pointer;
  position: relative;
  transition: background var(--duration-fast) var(--ease-out);
  font-size: var(--font-13);

  &:hover {
    background: var(--color-bg-3);
  }

  &.active {
    background: var(--primary-fade-08);
    color: var(--color-text-1);

    &::before {
      content: "";
      position: absolute;
      left: 0;
      top: 4px;
      bottom: 4px;
      width: 2px;
      background: var(--primary-6);
      border-radius: 0 var(--dm-radius-1) var(--dm-radius-1) 0;
    }
  }

  // 缩进层级
  &.lv-1 { padding-left: var(--space-4); }
  &.lv-2 { padding-left: 32px; }
  &.lv-3 { padding-left: 48px; }
  &.lv-4 { padding-left: 64px; }
  &.lv-5 { padding-left: 80px; }

  .twist {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 14px;
    height: 14px;
    color: var(--color-text-4);
    transition: transform var(--duration-fast) var(--ease-out);

    svg {
      width: 12px;
      height: 12px;
    }

    .twist-spacer {
      width: 12px;
      height: 12px;
      display: inline-block;
    }
  }

  &.expanded > .twist {
    transform: rotate(90deg);
  }

  .node-chip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: var(--dm-radius-1);
    flex-shrink: 0;

    svg {
      width: 12px;
      height: 12px;
    }

    &.t-civil {
      background: rgba(167, 139, 250, 0.12);
      color: var(--node-civil);
    }
    &.t-biz {
      background: rgba(6, 182, 212, 0.12);
      color: var(--node-biz);
    }
    &.t-virtual {
      background: rgba(251, 146, 60, 0.12);
      color: var(--node-virtual);
    }
    &.t-device {
      background: rgba(59, 130, 246, 0.12);
      color: var(--node-device);
    }
    &.t-channel {
      background: rgba(16, 185, 129, 0.12);
      color: var(--node-channel-on);
    }
  }

  .label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-size: 11px;
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
  }

  .anomaly-flag {
    color: var(--status-warning);
    font-size: 11px;
  }
}
</style>
