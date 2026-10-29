<template>
  <a-card :bordered="false" class="uvp-system-panel https-settings">
    <div class="https-settings__heading">
      <div>
        <h3>管理页面 HTTPS</h3>
        <p>控制安装包内置 Nginx 的访问方式，并管理访问证书。</p>
      </div>
      <a-button data-testid="refresh" :loading="loading" @click="refresh">刷新状态</a-button>
    </div>
    <a-alert v-if="status && !status.supported" type="info">{{ status.reason }}</a-alert>
    <a-alert v-if="error" type="error" class="https-settings__notice">{{ error }}</a-alert>
    <a-alert v-if="notice" type="info" class="https-settings__notice">{{ notice }}</a-alert>
    <a-alert v-if="status?.operation?.error" type="warning" class="https-settings__notice">
      {{ status.operation.error }}
    </a-alert>
    <fieldset :disabled="disabled">
      <a-form :model="draft" layout="vertical" class="uvp-system-form">
        <a-form-item label="是否启用 HTTPS">
          <a-switch v-model="draft.enabled" :disabled="disabled" />
          <template #extra>切换后需点击“应用 HTTPS 配置”才会生效；ZLMediaKit 的 HTTPS/WSS 仍保持开启。</template>
        </a-form-item>
        <div v-if="status?.supported" data-testid="access-preview" class="https-settings__access-preview">
          <strong>应用后访问地址</strong>
          <span>{{ draft.enabled ? "HTTPS 已选择开启" : "HTTPS 已选择关闭，将使用 HTTP" }}</span>
          <code>{{ previewAddress }}</code>
        </div>
        <a-form-item :label="draft.enabled ? 'HTTPS 端口' : 'HTTP 端口'">
          <a-space>
            <a-input-number v-model="displayPort" :min="1" :max="65535" :precision="0" :disabled="disabled || !draft.enabled" />
            <a-button v-if="draft.enabled" :loading="checking" :disabled="disabled" @click="checkPort">检测端口</a-button>
          </a-space>
          <template #extra>
            <span v-if="draft.enabled">关闭 HTTPS 后使用 HTTP 端口 {{ status?.httpPort || "—" }}。</span>
            <span v-else>HTTP 端口由安装配置提供，此处只读；再次开启将恢复 HTTPS 端口 {{ draft.port }}。</span>
          </template>
        </a-form-item>
        <div :key="uploadVersion" class="https-settings__files">
          <a-form-item v-for="field in uploadFields" :key="field.key" :label="field.label">
            <div class="https-settings__upload">
              <label
                :data-testid="`${field.key}-dropzone`"
                class="https-settings__dropzone"
                :class="{ 'is-dragover': dragging === field.key, 'is-disabled': disabled }"
                @dragover.prevent="!disabled && (dragging = field.key)"
                @dragleave.prevent="dragging = undefined"
                @drop.prevent="dropFile($event, field.key)"
              >
                <input
                  :data-testid="field.key"
                  :aria-label="field.label"
                  type="file"
                  :accept="field.accept"
                  :disabled="disabled"
                  @change="chooseFile($event, field.key)"
                />
                <IconUpload class="https-settings__upload-icon" aria-hidden="true" />
                <span class="https-settings__upload-text">拖拽文件到此处 或 <strong>点击选择文件</strong></span>
                <span class="https-settings__upload-hint">{{ field.hint }}，单文件最大 1 MiB</span>
              </label>
              <div v-if="field.file" class="https-settings__selected-file">
                <IconFile aria-hidden="true" />
                <span class="https-settings__file-name" :title="field.file.name">{{ field.file.name }}</span>
                <span class="https-settings__file-size">{{ Math.max(1, Math.ceil(field.file.size / 1024)) }} KB</span>
                <a-button
                  :data-testid="`remove-${field.key}`"
                  :aria-label="`移除${field.label}`"
                  type="text"
                  size="small"
                  :disabled="disabled"
                  @click="selectFile(undefined, field.key)"
                  >移除</a-button
                >
              </div>
            </div>
          </a-form-item>
        </div>
        <p v-if="validating">正在校验证书与私钥…</p>
        <p v-if="uploaded">新证书：{{ uploaded.subject }} · 有效期至 {{ formatDate(uploaded.notAfter) }}</p>
        <a-form-item v-if="certificate || privateKey" label="证书同步">
          <div>
            <label class="https-settings__checkbox">
              <input v-model="syncZlm" data-testid="sync-zlm" type="checkbox" :disabled="disabled" />
              同时同步到 ZLMediaKit
            </label>
            <p v-if="syncZlm" class="https-settings__warning">
              同步需要重启 ZLMediaKit，正在进行的预览、回放、推流及对讲可能中断，需要重新连接。
            </p>
            <p v-else>仅更新 Nginx 证书，ZLMediaKit 保留原证书。</p>
          </div>
        </a-form-item>
      </a-form>
    </fieldset>
    <div v-if="status?.supported" class="https-settings__status">
      <span>当前入口：{{ status.nginxHealthy ? "探测正常" : "尚未确认生效" }}</span>
      <span v-if="status.operation">最近操作：{{ operationLabel(status.operation.state) }}</span>
      <span v-if="status.certificate"
        >当前证书：{{ status.certificate.subject }} · {{ formatDate(status.certificate.notAfter) }}</span
      >
      <span v-if="status.nginxFingerprint" class="https-settings__fingerprint">Nginx 指纹：{{ status.nginxFingerprint }}</span>
      <span v-if="status.zlmFingerprint" class="https-settings__fingerprint">ZLMediaKit 指纹：{{ status.zlmFingerprint }}</span>
      <span v-if="status.probeError">{{ status.probeError }}</span>
    </div>
    <div class="https-settings__actions">
      <a-button
        data-testid="apply"
        type="primary"
        :loading="busy"
        :disabled="disabled || validating || !!((certificate || privateKey) && !uploaded)"
        @click="confirmApply"
        >应用 HTTPS 配置</a-button
      >
      <a v-if="targetAddress" :href="targetAddress" target="_blank" rel="noopener noreferrer">访问新入口：{{ targetAddress }}</a>
    </div>
    <p v-if="targetAddress">协议或端口变化后可能需要重新登录；请在新入口刷新状态，确认配置已生效。</p>
  </a-card>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { IconFile, IconUpload } from "@arco-design/web-vue/es/icon";
