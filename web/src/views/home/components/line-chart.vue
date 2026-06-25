<template>
  <svg :viewBox="`0 0 ${W} ${H}`" class="line-chart" preserveAspectRatio="none">
    <!-- legend (top) -->
    <g class="legend">
      <g v-for="(s, i) in series" :key="s.label" :transform="`translate(${legendX(i)}, 8)`">
        <line :x1="0" :y1="6" :x2="14" :y2="6" :stroke="s.color" stroke-width="2" />
        <text x="20" y="10" class="legend-text">{{ s.label }}</text>
      </g>
    </g>

    <!-- y axis grid -->
    <g class="grid">
      <line v-for="g in yGrid" :key="g.label" :x1="padL" :y1="g.y" :x2="W - padR" :y2="g.y" stroke="#f0f0f0" stroke-dasharray="4 4" />
      <text v-for="g in yGrid" :key="'t' + g.label" :x="padL - 6" :y="g.y + 3" class="axis-text" text-anchor="end">{{ g.label }}</text>
    </g>

    <!-- x axis labels -->
    <g class="x-axis">
      <text v-for="(l, i) in labels" :key="l" :x="xPos(i)" :y="H - 6" class="axis-text" text-anchor="middle">{{ l }}</text>
    </g>

    <!-- y axis title -->
    <text :x="6" :y="40" class="axis-title">数量(次)</text>

    <!-- lines + points -->
    <g v-for="s in series" :key="s.label">
      <polyline :points="linePoints(s.data)" fill="none" :stroke="s.color" stroke-width="2" />
      <circle v-for="(v, i) in s.data" :key="i" :cx="xPos(i)" :cy="yPos(v)" r="2.5" :fill="s.color" />
    </g>
  </svg>
</template>

<script setup lang="ts">
import { computed } from "vue";

interface Series {
  label: string;
  color: string;
  data: number[];
}
const props = defineProps<{ series: Series[]; labels: string[] }>();

const W = 460;
const H = 200;
const padL = 36;
const padR = 12;
const padT = 36;
const padB = 22;

const maxVal = computed(() => {
  const all = props.series.flatMap((s) => s.data);
  const m = Math.max(...all);
  return Math.ceil(m / 200) * 200 || 200;
});

const yGrid = computed(() => {
  const steps = 4;
  const arr: { y: number; label: string }[] = [];
  for (let i = 0; i <= steps; i++) {
    const v = maxVal.value * (1 - i / steps);
    const y = padT + ((H - padT - padB) * i) / steps;
    arr.push({ y, label: String(Math.round(v)) });
  }
  return arr;
});

const xPos = (i: number) => {
  const usable = W - padL - padR;
  return padL + (usable * i) / (props.labels.length - 1);
};
const yPos = (v: number) => padT + (H - padT - padB) * (1 - v / maxVal.value);

const linePoints = (data: number[]) => data.map((v, i) => `${xPos(i)},${yPos(v)}`).join(" ");

const legendX = (i: number) => 50 + i * 80;
</script>

<style scoped lang="scss">
.line-chart {
  width: 100%;
  height: 200px;
  display: block;
}
.legend-text {
  font-size: 11px;
  fill: #666;
  font-family: -apple-system, "PingFang SC", sans-serif;
  dominant-baseline: middle;
}
.axis-text {
  font-size: 10px;
  fill: #999;
  font-family: -apple-system, "PingFang SC", sans-serif;
}
.axis-title {
  font-size: 11px;
  fill: #999;
  font-family: -apple-system, "PingFang SC", sans-serif;
}
</style>
