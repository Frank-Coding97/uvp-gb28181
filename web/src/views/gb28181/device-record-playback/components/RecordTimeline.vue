<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { LocateFixed, Minus, Plus } from "@lucide/vue";
import {
    buildTimelineSegments,
    buildTimelineTicks,
    centerTimelineViewport,
    panTimelineViewport,
    positionToTime,
    timelineZoomLevels,
    timeToPercent,
    zoomTimelineViewport,
    type TimelineRange,
    type TimelineRecord,
    type TimelineViewport
} from "../timeline";

export interface TimelineLocateEvent {
    time: string;
    recordKey: string | null;
}

const props = defineProps<{
    range: TimelineRange;
    records: TimelineRecord[];
    selectedRecordKey: string | null;
    currentTime: string | null;
}>();

const emit = defineEmits<{
    select: [recordKey: string];
    locate: [event: TimelineLocateEvent];
}>();

const defaultZoom = 4;
const surface = ref<HTMLElement | null>(null);
const viewport = ref<TimelineViewport>(centerTimelineViewport(props.range, defaultZoom, preferredFocusTime()));
const locatedInGap = ref(false);
const dragging = ref(false);
let dragStartX = 0;
let dragMoved = false;
let dragStartedOnSegment = false;
let dragStartViewport: TimelineViewport | null = null;

const ticks = computed(() => {
    const start = Date.parse(props.range.startTime);
    const end = Date.parse(props.range.endTime);
    return buildTimelineTicks(viewport.value).filter(tick => {
        const value = Date.parse(tick.time);
        return value >= start && value <= end;
    });
});
const segments = computed(() => buildTimelineSegments(viewport.value, props.records));
const laneCount = computed(() => Math.max(1, ...segments.value.map(segment => segment.lane + 1)));
const surfaceHeight = computed(() => 70 + laneCount.value * 28);
const playheadTime = computed(() => {
    const start = Date.parse(viewport.value.startTime);
    const end = Date.parse(viewport.value.endTime);
    return Number.isFinite(start) && Number.isFinite(end) && end > start
        ? positionToTime(viewport.value, 0.5, 1)
        : props.range.startTime;
});
const playheadPercent = computed(() => playheadTime.value ? timeToPercent(props.range, playheadTime.value) : 0);
const queryStartPercent = computed(() => timeToPercent(viewport.value, props.range.startTime));
const queryEndPercent = computed(() => timeToPercent(viewport.value, props.range.endTime));
const playheadInGap = computed(() => locatedInGap.value || !recordAt(playheadTime.value));
const selectedRecordRangeText = computed(() => {
    const record = selectedRecord();
    return record?.startTime && record.endTime
        ? `${formatDateTime(record.startTime)} - ${formatDateTime(record.endTime)}`
        : "未选择";
});

watch(() => [props.range.startTime, props.range.endTime], () => {
    viewport.value = centerTimelineViewport(props.range, defaultZoom, preferredFocusTime());
    locatedInGap.value = false;
});

watch(() => props.selectedRecordKey, () => {
    if (props.currentTime) return;
    const record = selectedRecord();
    if (record?.startTime) viewport.value = centerTimelineViewport(props.range, viewport.value.zoom || defaultZoom, record.startTime);
    locatedInGap.value = false;
});

watch(() => props.currentTime, value => {
    if (!value || dragging.value) return;
    viewport.value = centerTimelineViewport(props.range, viewport.value.zoom || defaultZoom, value);
    locatedInGap.value = false;
});

function formatDateTime(value: string) {
    return value.replace("T", " ").replace(/\.\d{3}/, "").replace(/([+-]\d{2}:\d{2}|Z)$/, "");
}

function formatPointerTime(value: string) {
    return value ? formatDateTime(value).slice(11, 19) : "--:--:--";
}

function selectedRecord() {
    return props.records.find(item => item.recordKey === props.selectedRecordKey) || null;
}

