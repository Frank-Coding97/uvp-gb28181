<script setup lang="ts">
/**
 * 地图上的轨迹控制条。
 *
 * 只负责"展示 + 抛事件"，不碰地图实例 —— 折线、起止点、回放标记都在 index.vue
 * 里画（它们必须和 markera 的生命周期绑在一起）。这样这个组件能独立单测，
 * 也避免把 maplibre 的实例状态塞进 props。
 */
import { computed, ref, watch } from "vue";
import dayjs from "dayjs";
import { Loader2, Pause, Play, Route, X } from "@lucide/vue";
import { formatDistance, type TrackPoint } from "./mapTrajectory";

interface Props {
  visible: boolean;
  /** 通道名（标题）。 */
  channelName: string;
  loading?: boolean;
  points: TrackPoint[];
  distanceMeters: number;
  /** 点数超出上限被抽稀。必须在界面上说明，否则用户会以为设备漏报了。 */
  truncated?: boolean;
  /** 空轨迹时的说明文案（三种原因由 mapTrajectory.describeEmptyTrajectory 给出）。 */
  emptyText?: string;
  playing?: boolean;
  /** 回放进度 0~1。 */
  progress: number;
  speed: number;
  /** 回放当前位置的时间文案（空表示未开始回放）。 */
  currentTime?: string;
  canPlay?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
  truncated: false,
  emptyText: "",
  playing: false,
  currentTime: "",
  canPlay: true
});

const emit = defineEmits<{
  (e: "close"): void;
  (e: "search", range: { startTime: string; endTime: string }): void;
  (e: "toggle"): void;
  (e: "update:progress", value: number): void;
  (e: "update:speed", value: number): void;
}>();

const TIME_FORMAT = "YYYY-MM-DD HH:mm:ss";

// 默认查最近 24 小时，与后端 trajectoryDefaultWindow 一致。
// ⛔ 别改成 1 小时：执法仪的值班周期是一整天，默认 1 小时会让早上打开的人
//    看到一条几乎空的轨迹（2026-09-21 设计评审已定 Q2 = 24 小时）。
const range = ref<[string, string]>([dayjs().subtract(24, "hour").format(TIME_FORMAT), dayjs().format(TIME_FORMAT)]);

// 每次重新打开面板把时间窗刷新到"现在"——复用上一次的时间范围会让用户
// 第二天打开还在查昨天的空区间，看起来像功能坏了。
watch(
  () => props.visible,
  visible => {
    if (visible) {
      range.value = [dayjs().subtract(24, "hour").format(TIME_FORMAT), dayjs().format(TIME_FORMAT)];
    }
  }
);

const canSearch = computed(() => !!range.value?.[0] && !!range.value?.[1]);

const pointCount = computed(() => props.points.length);

const distanceText = computed(() => formatDistance(props.distanceMeters));

const progressPercent = computed(() => Math.round(Math.min(1, Math.max(0, props.progress)) * 100));

function onSearch() {
  if (!canSearch.value) return;
  emit("search", { startTime: range.value[0], endTime: range.value[1] });
}

// ⛔ 不用 `v-model="range as any"`：v-model 需要一个**可赋值**的表达式，
//    带 `as` 的表达式不是合法左值，编译期能过、运行时赋值会失败（时间范围永远改不动）。
//    这里改成显式 model-value + change，两条路径都只写 ref。
function onRangeChange(value: unknown) {
  if (!Array.isArray(value) || value.length !== 2) return;
  const [start, end] = value;
  if (!start || !end) return;
  range.value = [String(start), String(end)];
}

function onProgressChange(value: number | number[]) {
  const next = Array.isArray(value) ? value[0] : value;
  emit("update:progress", Math.min(1, Math.max(0, next / 100)));
}

function onSpeedChange(value: unknown) {
  emit("update:speed", Number(value));
}

// 档位按 2026-09-21 设计评审定稿（Q3：1× / 4× / 16×）——
// 16× 是给"抽稀后仍有上千点"的长轨迹快速扫一遍用的。
const speedOptions = [
  { label: "1×", value: 1 },
  { label: "4×", value: 4 },
  { label: "16×", value: 16 }
];
</script>

