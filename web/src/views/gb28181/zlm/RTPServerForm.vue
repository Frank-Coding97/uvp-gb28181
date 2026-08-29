<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import type { ZLMCapabilityState, ZLMRTPServerCreateRequest } from "@/api/gb28181-zlm-ingress";
import { buildRTPCreateRequest, type RTPServerFormState } from "./rtpServicesState";

const props = defineProps<{
  visible: boolean;
  capability: ZLMCapabilityState;
  permitted: boolean;
  loading?: boolean;
}>();
const emit = defineEmits<{
  "update:visible": [visible: boolean];
  submit: [request: ZLMRTPServerCreateRequest];
}>();

const form = reactive<RTPServerFormState>({
  vhost: "__defaultVhost__",
  app: "rtp",
  stream: "",
  port: "",
  tcpMode: "0",
  ssrc: "",
  onlyTrack: "0",
  localIp: "",
  reuse: false
});
const errors = reactive<Record<string, string>>({});
const canSubmit = computed(() => props.capability === "supported" && props.permitted && !props.loading);
const disabledReason = computed(() => {
  if (!props.permitted) return "当前账号缺少 RTP 管理权限。";
  if (props.capability === "unsupported") return "当前节点不支持完整 RTP 管理能力。";
  if (props.capability === "unknown") return "当前节点 RTP 能力尚未探测。";
  return "";
});

function clearErrors() {
  Object.keys(errors).forEach(key => delete errors[key]);
}

function reset() {
  Object.assign(form, {
    vhost: "__defaultVhost__",
    app: "rtp",
    stream: "",
    port: "",
    tcpMode: "0",
    ssrc: "",
    onlyTrack: "0",
    localIp: "",
    reuse: false
  });
  clearErrors();
}

watch(() => props.visible, visible => { if (visible) reset(); });

function validate() {
  const result = buildRTPCreateRequest(form);
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
  <a-modal :visible="visible" modal-class="uvp-system-dialog" :width="720" :footer="false" :mask-closable="false" unmount-on-close @cancel="close">
    <template #title>创建 RTP 服务</template>
    <a-alert v-if="disabledReason" type="warning">{{ disabledReason }}</a-alert>
    <a-form :model="form" layout="vertical" class="rtp-form" @submit-success="submit">
      <div class="rtp-form__grid">
        <a-form-item label="VHost" required :validate-status="errors.vhost ? 'error' : undefined" :help="errors.vhost">
          <a-input v-model="form.vhost" allow-clear @blur="validate" />
        </a-form-item>
        <a-form-item label="App" required :validate-status="errors.app ? 'error' : undefined" :help="errors.app">
          <a-input v-model="form.app" allow-clear @blur="validate" />
        </a-form-item>
        <a-form-item label="Stream" required :validate-status="errors.stream ? 'error' : undefined" :help="errors.stream">
          <a-input v-model="form.stream" allow-clear placeholder="receiver-01" @blur="validate" />
        </a-form-item>
        <a-form-item label="监听端口" :validate-status="errors.port ? 'error' : undefined" :help="errors.port || '留空或 0 由 ZLM 自动分配。'">
          <a-input v-model="form.port" allow-clear inputmode="numeric" placeholder="自动分配" @blur="validate" />
        </a-form-item>
        <a-form-item label="TCP 模式" :validate-status="errors.tcpMode ? 'error' : undefined" :help="errors.tcpMode || '0 UDP，1 TCP 被动，2 TCP 主动。'">
          <a-input v-model="form.tcpMode" allow-clear inputmode="numeric" @blur="validate" />
        </a-form-item>
        <a-form-item label="轨道模式" :validate-status="errors.onlyTrack ? 'error' : undefined" :help="errors.onlyTrack || '0 自动，1 仅音频，2 仅视频。'">
          <a-input v-model="form.onlyTrack" allow-clear inputmode="numeric" @blur="validate" />
        </a-form-item>
        <a-form-item label="SSRC" :validate-status="errors.ssrc ? 'error' : undefined" :help="errors.ssrc || '可选，最多 32 位十进制数字。'">
          <a-input v-model="form.ssrc" allow-clear inputmode="numeric" @blur="validate" />
        </a-form-item>
        <a-form-item label="本地 IP" :validate-status="errors.localIp ? 'error' : undefined" :help="errors.localIp || '可选，留空由节点决定。'">
          <a-input v-model="form.localIp" allow-clear placeholder="0.0.0.0" @blur="validate" />
        </a-form-item>
        <a-form-item label="端口复用" :validate-status="errors.reuse ? 'error' : undefined" :help="errors.reuse || '开启时必须填写明确端口。'">
          <a-switch v-model="form.reuse" @change="validate" />
        </a-form-item>
      </div>
      <div class="rtp-form__actions">
        <a-button :disabled="loading" @click="close">取消</a-button>
        <a-button type="primary" html-type="submit" :loading="loading" :disabled="!canSubmit">确认创建</a-button>
      </div>
    </a-form>
  </a-modal>
</template>

<style scoped>
.rtp-form { margin-top: 16px; }
.rtp-form__grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.rtp-form__actions { display: flex; justify-content: flex-end; gap: 8px; }
@media (max-width: 760px) { .rtp-form__grid { grid-template-columns: 1fr; } }
</style>
