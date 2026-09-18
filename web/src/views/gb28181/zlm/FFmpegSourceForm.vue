<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import type { ZLMCapabilityState, ZLMFFmpegSourceCreateRequest } from "@/api/gb28181-zlm-ingress";
import {
  buildFFmpegCreateRequest,
  ffmpegCreateDecision,
  type FFmpegSourceFormState
} from "./ffmpegSourcesState";

const props = defineProps<{
  visible: boolean;
  capability: ZLMCapabilityState;
  templates: string[];
  permitted: boolean;
  loading?: boolean;
}>();
const emit = defineEmits<{
  "update:visible": [visible: boolean];
  submit: [request: ZLMFFmpegSourceCreateRequest];
}>();

const form = reactive<FFmpegSourceFormState>({
  templateKey: "",
  srcUrl: "",
  dstUrl: "",
  timeoutMs: "3000",
  enableHls: false,
  enableMp4: false
});
const errors = reactive<Record<string, string>>({});
const decision = computed(() => ffmpegCreateDecision(props.capability, props.templates, props.permitted));

function clearErrors() {
  Object.keys(errors).forEach(key => delete errors[key]);
}

function reset() {
  Object.assign(form, {
    templateKey: props.templates[0] ?? "",
    srcUrl: "",
    dstUrl: "",
    timeoutMs: "3000",
    enableHls: false,
    enableMp4: false
  });
  clearErrors();
}

watch(() => props.visible, visible => { if (visible) reset(); });

function validate() {
  const result = buildFFmpegCreateRequest(form, props.templates);
  clearErrors();
  Object.assign(errors, result.errors);
  return result;
}

function submit() {
  if (!decision.value.allowed || props.loading) return;
  const result = validate();
  if (result.request) emit("submit", result.request);
}

function close() {
  if (!props.loading) emit("update:visible", false);
}
</script>

<template>
  <a-modal :visible="visible" modal-class="uvp-system-dialog" :width="680" :footer="false" :mask-closable="false" unmount-on-close @cancel="close">
    <template #title>创建 FFmpeg 源</template>
    <a-alert v-if="!decision.allowed" type="warning">{{ decision.reason }}</a-alert>
    <a-form :model="form" layout="vertical" class="ffmpeg-form" @submit-success="submit">
      <a-form-item label="模板" required :validate-status="errors.templateKey ? 'error' : undefined" :help="errors.templateKey || '只显示后端登记的模板标识，不向浏览器返回模板内容。'">
        <a-select v-model="form.templateKey" allow-clear placeholder="选择模板" :disabled="templates.length === 0" @change="validate">
          <a-option v-for="key in templates" :key="key" :value="key">{{ key }}</a-option>
        </a-select>
      </a-form-item>
      <a-form-item label="源地址" required :validate-status="errors.srcUrl ? 'error' : undefined" :help="errors.srcUrl || '完整地址只随创建请求发送，创建后仅返回脱敏摘要。'">
        <a-input-password v-model="form.srcUrl" allow-clear autocomplete="off" placeholder="rtsp://camera.example/live" @blur="validate" />
      </a-form-item>
      <a-form-item label="目标地址" required :validate-status="errors.dstUrl ? 'error' : undefined" :help="errors.dstUrl || '例如 rtmp://media.example/live/camera'">
        <a-input-password v-model="form.dstUrl" allow-clear autocomplete="off" placeholder="rtmp://media.example/live/camera" @blur="validate" />
      </a-form-item>
      <div class="ffmpeg-form__row">
        <a-form-item label="超时（毫秒）" :validate-status="errors.timeoutMs ? 'error' : undefined" :help="errors.timeoutMs || '范围 1–300000。'">
          <a-input v-model="form.timeoutMs" allow-clear inputmode="numeric" @blur="validate" />
        </a-form-item>
        <a-form-item label="输出能力">
          <a-space>
            <a-checkbox v-model="form.enableHls">生成 HLS</a-checkbox>
            <a-checkbox v-model="form.enableMp4">生成 MP4</a-checkbox>
          </a-space>
        </a-form-item>
      </div>
      <div class="ffmpeg-form__actions">
        <a-button :disabled="loading" @click="close">取消</a-button>
        <a-button type="primary" html-type="submit" :loading="loading" :disabled="!decision.allowed">确认创建</a-button>
      </div>
    </a-form>
  </a-modal>
</template>

<style scoped>
.ffmpeg-form { margin-top: 16px; }
.ffmpeg-form__row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; }
.ffmpeg-form__actions { display: flex; justify-content: flex-end; gap: 8px; }
@media (max-width: 700px) { .ffmpeg-form__row { grid-template-columns: 1fr; } }
</style>
