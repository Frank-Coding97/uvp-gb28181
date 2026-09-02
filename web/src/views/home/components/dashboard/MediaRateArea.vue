<template>
  <div class="media-rate-area">
    <div class="media-rate-area__plot">
      <div class="media-rate-area__y-axis"><span>{{ yAxisLabels[0] }}</span><span>{{ yAxisLabels[1] }}</span><span>{{ yAxisLabels[2] }}</span></div>
      <svg viewBox="0 0 100 36" preserveAspectRatio="none" aria-label="媒体实时速率趋势">
        <defs>
          <linearGradient :id="gradientId" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" :stop-color="color" stop-opacity="0.3" />
            <stop offset="100%" :stop-color="color" stop-opacity="0.02" />
          </linearGradient>
        </defs>
        <path v-for="y in [8, 20, 32]" :key="y" class="media-rate-area__grid" :d="`M 0 ${y} L 100 ${y}`" />
        <path class="media-rate-area__fill" :d="areaPath" :fill="`url(#${gradientId})`" />
        <path class="media-rate-area__line" :d="linePath" :stroke="color" />
        <circle v-if="points.length" class="media-rate-area__point" :cx="points.at(-1)?.x" :cy="points.at(-1)?.y" r="1.25" :fill="color" />
      </svg>
    </div>
    <div class="media-rate-area__x-axis"><span>2 分钟前</span><span>1 分钟前</span><span>现在</span></div>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance } from "vue";

const props = withDefaults(defineProps<{ values: number[]; color?: string }>(), {
  color: "var(--uvp-brand)"
});
const gradientId = `media-rate-area-${getCurrentInstance()?.uid ?? 0}`;
const axisMax = computed(() => props.values.length ? Math.max(...props.values, 1) : null);
const yAxisLabels = computed(() => axisMax.value == null ? ["--", "--", "0 B/s"] : [formatRate(axisMax.value), formatRate(axisMax.value / 2), "0 B/s"]);

const points = computed(() => {
  if (!props.values.length) return [{ x: 0, y: 32 }, { x: 100, y: 32 }];
  const max = Math.max(...props.values, 1);
  const step = 100 / Math.max(props.values.length - 1, 1);
  const values = props.values.map((value, index) => ({ x: index * step, y: 32 - Math.max(0, value) / max * 26 }));
  return values.length === 1 ? [{ x: 0, y: values[0].y }, { x: 100, y: values[0].y }] : values;
});

const linePath = computed(() => points.value.reduce((path, point, index, values) => {
  if (!index) return `M ${point.x} ${point.y}`;
  const previous = values[index - 1];
  const middle = (previous.x + point.x) / 2;
  return `${path} C ${middle} ${previous.y}, ${middle} ${point.y}, ${point.x} ${point.y}`;
}, ""));
const areaPath = computed(() => `${linePath.value} L 100 36 L 0 36 Z`);

function formatRate(value: number): string {
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB/s`;
  if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MB/s`;
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB/s`;
  return `${value.toFixed(0)} B/s`;
}
</script>

<style scoped>
.media-rate-area{display:flex;flex-direction:column;height:calc(100% - 74px);min-height:150px}.media-rate-area__plot{display:grid;flex:1;grid-template-columns:54px minmax(0,1fr);min-height:0}.media-rate-area__plot svg{width:100%;height:100%;overflow:visible}.media-rate-area__y-axis{display:flex;flex-direction:column;justify-content:space-between;padding:1px 8px 9px 0;font-size:10px;color:var(--uvp-text-tertiary);text-align:right;white-space:nowrap}.media-rate-area__grid{fill:none;stroke:var(--uvp-panel-border);stroke-width:.45;vector-effect:non-scaling-stroke}.media-rate-area__fill,.media-rate-area__line{pointer-events:none}.media-rate-area__line{fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round;vector-effect:non-scaling-stroke}.media-rate-area__point{stroke:var(--uvp-panel-bg);stroke-width:1;vector-effect:non-scaling-stroke}.media-rate-area__x-axis{display:flex;justify-content:space-between;padding-left:54px;margin-top:5px;font-size:10px;color:var(--uvp-text-tertiary)}
</style>
