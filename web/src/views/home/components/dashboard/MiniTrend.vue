<template><svg class="mini-trend" viewBox="0 0 100 24" preserveAspectRatio="none" aria-hidden="true"><polyline :points="points" /></svg></template>
<script setup lang="ts">
import { computed } from "vue";
const props = defineProps<{ values: number[] }>();
const points = computed(() => {
  if (!props.values.length) return "0,20 100,20";
  const max = Math.max(...props.values, 1); const step = 100 / Math.max(props.values.length - 1, 1);
  return props.values.map((value, index) => `${index * step},${22 - value / max * 18}`).join(" ");
});
</script>
<style scoped>.mini-trend{position:absolute;right:14px;bottom:12px;width:58px;height:26px}.mini-trend polyline{fill:none;stroke:var(--uvp-brand);stroke-width:2;vector-effect:non-scaling-stroke}</style>
