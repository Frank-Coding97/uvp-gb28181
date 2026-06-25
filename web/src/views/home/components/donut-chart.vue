<template>
  <svg viewBox="0 0 130 130" class="donut">
    <g transform="translate(65 65)">
      <circle
        v-for="(seg, i) in segments"
        :key="i"
        r="40"
        cx="0"
        cy="0"
        fill="transparent"
        :stroke="seg.color"
        :stroke-width="16"
        :stroke-dasharray="`${seg.len} ${circumference - seg.len}`"
        :stroke-dashoffset="-seg.offset"
        transform="rotate(-90)"
      />
      <text x="0" y="-2" class="total" text-anchor="middle">{{ total }}</text>
      <text x="0" y="14" class="label" text-anchor="middle">{{ totalLabel }}</text>
    </g>
  </svg>
</template>

<script setup lang="ts">
import { computed } from "vue";

interface DataItem {
  name: string;
  count: number;
  color: string;
}
const props = defineProps<{
  data: DataItem[];
  total: string;
  totalLabel: string;
}>();

const r = 40;
const circumference = 2 * Math.PI * r;

const segments = computed(() => {
  const sum = props.data.reduce((a, b) => a + b.count, 0);
  let offset = 0;
  return props.data.map((d) => {
    const len = (d.count / sum) * circumference;
    const seg = { color: d.color, len, offset };
    offset += len;
    return seg;
  });
});
</script>

<style scoped lang="scss">
.donut {
  width: 130px;
  height: 130px;
  flex-shrink: 0;
}
.total {
  font-size: 24px;
  font-weight: 700;
  fill: #333;
  font-family: -apple-system, "PingFang SC", sans-serif;
}
.label {
  font-size: 11px;
  fill: #999;
  font-family: -apple-system, "PingFang SC", sans-serif;
}
</style>
