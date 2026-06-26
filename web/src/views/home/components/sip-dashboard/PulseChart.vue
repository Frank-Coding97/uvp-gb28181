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
    <div
      class="pulse__chart"
      @mousemove="onMove"
      @mouseleave="hoverIdx = -1"
    >
      <svg
        v-if="hasData"
        ref="svgEl"
        class="pulse__svg"
        :viewBox="`0 0 ${W} ${H}`"
        preserveAspectRatio="none"
      >
        <!-- 顶蓝到底透明的渐变,给面积一点呼吸感 -->
        <defs>
          <linearGradient id="pulseMsgGradient" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stop-color="#1890ff" stop-opacity="0.28" />
            <stop offset="100%" stop-color="#1890ff" stop-opacity="0.02" />
          </linearGradient>
        </defs>

        <!-- 异常时间窗背景 -->
        <rect
          v-for="(win, i) in abnormalRects"
          :key="i"
          :x="win.x"
          y="0"
          :width="win.w"
          :height="H"
          fill="rgba(255, 77, 79, 0.08)"
        />
        <!-- 主脉搏面积(消息数/分钟) -->
        <polygon
          :points="msgArea"
          fill="url(#pulseMsgGradient)"
          stroke="none"
        />
        <!-- 面积顶边(描边,让轮廓清晰) -->
        <polyline
          :points="msgLine"
          fill="none"
          stroke="#1890ff"
          stroke-width="1.5"
          opacity="0.9"
          vector-effect="non-scaling-stroke"
        />
        <!-- 失败率虚线(不填充,保持对比) -->
        <polyline
          :points="failLine"
          fill="none"
          stroke="#ff4d4f"
          stroke-width="1"
          stroke-dasharray="2,2"
          opacity="0.7"
          vector-effect="non-scaling-stroke"
        />
        <!-- 当前点 — 描白边避免被面积遮 -->
        <circle
          v-if="lastPt"
          :cx="lastPt.x"
          :cy="lastPt.y"
          r="2.8"
          fill="#1890ff"
          stroke="#fff"
          stroke-width="1"
        />
        <!-- hover 竖辅助线 + 高亮圆点 -->
        <template v-if="hoverPt">
          <line
            :x1="hoverPt.x"
            y1="0"
            :x2="hoverPt.x"
            :y2="H"
            stroke="#8c8c8c"
            stroke-width="1"
            stroke-dasharray="2,2"
            vector-effect="non-scaling-stroke"
          />
          <circle :cx="hoverPt.x" :cy="hoverPt.y" r="3.2" fill="#1890ff" stroke="#fff" stroke-width="1.2" />
        </template>
      </svg>
      <div v-else class="pulse__empty">暂无信令</div>

      <!-- hover tooltip -->
      <div
        v-if="hoverIdx >= 0 && hoverInfo"
        class="pulse__tip"
        :style="tipStyle"
      >
        <div class="pulse__tip-time">{{ hoverInfo.time }}</div>
        <div class="pulse__tip-row">
          <i class="pulse__tip-dot pulse__tip-dot--blue" />
          消息 <b>{{ hoverInfo.msg }}</b> 条/分钟
        </div>
        <div class="pulse__tip-row">
          <i class="pulse__tip-dot pulse__tip-dot--red" />
          失败率 <b>{{ hoverInfo.failPct }}</b>
        </div>
      </div>
    </div>
    <div class="pulse__hint">采样 1m · 仅显示 GB28181 信令</div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import type { PulseSample, AbnormalWindow } from "@/api/gb28181";

interface Props {
  samples: PulseSample[];
  abnormalWindows: AbnormalWindow[];
}
const props = defineProps<Props>();

const W = 600;
const H = 60;

const svgEl = ref<SVGSVGElement | null>(null);
const hoverIdx = ref<number>(-1);
const hoverX = ref<number>(0);

const hasData = computed((): boolean =>
  props.samples.some((s) => s.msgPerSec > 0 || s.failPct > 0)
);

const maxMsg = computed((): number => {
  return Math.max(1, ...props.samples.map((s) => s.msgPerSec));
});

const currentMsg = computed((): number => {
  if (props.samples.length === 0) return 0;
  return props.samples[props.samples.length - 1].msgPerSec;
});

interface Pt {
  x: number;
  y: number;
}

const points = computed((): { msg: Pt[]; fail: Pt[] } => {
  const n = props.samples.length;
  if (n === 0) return { msg: [], fail: [] };
  const xStep = n > 1 ? W / (n - 1) : W;
  const msg: Pt[] = [];
  const fail: Pt[] = [];
  for (let i = 0; i < n; i++) {
    const s = props.samples[i];
    const x = i * xStep;
    msg.push({ x, y: H - (s.msgPerSec / maxMsg.value) * (H - 8) - 4 });
    const failNorm = Math.min(1, s.failPct / 200);
    fail.push({ x, y: H - failNorm * 8 - 2 });
  }
  return { msg, fail };
});