import { hasPermission } from "@/hooks/usePermission";
import { applyHTTPS, checkHTTPSPort, getHTTPSStatus, validateHTTPSCertificate } from "@/api/https-management";
import type { HTTPSCertificate, HTTPSOperation, HTTPSStatus } from "@/api/https-management";

const status = ref<HTTPSStatus>();
// 与后端默认保持一致：新装的管理页是明文 HTTP，开关由管理员在这里打开。
const draft = reactive({ enabled: false, port: 51000 });
const certificate = ref<File>();
const privateKey = ref<File>();
type UploadField = "certificate" | "privateKey";
const dragging = ref<UploadField>();
const uploadFields = computed(() => [
  {
    key: "certificate" as const,
    label: "证书文件",
    accept: ".pem,.crt,.cer",
    hint: "支持 PEM / CRT / CER",
    file: certificate.value
  },
  { key: "privateKey" as const, label: "私钥文件", accept: ".pem,.key", hint: "支持未加密 PEM / KEY", file: privateKey.value }
]);
const uploaded = ref<HTTPSCertificate>();
const syncZlm = ref(true);
const loading = ref(false);
const checking = ref(false);
const validating = ref(false);
const busy = ref(false);
const error = ref("");
const notice = ref("");
const targetAddress = ref("");
const uploadVersion = ref(0);
let submitted: { id: string; generation: number } | undefined;
const disabled = computed(() => !status.value?.supported || busy.value || !hasPermission("system:config:update"));
let timer: ReturnType<typeof setTimeout> | undefined;
let validationGeneration = 0;
let disposed = false;
const formatDate = (value: string) => new Date(value).toLocaleDateString();
const operationLabel = (state: HTTPSOperation["state"]) =>
  ({ running: "执行中", succeeded: "已生效", failed: "失败", partial: "部分成功", recovery_required: "需要检查恢复" })[state];
const serviceLabel = (state: string) =>
  ({ pending: "等待应用", unchanged: "未变更", applied: "已应用", restored: "已恢复", recovery_required: "需要检查恢复" })[
    state
  ] || state;
