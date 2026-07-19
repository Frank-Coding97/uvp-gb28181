<script setup lang="ts">
import { ref } from "vue";
import { Eye, EyeOff, RotateCcw } from "lucide-vue-next";
import type { SipSetupForm } from "../useSipSetup";
import { deriveDomain } from "../sipSetupRules";

const props = defineProps<{ form: SipSetupForm; hasExistingPassword: boolean }>();
const emit = defineEmits<{ update: [patch: Partial<SipSetupForm>] }>();
const domainMode = ref<"auto" | "custom">(
    props.form.domain && props.form.domain !== deriveDomain(props.form.serverId) ? "custom" : "auto"
);
const passwordVisible = ref(false);

function updateServerId(value: string) {
    const patch: Partial<SipSetupForm> = { serverId: value };
    if (domainMode.value === "auto") patch.domain = deriveDomain(value);
    emit("update", patch);
}

function updateDomain(value: string) {
    domainMode.value = "custom";
    emit("update", { domain: value });
}

function restoreAutomaticDomain() {
    domainMode.value = "auto";
    emit("update", { domain: deriveDomain(props.form.serverId) });
}
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

        <a-form-item label="SIP 域" required>
            <a-input
                :model-value="form.domain"
                :max-length="10"
                placeholder="默认取平台 ID 前 10 位"
                @update:model-value="updateDomain"
            >
                <template #suffix>
                    <a-button
                        v-if="domainMode === 'custom'"
                        type="text"
                        shape="circle"
                        title="恢复自动推导"
                        @click="restoreAutomaticDomain"
                    >
                        <RotateCcw :size="15" />
                    </a-button>
                </template>
            </a-input>
            <template #extra>{{ domainMode === "auto" ? "自动跟随平台 ID" : "自定义域" }}</template>
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
