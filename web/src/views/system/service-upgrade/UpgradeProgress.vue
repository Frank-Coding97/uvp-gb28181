<template>
  <section class="upgrade-progress" :data-state="state" :aria-label="title">
    <div class="progress-heading">
      <strong>{{ title }}</strong>
      <span v-if="reconnecting" class="reconnect-state">正在重连 · 显示上次状态</span>
      <slot name="action" />
    </div>
    <p class="progress-detail" aria-live="polite">{{ detail }}</p>
    <div v-if="upload" class="upload-info">
      <span class="upload-filename" :title="upload.filename">{{ upload.filename }}</span>
      <template v-if="showUploadBar">
        <div class="upload-numbers">
          <span>{{ sizeText }}</span>
          <strong v-if="percentage !== undefined">{{ percentage }}%</strong>
        </div>
        <div
          role="progressbar"
          aria-label="升级包上传进度"
          :aria-valuenow="percentage"
          :aria-valuemin="percentage === undefined ? undefined : 0"
          :aria-valuemax="percentage === undefined ? undefined : 100"
          :aria-valuetext="sizeText"
          :aria-busy="percentage === undefined"
        >
          <div v-if="percentage === undefined" class="unknown-track" :class="{ stopped: state === 'error' }"><span /></div>
          <a-progress
            v-else
            :percent="(percentage || 0) / 100"
            :show-text="false"
            :status="state === 'error' ? 'danger' : 'normal'"
            aria-hidden="true"
          />
        </div>
      </template>
    </div>
    <ol class="progress-steps" aria-label="处理步骤">
      <li
        v-for="(step, index) in steps"
        :key="step"
        :data-step="index"
        :class="{ done: index < current || (state === 'success' && index === current), current: index === current }"
        :aria-current="index === current ? 'step' : undefined"
      >
        <span class="step-marker" aria-hidden="true">{{
          index < current || (state === "success" && index === current)
            ? "✓"
            : index === current && state === "error"
              ? "!"
              : index + 1
        }}</span>
        <span>{{ step }}</span>
        <span class="step-state">{{ stepState(index) }}</span>
      </li>
    </ol>
    <details v-if="error" class="progress-error" @toggle="errorExpanded = ($event.target as HTMLDetailsElement).open">
      <summary>查看原因</summary>
      <pre v-if="errorExpanded">{{ error }}</pre>
    </details>
    <slot />
  </section>
</template>

<script lang="ts">
export interface UploadTransfer {
  filename: string;
  loaded: number;
  total?: number;
  state: "uploading" | "waiting" | "failed";
  error?: string;
}
export interface ProgressDisplay {
  title: string;
  detail: string;
  steps: string[];
  current: number;
  state: "active" | "waiting" | "success" | "error" | "warning" | "unknown";
  error?: string;
}
</script>

<script setup lang="ts">
import { computed, ref } from "vue";
import AProgress from "@arco-design/web-vue/es/progress/progress";

const props = defineProps<ProgressDisplay & { reconnecting?: boolean; upload?: UploadTransfer; showUploadBar?: boolean }>();
const errorExpanded = ref(false);
const percentage = computed(() => {
  if (!props.upload?.total || !Number.isFinite(props.upload.total) || props.upload.total <= 0) return undefined;
  return Math.min(100, Math.max(0, Math.floor((props.upload.loaded / props.upload.total) * 100)));
});
const formatBytes = (bytes: number) => {
  if (!Number.isFinite(bytes) || bytes < 0) return "未知";
  const unit = bytes >= 1024 * 1024 ? 1024 * 1024 : bytes >= 1024 ? 1024 : 1;
  return `${Number((bytes / unit).toFixed(1))} ${unit === 1 ? "B" : unit === 1024 ? "KiB" : "MiB"}`;
};
const sizeText = computed(() =>
  props.upload?.total && percentage.value !== undefined
    ? `${formatBytes(Math.min(props.upload.loaded, props.upload.total))} / ${formatBytes(props.upload.total)}`
    : `已传 ${formatBytes(props.upload?.loaded || 0)}`
);
const stepState = (index: number) => {
  if (index < props.current || (props.state === "success" && index === props.current)) return "已完成";
  if (index !== props.current) return "待处理";
  if (props.reconnecting) return "等待重连";
  return { active: "处理中", waiting: "等待中", success: "已完成", error: "未完成", warning: "请查看结果", unknown: "状态未知" }[
    props.state
  ];
};
</script>