const displayPort = computed({
  get: () => (draft.enabled ? draft.port : status.value?.httpPort),
  set: (port: number | undefined) => {
    if (draft.enabled && port !== undefined) draft.port = port;
  }
});
const previewAddress = computed(() => {
  const url = new URL(window.location.href);
  url.protocol = draft.enabled ? "https:" : "http:";
  url.port = String(draft.enabled ? draft.port : status.value?.httpPort);
  return url.href;
});
async function refresh() {
  loading.value = true;
  clearTimeout(timer);
  try {
    const response = await getHTTPSStatus();
    status.value = response.data;
    busy.value = response.data.operation?.state === "running";
    error.value = "";
    if (response.data.supported && !busy.value) Object.assign(draft, response.data.settings);
    const operation = response.data.operation;
    if (submitted && operation?.id === submitted.id && operation.state !== "running") {
      if (operation.nginx === "applied" && submitted.generation === validationGeneration) {
        certificate.value = undefined;
        privateKey.value = undefined;
        uploaded.value = undefined;
        ++validationGeneration;
        ++uploadVersion.value;
      }
      submitted = undefined;
    }
    if (!busy.value && response.data.operation) {
      notice.value = `最近操作${operationLabel(response.data.operation.state)}；Nginx：${serviceLabel(response.data.operation.nginx)}，ZLMediaKit：${serviceLabel(response.data.operation.zlm)}`;
    }
    if (busy.value && !disposed) timer = setTimeout(refresh, 2000);
  } catch (e) {
    busy.value = false;
    error.value = targetAddress.value
      ? "连接已变化，尚未确认生效。请打开新入口，登录后刷新状态。"
      : (e as Error).message || "读取 HTTPS 状态失败";
  } finally {
    loading.value = false;
  }
}
async function checkPort() {
  checking.value = true;
  try {
    await checkHTTPSPort(draft.port);
    Message.success("端口检测通过；应用时将再次校验");
  } catch (e) {
    Message.error((e as Error).message || "端口检测失败");
  } finally {
    checking.value = false;
  }
}
async function chooseFile(event: Event, field: "certificate" | "privateKey") {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (file) await selectFile(file, field);
}
async function dropFile(event: DragEvent, field: UploadField) {
  dragging.value = undefined;
  if (disabled.value) return;
  const files = event.dataTransfer?.files;
  if (!files?.length) return;
  if (files.length !== 1) {
    error.value = "每次只能选择一个文件";
    return;
  }
  await selectFile(files[0], field);
}
async function selectFile(file: File | undefined, field: UploadField) {
  if (disabled.value) return;
  if (file && !(field === "certificate" ? /\.(pem|crt|cer)$/i : /\.(pem|key)$/i).test(file.name)) {
    error.value = field === "certificate" ? "请选择 PEM、CRT 或 CER 格式的证书" : "请选择未加密 PEM 或 KEY 格式的私钥";
    return;
  }
  if (file && file.size > 1024 * 1024) {
    error.value = "每个证书或私钥文件不能超过 1 MiB";
    return;
  }
  if (field === "certificate") certificate.value = file;
  else privateKey.value = file;
  uploaded.value = undefined;
  error.value = "";
  const generation = ++validationGeneration;
  validating.value = false;
  if (!certificate.value || !privateKey.value) return;
  validating.value = true;
  try {
    const response = await validateHTTPSCertificate(certificate.value, privateKey.value);
    if (generation === validationGeneration) uploaded.value = response.data;
  } catch (e) {
    if (generation === validationGeneration) error.value = (e as Error).message || "证书校验失败";
  } finally {
    if (generation === validationGeneration) validating.value = false;
  }
}
function confirmApply() {
  if (disabled.value || validating.value || ((certificate.value || privateKey.value) && !uploaded.value)) return;
  if (!Number.isInteger(draft.port) || draft.port < 1 || draft.port > 65535) {
    Message.error("请输入 1—65535 之间的 HTTPS 端口");
    return;
  }
  const synchronize = !!uploaded.value && syncZlm.value;
  const options = { ...draft, syncZlm: synchronize };
  const files = { certificate: certificate.value, privateKey: privateKey.value };
  const generation = validationGeneration;
  const nextAddress = previewAddress.value;
  Modal.confirm({
    title: "应用 HTTPS 配置",
    content:
      (synchronize
        ? "本次将重载 Nginx 并重启 ZLMediaKit。预览、回放、推流及对讲可能中断，需要重新连接。"
        : "本次将重载 Nginx，管理页面连接可能断开。") + `应用后请访问：${nextAddress}。是否继续？`,
    onOk: async () => {
      if (disabled.value) return;
      busy.value = true;
      error.value = "";
      targetAddress.value = nextAddress;
      try {
        const response = await applyHTTPS(options, files.certificate, files.privateKey);
        submitted = { id: response.data.id, generation };
        notice.value = "配置正在应用，请等待状态确认；连接断开时请使用新入口。";
        if (!disposed) timer = setTimeout(refresh, 1500);
      } catch (e) {
        busy.value = false;
        error.value = `${(e as Error).message || "应用请求失败"}。尚未确认生效，请刷新状态或访问新入口核对。`;
      }
    }
  });
}
onMounted(refresh);
onBeforeUnmount(() => {
  disposed = true;
  ++validationGeneration;
  clearTimeout(timer);
});
</script>

