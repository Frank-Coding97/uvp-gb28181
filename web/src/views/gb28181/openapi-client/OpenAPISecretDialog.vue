<script setup lang="ts">
import { Message } from "@arco-design/web-vue";

const props = defineProps<{
  visible: boolean;
  accessKey: string;
  secretKey: string;
  operation: "create" | "rotate";
}>();

const emit = defineEmits<{ close: []; configure: [] }>();

async function copy(value: string, label: string) {
  if (!value || !navigator.clipboard) return;
  try {
    await navigator.clipboard.writeText(value);
    Message.success(`${label}已复制`);
  } catch {
    Message.error("复制失败，请手动复制");
  }
}

function close() {
  emit("close");
}
</script>

<template>
  <a-modal
    :visible="props.visible"
    title="一次性密钥"
    :mask-closable="false"
    :esc-to-close="false"
    :closable="false"
    :footer="false"
    @cancel="close"
  >
    <a-alert type="warning" class="openapi-secret-dialog__warning">
      {{ operation === "create" ? "SK 只在创建成功后展示这一次。" : "轮换后旧 SK 立即失效，新的 SK 只展示这一次。" }}
      关闭窗口后平台无法找回明文 SK，请先将它交给接入方安全保存。
    </a-alert>
    <div class="openapi-secret-dialog__field">
      <span class="openapi-secret-dialog__label">AK</span>
      <code>{{ accessKey }}</code>
      <a-button type="text" size="small" aria-label="复制 AK" @click="copy(accessKey, 'AK')">复制</a-button>
    </div>
    <div class="openapi-secret-dialog__field openapi-secret-dialog__field--secret">
      <span class="openapi-secret-dialog__label">SK</span>
      <code>{{ secretKey }}</code>
      <a-button type="text" size="small" aria-label="复制 SK" @click="copy(secretKey, 'SK')">复制</a-button>
    </div>
    <div class="openapi-secret-dialog__actions">
      <a-button @click="close">我已安全保存</a-button>
      <a-button v-if="operation === 'create'" type="primary" @click="emit('configure')">继续配置能力</a-button>
    </div>
  </a-modal>
</template>

<style scoped>
.openapi-secret-dialog__warning {
  margin-bottom: 16px;
}

.openapi-secret-dialog__field {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border-2);
}

.openapi-secret-dialog__field:last-child {
  border-bottom: 0;
}

.openapi-secret-dialog__label {
  flex: 0 0 32px;
  font-weight: 600;
  color: var(--color-text-2);
}

.openapi-secret-dialog__field code {
  flex: 1;
  min-width: 0;
  color: var(--color-text-1);
  overflow-wrap: anywhere;
  user-select: text;
}

.openapi-secret-dialog__field--secret code {
  color: rgb(var(--warning-6));
}

.openapi-secret-dialog__actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 18px;
}
</style>