const msgLine = computed((): string =>
  points.value.msg.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")
);

// 面积多边形:折线 + 右下角 + 左下角,闭合包面积
const msgArea = computed((): string => {
  const m = points.value.msg;
  if (m.length === 0) return "";
  const line = m.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ");
  const right = m[m.length - 1].x.toFixed(1);
  const left = m[0].x.toFixed(1);
  return `${line} ${right},${H} ${left},${H}`;
});

const failLine = computed((): string =>
  points.value.fail.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")
);

const lastPt = computed((): Pt | null => {
  const m = points.value.msg;
  return m.length ? m[m.length - 1] : null;
});

const hoverPt = computed((): Pt | null => {
  if (hoverIdx.value < 0 || hoverIdx.value >= points.value.msg.length) return null;
  return points.value.msg[hoverIdx.value];
});

interface HoverInfo {
  time: string;
  msg: number;
  failPct: string;
}

const hoverInfo = computed((): HoverInfo | null => {
  if (hoverIdx.value < 0 || hoverIdx.value >= props.samples.length) return null;
  const s = props.samples[hoverIdx.value];
  const d = new Date(s.t * 1000);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  // failPct 是千分位整数,转成百分号显示
  const fpStr = s.failPct === 0 ? "0%" : `${(s.failPct / 10).toFixed(1)}%`;
  return {
    time: `${hh}:${mm}`,
    msg: s.msgPerSec,
    failPct: fpStr
  };
});

// tooltip 跟随鼠标,但靠近右边缘时翻到左侧避免溢出
const tipStyle = computed(() => {
  const ratio = hoverX.value / W;
  if (ratio > 0.7) {
    return { right: `${(1 - ratio) * 100}%`, transform: "translateX(8px)" };
  }
  return { left: `${ratio * 100}%`, transform: "translateX(8px)" };
});

function onMove(ev: MouseEvent): void {
  if (!hasData.value || !svgEl.value) return;
  const rect = svgEl.value.getBoundingClientRect();
  const relX = ev.clientX - rect.left;
  // 等比换算到 viewBox 坐标
  const vbX = (relX / rect.width) * W;
  // 找最近样本索引
  const n = props.samples.length;
  if (n === 0) return;
  const xStep = n > 1 ? W / (n - 1) : W;
  const idx = Math.round(vbX / xStep);
  hoverIdx.value = Math.max(0, Math.min(n - 1, idx));
  hoverX.value = vbX;
}

const abnormalRects = computed((): { x: number; w: number }[] => {
  if (props.samples.length === 0 || props.abnormalWindows.length === 0) return [];
  const first = props.samples[0].t;
  const last = props.samples[props.samples.length - 1].t;
  const span = Math.max(1, last - first);
  return props.abnormalWindows.map((w) => {
    const x = ((w.startT - first) / span) * W;
    const x2 = ((w.endT - first) / span) * W;
    return { x: Math.max(0, x), w: Math.max(1, x2 - x) };
  });
});
</script>

<style scoped lang="scss">
.pulse {
  background: #fafafa;
  border: 1px solid #e8e8e8;
  border-radius: 6px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.pulse__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pulse__title {
  font-size: 12px;
  color: #666;
  letter-spacing: 0.5px;
}

.pulse__stats {
  margin-left: 12px;
  color: #999;
  font-size: 11px;
  letter-spacing: 0;

  b {
    color: #1890ff;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    margin: 0 1px;
  }
}

.pulse__legend {
  display: flex;
  gap: 14px;
  font-size: 11px;
  color: #999;
}

.pulse__legend-line {
  display: inline-block;
  width: 10px;
  height: 2px;
  margin-right: 5px;
  vertical-align: middle;
  background: #1890ff;
}

.pulse__legend-line--red {
  background: #ff4d4f;
}

.pulse__chart {
  position: relative;
  width: 100%;
  height: 60px;
}

.pulse__svg {
  width: 100%;
  height: 60px;
  display: block;
}

.pulse__empty {
  height: 60px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #bfbfbf;
  font-size: 12px;
}

.pulse__tip {
  position: absolute;
  top: 0;
  pointer-events: none;
  background: rgba(0, 0, 0, 0.78);
  color: #fff;
  font-size: 11px;
  padding: 6px 8px;
  border-radius: 4px;
  white-space: nowrap;
  z-index: 10;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);

  b {
    color: #fff;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    margin: 0 2px;
  }
}

.pulse__tip-time {
  font-size: 10px;
  color: #d9d9d9;
  margin-bottom: 4px;
}

.pulse__tip-row {
  display: flex;
  align-items: center;
  gap: 4px;
  line-height: 1.5;
}

.pulse__tip-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #1890ff;
}

.pulse__tip-dot--red {
  background: #ff4d4f;
}

.pulse__hint {
  font-size: 10px;
  color: #bfbfbf;
  text-align: right;
}
</style>