function preferredFocusTime() {
    if (props.currentTime) return props.currentTime;
    const selected = selectedRecord();
    if (selected?.startTime) return selected.startTime;
    const start = Date.parse(props.range.startTime);
    const end = Date.parse(props.range.endTime);
    return Number.isFinite(start) && Number.isFinite(end)
        ? new Date((start + end) / 2).toISOString()
        : props.range.startTime;
}

function recordAt(time: string) {
    const value = Date.parse(time);
    return props.records.find(record => {
        const start = Date.parse(record.startTime || "");
        const end = Date.parse(record.endTime || "");
        return Number.isFinite(start) && Number.isFinite(end) && value >= start && value <= end;
    }) || null;
}

function nextZoom(direction: 1 | -1) {
    const currentIndex = timelineZoomLevels.findIndex(level => level === viewport.value.zoom);
    const index = Math.min(timelineZoomLevels.length - 1, Math.max(0, currentIndex + direction));
    return timelineZoomLevels[index];
}

function applyZoom(zoom: number, anchorTime?: string) {
    viewport.value = zoomTimelineViewport(props.range, viewport.value, zoom, anchorTime || playheadTime.value);
}

function locateCurrent() {
    viewport.value = centerTimelineViewport(props.range, viewport.value.zoom || defaultZoom, preferredFocusTime());
    locatedInGap.value = false;
}

function pointerTime(clientX: number) {
    const rect = surface.value?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return null;
    return positionToTime(viewport.value, clientX - rect.left, rect.width);
}

function locateTime(time: string, preferredRecordKey?: string) {
    const record = preferredRecordKey
        ? props.records.find(item => item.recordKey === preferredRecordKey) || null
        : recordAt(time);
    locatedInGap.value = !record;
    if (record) emit("select", record.recordKey);
    emit("locate", { time, recordKey: record?.recordKey || null });
}

function centerAndLocate(time: string, preferredRecordKey?: string) {
    viewport.value = centerTimelineViewport(props.range, viewport.value.zoom || defaultZoom, time);
    locateTime(time, preferredRecordKey);
}

function onSegmentClick(recordKey: string, event: MouseEvent) {
    if (dragMoved) return;
    const time = pointerTime(event.clientX);
    const record = props.records.find(item => item.recordKey === recordKey);
    if (!record) return;
    const value = Date.parse(time || "");
    const start = Date.parse(record.startTime || "");
    const end = Date.parse(record.endTime || "");
    const located = Number.isFinite(value) && value >= start && value <= end
        ? time!
        : positionToTime({ startTime: record.startTime!, endTime: record.endTime! }, 0.5, 1);
    centerAndLocate(located, recordKey);
}

function startTimelineDrag(event: PointerEvent, fromSegment = false) {
    if (event.button !== 0) return;
    dragStartX = event.clientX;
    dragMoved = false;
    dragStartedOnSegment = fromSegment;
    dragStartViewport = { ...viewport.value };
    dragging.value = true;
    window.addEventListener("pointermove", onWindowPointerMove);
    window.addEventListener("pointerup", onWindowPointerUp, { once: true });
}

function onSurfacePointerDown(event: PointerEvent) {
    startTimelineDrag(event);
}

function onSegmentPointerDown(event: PointerEvent) {
    startTimelineDrag(event, true);
}

function onWindowPointerMove(event: PointerEvent) {
    if (!dragging.value || !surface.value || !dragStartViewport) return;
    const width = surface.value.getBoundingClientRect().width;
    if (width <= 0) return;
    const deltaX = event.clientX - dragStartX;
    if (Math.abs(deltaX) > 3) dragMoved = true;
    const duration = Date.parse(dragStartViewport.endTime) - Date.parse(dragStartViewport.startTime);
    viewport.value = panTimelineViewport(props.range, dragStartViewport, -(deltaX / width) * duration);
}

function onWindowPointerUp(event: PointerEvent) {
    window.removeEventListener("pointermove", onWindowPointerMove);
    if (dragMoved) locateTime(playheadTime.value);
    else if (!dragStartedOnSegment) {
        const time = pointerTime(event.clientX);
        if (time) centerAndLocate(time);
    }
    dragging.value = false;
    dragStartedOnSegment = false;
    dragStartViewport = null;
}

