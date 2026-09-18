<template>
  <svg class="mini-trend" viewBox="0 0 100 24" preserveAspectRatio="none" aria-hidden="true">
    <defs>
      <linearGradient :id="gradientId" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" :stop-color="color" stop-opacity="0.28" />
        <stop offset="100%" :stop-color="color" stop-opacity="0" />
      </linearGradient>
    </defs>
    <path class="mini-trend__area" :d="areaPath" :fill="`url(#${gradientId})`" />
    <path class="mini-trend__line" :d="linePath" :stroke="color" />
  </svg>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance } from "vue";

const props = withDefaults(defineProps<{ values: number[]; color?: string }>(), {
  color: "var(--uvp-brand)"
});
const gradientId = `mini-trend-${getCurrentInstance()?.uid ?? 0}`;

const points = computed(() => {
  if (!props.values.length) return [{ x: 0, y: 20 }, { x: 100, y: 20 }];
  const max = Math.max(...props.values, 1);
  const step = 100 / Math.max(props.values.length - 1, 1);
  const values = props.values.map((value, index) => ({ x: index * step, y: 22 - value / max * 18 }));
  return values.length === 1 ? [{ x: 0, y: values[0].y }, { x: 100, y: values[0].y }] : values;
});

const linePath = computed(() => points.value.reduce((path, point, index, values) => {
  if (!index) return `M ${point.x} ${point.y}`;
  const previous = values[index - 1];
  const middle = (previous.x + point.x) / 2;
  return `${path} C ${middle} ${previous.y}, ${middle} ${point.y}, ${point.x} ${point.y}`;
}, ""));
const areaPath = computed(() => `${linePath.value} L 100 24 L 0 24 Z`);
</script>

<style scoped>
.mini-trend{position:absolute;right:14px;bottom:44px;width:64px;height:30px;overflow:visible}.mini-trend__area,.mini-trend__line{pointer-events:none}.mini-trend__line{fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round;vector-effect:non-scaling-stroke}
</style>
