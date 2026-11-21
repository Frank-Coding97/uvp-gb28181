<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import { LoaderCircle } from "@lucide/vue";
import type { RecordQueryItem } from "../../gb28181/device-mgmt/api";
import {
  buildTimelineSegments,
  buildTimelineTicks,
  positionToTime,
  timeToPercent
} from "../../gb28181/device-record-playback/timeline";
import { formatClock, type DayRange } from "./urlState";

const props = defineProps<{
  range: DayRange;
  records: RecordQueryItem[];
  currentTime: string | null;
  selectedRecordKey: string | null;
  loading?: boolean;
  /** 展示的是上一次的结果（设备没答/超时），不可当成当天的结论。 */
  stale?: boolean;
}>();

const emit = defineEmits<{
  seek: [time: string];
  select: [recordKey: string];
}>();

const surface = ref<HTMLElement | null>(null);
const hoverPercent = ref<number | null>(null);
const dragging = ref(false);
const dragPercent = ref(0);

const segments = computed(() => buildTimelineSegments(props.range, props.records));
const laneCount = computed(() => Math.max(1, ...segments.value.map(segment => segment.lane + 1)));
const trackHeight = computed(() => 22 + (laneCount.value - 1) * 16);
/** 每 2 小时一条刻度，整点显示，6 小时的那几条加粗并带文字。 */
const ticks = computed(() => buildTimelineTicks(props.range).filter(tick => tick.major || tick.label));
const playheadPercent = computed(() => (props.currentTime ? timeToPercent(props.range, props.currentTime) : -1));
const hoverTime = computed(() =>
  hoverPercent.value === null || !surface.value
    ? null
    : positionToTime(props.range, (hoverPercent.value / 100) * surface.value.clientWidth, surface.value.clientWidth)
);

function percentAt(clientX: number) {
  const element = surface.value;
  if (!element) return 0;
  const rect = element.getBoundingClientRect();
  if (rect.width <= 0) return 0;
  return Math.min(100, Math.max(0, ((clientX - rect.left) / rect.width) * 100));
}

function timeAt(clientX: number) {
  const element = surface.value;
  if (!element) return props.range.startTime;
  return positionToTime(props.range, clientX - element.getBoundingClientRect().left, element.clientWidth);
}

function onTrackClick(event: MouseEvent) {
  if (dragging.value) return;
  emit("seek", timeAt(event.clientX));
}

function onTrackMove(event: MouseEvent) {
  hoverPercent.value = percentAt(event.clientX);
}

function onTrackLeave() {
  hoverPercent.value = null;
}

function onSegmentClick(recordKey: string, event: MouseEvent) {
  event.stopPropagation();
  emit("select", recordKey);
}

function startDrag(event: MouseEvent) {
  event.preventDefault();
  event.stopPropagation();
  dragging.value = true;
  dragPercent.value = playheadPercent.value >= 0 ? playheadPercent.value : percentAt(event.clientX);
  window.addEventListener("mousemove", onDrag);
  window.addEventListener("mouseup", endDrag);
}

function onDrag(event: MouseEvent) {
  dragPercent.value = percentAt(event.clientX);
}

function endDrag(event: MouseEvent) {
  window.removeEventListener("mousemove", onDrag);
  window.removeEventListener("mouseup", endDrag);
  dragging.value = false;
  const time = timeAt(event.clientX);
  emit("seek", time);
}

onUnmounted(() => {
  window.removeEventListener("mousemove", onDrag);
  window.removeEventListener("mouseup", endDrag);
});
</script>

