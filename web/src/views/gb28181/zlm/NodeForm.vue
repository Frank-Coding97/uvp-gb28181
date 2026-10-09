<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  createZLMNode,
  probeZLMNode,
  updateZLMNode,
  type CreateZLMNodeReq,
  type UpdateZLMNodeReq,
  type ZLMNode,
  type ZLMNodeProbeResult
} from "@/api/gb28181-zlm";
import {
  buildNodeRequest,
  clearNodeSecret,
  createNodeFormState,
  validateNodeForm,
  type NodeFormErrors,
  type NodeFormState
} from "./nodeFormState";
import { zlmErrorPresentation } from "./components/zlmFormatters";

const props = defineProps<{
  visible: boolean;
  node?: ZLMNode | null;
}>();

const emit = defineEmits<{
  "update:visible": [visible: boolean];
  saved: [];
}>();

const form = ref<NodeFormState>(createNodeFormState());
const errors = ref<NodeFormErrors>({});
const loading = ref(false);
const currentStep = ref(1);
const probeResult = ref<ZLMNodeProbeResult | null>(null);
const editing = computed(() => Boolean(props.node));
const protocolItems = computed(() => {
  const config = probeResult.value?.serverConfig;
  return [
    { label: "RTSP", enabled: Boolean(config?.rtspEnabled) },
    { label: "RTMP", enabled: Boolean(config?.rtmpEnabled) },
    { label: "HLS", enabled: Boolean(config?.hlsEnabled) },
    { label: "TS", enabled: Boolean(config?.tsEnabled) },
    { label: "fMP4", enabled: Boolean(config?.fmp4Enabled) }
  ];
});

watch(
  () => [props.visible, props.node] as const,
  ([visible]) => {
    if (!visible) return;
    form.value = createNodeFormState(props.node);
    errors.value = {};
    currentStep.value = 1;
    probeResult.value = null;
  },
  { immediate: true }
);

function validateField(field: keyof NodeFormState) {
  const next = validateNodeForm(form.value, editing.value);
  errors.value = { ...errors.value, [field]: next[field] };
}

function close() {
  if (loading.value) return;
  form.value = clearNodeSecret(form.value);
  emit("update:visible", false);
}

function handleVisibleUpdate(value: boolean) {
  if (!value) close();
}

async function handleProbe() {
  const nextErrors = validateNodeForm(form.value, false);
  errors.value = nextErrors;
  if (Object.keys(nextErrors).length > 0) {
    Message.warning("请先完整填写必填参数");
    return;
  }

  loading.value = true;
  try {
    const request = buildNodeRequest(form.value, false) as CreateZLMNodeReq;
    const response = await probeZLMNode(request);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取 ZL 信息失败");
    probeResult.value = response.data;
    currentStep.value = 2;
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    loading.value = false;
  }
}

function returnToConnectionStep() {
  if (loading.value) return;
  probeResult.value = null;
  currentStep.value = 1;
}

async function handleSubmit() {
  const nextErrors = validateNodeForm(form.value, editing.value);
  errors.value = nextErrors;
  if (Object.keys(nextErrors).length > 0) {
    Message.warning("请修正表单中的校验错误");
    return;
  }

  loading.value = true;
  let saved = false;
  try {
    const request = buildNodeRequest(form.value, editing.value);
    const response = editing.value
      ? await updateZLMNode(props.node!.id, request as UpdateZLMNodeReq)
      : await createZLMNode(request as CreateZLMNodeReq);
    if (response.code !== 0) throw new Error(response.message || "节点保存失败");

    saved = true;
    Message.success(editing.value ? "候选连接已验证并更新" : "节点已验证并创建");
  } catch (error) {
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    loading.value = false;
    if (saved) {
      close();
      emit("saved");
    }
  }
}
</script>

