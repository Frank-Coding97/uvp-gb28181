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
  gap: 20px;
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid rgba(61, 220, 132, 0.2);
  background: rgba(61, 220, 132, 0.08);
  transition: all 0.3s ease;
}

.summary-bar--warn {
  background: rgba(255, 181, 71, 0.08);
  border-color: rgba(255, 181, 71, 0.25);
}

.summary-bar--danger {
  background: rgba(255, 90, 95, 0.08);
  border-color: rgba(255, 90, 95, 0.25);
}

.summary-bar--idle {
  background: rgba(90, 100, 120, 0.08);
  border-color: rgba(90, 100, 120, 0.25);
}

.summary-bar__health {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.summary-bar__num {
  font-size: 32px;
  font-weight: 700;
  color: #3ddc84;
  letter-spacing: -1px;
  font-variant-numeric: tabular-nums;
  transition: color 0.3s;
}

.summary-bar--warn .summary-bar__num {
  color: #ffb547;
}
.summary-bar--danger .summary-bar__num {
  color: #ff5a5f;
}
.summary-bar--idle .summary-bar__num {
  color: #8b97ad;
}

.summary-bar__label {
  font-size: 11px;
  color: #8b97ad;
  letter-spacing: 0.5px;
}

.summary-bar__divider {
  width: 1px;
  height: 32px;
  background: #1f2a40;
}

.summary-bar__stats {
  display: flex;
  gap: 24px;
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
  color: #e6edf7;
}

.summary-bar__value--warn {
  color: #ffb547;
}

.summary-bar__value--danger {
  color: #ff5a5f;
}

.summary-bar__caption {
  font-size: 11px;
  color: #5a6478;
}
</style>
