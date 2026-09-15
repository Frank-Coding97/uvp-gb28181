<template>
  <div class="media-rate-area">
    <div class="media-rate-area__plot">
      <DashboardChart
        v-if="props.samples.length"
        :spec="chartSpec"
        title="媒体实时速率"
        :summary="summary"
      />
      <div v-else class="media-rate-area__empty" role="status">暂无采样</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { ChartDatum, MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";
import DashboardChart from "./DashboardChart.vue";

interface MediaRateSample {
  upstream: number;
  downstream: number;
  sampledAt: number;
}

const props = defineProps<{ samples: MediaRateSample[] }>();

const WINDOW_MS = 5 * 60 * 1000;
const DATA_ID = "dashboard-media-rate";

const latestSampledAt = computed(() => props.samples.at(-1)?.sampledAt ?? null);
const windowStart = computed(() => latestSampledAt.value === null ? null : latestSampledAt.value - WINDOW_MS);
const axisMax = computed(() => props.samples.length
  ? Math.max(1, ...props.samples.flatMap(sample => [sample.upstream, sample.downstream]))
  : 1);
const values = computed<ChartDatum[]>(() => props.samples.map(sample => ({
  upstream: sample.upstream,
  downstream: sample.downstream,
  sampledAt: sample.sampledAt
})));
const summary = computed(() => props.samples.length ? `最近 5 分钟已记录 ${props.samples.length} 个采样点` : "暂无采样");

const chartSpec = computed<MediaChartSpec>(() => {
  const latest = latestSampledAt.value;
  const start = windowStart.value;
  const timeDomain = start === null || latest === null ? {} : { min: start, max: latest };
  return {
    type: "common",
    background: "transparent",
    data: [{ id: DATA_ID, values: values.value }],
    series: [
      createRateSeries("upstream", "实时上行", "var(--uvp-brand)", 0.22, latest),
      createRateSeries("downstream", "实时下行", "var(--uvp-brand-cyan)", 0.18, latest)
    ],
    axes: [
      {
        orient: "left",
        type: "linear",
        min: 0,
        max: axisMax.value,
        nice: false,
        label: { formatMethod: (value: number) => formatRate(value), autoHide: true, style: { fill: "var(--uvp-text-tertiary)" } },
        grid: { visible: true, style: { stroke: "var(--uvp-panel-border)" } },
        domainLine: { visible: false },
        tick: { visible: false, tickCount: 3 }
      },
      {
        orient: "bottom",
        type: "time",
        nice: false,
        ...timeDomain,
        layers: [{ tickCount: 3, timeFormat: "%H:%M", timeFormatMode: "local" }],
        label: { autoHide: false, autoRotate: false, style: { fill: "var(--uvp-text-tertiary)" } },
        grid: { visible: false },
        domainLine: { visible: false },
        tick: { visible: false }
      }
    ],
    legends: { visible: false },
    tooltip: { activeType: "dimension", confine: true },
    crosshair: { xField: { visible: true, line: { type: "line", style: { stroke: "var(--uvp-text-tertiary)", lineDash: [2, 2] } } } },
    padding: { left: 8, right: 12, top: 8, bottom: 8 },
    animationAppear: { duration: 220 },
    animationUpdate: { duration: 220, easing: "linear" }
  };
});

function createRateSeries(field: "upstream" | "downstream", label: string, color: string, fillOpacity: number, latest: number | null) {
  return {
    type: "area",
    data: { id: DATA_ID },
    xField: "sampledAt",
    yField: field,
    invalidType: "break",
    line: { style: { curveType: "monotone", stroke: color, lineWidth: 1.5 } },
    area: {
      style: {
        curveType: "monotone",
        fillOpacity,
        fill: {
          gradient: "linear",
          x0: 0,
          y0: 0,
          x1: 0,
          y1: 1,
          stops: [
            { offset: 0, color, opacity: 0.78 },
            { offset: 0.68, color, opacity: 0.24 },
            { offset: 1, color, opacity: 0.04 }
          ]
        }
      }
    },
    point: {
      visible: true,
      style: {
        size: (datum: ChartDatum) => datum.sampledAt === latest ? 6 : 0,
        fill: color,
        stroke: "var(--uvp-panel-bg)",
        lineWidth: 1
      },
      state: { dimension_hover: { size: 6.4 } }
    },
    tooltip: {
      dimension: {
        title: { value: { field: "sampledAt" }, valueTimeFormat: "%H:%M:%S", valueTimeFormatMode: "local" },
        content: [{ key: label, value: (datum?: ChartDatum) => formatRate(datum?.[field]) }]
      }
    }
  };
}

function formatRate(value: unknown): string {
  const numeric = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(numeric)) return "—";
  const rate = Math.max(0, numeric);
  if (rate >= 1024 ** 3) return `${(rate / 1024 ** 3).toFixed(1)} GB/s`;
  if (rate >= 1024 ** 2) return `${(rate / 1024 ** 2).toFixed(1)} MB/s`;
  if (rate >= 1024) return `${(rate / 1024).toFixed(1)} KB/s`;
  return `${rate.toFixed(rate > 0 && rate < 1 ? 1 : 0)} B/s`;
}
</script>

<style scoped>
.media-rate-area{display:flex;flex-direction:column;height:calc(100% - 74px);min-height:150px;min-width:0;overflow:hidden}.media-rate-area__plot{display:grid;flex:1;grid-template-rows:minmax(0,1fr);min-height:0;overflow:hidden}.media-rate-area__plot :deep(.dashboard-chart){width:100%;height:100%;min-height:0}.media-rate-area__empty{display:flex;align-items:center;justify-content:center;min-height:150px;color:var(--uvp-text-tertiary);font-size:12px}
</style>
