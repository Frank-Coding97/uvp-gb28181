<script setup lang="ts">
import { computed } from "vue";
import type { SipSetupForm } from "../useSipSetup";
import { formatRegisterUri } from "../sipSetupRules";

const props = defineProps<{ form: SipSetupForm; hasPassword: boolean }>();
const registerUri = computed(() => formatRegisterUri(props.form.serverId, props.form.advertiseIp, props.form.port));
const rows = computed(() => [
    { label: "部署方式", value: props.form.deploymentMode === "public" ? "公网部署" : "局域网部署" },
    { label: "SIP 监听地址", value: `${props.form.listenIp}:${props.form.port}` },
    { label: "设备接入地址", value: `${props.form.advertiseIp}:${props.form.port}` },
    { label: "设备注册 URI", value: registerUri.value },
    { label: "平台 ID", value: props.form.serverId },
    { label: "SIP 域", value: props.form.domain },
    { label: "SIP 密码", value: props.form.password || props.hasPassword ? "******" : "-" }
]);
</script>

<template>
    <div class="confirm-list">
        <div v-for="row in rows" :key="row.label" class="confirm-row">
            <span>{{ row.label }}</span>
            <code>{{ row.value || "-" }}</code>
        </div>
        <a-alert type="warning" :show-icon="true">保存后需重启服务，新配置才会生效。</a-alert>
    </div>
</template>

<style scoped>
.confirm-list {
    display: grid;
    max-width: 560px;
    margin: 0 auto;
    border-top: 1px solid var(--uvp-divider);
}

.confirm-row {
    display: grid;
    grid-template-columns: 130px minmax(0, 1fr);
    gap: 16px;
    padding: 11px 4px;
    color: var(--uvp-text-tertiary);
    border-bottom: 1px solid var(--uvp-divider);
}

.confirm-row code {
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--uvp-text-primary);
    font-size: 13px;
    letter-spacing: 0;
}

.confirm-list :deep(.arco-alert) {
    margin-top: 16px;
}

@media (max-width: 520px) {
    .confirm-row {
        grid-template-columns: 1fr;
        gap: 4px;
    }
}
</style>