<template>
  <a-modal
    :visible="visible"
    :title="editing ? '编辑流媒体节点' : '添加流媒体节点'"
    modal-class="uvp-system-dialog zlm-node-form"
    :width="820"
    :mask-closable="!loading"
    :esc-to-close="!loading"
    :closable="!loading"
    unmount-on-close
    @cancel="close"
    @update:visible="handleVisibleUpdate"
  >
    <div v-if="editing" class="form-hint" role="status">
      {{
        editing
          ? "连接字段会作为候选配置先由后端探测；探测或收敛失败时保留旧节点，当前表单不会被清空。"
          : "后端会先探测 ZLM 连通性，成功后才登记节点；API Secret 只写入，不会从服务端回显。"
      }}
    </div>

    <a-steps v-if="!editing" class="create-steps" :current="currentStep" size="small">
      <a-step title="必填参数" description="填写 ZL 连接信息" />
      <a-step title="ZL 信息确认" description="读取后确认添加" />
    </a-steps>

    <a-form v-if="editing || currentStep === 1" :model="form" layout="vertical" @submit-success="handleSubmit">
      <div v-if="!editing" class="form-section-title">必填参数</div>
      <a-form-item
        v-if="editing"
        label="节点名"
        required
        :validate-status="errors.name ? 'error' : undefined"
        :help="errors.name"
      >
        <a-input
          v-model="form.name"
          allow-clear
          :max-length="80"
          show-word-limit
          placeholder="例如 zlm-bj-1"
          @blur="validateField('name')"
        />
      </a-form-item>

      <a-form-item
        :label="editing ? '管理地址（API Host）' : 'IP 地址'"
        required
        :validate-status="errors.host ? 'error' : undefined"
        :help="errors.host"
      >
        <a-input
          v-model="form.host"
          allow-clear
          :max-length="255"
          :placeholder="editing ? '后端访问 ZLM 的 IP 或域名' : '后端可访问的 ZLM IP'"
          @blur="validateField('host')"
        />
        <div v-if="editing" class="form-tip">编辑时允许提交候选地址；后端探测通过前不会覆盖旧连接。</div>
      </a-form-item>

      <a-form-item label="API 端口" required :validate-status="errors.apiPort ? 'error' : undefined" :help="errors.apiPort">
        <a-input
          v-model="form.apiPort"
          allow-clear
          inputmode="numeric"
          :max-length="5"
          placeholder="1-65535"
          @blur="validateField('apiPort')"
        />
      </a-form-item>

      <a-form-item
        :label="editing ? 'API Secret（只写，留空不修改）' : 'API Secret（只写）'"
        :required="!editing"
        :validate-status="errors.apiSecret ? 'error' : undefined"
        :help="errors.apiSecret"
      >
        <a-input-password
          v-model="form.apiSecret"
          allow-clear
          :invisible-button="true"
          autocomplete="new-password"
          :max-length="512"
          :placeholder="editing ? '不修改时保持为空' : '填写 ZLM api.secret'"
          @blur="validateField('apiSecret')"
        />
        <div class="form-tip">保存成功后立即从前端表单清空，后续编辑不会加载旧值。</div>
      </a-form-item>

      <template v-if="editing">
        <a-form-item label="设备收流地址">
          <a-input v-model="form.receiveHost" allow-clear :max-length="255" placeholder="写入 SDP 的地址，留空跟随管理地址" />
          <div class="form-tip">设备向此地址发送 RTP；公网部署时填写设备可达地址。</div>
        </a-form-item>

        <a-form-item label="调度权重" :validate-status="errors.weight ? 'error' : undefined" :help="errors.weight">
          <a-input
            v-model="form.weight"
            allow-clear
            inputmode="numeric"
            :max-length="3"
            placeholder="0-100"
            @blur="validateField('weight')"
          />
          <div class="form-tip">0 表示不参与加权调度；不会在输入时静默修正数值。</div>
        </a-form-item>

        <a-form-item
          label="RTP 端口范围"
          :validate-status="errors.rtpPortStart || errors.rtpPortEnd ? 'error' : undefined"
          :help="errors.rtpPortStart || errors.rtpPortEnd"
        >
          <a-space class="port-range">
            <a-input
              v-model="form.rtpPortStart"
              allow-clear
              inputmode="numeric"
              :max-length="5"
              aria-label="RTP 起始端口"
              @blur="validateField('rtpPortStart')"
            />
            <span class="form-tip-inline">至</span>
            <a-input
              v-model="form.rtpPortEnd"
              allow-clear
              inputmode="numeric"
              :max-length="5"
              aria-label="RTP 结束端口"
              @blur="validateField('rtpPortEnd')"
            />
          </a-space>
          <div class="form-tip">范围 1024-65535，结束端口不得小于起始端口。</div>
        </a-form-item>
      </template>

      <div class="form-section-title">媒体网络地址覆盖</div>
      <a-form-item label="Hook IP 覆盖" :validate-status="errors.hookIp ? 'error' : undefined" :help="errors.hookIp">
        <a-input
          v-model="form.hookIp"
          allow-clear
          :max-length="45"
          placeholder="具体 IPv4 或 IPv6"
          @blur="validateField('hookIp')"
        />
        <div class="form-tip">留空使用平台默认 Hook IP；平台默认未配置时沿用原地址策略。</div>
      </a-form-item>

      <a-form-item label="SDP IP 覆盖" :validate-status="errors.sdpIp ? 'error' : undefined" :help="errors.sdpIp">
        <a-input
          v-model="form.sdpIp"
          allow-clear
          :max-length="253"
          placeholder="设备可访问的 IP 或域名"
          @blur="validateField('sdpIp')"
        />
        <div class="form-tip">
          该节点下发 SDP 时通告的地址（设备据此回推 RTP 流）；留空使用平台默认 SDP IP，不能填 127.0.0.1。
        </div>
      </a-form-item>

      <a-form-item
        label="Stream IP 覆盖"
        :validate-status="errors.playbackHost ? 'error' : undefined"
        :help="errors.playbackHost"
      >
        <a-input
          v-model="form.playbackHost"
          allow-clear
          :max-length="253"
          placeholder="具体 IP 地址或域名"
          @blur="validateField('playbackHost')"
        />
        <div class="form-tip">留空使用平台默认 Stream IP；平台默认未配置时沿用原播放地址策略。</div>
      </a-form-item>
    </a-form>

    <section v-else class="probe-preview" aria-label="ZL 信息确认">
      <div class="probe-preview__status">
        <span class="probe-preview__indicator" aria-hidden="true" />
        <div>
          <div class="probe-preview__title">ZL 连接成功</div>
          <div class="probe-preview__subtitle">已从 {{ form.host }}:{{ form.apiPort }} 读取服务配置，请确认后添加节点。</div>
        </div>
      </div>

      <a-descriptions class="probe-details" :column="2" bordered size="medium">
        <a-descriptions-item label="IP 地址"
          ><span class="probe-value">{{ form.host }}</span></a-descriptions-item
        >
        <a-descriptions-item label="API 端口"
          ><span class="probe-value">{{ form.apiPort }}</span></a-descriptions-item
        >
        <a-descriptions-item label="API Secret"><span class="secret-confirmed">已填写（不回显）</span></a-descriptions-item>
        <a-descriptions-item label="mediaServerId"
          ><span class="probe-value">
            {{ probeResult?.mediaServerId || "未配置（登记后由平台生成）" }}
          </span></a-descriptions-item
        >
        <a-descriptions-item label="HTTP PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.httpPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="HTTPS PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.httpsPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="RTSP PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.rtspPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="RTSPS PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.rtspsPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="RTMP PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.rtmpPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="RTMPS PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.rtmpsPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="RTP Proxy PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.rtpProxyPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="ONVIF PORT"
          ><span class="probe-value">{{ probeResult?.serverConfig.onvifPort || "未开放" }}</span></a-descriptions-item
        >
        <a-descriptions-item label="RTP 端口范围"
          ><span class="probe-value">{{ form.rtpPortStart }} - {{ form.rtpPortEnd }}</span></a-descriptions-item
        >
        <a-descriptions-item label="协议状态" :span="2">
          <a-space wrap>
            <a-tag v-for="protocol in protocolItems" :key="protocol.label" :color="protocol.enabled ? 'green' : 'gray'">
              {{ protocol.label }} · {{ protocol.enabled ? "启用" : "关闭" }}
            </a-tag>
          </a-space>
        </a-descriptions-item>
      </a-descriptions>
    </section>

    <template #footer>
      <div class="form-actions">
        <a-button :disabled="loading" @click="close">取消</a-button>
        <a-button v-if="!editing && currentStep === 2" :disabled="loading" @click="returnToConnectionStep">上一步</a-button>
        <a-button v-if="!editing && currentStep === 1" type="primary" :loading="loading" @click="handleProbe"
          >连接并读取</a-button
        >
        <a-button v-else type="primary" :loading="loading" @click="handleSubmit">{{
          editing ? "验证并保存" : "确认添加"
        }}</a-button>
      </div>
    </template>
  </a-modal>
