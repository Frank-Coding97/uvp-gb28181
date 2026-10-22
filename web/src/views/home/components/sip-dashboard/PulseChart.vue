<template>
  <div class="pulse">
    <div class="pulse__head">
      <div class="pulse__title">
        信令脉搏 · 最近 60 分钟
        <span v-if="hasSamples" class="pulse__stats">
          峰值 <b>{{ maxMsg }}</b> · 最近完整分钟 <b>{{ currentMsg }}</b>
          <span v-if="currentBucket">
            · 本分钟累计 <b>{{ currentBucket.known === false ? "--" : currentBucket.msgPerSec }}</b> 条</span
          >
        </span>
      </div>
      <div class="pulse__legend">
        <span><i class="pulse__legend-line pulse__legend-line--blue" />事务数 / 分钟（左轴）</span>
        <span><i class="pulse__legend-line pulse__legend-line--red" />失败率（右轴）</span>
      </div>
    </div>
    <div class="pulse__chart">
      <DashboardChart
        v-if="hasData"
        :spec="spec"
        title="SIP 信令脉搏"
        :summary="`峰值 ${maxMsg}，最近完整分钟 ${currentMsg} 条/分钟`"
      />
      <div v-else class="pulse__empty">{{ currentBucket ? "等待首个完整分钟" : "暂无信令" }}</div>
    </div>
    <div class="pulse__hint">
      采样 1m · 曲线仅含完整分钟 · {{ hasGap ? "统计存在缺口（缺口按 0 计入）" : "仅显示 GB28181 事务" }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { PulseSample, AbnormalWindow } from "@/api/gb28181";
import type { MediaChartSpec } from "@/views/gb28181/zlm/workbench/chart/overviewChart";
import DashboardChart from "../dashboard/DashboardChart.vue";

const props = defineProps<{ samples: PulseSample[]; abnormalWindows: AbnormalWindow[]; asOf?: number }>();
// Use server snapshot time so client clock skew cannot turn a partial bucket into a full minute.
const completeSamples = computed(() => {
  const asOf = props.asOf;
  return asOf == null ? props.samples : props.samples.filter(sample => sample.t + 60 <= asOf);
});
const currentBucket = computed(() => {
  const sample = props.samples.at(-1);
  return sample && props.asOf != null && sample.t <= props.asOf && props.asOf < sample.t + 60 ? sample : null;
});
const hasSamples = computed(() => props.samples.length > 0);
const knownSamples = computed(() => completeSamples.value.filter(sample => sample.known !== false));
const hasGap = computed(() => props.samples.some(sample => sample.known === false));
// 只要拿到了完整分钟的桶就出图：统计覆盖未知的分钟按 0 绘制，不再折叠成"暂无信令"，
// 否则整窗都是空桶时仪表盘会退化成一句占位文案，看不出"确实是 0 条"。
const hasData = computed(() => completeSamples.value.length > 0);
const maxMsg = computed(() => Math.max(0, ...knownSamples.value.map(sample => sample.msgPerSec)));
const axisMax = computed(() => Math.max(1, Math.ceil(maxMsg.value * 1.1)));
const currentMsg = computed(() => {
  const current = completeSamples.value.at(-1);
  return current && current.known !== false ? current.msgPerSec : "--";
});

interface PulseDatum {
  time: number;
  messages: number;
  failure: number;
  known: boolean;
}
function timeLabel(datum: PulseDatum): string {
  return new Date(datum.time).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
}

const spec = computed<MediaChartSpec>(() => {
  // ⛔ 曲线必须连续：统计覆盖未知的分钟按 0 绘制。
  // 原先 unknown ⇒ null + invalidType: break，一条断成好几截的折线在仪表盘上
  // 会被读成"采集中断/平台掉线"，而实际上那条分钟就是没有信令（= 0 条）。
  // "存在缺口"这件事改由底部提示与 tooltip 说明承载，曲线本身不再断开。
  const values = completeSamples.value.map(sample => {
    const known = sample.known !== false;
    return {
      time: sample.t * 1000,
      messages: known ? sample.msgPerSec : 0,
      failure: known ? sample.failPct / 10 : 0,
      known
    };
  });
  const first = values[0]?.time ?? 0;
  const last = values.at(-1)?.time ?? first;
  return {
    type: "common",
    background: "transparent",
    animation: false,
    padding: { left: 8, right: 12, top: 8, bottom: 8 },
    data: [{ id: "pulse", values }],
    series: [
      {
        type: "area",
        invalidType: "break",
        data: { id: "pulse" },
        xField: "time",
        yField: "messages",
        line: { style: { curveType: "monotone", stroke: "var(--uvp-brand)", lineWidth: 1.5 } },
        // 面积观感对齐"媒体实时速率"：品牌色 + 纵向渐隐，靠 fillOpacity 控制整体浓度。
        area: {
          style: {
            curveType: "monotone",
            fillOpacity: 0.22,
            fill: {
              gradient: "linear",
              x0: 0,
              y0: 0,
              x1: 0,
              y1: 1,
              stops: [
                { offset: 0, color: "var(--uvp-brand)", opacity: 0.78 },
                { offset: 0.68, color: "var(--uvp-brand)", opacity: 0.24 },
                { offset: 1, color: "var(--uvp-brand)", opacity: 0.04 }
              ]
            }
          }
        },
        point: {
          visible: true,
          style: {
            // 端点标记只看"是不是最新完整分钟"——不再要求统计已知，
            // 否则最新一分钟是缺口时右侧末端会连点都没有，又像断了一截。
            size: (datum: PulseDatum) => (datum.time === last ? 6 : 0),
            fill: "var(--uvp-brand)",
            stroke: "var(--uvp-panel-bg)",
            lineWidth: 1
          },
          state: { dimension_hover: { size: 6.4 } }
        },
        tooltip: {
          dimension: {
            title: { value: timeLabel },
            content: [
              {
                key: "事务",
                value: (datum: PulseDatum) => (datum.known ? `${datum.messages} 条/分钟` : "0 条/分钟（统计缺口）")
              }
            ]
          }
        }
      },
      {
        type: "line",
        invalidType: "break",
        data: { id: "pulse" },
        xField: "time",
        yField: "failure",
        line: { style: { curveType: "monotone", stroke: "var(--uvp-danger)", lineWidth: 1, lineDash: [2, 2], opacity: 0.7 } },
        point: { visible: false },
        tooltip: {
          dimension: {
            title: { value: timeLabel },
            content: [
              {
                key: "失败率",
                value: (datum: PulseDatum) =>
                  datum.known === false ? "0%（统计缺口）" : datum.failure === 0 ? "0%" : `${datum.failure.toFixed(1)}%`
              }
            ]
          }
        }
      }
    ],
    axes: [
      {
        orient: "bottom",
        type: "time",
        visible: true,
        layers: [{ tickCount: 3, timeFormat: "%H:%M", timeFormatMode: "local" }],
        label: {
          autoHide: true,
          autoRotate: false,
          style: { fill: "var(--uvp-text-tertiary)", fontSize: 10 }
        },
        domainLine: { visible: false },
        tick: { visible: false },
        grid: { visible: false },
        min: first === last ? first - 60000 : first,
        max: last,
        zero: false,
        nice: false
      },
      {
        orient: "left",
        type: "linear",
        visible: true,
        seriesIndex: [0],
        min: 0,
        max: axisMax.value,
        nice: false,
        tickCount: 3,
        noDecimals: true,
        label: { autoHide: true, style: { fill: "var(--uvp-text-tertiary)", fontSize: 10 } },
        domainLine: { visible: false },
        tick: { visible: false },
        grid: { visible: true, style: { stroke: "var(--uvp-panel-border)" } }
      },
      {
        orient: "right",
        type: "linear",
        visible: true,
        seriesIndex: [1],
        min: 0,
        max: 100,
        nice: false,
        tickCount: 3,
        label: { formatMethod: (value: number) => `${value}%`, style: { fill: "var(--uvp-text-tertiary)", fontSize: 10 } },
        domainLine: { visible: false },
        tick: { visible: false },
        grid: { visible: false }
      }
    ],
    crosshair: {
      xField: { visible: true, line: { type: "line", style: { stroke: "var(--uvp-text-tertiary)", lineDash: [2, 2] } } }
    },
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
  min-width: 0;
  min-height: 150px;
  overflow: hidden;
}

.pulse__head {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
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
  flex-wrap: wrap;
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
  height: 0;
  background: transparent;
  border-top: 2px dashed var(--uvp-danger);
}
.pulse__chart {
  display: grid;
  flex: 1;
  grid-template-rows: minmax(0, 1fr);
  width: 100%;
  min-height: 150px;
  overflow: hidden;
}

.pulse__chart > .dashboard-chart {
  position: relative;
  min-height: 150px;
}

.pulse__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 150px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

.pulse__hint {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}
</style>
