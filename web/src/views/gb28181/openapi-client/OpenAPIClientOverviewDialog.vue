<script setup lang="ts">
import { OPENAPI_CLIENT_DATA_SCOPE_OPTIONS, type OpenAPIClientView } from "@/api/gb28181-openapi";

const props = defineProps<{
  visible: boolean;
  client: OpenAPIClientView | null;
}>();

const emit = defineEmits<{ close: [] }>();

function statusLabel(status: OpenAPIClientView["status"]) {
  return status === "active" ? "启用" : status === "disabled" ? "停用" : "已撤销";
}

function dataScopeLabel(scope: OpenAPIClientView["dataScope"]) {
  return OPENAPI_CLIENT_DATA_SCOPE_OPTIONS.find(option => option.value === scope)?.label || `数据范围 #${scope}`;
}

function formatTime(value?: string | null) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(
    date.getMinutes()
  )}:${pad(date.getSeconds())}`;
}
</script>

<template>
  <a-modal
    :visible="props.visible"
    title="客户端概览"
    width="720px"
    modal-class="uvp-system-dialog openapi-client-overview-dialog"
    :footer="false"
    unmount-on-close
    @cancel="emit('close')"
  >
    <a-descriptions
      v-if="props.client"
      class="uvp-system-description uvp-system-description--compact openapi-client-overview-dialog__description"
      :column="2"
      bordered
    >
      <a-descriptions-item label="客户端名称">{{ props.client.name }}</a-descriptions-item>
      <a-descriptions-item label="Access Key"
        ><code>{{ props.client.ak }}</code></a-descriptions-item
      >
      <a-descriptions-item label="归属部门">
        {{ props.client.ownerDeptName || `部门 #${props.client.ownerDeptId}` }}
      </a-descriptions-item>
      <a-descriptions-item label="数据范围">{{ dataScopeLabel(props.client.dataScope) }}</a-descriptions-item>
      <a-descriptions-item label="组织 / 公司名称">{{ props.client.responsibleOrgName || "-" }}</a-descriptions-item>
      <a-descriptions-item label="负责人姓名">{{ props.client.responsibleName || "-" }}</a-descriptions-item>
      <a-descriptions-item label="负责人联系方式">{{ props.client.responsibleContact || "-" }}</a-descriptions-item>
      <a-descriptions-item label="认证状态">{{ statusLabel(props.client.status) }}</a-descriptions-item>
      <a-descriptions-item label="调用限速">
        {{ props.client.rateLimit }} / 秒，突发 {{ props.client.burst }}
      </a-descriptions-item>
      <a-descriptions-item label="观看配额">{{ props.client.viewerQuota }}</a-descriptions-item>
      <a-descriptions-item label="创建时间">{{ formatTime(props.client.createdAt) }}</a-descriptions-item>
      <a-descriptions-item label="更新时间">{{ formatTime(props.client.updatedAt) }}</a-descriptions-item>
    </a-descriptions>
  </a-modal>
</template>

<style scoped>
.openapi-client-overview-dialog__description :deep(.arco-descriptions-body) {
  overflow: hidden;
  border-radius: 10px;
}

.openapi-client-overview-dialog__description :deep(.arco-descriptions-item-label) {
  width: 128px;
  white-space: nowrap;
}

.openapi-client-overview-dialog__description :deep(.arco-descriptions-item-value) {
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