</template>

<style scoped>
:global(.zlm-node-form .arco-modal-title) {
  font-size: var(--zlm-fs-h2);
  font-weight: var(--zlm-fw-semibold);
  color: var(--zlm-text-1);
}
:global(.zlm-node-form .arco-modal-body) {
  max-height: min(72vh, 720px);
  overflow-y: auto;
}
:global(.zlm-node-form .arco-form-item-content-flex) {
  flex-direction: column;
  align-items: stretch;
}
.form-hint {
  padding: var(--zlm-space-3) var(--zlm-space-4);
  margin-bottom: var(--zlm-space-4);
  font-size: var(--zlm-fs-caption);
  line-height: 1.6;
  color: var(--zlm-text-2);
  background: var(--zlm-brand-50);
  border-left: 3px solid var(--zlm-brand-500);
  border-radius: var(--zlm-radius-md);
}
.create-steps {
  padding: 0 var(--zlm-space-6);
  margin: 4px 0 var(--zlm-space-6);
}
.form-section-title {
  margin: 0 0 var(--zlm-space-4);
  font-size: var(--zlm-fs-body);
  font-weight: var(--zlm-fw-semibold);
  color: var(--zlm-text-1);
}
.probe-preview {
  display: grid;
  gap: var(--zlm-space-5);
}
.probe-preview__status {
  display: flex;
  gap: var(--zlm-space-3);
  align-items: center;
  padding: var(--zlm-space-4);
  background: var(--zlm-success-50, #f2fbf7);
  border: 1px solid var(--zlm-success-200, #bce8d2);
  border-radius: var(--zlm-radius-lg);
}
.probe-preview__indicator {
  flex: 0 0 auto;
  width: 10px;
  height: 10px;
  background: var(--zlm-success-500, #00a870);
  border-radius: 50%;
  box-shadow: 0 0 0 5px rgb(0 168 112 / 12%);
}
.probe-preview__title {
  font-weight: var(--zlm-fw-semibold);
  color: var(--zlm-text-1);
}
.probe-preview__subtitle {
  margin-top: 3px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.probe-details {
  width: 100%;
}
.probe-details :deep(.arco-descriptions-item-label) {
  width: 132px;
  font-weight: var(--zlm-fw-medium);
  color: var(--zlm-text-3);
}
.probe-details :deep(.arco-descriptions-item-value) {
  color: var(--zlm-text-1);
}
.probe-value {
  font-family: var(--zlm-font-mono);
}
.secret-confirmed {
  color: var(--zlm-success-600);
}
.form-tip {
  margin-top: 4px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}
.form-tip-inline {
  color: var(--zlm-text-3);
}
.port-range {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  width: 100%;
}
.form-actions {
  display: flex;
  gap: var(--zlm-space-2);
  justify-content: flex-end;
}
:global(.zlm-node-form .arco-input-wrapper),
:global(.zlm-node-form .arco-input-password) {
  box-sizing: border-box;
  background: var(--uvp-search-control-bg) !important;
  border-color: var(--uvp-search-secondary-btn-border) !important;
  border-radius: 10px !important;
  box-shadow: var(--uvp-search-control-shadow) !important;
}
:global(.zlm-node-form .arco-input-wrapper:focus-within),
:global(.zlm-node-form .arco-input-password:focus-within) {
  border-color: var(--uvp-brand) !important;
  box-shadow: var(--uvp-search-control-focus-shadow) !important;
}

@media (width <= 600px) {
  .port-range {
    grid-template-columns: 1fr;
  }
  .form-tip-inline {
    display: none;
  }
}
</style>
