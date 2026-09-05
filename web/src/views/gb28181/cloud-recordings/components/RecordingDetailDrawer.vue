<template>
  <a-drawer
    :visible="visible"
    width="min(820px, 94vw)"
    :footer="false"
    :esc-to-close="true"
    unmount-on-close
    class="recording-detail-drawer"
    @update:visible="emit('update:visible', $event)"
    @cancel="emit('update:visible', false)"
  >
    <template #title>
      <div class="recording-detail-title">
        <span><FileVideo2 :size="18" /></span>
        <div>
          <strong>录像详情</strong>
          <small>ID {{ recordingId || "--" }}</small>
        </div>
      </div>
    </template>

    <a-spin :loading="loading" class="recording-detail-spin">
      <div class="recording-detail-content">
        <a-alert v-if="errorMessage" type="error" class="recording-detail-error">
          <div class="recording-detail-error__body">
            <span>{{ errorMessage }}</span>
            <a-button data-testid="detail-retry" size="small" @click="loadDetail">重试</a-button>
          </div>
        </a-alert>

        <template v-else-if="detail">
          <section class="recording-detail-context" aria-label="录像归属">
            <div>
              <span>通道</span>
              <strong>{{ detail.channelName || detail.channelCode || "--" }}</strong>
              <code>{{ detail.channelCode || "--" }}</code>
            </div>
            <div>
              <span>设备</span>
              <strong>{{ detail.deviceName || detail.deviceId || "--" }}</strong>
              <code>{{ detail.deviceId || "--" }}</code>
            </div>
            <div>
              <span>存储节点</span>
              <strong>{{ detail.node.name || `节点 ${detail.node.id}` }}</strong>
              <code>{{ detail.node.state || "状态未知" }}</code>
            </div>
          </section>

          <section class="recording-detail-section">
            <div class="recording-detail-heading">
              <h3>文件信息</h3>
              <div class="recording-detail-tags">
                <a-tag :color="availability.color">{{ availability.label }}</a-tag>
                <a-tag :color="detail.metadataState === 'complete' ? 'blue' : 'orange'">
                  {{ detail.metadataState === "complete" ? "信息完整" : "待完善" }}
                </a-tag>
              </div>
            </div>
            <a-descriptions :column="columns" bordered size="medium">
              <a-descriptions-item label="文件名" :span="columns">{{ detail.fileName || "--" }}</a-descriptions-item>
              <a-descriptions-item label="开始时间">{{ formatDateTime(detail.startTime) }}</a-descriptions-item>
              <a-descriptions-item label="结束时间">{{ formatDateTime(detail.endTime) }}</a-descriptions-item>
              <a-descriptions-item label="录像时长">{{ formatDuration(detail.timeLen) }}</a-descriptions-item>
              <a-descriptions-item label="文件大小">{{ formatFileSize(detail.fileSize) }}</a-descriptions-item>
              <a-descriptions-item label="索引来源">{{ detail.source === "hook" ? "录像完成通知" : "目录对账" }}</a-descriptions-item>
              <a-descriptions-item label="最近发现">{{ formatDateTime(detail.lastSeenAt || detail.discoveredAt) }}</a-descriptions-item>
            </a-descriptions>
          </section>

          <div v-if="availability.canAccess && (canPlay || canDownload)" class="recording-detail-actions">
            <a-button v-if="canDownload" data-testid="detail-download" @click="emit('download', detail)">
              <template #icon><Download :size="15" /></template>
              下载
            </a-button>
            <a-button v-if="canPlay" type="primary" data-testid="detail-play" @click="emit('play', detail)">
              <template #icon><Play :size="15" /></template>
              播放
            </a-button>
          </div>
        </template>

        <div v-else class="recording-detail-placeholder">
          <FileVideo2 :size="28" />
          <span>{{ loading ? "正在加载录像详情" : "请选择一条录像" }}</span>
        </div>
      </div>
    </a-spin>
  </a-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Download, FileVideo2, Play } from "@lucide/vue";
import { getRecordingDetail, type RecordingFile } from "../api";
import { availabilityPresentation, recordingErrorPresentation } from "../recordingState";

