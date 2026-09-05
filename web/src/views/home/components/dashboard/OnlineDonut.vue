<template>
  <div class="online-donut">
    <div class="donut">
      <DashboardChart class="donut-chart" :spec="chartSpec" :title="label" :summary="summary" />
      <div class="donut-center"><strong>{{ ratio.toFixed(1) }}%</strong><small>{{ label }}</small></div>
    </div>
    <div class="legend"><span><i class="online" />在线<strong>{{ online }}</strong></span><span><i />离线<strong>{{ offline }}</strong></span><span><i class="total" />总计<strong>{{ total }}</strong></span></div>
  </div>
</template>
<script setup lang="ts">
import { computed } from "vue";
import type { MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";
import DashboardChart from "./DashboardChart.vue";

const props = defineProps<{ online: number; total: number; label: string }>();
const offline = computed(() => Math.max(0, props.total - props.online));
const ratio = computed(() => props.total > 0 ? Math.min(100, props.online / props.total * 100) : 0);
const summary = computed(() => `${props.label}：在线 ${props.online}，离线 ${offline.value}，总计 ${props.total}，在线率 ${ratio.value.toFixed(1)}%`);
const chartSpec = computed<MediaChartSpec>(() => ({
  type: "pie",
  background: "transparent",
  data: [{
    id: "online-donut",
    values: [
      { category: "在线", value: props.online },
      { category: "离线", value: offline.value }
    ]
  }],
  categoryField: "category",
  valueField: "value",
  color: ["var(--uvp-brand-cyan)", "var(--uvp-list-toolbar-bg)"],
  outerRadius: 1,
  innerRadius: 78 / 112,
  startAngle: -90,
  endAngle: 270,
  padding: 0,
  minAngle: 0,
  padAngle: 0,
  showAllZero: false,
  emptyPlaceholder: {
    showEmptyCircle: true,
    emptyCircle: { style: { fill: "var(--uvp-list-toolbar-bg)", cornerRadius: 0 } }
  },
  pie: { style: { cornerRadius: 0 } },
  legends: { visible: false },
  label: { visible: false },
  tooltip: { visible: false }
}));
</script>
<style scoped>
.online-donut{display:flex;height:calc(100% - 28px);flex-direction:column;align-items:center;justify-content:center;gap:15px}.donut{position:relative;flex:none;width:112px;height:112px}.donut-chart{position:absolute;inset:0;width:100%;height:100%}.donut-center{position:absolute;top:50%;left:50%;display:flex;width:78px;height:78px;flex-direction:column;align-items:center;justify-content:center;pointer-events:none;background:var(--uvp-panel-bg);border-radius:50%;transform:translate(-50%,-50%)}.donut-center strong{font-size:22px}.donut-center small{font-size:10px;color:var(--uvp-text-tertiary)}.legend{display:flex;width:100%;flex-direction:column;gap:8px;font-size:11px}.legend span{display:grid;grid-template-columns:10px 1fr auto;align-items:center;border-bottom:1px solid var(--uvp-panel-border);padding-bottom:5px}.legend i{width:7px;height:7px;background:var(--uvp-text-tertiary);border-radius:50%}.legend i.online{background:var(--uvp-brand-cyan)}.legend i.total{background:var(--uvp-brand)}
</style>
