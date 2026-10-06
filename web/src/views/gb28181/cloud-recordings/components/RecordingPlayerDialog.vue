<template>
  <!--
    ⛔ `modal-class="uvp-system-dialog"` 不能省（这就是它跟系统"不像一家"的原因）：
       本仓弹窗壳层的底色 / 描边 / 阴影 / 16px 圆角 / header 渐变与底边线 / body 内边距
       全部由全局 `.uvp-system-dialog .arco-modal-*` 那组规则提供（见 uvp-ui-language.scss）。
       本组件原来写的是 `class="recording-player-dialog"` —— 而 Arco 的 Modal 是
       `inheritAttrs: false`，`class` 经 `$attrs` 落在**外层 `.arco-modal-container`** 上，
       `modal-class` 才落在 `.arco-modal` 面板上（已核对 node_modules 渲染函数）。
       于是壳层规则一条都没命中 ⇒ 暗色下弹出的是 Arco 自带灰面板，
       摆在深蓝黑控制台界面上就是"两套皮肤"。同目录 RecordingDetailDrawer 是对的。
  -->
  <a-modal
    :visible="visible"
    width="min(94vw, 960px)"
    :footer="false"
    :esc-to-close="true"
    unmount-on-close
    modal-class="uvp-system-dialog recording-player-dialog"
    @update:visible="emit('update:visible', $event)"
    @cancel="emit('update:visible', false)"
  >
    <template #title>
      <div class="recording-player-title">
        <span><CirclePlay :size="18" /></span>
        <div>
          <!-- 两行都是ellipsis 截断，鼠标悬停给出全文，否则长通道名/文件名看不到 -->
          <strong :title="recording?.channelName || '云端录像'">
            {{ recording?.channelName || "云端录像" }}
          </strong>
          <small :title="recording?.fileName || '正在准备播放'">
            {{ recording?.fileName || "正在准备播放" }}
          </small>
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
            controlsList="nodownload"
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
.recording-player-title {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}
.recording-player-title > span {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  color: var(--uvp-primary);
  background: color-mix(in srgb, var(--uvp-primary) 10%, transparent);
  border-radius: 6px;
}
.recording-player-title > div {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.recording-player-title strong {
  font-size: 15px;
}
.recording-player-title strong,
.recording-player-title small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.recording-player-title small {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.recording-player-body {
  min-width: 0;
  color: var(--uvp-text-primary);
}

/* 视频舞台：底色是**两主题共用的深灰**，不是主题色。
   播放画面（尤其暗场录像）压在浅色面板上会"发灰看不清"，压在纯黑上又太突兀，
   所以固定用 #090b0f —— 与多屏回放 `.screen-slot.empty` 同一口径。
   ⛔ 别把它改成 var(--uvp-panel-bg) 之类跟着主题走：那样亮色下白底 + 深色控件栏会很刺眼。 */
.recording-player-stage {
  display: grid;
  place-items: center;
  width: 100%;
  aspect-ratio: 16 / 9;
  overflow: hidden;
  background: #090b0f;
  border-radius: 12px;
  box-shadow: inset 0 0 0 1px rgb(255 255 255 / 6%);
}
.recording-player-stage video {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.recording-player-placeholder {
  display: flex;
  flex-direction: column;
  gap: 10px;
  align-items: center;

  /* 同理：占位在深色舞台上，必须用固定浅色，不能跟主题走 */
  color: #a8b0bd;
}
.recording-player-error {
  margin-top: 12px;
}
.recording-player-error > div {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
}

@media (width <= 640px) {
  .recording-player-stage {
    aspect-ratio: 4 / 3;
  }
  .recording-player-error :deep(.arco-btn) {
    min-height: 44px;
  }
}
</style>
