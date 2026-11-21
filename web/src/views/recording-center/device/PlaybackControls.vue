<script setup lang="ts">
import { computed } from "vue";
import { CircleStop, Download, LoaderCircle, Maximize2, Pause, Play, SkipBack, SkipForward } from "@lucide/vue";
import type { RecordQueryItem } from "../../gb28181/device-mgmt/api";
import type { PlaybackState } from "../../gb28181/device-record-playback/playbackState";
import { durationText, formatClock } from "./urlState";

const props = defineProps<{
  playback: PlaybackState;
  selectedRecord: RecordQueryItem | null;
  canPlay: boolean;
  canDownload: boolean;
  controlPending?: boolean;
  downloadPending?: boolean;
  /** 列表里是否还有上一段 / 下一段，用来禁用两端的按钮。 */
  hasPrev: boolean;
  hasNext: boolean;
}>();

const emit = defineEmits<{
  toggle: [];
  stop: [];
  prev: [];
  next: [];
  scale: [value: number];
  download: [];
  fullscreen: [];
}>();

const scaleOptions = [0.25, 0.5, 1, 2, 4];

const isPlaying = computed(() => props.playback.status === "playing");
const isLoading = computed(() => ["creating", "buffering", "stopping"].includes(props.playback.status));
const isBusy = computed(() => Boolean(props.controlPending) || isLoading.value);
const playLabel = computed(() => {
  if (isLoading.value) return "加载中";
  return isPlaying.value ? "暂停" : "播放";
});

const scaleValue = computed<number>({
  get: () => props.playback.scale || 1,
  set: value => emit("scale", Number(value))
});

/** 当前段总长；没选段时显示 `--`，不要显示 00:00:00 让人以为是一段空录像。 */
const totalText = computed(() =>
  props.selectedRecord ? durationText(props.selectedRecord.startTime, props.selectedRecord.endTime) : "--:--:--"
);
</script>

<template>
  <div class="playback-controls" data-testid="playback-controls">
    <a-button
      data-testid="playback-prev"
      type="secondary"
      :disabled="!canPlay || !hasPrev || isBusy"
      aria-label="上一段"
      title="上一段"
      @click="emit('prev')"
    >
      <template #icon><SkipBack :size="15" /></template>
    </a-button>

    <a-button
      data-testid="playback-toggle"
      type="primary"
      class="control-toggle"
      :disabled="!canPlay || !selectedRecord || (isBusy && !isLoading)"
      @click="emit('toggle')"
    >
      <template #icon>
        <LoaderCircle v-if="isLoading" :size="15" class="spin" />
        <Pause v-else-if="isPlaying" :size="15" fill="currentColor" />
        <Play v-else :size="15" fill="currentColor" />
      </template>
      {{ playLabel }}
    </a-button>

    <a-button
      data-testid="playback-next"
      type="secondary"
      :disabled="!canPlay || !hasNext || isBusy"
      aria-label="下一段"
      title="下一段"
      @click="emit('next')"
    >
      <template #icon><SkipForward :size="15" /></template>
    </a-button>

    <a-button
      data-testid="playback-stop"
      type="secondary"
      :disabled="!canPlay || !selectedRecord || isBusy"
      aria-label="停止播放"
      title="停止播放"
      @click="emit('stop')"
    >
      <template #icon><CircleStop :size="15" /></template>
    </a-button>

    <span class="control-clock" data-testid="playback-clock">
      <strong>{{ formatClock(playback.currentTime) }}</strong>
      <i>/</i>
      <span>{{ totalText }}</span>
    </span>

    <span class="control-spacer" />

    <a-select
      v-model="scaleValue"
      data-testid="playback-scale"
      class="control-scale"
      size="small"
      aria-label="播放倍速"
      title="播放倍速"
      :disabled="!canPlay"
    >
      <a-option v-for="scale in scaleOptions" :key="scale" :value="scale">{{ scale }}x</a-option>
    </a-select>

    <a-button
      data-testid="playback-download"
      type="secondary"
      :disabled="!canDownload || !selectedRecord || downloadPending"
      aria-label="归档到服务器"
      title="归档到服务器（在后台把这段录像存到平台，去「录像缓存」看进度）"
      @click="emit('download')"
    >
      <template #icon>
        <LoaderCircle v-if="downloadPending" :size="15" class="spin" />
        <Download v-else :size="15" />
      </template>
    </a-button>

    <a-button data-testid="playback-fullscreen" type="secondary" aria-label="全屏" title="全屏" @click="emit('fullscreen')">
      <template #icon><Maximize2 :size="15" /></template>
    </a-button>
  </div>
</template>

<style scoped>
.playback-controls {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 8px 14px 10px;
  border-top: 1px solid var(--uvp-panel-border);
}
.control-toggle {
  min-width: 86px;
}
.control-clock {
  display: inline-flex;
  gap: 5px;
  align-items: baseline;
  padding-left: 6px;
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.control-clock strong {
  font-size: 12px;
  font-weight: 500;
  color: var(--uvp-text-primary);
}
.control-clock i {
  font-style: normal;
  color: var(--uvp-text-tertiary);
}
.control-spacer {
  flex: 1;
}
.control-scale {
  width: 84px;
}
.spin {
  animation: playback-controls-spin 0.9s linear infinite;
}

@keyframes playback-controls-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