function onWheel(event: WheelEvent) {
    if (!surface.value) return;
    if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
        const duration = Date.parse(viewport.value.endTime) - Date.parse(viewport.value.startTime);
        viewport.value = panTimelineViewport(props.range, viewport.value, (event.deltaX / surface.value.getBoundingClientRect().width) * duration);
        return;
    }
    applyZoom(nextZoom(event.deltaY < 0 ? 1 : -1), playheadTime.value);
}

function onKeyboard(event: KeyboardEvent) {
    if (!["ArrowLeft", "ArrowRight"].includes(event.key)) return;
    event.preventDefault();
    const duration = Date.parse(viewport.value.endTime) - Date.parse(viewport.value.startTime);
    viewport.value = panTimelineViewport(props.range, viewport.value, duration * (event.key === "ArrowLeft" ? -0.1 : 0.1));
    locateTime(playheadTime.value);
}

onUnmounted(() => {
    window.removeEventListener("pointermove", onWindowPointerMove);
    window.removeEventListener("pointerup", onWindowPointerUp);
});
</script>

<template>
    <section class="record-timeline" data-testid="record-timeline">
        <header class="timeline-header">
            <div class="timeline-title">
                <strong>录像时间轴</strong>
                <span data-testid="timeline-range">当前录像 · {{ selectedRecordRangeText }}</span>
            </div>
            <div class="timeline-actions">
                <div class="timeline-legend" aria-label="录像类型图例">
                    <span class="time">定时</span><span class="alarm">报警</span><span class="manual">手动</span><span class="unknown">其他</span>
                </div>
                <div class="zoom-controls" aria-label="时间轴缩放">
                    <button type="button" data-testid="timeline-zoom-out" aria-label="缩小时间轴" title="缩小" :disabled="viewport.zoom === 1" @click="applyZoom(nextZoom(-1))"><Minus :size="14" /></button>
                    <output data-testid="timeline-zoom-value" aria-label="当前时间轴倍率">{{ viewport.zoom }}x</output>
                    <button type="button" data-testid="timeline-zoom-in" aria-label="放大时间轴" title="放大" :disabled="viewport.zoom === 32" @click="applyZoom(nextZoom(1))"><Plus :size="14" /></button>
                    <button type="button" class="locate-button" aria-label="定位当前播放时间" title="定位当前播放时间" :disabled="!selectedRecordKey && !currentTime" @click="locateCurrent"><LocateFixed :size="14" /></button>
                </div>
            </div>
        </header>

        <div
            ref="surface"
            data-testid="timeline-surface"
            :class="['timeline-surface', { dragging }]"
            :style="{ height: `${surfaceHeight}px` }"
            role="group"
            aria-label="录像时间轴"
            tabindex="0"
            @pointerdown="onSurfacePointerDown"
            @wheel.prevent="onWheel"
            @keydown="onKeyboard"
        >
            <div class="tick-layer" aria-hidden="true">
                <span
                    v-for="tick in ticks"
                    :key="tick.time"
                    :class="['timeline-tick', { major: tick.major }]"
                    :style="{ left: `${tick.percent}%` }"
                ><i></i><b v-if="tick.major">{{ tick.label }}</b></span>
            </div>
            <div class="record-lane" aria-hidden="true"></div>
            <div v-if="queryStartPercent > 0" class="range-gutter left" :style="{ width: `${queryStartPercent}%` }" aria-hidden="true"></div>
            <div v-if="queryEndPercent < 100" class="range-gutter right" :style="{ width: `${100 - queryEndPercent}%` }" aria-hidden="true"></div>
            <button
                v-for="segment in segments"
                :key="segment.key"
                type="button"
                :data-testid="`timeline-segment-${segment.key}`"
                :class="['timeline-segment', segment.type, { selected: selectedRecordKey === segment.key }]"
                :style="{ left: `${segment.left}%`, width: `${Math.max(segment.width, 0.55)}%`, top: `${47 + segment.lane * 28}px` }"
                :title="`${formatPointerTime(segment.trueStartTime)} - ${formatPointerTime(segment.trueEndTime)}`"
                @pointerdown.stop="onSegmentPointerDown"
                @click.stop="onSegmentClick(segment.key, $event)"
            >
                <span v-if="segment.width >= 8">{{ formatPointerTime(segment.trueStartTime) }} - {{ formatPointerTime(segment.trueEndTime) }}</span>
            </button>
            <div
                data-testid="timeline-playhead"
                :class="['timeline-playhead', { gap: playheadInGap }]"
                role="slider"
                aria-label="播放头"
                tabindex="-1"
                :aria-valuenow="Math.round(playheadPercent)"
                :aria-valuetext="formatPointerTime(playheadTime)"
            >
                <span>{{ formatPointerTime(playheadTime) }}<b v-if="playheadInGap">无录像</b></span><i></i>
            </div>
        </div>
    </section>
