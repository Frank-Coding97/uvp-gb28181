<script setup lang="ts">
/**
 * StatusDot — 状态色点(C3 / D1 / D3 通用)
 * 视觉:8px 圆 + 3px halo(在线 / 告警 / 严重 时);严重态呼吸
 */
type StatusKind = "online" | "offline" | "warning" | "danger";

interface Props {
  status: StatusKind;
  /** 是否带 halo(default true 时上面颜色规则) */
  halo?: boolean;
}

withDefaults(defineProps<Props>(), { halo: true });
</script>

<template>
  <span :class="['status-dot', status, { 'no-halo': halo === false }]" />
</template>

<style lang="scss" scoped>
.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;

  &.online {
    background: var(--status-online);
    box-shadow: 0 0 0 3px var(--status-online-fade);
  }

  &.offline {
    background: var(--status-offline);
  }

  &.warning {
    background: var(--status-warning);
    box-shadow: 0 0 0 3px var(--status-warning-fade);
  }

  &.danger {
    background: var(--status-danger);
    box-shadow: 0 0 0 3px var(--status-danger-fade);
    animation: dot-pulse 2s var(--ease-out) infinite;
  }

  &.no-halo {
    box-shadow: none;
    animation: none;
  }
}

@keyframes dot-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 3px var(--status-danger-fade);
  }
  50% {
    box-shadow: 0 0 0 6px var(--status-danger-fade);
  }
}
</style>