<style scoped lang="scss">
.https-settings {
  max-width: 1000px;
}
.https-settings__heading {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 20px;
}
h3 {
  margin: 0 0 8px;
}
p {
  line-height: 1.6;
  color: var(--uvp-text-secondary);
}
fieldset {
  min-width: 0;
  padding: 0;
  margin: 20px 0 0;
  border: 0;
}
.https-settings__access-preview {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px;
  margin-bottom: 20px;
  color: var(--color-text-1);
  background: var(--color-primary-light-1);
  border: 1px solid rgb(var(--primary-6));
  border-radius: 8px;
}
.https-settings__access-preview code {
  font-size: 16px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.https-settings__files {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 24px;
}
.https-settings__files > :deep(.arco-form-item),
.https-settings__upload {
  width: 100%;
  min-width: 0;
}
.https-settings__dropzone {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 8px;
  align-items: center;
  justify-content: center;
  min-height: 160px;
  padding: 24px 16px;
  text-align: center;
  background: var(--uvp-list-panel-bg);
  border: 1.5px dashed var(--uvp-panel-border);
  border-radius: 10px;
  transition:
    background 0.2s,
    border-color 0.2s;

  &:hover:not(.is-disabled),
  &:focus-within,
  &.is-dragover {
    outline: 2px solid var(--uvp-brand-soft);
    background: var(--uvp-brand-soft);
    border-color: var(--uvp-brand);
  }

  &.is-disabled {
    opacity: 0.5;
  }

  input {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    cursor: pointer;
    opacity: 0;

    &:disabled {
      cursor: not-allowed;
    }
  }
}
.https-settings__upload-icon {
  font-size: 32px;
  color: var(--uvp-text-tertiary);
}
.https-settings__upload-text {
  font-size: 14px;
  color: var(--uvp-text-secondary);

  strong {
    font-weight: 500;
    color: var(--uvp-brand);
  }
}
.https-settings__upload-hint,
.https-settings__file-size {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.https-settings__selected-file {
  display: flex;
  gap: 8px;
  align-items: center;
  padding: 8px 12px;
  margin-top: 10px;
  background: var(--uvp-list-panel-bg);
  border-radius: 8px;
}
.https-settings__file-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.https-settings__file-size {
  flex-shrink: 0;
}
.https-settings__notice {
  margin-bottom: 12px;
}
.https-settings__checkbox {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  cursor: pointer;
}
.https-settings__warning {
  color: var(--color-warning-6);
}
.https-settings__status {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px;
  margin-bottom: 20px;
  background: var(--color-fill-1);
  border-radius: 8px;
}
.https-settings__fingerprint {
  font-size: 12px;
  color: var(--uvp-text-secondary);
  overflow-wrap: anywhere;
}
.https-settings__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: center;
}
.https-settings__actions a {
  color: rgb(var(--primary-6));
  overflow-wrap: anywhere;
}

@media (width <= 600px) {
  .https-settings__files {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
