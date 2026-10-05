<script setup lang="ts">
import { computed } from "vue";
import { RefreshCcw, RotateCcw, Send } from "@lucide/vue";
import DeviceConfigSlider from "../../device-mgmt/DeviceConfigSlider.vue";
import type { VideoParamCodecItem } from "../../videoParamCodec";

const props = defineProps<{
  rows: VideoParamCodecItem[];
  selectedStream: number;
  reconcileText: string;
  reconcileTone: string;
  error: string;
  canRead: boolean;
  canApply: boolean;
  dirtyCount: number;
  applying: boolean;
}>();

const emit = defineEmits<{
  (e: "update:selectedStream", value: number): void;
  (e: "reset"): void;
  (e: "read"): void;
  (e: "apply"): void;
}>();

const VIDEO_FORMAT_OPTIONS = [
  { label: "MPEG-4", value: "1" },
  { label: "H.264", value: "2" },
  { label: "SVAC", value: "3" },
  { label: "3GP", value: "4" },
  { label: "H.265", value: "5" }
];

const RESOLUTION_OPTIONS = [
  { label: "QCIF", value: "1" },
  { label: "CIF", value: "2" },
  { label: "4CIF", value: "3" },
  { label: "D1", value: "4" },
  { label: "720P", value: "5" },
  { label: "1080P", value: "6" }
];

const BIT_RATE_TYPE_OPTIONS = [
  { label: "CBR", value: "1" },
  { label: "VBR", value: "2" }
];

const streamLabel = (num: number) => (num === 0 ? "主码流" : `子码流 ${num}`);
const videoBitRateDisabled = (row: VideoParamCodecItem) => row.bitRateType === "2";

const currentRow = computed(() => props.rows.find(r => r.streamNumber === props.selectedStream));
const isEmpty = computed(() => !props.rows.length);
</script>

<template>
  <section class="video-param-card" data-testid="video-param-card">
    <!-- 顶部：对账状态 + 码流选择 + 操作按钮 -->
    <div class="vpc-header">
      <div class="vpc-reconcile" :class="`is-${reconcileTone}`">
        <span class="vpc-reconcile-dot" />
        <span>{{ reconcileText }}</span>
      </div>
      <div class="vpc-actions">
        <a-select
          :model-value="selectedStream"
          size="small"
          class="vpc-stream-select"
          aria-label="码流选择"
          @change="emit('update:selectedStream', Number($event))"
        >
          <a-option v-for="row in rows" :key="row.streamNumber" :value="row.streamNumber">
            {{ streamLabel(row.streamNumber) }}
          </a-option>
        </a-select>
        <a-button
          type="text"
          size="mini"
          shape="square"
          html-type="button"
          aria-label="还原码流改动"
          class="vpc-btn"
          data-testid="vpc-reset"
          :disabled="!dirtyCount"
          :title="dirtyCount ? `还原 ${dirtyCount} 项改动` : '无改动'"
          @click="emit('reset')"
        >
          <RotateCcw :size="11" />
        </a-button>
        <a-button
          type="text"
          size="mini"
          shape="square"
          html-type="button"
          aria-label="读取编码参数"
          class="vpc-btn"
          data-testid="vpc-read"
          :disabled="!canRead || applying"
          @click="emit('read')"
        >
          <RefreshCcw :size="11" />
        </a-button>
        <a-button
          type="primary"
          size="mini"
          shape="square"
          html-type="button"
          aria-label="应用编码参数"
          class="vpc-btn is-primary"
          data-testid="vpc-apply"
          :disabled="applying || !dirtyCount || !canApply"
          @click="emit('apply')"
        >
          <Send :size="11" />
        </a-button>
      </div>
    </div>

    <p v-if="error" class="vpc-error" data-testid="vpc-error">{{ error }}</p>

    <div v-if="isEmpty" class="vpc-empty" data-testid="vpc-empty">
      <p>还没有回读值</p>
      <em>点击「读取」获取当前编码参数</em>
    </div>

    <!-- 参数表单：所有字段紧凑排列，无分组分割线 -->
    <div v-else-if="currentRow" class="vpc-form">
      <div class="vpc-fields">
        <!-- 编码格式 -->
        <div class="vpc-field">
          <label>编码格式</label>
          <a-select v-model="currentRow.videoFormat" size="small">
            <a-option v-for="opt in VIDEO_FORMAT_OPTIONS" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </a-option>
          </a-select>
        </div>

        <!-- 分辨率 -->
        <div class="vpc-field">
          <label>分辨率</label>
          <a-select v-model="currentRow.resolution" size="small">
            <a-option v-for="opt in RESOLUTION_OPTIONS" :key="opt.value" :value="opt.value">
              {{ opt.label }}
            </a-option>
          </a-select>
        </div>

        <!-- 帧率 -->
        <div class="vpc-field vpc-field-slider">
          <label>帧率</label>
          <DeviceConfigSlider v-model="currentRow.frameRate" :min="0" :max="99" label="帧率" />
        </div>

        <!-- 码率类型 -->
        <div class="vpc-field">
          <label>码率类型</label>
          <div class="vpc-segment" role="group">
            <a-button
              v-for="opt in BIT_RATE_TYPE_OPTIONS"
              :key="opt.value"
              type="text"
              size="mini"
              html-type="button"
              :class="{ 'is-on': currentRow.bitRateType === opt.value }"
              @click="currentRow.bitRateType = opt.value"
            >
              {{ opt.label }}
            </a-button>
          </div>
        </div>

        <!-- 码率值 -->
        <div class="vpc-field vpc-field-slider">
          <label>码率</label>
          <DeviceConfigSlider
            v-model="currentRow.videoBitRate"
            :min="0"
            :max="100000"
            :step="100"
            unit="kb/s"
            label="码率"
            :disabled="videoBitRateDisabled(currentRow)"
          />
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.video-param-card {
  display: grid;
  gap: 8px;
  padding: 10px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}

