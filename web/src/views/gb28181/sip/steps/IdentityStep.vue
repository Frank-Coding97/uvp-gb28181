<script setup lang="ts">
import { ref, watch } from "vue";
import { Eye, EyeOff } from "lucide-vue-next";
import type { SipSetupForm } from "../useSipSetup";
import { deriveDomain } from "../sipSetupRules";

const props = defineProps<{ form: SipSetupForm; hasExistingPassword: boolean }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();
// 引导页首次配置场景,默认让密码明文可见(新填的密码需要能看到自己在敲什么).
// 用户点眼睛图标可切回遮罩.
const passwordVisible = ref(true);

// SIP 域按 GB/T 28181-2016 规定必然是平台 ID 前 10 位(行政区划码).
// 用户不再手动编辑,serverId 变化时自动同步 domain.
function updateServerId(value: string) {
    emit("update", { serverId: value, domain: deriveDomain(value) });
}

// 兜底:进入本步时若 domain 与 serverId 不一致(比如老配置或跨步骤回填)也同步一次.
watch(
    () => props.form.serverId,
    serverId => {
        const derived = deriveDomain(serverId);
        if (derived && derived !== props.form.domain) emit("update", { domain: derived });
    },
    { immediate: true }
);
</script>

<template>
    <a-form layout="vertical" class="identity-form">
        <div class="form-grid">
            <a-form-item label="SIP 端口" required>
                <a-input-number
                    :model-value="form.port"
                    :min="1"
                    :max="65535"
                    @change="emit('update', { port: Number($event) })"
                />
            </a-form-item>
            <a-form-item label="SIP 平台 ID" required>
                <a-input
                    :model-value="form.serverId"
                    :max-length="20"
                    placeholder="20 位数字编码"
                    @update:model-value="updateServerId"
                />
            </a-form-item>
        </div>

        <a-form-item label="SIP 域">
            <a-input
                :model-value="form.domain"
                disabled
                placeholder="填入平台 ID 后自动生成"
            />
            <template #extra>按 GB/T 28181-2016,SIP 域取平台 ID 前 10 位(行政区划码),无需手动填写。</template>
        </a-form-item>

        <a-form-item label="SIP 密码" :required="!hasExistingPassword">
            <a-input
                :model-value="form.password"
                :type="passwordVisible ? 'text' : 'password'"
                :placeholder="hasExistingPassword ? '留空保留原密码' : '至少 6 位'"
                @update:model-value="emit('update', { password: $event })"
            >
                <template #suffix>
                    <a-button
                        type="text"
                        shape="circle"
                        :title="passwordVisible ? '隐藏密码' : '显示密码'"
                        @click="passwordVisible = !passwordVisible"
                    >
                        <EyeOff v-if="passwordVisible" :size="16" />
                        <Eye v-else :size="16" />
                    </a-button>
                </template>
            </a-input>
        </a-form-item>
    </a-form>
</template>

<style scoped>
.identity-form {
    max-width: 560px;
    margin: 0 auto;
}

.form-grid {
    display: grid;
    grid-template-columns: minmax(140px, 0.6fr) minmax(260px, 1.4fr);
    gap: 14px;
}

@media (max-width: 640px) {
    .form-grid {
        grid-template-columns: 1fr;
        gap: 0;
    }
}
</style>
