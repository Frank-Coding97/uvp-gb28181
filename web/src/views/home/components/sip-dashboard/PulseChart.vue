<template>
  <div class="pulse">
    <div class="pulse__head">
      <div class="pulse__title">信令脉搏 · 最近 60 分钟</div>
      <div class="pulse__legend">
        <span><i class="pulse__legend-line pulse__legend-line--blue" />消息速率</span>
        <span><i class="pulse__legend-line pulse__legend-line--red" />失败率</span>
      </div>
    </div>
    <svg
      v-if="hasData"
      class="pulse__svg"
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="none"
    >
      <!-- 异常时间窗背景 -->
      <rect
        v-for="(win, i) in abnormalRects"
        :key="i"
        :x="win.x"
        y="0"
        :width="win.w"
        :height="H"
        fill="rgba(255, 90, 95, 0.08)"
      />
      <!-- 主脉搏曲线(msg/s) -->
      <polyline
        :points="msgLine"
        fill="none"
        stroke="#4d8eff"
        stroke-width="1.5"
        opacity="0.9"
      />
      <!-- 失败率虚线 -->
      <polyline
        :points="failLine"
        fill="none"
        stroke="#ff5a5f"
        stroke-width="1"
        stroke-dasharray="2,2"
        opacity="0.7"
      />
      <!-- 当前点 -->
      <circle v-if="lastPt" :cx="lastPt.x" :cy="lastPt.y" r="2.5" fill="#4d8eff" />
    </svg>
    <div v-else class="pulse__empty">暂无信令</div>
    <div class="pulse__hint">采样 1m · 仅显示 GB28181 信令</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import type { PulseSample, AbnormalWindow } from "@/api/gb28181";

interface Props {
  samples: PulseSample[];
  abnormalWindows: AbnormalWindow[];
}
const props = defineProps<Props>();

const W = 600;
const H = 60;

const hasData = computed((): boolean =>
  props.samples.some((s) => s.msgPerSec > 0 || s.failPct > 0)
);

const maxMsg = computed((): number => {
  const m = Math.max(1, ...props.samples.map((s) => s.msgPerSec));
  return m;
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
    // failPct 千分位 → 0-1000,把 0-200‰ (20%) 映射到底部 8px
    const failNorm = Math.min(1, s.failPct / 200);
    fail.push({ x, y: H - failNorm * 8 - 2 });
  }
  return { msg, fail };
});

const msgLine = computed((): string =>
  points.value.msg.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")
);

const failLine = computed((): string =>
  points.value.fail.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")
);

const lastPt = computed((): Pt | null => {
  const m = points.value.msg;
  return m.length ? m[m.length - 1] : null;
});

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
  background: #1a2235;
  border: 1px solid #1f2a40;
  border-radius: 8px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  color: #e6edf7;
}

.pulse__head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pulse__title {
  font-size: 11px;
  color: #8b97ad;
  letter-spacing: 0.5px;
}

.pulse__legend {
  display: flex;
  gap: 12px;
  font-size: 10px;
  color: #5a6478;
}

.pulse__legend-line {
  display: inline-block;
  width: 8px;
  height: 2px;
  margin-right: 4px;
  vertical-align: middle;
  background: #4d8eff;
}

.pulse__legend-line--red {
  background: #ff5a5f;
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
  color: #5a6478;
  font-size: 12px;
}

.pulse__hint {
  font-size: 10px;
  color: #5a6478;
  text-align: right;
}
</style>
