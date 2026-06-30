<script setup lang="ts">
/**
 * AnomalyFlag — anomaly 标记(C3 行内 / D1 卡片 / E1 抽屉 通用)
 */
interface Props {
  reason?: string;
  size?: "sm" | "md";
}

withDefaults(defineProps<Props>(), { size: "sm" });
</script>

<template>
  <span :class="['anomaly-flag', size]" :title="reason || '编码不规范,已兜底进 virtual_org'">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
    <span class="text">异常</span>
  </span>
</template>

<style lang="scss" scoped>
.anomaly-flag {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  height: 16px;
  padding: 0 6px;
  border-radius: var(--dm-radius-1);
  background: var(--status-warning-fade);
  color: var(--status-warning);
  font-size: 10px;
  font-weight: 600;
  cursor: help;

  svg {
    width: 10px;
    height: 10px;
  }

  &.md {
    height: 20px;
    padding: 0 8px;
    font-size: 12px;

    svg {
      width: 12px;
      height: 12px;
    }
  }
}
</style>