</template>

<style scoped>
.record-timeline { padding: 10px 12px 12px; background: var(--uvp-panel-bg); border: 1px solid var(--uvp-panel-border); }
.timeline-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 32px; margin-bottom: 8px; }
.timeline-title { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
.timeline-title strong { flex: none; font-size: 12px; }
.timeline-title span { overflow: hidden; color: var(--uvp-text-tertiary); font: 10px ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.timeline-actions { display: flex; align-items: center; gap: 14px; }
.timeline-legend { display: flex; gap: 9px; }
.timeline-legend span { color: var(--uvp-text-tertiary); font-size: 10px; white-space: nowrap; }
.timeline-legend span::before { display: inline-block; width: 8px; height: 3px; margin-right: 4px; vertical-align: middle; background: #708090; border-radius: 2px; content: ""; }
.timeline-legend .time::before { background: var(--uvp-brand); }.timeline-legend .alarm::before { background: var(--uvp-danger); }.timeline-legend .manual::before { background: var(--uvp-warning); }
.zoom-controls { display: grid; grid-template-columns: 28px 38px 28px 28px; align-items: center; height: 28px; overflow: hidden; background: var(--uvp-search-control-bg); border: 1px solid var(--uvp-search-secondary-btn-border); border-radius: 6px; }
.zoom-controls button { display: inline-flex; align-items: center; justify-content: center; width: 28px; height: 28px; padding: 0; color: var(--uvp-text-secondary); background: transparent; border: 0; border-right: 1px solid var(--uvp-search-secondary-btn-border); cursor: pointer; }
.zoom-controls .locate-button { border-right: 0; border-left: 1px solid var(--uvp-search-secondary-btn-border); }
.zoom-controls button:hover:not(:disabled) { color: var(--uvp-brand); background: var(--uvp-brand-soft); }
.zoom-controls button:disabled { cursor: not-allowed; opacity: .38; }
.zoom-controls output { color: var(--uvp-text-primary); font: 10px ui-monospace, SFMono-Regular, Menlo, monospace; text-align: center; }
.timeline-surface { position: relative; overflow: hidden; background: var(--uvp-search-control-bg); border: 1px solid var(--uvp-panel-border); border-radius: 4px; outline: none; cursor: grab; touch-action: none; user-select: none; }
.timeline-surface.dragging { cursor: grabbing; }
.timeline-surface:focus-visible { box-shadow: 0 0 0 2px var(--uvp-brand-soft), 0 0 0 3px var(--uvp-brand); }
.tick-layer { position: absolute; inset: 0; pointer-events: none; }
.timeline-tick { position: absolute; top: 0; bottom: 0; width: 1px; color: var(--uvp-text-tertiary); background: color-mix(in srgb, var(--uvp-panel-border) 62%, transparent); }
.timeline-tick i { position: absolute; top: 0; left: 0; width: 1px; height: 6px; background: var(--uvp-text-tertiary); opacity: .54; }
.timeline-tick.major { background: var(--uvp-panel-border); }
.timeline-tick.major i { height: 10px; opacity: .86; }
.timeline-tick b { position: absolute; top: 11px; left: 4px; color: var(--uvp-text-secondary); font: 9px ui-monospace, SFMono-Regular, Menlo, monospace; font-weight: 500; white-space: nowrap; }
.record-lane { position: absolute; top: 36px; right: 0; bottom: 0; left: 0; background: color-mix(in srgb, var(--uvp-list-toolbar-bg) 72%, transparent); border-top: 1px solid var(--uvp-panel-border); }
.range-gutter { position: absolute; z-index: 1; top: 36px; bottom: 0; background: color-mix(in srgb, var(--uvp-shell-muted) 86%, transparent); background-image: repeating-linear-gradient(135deg, transparent 0 7px, color-mix(in srgb, var(--uvp-panel-border) 36%, transparent) 7px 8px); pointer-events: none; }.range-gutter.left { left: 0; border-right: 1px dashed var(--uvp-panel-border); }.range-gutter.right { right: 0; border-left: 1px dashed var(--uvp-panel-border); }
.timeline-segment { position: absolute; z-index: 3; display: flex; align-items: center; min-width: 6px; height: 22px; padding: 0 7px; overflow: hidden; color: #fff; font: 9px ui-monospace, SFMono-Regular, Menlo, monospace; text-align: left; text-shadow: 0 1px 2px rgb(0 0 0 / 38%); white-space: nowrap; background: #708090; border: 1px solid rgb(255 255 255 / 48%); border-radius: 3px; cursor: pointer; transition: filter 160ms ease, box-shadow 160ms ease; }
.timeline-segment:hover { filter: brightness(1.12); }.timeline-segment.time { background: var(--uvp-brand); }.timeline-segment.alarm { background: var(--uvp-danger); }.timeline-segment.manual { background: var(--uvp-warning); }
.timeline-segment.selected { z-index: 4; box-shadow: 0 0 0 2px var(--uvp-panel-bg), 0 0 0 3px var(--uvp-text-primary); }
.timeline-playhead { position: absolute; z-index: 7; top: 0; bottom: 0; left: 50%; width: 2px; background: var(--uvp-brand); box-shadow: 0 0 0 1px color-mix(in srgb, var(--uvp-panel-bg) 68%, transparent); cursor: ew-resize; pointer-events: auto; }
.timeline-playhead > span { position: absolute; top: 4px; left: 50%; min-width: 72px; padding: 4px 8px; color: #fff; font: 10px ui-monospace, SFMono-Regular, Menlo, monospace; text-align: center; white-space: nowrap; background: var(--uvp-brand); border-radius: 4px; box-shadow: 0 2px 7px rgb(0 0 0 / 18%); transform: translateX(-50%); }
.timeline-playhead > span::after { position: absolute; bottom: -5px; left: 50%; width: 9px; height: 6px; background: inherit; clip-path: polygon(0 0, 100% 0, 50% 100%); content: ""; transform: translateX(-50%); }
.timeline-playhead > span b { margin-left: 5px; color: #fff0ca; font-weight: 500; }.timeline-playhead > i { position: absolute; right: -4px; bottom: 0; width: 10px; height: 4px; background: var(--uvp-brand); border-radius: 4px 4px 0 0; }
.timeline-playhead.gap { background: var(--uvp-warning); }.timeline-playhead.gap > span, .timeline-playhead.gap > i { background: var(--uvp-warning); }
@media (max-width: 768px) {
    .timeline-header { align-items: flex-start; flex-direction: column; gap: 7px; }.timeline-actions { justify-content: space-between; width: 100%; }.timeline-legend { display: none; }.zoom-controls { margin-left: auto; }.zoom-controls button { width: 32px; height: 32px; }.zoom-controls { grid-template-columns: 32px 42px 32px 32px; height: 32px; }
}
@media (prefers-reduced-motion: reduce) { .timeline-segment { transition-duration: .01ms; } }
</style>