<style scoped lang="scss">
.upgrade-progress {
  min-width: 0;
  padding: 20px 24px;
  color: var(--color-text-1);
  background: var(--uvp-panel-bg, var(--color-bg-2));
  border: 1px solid var(--uvp-panel-border, var(--color-border-2));
  border-radius: 14px;
  box-shadow: var(--uvp-panel-shadow);
  container-type: inline-size;
}
.progress-heading {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  align-items: center;
  strong {
    margin-right: auto;
  }
}
.reconnect-state {
  font-size: 12px;
  color: rgb(var(--orange-6));
}
.progress-detail {
  margin: 8px 0 16px;
  color: var(--color-text-2);
  overflow-wrap: anywhere;
}
.upload-info {
  margin-bottom: 18px;
}
.upload-filename {
  display: block;
  font-size: 12px;
  color: var(--color-text-3);
  overflow-wrap: anywhere;
}
.upload-numbers {
  display: flex;
  gap: 12px;
  justify-content: space-between;
  margin: 8px 0 4px;
  font-variant-numeric: tabular-nums;
}
.unknown-track {
  height: 4px;
  overflow: hidden;
  background: var(--color-fill-3);
  border-radius: 4px;
  span {
    display: block;
    width: 30%;
    height: 100%;
    background: rgb(var(--primary-6));
    animation: upload-pending 1.4s ease-in-out infinite alternate;
  }
  &.stopped span {
    background: rgb(var(--danger-6));
    animation: none;
  }
}

@keyframes upload-pending {
  to {
    transform: translateX(230%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .unknown-track span {
    animation: none;
  }
}
.progress-steps {
  display: flex;
  padding: 0;
  margin: 0;
  list-style: none;
}
.progress-steps li {
  position: relative;
  z-index: 0;
  display: grid;
  flex: 1;
  grid-template-columns: 26px 1fr;
  gap: 2px 8px;
  min-width: 0;
  padding-right: 12px;
  font-size: 13px;
  color: var(--color-text-3);
  &:not(:last-child)::after {
    position: absolute;
    top: 12px;
    right: 10px;
    left: 32px;
    z-index: -1;
    content: "";
    border-top: 1px solid var(--color-border-2);
  }
  > span:not(.step-marker) {
    justify-self: start;
    background: var(--uvp-panel-bg, var(--color-bg-2));
  }
  &.current {
    color: rgb(var(--primary-6));
  }
  &.done {
    color: var(--color-text-2);
  }
}
.step-marker {
  display: grid;
  place-items: center;
  width: 24px;
  height: 24px;
  background: var(--color-fill-2);
  border-radius: 50%;
}
.current .step-marker {
  color: white;
  background: rgb(var(--primary-6));
}
.done .step-marker {
  color: rgb(var(--success-6));
  background: var(--color-success-light-1);
}
.step-state {
  grid-column: 2;
  font-size: 12px;
  color: var(--color-text-3);
}
[data-state="error"] .current {
  color: rgb(var(--danger-6));
  .step-marker {
    color: white;
    background: rgb(var(--danger-6));
  }
}
[data-state="warning"] .current {
  color: rgb(var(--orange-6));
  .step-marker {
    color: white;
    background: rgb(var(--orange-6));
  }
}
.progress-error {
  margin-top: 14px;
  color: rgb(var(--danger-6));
  summary {
    cursor: pointer;
  }
  pre {
    max-height: 180px;
    overflow: auto;
    color: var(--color-text-2);
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
}

@container (max-width: 520px) {
  .progress-steps {
    flex-direction: column;
    gap: 14px;
  }
  .progress-steps li:not(:last-child)::after {
    top: 28px;
    bottom: -10px;
    left: 12px;
    width: 0;
    border-top: 0;
    border-left: 1px solid var(--color-border-2);
  }
}

@media (width <= 600px) {
  .upgrade-progress {
    padding: 16px;
  }
}
</style>
