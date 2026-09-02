<template>
  <div class="online-donut">
    <div class="donut" :style="{ background: `conic-gradient(var(--uvp-brand-cyan) ${ratio}%, var(--uvp-list-toolbar-bg) 0)` }"><div><strong>{{ ratio.toFixed(1) }}%</strong><small>{{ label }}</small></div></div>
    <div class="legend"><span><i class="online" />在线<strong>{{ online }}</strong></span><span><i />离线<strong>{{ offline }}</strong></span><span><i class="total" />总计<strong>{{ total }}</strong></span></div>
  </div>
</template>
<script setup lang="ts">
import { computed } from "vue";
const props = defineProps<{ online: number; total: number; label: string }>();
const offline = computed(() => Math.max(0, props.total - props.online));
const ratio = computed(() => props.total > 0 ? Math.min(100, props.online / props.total * 100) : 0);
</script>
<style scoped>
.online-donut{display:flex;height:calc(100% - 28px);flex-direction:column;align-items:center;justify-content:center;gap:15px}.donut{display:grid;width:112px;height:112px;border-radius:50%;place-items:center}.donut>div{display:flex;width:78px;height:78px;flex-direction:column;align-items:center;justify-content:center;background:var(--uvp-panel-bg);border-radius:50%}.donut strong{font-size:22px}.donut small{font-size:10px;color:var(--uvp-text-tertiary)}.legend{display:flex;width:100%;flex-direction:column;gap:8px;font-size:11px}.legend span{display:grid;grid-template-columns:10px 1fr auto;align-items:center;border-bottom:1px solid var(--uvp-panel-border);padding-bottom:5px}.legend i{width:7px;height:7px;background:var(--uvp-text-tertiary);border-radius:50%}.legend i.online{background:var(--uvp-brand-cyan)}.legend i.total{background:var(--uvp-brand)}
</style>
