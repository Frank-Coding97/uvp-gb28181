<template>
  <div class="pulse">
    <div class="pulse__head">
      <div class="pulse__title">
        信令脉搏 · 最近 60 分钟
        <span v-if="hasData" class="pulse__stats">
          峰值 <b>{{ maxMsg }}</b> · 当前 <b>{{ currentMsg }}</b>
        </span>
      </div>
      <div class="pulse__legend">
        <span><i class="pulse__legend-line pulse__legend-line--blue" />消息数 / 分钟</span>
        <span><i class="pulse__legend-line pulse__legend-line--red" />失败率</span>
      </div>
    </div>
    <div class="pulse__chart">
      <DashboardChart v-if="hasData" :spec="spec" title="SIP 信令脉搏" :summary="`峰值 ${maxMsg}，当前 ${currentMsg} 条/分钟`" />
      <div v-else class="pulse__empty">暂无信令</div>
    </div>
    <div class="pulse__hint">采样 1m · 仅显示 GB28181 信令</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { PulseSample, AbnormalWindow } from "@/api/gb28181";
import type { MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";
import DashboardChart from "../dashboard/DashboardChart.vue";

const props = defineProps<{ samples: PulseSample[]; abnormalWindows: AbnormalWindow[] }>();
const hasData = computed(() => props.samples.some(sample => sample.msgPerSec > 0 || sample.failPct > 0));
const maxMsg = computed(() => Math.max(1, ...props.samples.map(sample => sample.msgPerSec)));
const currentMsg = computed(() => props.samples.at(-1)?.msgPerSec ?? 0);

interface PulseDatum { time: number; messages: number; failure: number; }
function timeLabel(datum: PulseDatum): string {
  return new Date(datum.time).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
}

const spec = computed<MediaChartSpec>(() => {
  const values = props.samples.map(sample => ({ time: sample.t * 1000, messages: sample.msgPerSec, failure: sample.failPct / 10 }));
  const first = values[0]?.time ?? 0;
  const last = values.at(-1)?.time ?? first;
  return {
    type: "common",
    background: "transparent",
    animation: false,
    padding: { left: 4, right: 4, top: 5, bottom: 5 },
    data: [{ id: "pulse", values }],
    series: [
      {
        type: "area", data: { id: "pulse" }, xField: "time", yField: "messages",
        line: { style: { curveType: "monotone", stroke: "var(--uvp-brand)", lineWidth: 1.5 } },
        area: { style: { curveType: "monotone", fillOpacity: 0.28, fill: { gradient: "linear", x0: 0, y0: 0, x1: 0, y1: 1, stops: [{ offset: 0, color: "var(--uvp-brand)" }, { offset: 1, color: "transparent" }] } } },
        point: { visible: true, style: { size: (datum: PulseDatum) => datum.time === last ? 5.6 : 0, fill: "var(--uvp-brand)", stroke: "var(--uvp-panel-bg)", lineWidth: 1 }, state: { dimension_hover: { size: 6.4 } } },
        tooltip: { dimension: { title: { value: timeLabel }, content: [{ key: "消息", value: (datum: PulseDatum) => `${datum.messages} 条/分钟` }] } }
      },
      {
        type: "line", data: { id: "pulse" }, xField: "time", yField: "failure",
        line: { style: { curveType: "monotone", stroke: "var(--uvp-danger)", lineWidth: 1, lineDash: [2, 2], opacity: 0.7 } },
        point: { visible: false },
        tooltip: { dimension: { title: { value: timeLabel }, content: [{ key: "失败率", value: (datum: PulseDatum) => datum.failure === 0 ? "0%" : `${datum.failure.toFixed(1)}%` }] } }
      }
    ],
    axes: [
      { orient: "bottom", type: "linear", visible: false, min: first === last ? first - 60000 : first, max: last, zero: false, nice: false },
      { orient: "left", type: "linear", visible: false, seriesIndex: [0], min: 0, max: maxMsg.value, nice: false },
      { orient: "right", type: "linear", visible: false, seriesIndex: [1], min: 0, max: 100, nice: false }
    ],
    markArea: props.abnormalWindows
      .filter(win => win.endT * 1000 > first && win.startT * 1000 < last)
      .map(win => ({ x: Math.max(first, win.startT * 1000), x1: Math.min(last, win.endT * 1000), relativeSeriesIndex: 0, area: { style: { fill: "var(--uvp-danger-soft)" } }, label: { visible: false }, interactive: false })),
    crosshair: { xField: { visible: true, line: { type: "line", style: { stroke: "var(--uvp-text-tertiary)", lineDash: [2, 2] } } } },
    tooltip: { activeType: "dimension", confine: true },
    legends: { visible: false }
  };
});
</script>

<style scoped lang="scss">
.pulse {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}

.pulse__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.pulse__title {
  font-size: 12px;
  color: var(--uvp-text-secondary);
  letter-spacing: 0;
}

.pulse__stats {
  margin-left: 12px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0;

  b {
    margin: 0 1px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    color: var(--uvp-brand);
  }
}

.pulse__legend {
  display: flex;
  gap: 14px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.pulse__legend-line {
  display: inline-block;
  width: 10px;
  height: 2px;
  margin-right: 5px;
  vertical-align: middle;
  background: var(--uvp-brand);
}

.pulse__legend-line--red {
  background: var(--uvp-danger);
}

.pulse__chart {
  position: relative;
  flex: 1;
  width: 100%;
  height: 90px;
  min-height: 90px;
}

.pulse__chart > .dashboard-chart {
  position: absolute;
  inset: 0;
}

.pulse__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 90px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

.pulse__hint {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}
</style>
