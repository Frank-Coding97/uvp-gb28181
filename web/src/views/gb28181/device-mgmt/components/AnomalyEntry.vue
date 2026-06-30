<script setup lang="ts">
/**
 * AnomalyEntry — 左侧目录底部"目录异常 N"入口(C2)
 *
 * 视觉:
 * - 非 0 时琥珀色 + 呼吸点
 * - click → router.push('/gb28181/device-mgmt/anomaly')
 */
import { computed } from "vue";
import { useRouter } from "vue-router";
import { useCatalogStore } from "../stores/catalog";

const router = useRouter();
const store = useCatalogStore();

const count = computed(() => store.anomalyCount);
const hasAnomaly = computed(() => count.value > 0);

function open() {
  router.push("/gb28181/device-mgmt/anomaly");
}
</script>

<template>
  <button
    type="button"
    :class="['anomaly-entry', { 'has-anomaly': hasAnomaly }]"
    @click="open"
  >
    <span class="dot" v-if="hasAnomaly" />
    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" />
      <line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
    <span class="label">目录异常</span>
    <span class="count">{{ count }}</span>
  </button>
</template>

<style lang="scss" scoped>
.anomaly-entry {
  appearance: none;
  border: 0;
  background: transparent;
  width: 100%;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 36px;
  padding: 0 var(--space-4);
  border-top: 1px solid var(--color-border-1);
  color: var(--color-text-3);
  cursor: pointer;
  font-size: var(--font-13);
  text-align: left;
  transition: background var(--duration-fast) var(--ease-out),
    color var(--duration-fast) var(--ease-out);

  &:hover {
    background: var(--color-bg-3);
  }

  &.has-anomaly {
    color: var(--status-warning);

    .count {
      color: var(--status-warning);
      font-weight: 600;
    }
  }

  .label {
    flex: 1;
  }

  .count {
    font-variant-numeric: tabular-nums;
    color: var(--color-text-3);
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--status-warning);
    box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.6);
    animation: pulse 1.6s ease-out infinite;
  }
}

@keyframes pulse {
  0% {
    box-shadow: 0 0 0 0 rgba(245, 158, 11, 0.6);
  }
  70% {
    box-shadow: 0 0 0 6px rgba(245, 158, 11, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(245, 158, 11, 0);
  }
}
</style>
