<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  createZLMNode,
  updateZLMNode,
  type CreateZLMNodeReq,
  type UpdateZLMNodeReq,
  type ZLMNode
} from "@/api/gb28181-zlm";
import {
  buildNodeRequest,
  clearNodeSecret,
  createNodeFormState,
  validateNodeForm,
  type NodeFormErrors,
  type NodeFormState
} from "./nodeFormState";

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
const editing = computed(() => Boolean(props.node));

watch(
  () => [props.visible, props.node] as const,
  ([visible]) => {
    if (!visible) return;
    form.value = createNodeFormState(props.node);
    errors.value = {};
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

async function handleSubmit() {
  const nextErrors = validateNodeForm(form.value, editing.value);
  errors.value = nextErrors;
  if (Object.keys(nextErrors).length > 0) {
    Message.warning("请修正表单中的校验错误");
    return;
  }

  loading.value = true;
  try {
    const request = buildNodeRequest(form.value, editing.value);
    const response = editing.value
      ? await updateZLMNode(props.node!.id, request as UpdateZLMNodeReq)
      : await createZLMNode(request as CreateZLMNodeReq);
    if (response.code !== 0) throw new Error(response.message || "节点保存失败");

    form.value = clearNodeSecret(form.value);
    Message.success(editing.value ? "候选连接已验证并更新" : "节点已验证并创建");
    emit("saved");
    emit("update:visible", false);
  } catch (error) {
    const message = (error as { response?: { data?: { message?: string } }; message?: string })?.response?.data?.message
      || (error as Error)?.message
      || "节点保存失败";
    Message.error(message);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <a-drawer
    :visible="visible"
    :title="editing ? '编辑流媒体节点' : '添加流媒体节点'"
    :width="540"
    :mask-closable="!loading"
    :esc-to-close="!loading"
    :closable="!loading"
    :footer="false"
    class="zlm-node-form"
    unmount-on-close
    @cancel="close"
    @update:visible="handleVisibleUpdate"
  >
    <div class="form-hint" role="status">
      {{ editing
        ? "连接字段会作为候选配置先由后端探测；探测或收敛失败时保留旧节点，当前表单不会被清空。"
        : "后端会先探测 ZLM 连通性，成功后才登记节点；API Secret 只写入，不会从服务端回显。" }}
    </div>

    <a-form :model="form" layout="vertical" @submit-success="handleSubmit">
      <a-form-item label="节点名" required :validate-status="errors.name ? 'error' : undefined" :help="errors.name">
        <a-input
          v-model="form.name"
          allow-clear
          :max-length="80"
          show-word-limit
          placeholder="例如 zlm-bj-1"
          @blur="validateField('name')"
        />
      </a-form-item>

      <a-form-item label="管理地址（API Host）" required :validate-status="errors.host ? 'error' : undefined" :help="errors.host">
        <a-input
          v-model="form.host"
          allow-clear
          :max-length="255"
          placeholder="后端访问 ZLM 的 IP 或域名"
          @blur="validateField('host')"
        />
        <div class="form-tip">编辑时允许提交候选地址；后端探测通过前不会覆盖旧连接。</div>
      </a-form-item>

      <a-form-item label="设备收流地址">
        <a-input
          v-model="form.receiveHost"
          allow-clear
          :max-length="255"
          placeholder="写入 SDP 的地址，留空跟随管理地址"
        />
        <div class="form-tip">设备向此地址发送 RTP；公网部署时填写设备可达地址。</div>
      </a-form-item>

      <a-form-item label="播放访问地址">
        <a-input
          v-model="form.playbackHost"
          allow-clear
          :max-length="255"
          placeholder="浏览器可访问的 IP 或域名"
        />
        <div class="form-tip">用于后端生成安全播放出口，留空跟随管理地址。</div>
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
          :invisible-button="false"
          autocomplete="new-password"
          :max-length="512"
          :placeholder="editing ? '不修改时保持为空' : '填写 ZLM api.secret'"
          @blur="validateField('apiSecret')"
        />
        <div class="form-tip">保存成功后立即从前端表单清空，后续编辑不会加载旧值。</div>
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

      <a-form-item label="RTP 端口范围" :validate-status="errors.rtpPortStart || errors.rtpPortEnd ? 'error' : undefined" :help="errors.rtpPortStart || errors.rtpPortEnd">
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
    </a-form>

    <div class="form-actions">
      <a-button :disabled="loading" @click="close">取消</a-button>
      <a-button type="primary" :loading="loading" @click="handleSubmit">验证并保存</a-button>
    </div>
  </a-drawer>
</template>

<style scoped>
.zlm-node-form :deep(.arco-drawer-title) { color: var(--zlm-text-1); font-size: var(--zlm-fs-h2); font-weight: var(--zlm-fw-semibold); }
.form-hint { margin-bottom: var(--zlm-space-4); padding: var(--zlm-space-3) var(--zlm-space-4); color: var(--zlm-text-2); background: var(--zlm-brand-50); border-left: 3px solid var(--zlm-brand-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); line-height: 1.6; }
.form-tip { margin-top: 4px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.form-tip-inline { color: var(--zlm-text-3); }
.port-range { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr); width: 100%; }
.form-actions { position: sticky; bottom: -20px; display: flex; justify-content: flex-end; gap: var(--zlm-space-2); margin: var(--zlm-space-4) -20px -20px; padding: 14px 20px; background: var(--zlm-card); border-top: 1px solid var(--zlm-border); }
:global(.zlm-node-form .arco-input-wrapper), :global(.zlm-node-form .arco-input-password) { box-sizing: border-box; background: var(--uvp-search-control-bg) !important; border-color: var(--uvp-search-secondary-btn-border) !important; border-radius: 10px !important; box-shadow: var(--uvp-search-control-shadow) !important; }
:global(.zlm-node-form .arco-input-wrapper:focus-within), :global(.zlm-node-form .arco-input-password:focus-within) { border-color: var(--uvp-brand) !important; box-shadow: var(--uvp-search-control-focus-shadow) !important; }
@media (max-width: 600px) { .port-range { grid-template-columns: 1fr; } .form-tip-inline { display: none; } }
</style>