<template>
  <div v-if="visible" class="trajectory-panel">
    <header class="trajectory-panel__head">
      <span class="trajectory-panel__title">
        <Route :size="14" />
        <span class="trajectory-panel__name">{{ channelName || "通道轨迹" }}</span>
      </span>
      <button class="trajectory-panel__close" type="button" aria-label="关闭轨迹" @click="emit('close')">
        <X :size="14" />
      </button>
    </header>

    <div class="trajectory-panel__range">
      <a-range-picker
        :model-value="range"
        size="small"
        show-time
        :time-picker-props="{ format: 'HH:mm:ss' }"
        format="MM-DD HH:mm:ss"
        value-format="YYYY-MM-DD HH:mm:ss"
        :allow-clear="false"
        style="width: 100%"
        @change="onRangeChange"
      />
      <a-button class="uvp-btn-primary" size="mini" type="primary" :loading="loading" :disabled="!canSearch" @click="onSearch">
        查询
      </a-button>
    </div>

    <div v-if="loading" class="trajectory-panel__state"><Loader2 :size="14" class="spin" /> 正在查询轨迹</div>

    <div v-else-if="!pointCount" class="trajectory-panel__state trajectory-panel__state--empty">
      {{ emptyText || "该时间段内没有轨迹" }}
    </div>

    <template v-else>
      <div class="trajectory-panel__stats">
        <span
          ><b>{{ pointCount }}</b> 个点</span
        >
        <span
          >里程 <b>{{ distanceText }}</b></span
        >
        <span v-if="truncated" class="trajectory-panel__truncated" title="点位过多，只回传了抽稀后的路线"> 已抽稀 </span>
      </div>

      <div class="trajectory-panel__playback">
        <button
          class="trajectory-panel__play"
          type="button"
          :disabled="!canPlay || pointCount < 2"
          :aria-label="playing ? '暂停回放' : '开始回放'"
          @click="emit('toggle')"
        >
          <Pause v-if="playing" :size="14" />
          <Play v-else :size="14" />
        </button>
        <a-slider
          class="trajectory-panel__slider"
          :model-value="progressPercent"
          :show-tooltip="false"
          @change="onProgressChange"
        />
        <a-select
          class="trajectory-panel__speed"
          size="mini"
          :model-value="speed"
          :options="speedOptions"
          @change="onSpeedChange"
        />
      </div>

      <div class="trajectory-panel__meta">
        <span v-if="currentTime">回放到 {{ currentTime }}</span>
        <span v-else>共 {{ pointCount }} 个位置点</span>
        <span class="trajectory-panel__hint">拖动滑块可回看任意时刻</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.trajectory-panel {
  position: absolute;
  bottom: 16px;
  left: 16px;
  z-index: 6;
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 340px;
  padding: 12px;
  background: #ffffff;
  border-radius: 10px;
  box-shadow: 0 8px 24px rgb(15 23 42 / 16%);
}

.trajectory-panel__head {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}

.trajectory-panel__title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  color: #1d2129;
}

.trajectory-panel__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.trajectory-panel__close {
  display: inline-flex;
  padding: 2px;
  color: #86909c;
  cursor: pointer;
  background: transparent;
  border: none;
}

.trajectory-panel__close:hover {
  color: #1d2129;
}

.trajectory-panel__range {
  display: flex;
  gap: 8px;
  align-items: center;
}

.trajectory-panel__state {
  display: flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  line-height: 1.5;
  color: #4e5969;
}

.trajectory-panel__state--empty {
  color: #86909c;
}

.trajectory-panel__stats {
  display: flex;
  gap: 12px;
  align-items: center;
  font-size: 12px;
  color: #4e5969;
}

.trajectory-panel__stats b {
  font-weight: 600;
  color: #1d2129;
}

.trajectory-panel__truncated {
  padding: 0 6px;
  font-size: 11px;
  color: #d25f00;
  background: #fff7e8;
  border-radius: 8px;
}

.trajectory-panel__playback {
  display: flex;
  gap: 8px;
  align-items: center;
}

.trajectory-panel__play {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  color: #ffffff;
  cursor: pointer;
  background: var(--uvp-solid-bg, #2563eb);
  border: none;
  border-radius: 50%;
}

.trajectory-panel__play:disabled {
  cursor: not-allowed;
  background: #c9cdd4;
}

.trajectory-panel__slider {
  flex: 1 1 auto;
  min-width: 0;
}

.trajectory-panel__speed {
  flex: 0 0 76px;
}

.trajectory-panel__meta {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: #86909c;
}

.trajectory-panel__hint {
  white-space: nowrap;
}

/* 深色底图下这块浮层也跟着走暗色，否则白卡片在夜空底图上刺眼。
   ⛔ 必须把整条选择器包在 :global() 里：写成 `:global(.map-dark) .x` 只会把
      前半段变成全局，后半段仍带 scope 属性，父级 .map-dark 在组件外，匹配不上。 */
:global(.map-dark .trajectory-panel) {
  background: #1f2733;
  box-shadow: 0 8px 24px rgb(0 0 0 / 45%);
}

:global(.map-dark .trajectory-panel__title),
:global(.map-dark .trajectory-panel__stats b) {
  color: #e5e6eb;
}

:global(.map-dark .trajectory-panel__state),
:global(.map-dark .trajectory-panel__stats) {
  color: #c9cdd4;
}
</style>
