<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import type {
  ZLMCapabilityState,
  ZLMPullProxyCreateRequest,
  ZLMPushProxyCreateRequest
} from "@/api/gb28181-zlm-ingress";
import {
  buildProxyCreateRequest,
  proxyCapabilityPresentation,
  type ProxyFormState,
  type ProxyTab
} from "./proxyManagementState";

const props = defineProps<{
  visible: boolean;
  kind: ProxyTab;
  capability: ZLMCapabilityState;
  permitted: boolean;
  loading?: boolean;
}>();

const emit = defineEmits<{
  "update:visible": [visible: boolean];
  submit: [request: ZLMPullProxyCreateRequest | ZLMPushProxyCreateRequest];
}>();

const form = reactive<ProxyFormState>({
  schema: "rtsp",
  vhost: "__defaultVhost__",
  app: "live",
  stream: "",
  url: "",
  retryCount: "",
  rtpType: "",
  timeoutSec: ""
});
const errors = reactive<Record<string, string>>({});
const capability = computed(() => proxyCapabilityPresentation(props.capability));
const canSubmit = computed(() => capability.value.actionable && props.permitted && !props.loading);

function clearErrors() {
  Object.keys(errors).forEach(key => delete errors[key]);
}

function reset() {
  Object.assign(form, {
    schema: "rtsp",
    vhost: "__defaultVhost__",
    app: "live",
    stream: "",
    url: "",
    retryCount: "",
    rtpType: "",
    timeoutSec: ""
  });
  clearErrors();
}

watch(() => [props.visible, props.kind] as const, ([visible]) => {
  if (visible) reset();
});

function validate() {
  const result = buildProxyCreateRequest(props.kind, form);
  clearErrors();
  Object.assign(errors, result.errors);
  return result;
}

function submit() {
  if (!canSubmit.value) return;
  const result = validate();
  if (result.request) emit("submit", result.request);
}

function close() {
  if (!props.loading) emit("update:visible", false);
}
</script>

<template>
  <a-modal
    :visible="visible"
    modal-class="uvp-system-dialog"
    :width="680"
    :footer="false"
    :mask-closable="false"
    unmount-on-close
    @cancel="close"
  >
    <template #title>创建{{ kind === 'pull' ? '拉流' : '推流' }}代理</template>
    <a-alert v-if="!capability.actionable" type="warning">{{ capability.label }}，创建操作保持禁用。</a-alert>
    <a-alert v-else-if="!permitted" type="warning">当前账号缺少代理管理权限。</a-alert>
    <a-form :model="form" layout="vertical" class="proxy-form" @submit-success="submit">
      <div class="form-grid form-grid--media">
        <a-form-item label="Schema" required :validate-status="errors.schema ? 'error' : undefined" :help="errors.schema">
          <a-input v-model="form.schema" allow-clear placeholder="rtsp" @blur="validate" />
        </a-form-item>
        <a-form-item label="VHost" required :validate-status="errors.vhost ? 'error' : undefined" :help="errors.vhost">
          <a-input v-model="form.vhost" allow-clear placeholder="__defaultVhost__" @blur="validate" />
        </a-form-item>
        <a-form-item label="App" required :validate-status="errors.app ? 'error' : undefined" :help="errors.app">
          <a-input v-model="form.app" allow-clear placeholder="live" @blur="validate" />
        </a-form-item>
        <a-form-item label="Stream" required :validate-status="errors.stream ? 'error' : undefined" :help="errors.stream">
          <a-input v-model="form.stream" allow-clear placeholder="camera-01" @blur="validate" />
        </a-form-item>
      </div>
      <a-form-item
        :label="kind === 'pull' ? '源地址' : '目标地址'"
        required
        :validate-status="errors.url ? 'error' : undefined"
        :help="errors.url || '完整地址仅在本次创建请求中提交；列表只展示后端脱敏摘要。'"
      >
        <a-input-password
          v-model="form.url"
          allow-clear
          autocomplete="off"
          :placeholder="kind === 'pull' ? 'rtsp://camera.example/live' : 'rtmp://media.example/live/out'"
          @blur="validate"
        />
      </a-form-item>
      <div class="form-grid">
        <a-form-item label="重试次数" :validate-status="errors.retryCount ? 'error' : undefined" :help="errors.retryCount || '留空使用后端默认值，范围 0–10。'">
          <a-input v-model="form.retryCount" allow-clear inputmode="numeric" placeholder="留空" @blur="validate" />
        </a-form-item>
        <a-form-item label="RTP 类型" :validate-status="errors.rtpType ? 'error' : undefined" :help="errors.rtpType || '0 / 1 / 2；留空使用后端默认值。'">
          <a-input v-model="form.rtpType" allow-clear inputmode="numeric" placeholder="留空" @blur="validate" />
        </a-form-item>
        <a-form-item label="超时（秒）" :validate-status="errors.timeoutSec ? 'error' : undefined" :help="errors.timeoutSec || '0.1–30；留空使用后端默认值。'">
          <a-input v-model="form.timeoutSec" allow-clear inputmode="decimal" placeholder="留空" @blur="validate" />
        </a-form-item>
      </div>
      <div class="form-actions">
        <a-button :disabled="loading" @click="close">取消</a-button>
        <a-button type="primary" html-type="submit" :loading="loading" :disabled="!canSubmit">确认创建</a-button>
      </div>
    </a-form>
  </a-modal>
</template>

<style scoped>
.proxy-form { margin-top: 16px; }
.form-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.form-grid--media { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.form-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
@media (max-width: 700px) { .form-grid, .form-grid--media { grid-template-columns: 1fr; } }
</style>
