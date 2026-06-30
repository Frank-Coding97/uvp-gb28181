<script setup lang="ts">
/**
 * OnlineRing — 在线率环形(C3 / D1 通用)
 *
 * 用 SVG 圆环 stroke-dasharray 表达百分比:
 *   ≥ 90% → 主色蓝
 *   ≥ 70% → 中性蓝
 *   < 70% → 琥珀
 *   = 0   → 灰
 *
 * 中心显示百分比数值
 */
import { computed } from "vue";

interface Props {
  value: number; // 0-1
  size?: number; // px,default 36
}

const props = withDefaults(defineProps<Props>(), { size: 36 });

const ratio = computed(() => Math.max(0, Math.min(1, props.value)));
const percent = computed(() => Math.round(ratio.value * 100));

const radius = computed(() => (props.size - 4) / 2);
const circumference = computed(() => 2 * Math.PI * radius.value);
const offset = computed(() => circumference.value * (1 - ratio.value));

const colorClass = computed(() => {
  if (ratio.value === 0) return "zero";
  if (ratio.value >= 0.9) return "high";
  if (ratio.value >= 0.7) return "mid";
  return "low";
});
</script>

<template>
  <div :class="['online-ring', colorClass]" :style="{ width: `${size}px`, height: `${size}px` }">
    <svg :width="size" :height="size">
      <circle
        :cx="size / 2"
        :cy="size / 2"
        :r="radius"
        class="track"
        fill="none"
        stroke-width="3"
      />
      <circle
        :cx="size / 2"
        :cy="size / 2"
        :r="radius"
        class="bar"
        fill="none"
        stroke-width="3"
        stroke-linecap="round"
        :stroke-dasharray="circumference"
        :stroke-dashoffset="offset"
        :transform="`rotate(-90 ${size / 2} ${size / 2})`"
      />
    </svg>
    <span class="label">{{ percent }}</span>
  </div>
</template>

<style lang="scss" scoped>
.online-ring {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;

  .track {
    stroke: var(--color-border-2);
  }

  .label {
    position: absolute;
    font-size: 10px;
    font-weight: 600;
    font-family: var(--font-mono);
    color: var(--color-text-2);
  }

  &.high .bar {
    stroke: var(--primary-5);
  }
  &.mid .bar {
    stroke: #93c5fd;
  }
  &.low .bar {
    stroke: var(--status-warning);
  }
  &.zero {
    .bar {
      stroke: var(--color-border-3);
    }
    .label {
      color: var(--color-text-4);
    }
  }
}
</style>
