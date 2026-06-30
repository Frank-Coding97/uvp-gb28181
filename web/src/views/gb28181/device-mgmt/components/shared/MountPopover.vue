<script setup lang="ts">
/**
 * MountPopover — 多挂载浮层(C3 / D1 / E1 通用)
 *
 * 显示该通道所有挂载位置,主挂载青色描边
 */
import type { ChannelMountVO } from "../../api/device";

interface Props {
  mounts: ChannelMountVO[];
}

defineProps<Props>();
</script>

<template>
  <div class="mount-popover">
    <div class="mp-head">挂载位置 ({{ mounts.length }})</div>
    <ul class="mp-list">
      <li v-for="m in mounts" :key="m.id" :class="['mp-item', { primary: m.isPrimary }]">
        <span class="path">{{ m.parentPath }}</span>
        <span class="name">{{ m.parentName }}</span>
        <span v-if="m.isPrimary" class="badge">主</span>
      </li>
    </ul>
  </div>
</template>

<style lang="scss" scoped>
.mount-popover {
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--dm-radius-2);
  box-shadow: var(--shadow-overlay);
  padding: var(--space-3);
  min-width: 280px;
  max-width: 360px;
}

.mp-head {
  color: var(--color-text-3);
  font-size: var(--font-12);
  margin-bottom: var(--space-2);
}

.mp-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.mp-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2);
  border-radius: var(--dm-radius-1);
  background: var(--color-bg-3);
  font-size: var(--font-12);

  &.primary {
    border: 1px solid rgba(6, 182, 212, 0.4);
  }

  .path {
    color: var(--color-text-4);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .name {
    flex: 1;
    color: var(--color-text-2);
  }

  .badge {
    background: var(--node-mount);
    color: var(--color-bg-1);
    font-size: 10px;
    padding: 1px 6px;
    border-radius: var(--dm-radius-pill);
    font-weight: 600;
  }
}
</style>
