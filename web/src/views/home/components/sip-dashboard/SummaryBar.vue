<template>
  <div class="summary-bar" :class="severityClass">
    <div class="summary-bar__health">
      <span class="summary-bar__num">{{ healthDisplay }}</span>
      <span class="summary-bar__label">% 接入健康度</span>
    </div>
    <div class="summary-bar__divider" />
    <div class="summary-bar__stats">
      <div class="summary-bar__stat">
        <div class="summary-bar__value">{{ summaryValue(todayTotal) }}</div>
        <div class="summary-bar__caption">今日事务</div>
      </div>
      <div class="summary-bar__stat">
        <div
          class="summary-bar__value"
          :class="{ 'summary-bar__value--warn': health !== HEALTH_EMPTY && todayAbnormal > 0 }"
        >
          {{ summaryValue(todayAbnormal) }}
        </div>
        <div class="summary-bar__caption">异常事务</div>
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

function summaryValue(n: number): string {
  return props.health === HEALTH_EMPTY ? "--" : formatNumber(n);
}
</script>

<style scoped lang="scss">
.summary-bar {
  display: grid;
  grid-template-columns: minmax(180px, 1.15fr) 1px minmax(0, 2fr);
  gap: 24px;
  align-items: center;
  padding: 10px 2px 14px;
  border-bottom: 1px solid var(--uvp-panel-border);
}

.summary-bar__health {
  display: flex;
  gap: 8px;
  align-items: baseline;
}

.summary-bar__num {
  font-size: 34px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  color: var(--uvp-brand-cyan);
  letter-spacing: 0;
  transition: color 0.2s ease;
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
  letter-spacing: 0;
}

.summary-bar__divider {
  width: 1px;
  height: 32px;
  background: var(--uvp-panel-border);
}

.summary-bar__stats {
  display: flex;
  flex: 1;
  gap: 38px;
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