const props = defineProps<{
  visible: boolean;
  recordingId: string | null;
  canPlay: boolean;
  canDownload: boolean;
}>();
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "play", value: RecordingFile): void;
  (event: "download", value: RecordingFile): void;
}>();

const detail = ref<RecordingFile | null>(null);
const loading = ref(false);
const errorMessage = ref("");
const columns = computed(() => (window.innerWidth < 640 ? 1 : 2));
const availability = computed(() => availabilityPresentation(detail.value?.availability ?? "access_unavailable"));
let requestToken = 0;

async function loadDetail() {
  if (!props.visible || !props.recordingId) return;
  const token = ++requestToken;
  loading.value = true;
  errorMessage.value = "";
  detail.value = null;
  try {
    const response = await getRecordingDetail(props.recordingId);
    if (token === requestToken) detail.value = response.data;
  } catch (error) {
    if (token === requestToken) errorMessage.value = recordingErrorPresentation(error);
  } finally {
    if (token === requestToken) loading.value = false;
  }
}

function formatDateTime(value: string | null | undefined) {
  if (!value) return "--";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "--" : date.toLocaleString("zh-CN", { hour12: false });
}

function formatDuration(seconds: number | null) {
  if (seconds == null || seconds < 0) return "--";
  const total = Math.round(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const remain = total % 60;
  return hours ? `${hours} 小时 ${minutes} 分` : minutes ? `${minutes} 分 ${remain} 秒` : `${remain} 秒`;
}

function formatFileSize(bytes: number | null) {
  if (bytes == null || bytes < 0) return "--";
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}

watch(
  () => [props.visible, props.recordingId] as const,
  ([visible, id]) => {
    if (visible && id) loadDetail();
    else {
      requestToken += 1;
      loading.value = false;
      detail.value = null;
      errorMessage.value = "";
    }
  },
  { immediate: true }
);
</script>

<style scoped>
.recording-detail-spin,
.recording-detail-content { min-height: 470px; }
.recording-detail-content { color: var(--uvp-text-primary); }
.recording-detail-title { display: flex; gap: 10px; align-items: center; min-width: 0; }
.recording-detail-title > span { display: grid; width: 34px; height: 34px; place-items: center; color: var(--uvp-primary); background: color-mix(in srgb, var(--uvp-primary) 10%, transparent); border-radius: 6px; }
.recording-detail-title > div { display: flex; flex-direction: column; min-width: 0; }
.recording-detail-title strong { font-size: 15px; }
.recording-detail-title small { overflow: hidden; color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; text-overflow: ellipsis; white-space: nowrap; }
.recording-detail-context { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; padding: 14px 18px; background: var(--uvp-bg-secondary); border-bottom: 1px solid var(--uvp-border); }
.recording-detail-context > div { display: flex; flex-direction: column; min-width: 0; }
.recording-detail-context span { color: var(--uvp-text-tertiary); font-size: 11px; }
.recording-detail-context strong,
.recording-detail-context code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.recording-detail-context strong { margin: 3px 0; font-size: 13px; }
.recording-detail-context code { color: var(--uvp-text-secondary); font-size: 11px; }
.recording-detail-section { padding: 18px 18px 0; }
.recording-detail-heading { display: flex; gap: 12px; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.recording-detail-heading h3 { margin: 0; font-size: 14px; }
.recording-detail-tags { display: flex; gap: 6px; }
.recording-detail-actions { display: flex; gap: 10px; justify-content: flex-end; padding: 18px; }
.recording-detail-error { margin: 18px; }
.recording-detail-error__body { display: flex; gap: 12px; align-items: center; justify-content: space-between; }
.recording-detail-placeholder { display: flex; min-height: 430px; flex-direction: column; gap: 10px; align-items: center; justify-content: center; color: var(--uvp-text-tertiary); }
@media (max-width: 640px) {
  .recording-detail-spin,
  .recording-detail-content { min-height: 400px; }
  .recording-detail-context { grid-template-columns: 1fr; gap: 10px; padding: 12px; }
  .recording-detail-section { padding: 14px 12px 0; }
  .recording-detail-actions { padding: 14px 12px; }
  .recording-detail-actions :deep(.arco-btn) { min-height: 44px; }
}
</style>
