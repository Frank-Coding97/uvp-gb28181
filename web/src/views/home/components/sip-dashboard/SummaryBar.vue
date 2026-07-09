<template>
  <div class="summary-bar" :class="severityClass">
    <div class="summary-bar__health">
      <span class="summary-bar__num">{{ healthDisplay }}</span>
      <span class="summary-bar__label">% 接入健康度</span>
    </div>
    <div class="summary-bar__divider" />
    <div class="summary-bar__stats">
      <div class="summary-bar__stat">
        <div class="summary-bar__value">{{ formatNumber(todayTotal) }}</div>
        <div class="summary-bar__caption">今日信令</div>
      </div>
      <div class="summary-bar__stat">
        <div class="summary-bar__value summary-bar__value--warn">
          {{ formatNumber(todayAbnormal) }}
        </div>
        <div class="summary-bar__caption">异常事务</div>
      </div>
      <div class="summary-bar__stat">
        <div
          class="summary-bar__value"
          :class="{ 'summary-bar__value--danger': pending > 0 }"
        >
          {{ formatNumber(pending) }}
        </div>
        <div class="summary-bar__caption">待处理</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { HEALTH_EMPTY } from "@/api/gb28181";

interface Props {
  health: number;
  todayTotal: number;
  todayAbnormal: number;
  pending: number;
}
const props = defineProps<Props>();

const healthDisplay = computed((): string => {
  if (props.health === HEALTH_EMPTY) return "--";
  return props.health.toFixed(1);
});

const severityClass = computed((): string => {
  if (props.health === HEALTH_EMPTY) return "summary-bar--idle";
  if (props.health < 90) return "summary-bar--danger";
  if (props.health < 95) return "summary-bar--warn";
  return "summary-bar--ok";
});

function formatNumber(n: number): string {
  return n.toLocaleString("en-US");
}
</script>

<style scoped lang="scss">
.summary-bar {
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 14px 18px;
  border-radius: 10px;
  border: 1px solid var(--uvp-panel-border);
  background: var(--uvp-list-toolbar-bg);
  transition: all 0.3s ease;
}

.summary-bar--ok {
  background: rgb(15 170 166 / 9%);
  border-color: rgb(15 170 166 / 18%);
}

.summary-bar--warn {
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}

.summary-bar--danger {
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}

.summary-bar--idle {
  background: var(--uvp-list-toolbar-bg);
  border-color: var(--uvp-panel-border);
}

.summary-bar__health {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.summary-bar__num {
  font-size: 32px;
  font-weight: 700;
  color: var(--uvp-brand-cyan);
  letter-spacing: 0;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  transition: color 0.3s;
}

.summary-bar--warn .summary-bar__num {
  color: var(--uvp-warning);
}
.summary-bar--danger .summary-bar__num {
  color: var(--uvp-danger);
}
.summary-bar--idle .summary-bar__num {
  color: var(--uvp-text-tertiary);
}

.summary-bar__label {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0.5px;
}

.summary-bar__divider {
  width: 1px;
  height: 32px;
  background: var(--uvp-panel-border);
}

.summary-bar__stats {
  display: flex;
  gap: 32px;
  flex: 1;
}

.summary-bar__stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.summary-bar__value {
  font-size: 18px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-text-primary);
}

.summary-bar__value--warn {
  color: var(--uvp-warning);
}

.summary-bar__value--danger {
  color: var(--uvp-danger);
}

.summary-bar__caption {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
</style>
