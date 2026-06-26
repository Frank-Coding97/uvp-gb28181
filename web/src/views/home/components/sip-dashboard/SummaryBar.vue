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
  border-radius: 6px;
  border: 1px solid #e8e8e8;
  background: #fafafa;
  transition: all 0.3s ease;
}

.summary-bar--ok {
  background: #f6ffed;
  border-color: #b7eb8f;
}

.summary-bar--warn {
  background: #fff7e6;
  border-color: #ffd591;
}

.summary-bar--danger {
  background: #fff1f0;
  border-color: #ffa39e;
}

.summary-bar--idle {
  background: #fafafa;
  border-color: #e8e8e8;
}

.summary-bar__health {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.summary-bar__num {
  font-size: 32px;
  font-weight: 700;
  color: #52c41a;
  letter-spacing: -1px;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  transition: color 0.3s;
}

.summary-bar--warn .summary-bar__num {
  color: #fa8c16;
}
.summary-bar--danger .summary-bar__num {
  color: #ff4d4f;
}
.summary-bar--idle .summary-bar__num {
  color: #bfbfbf;
}

.summary-bar__label {
  font-size: 12px;
  color: #999;
  letter-spacing: 0.5px;
}

.summary-bar__divider {
  width: 1px;
  height: 32px;
  background: #e8e8e8;
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
  color: #333;
}

.summary-bar__value--warn {
  color: #fa8c16;
}

.summary-bar__value--danger {
  color: #ff4d4f;
}

.summary-bar__caption {
  font-size: 12px;
  color: #999;
}
</style>
