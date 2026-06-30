<script setup lang="ts">
/**
 * MountBadge — 多挂载链状角标(Q3 决议 / C3 / D1 / E1 通用)
 *
 * 当通道挂载数 > 1 时显示 🔗 N 角标;hover 触发 MountPopover(C3 后期 / E1 接入)
 */
interface Props {
  count: number;
}

defineProps<Props>();
</script>

<template>
  <span v-if="count > 1" class="mount-badge" :title="`挂载在 ${count} 处`">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </svg>
    {{ count }}
  </span>
</template>

<style lang="scss" scoped>
.mount-badge {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: 14px;
  padding: 0 5px;
  margin-left: var(--space-1);
  background: rgba(6, 182, 212, 0.12);
  color: var(--node-mount);
  border-radius: var(--dm-radius-pill);
  font-size: 10px;
  font-weight: 600;
  cursor: help;

  svg {
    width: 10px;
    height: 10px;
  }
}
</style>