/* 顶部：对账状态 + 操作栏 */
.vpc-header {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}

.vpc-reconcile {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  padding: 4px 8px;
  font-size: 10px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}

.vpc-reconcile-dot {
  width: 5px;
  height: 5px;
  background: currentcolor;
  border-radius: 50%;
}

.vpc-reconcile.is-ready {
  color: var(--uvp-success);
  border-color: var(--uvp-success-border);
}

.vpc-reconcile.is-stale {
  color: var(--uvp-warning);
  border-color: var(--uvp-warning-border);
}

.vpc-reconcile.is-mismatch {
  color: var(--uvp-danger);
  border-color: var(--uvp-danger-border);
}

.vpc-actions {
  display: flex;
  gap: 4px;
  align-items: center;
}

.vpc-stream-select {
  width: 90px;
}

.vpc-btn.arco-btn[type="button"] {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
  transition: all 0.15s ease;
}

.vpc-btn.arco-btn[type="button"]:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

.vpc-btn.arco-btn[type="button"].is-primary {
  color: #ffffff;
  background: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

.vpc-btn.arco-btn[type="button"].is-primary:hover:not(:disabled) {
  background: var(--uvp-brand-strong);
}

.vpc-btn.arco-btn[type="button"]:disabled {
  color: var(--uvp-text-disabled);
  cursor: not-allowed;
  background: var(--uvp-dialog-control-bg);
  border-color: var(--uvp-panel-border);
  opacity: 1;
}

.vpc-error {
  padding: 6px 8px;
  margin: 0;
  font-size: 10.5px;
  line-height: 1.4;
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border: 1px solid var(--uvp-danger-border);
  border-radius: 6px;
}

.vpc-empty {
  padding: 12px;
  text-align: center;
}

.vpc-empty p {
  margin: 0 0 4px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
}

.vpc-empty em {
  font-size: 10px;
  font-style: normal;
  color: var(--uvp-text-secondary);
}

/* 表单：紧凑排列，无分组标题 */
.vpc-form {
  display: grid;
  gap: 0;
}

.vpc-fields {
  display: grid;
  gap: 6px;
}

.vpc-field {
  display: grid;
  grid-template-columns: 70px 1fr;
  gap: 8px;
  align-items: center;
}

.vpc-field label {
  font-size: 10.5px;
  color: var(--uvp-text-secondary);
}

.vpc-field-slider {
  grid-template-columns: 70px minmax(0, 1fr);
}

/* 分段控件（CBR/VBR） */
.vpc-segment {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 4px;
}

.vpc-segment button.arco-btn[type="button"] {
  height: 26px;
  padding: 0 8px;
  font-size: 10.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
  transition: all 0.15s ease;
}

.vpc-segment button.arco-btn[type="button"]:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

.vpc-segment button.arco-btn[type="button"].is-on {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
}

body[arco-theme="dark"] .vpc-segment button.arco-btn[type="button"].is-on:not(:disabled) {
  color: #ffffff;
  background: #2563eb;
  border-color: #2563eb;
}

.vpc-segment button.arco-btn[type="button"]:disabled {
  color: var(--uvp-text-disabled);
  cursor: not-allowed;
  background: var(--uvp-dialog-control-bg);
  border-color: var(--uvp-panel-border);
  opacity: 1;
}
</style>
