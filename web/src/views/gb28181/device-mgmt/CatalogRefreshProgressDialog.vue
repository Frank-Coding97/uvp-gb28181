<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from "vue";
import type { CatalogRefreshProgress } from "./api";

const props = defineProps<{
  visible: boolean;
  deviceName: string;
  deviceId: string;
  progress: CatalogRefreshProgress | null;
}>();

const emit = defineEmits<{ close: [] }>();

let closeTimer: ReturnType<typeof setTimeout> | null = null;

// ⛔ Arco 的 `a-progress` 收的是 **0~1 的比值**，不是百分数：
//    circle 的 `strokeDashoffset = (percent >= 1 ? 0 : 1 - percent) * 周长`
//    （@arco-design/web-vue/es/progress/circle.js）。传 40 ⇒ offset 恒为 0
//    ⇒ **圆环从头到尾画满**，还会因 `percent >= 1` 被判成 success 而提前显示 ✓。
//    所以展示用百分数（progressPercent），喂组件用比值（progressRatio）。
//    判别依据与防回归见 `src/style/model/arco-progress-percent.test.ts`。
const progressPercent = computed(() => {
  const progress = props.progress;
  if (progress?.status === "completed") return 100;
  if (!progress?.totalCount || progress.totalCount <= 0) return 0;
  return Math.min(100, Math.round((progress.receivedCount / progress.totalCount) * 100));
});

const progressRatio = computed(() => progressPercent.value / 100);

const progressStatus = computed<"normal" | "success" | "danger">(() => {
  if (props.progress?.status === "completed") return "success";
  if (props.progress?.status === "failed" || props.progress?.status === "timeout") return "danger";
  return "normal";
});

const message = computed(() => {
  const progress = props.progress;
  if (progress?.status === "completed") return "刷新成功";
  if (progress?.status === "failed" || progress?.status === "timeout") {
    return progress.errorMessage || (progress.status === "timeout" ? "刷新超时" : "刷新失败");
  }
  if (progress?.status === "persisting") return "目录已收齐,正在入库";
  if (progress?.status === "receiving") {
    if (progress.totalCount != null) return `同步中...[${progress.receivedCount}/${progress.totalCount}]`;
    return `同步中...[${progress.receivedCount}]`;
  }
  return "等待同步中";
});

const channelCountLabel = computed(() => {
  const progress = props.progress;
  const receivedCount = progress?.receivedCount ?? 0;
  if (progress?.totalCount != null) return `已刷新 ${receivedCount} / ${progress.totalCount} 个通道`;
  return `已刷新 ${receivedCount} 个通道`;
});

function clearCloseTimer() {
  if (closeTimer !== null) {
    clearTimeout(closeTimer);
    closeTimer = null;
  }
}

function close() {
  clearCloseTimer();
  emit("close");
}

watch(
  () => props.progress?.status,
  status => {
    clearCloseTimer();
    if (status === "completed") closeTimer = setTimeout(close, 3000);
  }
);

onBeforeUnmount(clearCloseTimer);
</script>

<template>
  <a-modal
    :visible="visible"
    :width="240"
    modal-class="uvp-system-dialog catalog-refresh-progress-dialog"
    hide-title
    :footer="false"
    :mask-closable="false"
    unmount-on-close
    @cancel="close"
  >
    <div class="catalog-refresh-progress" :aria-label="`${deviceName || deviceId}刷新进度`">
      <a-progress type="circle" :percent="progressRatio" :status="progressStatus" :width="120" />
      <div class="progress-message">{{ message }}</div>
      <div class="progress-count">{{ channelCountLabel }}</div>
    </div>
  </a-modal>
</template>

<style scoped>
.catalog-refresh-progress {
  display: flex;
  flex-direction: column;
  gap: 12px;
  align-items: center;
  padding: 4px 0 8px;
  text-align: center;
}

.progress-message {
  max-width: 208px;
  min-height: 20px;
  line-height: 20px;
  color: var(--color-text-1);
  overflow-wrap: anywhere;
}

.progress-count {
  min-height: 18px;
  font-size: 12px;
  line-height: 18px;
  color: var(--uvp-text-tertiary, var(--color-text-3));
}
</style>
