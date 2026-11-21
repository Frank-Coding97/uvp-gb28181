<script setup lang="ts">
import { computed } from "vue";
import { CircleAlert, LoaderCircle, VideoOff } from "@lucide/vue";
import PlayWindow from "../../gb28181/components/PlayWindow.vue";

const props = defineProps<{
  url: string;
  zlmWebrtc?: boolean;
  hasAudio?: boolean;
  /** 会话创建 / 缓冲中：显示转圈而不是空态。 */
  loading?: boolean;
  statusText: string;
  failed?: boolean;
  /** 有画面时左上角显示「通道名 · 录像类型」。 */
  osdTitle?: string;
  /** 有画面时右上角显示「倍速 · 状态」。 */
  osdMeta?: string;
  /** 没有画面时展示的引导；为空表示「还没选通道」，用默认文案。 */
  emptyTitle?: string;
  emptyDescription?: string;
  emptyAction?: string;
}>();

const emit = defineEmits<{
  error: [message: string];
  timeupdate: [timestamp: number];
  loadingChange: [value: boolean];
  emptyAction: [];
}>();

const showEmpty = computed(() => !props.url && !props.loading);
const actionText = computed(() => props.emptyAction || "");
</script>

<template>
  <div class="playback-stage" data-testid="playback-stage">
    <PlayWindow
      v-if="url"
      data-testid="playback-player"
      :data-media-url="url"
      :url="url"
      :zlm-webrtc="zlmWebrtc || false"
      :has-audio="hasAudio || false"
      playback
      @error="message => emit('error', message)"
      @timeupdate="timestamp => emit('timeupdate', timestamp)"
      @loading="value => emit('loadingChange', value)"
    />

    <div v-if="url && osdTitle" class="stage-osd is-left">{{ osdTitle }}</div>
    <div v-if="url && osdMeta" class="stage-osd is-right">{{ osdMeta }}</div>

    <div v-if="loading" class="stage-overlay" data-testid="playback-loading">
      <LoaderCircle :size="34" class="spin" aria-hidden="true" />
      <span>{{ statusText }}</span>
    </div>

    <div v-else-if="showEmpty" class="stage-overlay is-empty" data-testid="playback-empty">
      <CircleAlert v-if="failed" :size="38" aria-hidden="true" />
      <VideoOff v-else :size="40" :stroke-width="1.3" aria-hidden="true" />
      <strong>{{ emptyTitle || "从左侧选择一个通道" }}</strong>
      <span v-if="emptyDescription">{{ emptyDescription }}</span>
      <a-button v-if="actionText" type="secondary" size="small" @click="emit('emptyAction')">{{ actionText }}</a-button>
    </div>

    <span class="stage-status" data-testid="playback-status" aria-live="polite">{{ statusText }}</span>
  </div>
</template>

<style scoped>
.playback-stage {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: #1f1f1e;
}
.stage-osd {
  position: absolute;
  top: 8px;
  padding: 2px 8px;
  font-size: 11px;
  color: #f1efe8;
  pointer-events: none;
  background: rgb(0 0 0 / 45%);
  border-radius: 4px;
}
.stage-osd.is-left {
  left: 10px;
}
.stage-osd.is-right {
  right: 10px;
}
.stage-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  color: #b4b2a9;
  text-align: center;
}
.stage-overlay.is-empty strong {
  font-size: 13px;
  font-weight: 500;
  color: #d3d1c7;
}
.stage-status {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
}
.spin {
  animation: playback-stage-spin 0.9s linear infinite;
}

@keyframes playback-stage-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
