<template>
  <a-modal
    :visible="visible"
    :width="960"
    :footer="false"
    :esc-to-close="true"
    unmount-on-close
    class="recording-player-dialog"
    @update:visible="emit('update:visible', $event)"
    @cancel="emit('update:visible', false)"
  >
    <template #title>
      <div class="recording-player-title">
        <span><CirclePlay :size="18" /></span>
        <div>
          <strong>{{ recording?.channelName || "云端录像" }}</strong>
          <small>{{ recording?.fileName || "正在准备播放" }}</small>
        </div>
      </div>
    </template>

    <div class="recording-player-body">
      <a-spin :loading="loading">
        <div class="recording-player-stage">
          <video
            v-if="source"
            ref="videoRef"
            :src="source"
            controls
            playsinline
            preload="metadata"
            @error="handleMediaError"
            @loadedmetadata="restorePosition"
          />
          <div v-else class="recording-player-placeholder">
            <CirclePlay :size="34" />
            <span>{{ loading ? "正在申请播放权限" : "暂无可播放内容" }}</span>
          </div>
        </div>
      </a-spin>
      <a-alert v-if="errorMessage" type="error" class="recording-player-error">
        <div>
          <span>{{ errorMessage }}</span>
          <a-button data-testid="player-retry" size="small" @click="loadAccess(true)">重试</a-button>
        </div>
      </a-alert>
    </div>
  </a-modal>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
import { CirclePlay } from "@lucide/vue";
import { contentURL, issueRecordingAccess, type RecordingFile } from "../api";
import { createPlaybackRecovery, recordingErrorPresentation } from "../recordingState";

type PlaybackRecording = Pick<RecordingFile, "id" | "fileName" | "channelName" | "startTime">;

const props = defineProps<{ visible: boolean; recording: PlaybackRecording | null }>();
const emit = defineEmits<{ (event: "update:visible", value: boolean): void }>();

const videoRef = ref<HTMLVideoElement | null>(null);
const source = ref("");
const loading = ref(false);
const errorMessage = ref("");
const recovery = createPlaybackRecovery();
let pendingResumeAt: number | null = null;
let requestToken = 0;

async function loadAccess(resetRecovery = false) {
  if (!props.visible || !props.recording) return;
  if (resetRecovery) recovery.reset();
  const token = ++requestToken;
  loading.value = true;
  errorMessage.value = "";
  try {
    const response = await issueRecordingAccess(props.recording.id, "play");
    if (token === requestToken) source.value = contentURL(props.recording.id, response.data.capability);
  } catch (error) {
    if (token === requestToken) {
      source.value = "";
      errorMessage.value = recordingErrorPresentation(error);
    }
  } finally {
    if (token === requestToken) loading.value = false;
  }
}

async function handleMediaError() {
  const position = videoRef.value?.currentTime ?? 0;
  const decision = recovery.next(Number.isFinite(position) ? position : 0);
  if (!decision.retry) {
    errorMessage.value = "播放失败，请重试";
    return;
  }
  pendingResumeAt = decision.resumeAt;
  await loadAccess(false);
}

function restorePosition() {
  if (videoRef.value && pendingResumeAt !== null) {
    videoRef.value.currentTime = pendingResumeAt;
    pendingResumeAt = null;
  }
}

watch(
  () => [props.visible, props.recording?.id] as const,
  ([visible, id]) => {
    requestToken += 1;
    source.value = "";
    errorMessage.value = "";
    pendingResumeAt = null;
    recovery.reset();
    if (visible && id) loadAccess(false);
    else loading.value = false;
  },
  { immediate: true }
);
</script>

<style scoped>
.recording-player-title { display: flex; gap: 10px; align-items: center; min-width: 0; }
.recording-player-title > span { display: grid; width: 34px; height: 34px; place-items: center; color: var(--uvp-primary); background: color-mix(in srgb, var(--uvp-primary) 10%, transparent); border-radius: 6px; }
.recording-player-title > div { display: flex; flex-direction: column; min-width: 0; }
.recording-player-title strong,
.recording-player-title small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.recording-player-title small { color: var(--uvp-text-tertiary); font-size: 11px; }
.recording-player-body { min-width: 0; }
.recording-player-stage { display: grid; width: 100%; aspect-ratio: 16 / 9; place-items: center; overflow: hidden; background: #090b0f; }
.recording-player-stage video { display: block; width: 100%; height: 100%; object-fit: contain; }
.recording-player-placeholder { display: flex; flex-direction: column; gap: 10px; align-items: center; color: #a8b0bd; }
.recording-player-error { margin-top: 12px; }
.recording-player-error > div { display: flex; gap: 12px; align-items: center; justify-content: space-between; }
@media (max-width: 640px) {
  .recording-player-stage { aspect-ratio: 4 / 3; }
  .recording-player-error :deep(.arco-btn) { min-height: 44px; }
}
</style>