<template>
  <section class="day-timeline" :class="{ 'is-stale': stale, 'is-loading': loading }" aria-label="当天录像时间轴">
    <header class="timeline-head">
      <span class="timeline-title">当天录像</span>
      <span v-if="loading" class="timeline-hint">
        <LoaderCircle :size="12" class="spin" aria-hidden="true" />正在向设备查询录像目录
      </span>
      <span v-else-if="stale" class="timeline-hint is-warning">查询未完成，下面是上一次的结果</span>
      <span v-else class="timeline-hint">{{ records.length }} 段</span>
      <span v-if="hoverTime" class="timeline-hover">{{ formatClock(hoverTime) }}</span>
      <span v-else-if="currentTime" class="timeline-now">{{ formatClock(currentTime) }}</span>
    </header>

    <div
      ref="surface"
      class="timeline-surface"
      :style="{ height: `${trackHeight}px` }"
      role="group"
      aria-label="点击定位播放位置"
      @click="onTrackClick"
      @mousemove="onTrackMove"
      @mouseleave="onTrackLeave"
    >
      <div class="timeline-track" aria-hidden="true">
        <span v-for="tick in ticks" :key="tick.time" class="timeline-tick" :style="{ left: `${tick.percent}%` }" />
      </div>

      <button
        v-for="segment in segments"
        :key="segment.key"
        type="button"
        class="timeline-segment"
        :class="[`type-${segment.type}`, { selected: segment.key === selectedRecordKey }]"
        :style="{
          left: `${segment.left}%`,
          width: `${Math.max(0.35, segment.width)}%`,
          top: `${segment.lane * 16 + 2}px`
        }"
        :title="`${formatClock(segment.trueStartTime)} - ${formatClock(segment.trueEndTime)}`"
        @click="onSegmentClick(segment.key, $event)"
      />

      <div v-if="dragging" class="timeline-playhead is-dragging" :style="{ left: `${dragPercent}%` }" aria-hidden="true">
        <span class="playhead-grip" />
      </div>
      <div
        v-else-if="playheadPercent >= 0"
        class="timeline-playhead"
        :style="{ left: `${playheadPercent}%` }"
        role="slider"
        tabindex="0"
        aria-label="播放位置"
        :aria-valuetext="formatClock(currentTime)"
        @mousedown="startDrag"
      >
        <span class="playhead-grip" />
      </div>

      <span v-if="hoverPercent !== null" class="timeline-cursor" :style="{ left: `${hoverPercent}%` }" aria-hidden="true" />
    </div>

    <footer class="timeline-scale" aria-hidden="true">
      <span>00:00</span>
      <span>06:00</span>
      <span>12:00</span>
      <span>18:00</span>
      <span>24:00</span>
    </footer>

    <footer class="timeline-legend">
      <span><i class="legend-dot type-time" />定时</span>
      <span><i class="legend-dot type-alarm" />报警</span>
      <span><i class="legend-dot type-manual" />手动</span>
    </footer>
  </section>
</template>

<style scoped>
.day-timeline {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 14px 10px;
  background: var(--uvp-card, transparent);
  border-top: 1px solid var(--uvp-panel-border);
}
.day-timeline.is-stale .timeline-surface {
  opacity: 0.5;
}
.timeline-head {
  display: flex;
  gap: 10px;
  align-items: center;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.timeline-title {
  font-weight: 500;
  color: var(--uvp-text-secondary);
}
.timeline-hint {
  display: inline-flex;
  gap: 5px;
  align-items: center;
}
.timeline-hint.is-warning {
  color: var(--uvp-warning);
}
.timeline-hover,
.timeline-now {
  margin-left: auto;
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--uvp-text-secondary);
}
.timeline-surface {
  position: relative;
  cursor: crosshair;
  user-select: none;
  background: color-mix(in srgb, var(--uvp-panel-border) 45%, transparent);
  border-radius: 5px;
}
.timeline-track {
  position: absolute;
  inset: 0;
  overflow: hidden;
  border-radius: inherit;
}
.timeline-tick {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: color-mix(in srgb, var(--uvp-panel-border) 90%, transparent);
}
.timeline-segment {
  position: absolute;
  height: 12px;
  padding: 0;
  cursor: pointer;
  border: 0;
  border-radius: 2px;
  transition: filter 0.15s ease;
}
.timeline-segment:hover {
  filter: brightness(1.12);
}
.timeline-segment.selected {
  outline: 1px solid var(--uvp-text-primary);
  outline-offset: 1px;
}
.timeline-segment.type-time {
  background: #378add;
}
.timeline-segment.type-alarm {
  background: #e24b4a;
}
.timeline-segment.type-manual {
  background: #ef9f27;
}
.timeline-segment.type-unknown {
  background: #b4b2a9;
}
.timeline-playhead {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  margin-left: -1px;
  cursor: ew-resize;
  background: var(--uvp-text-primary);
}
.timeline-playhead:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 2px;
}
.playhead-grip {
  position: absolute;
  top: -3px;
  left: -4px;
  width: 10px;
  height: 8px;
  background: var(--uvp-text-primary);
  border-radius: 2px;
}
.timeline-cursor {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  pointer-events: none;
  background: color-mix(in srgb, var(--uvp-text-primary) 35%, transparent);
}
.timeline-scale {
  display: flex;
  justify-content: space-between;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.timeline-legend {
  display: flex;
  gap: 14px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.timeline-legend span {
  display: inline-flex;
  gap: 5px;
  align-items: center;
}
.legend-dot {
  display: inline-block;
  width: 9px;
  height: 9px;
  border-radius: 2px;
}
.legend-dot.type-time {
  background: #378add;
}
.legend-dot.type-alarm {
  background: #e24b4a;
}
.legend-dot.type-manual {
  background: #ef9f27;
}
.spin {
  animation: day-timeline-spin 0.9s linear infinite;
}

@keyframes day-timeline-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
